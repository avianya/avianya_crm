package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"crm-lead-management/backend/internal/config"
	"crm-lead-management/backend/internal/mail"
	"crm-lead-management/backend/internal/model"
	"crm-lead-management/backend/internal/secrets"
)

// The Google Sheets export end to end, through the real router and database,
// against a fake Google: connect, choose a sheet, export, update without
// duplicates, fail and retry, permissions, disconnect.

// fakeGoogle is enough of Google's OAuth, Sheets and Drive APIs for the export.
type fakeGoogle struct {
	t  *testing.T
	mu sync.Mutex
	// tabs[spreadsheet][tab] is a grid of cells, row 1 first.
	tabs       map[string]map[string][][]string
	revoked    map[string]bool
	failAppend bool
	refreshes  int
}

func newFakeGoogle(t *testing.T) (*fakeGoogle, *httptest.Server) {
	g := &fakeGoogle{t: t, tabs: map[string]map[string][][]string{}, revoked: map[string]bool{}}
	srv := httptest.NewServer(g)
	t.Cleanup(srv.Close)
	return g, srv
}

func (g *fakeGoogle) grid(sheet, tab string) [][]string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.tabs[sheet][tab]
}

func (g *fakeGoogle) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	reply := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	authed := func() bool {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if tok == "" || g.revoked[tok] {
			reply(401, map[string]any{"error": map[string]any{"message": "Invalid Credentials"}})
			return false
		}
		return true
	}
	p := r.URL.Path
	switch {
	case p == "/token":
		_ = r.ParseForm()
		if r.Form.Get("client_secret") != "test-secret" {
			reply(401, map[string]string{"error": "invalid_client"})
			return
		}
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			if r.Form.Get("code_verifier") == "" {
				reply(400, map[string]string{"error": "invalid_request", "error_description": "missing verifier"})
				return
			}
			scope := "openid email https://www.googleapis.com/auth/drive.file"
			if r.Form.Get("code") == "no-drive" {
				scope = "openid email"
			}
			g.refreshes++
			reply(200, map[string]any{"access_token": fmt.Sprintf("at-c%d", g.refreshes), "refresh_token": "rt-1", "expires_in": 3600, "scope": scope})
		case "refresh_token":
			if g.revoked[r.Form.Get("refresh_token")] {
				reply(400, map[string]string{"error": "invalid_grant", "error_description": "Token has been expired or revoked."})
				return
			}
			g.refreshes++
			reply(200, map[string]any{"access_token": fmt.Sprintf("at-r%d", g.refreshes), "expires_in": 3600})
		}
	case p == "/revoke":
		_ = r.ParseForm()
		g.revoked[r.Form.Get("token")] = true
		reply(200, map[string]any{})
	case p == "/userinfo":
		if authed() {
			reply(200, map[string]string{"email": "owner@example.test"})
		}
	case p == "/drive/files":
		if !authed() {
			return
		}
		files := []map[string]string{}
		for id := range g.tabs {
			files = append(files, map[string]string{"id": id, "name": "Sheet " + id, "webViewLink": "https://docs.example/" + id})
		}
		reply(200, map[string]any{"files": files})
	case strings.HasPrefix(p, "/sheets/spreadsheets"):
		if authed() {
			g.sheets(w, r, reply)
		}
	default:
		reply(404, map[string]any{"error": map[string]any{"message": "no such fake endpoint " + p}})
	}
}

func (g *fakeGoogle) sheets(w http.ResponseWriter, r *http.Request, reply func(int, any)) {
	rest := strings.TrimPrefix(r.URL.Path, "/sheets/spreadsheets")
	body := map[string]any{}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	if rest == "" && r.Method == http.MethodPost { // create
		id := fmt.Sprintf("new%d", len(g.tabs)+1)
		g.tabs[id] = map[string][][]string{"Leads": nil}
		title := body["properties"].(map[string]any)["title"]
		reply(200, map[string]any{"spreadsheetId": id, "spreadsheetUrl": "https://docs.example/" + id,
			"properties": map[string]any{"title": title}})
		return
	}
	rest = strings.TrimPrefix(rest, "/")
	id, after, _ := strings.Cut(rest, "/")
	if strings.HasSuffix(id, ":batchUpdate") { // add a tab
		id = strings.TrimSuffix(id, ":batchUpdate")
		reqs := body["requests"].([]any)
		title := reqs[0].(map[string]any)["addSheet"].(map[string]any)["properties"].(map[string]any)["title"].(string)
		g.tabs[id][title] = nil
		reply(200, map[string]any{})
		return
	}
	tabs, ok := g.tabs[id]
	if !ok {
		reply(404, map[string]any{"error": map[string]any{"message": "Requested entity was not found."}})
		return
	}
	switch {
	case after == "":
		sheets := []any{}
		for title := range tabs {
			sheets = append(sheets, map[string]any{"properties": map[string]any{"title": title}})
		}
		reply(200, map[string]any{"spreadsheetId": id, "spreadsheetUrl": "https://docs.example/" + id,
			"properties": map[string]any{"title": "Sheet " + id}, "sheets": sheets})
	case after == "values:batchUpdate":
		if body["valueInputOption"] != "RAW" {
			g.t.Errorf("batchUpdate valueInputOption = %v, want RAW", body["valueInputOption"])
		}
		for _, d := range body["data"].([]any) {
			d := d.(map[string]any)
			tab, row, col := parseA1(d["range"].(string))
			for i, v := range d["values"].([]any)[0].([]any) {
				g.set(id, tab, row, col+i, v.(string))
			}
		}
		reply(200, map[string]any{})
	case strings.HasPrefix(after, "values/") && strings.HasSuffix(after, ":append"):
		if g.failAppend {
			reply(400, map[string]any{"error": map[string]any{"message": "Invalid values"}})
			return
		}
		if r.URL.Query().Get("valueInputOption") != "RAW" || r.URL.Query().Get("insertDataOption") != "INSERT_ROWS" {
			g.t.Errorf("append query = %s", r.URL.RawQuery)
		}
		tab, _, _ := parseA1(strings.TrimSuffix(strings.TrimPrefix(after, "values/"), ":append"))
		for _, v := range body["values"].([]any) {
			next := len(g.tabs[id][tab]) + 1
			for i, cell := range v.([]any) {
				g.set(id, tab, next, i, cell.(string))
			}
		}
		reply(200, map[string]any{})
	case strings.HasPrefix(after, "values/"):
		tab, row, col := parseA1(strings.TrimPrefix(after, "values/"))
		grid := tabs[tab]
		out := [][]string{}
		switch {
		case row > 0: // one row
			if row <= len(grid) {
				out = append(out, grid[row-1])
			}
		case col >= 0: // one column
			for _, r := range grid {
				if col < len(r) {
					out = append(out, []string{r[col]})
				} else {
					out = append(out, []string{})
				}
			}
		default:
			out = grid
		}
		reply(200, map[string]any{"values": out})
	}
}

func (g *fakeGoogle) set(id, tab string, row, col int, v string) {
	grid := g.tabs[id][tab]
	for len(grid) < row {
		grid = append(grid, []string{})
	}
	for len(grid[row-1]) <= col {
		grid[row-1] = append(grid[row-1], "")
	}
	grid[row-1][col] = v
	g.tabs[id][tab] = grid
}

// parseA1 reads 'Tab'!A5:C5 (row 5, column 0), 'Tab'!1:1 (row 1),
// 'Tab'!A:A (column 0) or 'Tab' (everything: row 0, column -1).
func parseA1(rng string) (tab string, row, col int) {
	tab, cells, _ := strings.Cut(rng, "!")
	tab = strings.ReplaceAll(strings.Trim(tab, "'"), "''", "'")
	col = -1
	if cells == "" {
		return tab, 0, -1
	}
	first, _, _ := strings.Cut(cells, ":")
	letters := strings.TrimRight(first, "0123456789")
	digits := first[len(letters):]
	if letters != "" {
		col = 0
		for _, ch := range letters {
			col = col*26 + int(ch-'A'+1)
		}
		col--
	}
	row, _ = strconv.Atoi(digits)
	return tab, row, col
}

func newSheetsEnv(t *testing.T) (*authEnv, *fakeGoogle) {
	t.Helper()
	e := newAuthEnv(t)
	g, srv := newFakeGoogle(t)
	key := make([]byte, secrets.KeySize)
	box, err := secrets.New(key)
	if err != nil {
		t.Fatal(err)
	}
	e.st.SetSecretsBox(box)
	cfg := config.Config{AdminAPIKey: testAdminKey, Env: "development", BaseURL: "http://app.test", SecretsKey: key,
		GoogleSheets: config.GoogleSheetsConfig{
			ClientID: "test-client", ClientSecret: "test-secret", RedirectURL: "http://api.test" + sheetsCallbackPath,
			AuthURL: srv.URL + "/auth", TokenURL: srv.URL + "/token", RevokeURL: srv.URL + "/revoke",
			UserInfoURL: srv.URL + "/userinfo", SheetsURL: srv.URL + "/sheets", DriveURL: srv.URL + "/drive",
		}}
	gin.SetMode(gin.TestMode)
	e.router = NewRouter(cfg, e.st, slog.New(slog.NewTextHandler(io.Discard, nil)), mail.New(config.SMTPConfig{}), time.UTC)

	// Leave no settings or history behind, before or after.
	clean := func() {
		ctx := context.Background()
		_, _ = e.db.Exec(ctx, `DELETE FROM integration_settings WHERE provider = 'google_sheets'`)
		_, _ = e.db.Exec(ctx, `DELETE FROM sheet_syncs`)
		_, _ = e.db.Exec(ctx, `DELETE FROM google_oauth_states`)
	}
	clean()
	t.Cleanup(clean)
	return e, g
}

// callback is Google sending the browser back, returning where it redirects.
func (e *authEnv) callback(t *testing.T, token string, query url.Values) string {
	t.Helper()
	req := httptest.NewRequest("GET", sheetsCallbackPath+"?"+query.Encode(), nil)
	if token != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	}
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("callback = %d %s, want a redirect", rec.Code, rec.Body.String())
	}
	return rec.Header().Get("Location")
}

// connectURL starts a sign-in and returns its state.
func (e *authEnv) connectState(t *testing.T, token string) string {
	t.Helper()
	var out struct {
		URL string `json:"url"`
	}
	if code, body := e.as(t, token, "POST", "/api/integrations/google-sheets/connect", nil, &out); code != 200 {
		t.Fatalf("connect = %d %s", code, body)
	}
	u, err := url.Parse(out.URL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("scope") != "openid email https://www.googleapis.com/auth/drive.file" ||
		q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" ||
		q.Get("redirect_uri") != "http://api.test"+sheetsCallbackPath || q.Get("access_type") != "offline" {
		t.Fatalf("consent URL asks for the wrong things: %s", out.URL)
	}
	return q.Get("state")
}

type sheetsView struct {
	Settings model.SheetsSettings `json:"settings"`
	Syncs    []model.SheetSync    `json:"syncs"`
}

// waitSync waits for the newest sync to finish and returns it.
func (e *authEnv) waitSync(t *testing.T, token string, before int) model.SheetSync {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var v sheetsView
		e.as(t, token, "GET", "/api/integrations/google-sheets", nil, &v)
		if len(v.Syncs) > before && v.Syncs[0].Status != "running" {
			return v.Syncs[0]
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("sync did not finish")
	return model.SheetSync{}
}

// leadRows maps lead id to its rows in the tab.
func leadRows(grid [][]string) map[string]int {
	rows := map[string]int{}
	for i, r := range grid {
		if i > 0 && len(r) > 0 && r[0] != "" {
			rows[r[0]]++
		}
	}
	return rows
}

func TestGoogleSheetsEndToEnd(t *testing.T) {
	e, g := newSheetsEnv(t)
	admin := e.account(t, model.RoleAdmin, "gs-admin", testPassword)
	admin2 := e.account(t, model.RoleAdmin, "gs-admin2", testPassword)
	sales := e.account(t, model.RoleSales, "gs-sales", testPassword)
	adminTok, _ := e.login(t, admin.Email, testPassword)
	admin2Tok, _ := e.login(t, admin2.Email, testPassword)
	salesTok, _ := e.login(t, sales.Email, testPassword)
	salesLead := e.lead(t, "gs-1", &sales.ID)
	other := e.lead(t, "gs-2", nil)

	// Salespeople cannot see, configure or run it.
	for _, r := range [][2]string{{"GET", ""}, {"POST", "/sync"}, {"POST", "/connect"}, {"PUT", ""}} {
		if code, _ := e.as(t, salesTok, r[0], "/api/integrations/google-sheets"+r[1], map[string]any{}, nil); code != 403 {
			t.Errorf("sales %s %s = %d, want 403", r[0], r[1], code)
		}
	}

	var view sheetsView
	e.as(t, adminTok, "GET", "/api/integrations/google-sheets", nil, &view)
	if view.Settings.Status != model.SheetNotConnected || !view.Settings.OAuthConfigured {
		t.Fatalf("before connecting: %+v", view.Settings)
	}

	// Cancelled consent.
	state := e.connectState(t, adminTok)
	if loc := e.callback(t, adminTok, url.Values{"state": {state}, "error": {"access_denied"}}); !strings.Contains(loc, "google_sheets_error=") || !strings.Contains(loc, "not+given") {
		t.Errorf("cancelled consent redirected to %s", loc)
	}
	// Someone else's session cannot finish an admin's sign-in.
	state = e.connectState(t, adminTok)
	if loc := e.callback(t, salesTok, url.Values{"state": {state}, "code": {"good"}}); !strings.Contains(loc, "google_sheets_error=") {
		t.Errorf("sales finishing the sign-in redirected to %s", loc)
	}
	if loc := e.callback(t, admin2Tok, url.Values{"state": {state}, "code": {"good"}}); !strings.Contains(loc, "google_sheets_error=") {
		t.Errorf("another admin finishing the sign-in redirected to %s", loc)
	}
	// Unticking Drive access on the consent screen is refused.
	state = e.connectState(t, adminTok)
	if loc := e.callback(t, adminTok, url.Values{"state": {state}, "code": {"no-drive"}}); !strings.Contains(loc, "google_sheets_error=") {
		t.Errorf("missing scope redirected to %s", loc)
	}
	// The real thing, once.
	state = e.connectState(t, adminTok)
	if loc := e.callback(t, adminTok, url.Values{"state": {state}, "code": {"good"}}); loc != "http://app.test/integrations?google_sheets=connected" {
		t.Fatalf("connect redirected to %s", loc)
	}
	if loc := e.callback(t, adminTok, url.Values{"state": {state}, "code": {"good"}}); !strings.Contains(loc, "expired+or+was+already+used") {
		t.Errorf("replayed state redirected to %s", loc)
	}
	var sealed []byte
	if err := e.db.QueryRow(context.Background(), `SELECT secrets FROM integration_settings WHERE provider = 'google_sheets'`).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if len(sealed) == 0 || strings.Contains(string(sealed), "rt-1") {
		t.Fatal("the refresh token must be stored sealed")
	}
	e.as(t, adminTok, "GET", "/api/integrations/google-sheets", nil, &view)
	if view.Settings.Status != model.SheetConnected || view.Settings.AccountEmail != "owner@example.test" {
		t.Fatalf("after connecting: %+v", view.Settings)
	}
	if strings.Contains(fmt.Sprint(view), "rt-1") {
		t.Fatal("the settings must not show the token")
	}

	// Nothing chosen yet.
	if code, _ := e.as(t, adminTok, "POST", "/api/integrations/google-sheets/sync", nil, nil); code != 409 {
		t.Errorf("sync with no sheet = %d, want 409", code)
	}

	// A spreadsheet with a tab of someone else's data, and an empty one.
	g.mu.Lock()
	g.tabs["s1"] = map[string][][]string{"Budget": {{"Item", "Cost"}, {"Pens", "40"}}, "Leads": nil}
	g.mu.Unlock()
	var sheets []map[string]any
	if code, body := e.as(t, adminTok, "GET", "/api/integrations/google-sheets/spreadsheets", nil, &sheets); code != 200 || len(sheets) != 1 {
		t.Fatalf("list spreadsheets = %d %s", code, body)
	}
	cfg := map[string]any{"spreadsheet_id": "s1", "sheet_title": "Budget",
		"columns": []string{"name", "email", "owner", "stage"}, "auto_sync": false, "auto_sync_minutes": 15}
	if code, body := e.as(t, adminTok, "PUT", "/api/integrations/google-sheets", cfg, nil); code != 200 {
		t.Fatalf("save = %d %s", code, body)
	}
	var test struct {
		OK      bool   `json:"ok"`
		Message string `json:"message"`
	}
	e.as(t, adminTok, "POST", "/api/integrations/google-sheets/test", nil, &test)
	if test.OK || !strings.Contains(test.Message, "other data") {
		t.Errorf("test on a tab in use = %+v", test)
	}
	// Syncing there would fail, and must not touch the budget.
	e.as(t, adminTok, "POST", "/api/integrations/google-sheets/sync", nil, nil)
	if y := e.waitSync(t, adminTok, 0); y.Status != "failed" {
		t.Errorf("sync to a tab in use = %+v", y)
	}
	if got := g.grid("s1", "Budget"); len(got) != 2 || got[1][0] != "Pens" {
		t.Fatalf("the budget tab was changed: %v", got)
	}

	cfg["sheet_title"] = "Leads"
	if code, body := e.as(t, adminTok, "PUT", "/api/integrations/google-sheets", cfg, nil); code != 200 {
		t.Fatalf("save = %d %s", code, body)
	}
	e.as(t, adminTok, "POST", "/api/integrations/google-sheets/test", nil, &test)
	if !test.OK {
		t.Fatalf("test = %+v", test)
	}

	// First export: every lead added once, header first.
	if code, body := e.as(t, adminTok, "POST", "/api/integrations/google-sheets/sync", nil, nil); code != 202 {
		t.Fatalf("sync = %d %s", code, body)
	}
	first := e.waitSync(t, adminTok, 1)
	if first.Status != "success" || first.RowsAdded != first.LeadsTotal || first.LeadsTotal < 2 || first.StartedBy == "" {
		t.Fatalf("first sync = %+v", first)
	}
	grid := g.grid("s1", "Leads")
	if want := []string{"CRM Lead ID", "Name", "Email", "Assigned To", "Status"}; fmt.Sprint(grid[0]) != fmt.Sprint(want) {
		t.Errorf("header = %v", grid[0])
	}
	rows := leadRows(grid)
	for _, id := range []int64{salesLead.ID, other.ID} {
		if rows[strconv.FormatInt(id, 10)] != 1 {
			t.Errorf("lead %d is on %d rows, want 1", id, rows[strconv.FormatInt(id, 10)])
		}
	}

	// Changes update the same rows: no duplicates.
	if _, err := e.db.Exec(context.Background(), `UPDATE leads SET name = '=HYPERLINK("x")' WHERE id = $1`, other.ID); err != nil {
		t.Fatal(err)
	}
	e.as(t, adminTok, "POST", "/api/integrations/google-sheets/sync", nil, nil)
	second := e.waitSync(t, adminTok, 2)
	if second.Status != "success" || second.RowsAdded != 0 || second.RowsUpdated != first.LeadsTotal {
		t.Fatalf("second sync = %+v", second)
	}
	grid = g.grid("s1", "Leads")
	if len(grid) != first.LeadsTotal+1 {
		t.Errorf("tab has %d rows after updating, want %d", len(grid), first.LeadsTotal+1)
	}
	for _, r := range grid {
		if r[0] == strconv.FormatInt(other.ID, 10) && r[1] != `=HYPERLINK("x")` {
			t.Errorf("updated name = %q", r[1])
		}
	}

	// A failed write is recorded with its error, and Retry finishes it.
	added := e.lead(t, "gs-3", nil)
	g.mu.Lock()
	g.failAppend = true
	g.mu.Unlock()
	e.as(t, adminTok, "POST", "/api/integrations/google-sheets/sync", nil, nil)
	failed := e.waitSync(t, adminTok, 3)
	if failed.Status != "partial" || failed.RowsFailed != 1 || !strings.Contains(failed.Error, "Invalid values") {
		t.Fatalf("failing sync = %+v", failed)
	}
	g.mu.Lock()
	g.failAppend = false
	g.mu.Unlock()
	if code, body := e.as(t, adminTok, "POST", fmt.Sprintf("/api/integrations/google-sheets/syncs/%d/retry", second.ID), nil, nil); code != 409 {
		t.Errorf("retrying a successful sync = %d %s, want 409", code, body)
	}
	if code, body := e.as(t, adminTok, "POST", fmt.Sprintf("/api/integrations/google-sheets/syncs/%d/retry", failed.ID), nil, nil); code != 202 {
		t.Fatalf("retry = %d %s", code, body)
	}
	retried := e.waitSync(t, adminTok, 4)
	if retried.Status != "success" || retried.Trigger != "retry" || retried.RetryOf == nil || *retried.RetryOf != failed.ID || retried.RowsAdded != 1 {
		t.Fatalf("retried sync = %+v", retried)
	}
	if n := leadRows(g.grid("s1", "Leads"))[strconv.FormatInt(added.ID, 10)]; n != 1 {
		t.Errorf("new lead is on %d rows after retry, want 1", n)
	}

	// The connecting admin demoted: syncs stop rather than run with
	// someone else's (or less) access.
	if _, err := e.db.Exec(context.Background(), `UPDATE team_members SET role = 'sales' WHERE id = $1`, admin.ID); err != nil {
		t.Fatal(err)
	}
	if code, body := e.as(t, admin2Tok, "POST", "/api/integrations/google-sheets/sync", nil, nil); code != 409 || !strings.Contains(body, "no longer an active admin") {
		t.Errorf("sync after the connecting admin was demoted = %d %s", code, body)
	}
	if _, err := e.db.Exec(context.Background(), `UPDATE team_members SET role = 'admin' WHERE id = $1`, admin.ID); err != nil {
		t.Fatal(err)
	}

	// Disconnect: the token is revoked at Google and forgotten here.
	if code, body := e.as(t, admin2Tok, "POST", "/api/integrations/google-sheets/disconnect", nil, &view); code != 200 {
		t.Fatalf("disconnect = %d %s", code, body)
	}
	g.mu.Lock()
	revoked := g.revoked["rt-1"]
	g.mu.Unlock()
	if !revoked {
		t.Error("disconnect did not revoke the refresh token")
	}
	if view.Settings.Status != model.SheetNotConnected || view.Settings.SpreadsheetID != "" || view.Settings.AutoSync {
		t.Errorf("after disconnect: %+v", view.Settings)
	}
	if err := e.db.QueryRow(context.Background(), `SELECT secrets FROM integration_settings WHERE provider = 'google_sheets'`).Scan(&sealed); err != nil || sealed != nil {
		t.Errorf("secrets after disconnect = %v, %v; want NULL", sealed, err)
	}
	if code, _ := e.as(t, admin2Tok, "POST", "/api/integrations/google-sheets/sync", nil, nil); code != 409 {
		t.Errorf("sync after disconnect = %d, want 409", code)
	}
}

func TestGoogleSheetsAdminKeyCannotConnect(t *testing.T) {
	e, _ := newSheetsEnv(t)
	req := httptest.NewRequest("POST", "/api/integrations/google-sheets/connect", nil)
	req.Header.Set(adminKeyHeader, testAdminKey)
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	if rec.Code != 409 {
		t.Errorf("connect with the admin key = %d %s, want 409", rec.Code, rec.Body.String())
	}
}

func TestGoogleSheetsCallbackNeedsASession(t *testing.T) {
	e, _ := newSheetsEnv(t)
	if loc := e.callback(t, "", url.Values{"state": {"x"}, "code": {"y"}}); !strings.Contains(loc, "session+ended") {
		t.Errorf("callback without a session redirected to %s", loc)
	}
}
