package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"crm-lead-management/backend/internal/model"
	"crm-lead-management/backend/internal/store"
)

// Tests for Settings > Marketing Integrations > Meta CAPI and the lifecycle
// events it sends. Meta is the fake Graph API from metaads_test.go: these
// prove the CRM's side, not that Meta accepts the events. That needs the
// manual Test Events check in the docs.

// capiSettings is a valid PUT /api/integrations/meta-capi body.
func (e *metaEnv) capiSettings(withToken bool) map[string]any {
	body := map[string]any{
		"dataset_id": e.graph.datasetID, "app_id": e.graph.appID, "test_event_code": "TEST123",
		"test_mode": true, "enabled": true, "crm_name": "Test CRM", "phone_country_code": "91",
		"event_mapping": []map[string]any{
			{"stage": "new", "event_name": "Lead"},
			{"stage": "qualified", "event_name": "Qualified Lead"},
			{"stage": "won", "event_name": "Converted"},
		},
	}
	if withToken {
		body["access_token"] = e.graph.capiToken
	}
	return body
}

type capiViewBody struct {
	DatasetID   string            `json:"dataset_id"`
	Enabled     bool              `json:"enabled"`
	AccessToken model.SecretState `json:"access_token"`
	Status      model.CAPIHealth  `json:"status"`
	Rotated     *time.Time        `json:"token_rotated_at"`
}

func TestMetaCAPIConfigurationValidationAndSecrets(t *testing.T) {
	e := newMetaEnv(t)
	ctx := context.Background()
	t.Cleanup(func() { _, _ = e.db.Exec(ctx, `DELETE FROM audit_log WHERE action LIKE 'meta_capi.%'`) })

	// Admin only, every route.
	for _, r := range []struct{ method, path string }{
		{"GET", "/api/integrations/meta-capi"}, {"PUT", "/api/integrations/meta-capi"},
		{"PUT", "/api/integrations/meta-capi/token"}, {"POST", "/api/integrations/meta-capi/test"},
		{"POST", "/api/integrations/meta-capi/test-event"}, {"POST", "/api/integrations/meta-capi/disable"},
		{"GET", "/api/integrations/meta-capi/events"}, {"GET", "/api/integrations/meta-capi/audit"},
	} {
		if code, _ := e.anon(t, r.method, r.path, map[string]any{}); code != http.StatusUnauthorized {
			t.Errorf("%s %s without logging in: %d", r.method, r.path, code)
		}
		if code, _ := e.do(t, false, r.method, r.path, map[string]any{}, nil); code != http.StatusForbidden {
			t.Errorf("%s %s as a manager: %d, want 403", r.method, r.path, code)
		}
	}

	// Not configured to begin with.
	var view capiViewBody
	e.do(t, true, "GET", "/api/integrations/meta-capi", nil, &view)
	if view.Status != model.CAPINotConfigured || view.AccessToken.Set {
		t.Errorf("fresh = %+v", view)
	}

	// Missing Pixel/Dataset ID, missing token, malformed values: refused.
	missing := e.capiSettings(true)
	missing["dataset_id"] = ""
	if code, body := e.do(t, true, "PUT", "/api/integrations/meta-capi", missing, nil); code != http.StatusUnprocessableEntity || !strings.Contains(body, "config.dataset_id") {
		t.Errorf("no dataset: %d %s", code, body)
	}
	if code, body := e.do(t, true, "PUT", "/api/integrations/meta-capi", e.capiSettings(false), nil); code != http.StatusUnprocessableEntity || !strings.Contains(body, "secrets.capi_token") {
		t.Errorf("no token: %d %s", code, body)
	}
	bad := e.capiSettings(true)
	bad["dataset_id"], bad["app_id"] = "pixel-1", "abc"
	if code, body := e.do(t, true, "PUT", "/api/integrations/meta-capi", bad, nil); code != http.StatusUnprocessableEntity ||
		!strings.Contains(body, "config.app_id") || !strings.Contains(body, "config.dataset_id") {
		t.Errorf("malformed ids: %d %s", code, body)
	}
	noCode := e.capiSettings(true)
	noCode["test_event_code"] = ""
	if code, _ := e.do(t, true, "PUT", "/api/integrations/meta-capi", noCode, nil); code != http.StatusUnprocessableEntity {
		t.Errorf("test mode without a code: %d", code)
	}

	// Valid: saved, the token masked and never returned.
	code, body := e.do(t, true, "PUT", "/api/integrations/meta-capi", e.capiSettings(true), &view)
	if code != http.StatusOK || strings.Contains(body, e.graph.capiToken) {
		t.Fatalf("save: %d %s", code, body)
	}
	if !view.AccessToken.Set || view.AccessToken.Source != "saved" || view.Status != "untested" || !view.Enabled {
		t.Errorf("saved view = %+v", view)
	}

	// Saving again without a token keeps it.
	if code, _ := e.do(t, true, "PUT", "/api/integrations/meta-capi", e.capiSettings(false), &view); code != http.StatusOK || !view.AccessToken.Set {
		t.Errorf("save without token: %d %+v", code, view)
	}

	// Test Connection: connected, then a revoked token, then a missing permission.
	var result struct {
		OK     bool             `json:"ok"`
		Status model.CAPIHealth `json:"status"`
		Detail string           `json:"detail"`
	}
	e.do(t, true, "POST", "/api/integrations/meta-capi/test", nil, &result)
	if !result.OK || result.Status != model.CAPIConnected {
		t.Errorf("test: %+v", result)
	}
	for mode, want := range map[string]model.CAPIHealth{"auth": model.CAPIInvalidCredentials, "permission": model.CAPIPermissionDenied} {
		e.graph.datasetMode = mode
		e.do(t, true, "POST", "/api/integrations/meta-capi/test", nil, &result)
		e.do(t, true, "GET", "/api/integrations/meta-capi", nil, &view)
		if result.OK || result.Status != want || view.Status != want || strings.Contains(result.Detail, e.graph.capiToken) {
			t.Errorf("%s: result %+v, view %s", mode, result, view.Status)
		}
	}
	e.graph.datasetMode = ""

	// Update Access Token: a rotation, which resets the status until tested.
	if code, _ := e.do(t, true, "PUT", "/api/integrations/meta-capi/token", map[string]any{"access_token": "has a space"}, nil); code != http.StatusUnprocessableEntity {
		t.Errorf("bad token: %d", code)
	}
	newToken := e.graph.capiToken + "-ROTATED"
	code, body = e.do(t, true, "PUT", "/api/integrations/meta-capi/token", map[string]any{"access_token": newToken}, &view)
	if code != http.StatusOK || strings.Contains(body, newToken) || view.Rotated == nil || view.Status != "untested" {
		t.Errorf("rotate: %d %s", code, body)
	}

	// The audit log records what changed, and never a token.
	_, auditBody := e.do(t, true, "GET", "/api/integrations/meta-capi/audit", nil, nil)
	for _, want := range []string{"meta_capi.settings_saved", "meta_capi.token_rotated", "meta_capi.tested", `"replaced"`} {
		if !strings.Contains(auditBody, want) {
			t.Errorf("audit log has no %s", want)
		}
	}
	if strings.Contains(auditBody, e.graph.capiToken) || strings.Contains(auditBody, "ROTATED") {
		t.Error("a token reached the audit log")
	}

	// Disable: status Disabled; the token stays saved.
	e.do(t, true, "POST", "/api/integrations/meta-capi/disable", nil, &view)
	if view.Status != model.CAPIDisabled || view.Enabled || !view.AccessToken.Set {
		t.Errorf("after disable = %+v", view)
	}
}

func TestMetaCAPISendTestEvent(t *testing.T) {
	e := newMetaEnv(t)
	e.do(t, true, "PUT", "/api/meta/settings", e.settings(false, true), nil) // the lead-ads half, to import a lead
	settings := e.capiSettings(true)
	settings["test_event_code"] = ""
	settings["test_mode"] = false
	// Live mode (enabled, no test mode) needs a passed test first.
	if code, _ := e.do(t, true, "PUT", "/api/integrations/meta-capi", settings, nil); code != http.StatusConflict {
		t.Fatalf("live before a test: %d, want 409", code)
	}
	settings["test_mode"], settings["enabled"] = true, false
	if code, body := e.do(t, true, "PUT", "/api/integrations/meta-capi", settings, nil); code != http.StatusOK {
		t.Fatalf("save without a test code: %d %s", code, body)
	}

	// Without a test code it refuses: a test must never count as a conversion.
	if code, body := e.do(t, true, "POST", "/api/integrations/meta-capi/test-event", nil, nil); code != http.StatusConflict || !strings.Contains(body, "test event code") {
		t.Errorf("no test code: %d %s", code, body)
	}
	e.do(t, true, "PUT", "/api/integrations/meta-capi", e.capiSettings(true), nil)

	var res struct {
		EventName      string `json:"event_name"`
		EventsReceived int    `json:"events_received"`
		UsedLeadID     bool   `json:"used_lead_id"`
	}
	// The test DB may hold Meta leads from other tests; make sure there are
	// none that can be shared, to check the no-lead path first.
	ctx := context.Background()
	_, _ = e.db.Exec(ctx, `UPDATE leads SET meta_share_opt_out = true WHERE id IN (SELECT lead_id FROM meta_lead_attribution)`)
	if code, body := e.do(t, true, "POST", "/api/integrations/meta-capi/test-event", nil, &res); code != http.StatusOK {
		t.Fatalf("test event: %d %s", code, body)
	}
	sent := e.graph.events[len(e.graph.events)-1]
	ev := sent["data"].([]any)[0].(map[string]any)
	user := ev["user_data"].(map[string]any)
	if res.UsedLeadID || user["lead_id"] != nil || user["em"] == nil || sent["test_event_code"] != "TEST123" ||
		ev["event_name"] != "Lead" || ev["action_source"] != "system_generated" {
		t.Errorf("test event without a lead = %+v / %v", res, sent)
	}

	// With a real Meta lead stored, its lead id is used (a made-up one is rejected by Meta).
	leadgen := fmt.Sprint(e.stamp + 30)
	e.addLead(leadgen, "Test Event Lead", fmt.Sprintf("te-%d@acme.example", e.stamp), "+91 98765 43210")
	e.signedWebhook(leadgen)
	e.do(t, true, "POST", "/api/integrations/meta-capi/test-event", nil, &res)
	user = e.graph.events[len(e.graph.events)-1]["data"].([]any)[0].(map[string]any)["user_data"].(map[string]any)
	if !res.UsedLeadID || fmt.Sprintf("%.0f", user["lead_id"]) != leadgen {
		t.Errorf("test event with a lead: %+v %v", res, user)
	}
}

func TestMetaCAPILifecycleEvents(t *testing.T) {
	e := newMetaEnv(t)
	ctx := context.Background()
	e.do(t, true, "PUT", "/api/meta/settings", e.settings(false, true), nil) // page token etc.
	if code, body := e.do(t, true, "PUT", "/api/integrations/meta-capi", e.capiSettings(true), nil); code != http.StatusOK {
		t.Fatalf("capi settings: %d %s", code, body)
	}

	importLead := func(n int64, name string) model.Lead {
		t.Helper()
		id := fmt.Sprint(e.stamp + n)
		e.addLead(id, name, fmt.Sprintf("life-%d-%d@acme.example", n, e.stamp), "+91 98765 43210")
		if rec := e.signedWebhook(id); rec.Code != http.StatusOK {
			t.Fatalf("webhook: %d", rec.Code)
		}
		lead, err := e.st.GetLead(ctx, *adLeadFor(t, e.st, model.ProviderMeta, id).LeadID)
		if err != nil {
			t.Fatal(err)
		}
		return lead
	}
	eventsOf := func(id int64) []model.CAPIEvent {
		t.Helper()
		got, err := e.st.ListCAPIEvents(ctx, model.CAPIEventQuery{LeadID: id})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	stage := func(id int64, s string) {
		t.Helper()
		body := map[string]any{"stage": s}
		if s == "won" {
			body["close_reason"] = "price"
		}
		if code, raw := e.do(t, false, "PATCH", fmt.Sprintf("/api/leads/%d/stage", id), body, nil); code != http.StatusOK {
			t.Fatalf("stage %s: %d %s", s, code, raw)
		}
	}

	// --- Lead created ------------------------------------------------------
	a := importLead(40, "Asha Lifecycle")
	if got := eventsOf(a.ID); len(got) != 1 || got[0].EventName != "Lead" || got[0].Stage != model.StageNew {
		t.Fatalf("on import: %+v", got)
	}

	// --- A jump straight to Won backfills Qualified, in order -------------
	stage(a.ID, "won")
	got := eventsOf(a.ID)
	if len(got) != 3 {
		t.Fatalf("after won: %d events %+v", len(got), got)
	}
	byName := map[string]model.CAPIEvent{}
	for _, ev := range got {
		byName[ev.EventName] = ev
	}
	if byName["Lead"].EventTime.After(byName["Qualified Lead"].EventTime) || byName["Qualified Lead"].EventTime.After(byName["Converted"].EventTime) {
		t.Errorf("event times out of lifecycle order: %+v", byName)
	}

	// --- Repeated transitions make no new milestone --------------------------
	stage(a.ID, "negotiation")
	stage(a.ID, "won")
	stage(a.ID, "qualified")
	if n := len(eventsOf(a.ID)); n != 3 {
		t.Errorf("repeated transitions made %d events, want still 3", n)
	}

	// Sent in lifecycle order, with stable ids a Pixel could reuse.
	e.graph.events = nil
	if n, err := e.svc.SendDue(ctx); err != nil || n < 3 {
		t.Fatalf("send: %d %v", n, err)
	}
	var order []string
	for _, body := range e.graph.events {
		ev := body["data"].([]any)[0].(map[string]any)
		if ev["event_id"] == model.CAPIEventID(a.ID, ev["event_name"].(string), true) {
			order = append(order, ev["event_name"].(string))
		}
	}
	if strings.Join(order, ",") != "Lead,Qualified Lead,Converted" {
		t.Errorf("sent order = %v", order)
	}

	// The lead table carries the latest event.
	var list struct {
		Data []model.Lead `json:"data"`
	}
	rec := send(e.router, "GET", "/api/leads?search=Asha+Lifecycle", "", nil)
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list.Data) != 1 || list.Data[0].MetaCAPIStatus != model.CAPISent || list.Data[0].MetaLastEventAt == nil ||
		list.Data[0].QualificationStatus != model.QualificationQualified {
		t.Errorf("lead list row = %+v", list.Data)
	}

	// --- Spam: never Qualified or Converted ----------------------------------
	b := importLead(41, "Spam Lifecycle")
	if code, _ := e.do(t, false, "PATCH", fmt.Sprintf("/api/leads/%d/flags", b.ID), map[string]any{"spam_confirmed": true}, nil); code != http.StatusOK {
		t.Fatalf("flag spam: %d", code)
	}
	stage(b.ID, "won")
	for _, ev := range eventsOf(b.ID) {
		if ev.Stage != model.StageNew {
			t.Errorf("a spam lead queued %s", ev.EventName)
		}
	}
	spam, _ := e.st.GetLead(ctx, b.ID)
	if spam.Quality != model.QualityPoor || !spam.SpamConfirmed {
		t.Errorf("spam lead quality = %s", spam.Quality)
	}

	// --- Opt-out: pending events are cancelled, and nothing more queues -----
	c := importLead(42, "Optout Lifecycle")
	if code, _ := e.do(t, false, "PATCH", fmt.Sprintf("/api/leads/%d/flags", c.ID), map[string]any{"meta_share_opt_out": true}, nil); code != http.StatusOK {
		t.Fatal("opt out")
	}
	stage(c.ID, "qualified")
	got = eventsOf(c.ID)
	if len(got) != 1 || got[0].Status != model.CAPICancelled {
		t.Errorf("after opt-out = %+v", got)
	}

	// --- Consent checked again at send time ----------------------------------
	d := importLead(43, "Late Optout")
	_, _ = e.db.Exec(ctx, `UPDATE leads SET meta_share_opt_out = true WHERE id = $1`, d.ID) // behind the API's back
	if _, err := e.svc.SendDue(ctx); err != nil {
		t.Fatal(err)
	}
	dEvent := model.CAPIEventID(d.ID, "Lead", true)
	for _, body := range e.graph.events {
		if body["data"].([]any)[0].(map[string]any)["event_id"] == dEvent {
			t.Error("an event was sent for a lead that opted out after it was queued")
		}
	}
	if got := eventsOf(d.ID); got[0].Status != model.CAPICancelled {
		t.Errorf("opted out after queueing: %+v", got[0])
	}

	// --- A lead not from a Meta form never queues --------------------------
	plain := model.LeadInput{Name: "Plain Lifecycle", Company: "Plain", Email: fmt.Sprintf("plain-life-%d@example.test", e.stamp)}
	plain.Normalize()
	p, _ := e.st.CreateLead(ctx, plain)
	t.Cleanup(func() { _ = e.st.DeleteLead(ctx, p.ID) })
	stage(p.ID, "won")
	if n := len(eventsOf(p.ID)); n != 0 {
		t.Errorf("a non-Meta lead queued %d events", n)
	}

	// --- Disabled: events wait, nothing is sent ------------------------------
	f := importLead(44, "Disabled Lifecycle")
	e.do(t, true, "POST", "/api/integrations/meta-capi/disable", nil, nil)
	before := len(e.graph.events)
	if n, _ := e.svc.SendDue(ctx); n != 0 || len(e.graph.events) != before {
		t.Errorf("sent %d while disabled", n)
	}
	if got := eventsOf(f.ID); got[0].Status != model.CAPIPending {
		t.Errorf("event while disabled = %s, want pending", got[0].Status)
	}
	stage(f.ID, "qualified") // disabled: nothing new queued
	if n := len(eventsOf(f.ID)); n != 1 {
		t.Errorf("queued %d while disabled", n)
	}
}

func TestMetaCAPITimeoutsAndGivingUp(t *testing.T) {
	e := newMetaEnv(t)
	ctx := context.Background()
	e.do(t, true, "PUT", "/api/meta/settings", e.settings(false, true), nil)
	e.do(t, true, "PUT", "/api/integrations/meta-capi", e.capiSettings(true), nil)

	id := fmt.Sprint(e.stamp + 50)
	e.addLead(id, "Slow Meta", fmt.Sprintf("slow-%d@acme.example", e.stamp), "+91 98765 43210")
	e.signedWebhook(id)
	lead := adLeadFor(t, e.st, model.ProviderMeta, id)

	// Meta takes longer than the timeout: a transient failure, retried later.
	e.graph.eventsDelay = 300 * time.Millisecond
	e.svc.SetSendTimeout(50 * time.Millisecond)
	if _, err := e.svc.SendDue(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := e.st.ListCAPIEvents(ctx, model.CAPIEventQuery{LeadID: *lead.LeadID})
	if got[0].Status != model.CAPIRetryScheduled || got[0].ErrorCode != nil || !strings.Contains(got[0].LastError, "could not be reached") {
		t.Fatalf("after a timeout = %+v", got[0])
	}
	var view capiViewBody
	e.do(t, true, "GET", "/api/integrations/meta-capi", nil, &view)
	if view.Status != model.CAPITemporarilyUnavailable {
		t.Errorf("status after a timeout = %s", view.Status)
	}

	// On its last attempt it gives up: failed, not retried forever.
	_, _ = e.db.Exec(ctx, `UPDATE capi_events SET attempts = $2, next_attempt_at = now() WHERE id = $1`,
		got[0].ID, model.MaxCAPIAttempts-1)
	if _, err := e.svc.SendDue(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ = e.st.ListCAPIEvents(ctx, model.CAPIEventQuery{LeadID: *lead.LeadID})
	if got[0].Status != model.CAPIFailed || !strings.Contains(got[0].LastError, "gave up") {
		t.Errorf("after the last attempt = %+v", got[0])
	}

	// A permanent refusal (a bad token) is not retried, and keeps Meta's code.
	e.graph.eventsDelay = 0
	e.svc.SetSendTimeout(5 * time.Second)
	e.graph.eventsMode = "auth"
	e.do(t, true, "POST", fmt.Sprintf("/api/integrations/meta-capi/events/%d/retry", got[0].ID), nil, nil)
	_, _ = e.svc.SendDue(ctx)
	got, _ = e.st.ListCAPIEvents(ctx, model.CAPIEventQuery{LeadID: *lead.LeadID})
	if got[0].Status != model.CAPIFailed || got[0].ErrorCode == nil || *got[0].ErrorCode != 190 || got[0].Attempts != 1 {
		t.Errorf("after an auth error = %+v", got[0])
	}
	e.do(t, true, "GET", "/api/integrations/meta-capi", nil, &view)
	if view.Status != model.CAPIInvalidCredentials {
		t.Errorf("status after an auth error = %s", view.Status)
	}
}

func TestMetaCAPIRateLimitsCallers(t *testing.T) {
	e := newMetaEnv(t)
	e.do(t, true, "PUT", "/api/integrations/meta-capi", e.capiSettings(true), nil)
	limited := 0
	for range 12 {
		req := httptest.NewRequest("POST", "/api/integrations/meta-capi/test", nil)
		req.Header.Set("X-Admin-Key", testAdminKey)
		rec := httptest.NewRecorder()
		e.router.ServeHTTP(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			limited++
			if rec.Header().Get("Retry-After") == "" {
				t.Error("429 without Retry-After")
			}
		}
	}
	if limited == 0 {
		t.Error("12 tests in a row were never rate limited")
	}
}

func TestQualityFollowsTimeline(t *testing.T) {
	e := newMetaEnv(t)
	ctx := context.Background()
	in := model.LeadInput{Name: "Timeline Quality", Company: "TQ", Email: fmt.Sprintf("tq-%d@acme.example", e.stamp), Phone: "+91 98765 43210"}
	in.Normalize()
	var lead model.Lead
	if code, body := e.do(t, false, "POST", "/api/leads", in, &lead); code != http.StatusCreated {
		t.Fatalf("create: %d %s", code, body)
	}
	t.Cleanup(func() { _ = e.st.DeleteLead(ctx, lead.ID) })
	if lead.Quality != model.QualityAverage || lead.QualityUpdatedAt == nil {
		t.Fatalf("new manual lead = %s", lead.Quality)
	}
	// A logged call and a logged meeting are evidence of fit: the score rises.
	for _, typ := range []string{"call", "meeting"} {
		if code, _ := e.do(t, false, "POST", fmt.Sprintf("/api/leads/%d/activities", lead.ID), map[string]any{"type": typ, "note": "Spoke"}, nil); code != http.StatusCreated {
			t.Fatalf("log %s: %d", typ, code)
		}
	}
	got, _ := e.st.GetLead(ctx, lead.ID)
	if got.QualityScore == nil || *got.QualityScore != 70 || got.Quality != model.QualityGood {
		t.Errorf("after a call and a meeting = %v %s %q", got.QualityScore, got.Quality, got.QualityReason)
	}
}

// keep the store import used if helpers above change
var _ = store.ErrNotFound
