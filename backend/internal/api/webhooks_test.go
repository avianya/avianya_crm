package api

import (
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
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"crm-lead-management/backend/internal/config"
	"crm-lead-management/backend/internal/mail"
	"crm-lead-management/backend/internal/model"
	"crm-lead-management/backend/internal/store"
)

const (
	testAppSecret   = "test-app-secret"
	testVerifyToken = "test-verify-token"
	testGoogleKey   = "test-google-key"
)

// newWebhookAPI is newTestAPI with both platforms set up, Meta's Graph API
// pointed at graphURL.
func newWebhookAPI(t *testing.T, graphURL string) (*gin.Engine, *store.Store) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set - skipping API integration tests")
	}
	st, err := store.New(context.Background(), dsn, "")
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(st.Close)

	gin.SetMode(gin.TestMode)
	cfg := config.Config{
		Meta: config.MetaConfig{
			AppSecret: testAppSecret, VerifyToken: testVerifyToken,
			PageAccessToken: "test-page-token", GraphURL: graphURL,
		},
		GoogleLeadsKey: testGoogleKey,
		AdminAPIKey:    testAdminKey,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewRouter(cfg, st, logger, mail.New(config.SMTPConfig{}), time.UTC), st
}

func send(router *gin.Engine, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	// API calls in tests authenticate with the admin key; the webhooks need
	// none and ignore it.
	req.Header.Set(adminKeyHeader, testAdminKey)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// adLeadFor finds the record of one submission among the latest received.
func adLeadFor(t *testing.T, st *store.Store, provider model.AdProvider, externalID string) model.AdLead {
	t.Helper()
	records, err := st.ListAdLeads(context.Background(), 200)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range records {
		if r.Provider == provider && r.ExternalID == externalID {
			return r
		}
	}
	t.Fatalf("no ad lead recorded for %s %s", provider, externalID)
	return model.AdLead{}
}

func cleanupLead(t *testing.T, st *store.Store, id *int64) {
	if id != nil {
		leadID := *id
		t.Cleanup(func() { _ = st.DeleteLead(context.Background(), leadID) })
	}
}

func TestGoogleLeadWebhook(t *testing.T) {
	router, st := newWebhookAPI(t, "")
	stamp := time.Now().UnixNano()
	email := fmt.Sprintf("google-%d@example.test", stamp)

	body := func(leadID string, isTest bool, key string) string {
		return fmt.Sprintf(`{"lead_id":%q,"google_key":%q,"is_test":%t,"form_id":12345678901234567,"campaign_id":987,
			"user_column_data":[
				{"column_id":"FULL_NAME","column_name":"Full Name","string_value":"Asha Rao"},
				{"column_id":"EMAIL","column_name":"User Email","string_value":%q},
				{"column_id":"CITY","column_name":"City","string_value":"Pune"}]}`, leadID, key, isTest, email)
	}

	if rec := send(router, http.MethodPost, "/api/webhooks/google", body("x", false, "wrong"), nil); rec.Code != http.StatusForbidden {
		t.Errorf("wrong key: %d", rec.Code)
	}

	// Google's test data is recorded but makes no lead.
	testID := fmt.Sprintf("g-test-%d", stamp)
	if rec := send(router, http.MethodPost, "/api/webhooks/google", body(testID, true, testGoogleKey), nil); rec.Code != http.StatusOK {
		t.Fatalf("test lead: %d %s", rec.Code, rec.Body.String())
	}
	if r := adLeadFor(t, st, model.ProviderGoogle, testID); r.Status != model.AdLeadTest || r.LeadID != nil {
		t.Errorf("test record = %+v", r)
	}

	// A real lead, sent twice (as Google does when unsure), is taken once.
	leadID := fmt.Sprintf("g-%d", stamp)
	for range 2 {
		if rec := send(router, http.MethodPost, "/api/webhooks/google", body(leadID, false, testGoogleKey), nil); rec.Code != http.StatusOK {
			t.Fatalf("lead: %d %s", rec.Code, rec.Body.String())
		}
	}
	r := adLeadFor(t, st, model.ProviderGoogle, leadID)
	cleanupLead(t, st, r.LeadID)
	if r.Status != model.AdLeadCreated || r.LeadID == nil || r.FormID != "12345678901234567" {
		t.Fatalf("record = %+v", r)
	}
	lead, err := st.GetLead(context.Background(), *r.LeadID)
	if err != nil {
		t.Fatal(err)
	}
	if lead.Source != model.SourceGoogleAds || lead.Company != model.NotProvided || !strings.Contains(lead.Notes, "City: Pune") {
		t.Errorf("lead = %+v", lead)
	}

	// The same person on another submission: a note on their lead, not a second lead.
	again := fmt.Sprintf("g-again-%d", stamp)
	send(router, http.MethodPost, "/api/webhooks/google", body(again, false, testGoogleKey), nil)
	if r := adLeadFor(t, st, model.ProviderGoogle, again); r.Status != model.AdLeadDuplicate || r.LeadID == nil || *r.LeadID != lead.ID {
		t.Errorf("repeat record = %+v", r)
	}
	lead, _ = st.GetLead(context.Background(), lead.ID)
	noted := false
	for _, a := range lead.Activities {
		noted = noted || a.Type == model.ActivityAdForm
	}
	if !noted {
		t.Error("repeat submission not on the lead's timeline")
	}
}

func TestMetaLeadWebhook(t *testing.T) {
	stamp := time.Now().UnixNano()
	email := fmt.Sprintf("meta-%d@example.test", stamp)
	var graphDown atomic.Bool
	graphDown.Store(true)

	graph := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("access_token") != "test-page-token" {
			t.Errorf("graph call without the page token: %s", r.URL)
		}
		if graphDown.Load() {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"(#100) Missing permission: leads_retrieval"}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": strings.TrimPrefix(r.URL.Path, "/"), "form_id": "555", "campaign_name": "Diwali offer",
			"field_data": []map[string]any{
				{"name": "full_name", "values": []string{"Meera Shah"}},
				{"name": "email", "values": []string{email}},
				{"name": "what_is_your_budget?", "values": []string{"5-10 lakh"}},
			},
		})
	}))
	defer graph.Close()

	router, st := newWebhookAPI(t, graph.URL)

	// Meta's subscription check.
	rec := send(router, http.MethodGet, "/api/webhooks/meta?hub.mode=subscribe&hub.verify_token="+testVerifyToken+"&hub.challenge=42", "", nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "42" {
		t.Errorf("verify: %d %q", rec.Code, rec.Body.String())
	}
	if rec := send(router, http.MethodGet, "/api/webhooks/meta?hub.mode=subscribe&hub.verify_token=nope&hub.challenge=42", "", nil); rec.Code != http.StatusForbidden {
		t.Errorf("verify with a wrong token: %d", rec.Code)
	}

	leadgenID := fmt.Sprintf("m-%d", stamp)
	payload := fmt.Sprintf(`{"object":"page","entry":[{"id":"1","changes":[{"field":"leadgen","value":{"leadgen_id":%q,"form_id":"555","ad_id":"777"}}]}]}`, leadgenID)
	mac := hmac.New(sha256.New, []byte(testAppSecret))
	mac.Write([]byte(payload))
	signed := map[string]string{"X-Hub-Signature-256": "sha256=" + hex.EncodeToString(mac.Sum(nil))}

	if rec := send(router, http.MethodPost, "/api/webhooks/meta", payload, map[string]string{"X-Hub-Signature-256": "sha256=00"}); rec.Code != http.StatusForbidden {
		t.Errorf("bad signature: %d", rec.Code)
	}

	// The Graph API refuses: recorded as failed, with Meta's reason.
	if rec := send(router, http.MethodPost, "/api/webhooks/meta", payload, signed); rec.Code != http.StatusOK {
		t.Fatalf("webhook: %d %s", rec.Code, rec.Body.String())
	}
	failed := adLeadFor(t, st, model.ProviderMeta, leadgenID)
	if failed.Status != model.AdLeadFailed || !strings.Contains(failed.Error, "leads_retrieval") || failed.Ad != "777" {
		t.Fatalf("failed record = %+v", failed)
	}

	// Fixed on Meta's side, Retry takes it in.
	graphDown.Store(false)
	var retried model.AdLead
	if code, raw := call(t, router, http.MethodPost, fmt.Sprintf("/api/ad-leads/%d/retry", failed.ID), nil, &retried); code != http.StatusOK {
		t.Fatalf("retry: %d %s", code, raw)
	}
	cleanupLead(t, st, retried.LeadID)
	if retried.Status != model.AdLeadCreated || retried.LeadID == nil || retried.Campaign != "Diwali offer" {
		t.Fatalf("retried record = %+v", retried)
	}
	lead, err := st.GetLead(context.Background(), *retried.LeadID)
	if err != nil {
		t.Fatal(err)
	}
	if lead.Name != "Meera Shah" || lead.Source != model.SourceMetaAds || !strings.Contains(lead.Notes, "What is your budget?: 5-10 lakh") {
		t.Errorf("lead = %+v", lead)
	}

	// Meta resending the webhook makes no second lead.
	send(router, http.MethodPost, "/api/webhooks/meta", payload, signed)
	if again := adLeadFor(t, st, model.ProviderMeta, leadgenID); again.Status != model.AdLeadCreated || *again.LeadID != lead.ID {
		t.Errorf("after resend = %+v", again)
	}

	// The in-app test, and the status page.
	var sample model.AdLead
	if code, raw := call(t, router, http.MethodPost, "/api/integrations/test", map[string]string{"provider": "meta"}, &sample); code != http.StatusCreated || sample.Status != model.AdLeadTest {
		t.Errorf("test lead: %d %s", code, raw)
	}
	var page struct {
		Meta   integrationStatus `json:"meta"`
		Recent []model.AdLead    `json:"recent"`
	}
	if code, raw := call(t, router, http.MethodGet, "/api/integrations", nil, &page); code != http.StatusOK || !page.Meta.Configured || len(page.Recent) == 0 {
		t.Errorf("integrations: %d %s", code, raw)
	}
}

func TestWebhooksRefuseWhenNotSetUp(t *testing.T) {
	router, _ := newTestAPI(t) // no Meta or Google settings
	for _, path := range []string{"/api/webhooks/meta", "/api/webhooks/google"} {
		if rec := send(router, http.MethodPost, path, `{}`, nil); rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: %d, want 503", path, rec.Code)
		}
	}
}
