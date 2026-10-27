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

	"crm-lead-management/backend/internal/config"
	"crm-lead-management/backend/internal/mail"
	"crm-lead-management/backend/internal/model"
	"crm-lead-management/backend/internal/store"
)

// These go through the real router and a real database, like the store
// tests, and are skipped the same way unless TEST_DATABASE_URL is set.

func newTestAPI(t *testing.T) (*gin.Engine, *store.Store) {
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
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	// No SMTP settings: email is "not set up", as on a fresh install.
	// Requests in these tests authenticate with the admin key (see call).
	router := NewRouter(config.Config{AdminAPIKey: testAdminKey}, st, logger, mail.New(config.SMTPConfig{}), time.UTC)
	return router, st
}

// call makes one request and decodes the response's "data" into out (if not
// nil). It returns the status code and the raw body.
func call(t *testing.T, router *gin.Engine, method, path string, body any, out any) (int, string) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(adminKeyHeader, testAdminKey)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if out != nil && rec.Code < 300 {
		envelope := struct {
			Data json.RawMessage `json:"data"`
		}{}
		if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("%s %s: decode %q: %v", method, path, rec.Body.String(), err)
		}
		if err := json.Unmarshal(envelope.Data, out); err != nil {
			t.Fatalf("%s %s: decode data %q: %v", method, path, envelope.Data, err)
		}
	}
	return rec.Code, rec.Body.String()
}

func TestEmailTemplatesCRUD(t *testing.T) {
	router, _ := newTestAPI(t)
	name := fmt.Sprintf("API test template %d", time.Now().UnixNano())

	var created model.EmailTemplate
	code, body := call(t, router, "POST", "/api/email-templates",
		model.EmailTemplateInput{Name: name, Subject: "Hi {{first_name}}", Body: "Hello {{company}}"}, &created)
	if code != http.StatusCreated {
		t.Fatalf("create = %d %s, want 201", code, body)
	}
	t.Cleanup(func() { call(t, router, "DELETE", fmt.Sprintf("/api/email-templates/%d", created.ID), nil, nil) })

	// The same name in other letters is a duplicate.
	code, body = call(t, router, "POST", "/api/email-templates",
		model.EmailTemplateInput{Name: strings.ToUpper(name), Subject: "s", Body: "b"}, nil)
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "already used") {
		t.Errorf("duplicate name = %d %s, want 422 'already used'", code, body)
	}

	// A typo in a field is rejected before it is saved.
	code, body = call(t, router, "PUT", fmt.Sprintf("/api/email-templates/%d", created.ID),
		model.EmailTemplateInput{Name: name, Subject: "Hi {{frist_name}}", Body: "b"}, nil)
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "frist_name") {
		t.Errorf("typo = %d %s, want 422 naming the field", code, body)
	}

	var updated model.EmailTemplate
	code, body = call(t, router, "PUT", fmt.Sprintf("/api/email-templates/%d", created.ID),
		model.EmailTemplateInput{Name: name, Subject: "Hello {{first_name}}", Body: "Updated"}, &updated)
	if code != http.StatusOK || updated.Subject != "Hello {{first_name}}" {
		t.Errorf("update = %d %s", code, body)
	}

	var list []model.EmailTemplate
	call(t, router, "GET", "/api/email-templates", nil, &list)
	found := false
	for _, tpl := range list {
		found = found || tpl.ID == created.ID
	}
	if !found {
		t.Errorf("created template %d is not in the list", created.ID)
	}

	if code, _ := call(t, router, "DELETE", fmt.Sprintf("/api/email-templates/%d", created.ID), nil, nil); code != http.StatusNoContent {
		t.Errorf("delete = %d, want 204", code)
	}
	if code, _ := call(t, router, "DELETE", fmt.Sprintf("/api/email-templates/%d", created.ID), nil, nil); code != http.StatusNotFound {
		t.Errorf("second delete = %d, want 404", code)
	}
}

func TestLeadEmailPreviewSendAndLog(t *testing.T) {
	router, st := newTestAPI(t)
	ctx := context.Background()

	suffix := fmt.Sprint(time.Now().UnixNano())
	in := model.LeadInput{Name: "Asha Rao", Company: "Kestrel " + suffix, Email: "asha-" + suffix + "@example.test",
		Source: model.SourceWebsite, Stage: model.StageQualified}
	in.Normalize()
	lead, err := st.CreateLead(ctx, in)
	if err != nil {
		t.Fatalf("CreateLead: %v", err)
	}
	t.Cleanup(func() { _ = st.DeleteLead(ctx, lead.ID) })

	leadPath := fmt.Sprintf("/api/leads/%d", lead.ID)
	email := model.EmailInput{Subject: "Hi {{first_name}}", Body: "About {{company}}. {{owner_name}}"}

	// Preview fills in the fields, says what is blank, and logs nothing.
	var preview model.RenderedEmail
	code, body := call(t, router, "POST", leadPath+"/email/preview", email, &preview)
	if code != http.StatusOK {
		t.Fatalf("preview = %d %s", code, body)
	}
	if preview.Subject != "Hi Asha" || preview.Body != "About "+lead.Company+". " || preview.To != lead.Email {
		t.Errorf("preview = %+v", preview)
	}
	if len(preview.Missing) != 1 || preview.Missing[0] != "owner_name" {
		t.Errorf("missing = %v, want [owner_name] for an unowned lead", preview.Missing)
	}

	// Sending needs SMTP, which this server does not have: 409, nothing logged.
	email.Send = true
	if code, body := call(t, router, "POST", leadPath+"/emails", email, nil); code != http.StatusConflict {
		t.Errorf("send without SMTP = %d %s, want 409", code, body)
	}
	activities, err := st.ListActivities(ctx, lead.ID)
	if err != nil {
		t.Fatalf("ListActivities: %v", err)
	}
	for _, a := range activities {
		if a.Type == model.ActivityEmail {
			t.Fatalf("a failed send was logged: %+v", a)
		}
	}

	// Logging only, for an email sent from the person's own app.
	email.Send = false
	var result struct {
		Activity model.Activity      `json:"activity"`
		Email    model.RenderedEmail `json:"email"`
		Sent     bool                `json:"sent"`
	}
	code, body = call(t, router, "POST", leadPath+"/emails", email, &result)
	if code != http.StatusCreated {
		t.Fatalf("log email = %d %s, want 201", code, body)
	}
	if result.Sent || result.Activity.Type != model.ActivityEmail ||
		!strings.HasPrefix(result.Activity.Note, "Logged · Subject: Hi Asha") {
		t.Errorf("log result = %+v", result)
	}

	// It is an ordinary email: a conversation for the engagement score.
	got, err := st.GetLead(ctx, lead.ID)
	if err != nil {
		t.Fatalf("GetLead: %v", err)
	}
	for _, f := range got.Score.Factors {
		if f.Key == "engagement" && f.Points != 5 {
			t.Errorf("engagement = %d after one logged email, want 5", f.Points)
		}
	}

	// A typo and a missing lead are both caught.
	if code, _ := call(t, router, "POST", leadPath+"/emails",
		model.EmailInput{Subject: "Hi {{frist_name}}", Body: "b"}, nil); code != http.StatusUnprocessableEntity {
		t.Errorf("typo = %d, want 422", code)
	}
	if code, _ := call(t, router, "POST", "/api/leads/999999999/email/preview", email, nil); code != http.StatusNotFound {
		t.Errorf("missing lead = %d, want 404", code)
	}
}
