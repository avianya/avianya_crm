package meta

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHashingFollowsMetaNormalisation(t *testing.T) {
	// SHA-256 of the normalised values, worked out with Python's hashlib.
	if got := HashEmail("  Asha@Example.COM "); got != "ea4e36a829983d27e865b649cd10fcb61f2f1bcd857b47ab709e7adbe941ab68" {
		t.Errorf("HashEmail = %q", got)
	}
	if got := HashPhone("+91 98765 43210", ""); got != "92b5072176e723878b5e06ff3ca61898e4eb74e8c46642a0f2db800b17364ab0" {
		t.Errorf("HashPhone = %q", got)
	}
	if HashEmail("   ") != "" {
		t.Error("blank email hashed")
	}

	for _, tc := range []struct{ in, cc, want string }{
		{"+91 98765 43210", "", "919876543210"},
		{"0091-98765-43210", "", "919876543210"},
		{"098765 43210", "91", "919876543210"},
		{"(987) 654-3210", "1", "19876543210"},
		{"98765 43210", "", ""}, // no country code to use: left out
		{"", "91", ""},
	} {
		if got := NormalizePhone(tc.in, tc.cc); got != tc.want {
			t.Errorf("NormalizePhone(%q, %q) = %q, want %q", tc.in, tc.cc, got, tc.want)
		}
	}
	if HashPhone("+91 98765 43210", "") != HashPhone("98765-43210", "91") {
		t.Error("the same number hashed two ways")
	}
}

func TestServerEventShape(t *testing.T) {
	at := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	ev := LeadEvent{
		EventName: "Qualified", EventID: "crm-7-qualified-live", EventTime: at,
		MetaLeadID: "1234567890123456", Email: "A@B.com", Phone: "+1 555 0100", CRMName: "Acme CRM",
	}.ServerEvent()

	raw, _ := json.Marshal(ev)
	var got map[string]any
	_ = json.Unmarshal(raw, &got)
	if got["action_source"] != "system_generated" || got["event_time"].(float64) != float64(at.Unix()) {
		t.Errorf("event = %s", raw)
	}
	user := got["user_data"].(map[string]any)
	if user["lead_id"].(float64) != 1234567890123456 {
		t.Errorf("lead_id = %v", user["lead_id"])
	}
	if em := user["em"].([]any)[0].(string); em != HashEmail("a@b.com") {
		t.Errorf("em = %v", em)
	}
	if strings.Contains(string(raw), "A@B.com") || strings.Contains(string(raw), "555") {
		t.Errorf("plain customer data in the event: %s", raw)
	}
	custom := got["custom_data"].(map[string]any)
	if custom["event_source"] != "crm" || custom["lead_event_source"] != "Acme CRM" {
		t.Errorf("custom_data = %v", custom)
	}
}

func TestEventTimeFor(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	recent := now.Add(-2 * 24 * time.Hour)
	if got := EventTimeFor(recent, now); !got.Equal(recent) {
		t.Errorf("recent event time moved: %v", got)
	}
	if got := EventTimeFor(now.Add(-8*24*time.Hour), now); !got.Equal(now) {
		t.Errorf("too-old event time kept: %v", got)
	}
}

func TestErrorsAreClassifiedAndCarryNoToken(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		kind   ErrorKind
		retry  bool
	}{
		{400, `{"error":{"message":"Error validating access token: Session has expired","code":190,"error_subcode":463}}`, KindAuth, false},
		{403, `{"error":{"message":"(#200) Requires leads_retrieval permission","code":200}}`, KindPermission, false},
		{400, `{"error":{"message":"(#17) User request limit reached","code":17}}`, KindRateLimit, true},
		{429, ``, KindRateLimit, true},
		{400, `{"error":{"message":"(#100) Invalid parameter","code":100}}`, KindInvalid, false},
		{500, `{"error":{"message":"An unknown error occurred","code":1}}`, KindTransient, true},
		{503, `oops`, KindTransient, true},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "30")
			w.WriteHeader(tc.status)
			_, _ = io.WriteString(w, tc.body)
		}))
		_, err := NewClient(srv.URL).GetLead(context.Background(), "SECRET-TOKEN", "1")
		srv.Close()

		e, ok := AsError(err)
		if !ok {
			t.Fatalf("%d %s: not a *meta.Error: %v", tc.status, tc.body, err)
		}
		if e.Kind != tc.kind || e.Retryable() != tc.retry {
			t.Errorf("%d %s: kind %s retry %v, want %s %v", tc.status, tc.body, e.Kind, e.Retryable(), tc.kind, tc.retry)
		}
		if strings.Contains(err.Error(), "SECRET-TOKEN") {
			t.Errorf("token in error: %v", err)
		}
		if tc.status == 429 && e.RetryAfter != 30*time.Second {
			t.Errorf("Retry-After not read: %v", e.RetryAfter)
		}
	}

	// Unreachable: the URL (which holds the token) must not leak.
	_, err := NewClient("http://127.0.0.1:1").GetLead(context.Background(), "SECRET-TOKEN", "1")
	if e, ok := AsError(err); !ok || e.Kind != KindTransient || strings.Contains(err.Error(), "SECRET-TOKEN") {
		t.Errorf("unreachable: %v", err)
	}
}

func TestFormLeadsPaginatesAndFilters(t *testing.T) {
	var sawFilter, sawAfter string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/555/leads" {
			t.Errorf("path %s", r.URL.Path)
		}
		sawFilter, sawAfter = r.URL.Query().Get("filtering"), r.URL.Query().Get("after")
		if sawAfter == "" {
			_, _ = io.WriteString(w, `{"data":[{"id":"1","created_time":"2026-10-20T09:15:02+0000"}],"paging":{"cursors":{"after":"CUR"},"next":"https://x"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"data":[{"id":"2"}],"paging":{"cursors":{"after":"END"}}}`)
	}))
	defer srv.Close()
	c := NewClient(srv.URL)

	since := time.Unix(1_700_000_000, 0)
	first, err := c.FormLeads(context.Background(), "t", "555", since, "", 100)
	if err != nil || len(first.Leads) != 1 || first.After != "CUR" {
		t.Fatalf("first page = %+v, %v", first, err)
	}
	if !strings.Contains(sawFilter, `"time_created"`) || !strings.Contains(sawFilter, "1700000000") {
		t.Errorf("filtering = %q", sawFilter)
	}
	if got := first.Leads[0].Created(); got.IsZero() || got.UTC().Hour() != 9 {
		t.Errorf("created_time parsed as %v", got)
	}
	last, err := c.FormLeads(context.Background(), "t", "555", since, first.After, 100)
	if err != nil || len(last.Leads) != 1 || last.After != "" || sawAfter != "CUR" {
		t.Fatalf("last page = %+v, %v (after=%q)", last, err, sawAfter)
	}
}

func TestSendEventsPutsTokenInBody(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/999/events" || r.URL.RawQuery != "" {
			t.Errorf("%s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = io.WriteString(w, `{"events_received":1,"fbtrace_id":"TRACE"}`)
	}))
	defer srv.Close()

	ev := LeadEvent{EventName: "Qualified", EventID: "e1", EventTime: time.Now(), MetaLeadID: "1"}.ServerEvent()
	res, err := NewClient(srv.URL).SendEvents(context.Background(), "CAPI", "999", []ServerEvent{ev}, "TEST123")
	if err != nil || res.EventsReceived != 1 || res.FBTraceID != "TRACE" {
		t.Fatalf("send = %+v, %v", res, err)
	}
	if body["access_token"] != "CAPI" || body["test_event_code"] != "TEST123" || len(body["data"].([]any)) != 1 {
		t.Errorf("body = %v", body)
	}
}
