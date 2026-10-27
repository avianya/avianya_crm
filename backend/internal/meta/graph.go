// Package meta talks to Meta's Graph API: reading lead form submissions
// (Lead Ads, with a Page access token holding leads_retrieval) and sending
// conversion events (the Conversions API, with a token for the dataset).
//
// Those are two different tokens with different permissions. A Conversions
// API token cannot read leads, and a Page token cannot send events, which is
// why the settings page asks for both.
//
// Every failure comes back as an *Error that says what kind it is, so callers
// can decide whether trying again later could help (a rate limit, Meta being
// down) or a person has to fix something first (a revoked token, a missing
// permission). Access tokens never appear in an error.
package meta

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// maxResponseBytes bounds a Graph API answer; real ones are a few kilobytes.
const maxResponseBytes = 4 << 20

// ErrorKind sorts a Graph API failure by what can be done about it.
type ErrorKind string

const (
	// KindAuth: the token is invalid, expired or revoked. Replace it.
	KindAuth ErrorKind = "auth"
	// KindPermission: the token is fine but lacks a permission, or the app
	// has no access to this object (e.g. Leads Access Manager). Grant it.
	KindPermission ErrorKind = "permission"
	// KindRateLimit: too many calls. Wait, then try again.
	KindRateLimit ErrorKind = "rate_limit"
	// KindInvalid: Meta understood the request and refused it: a wrong id, a
	// malformed event. Trying again unchanged will fail the same way.
	KindInvalid ErrorKind = "invalid"
	// KindTransient: Meta was unreachable or had a server error. Try again.
	KindTransient ErrorKind = "transient"
)

// Error is a failed Graph API call.
type Error struct {
	Kind       ErrorKind
	HTTPStatus int    // 0 when Meta could not be reached
	Code       int    // Meta's error code, e.g. 190
	Subcode    int    // Meta's error_subcode, when given
	Message    string // Meta's own message, or what went wrong reaching it
	FBTraceID  string // quote this to Meta support
	// RetryAfter is how long Meta asked us to wait, when it said; 0 otherwise.
	RetryAfter time.Duration
}

func (e *Error) Error() string {
	var b strings.Builder
	switch e.Kind {
	case KindAuth:
		b.WriteString("Meta refused the access token (expired, revoked or wrong)")
	case KindPermission:
		b.WriteString("The access token is missing a permission or access")
	case KindRateLimit:
		b.WriteString("Meta's rate limit was reached")
	case KindTransient:
		b.WriteString("Meta could not be reached or had an error")
	default:
		b.WriteString("Meta refused the request")
	}
	if e.Message != "" {
		b.WriteString(": ")
		b.WriteString(e.Message)
	}
	if e.Code != 0 {
		fmt.Fprintf(&b, " (code %d", e.Code)
		if e.Subcode != 0 {
			fmt.Fprintf(&b, ", subcode %d", e.Subcode)
		}
		b.WriteString(")")
	}
	return b.String()
}

// Retryable reports whether the same call could succeed later on its own.
func (e *Error) Retryable() bool { return e.Kind == KindRateLimit || e.Kind == KindTransient }

// AsError returns err as an *Error, if it is one.
func AsError(err error) (*Error, bool) {
	var e *Error
	ok := errors.As(err, &e)
	return e, ok
}

// classify sorts Meta's error codes. The codes are from Meta's Graph API error
// reference: 190 and 102 are token problems; 10 and 200-299 are permissions;
// 4, 17, 32, 613 and 80000-80014 are rate limits; 1 and 2 are temporary.
func classify(httpStatus, code int) ErrorKind {
	switch {
	case code == 190 || code == 102 || code == 463 || code == 467:
		return KindAuth
	case code == 10 || (code >= 200 && code <= 299):
		return KindPermission
	case code == 4 || code == 17 || code == 32 || code == 613 || (code >= 80000 && code <= 80014):
		return KindRateLimit
	case code == 1 || code == 2:
		return KindTransient
	case httpStatus == http.StatusTooManyRequests:
		return KindRateLimit
	case httpStatus == http.StatusUnauthorized:
		return KindAuth
	case httpStatus == http.StatusForbidden:
		return KindPermission
	case httpStatus >= 500:
		return KindTransient
	default:
		return KindInvalid
	}
}

// Client calls the Graph API. BaseURL is e.g. https://graph.facebook.com/v23.0
// (tests point it at a fake server).
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// NewClient returns a client with a sensible timeout as a backstop; each call
// should also carry its own context deadline.
func NewClient(baseURL string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), HTTP: &http.Client{Timeout: 30 * time.Second}}
}

// get calls GET {BaseURL}/{path}?{params}&access_token=... into out.
func (c *Client) get(ctx context.Context, path, token string, params url.Values, out any) error {
	if params == nil {
		params = url.Values{}
	}
	params.Set("access_token", token)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/"+strings.TrimLeft(path, "/")+"?"+params.Encode(), nil)
	if err != nil {
		return &Error{Kind: KindInvalid, Message: "the request could not be built"}
	}
	return c.do(req, out)
}

// post calls POST {BaseURL}/{path} with a JSON body. The token goes in the
// body, not the URL, so no proxy log in between can record it.
func (c *Client) post(ctx context.Context, path, token string, body map[string]any, out any) error {
	body["access_token"] = token
	raw, err := json.Marshal(body)
	if err != nil {
		return &Error{Kind: KindInvalid, Message: "the request could not be encoded"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/"+strings.TrimLeft(path, "/"), bytes.NewReader(raw))
	if err != nil {
		return &Error{Kind: KindInvalid, Message: "the request could not be built"}
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, out)
}

func (c *Client) do(req *http.Request, out any) error {
	resp, err := c.HTTP.Do(req)
	if err != nil {
		// The URL can hold the access token; report the cause without it.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		if errors.Is(err, context.Canceled) {
			return &Error{Kind: KindTransient, Message: "the call was cancelled"}
		}
		return &Error{Kind: KindTransient, Message: fmt.Sprintf("could not reach the Graph API (%v)", err)}
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return &Error{Kind: KindTransient, HTTPStatus: resp.StatusCode, Message: "the answer could not be read"}
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var body struct {
			Error struct {
				Message   string `json:"message"`
				Code      int    `json:"code"`
				Subcode   int    `json:"error_subcode"`
				UserMsg   string `json:"error_user_msg"`
				FBTraceID string `json:"fbtrace_id"`
			} `json:"error"`
		}
		_ = json.Unmarshal(raw, &body)
		e := &Error{
			Kind:       classify(resp.StatusCode, body.Error.Code),
			HTTPStatus: resp.StatusCode,
			Code:       body.Error.Code,
			Subcode:    body.Error.Subcode,
			Message:    firstNonEmpty(body.Error.UserMsg, body.Error.Message),
			FBTraceID:  body.Error.FBTraceID,
		}
		if e.Message == "" {
			e.Message = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		if secs, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && secs > 0 {
			e.RetryAfter = time.Duration(secs) * time.Second
		}
		return e
	}

	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return &Error{Kind: KindInvalid, HTTPStatus: resp.StatusCode, Message: fmt.Sprintf("the answer was not understood (%v)", err)}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Leads
// ---------------------------------------------------------------------------

// LeadFields are the fields read for each lead. Campaign and ad names need the
// token's app to have ads_read on the ad account; the ids do not.
const LeadFields = "id,created_time,form_id,ad_id,ad_name,adset_id,adset_name,campaign_id,campaign_name,is_organic,field_data"

// Lead is one lead form submission as the Graph API returns it.
type Lead struct {
	ID           string `json:"id"`
	CreatedTime  string `json:"created_time"` // e.g. 2026-10-20T09:15:02+0000
	FormID       string `json:"form_id"`
	AdID         string `json:"ad_id"`
	AdName       string `json:"ad_name"`
	AdsetID      string `json:"adset_id"`
	AdsetName    string `json:"adset_name"`
	CampaignID   string `json:"campaign_id"`
	CampaignName string `json:"campaign_name"`
	IsOrganic    bool   `json:"is_organic"`
	FieldData    []struct {
		Name   string   `json:"name"`
		Values []string `json:"values"`
	} `json:"field_data"`
}

// Created parses CreatedTime; the zero time when Meta gave none.
func (l Lead) Created() time.Time {
	for _, layout := range []string{"2006-01-02T15:04:05-0700", time.RFC3339} {
		if t, err := time.Parse(layout, l.CreatedTime); err == nil {
			return t
		}
	}
	return time.Time{}
}

// GetLead reads one lead by its leadgen id.
func (c *Client) GetLead(ctx context.Context, pageToken, leadgenID string) (Lead, error) {
	var lead Lead
	err := c.get(ctx, url.PathEscape(leadgenID), pageToken, url.Values{"fields": {LeadFields}}, &lead)
	return lead, err
}

// FormLeadsPage is one page of a form's leads.
type FormLeadsPage struct {
	Leads []Lead
	// After is the cursor for the next page; "" on the last one.
	After string
}

// FormLeads reads one page of a form's leads, newest first, submitted after
// since (all of them for the zero time). after continues from a cursor.
func (c *Client) FormLeads(ctx context.Context, pageToken, formID string, since time.Time, after string, limit int) (FormLeadsPage, error) {
	params := url.Values{"fields": {LeadFields}, "limit": {strconv.Itoa(limit)}}
	if !since.IsZero() {
		filter, _ := json.Marshal([]map[string]any{{
			"field": "time_created", "operator": "GREATER_THAN", "value": since.Unix(),
		}})
		params.Set("filtering", string(filter))
	}
	if after != "" {
		params.Set("after", after)
	}
	var body struct {
		Data   []Lead `json:"data"`
		Paging struct {
			Cursors struct {
				After string `json:"after"`
			} `json:"cursors"`
			Next string `json:"next"`
		} `json:"paging"`
	}
	if err := c.get(ctx, url.PathEscape(formID)+"/leads", pageToken, params, &body); err != nil {
		return FormLeadsPage{}, err
	}
	page := FormLeadsPage{Leads: body.Data}
	// Meta leaves the cursor in place on the last page; "next" is what says
	// there is more.
	if body.Paging.Next != "" {
		page.After = body.Paging.Cursors.After
	}
	return page, nil
}

// Object reads a few fields of any Graph object, to check it exists and the
// token may see it. Used by Test Connection.
func (c *Client) Object(ctx context.Context, token, id, fields string) (map[string]any, error) {
	var out map[string]any
	err := c.get(ctx, url.PathEscape(id), token, url.Values{"fields": {fields}}, &out)
	return out, err
}

// CheckAppSecret reads the token's own object (/me) with appsecret_proof, the
// HMAC of the token keyed by the app secret. Meta refuses the call when the
// secret is wrong, so a success proves the app secret matches the app the
// token was made with.
func (c *Client) CheckAppSecret(ctx context.Context, token, appSecret string) (map[string]any, error) {
	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write([]byte(token))
	var out map[string]any
	err := c.get(ctx, "me", token, url.Values{
		"fields":          {"id,name"},
		"appsecret_proof": {hex.EncodeToString(mac.Sum(nil))},
	}, &out)
	return out, err
}

// ---------------------------------------------------------------------------
// Conversions API
// ---------------------------------------------------------------------------

// ServerEvent is one event for the Conversions API, in the shape Meta's CRM
// integration ("conversion leads") expects: action_source system_generated,
// the lead's Meta id in user_data, and the CRM named in custom_data.
type ServerEvent struct {
	EventName    string         `json:"event_name"`
	EventTime    int64          `json:"event_time"` // unix seconds
	EventID      string         `json:"event_id"`   // lets Meta drop a duplicate
	ActionSource string         `json:"action_source"`
	UserData     map[string]any `json:"user_data"`
	CustomData   map[string]any `json:"custom_data"`
}

// SendResult is Meta's answer to a successful send.
type SendResult struct {
	EventsReceived int    `json:"events_received"`
	FBTraceID      string `json:"fbtrace_id"`
}

// SendEvents posts events to a dataset (pixel). testEventCode, when not
// empty, routes them to Events Manager's Test Events tab only.
func (c *Client) SendEvents(ctx context.Context, capiToken, datasetID string, events []ServerEvent, testEventCode string) (SendResult, error) {
	body := map[string]any{"data": events}
	if testEventCode != "" {
		body["test_event_code"] = testEventCode
	}
	var result SendResult
	err := c.post(ctx, url.PathEscape(datasetID)+"/events", capiToken, body, &result)
	return result, err
}
