// Package gsheets exports leads to a Google Sheet an admin picks.
//
// It signs in with OAuth 2.0 (authorization code + PKCE) and asks only for
// drive.file: access to the spreadsheets the admin creates through the CRM or
// picks with Google's file picker, and to no other file in their Drive. The
// refresh token is kept sealed in the database; access tokens live in memory
// for their hour and are never stored or logged.
package gsheets

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	mrand "math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"crm-lead-management/backend/internal/config"
)

// Scopes are what the CRM asks Google for. drive.file is per file: the CRM
// can open only spreadsheets it created or the admin picked for it.
var Scopes = []string{
	"openid",
	"email",
	"https://www.googleapis.com/auth/drive.file",
}

// DriveFileScope must be among the granted scopes; Google lets people untick it.
const DriveFileScope = "https://www.googleapis.com/auth/drive.file"

// Errors worth telling apart.
var (
	// ErrRevoked: Google no longer accepts the refresh token (access was
	// removed in the Google account, or the token expired). Connect again.
	ErrRevoked = errors.New("Google access was removed or has expired. Connect the Google account again.")
	// ErrNoAccess: the spreadsheet is gone, or the CRM was never given it.
	ErrNoAccess = errors.New("The CRM cannot open this spreadsheet. It may have been deleted, or it was not chosen through the CRM. Choose it again in Configure.")
)

// APIError is an error answer from Google, with what it said.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("Google answered HTTP %d", e.Status)
	}
	return fmt.Sprintf("Google answered HTTP %d: %s", e.Status, e.Message)
}

// retryable reports whether trying again later may work: rate limits and
// Google's own temporary trouble.
func retryable(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusInternalServerError ||
		status == http.StatusBadGateway || status == http.StatusServiceUnavailable ||
		status == http.StatusGatewayTimeout
}

// Client talks to Google's OAuth, Sheets and Drive endpoints.
type Client struct {
	cfg  config.GoogleSheetsConfig
	http *http.Client
	// MaxAttempts and BaseDelay shape the retry backoff; tests shrink them.
	MaxAttempts int
	BaseDelay   time.Duration
}

// NewClient returns a Client for the configured OAuth app.
func NewClient(cfg config.GoogleSheetsConfig) *Client {
	return &Client{cfg: cfg, http: &http.Client{Timeout: 30 * time.Second}, MaxAttempts: 5, BaseDelay: time.Second}
}

// --- OAuth -------------------------------------------------------------------

// NewPKCE returns a code verifier and its S256 challenge.
func NewPKCE() (verifier, challenge string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("make PKCE verifier: %w", err)
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

// AuthURL is Google's consent page for this state and PKCE challenge.
// access_type=offline with prompt=consent makes Google return a refresh token
// every time, so reconnecting always leaves a working one.
func (c *Client) AuthURL(state, challenge, loginHint string) string {
	q := url.Values{
		"client_id":              {c.cfg.ClientID},
		"redirect_uri":           {c.cfg.RedirectURL},
		"response_type":          {"code"},
		"scope":                  {strings.Join(Scopes, " ")},
		"state":                  {state},
		"code_challenge":         {challenge},
		"code_challenge_method":  {"S256"},
		"access_type":            {"offline"},
		"prompt":                 {"consent"},
		"include_granted_scopes": {"false"},
	}
	if loginHint != "" {
		q.Set("login_hint", loginHint)
	}
	return c.cfg.AuthURL + "?" + q.Encode()
}

// Token is Google's token response.
type Token struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
}

// HasScope reports whether Google granted scope.
func (t Token) HasScope(scope string) bool {
	for _, s := range strings.Fields(t.Scope) {
		if s == scope {
			return true
		}
	}
	return false
}

// Exchange trades the code Google sent back for tokens.
func (c *Client) Exchange(ctx context.Context, code, verifier string) (Token, error) {
	return c.token(ctx, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {c.cfg.RedirectURL},
	})
}

// Refresh gets a new access token. ErrRevoked when Google refuses the
// refresh token for good.
func (c *Client) Refresh(ctx context.Context, refreshToken string) (Token, error) {
	return c.token(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	})
}

func (c *Client) token(ctx context.Context, form url.Values) (Token, error) {
	form.Set("client_id", c.cfg.ClientID)
	form.Set("client_secret", c.cfg.ClientSecret)
	var tok Token
	err := c.do(ctx, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.TokenURL, strings.NewReader(form.Encode()))
		if err == nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		return req, err
	}, &tok)
	var apiErr *APIError
	if errors.As(err, &apiErr) && (apiErr.Message == "invalid_grant" || strings.HasPrefix(apiErr.Message, "invalid_grant")) {
		return Token{}, ErrRevoked
	}
	if errors.As(err, &apiErr) && strings.HasPrefix(apiErr.Message, "invalid_client") {
		return Token{}, errors.New("Google did not accept the OAuth client. Check GOOGLE_OAUTH_CLIENT_ID and GOOGLE_OAUTH_CLIENT_SECRET.")
	}
	return tok, err
}

// Revoke tells Google to forget the token. Best effort: a token Google has
// already dropped is not an error worth stopping a disconnect for.
func (c *Client) Revoke(ctx context.Context, token string) error {
	return c.do(ctx, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.RevokeURL,
			strings.NewReader(url.Values{"token": {token}}.Encode()))
		if err == nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		return req, err
	}, nil)
}

// Email returns the signed-in Google account's email address.
func (c *Client) Email(ctx context.Context, accessToken string) (string, error) {
	var info struct {
		Email string `json:"email"`
	}
	if err := c.get(ctx, accessToken, c.cfg.UserInfoURL, &info); err != nil {
		return "", err
	}
	return info.Email, nil
}

// --- Sheets and Drive ----------------------------------------------------------

// Spreadsheet is a spreadsheet and its tabs.
type Spreadsheet struct {
	ID   string   `json:"id"`
	Name string   `json:"name"`
	URL  string   `json:"url"`
	Tabs []string `json:"tabs"`
}

// ListSpreadsheets lists the spreadsheets the CRM may open: with drive.file,
// Drive returns only those it created or the admin picked for it.
func (c *Client) ListSpreadsheets(ctx context.Context, accessToken string) ([]Spreadsheet, error) {
	q := url.Values{
		"q":        {"mimeType='application/vnd.google-apps.spreadsheet' and trashed=false"},
		"fields":   {"files(id,name,webViewLink)"},
		"orderBy":  {"modifiedTime desc"},
		"pageSize": {"100"},
	}
	var out struct {
		Files []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Link string `json:"webViewLink"`
		} `json:"files"`
	}
	if err := c.get(ctx, accessToken, c.cfg.DriveURL+"/files?"+q.Encode(), &out); err != nil {
		return nil, err
	}
	list := make([]Spreadsheet, 0, len(out.Files))
	for _, f := range out.Files {
		list = append(list, Spreadsheet{ID: f.ID, Name: f.Name, URL: f.Link, Tabs: []string{}})
	}
	return list, nil
}

// GetSpreadsheet returns a spreadsheet's name and tabs. ErrNoAccess when the
// CRM cannot open it.
func (c *Client) GetSpreadsheet(ctx context.Context, accessToken, id string) (Spreadsheet, error) {
	var out struct {
		ID         string `json:"spreadsheetId"`
		URL        string `json:"spreadsheetUrl"`
		Properties struct {
			Title string `json:"title"`
		} `json:"properties"`
		Sheets []struct {
			Properties struct {
				Title string `json:"title"`
			} `json:"properties"`
		} `json:"sheets"`
	}
	u := c.cfg.SheetsURL + "/spreadsheets/" + url.PathEscape(id) +
		"?fields=" + url.QueryEscape("spreadsheetId,spreadsheetUrl,properties.title,sheets.properties.title")
	if err := c.get(ctx, accessToken, u, &out); err != nil {
		return Spreadsheet{}, noAccess(err)
	}
	s := Spreadsheet{ID: out.ID, Name: out.Properties.Title, URL: out.URL, Tabs: []string{}}
	for _, sh := range out.Sheets {
		s.Tabs = append(s.Tabs, sh.Properties.Title)
	}
	return s, nil
}

// CreateSpreadsheet makes a new spreadsheet with one tab.
func (c *Client) CreateSpreadsheet(ctx context.Context, accessToken, name, tab string) (Spreadsheet, error) {
	body := map[string]any{
		"properties": map[string]any{"title": name},
		"sheets":     []any{map[string]any{"properties": map[string]any{"title": tab}}},
	}
	var out struct {
		ID         string `json:"spreadsheetId"`
		URL        string `json:"spreadsheetUrl"`
		Properties struct {
			Title string `json:"title"`
		} `json:"properties"`
	}
	if err := c.send(ctx, accessToken, http.MethodPost, c.cfg.SheetsURL+"/spreadsheets", body, &out); err != nil {
		return Spreadsheet{}, err
	}
	return Spreadsheet{ID: out.ID, Name: out.Properties.Title, URL: out.URL, Tabs: []string{tab}}, nil
}

// AddTab adds a tab to a spreadsheet.
func (c *Client) AddTab(ctx context.Context, accessToken, id, tab string) error {
	body := map[string]any{"requests": []any{
		map[string]any{"addSheet": map[string]any{"properties": map[string]any{"title": tab}}},
	}}
	err := c.send(ctx, accessToken, http.MethodPost,
		c.cfg.SheetsURL+"/spreadsheets/"+url.PathEscape(id)+":batchUpdate", body, nil)
	return noAccess(err)
}

// A1 quotes a tab name for A1 notation: 'Leads 2026'!A1.
func A1(tab, cells string) string {
	return "'" + strings.ReplaceAll(tab, "'", "''") + "'!" + cells
}

// GetValues reads a range. Rows and cells past the last value are left out
// by Google, so rows can be shorter than the range.
func (c *Client) GetValues(ctx context.Context, accessToken, id, rng string) ([][]string, error) {
	var out struct {
		Values [][]any `json:"values"`
	}
	u := c.cfg.SheetsURL + "/spreadsheets/" + url.PathEscape(id) + "/values/" + url.PathEscape(rng) +
		"?valueRenderOption=FORMATTED_VALUE"
	if err := c.get(ctx, accessToken, u, &out); err != nil {
		return nil, noAccess(err)
	}
	rows := make([][]string, len(out.Values))
	for i, r := range out.Values {
		rows[i] = make([]string, len(r))
		for j, v := range r {
			rows[i][j] = fmt.Sprint(v)
		}
	}
	return rows, nil
}

// RangeValues is one range and what to write there.
type RangeValues struct {
	Range  string     `json:"range"`
	Values [][]string `json:"values"`
}

// BatchUpdate writes several ranges at once. RAW: values are stored as typed,
// never parsed as formulas.
func (c *Client) BatchUpdate(ctx context.Context, accessToken, id string, data []RangeValues) error {
	body := map[string]any{"valueInputOption": "RAW", "data": data}
	err := c.send(ctx, accessToken, http.MethodPost,
		c.cfg.SheetsURL+"/spreadsheets/"+url.PathEscape(id)+"/values:batchUpdate", body, nil)
	return noAccess(err)
}

// Append adds rows below the table on the tab. INSERT_ROWS makes room for
// them, so nothing already in the sheet is written over.
func (c *Client) Append(ctx context.Context, accessToken, id, tab string, rows [][]string) error {
	q := url.Values{"valueInputOption": {"RAW"}, "insertDataOption": {"INSERT_ROWS"}}
	u := c.cfg.SheetsURL + "/spreadsheets/" + url.PathEscape(id) + "/values/" +
		url.PathEscape(A1(tab, "A1")) + ":append?" + q.Encode()
	err := c.send(ctx, accessToken, http.MethodPost, u, map[string]any{"values": rows}, nil)
	return noAccess(err)
}

// noAccess turns "not found" and "forbidden" into ErrNoAccess.
func noAccess(err error) error {
	var apiErr *APIError
	if errors.As(err, &apiErr) && (apiErr.Status == http.StatusNotFound || apiErr.Status == http.StatusForbidden) {
		return fmt.Errorf("%w (%s)", ErrNoAccess, apiErr.Message)
	}
	return err
}

// --- HTTP ----------------------------------------------------------------------

func (c *Client) get(ctx context.Context, accessToken, u string, out any) error {
	return c.do(ctx, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err == nil {
			req.Header.Set("Authorization", "Bearer "+accessToken)
		}
		return req, err
	}, out)
}

func (c *Client) send(ctx context.Context, accessToken, method, u string, body, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}
	return c.do(ctx, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(raw))
		if err == nil {
			req.Header.Set("Authorization", "Bearer "+accessToken)
			req.Header.Set("Content-Type", "application/json")
		}
		return req, err
	}, out)
}

// do sends a request, retrying rate limits and temporary errors with
// exponential backoff and jitter, honouring Retry-After. The request is built
// afresh for each attempt so its body can be read again.
func (c *Client) do(ctx context.Context, build func() (*http.Request, error), out any) error {
	attempts := max(c.MaxAttempts, 1)
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			wait := c.backoff(attempt, lastErr)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
		}
		req, err := build()
		if err != nil {
			return fmt.Errorf("build request: %w", err)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// The URL can hold a spreadsheet id but never a token; still,
			// report the cause without the request.
			lastErr = &retryAfter{err: fmt.Errorf("could not reach Google: %w", errors.Unwrap(err))}
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if out != nil && len(body) > 0 {
				if err := json.Unmarshal(body, out); err != nil {
					return fmt.Errorf("read Google's answer: %w", err)
				}
			}
			return nil
		}
		apiErr := &APIError{Status: resp.StatusCode, Message: googleMessage(body)}
		if !retryable(resp.StatusCode) {
			return apiErr
		}
		lastErr = &retryAfter{err: apiErr, after: parseRetryAfter(resp.Header.Get("Retry-After"))}
	}
	if ra, ok := lastErr.(*retryAfter); ok {
		return fmt.Errorf("gave up after %d tries: %w", attempts, ra.err)
	}
	return lastErr
}

type retryAfter struct {
	err   error
	after time.Duration
}

func (r *retryAfter) Error() string { return r.err.Error() }
func (r *retryAfter) Unwrap() error { return r.err }

func (c *Client) backoff(attempt int, last error) time.Duration {
	if ra, ok := last.(*retryAfter); ok && ra.after > 0 {
		return min(ra.after, time.Minute)
	}
	d := time.Duration(float64(c.BaseDelay) * math.Pow(2, float64(attempt-1)))
	return d + time.Duration(mrand.Int64N(int64(c.BaseDelay)/2+1))
}

func parseRetryAfter(v string) time.Duration {
	if secs, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	return 0
}

// googleMessage digs the readable part out of Google's error body: the API
// style {"error": {"message": ...}} or OAuth's {"error": "invalid_grant"}.
func googleMessage(body []byte) string {
	var apiStyle struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &apiStyle) == nil && apiStyle.Error.Message != "" {
		return apiStyle.Error.Message
	}
	var oauthStyle struct {
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if json.Unmarshal(body, &oauthStyle) == nil && oauthStyle.Error != "" {
		if oauthStyle.Description != "" {
			return oauthStyle.Error + ": " + oauthStyle.Description
		}
		return oauthStyle.Error
	}
	return ""
}
