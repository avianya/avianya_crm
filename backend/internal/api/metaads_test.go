package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"crm-lead-management/backend/internal/auth"
	"crm-lead-management/backend/internal/config"
	"crm-lead-management/backend/internal/mail"
	"crm-lead-management/backend/internal/metaads"
	"crm-lead-management/backend/internal/model"
	"crm-lead-management/backend/internal/secrets"
	"crm-lead-management/backend/internal/store"
)

// End-to-end tests of the Meta Ads integration: the real router and database,
// and a fake Graph API that checks every token it is sent. Nothing here
// proves the integration works against Meta itself; only a test with a real
// app, Page and dataset can.

const testAdminKey = "test-admin-key-0123456789"

// fakeGraph stands in for graph.facebook.com.
type fakeGraph struct {
	t                                    *testing.T
	mu                                   sync.Mutex
	pageID, formID, datasetID, adAccount string
	appID                                string
	pageToken, capiToken, appSecret      string
	leads                                map[string]map[string]any // by leadgen id
	formPages                            [][]map[string]any        // what /{form}/leads returns, page by page
	leadsMode                            string                    // "" ok, "rate_limit"
	eventsMode                           string                    // "" ok, "rate_limit_once", "auth"
	events                               []map[string]any          // bodies of every POST /events
	leadFetchMode                        string                    // "" ok, "auth"
	datasetMode                          string                    // "" ok, "auth", "permission"
	eventsDelay                          time.Duration             // how long POST /events takes
	leadReads                            int
}

func (f *fakeGraph) graphError(w http.ResponseWriter, status, code int, msg string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": msg, "code": code, "fbtrace_id": "TRACE"}})
}

func (f *fakeGraph) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	path := strings.TrimPrefix(r.URL.Path, "/")

	if r.Method == http.MethodPost && path == f.datasetID+"/events" {
		if f.eventsDelay > 0 {
			f.mu.Unlock()
			time.Sleep(f.eventsDelay)
			f.mu.Lock()
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["access_token"] != f.capiToken {
			f.graphError(w, 400, 190, "Invalid OAuth access token")
			return
		}
		f.events = append(f.events, body)
		switch f.eventsMode {
		case "rate_limit_once":
			f.eventsMode = ""
			w.Header().Set("Retry-After", "120")
			f.graphError(w, 429, 17, "User request limit reached")
		case "auth":
			f.graphError(w, 400, 190, "Error validating access token: The session has been invalidated")
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"events_received": len(body["data"].([]any)), "fbtrace_id": "SENT-TRACE"})
		}
		return
	}

	token := r.URL.Query().Get("access_token")
	switch path {
	case "app":
		if token != f.pageToken {
			f.graphError(w, 400, 190, "Invalid OAuth access token")
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": f.appID, "name": "Test App"})
	case "me":
		if token != f.pageToken {
			f.graphError(w, 400, 190, "Invalid OAuth access token")
			return
		}
		if proof := r.URL.Query().Get("appsecret_proof"); proof != "" {
			mac := hmac.New(sha256.New, []byte(f.appSecret))
			mac.Write([]byte(token))
			if proof != hex.EncodeToString(mac.Sum(nil)) {
				f.graphError(w, 400, 100, "Invalid appsecret_proof provided in the API argument")
				return
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": f.pageID, "name": "Test Page"})
	case f.pageID:
		_ = json.NewEncoder(w).Encode(map[string]any{"id": f.pageID, "name": "Test Page"})
	case f.formID:
		_ = json.NewEncoder(w).Encode(map[string]any{"id": f.formID, "name": "Test form", "status": "ACTIVE"})
	case f.formID + "/leads":
		if token != f.pageToken {
			f.graphError(w, 400, 190, "Invalid OAuth access token")
			return
		}
		if f.leadsMode == "rate_limit" {
			f.graphError(w, 400, 32, "Page request limit reached")
			return
		}
		page := 0
		if after := r.URL.Query().Get("after"); after != "" {
			fmt.Sscanf(after, "p%d", &page)
		}
		body := map[string]any{"data": []any{}}
		if page < len(f.formPages) {
			body["data"] = f.formPages[page]
			if page+1 < len(f.formPages) {
				body["paging"] = map[string]any{"cursors": map[string]any{"after": fmt.Sprintf("p%d", page+1)}, "next": "https://next"}
			}
		}
		_ = json.NewEncoder(w).Encode(body)
	case f.datasetID:
		switch f.datasetMode {
		case "auth":
			f.graphError(w, 400, 190, "Error validating access token: The user has not authorized application")
			return
		case "permission":
			f.graphError(w, 403, 200, "Permissions error: the token cannot access this dataset")
			return
		}
		if token != f.capiToken {
			f.graphError(w, 400, 190, "Invalid OAuth access token")
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": f.datasetID, "name": "Test pixel"})
	case "act_" + f.adAccount:
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "act_" + f.adAccount, "name": "Test ads", "account_status": 1})
	default:
		if token != f.pageToken {
			f.graphError(w, 400, 190, "Invalid OAuth access token")
			return
		}
		if f.leadFetchMode == "auth" {
			f.graphError(w, 400, 190, "Error validating access token: Session has expired")
			return
		}
		lead, ok := f.leads[path]
		if !ok {
			f.graphError(w, 400, 100, "Unsupported get request")
			return
		}
		f.leadReads++
		_ = json.NewEncoder(w).Encode(lead)
	}
}

type metaEnv struct {
	router *gin.Engine
	st     *store.Store
	svc    *metaads.Service
	graph  *fakeGraph
	db     *pgxpool.Pool
	stamp  int64
	// managerToken is a logged-in manager's session: e.do(t, false, ...)
	// makes requests as them, e.do(t, true, ...) with the admin key.
	managerToken string
}

// newMetaEnv wires the router to the test database and a fake Graph API, with
// an admin key and an encryption key, and nothing in .env: every credential
// comes from the settings page. It removes the settings it saved when done,
// so the other webhook tests see a fresh install.
func newMetaEnv(t *testing.T) *metaEnv {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set - skipping API integration tests")
	}
	ctx := context.Background()
	st, err := store.New(ctx, dsn, "")
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(st.Close)
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)

	stamp := time.Now().UnixNano() % 1_000_000_000_000
	g := &fakeGraph{
		t:         t,
		pageID:    fmt.Sprint(100000 + stamp%1000),
		formID:    fmt.Sprint(stamp),
		datasetID: fmt.Sprint(stamp + 1),
		adAccount: fmt.Sprint(stamp + 2),
		appID:     fmt.Sprint(stamp + 3),
		pageToken: "PAGE-TOKEN-" + fmt.Sprint(stamp), capiToken: "CAPI-TOKEN-" + fmt.Sprint(stamp),
		appSecret: "app-secret-" + fmt.Sprint(stamp),
		leads:     map[string]map[string]any{},
	}
	srv := httptest.NewServer(g)
	t.Cleanup(srv.Close)

	key := bytes.Repeat([]byte{42}, secrets.KeySize)
	box, _ := secrets.New(key)
	st.SetSecretsBox(box)

	gin.SetMode(gin.TestMode)
	cfg := config.Config{AdminAPIKey: testAdminKey, SecretsKey: key, Meta: config.MetaConfig{GraphURL: srv.URL}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := NewRouter(cfg, st, logger, mail.New(config.SMTPConfig{}), time.UTC)

	started := time.Now()
	t.Cleanup(func() {
		_, _ = db.Exec(ctx, `DELETE FROM audit_log WHERE created_at >= $1`, started)
		_, _ = db.Exec(ctx, `DELETE FROM integration_settings WHERE provider = 'meta'`)
		_, _ = db.Exec(ctx, `DELETE FROM app_settings WHERE key = 'lead_quality_rules'`)
		_, _ = db.Exec(ctx, `DELETE FROM leads WHERE id IN (SELECT lead_id FROM meta_lead_attribution WHERE form_id = $1)`, g.formID)
		_, _ = db.Exec(ctx, `DELETE FROM ad_leads WHERE form_id = $1`, g.formID)
	})
	env := &metaEnv{router: router, st: st, svc: metaads.New(st, cfg, logger), graph: g, db: db, stamp: stamp}
	env.managerToken = loginAs(t, st, db, model.RoleManager, fmt.Sprintf("manager-%d@example.test", stamp))
	return env
}

// loginAs makes a member with a role and a live session, and returns the
// session's token for the cookie. The member is removed after the test.
func loginAs(t *testing.T, st *store.Store, db *pgxpool.Pool, role model.Role, email string) string {
	t.Helper()
	ctx := context.Background()
	m, _, err := st.UpsertAccount(ctx, "Test "+string(role), email, role, "$2a$04$not.a.real.hash.but.set.so.the.account.counts..")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec(ctx, `DELETE FROM team_members WHERE id = $1`, m.ID) })
	token, hash, err := auth.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(ctx, m.ID, hash, "test", "test"); err != nil {
		t.Fatal(err)
	}
	return token
}

// anon makes a request with no credentials at all.
func (e *metaEnv) anon(t *testing.T, method, path string, body any) (int, string) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// do makes a request, with the admin key when admin is true, and decodes
// "data" into out.
func (e *metaEnv) do(t *testing.T, admin bool, method, path string, body any, out any) (int, string) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if admin {
		req.Header.Set("X-Admin-Key", testAdminKey)
	} else {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: e.managerToken})
		req.Header.Set(csrfHeader, "test")
	}
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	if out != nil && rec.Code < 300 {
		var env struct {
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil || json.Unmarshal(env.Data, out) != nil {
			t.Fatalf("%s %s: decode %s", method, path, rec.Body.String())
		}
	}
	return rec.Code, rec.Body.String()
}

// settings is a complete, valid settings body.
func (e *metaEnv) settings(capi, testMode bool) map[string]any {
	g := e.graph
	return map[string]any{
		"config": map[string]any{
			"dataset_id": g.datasetID, "app_id": g.appID, "ad_account_id": "act_" + g.adAccount, "page_id": g.pageID,
			"form_ids": []string{g.formID}, "capi_enabled": capi, "test_mode": testMode,
			"test_event_code": "TEST123", "crm_name": "Test CRM", "phone_country_code": "91",
			"event_mapping": []map[string]any{
				{"stage": "qualified", "event_name": "Qualified Lead"},
				{"stage": "won", "event_name": "Converted"},
			},
		},
		"secrets": map[string]any{
			"capi_token": g.capiToken, "page_access_token": g.pageToken,
			"app_secret": g.appSecret, "verify_token": "verify-" + fmt.Sprint(e.stamp),
		},
	}
}

// signedWebhook posts a leadgen notification signed with the app secret.
func (e *metaEnv) signedWebhook(leadgenID string) *httptest.ResponseRecorder {
	g := e.graph
	payload := fmt.Sprintf(`{"object":"page","entry":[{"id":%q,"changes":[{"field":"leadgen","value":{"leadgen_id":%q,"form_id":%q,"ad_id":"777","page_id":%q,"created_time":1760950502}}]}]}`,
		g.pageID, leadgenID, g.formID, g.pageID)
	mac := hmac.New(sha256.New, []byte(g.appSecret))
	mac.Write([]byte(payload))
	return send(e.router, http.MethodPost, "/api/webhooks/meta", payload,
		map[string]string{"X-Hub-Signature-256": "sha256=" + hex.EncodeToString(mac.Sum(nil))})
}

// addLead puts a lead on the fake Graph API.
func (e *metaEnv) addLead(id, name, email, phone string) {
	e.graph.mu.Lock()
	defer e.graph.mu.Unlock()
	e.graph.leads[id] = map[string]any{
		"id": id, "created_time": "2026-10-20T09:15:02+0000", "form_id": e.graph.formID,
		"ad_id": "777", "ad_name": "Diwali ad", "adset_id": "888", "adset_name": "Pune 25-45",
		"campaign_id": "999", "campaign_name": "Diwali offer",
		"field_data": []map[string]any{
			{"name": "full_name", "values": []string{name}},
			{"name": "email", "values": []string{email}},
			{"name": "phone_number", "values": []string{phone}},
			{"name": "city", "values": []string{"Pune"}},
		},
	}
}

func TestMetaSettingsNeedAdminAndNeverReturnSecrets(t *testing.T) {
	e := newMetaEnv(t)
	ctx := context.Background()

	if code, _ := e.anon(t, "GET", "/api/meta/settings", nil); code != http.StatusUnauthorized {
		t.Errorf("no key: %d, want 401", code)
	}
	req := httptest.NewRequest("GET", "/api/meta/settings", nil)
	req.Header.Set("X-Admin-Key", "wrong-key-wrong-key")
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("wrong key: %d, want 403", rec.Code)
	}

	var view model.MetaSettingsView
	code, body := e.do(t, true, "PUT", "/api/meta/settings", e.settings(false, true), &view)
	if code != http.StatusOK {
		t.Fatalf("save: %d %s", code, body)
	}
	for _, secret := range []string{e.graph.pageToken, e.graph.capiToken, e.graph.appSecret} {
		if strings.Contains(body, secret) {
			t.Fatalf("a secret came back in the response: %s", body)
		}
	}
	if s := view.Secrets["capi_token"]; !s.Set || s.Source != "saved" || !strings.HasSuffix(s.Hint, e.graph.capiToken[len(e.graph.capiToken)-4:]) {
		t.Errorf("capi_token state = %+v", s)
	}
	if view.Config.AdAccountID != e.graph.adAccount || !view.LeadsReady {
		t.Errorf("view = %+v", view)
	}

	// At rest: sealed, not plain.
	var sealed []byte
	if err := e.db.QueryRow(ctx, `SELECT secrets FROM integration_settings WHERE provider = 'meta'`).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte(e.graph.pageToken)) || bytes.Contains(sealed, []byte("PAGE-TOKEN")) {
		t.Fatal("the page token is stored in the clear")
	}

	// Saving again with blank secrets keeps them.
	again := e.settings(false, true)
	again["secrets"] = map[string]any{}
	again["config"].(map[string]any)["crm_name"] = "Renamed CRM"
	if code, body := e.do(t, true, "PUT", "/api/meta/settings", again, &view); code != http.StatusOK || !view.Secrets["page_access_token"].Set || view.Config.CRMName != "Renamed CRM" {
		t.Fatalf("save without secrets: %d %s", code, body)
	}

	// Live mode before a passed test is refused.
	if code, body := e.do(t, true, "PUT", "/api/meta/settings", e.settings(true, false), nil); code != http.StatusConflict {
		t.Errorf("live before test: %d %s, want 409", code, body)
	}

	// Test Connection makes real calls (to the fake) and records the status.
	var result model.ConnectionResult
	if code, body := e.do(t, true, "POST", "/api/meta/settings/test", nil, &result); code != http.StatusOK || !result.OK {
		t.Fatalf("test connection: %d %s", code, body)
	}
	for _, c := range result.Checks {
		if !c.OK {
			t.Errorf("check %s failed: %s", c.Key, c.Detail)
		}
	}
	if len(e.graph.events) != 0 {
		t.Errorf("Test Connection must send no event (Send Test Event does): %v", e.graph.events)
	}
	e.do(t, true, "GET", "/api/meta/settings", nil, &view)
	if view.Status != model.MetaConnected || view.LastTestedAt == nil {
		t.Errorf("after test: status %s", view.Status)
	}
	if code, body := e.do(t, true, "PUT", "/api/meta/settings", e.settings(true, false), nil); code != http.StatusOK {
		t.Errorf("live after a passed test: %d %s", code, body)
	}

	// An App ID the token was not issued by is caught by the test.
	wrongApp := e.settings(true, true)
	wrongApp["config"].(map[string]any)["app_id"] = "55555555"
	wrongApp["secrets"] = map[string]any{}
	e.do(t, true, "PUT", "/api/meta/settings", wrongApp, nil)
	e.do(t, true, "POST", "/api/meta/settings/test", nil, &result)
	if result.OK {
		t.Error("a wrong App ID passed the test")
	}
	for _, c := range result.Checks {
		if c.Key == "app" && (c.OK || !strings.Contains(c.Detail, e.graph.appID)) {
			t.Errorf("app check = %+v", c)
		}
	}

	// A wrong app secret is caught by the test.
	bad := e.settings(true, true)
	bad["secrets"] = map[string]any{"app_secret": "not-the-app-secret"}
	e.do(t, true, "PUT", "/api/meta/settings", bad, nil)
	e.do(t, true, "POST", "/api/meta/settings/test", nil, &result)
	if result.OK {
		t.Error("a wrong app secret passed the test")
	}
	e.do(t, true, "GET", "/api/meta/settings", nil, &view)
	if view.Status != model.MetaError || !strings.Contains(view.LastError, "App secret") {
		t.Errorf("after failed test: %s %q", view.Status, view.LastError)
	}

	// Disconnect forgets the credentials and stops conversion events.
	if code, _ := e.do(t, true, "DELETE", "/api/meta/settings/credentials", nil, &view); code != http.StatusOK {
		t.Fatalf("disconnect: %d", code)
	}
	if view.Secrets["page_access_token"].Set || view.Config.CAPIEnabled || view.Status != model.MetaNotConnected {
		t.Errorf("after disconnect: %+v", view)
	}
}

func TestWrongAdminKeyAndNoLoginAreRefused(t *testing.T) {
	router, _ := newTestAPI(t)
	req := httptest.NewRequest("GET", "/api/meta/settings", nil)
	req.Header.Set("X-Admin-Key", "anything-at-all-here")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("wrong admin key: %d, want 403", rec.Code)
	}
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("GET", "/api/leads", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no login: %d, want 401", rec.Code)
	}
}

func TestSavingCredentialsNeedsEncryptionKey(t *testing.T) {
	e := newMetaEnv(t)
	e.st.SetSecretsBox(nil) // as if SECRETS_ENCRYPTION_KEY were unset
	code, body := e.do(t, true, "PUT", "/api/meta/settings", e.settings(false, true), nil)
	if code != http.StatusServiceUnavailable || !strings.Contains(body, "SECRETS_ENCRYPTION_KEY") {
		t.Errorf("save without a key: %d %s", code, body)
	}
}

func TestMetaLeadImportQualityAndConversionFeedback(t *testing.T) {
	e := newMetaEnv(t)
	ctx := context.Background()
	if code, body := e.do(t, true, "PUT", "/api/meta/settings", e.settings(true, true), nil); code != http.StatusOK {
		t.Fatalf("save: %d %s", code, body)
	}

	// --- Import by webhook --------------------------------------------------
	leadgen := fmt.Sprint(e.stamp + 10)
	email := fmt.Sprintf("meera-%d@acme.example", e.stamp)
	e.addLead(leadgen, "Meera Shah", email, "+91 98765 43210")

	// Meta resends; the same lead can also arrive five times at once.
	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if rec := e.signedWebhook(leadgen); rec.Code != http.StatusOK {
				t.Errorf("webhook: %d %s", rec.Code, rec.Body.String())
			}
		}()
	}
	wg.Wait()
	var leads, records int
	_ = e.db.QueryRow(ctx, `SELECT COUNT(*) FROM leads WHERE email = $1`, email).Scan(&leads)
	_ = e.db.QueryRow(ctx, `SELECT COUNT(*) FROM ad_leads WHERE provider = 'meta' AND external_id = $1`, leadgen).Scan(&records)
	if leads != 1 || records != 1 {
		t.Fatalf("five deliveries made %d leads and %d records, want 1 and 1", leads, records)
	}

	record := adLeadFor(t, e.st, model.ProviderMeta, leadgen)
	if record.Status != model.AdLeadCreated || record.LeadID == nil {
		t.Fatalf("record = %+v", record)
	}
	lead, err := e.st.GetLead(ctx, *record.LeadID)
	if err != nil {
		t.Fatal(err)
	}
	a := lead.MetaAttribution
	if a == nil || a.MetaLeadID != leadgen || a.CampaignID != "999" || a.AdsetName != "Pune 25-45" ||
		a.PageID != e.graph.pageID || a.AdAccountID != e.graph.adAccount || a.MetaCreatedAt == nil ||
		a.MetaCreatedAt.UTC().Format("15:04") != "09:15" {
		t.Fatalf("attribution = %+v", a)
	}
	// Default rules: a valid email and phone are 40 decided points -> Average;
	// service, budget, follow-ups and meetings are not known yet, not failed.
	if lead.QualityScore == nil || *lead.QualityScore != 40 || lead.Quality != model.QualityAverage || len(lead.QualityBreakdown) != 6 ||
		!strings.Contains(lead.QualityReason, "not known yet") {
		t.Fatalf("quality = %v %s %v", lead.QualityScore, lead.Quality, lead.QualityBreakdown)
	}

	// --- A webhook for another Page is ignored ------------------------------
	other := fmt.Sprint(e.stamp + 11)
	e.addLead(other, "Other Page", "other-"+email, "+91 90000 00000")
	payload := fmt.Sprintf(`{"object":"page","entry":[{"id":"424242","changes":[{"field":"leadgen","value":{"leadgen_id":%q,"form_id":%q,"page_id":"424242"}}]}]}`, other, e.graph.formID)
	mac := hmac.New(sha256.New, []byte(e.graph.appSecret))
	mac.Write([]byte(payload))
	send(e.router, "POST", "/api/webhooks/meta", payload, map[string]string{"X-Hub-Signature-256": "sha256=" + hex.EncodeToString(mac.Sum(nil))})
	if ok, _ := e.st.AdLeadReceived(ctx, model.ProviderMeta, other); ok {
		t.Error("a lead from another Page was taken in")
	}

	// --- Stage changes queue conversion events, once ------------------------
	stage := func(s string, extra map[string]any) {
		t.Helper()
		body := map[string]any{"stage": s}
		for k, v := range extra {
			body[k] = v
		}
		if code, raw := e.do(t, false, "PATCH", fmt.Sprintf("/api/leads/%d/stage", lead.ID), body, nil); code != http.StatusOK {
			t.Fatalf("stage %s: %d %s", s, code, raw)
		}
	}
	events := func() []model.CAPIEvent {
		t.Helper()
		got, err := e.st.ListCAPIEvents(ctx, model.CAPIEventQuery{LeadID: lead.ID})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	stage("contacted", nil) // not mapped: nothing
	if n := len(events()); n != 0 {
		t.Fatalf("contacted queued %d events", n)
	}
	stage("qualified", nil)
	stage("contacted", nil)
	stage("qualified", nil) // again: still one event
	got := events()
	if len(got) != 1 || got[0].EventName != "Qualified Lead" || got[0].Status != model.CAPIPending || !got[0].TestMode {
		t.Fatalf("events after qualified twice = %+v", got)
	}

	// --- Sending: a rate limit, then success --------------------------------
	e.graph.events = nil
	e.graph.eventsMode = "rate_limit_once"
	if _, err := e.svc.SendDue(ctx); err != nil {
		t.Fatal(err)
	}
	got = events()
	if got[0].Status != model.CAPIRetryScheduled || got[0].Attempts != 1 || got[0].NextAttemptAt == nil ||
		time.Until(*got[0].NextAttemptAt) < 100*time.Second || !strings.Contains(got[0].LastError, "rate limit") {
		t.Fatalf("after a rate limit = %+v", got[0])
	}
	// Not due yet: nothing is sent.
	if n, _ := e.svc.SendDue(ctx); n != 0 {
		t.Fatalf("sent %d events before they were due", n)
	}
	// Retry now (admin), and it goes.
	if code, _ := e.anon(t, "POST", fmt.Sprintf("/api/meta/capi/events/%d/retry", got[0].ID), nil); code != http.StatusUnauthorized {
		t.Errorf("retry without the admin key: %d", code)
	}
	if code, body := e.do(t, true, "POST", fmt.Sprintf("/api/meta/capi/events/%d/retry", got[0].ID), nil, nil); code != http.StatusOK {
		t.Fatalf("retry: %d %s", code, body)
	}
	if n, err := e.svc.SendDue(ctx); err != nil || n != 1 {
		t.Fatalf("send: %d %v", n, err)
	}
	got = events()
	if got[0].Status != model.CAPISent || got[0].FBTraceID != "SENT-TRACE" || got[0].SentAt == nil {
		t.Fatalf("after send = %+v", got[0])
	}
	if code, _ := e.do(t, true, "POST", fmt.Sprintf("/api/meta/capi/events/%d/retry", got[0].ID), nil, nil); code != http.StatusConflict {
		t.Errorf("retrying a sent event: %d, want 409", code)
	}

	// What Meta was sent: the CRM integration shape, hashed customer data.
	sentBody := e.graph.events[len(e.graph.events)-1]
	if sentBody["test_event_code"] != "TEST123" {
		t.Errorf("test mode event without its code: %v", sentBody)
	}
	ev := sentBody["data"].([]any)[0].(map[string]any)
	user := ev["user_data"].(map[string]any)
	raw, _ := json.Marshal(sentBody)
	if ev["event_name"] != "Qualified Lead" || ev["action_source"] != "system_generated" ||
		ev["event_id"] != model.CAPIEventID(lead.ID, "Qualified Lead", true) ||
		fmt.Sprintf("%.0f", user["lead_id"]) != leadgen ||
		strings.Contains(string(raw), email) || strings.Contains(string(raw), "98765") {
		t.Errorf("event sent = %s", raw)
	}

	// --- A revoked token fails the event and shows on the settings page ------
	e.graph.eventsMode = "auth"
	stage("won", map[string]any{"close_reason": "price"})
	if _, err := e.svc.SendDue(ctx); err != nil {
		t.Fatal(err)
	}
	var won model.CAPIEvent
	for _, ev := range events() {
		if ev.EventName == "Converted" {
			won = ev
		}
	}
	if won.Status != model.CAPIFailed || !strings.Contains(won.LastError, "access token") || strings.Contains(won.LastError, e.graph.capiToken) {
		t.Fatalf("after auth error = %+v", won)
	}
	var view model.MetaSettingsView
	e.do(t, true, "GET", "/api/meta/settings", nil, &view)
	if view.Status != model.MetaError || !strings.Contains(view.LastError, "access token") {
		t.Errorf("settings after auth error: %s %q", view.Status, view.LastError)
	}
	// Token fixed: retry all failed.
	e.graph.eventsMode = ""
	var requeued struct {
		Requeued int `json:"requeued"`
	}
	if code, _ := e.do(t, true, "POST", "/api/meta/capi/events/retry-failed", nil, &requeued); code != http.StatusOK || requeued.Requeued < 1 {
		t.Fatalf("retry failed: %d %+v", code, requeued)
	}
	if n, _ := e.svc.SendDue(ctx); n < 1 {
		t.Error("requeued event not sent")
	}

	// --- A lead not from Meta never queues an event --------------------------
	plain := model.LeadInput{Name: "Plain Lead", Company: "Plain Co", Email: fmt.Sprintf("plain-%d@example.test", e.stamp)}
	plain.Normalize()
	p, err := e.st.CreateLead(ctx, plain)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.st.DeleteLead(ctx, p.ID) })
	if _, err := e.st.UpdateLeadStage(ctx, p.ID, model.StageInput{Stage: model.StageQualified}); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.st.ListCAPIEvents(ctx, model.CAPIEventQuery{LeadID: p.ID}); len(got) != 0 {
		t.Errorf("a non-Meta lead queued %d events", len(got))
	}

	// --- Quality override (admin) and qualification reason ------------------
	path := fmt.Sprintf("/api/leads/%d/quality-override", lead.ID)
	if code, _ := e.anon(t, "PUT", path, map[string]any{"score": 10, "reason": "Spam"}); code != http.StatusUnauthorized {
		t.Errorf("override without admin: %d", code)
	}
	if code, _ := e.do(t, true, "PUT", path, map[string]any{"score": 10}, nil); code != http.StatusUnprocessableEntity {
		t.Errorf("override without a reason: %d", code)
	}
	var overridden model.Lead
	if code, body := e.do(t, true, "PUT", path, map[string]any{"score": 10, "reason": "Fake number, never answers"}, &overridden); code != http.StatusOK {
		t.Fatalf("override: %d %s", code, body)
	}
	if overridden.Quality != model.QualityPoor || *overridden.QualityOverride != 10 || *overridden.QualityScore != 40 {
		t.Errorf("after override: %s %v %v", overridden.Quality, overridden.QualityOverride, overridden.QualityScore)
	}
	logged := false
	for _, act := range overridden.Activities {
		logged = logged || (act.Type == model.ActivityQualityChange && strings.Contains(act.Note, "Fake number"))
	}
	if !logged {
		t.Error("override not on the timeline")
	}
	var cleared model.Lead
	e.do(t, true, "DELETE", path, nil, &cleared)
	if cleared.Quality != model.QualityAverage || cleared.QualityOverride != nil {
		t.Errorf("after clearing: %s %v", cleared.Quality, cleared.QualityOverride)
	}
	var qualified model.Lead
	if code, _ := e.do(t, false, "PATCH", fmt.Sprintf("/api/leads/%d/qualification", lead.ID), map[string]any{"reason": "Budget confirmed on call"}, &qualified); code != http.StatusOK || qualified.QualificationReason != "Budget confirmed on call" {
		t.Errorf("qualification reason = %q", qualified.QualificationReason)
	}

	// --- The dashboard -------------------------------------------------------
	type page struct {
		Data       model.MetaLeadsReport `json:"data"`
		Pagination model.PageMeta        `json:"pagination"`
	}
	dash := func(query string) page {
		t.Helper()
		rec := send(e.router, "GET", "/api/meta/leads?form_id="+e.graph.formID+query, "", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("dashboard %s: %d %s", query, rec.Code, rec.Body.String())
		}
		var p page
		_ = json.Unmarshal(rec.Body.Bytes(), &p)
		return p
	}
	all := dash("")
	if all.Data.Summary.Total != 1 || all.Data.Summary.Converted != 1 || all.Data.Summary.Qualified != 1 ||
		all.Data.Summary.ConversionRate == nil || *all.Data.Summary.ConversionRate != 100 || len(all.Data.Leads) != 1 {
		t.Fatalf("dashboard = %+v", all.Data.Summary)
	}
	row := all.Data.Leads[0]
	if row.Attribution.CampaignName != "Diwali offer" || *row.EffectiveScore != 40 || row.CAPIStatus != model.CAPISent {
		t.Errorf("row = %+v", row)
	}
	for query, want := range map[string]int{
		"&quality=average": 1, "&quality=good": 0,
		"&score_min=30&score_max=50": 1, "&score_min=50": 0,
		"&stage=won": 1, "&stage=new": 0,
		"&campaign_id=999": 1, "&campaign_id=1": 0,
		"&capi_status=sent": 1, "&capi_status=failed": 0, "&capi_status=none": 0,
		"&from=2026-10-20&to=2026-10-20": 1, "&from=2026-10-21": 0,
		"&owner=none": 1, "&source=meta_ads": 1, "&source=website": 0,
		"&qualification=qualified": 1, "&qualification=pending": 0,
	} {
		if got := dash(query).Data.Summary.Total; got != want {
			t.Errorf("dashboard%s: %d leads, want %d", query, got, want)
		}
	}
	if rec := send(e.router, "GET", "/api/meta/leads?score_min=101", "", nil); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("bad score filter: %d", rec.Code)
	}
	found := false
	for _, o := range all.Data.Options.Campaigns {
		found = found || (o.Value == "999" && o.Label == "Diwali offer")
	}
	if !found {
		t.Errorf("campaign options = %+v", all.Data.Options.Campaigns)
	}

	// The delivery log is for admins, and has no secret or customer data in it.
	path = fmt.Sprintf("/api/meta/capi/events?lead_id=%d", lead.ID)
	if code, _ := e.anon(t, "GET", path, nil); code != http.StatusUnauthorized {
		t.Errorf("delivery log without the admin key: %d", code)
	}
	code, logBody := e.do(t, true, "GET", path, nil, nil)
	if code != http.StatusOK || strings.Contains(logBody, e.graph.capiToken) || strings.Contains(logBody, email) ||
		strings.Contains(logBody, "98765") {
		t.Errorf("delivery log: %d %s", code, logBody)
	}
}

func TestMetaLeadFetchFailuresAndSync(t *testing.T) {
	e := newMetaEnv(t)
	ctx := context.Background()
	e.do(t, true, "PUT", "/api/meta/settings", e.settings(false, true), nil)

	// A revoked Page token: the lead is recorded as failed, with attribution,
	// and the settings page shows the problem.
	leadgen := fmt.Sprint(e.stamp + 20)
	email := fmt.Sprintf("sync-%d@acme.example", e.stamp)
	e.addLead(leadgen, "Ravi Kumar", email, "+91 91111 11111")
	e.graph.leadFetchMode = "auth"
	if rec := e.signedWebhook(leadgen); rec.Code != http.StatusOK {
		t.Fatalf("webhook: %d", rec.Code)
	}
	failed := adLeadFor(t, e.st, model.ProviderMeta, leadgen)
	if failed.Status != model.AdLeadFailed || !strings.Contains(failed.Error, "access token") ||
		failed.Attribution == nil || failed.Attribution.PageID != e.graph.pageID {
		t.Fatalf("failed record = %+v", failed)
	}
	var view model.MetaSettingsView
	e.do(t, true, "GET", "/api/meta/settings", nil, &view)
	if view.Status != model.MetaError {
		t.Errorf("status after a revoked token = %s", view.Status)
	}

	// Sync needs the admin key.
	if code, _ := e.anon(t, "POST", "/api/meta/sync", nil); code != http.StatusUnauthorized {
		t.Errorf("sync without admin: %d", code)
	}

	// Rate limited: nothing taken, the error recorded, last sync not moved.
	e.graph.leadFetchMode = ""
	e.graph.leadsMode = "rate_limit"
	var result model.SyncResult
	if code, body := e.do(t, true, "POST", "/api/meta/sync", nil, &result); code != http.StatusOK || !result.Partial || len(result.Errors) == 0 {
		t.Fatalf("rate-limited sync: %d %s", code, body)
	}
	e.do(t, true, "GET", "/api/meta/settings", nil, &view)
	if view.LastSyncAt != nil || !strings.Contains(view.LastError, "rate limit") {
		t.Errorf("after rate-limited sync: %v %q", view.LastSyncAt, view.LastError)
	}

	// Two pages of leads: the failed one is taken in now, one is new, and one
	// was received before and is skipped.
	e.graph.leadsMode = ""
	newID := fmt.Sprint(e.stamp + 21)
	e.addLead(newID, "Asha Rao", fmt.Sprintf("asha-%d@gmail.com", e.stamp), "")
	e.graph.mu.Lock()
	e.graph.formPages = [][]map[string]any{{e.graph.leads[leadgen]}, {e.graph.leads[newID]}}
	e.graph.mu.Unlock()
	if code, body := e.do(t, true, "POST", "/api/meta/sync", nil, &result); code != http.StatusOK {
		t.Fatalf("sync: %d %s", code, body)
	}
	if result.Fetched != 2 || result.Created != 2 || result.Partial || len(result.Errors) != 0 {
		t.Fatalf("sync result = %+v", result)
	}
	if code, _ := e.do(t, true, "POST", "/api/meta/sync", nil, &result); code != http.StatusOK || result.Skipped != 2 || result.Created != 0 {
		t.Fatalf("second sync = %+v", result)
	}
	e.do(t, true, "GET", "/api/meta/settings", nil, &view)
	if view.LastSyncAt == nil || view.LastSyncCount == nil {
		t.Errorf("last sync not recorded: %+v", view)
	}

	// Only an email known: too little to judge -- Not Evaluated, not Poor.
	asha := adLeadFor(t, e.st, model.ProviderMeta, newID)
	lead, _ := e.st.GetLead(ctx, *asha.LeadID)
	if lead.QualityScore != nil || lead.Quality != model.QualityNotEvaluated {
		t.Errorf("asha quality = %v %s", lead.QualityScore, lead.Quality)
	}

	// New rules rescore every imported lead.
	rules := model.QualityRules{GoodFrom: 60, AverageFrom: 30, Rules: []model.QualityRule{
		{ID: "pune", Label: "In Pune", Field: model.FieldAnswer, AnswerKey: "city", Op: model.OpEquals, Value: "pune", Points: 60},
	}}
	if code, _ := e.anon(t, "PUT", "/api/meta/quality-rules", rules); code != http.StatusUnauthorized {
		t.Errorf("rules without admin: %d", code)
	}
	bad := rules
	bad.GoodFrom = 10
	if code, _ := e.do(t, true, "PUT", "/api/meta/quality-rules", bad, nil); code != http.StatusUnprocessableEntity {
		t.Errorf("bad rules: %d", code)
	}
	if code, body := e.do(t, true, "PUT", "/api/meta/quality-rules", rules, nil); code != http.StatusOK {
		t.Fatalf("save rules: %d %s", code, body)
	}
	lead, _ = e.st.GetLead(ctx, *asha.LeadID)
	if *lead.QualityScore != 60 || lead.Quality != model.QualityGood || lead.QualityBreakdown[0].RuleID != "pune" {
		t.Errorf("after new rules: %v %s %v", *lead.QualityScore, lead.Quality, lead.QualityBreakdown)
	}
}

func TestRequestLogRedactsSecrets(t *testing.T) {
	for raw, leaked := range map[string]string{
		"hub.mode=subscribe&hub.verify_token=SECRET-1&hub.challenge=42": "SECRET-1",
		"access_token=SECRET-2&fields=id":                               "SECRET-2",
	} {
		got := redactQuery(raw)
		if strings.Contains(got, leaked) || !strings.Contains(got, "redacted") {
			t.Errorf("redactQuery(%q) = %q", raw, got)
		}
	}
	if got := redactQuery("page=2&stage=new"); got != "page=2&stage=new" {
		t.Errorf("harmless query changed: %q", got)
	}
}

// Settings saved before the App ID field existed: the Ad Account ID and the
// credentials survive, and adding an App ID changes nothing else.
func TestSettingsFromBeforeAppIDKeepAdAccountAndCredentials(t *testing.T) {
	e := newMetaEnv(t)
	ctx := context.Background()
	if code, body := e.do(t, true, "PUT", "/api/meta/settings", e.settings(false, true), nil); code != http.StatusOK {
		t.Fatalf("save: %d %s", code, body)
	}
	// Make the stored row look like one written before app_id existed.
	if _, err := e.db.Exec(ctx, `UPDATE integration_settings SET config = config - 'app_id' WHERE provider = 'meta'`); err != nil {
		t.Fatal(err)
	}
	var sealedBefore []byte
	_ = e.db.QueryRow(ctx, `SELECT secrets FROM integration_settings WHERE provider = 'meta'`).Scan(&sealedBefore)

	var view model.MetaSettingsView
	e.do(t, true, "GET", "/api/meta/settings", nil, &view)
	if view.Config.AppID != "" || view.Config.AdAccountID != e.graph.adAccount || !view.Secrets["page_access_token"].Set {
		t.Fatalf("old row read as %+v", view.Config)
	}

	// Add the App ID, sending the config back as the page would (no secrets).
	update := e.settings(false, true)
	update["secrets"] = map[string]any{}
	if code, body := e.do(t, true, "PUT", "/api/meta/settings", update, &view); code != http.StatusOK {
		t.Fatalf("add app id: %d %s", code, body)
	}
	if view.Config.AppID != e.graph.appID || view.Config.AdAccountID != e.graph.adAccount {
		t.Errorf("after adding the App ID: %+v", view.Config)
	}
	var sealedAfter []byte
	_ = e.db.QueryRow(ctx, `SELECT secrets FROM integration_settings WHERE provider = 'meta'`).Scan(&sealedAfter)
	if !bytes.Equal(sealedBefore, sealedAfter) {
		t.Error("saving the App ID rewrote the saved credentials")
	}

	// The Ad Account ID is optional: clearing it is allowed, and leads still import.
	update["config"].(map[string]any)["ad_account_id"] = ""
	if code, body := e.do(t, true, "PUT", "/api/meta/settings", update, &view); code != http.StatusOK || view.Config.AdAccountID != "" {
		t.Errorf("clear ad account: %d %s", code, body)
	}
	var result model.ConnectionResult
	e.do(t, true, "POST", "/api/meta/settings/test", nil, &result)
	for _, c := range result.Checks {
		if c.Key == "ad_account" && !c.Skipped {
			t.Errorf("ad account check ran with no Ad Account ID: %+v", c)
		}
		if c.Key == "app" && !c.OK {
			t.Errorf("app check failed: %+v", c)
		}
	}
}
