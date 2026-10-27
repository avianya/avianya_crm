package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"crm-lead-management/backend/internal/auth"
	"crm-lead-management/backend/internal/config"
	"crm-lead-management/backend/internal/mail"
	"crm-lead-management/backend/internal/model"
	"crm-lead-management/backend/internal/store"
)

// Login and lead visibility, through the real router and database: two
// salespeople with different leads, a manager and an admin.

type authEnv struct {
	router *gin.Engine
	st     *store.Store
	db     *pgxpool.Pool
	stamp  int64
}

func newAuthEnv(t *testing.T) *authEnv {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set - skipping API integration tests")
	}
	ctx := context.Background()
	st, err := store.New(ctx, dsn, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	started := time.Now()
	t.Cleanup(func() { _, _ = db.Exec(ctx, `DELETE FROM audit_log WHERE created_at >= $1`, started) })
	gin.SetMode(gin.TestMode)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := NewRouter(config.Config{AdminAPIKey: testAdminKey, Env: "development"}, st, logger, mail.New(config.SMTPConfig{}), time.UTC)
	return &authEnv{router: router, st: st, db: db, stamp: time.Now().UnixNano()}
}

// account makes a member with a role and a real password; removed after.
func (e *authEnv) account(t *testing.T, role model.Role, label, password string) model.TeamMember {
	t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	m, _, err := e.st.UpsertAccount(context.Background(), "Auth "+label, fmt.Sprintf("auth-%s-%d@example.test", label, e.stamp), role, hash)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = e.db.Exec(context.Background(), `DELETE FROM team_members WHERE id = $1`, m.ID) })
	return m
}

// login logs in and returns the session token from the cookie.
func (e *authEnv) login(t *testing.T, email, password string) (string, *httptest.ResponseRecorder) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"email": email, "password": password})
	req := httptest.NewRequest("POST", "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = fmt.Sprintf("10.9.%d.%d:1234", e.stamp%200, len(email)) // its own rate-limit bucket
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == sessionCookie && ck.Value != "" {
			return ck.Value, rec
		}
	}
	return "", rec
}

// as makes a request with a session, decoding "data" into out.
func (e *authEnv) as(t *testing.T, token, method, path string, body any, out any) (int, string) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(csrfHeader, "test")
	if token != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	}
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	if out != nil && rec.Code < 300 {
		var env struct {
			Data json.RawMessage `json:"data"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &env)
		if err := json.Unmarshal(env.Data, out); err != nil {
			t.Fatalf("%s %s: decode %s", method, path, rec.Body.String())
		}
	}
	return rec.Code, rec.Body.String()
}

func (e *authEnv) lead(t *testing.T, label string, owner *int64) model.Lead {
	t.Helper()
	in := model.LeadInput{
		Name: "Auth Lead " + label, Company: fmt.Sprintf("AuthCo-%d %s", e.stamp, label),
		Email: fmt.Sprintf("auth-lead-%s-%d@example.test", label, e.stamp), OwnerID: owner,
	}
	in.Normalize()
	l, err := e.st.CreateLead(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.st.DeleteLead(context.Background(), l.ID) })
	return l
}

const testPassword = "correct horse battery"

func TestLoginLogoutAndSessions(t *testing.T) {
	e := newAuthEnv(t)
	ctx := context.Background()
	admin := e.account(t, model.RoleAdmin, "admin", testPassword)

	// Wrong password, unknown email: the same answer.
	_, wrong := e.login(t, admin.Email, "not the password")
	_, unknown := e.login(t, "nobody-"+admin.Email, testPassword)
	if wrong.Code != 401 || unknown.Code != 401 || wrong.Body.String() != unknown.Body.String() {
		t.Errorf("wrong password %d %s / unknown %d %s", wrong.Code, wrong.Body, unknown.Code, unknown.Body)
	}

	token, rec := e.login(t, strings.ToUpper(admin.Email), testPassword)
	if rec.Code != 200 || token == "" {
		t.Fatalf("login: %d %s", rec.Code, rec.Body)
	}
	cookie := rec.Header().Get("Set-Cookie")
	if !strings.Contains(cookie, "HttpOnly") || !strings.Contains(cookie, "SameSite=Lax") || !strings.Contains(cookie, "Path=/api") {
		t.Errorf("cookie flags: %s", cookie)
	}
	if strings.Contains(rec.Body.String(), "password") || strings.Contains(rec.Body.String(), "$2a$") {
		t.Errorf("login response mentions the password: %s", rec.Body)
	}

	// The database holds the token's hash, never the token.
	var stored int
	_ = e.db.QueryRow(ctx, `SELECT COUNT(*) FROM sessions WHERE token_hash = $1`, []byte(token)).Scan(&stored)
	if stored != 0 {
		t.Error("the raw session token is stored")
	}

	var me model.Viewer
	if code, _ := e.as(t, token, "GET", "/api/auth/me", nil, &me); code != 200 || me.MemberID != admin.ID || me.Role != model.RoleAdmin {
		t.Errorf("me = %d %+v", code, me)
	}

	// Writes need the CSRF header.
	req := httptest.NewRequest("POST", "/api/leads", strings.NewReader(`{}`))
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	r := httptest.NewRecorder()
	e.router.ServeHTTP(r, req)
	if r.Code != http.StatusForbidden {
		t.Errorf("write without %s: %d", csrfHeader, r.Code)
	}

	// The team list never carries a password hash.
	_, team := e.as(t, token, "GET", "/api/team", nil, nil)
	if strings.Contains(team, "$2a$") || strings.Contains(team, "password_hash") || !strings.Contains(team, `"has_login":true`) {
		t.Errorf("team list leaks or misses login info")
	}

	// Change own password: the old one stops working, this session stays.
	other, _ := e.login(t, admin.Email, testPassword)
	if code, body := e.as(t, token, "PUT", "/api/auth/password", map[string]string{"current_password": "nope", "new_password": "a brand new password"}, nil); code != 422 {
		t.Errorf("wrong current password: %d %s", code, body)
	}
	if code, _ := e.as(t, token, "PUT", "/api/auth/password", map[string]string{"current_password": testPassword, "new_password": "short"}, nil); code != 422 {
		t.Errorf("short password: %d", code)
	}
	if code, body := e.as(t, token, "PUT", "/api/auth/password", map[string]string{"current_password": testPassword, "new_password": "a brand new password"}, nil); code != 204 {
		t.Fatalf("change password: %d %s", code, body)
	}
	if code, _ := e.as(t, token, "GET", "/api/auth/me", nil, nil); code != 200 {
		t.Error("changing my password ended my own session")
	}
	if code, _ := e.as(t, other, "GET", "/api/auth/me", nil, nil); code != 401 {
		t.Error("changing my password left my other sessions alive")
	}
	if tok, _ := e.login(t, admin.Email, testPassword); tok != "" {
		t.Error("the old password still works")
	}

	// Logout ends the session.
	if code, _ := e.as(t, token, "POST", "/api/auth/logout", nil, nil); code != 204 {
		t.Errorf("logout: %d", code)
	}
	if code, _ := e.as(t, token, "GET", "/api/auth/me", nil, nil); code != 401 {
		t.Errorf("after logout: %d", code)
	}

	// An expired session is refused.
	tok2, _ := e.login(t, admin.Email, "a brand new password")
	_, _ = e.db.Exec(ctx, `UPDATE sessions SET expires_at = now() - interval '1 second' WHERE token_hash = $1`, auth.HashToken(tok2))
	if code, _ := e.as(t, tok2, "GET", "/api/auth/me", nil, nil); code != 401 {
		t.Errorf("expired session: %d", code)
	}
}

func TestLeadVisibilityByRole(t *testing.T) {
	e := newAuthEnv(t)
	ctx := context.Background()
	admin := e.account(t, model.RoleAdmin, "boss", testPassword)
	manager := e.account(t, model.RoleManager, "mgr", testPassword)
	asha := e.account(t, model.RoleSales, "asha", testPassword)
	ravi := e.account(t, model.RoleSales, "ravi", testPassword)
	adminTok, _ := e.login(t, admin.Email, testPassword)
	mgrTok, _ := e.login(t, manager.Email, testPassword)
	ashaTok, _ := e.login(t, asha.Email, testPassword)
	raviTok, _ := e.login(t, ravi.Email, testPassword)

	l1 := e.lead(t, "asha", &asha.ID)
	l2 := e.lead(t, "ravi", &ravi.ID)
	l3 := e.lead(t, "nobody", nil)
	// A task on each of Asha's and Ravi's leads.
	for _, l := range []model.Lead{l1, l2} {
		if _, err := e.st.CreateTask(ctx, l.ID, model.TaskInput{Title: "Call " + l.Name, DueDate: time.Now().Format("2006-01-02"), Type: model.TaskCall}); err != nil {
			t.Fatal(err)
		}
	}
	search := fmt.Sprintf("/api/leads?search=AuthCo-%d", e.stamp)

	type list struct {
		Data       []model.Lead   `json:"data"`
		Pagination model.PageMeta `json:"pagination"`
	}
	listAs := func(token, query string) list {
		t.Helper()
		var out list
		req := httptest.NewRequest("GET", query, nil)
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
		rec := httptest.NewRecorder()
		e.router.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("list %s: %d %s", query, rec.Code, rec.Body)
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return out
	}

	// --- Lists, search, filters and pagination -----------------------------
	if got := listAs(ashaTok, search); len(got.Data) != 1 || got.Data[0].ID != l1.ID || got.Pagination.TotalItems != 1 {
		t.Errorf("asha's list = %+v", got)
	}
	// Asking for Ravi's leads by filter does not reveal them.
	if got := listAs(ashaTok, search+fmt.Sprintf("&owner=%d", ravi.ID)); len(got.Data) != 0 {
		t.Errorf("asha filtered to ravi's leads saw %d", len(got.Data))
	}
	if got := listAs(ashaTok, search+"&page_size=1&page=2"); len(got.Data) != 0 || got.Pagination.TotalItems != 1 {
		t.Errorf("asha page 2 = %+v", got.Pagination)
	}
	if got := listAs(mgrTok, search); len(got.Data) != 3 {
		t.Errorf("manager sees %d of 3", len(got.Data))
	}
	if got := listAs(adminTok, search); len(got.Data) != 3 {
		t.Errorf("admin sees %d of 3", len(got.Data))
	}

	// --- One lead: someone else's is not found ------------------------------
	for _, r := range []struct{ method, path string }{
		{"GET", ""}, {"PUT", ""}, {"PATCH", "/stage"}, {"PATCH", "/notes"}, {"GET", "/activities"},
		{"POST", "/activities"}, {"GET", "/tasks"}, {"GET", "/next-steps"}, {"POST", "/email/preview"},
		{"PATCH", "/flags"}, {"PATCH", "/qualification"},
	} {
		path := fmt.Sprintf("/api/leads/%d%s", l2.ID, r.path)
		if code, _ := e.as(t, ashaTok, r.method, path, map[string]any{"stage": "contacted"}, nil); code != 404 {
			t.Errorf("asha %s %s: %d, want 404", r.method, path, code)
		}
	}
	if code, _ := e.as(t, ashaTok, "GET", fmt.Sprintf("/api/leads/%d", l1.ID), nil, nil); code != 200 {
		t.Errorf("asha's own lead: %d", code)
	}

	// --- Export ---------------------------------------------------------------
	req := httptest.NewRequest("GET", fmt.Sprintf("/api/leads/export?search=AuthCo-%d", e.stamp), nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: ashaTok})
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), l1.Email) || strings.Contains(rec.Body.String(), l2.Email) || strings.Contains(rec.Body.String(), l3.Email) {
		t.Errorf("asha's export: %d\n%s", rec.Code, rec.Body)
	}

	// --- Dashboard, pipeline, tasks, follow-ups -------------------------------
	var ashaOwned int
	_ = e.db.QueryRow(ctx, `SELECT COUNT(*) FROM leads WHERE owner_id = $1`, asha.ID).Scan(&ashaOwned)
	var dash model.DashboardSummary
	e.as(t, ashaTok, "GET", "/api/dashboard", nil, &dash)
	if dash.TotalLeads != ashaOwned {
		t.Errorf("asha's dashboard counts %d leads, she has %d", dash.TotalLeads, ashaOwned)
	}
	var cols []model.PipelineColumn
	e.as(t, ashaTok, "GET", "/api/pipeline", nil, &cols)
	inPipeline := 0
	for _, c := range cols {
		inPipeline += c.Count
	}
	if inPipeline != ashaOwned {
		t.Errorf("asha's pipeline has %d leads, she has %d", inPipeline, ashaOwned)
	}
	_, tasks := e.as(t, ashaTok, "GET", "/api/tasks", nil, nil)
	if !strings.Contains(tasks, "Call "+l1.Name) || strings.Contains(tasks, "Call "+l2.Name) {
		t.Errorf("asha's tasks include someone else's")
	}
	_, follow := e.as(t, ashaTok, "GET", "/api/follow-ups", nil, nil)
	if strings.Contains(follow, l2.Name) {
		t.Error("asha's follow-ups include ravi's lead")
	}
	for _, path := range []string{"/api/reports", "/api/sales-intelligence", "/api/notifications"} {
		if code, body := e.as(t, ashaTok, "GET", path, nil, nil); code != 200 || strings.Contains(body, l2.Email) || strings.Contains(body, l2.Company) {
			t.Errorf("%s for asha: %d, or it shows ravi's lead", path, code)
		}
	}

	// --- Meta Leads dashboard --------------------------------------------------
	form := fmt.Sprint(e.stamp)
	for _, l := range []model.Lead{l1, l2} {
		if _, err := e.db.Exec(ctx, `INSERT INTO meta_lead_attribution (lead_id, meta_lead_id, form_id) VALUES ($1, $2, $3)`,
			l.ID, fmt.Sprintf("%d%d", e.stamp, l.ID), form); err != nil {
			t.Fatal(err)
		}
	}
	var meta struct {
		Summary model.MetaLeadSummary `json:"summary"`
	}
	e.as(t, ashaTok, "GET", "/api/meta/leads?form_id="+form, nil, &meta)
	if meta.Summary.Total != 1 {
		t.Errorf("asha's Meta leads = %d, want 1", meta.Summary.Total)
	}
	e.as(t, mgrTok, "GET", "/api/meta/leads?form_id="+form, nil, &meta)
	if meta.Summary.Total != 2 {
		t.Errorf("manager's Meta leads = %d, want 2", meta.Summary.Total)
	}

	// --- Creating and editing: the owner comes from the session ---------------
	var created model.Lead
	newLead := map[string]any{"name": "Asha New", "company": fmt.Sprintf("AuthCo-%d new", e.stamp),
		"email": fmt.Sprintf("auth-new-%d@example.test", e.stamp), "source": "website", "stage": "new", "owner_id": ravi.ID}
	if code, body := e.as(t, ashaTok, "POST", "/api/leads", newLead, &created); code != 201 {
		t.Fatalf("asha creates: %d %s", code, body)
	}
	t.Cleanup(func() { _ = e.st.DeleteLead(ctx, created.ID) })
	if created.OwnerID == nil || *created.OwnerID != asha.ID {
		t.Errorf("asha's new lead is owned by %v, want her (%d)", created.OwnerID, asha.ID)
	}
	edit := map[string]any{"name": l1.Name, "company": l1.Company, "email": l1.Email, "source": "website", "stage": "contacted", "owner_id": ravi.ID}
	var edited model.Lead
	if code, _ := e.as(t, ashaTok, "PUT", fmt.Sprintf("/api/leads/%d", l1.ID), edit, &edited); code != 200 || *edited.OwnerID != asha.ID {
		t.Errorf("asha's edit gave the lead away: %v", edited.OwnerID)
	}

	// --- Assigning: managers and admins only -------------------------------------
	owner := func(id int64, to int64) map[string]any { return map[string]any{"owner_id": to} }
	if code, _ := e.as(t, ashaTok, "PATCH", fmt.Sprintf("/api/leads/%d/owner", l1.ID), owner(l1.ID, ravi.ID), nil); code != 403 {
		t.Errorf("asha reassigns: %d, want 403", code)
	}
	if code, _ := e.as(t, ashaTok, "DELETE", fmt.Sprintf("/api/leads/%d", l1.ID), nil, nil); code != 403 {
		t.Errorf("asha deletes: %d, want 403", code)
	}
	if code, _ := e.as(t, ashaTok, "POST", "/api/leads/bulk", map[string]any{"ids": []int64{l1.ID}, "action": "owner", "owner_id": ravi.ID}, nil); code != 403 {
		t.Errorf("asha bulk-reassigns: %d", code)
	}
	var bulk model.BulkResult
	e.as(t, ashaTok, "POST", "/api/leads/bulk", map[string]any{"ids": []int64{l1.ID, l2.ID}, "action": "stage", "stage": "contacted"}, &bulk)
	if bulk.Changed != 1 || len(bulk.Failed) != 1 || bulk.Failed[0].ID != l2.ID {
		t.Errorf("asha's bulk over hers and ravi's = %+v", bulk)
	}

	// The manager gives the unassigned lead to Asha: now she sees it.
	if code, body := e.as(t, mgrTok, "PATCH", fmt.Sprintf("/api/leads/%d/owner", l3.ID), owner(l3.ID, asha.ID), nil); code != 200 {
		t.Fatalf("manager assigns: %d %s", code, body)
	}
	if code, _ := e.as(t, ashaTok, "GET", fmt.Sprintf("/api/leads/%d", l3.ID), nil, nil); code != 200 {
		t.Errorf("asha cannot see the lead assigned to her: %d", code)
	}
	// ...and reassigns it to Ravi: Asha loses it, Ravi gains it.
	e.as(t, mgrTok, "PATCH", fmt.Sprintf("/api/leads/%d/owner", l3.ID), owner(l3.ID, ravi.ID), nil)
	if code, _ := e.as(t, ashaTok, "GET", fmt.Sprintf("/api/leads/%d", l3.ID), nil, nil); code != 404 {
		t.Errorf("asha still sees a lead reassigned away: %d", code)
	}
	if code, _ := e.as(t, raviTok, "GET", fmt.Sprintf("/api/leads/%d", l3.ID), nil, nil); code != 200 {
		t.Errorf("ravi cannot see his reassigned lead: %d", code)
	}

	// --- Admin-only routes ----------------------------------------------------------
	for _, r := range []struct{ method, path string }{
		{"GET", "/api/integrations"}, {"POST", "/api/team"}, {"GET", "/api/automations"},
		{"GET", "/api/integrations/meta-capi"}, {"PUT", fmt.Sprintf("/api/team/%d/password", ravi.ID)},
		{"PUT", fmt.Sprintf("/api/team/%d/account", ravi.ID)}, {"POST", "/api/sales-intelligence/seed"},
	} {
		for name, tok := range map[string]string{"sales": ashaTok, "manager": mgrTok} {
			if code, _ := e.as(t, tok, r.method, r.path, map[string]any{}, nil); code != 403 {
				t.Errorf("%s %s as %s: %d, want 403", r.method, r.path, name, code)
			}
		}
	}

	// --- Admin manages accounts ----------------------------------------------------
	if code, _ := e.as(t, adminTok, "PUT", fmt.Sprintf("/api/team/%d/password", ravi.ID), map[string]any{"password": "ravi's new password"}, nil); code != 200 {
		t.Errorf("admin sets a password: %d", code)
	}
	if code, _ := e.as(t, raviTok, "GET", "/api/auth/me", nil, nil); code != 401 {
		t.Error("a reset password left the old session alive")
	}
	raviTok, _ = e.login(t, ravi.Email, "ravi's new password")
	if raviTok == "" {
		t.Fatal("ravi cannot log in with the password the admin set")
	}
	if code, _ := e.as(t, adminTok, "PUT", fmt.Sprintf("/api/team/%d/account", ravi.ID), map[string]any{"active": false}, nil); code != 200 {
		t.Errorf("admin switches ravi off: %d", code)
	}
	if code, _ := e.as(t, raviTok, "GET", "/api/auth/me", nil, nil); code != 401 {
		t.Error("a switched-off member's session still works")
	}
	if tok, _ := e.login(t, ravi.Email, "ravi's new password"); tok != "" {
		t.Error("a switched-off member can log in")
	}

	// The last active admin cannot be demoted. (Only checked when this test's
	// admin is the only one, as the shared test database may hold others.)
	var otherAdmins int
	_ = e.db.QueryRow(ctx, `SELECT COUNT(*) FROM team_members WHERE role = 'admin' AND active AND password_hash IS NOT NULL AND id <> $1`, admin.ID).Scan(&otherAdmins)
	if otherAdmins == 0 {
		if code, _ := e.as(t, adminTok, "PUT", fmt.Sprintf("/api/team/%d/account", admin.ID), map[string]any{"role": "sales"}, nil); code != 409 {
			t.Errorf("demoting the last admin: %d, want 409", code)
		}
		if code, _ := e.as(t, adminTok, "DELETE", fmt.Sprintf("/api/team/%d", admin.ID), nil, nil); code != 409 {
			t.Errorf("deleting the last admin: %d, want 409", code)
		}
	}
}

func TestLoginIsRateLimited(t *testing.T) {
	e := newAuthEnv(t)
	limited := false
	for range 12 {
		req := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"email":"x@example.test","password":"wrong-wrong"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "10.250.1.1:999"
		rec := httptest.NewRecorder()
		e.router.ServeHTTP(rec, req)
		limited = limited || rec.Code == http.StatusTooManyRequests
	}
	if !limited {
		t.Error("12 logins in a row from one address were never rate limited")
	}
}

func TestSignupLogsInAsSalesWithNoLeads(t *testing.T) {
	e := newAuthEnv(t)
	ctx := context.Background()
	admin := e.account(t, model.RoleAdmin, "existing", testPassword)
	someone := e.account(t, model.RoleSales, "owner", testPassword)
	theirs := e.lead(t, "not-for-newcomers", &someone.ID)
	email := fmt.Sprintf("signup-%d@example.test", e.stamp)
	t.Cleanup(func() { _, _ = e.db.Exec(ctx, `DELETE FROM team_members WHERE email = $1`, email) })

	if code, _ := e.as(t, "", "POST", "/api/auth/signup", map[string]any{"name": "New Person", "email": email, "password": "short"}, nil); code != 422 {
		t.Errorf("short password: %d", code)
	}

	// Signing up logs straight in, as Sales.
	body, _ := json.Marshal(map[string]any{"name": "New Person", "email": email, "password": "a good long password"})
	req := httptest.NewRequest("POST", "/api/auth/signup", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatalf("sign up: %d %s", rec.Code, rec.Body)
	}
	var token string
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == sessionCookie {
			token = ck.Value
		}
	}
	var me model.Viewer
	if code, _ := e.as(t, token, "GET", "/api/auth/me", nil, &me); code != 200 || me.Role != model.RoleSales || me.Email != email {
		t.Fatalf("after sign-up: %d %+v", code, me)
	}
	// ...and can log in again later with the same details.
	if tok, _ := e.login(t, email, "a good long password"); tok == "" {
		t.Error("cannot log in with the sign-up's password")
	}

	// A new account sees no one else's leads.
	if code, _ := e.as(t, token, "GET", fmt.Sprintf("/api/leads/%d", theirs.ID), nil, nil); code != 404 {
		t.Errorf("new account opened someone's lead: %d", code)
	}
	var dash model.DashboardSummary
	e.as(t, token, "GET", "/api/dashboard", nil, &dash)
	if dash.TotalLeads != 0 {
		t.Errorf("new account's dashboard counts %d leads", dash.TotalLeads)
	}
	if code, _ := e.as(t, token, "GET", "/api/integrations", nil, nil); code != 403 {
		t.Errorf("new account reached an admin page: %d", code)
	}

	// An email already in the CRM is refused, and that member is unchanged.
	before, _ := e.st.PasswordHash(ctx, admin.ID)
	code, _ := e.as(t, "", "POST", "/api/auth/signup", map[string]any{"name": "Impostor", "email": admin.Email, "password": "attacker password!!"}, nil)
	after, _ := e.st.PasswordHash(ctx, admin.ID)
	if code != 409 || before != after {
		t.Errorf("sign-up over an existing account: %d, password changed: %v", code, before != after)
	}
	if tok, _ := e.login(t, admin.Email, "attacker password!!"); tok != "" {
		t.Error("an impostor's sign-up let them log in as the admin")
	}
}

func TestForgotAndResetPassword(t *testing.T) {
	e := newAuthEnv(t)
	ctx := context.Background()
	member := e.account(t, model.RoleSales, "forgetful", testPassword)
	oldTok, _ := e.login(t, member.Email, testPassword)

	// The same answer for an account and for nobody; with no email set up,
	// no link is made (an admin resets it instead).
	_, known := e.as(t, "", "POST", "/api/auth/forgot", map[string]any{"email": member.Email}, nil)
	_, unknown := e.as(t, "", "POST", "/api/auth/forgot", map[string]any{"email": "nobody-" + member.Email}, nil)
	if known != unknown {
		t.Errorf("forgot answers differ: %s / %s", known, unknown)
	}
	var links int
	_ = e.db.QueryRow(ctx, `SELECT COUNT(*) FROM password_resets WHERE member_id = $1`, member.ID).Scan(&links)
	if links != 0 {
		t.Error("a reset link was made with no way to email it")
	}

	// A link (as the email would carry) works once.
	token, hash, _ := auth.NewToken()
	if err := e.st.CreatePasswordReset(ctx, member.ID, hash); err != nil {
		t.Fatal(err)
	}
	if code, _ := e.as(t, "", "POST", "/api/auth/reset", map[string]any{"token": token, "password": "short"}, nil); code != 422 {
		t.Errorf("short new password: %d", code)
	}
	if code, body := e.as(t, "", "POST", "/api/auth/reset", map[string]any{"token": token, "password": "my new long password"}, nil); code != 204 {
		t.Fatalf("reset: %d %s", code, body)
	}
	if code, _ := e.as(t, "", "POST", "/api/auth/reset", map[string]any{"token": token, "password": "another new password"}, nil); code != 400 {
		t.Errorf("link used twice: %d", code)
	}
	if code, _ := e.as(t, oldTok, "GET", "/api/auth/me", nil, nil); code != 401 {
		t.Error("a reset left the old session alive")
	}
	if tok, _ := e.login(t, member.Email, "my new long password"); tok == "" {
		t.Error("cannot log in with the new password")
	}

	// An expired link is refused.
	token2, hash2, _ := auth.NewToken()
	_ = e.st.CreatePasswordReset(ctx, member.ID, hash2)
	_, _ = e.db.Exec(ctx, `UPDATE password_resets SET expires_at = now() - interval '1 second' WHERE token_hash = $1`, hash2)
	if code, _ := e.as(t, "", "POST", "/api/auth/reset", map[string]any{"token": token2, "password": "yet another password"}, nil); code != 400 {
		t.Errorf("expired link: %d", code)
	}
}
