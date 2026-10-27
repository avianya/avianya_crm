package gsheets

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"crm-lead-management/backend/internal/config"
)

func testClient(srvURL string) *Client {
	c := NewClient(config.GoogleSheetsConfig{
		ClientID: "id", ClientSecret: "secret", RedirectURL: "http://x/cb",
		TokenURL: srvURL + "/token", SheetsURL: srvURL, DriveURL: srvURL,
	})
	c.BaseDelay = time.Millisecond
	return c
}

func TestRetriesRateLimitsAndTemporaryErrors(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch calls.Add(1) {
		case 1:
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"Quota exceeded"}}`))
		case 2:
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			_, _ = w.Write([]byte(`{"values":[["CRM Lead ID"]]}`))
		}
	}))
	defer srv.Close()

	rows, err := testClient(srv.URL).GetValues(context.Background(), "tok", "s1", A1("Leads", "1:1"))
	if err != nil || len(rows) != 1 || rows[0][0] != "CRM Lead ID" {
		t.Fatalf("GetValues = %v, %v", rows, err)
	}
	if n := calls.Load(); n != 3 {
		t.Errorf("calls = %d, want 3", n)
	}
}

func TestGivesUpAfterMaxAttempts(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := testClient(srv.URL)
	c.MaxAttempts = 3
	err := c.BatchUpdate(context.Background(), "tok", "s1", nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusTooManyRequests {
		t.Fatalf("err = %v, want the 429", err)
	}
	if n := calls.Load(); n != 3 {
		t.Errorf("calls = %d, want 3", n)
	}
}

func TestDoesNotRetryPermanentErrors(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"message":"Requested entity was not found."}}`))
	}))
	defer srv.Close()

	_, err := testClient(srv.URL).GetSpreadsheet(context.Background(), "tok", "gone")
	if !errors.Is(err, ErrNoAccess) {
		t.Errorf("err = %v, want ErrNoAccess", err)
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("calls = %d, want 1", n)
	}
}

func TestRefreshRecognisesRevokedAccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Token has been expired or revoked."}`))
	}))
	defer srv.Close()

	if _, err := testClient(srv.URL).Refresh(context.Background(), "rt"); !errors.Is(err, ErrRevoked) {
		t.Errorf("err = %v, want ErrRevoked", err)
	}
}

func TestHonoursRetryAfter(t *testing.T) {
	if got := parseRetryAfter("3"); got != 3*time.Second {
		t.Errorf("parseRetryAfter(3) = %v", got)
	}
	c := NewClient(config.GoogleSheetsConfig{})
	if got := c.backoff(1, &retryAfter{err: errors.New("x"), after: 2 * time.Second}); got != 2*time.Second {
		t.Errorf("backoff with Retry-After = %v, want 2s", got)
	}
}

func TestPKCEChallengeMatchesVerifier(t *testing.T) {
	v, ch, err := NewPKCE()
	if err != nil || len(v) < 43 || ch == "" || ch == v {
		t.Fatalf("NewPKCE = %q, %q, %v", v, ch, err)
	}
}
