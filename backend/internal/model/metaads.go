package model

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

// This file is the Meta Ads integration's data: its settings, where each Meta
// lead came from, the conversion events sent back to Meta, and the Meta Leads
// dashboard's filters and totals.

// ---------------------------------------------------------------------------
// Settings
// ---------------------------------------------------------------------------

// MetaConfig is the non-secret part of the settings, stored as JSON.
type MetaConfig struct {
	// DatasetID is the Pixel / dataset that conversion events go to.
	DatasetID string `json:"dataset_id"`
	// AppID is the Meta app the Page token and webhook belong to. Not a
	// secret; Test Connection checks the token was issued by this app.
	AppID string `json:"app_id"`
	// AdAccountID (optional, an advanced setting) is the ad account's number,
	// without "act_". No API call needs it: it is stamped on each imported
	// lead's attribution for the dashboard's ad account filter. Saved
	// settings from before App ID existed keep theirs.
	AdAccountID string `json:"ad_account_id"`
	// PageID is the Facebook Page whose lead forms are read; webhooks for
	// other Pages are ignored once it is set.
	PageID string `json:"page_id"`
	// FormIDs are the lead forms Sync Now reads. Webhooks take leads from
	// every form on the Page, listed or not.
	FormIDs []string `json:"form_ids"`

	// CAPIEnabled turns conversion feedback on.
	CAPIEnabled bool `json:"capi_enabled"`
	// TestMode sends every event with TestEventCode, so it appears only in
	// Events Manager's Test Events tab. On until a connection test passes.
	TestMode      bool   `json:"test_mode"`
	TestEventCode string `json:"test_event_code"`
	// EventMapping says which stages are worth telling Meta about, and the
	// event name for each. A stage not listed sends nothing.
	EventMapping []StageEvent `json:"event_mapping"`
	// CRMName is sent as custom_data.lead_event_source.
	CRMName string `json:"crm_name"`
	// PhoneCountryCode (digits, e.g. "91") is used for phone numbers written
	// without one; empty leaves such numbers out of the events.
	PhoneCountryCode string `json:"phone_country_code"`
}

// StageEvent maps one stage to the conversion event it sends.
//
// Stage New stands for "lead created": Meta's conversion leads integration
// expects every stage, the raw lead included. Disabled turns one mapping off
// without forgetting its name; it is "disabled" rather than "enabled" so that
// mappings saved before the switch existed stay on.
type StageEvent struct {
	Stage     Stage  `json:"stage"`
	EventName string `json:"event_name"`
	Disabled  bool   `json:"disabled,omitempty"`
}

// DefaultMetaConfig is what a fresh install starts with: conversion feedback
// off, test mode on, and the two stages that say the most about a lead.
func DefaultMetaConfig() MetaConfig {
	return MetaConfig{
		FormIDs:  []string{},
		TestMode: true,
		CRMName:  "Pipeline CRM",
		// Event names are free-form stage names in Meta's CRM integration;
		// "Lead" for the raw lead follows Meta's own example payload.
		EventMapping: []StageEvent{
			{Stage: StageNew, EventName: "Lead"},
			{Stage: StageQualified, EventName: "Qualified Lead"},
			{Stage: StageWon, EventName: "Converted"},
		},
	}
}

// EventFor is the event name a stage sends, if it sends one.
func (c MetaConfig) EventFor(stage Stage) (string, bool) {
	for _, m := range c.EventMapping {
		if m.Stage == stage && !m.Disabled {
			return m.EventName, true
		}
	}
	return "", false
}

// EventsReaching is every enabled mapping a lead entering stage must have
// sent, in lifecycle order: the stage's own, and those of the stages before
// it, because Meta expects all earlier stages before a later one ("if a lead
// reaches Converted, all previous stages should have been sent"). Lost is off
// the main path: it brings only its own event.
func (c MetaConfig) EventsReaching(stage Stage) []StageEvent {
	if stage == StageLost {
		if name, ok := c.EventFor(StageLost); ok {
			return []StageEvent{{Stage: StageLost, EventName: name}}
		}
		return nil
	}
	var out []StageEvent
	for _, s := range Stages {
		if s == StageLost {
			continue
		}
		if name, ok := c.EventFor(s); ok {
			out = append(out, StageEvent{Stage: s, EventName: name})
		}
		if s == stage {
			break
		}
	}
	return out
}

// MetaSecrets are the credentials. They are sealed before they are stored,
// and never sent back to the browser or written to a log.
type MetaSecrets struct {
	// CAPIToken sends conversion events (a system user token for the dataset).
	CAPIToken string `json:"capi_token"`
	// PageAccessToken reads lead answers; needs leads_retrieval.
	PageAccessToken string `json:"page_access_token"`
	// AppSecret checks webhook signatures.
	AppSecret string `json:"app_secret"`
	// VerifyToken answers Meta's webhook subscription check.
	VerifyToken string `json:"verify_token"`
}

// SecretNames are MetaSecrets' JSON names, in the order the page shows them.
var SecretNames = []string{"capi_token", "page_access_token", "app_secret", "verify_token"}

// Get returns one secret by its JSON name.
func (s MetaSecrets) Get(name string) string {
	switch name {
	case "capi_token":
		return s.CAPIToken
	case "page_access_token":
		return s.PageAccessToken
	case "app_secret":
		return s.AppSecret
	case "verify_token":
		return s.VerifyToken
	}
	return ""
}

// Merge returns s with every non-empty secret in replace swapped in: a blank
// field in the form means "keep the saved one".
func (s MetaSecrets) Merge(replace MetaSecrets) MetaSecrets {
	pick := func(old, new string) string {
		if new != "" {
			return new
		}
		return old
	}
	return MetaSecrets{
		CAPIToken:       pick(s.CAPIToken, replace.CAPIToken),
		PageAccessToken: pick(s.PageAccessToken, replace.PageAccessToken),
		AppSecret:       pick(s.AppSecret, replace.AppSecret),
		VerifyToken:     pick(s.VerifyToken, replace.VerifyToken),
	}
}

// Empty reports whether no secret is set.
func (s MetaSecrets) Empty() bool { return s == MetaSecrets{} }

// MetaSettingsInput is PUT /api/meta/settings.
type MetaSettingsInput struct {
	Config MetaConfig `json:"config"`
	// Secrets: a non-empty field replaces the saved one; empty keeps it.
	Secrets MetaSecrets `json:"secrets"`
}

var (
	metaIDPattern     = regexp.MustCompile(`^[0-9]{5,25}$`)
	eventNamePattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _-]{0,49}$`)
	testCodePattern   = regexp.MustCompile(`^[A-Za-z0-9]{1,40}$`)
	countryCodeRegexp = regexp.MustCompile(`^[0-9]{1,4}$`)
)

// MaxMetaForms bounds how many forms Sync Now reads.
const MaxMetaForms = 20

// Normalize tidies the input: trims, drops "act_", de-duplicates form ids.
func (in *MetaSettingsInput) Normalize() {
	c := &in.Config
	c.DatasetID = strings.TrimSpace(c.DatasetID)
	c.AppID = strings.TrimSpace(c.AppID)
	c.AdAccountID = strings.TrimPrefix(strings.TrimSpace(c.AdAccountID), "act_")
	c.PageID = strings.TrimSpace(c.PageID)
	c.TestEventCode = strings.TrimSpace(c.TestEventCode)
	c.CRMName = strings.TrimSpace(c.CRMName)
	c.PhoneCountryCode = strings.TrimLeft(strings.TrimSpace(c.PhoneCountryCode), "+")

	forms := []string{}
	for _, f := range c.FormIDs {
		if f = strings.TrimSpace(f); f != "" && !slices.Contains(forms, f) {
			forms = append(forms, f)
		}
	}
	c.FormIDs = forms

	for i := range c.EventMapping {
		c.EventMapping[i].EventName = strings.TrimSpace(c.EventMapping[i].EventName)
	}
	if c.EventMapping == nil {
		c.EventMapping = []StageEvent{}
	}

	s := &in.Secrets
	for _, p := range []*string{&s.CAPIToken, &s.PageAccessToken, &s.AppSecret, &s.VerifyToken} {
		*p = strings.TrimSpace(*p)
	}
}

// Validate checks the settings, keyed by JSON path ("config.page_id").
func (in MetaSettingsInput) Validate() ValidationErrors {
	errs := ValidationErrors{}
	c := in.Config
	checkID := func(key, value string) {
		if value != "" && !metaIDPattern.MatchString(value) {
			errs[key] = "must be the numeric id Meta shows (5-25 digits)"
		}
	}
	checkID("config.dataset_id", c.DatasetID)
	checkID("config.app_id", c.AppID)
	checkID("config.ad_account_id", c.AdAccountID)
	checkID("config.page_id", c.PageID)
	if len(c.FormIDs) > MaxMetaForms {
		errs["config.form_ids"] = fmt.Sprintf("at most %d forms", MaxMetaForms)
	}
	for _, f := range c.FormIDs {
		if !metaIDPattern.MatchString(f) {
			errs["config.form_ids"] = fmt.Sprintf("%q is not a numeric form id", f)
			break
		}
	}

	if c.CAPIEnabled && c.DatasetID == "" {
		errs["config.dataset_id"] = "is required to send conversion events"
	}
	if c.TestEventCode != "" && !testCodePattern.MatchString(c.TestEventCode) {
		errs["config.test_event_code"] = "must be the code from Events Manager > Test Events (letters and digits)"
	}
	// Test mode without a code would send real events: refuse it.
	if c.CAPIEnabled && c.TestMode && c.TestEventCode == "" {
		errs["config.test_event_code"] = "is required while test mode is on"
	}
	if len([]rune(c.CRMName)) > 60 {
		errs["config.crm_name"] = "must be at most 60 characters"
	}
	if c.PhoneCountryCode != "" && !countryCodeRegexp.MatchString(c.PhoneCountryCode) {
		errs["config.phone_country_code"] = "must be 1-4 digits, e.g. 91"
	}

	seenStage := map[Stage]bool{}
	seenName := map[string]bool{}
	for i, m := range c.EventMapping {
		at := func(f string) string { return fmt.Sprintf("config.event_mapping[%d].%s", i, f) }
		switch {
		case !m.Stage.Valid():
			errs[at("stage")] = "is not a known stage"
		case seenStage[m.Stage]:
			errs[at("stage")] = "is mapped twice"
		}
		seenStage[m.Stage] = true
		key := strings.ToLower(m.EventName)
		switch {
		case !eventNamePattern.MatchString(m.EventName):
			errs[at("event_name")] = "must be 1-50 letters, digits, spaces, _ or -"
		case seenName[key]:
			errs[at("event_name")] = "is used for another stage"
		}
		seenName[key] = true
	}

	for _, name := range SecretNames {
		v := in.Secrets.Get(name)
		if len(v) > 2000 || strings.ContainsAny(v, " \t\r\n") {
			errs["secrets."+name] = "must be a single token with no spaces"
		}
	}
	return errs
}

// MetaStatus is the connection status shown on the settings page.
type MetaStatus string

const (
	MetaNotConnected MetaStatus = "not_connected"
	MetaConnected    MetaStatus = "connected"
	MetaError        MetaStatus = "error"
)

// MetaSettingsRecord is the integration_settings row, with secrets opened.
type MetaSettingsRecord struct {
	Config  MetaConfig
	Secrets MetaSecrets
	// SecretsLocked: credentials are saved but could not be decrypted
	// (SECRETS_ENCRYPTION_KEY missing or changed). Secrets is then empty.
	SecretsLocked bool
	// SecretHints are the saved credentials' masked forms, by JSON name.
	SecretHints   map[string]string
	Status        MetaStatus
	LastTestedAt  *time.Time
	LastSyncAt    *time.Time
	LastSyncCount *int
	LastError     string
	LastErrorAt   *time.Time
	UpdatedAt     *time.Time

	// The Conversions API's own health (see CAPIHealth); "" until something
	// was sent or tested.
	CAPIHealth         CAPIHealth
	CAPIHealthDetail   string
	CAPIHealthAt       *time.Time
	CAPITokenRotatedAt *time.Time
}

// CAPIHealth is the Conversions API's status, as the settings page shows it.
// The first four are stored from what Meta last answered; the last two are
// worked out from the settings.
type CAPIHealth string

const (
	CAPIConnected              CAPIHealth = "connected"
	CAPIInvalidCredentials     CAPIHealth = "invalid_credentials"
	CAPIPermissionDenied       CAPIHealth = "permission_denied"
	CAPITemporarilyUnavailable CAPIHealth = "temporarily_unavailable"
	CAPINotConfigured          CAPIHealth = "not_configured"
	CAPIDisabled               CAPIHealth = "disabled"
)

// SecretState says whether one credential is set, and where it comes from,
// without revealing it.
type SecretState struct {
	Set bool `json:"set"`
	// Hint is the masked form, e.g. "••••••••1234"; "" when not set.
	Hint string `json:"hint"`
	// Source is "saved" (this page), "env" (backend/.env) or "".
	Source string `json:"source"`
}

// MetaSettingsView is GET /api/meta/settings. It carries no secret.
type MetaSettingsView struct {
	Config        MetaConfig             `json:"config"`
	Secrets       map[string]SecretState `json:"secrets"`
	Status        MetaStatus             `json:"status"`
	LastTestedAt  *time.Time             `json:"last_tested_at"`
	LastSyncAt    *time.Time             `json:"last_sync_at"`
	LastSyncCount *int                   `json:"last_sync_count"`
	LastError     string                 `json:"last_error"`
	LastErrorAt   *time.Time             `json:"last_error_at"`
	WebhookURL    string                 `json:"webhook_url"`
	// EncryptionReady is false until SECRETS_ENCRYPTION_KEY is set, which
	// saving a credential needs.
	EncryptionReady bool `json:"encryption_ready"`
	// SecretsLocked: saved credentials cannot be decrypted with the current
	// key; replace them or disconnect.
	SecretsLocked bool `json:"secrets_locked"`
	// LeadsReady / CAPIReady: everything each half needs is filled in.
	LeadsReady bool     `json:"leads_ready"`
	CAPIReady  bool     `json:"capi_ready"`
	Missing    []string `json:"missing"`
}

// ConnectionCheck is one line of a Test Connection result.
type ConnectionCheck struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	OK      bool   `json:"ok"`
	Skipped bool   `json:"skipped"` // nothing to check: the setting is empty
	Detail  string `json:"detail"`
}

// ConnectionResult is POST /api/meta/settings/test.
type ConnectionResult struct {
	OK     bool              `json:"ok"`
	Checks []ConnectionCheck `json:"checks"`
}

// SyncResult is POST /api/meta/sync.
type SyncResult struct {
	Forms    int      `json:"forms"`
	Fetched  int      `json:"fetched"`
	Created  int      `json:"created"`
	Repeated int      `json:"repeated"` // already a lead: a timeline note
	Skipped  int      `json:"skipped"`  // received before: nothing to do
	Failed   int      `json:"failed"`
	Errors   []string `json:"errors"`
	// Partial: stopped early (rate limit, page cap); the next sync continues.
	Partial bool `json:"partial"`
}

// ---------------------------------------------------------------------------
// Attribution
// ---------------------------------------------------------------------------

// MetaAttribution is where a Meta lead came from.
type MetaAttribution struct {
	LeadID        int64      `json:"lead_id"`
	MetaLeadID    string     `json:"meta_lead_id"`
	PageID        string     `json:"page_id"`
	FormID        string     `json:"form_id"`
	AdAccountID   string     `json:"ad_account_id"`
	CampaignID    string     `json:"campaign_id"`
	CampaignName  string     `json:"campaign_name"`
	AdsetID       string     `json:"adset_id"`
	AdsetName     string     `json:"adset_name"`
	AdID          string     `json:"ad_id"`
	AdName        string     `json:"ad_name"`
	IsOrganic     bool       `json:"is_organic"`
	MetaCreatedAt *time.Time `json:"meta_created_at"`
	CreatedAt     time.Time  `json:"created_at"`
}

// ---------------------------------------------------------------------------
// Conversion events
// ---------------------------------------------------------------------------

// CAPIStatus is where a conversion event is in its delivery.
type CAPIStatus string

const (
	CAPIPending        CAPIStatus = "pending"
	CAPISent           CAPIStatus = "sent"
	CAPIFailed         CAPIStatus = "failed"
	CAPIRetryScheduled CAPIStatus = "retry_scheduled"
	// CAPICancelled: must no longer be sent (the lead opted out of sharing
	// with Meta, or was confirmed spam, after the event was queued).
	CAPICancelled CAPIStatus = "cancelled"
)

// CAPIStatuses lists every status, for filters.
var CAPIStatuses = []CAPIStatus{CAPIPending, CAPISent, CAPIFailed, CAPIRetryScheduled, CAPICancelled}

// Valid reports whether s is a known status.
func (s CAPIStatus) Valid() bool { return slices.Contains(CAPIStatuses, s) }

// CAPIEvent is one capi_events row.
type CAPIEvent struct {
	ID            int64      `json:"id"`
	LeadID        int64      `json:"lead_id"`
	LeadName      string     `json:"lead_name"`
	MetaLeadID    string     `json:"meta_lead_id"`
	Stage         Stage      `json:"stage"`
	EventName     string     `json:"event_name"`
	EventID       string     `json:"event_id"`
	EventTime     time.Time  `json:"event_time"`
	TestMode      bool       `json:"test_mode"`
	Status        CAPIStatus `json:"status"`
	Attempts      int        `json:"attempts"`
	NextAttemptAt *time.Time `json:"next_attempt_at"` // only while retry_scheduled
	LastError     string     `json:"last_error"`
	// ErrorCode is Meta's code for the last failure (190, 200 ...).
	ErrorCode *int       `json:"error_code"`
	FBTraceID string     `json:"fbtrace_id"`
	SentAt    *time.Time `json:"sent_at"`
	CreatedAt time.Time  `json:"created_at"`
}

// MaxCAPIAttempts is how many times an event is tried before it is Failed
// for good (a person can still retry it).
const MaxCAPIAttempts = 6

// capiBackoff is the wait after each failed attempt: about a day in all,
// inside the seven days Meta accepts an event.
var capiBackoff = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour}

// CAPIBackoff is how long to wait after the attempts-th failed attempt, at
// least as long as Meta asked (retryAfter).
func CAPIBackoff(attempts int, retryAfter time.Duration) time.Duration {
	i := min(max(attempts-1, 0), len(capiBackoff)-1)
	return max(capiBackoff[i], retryAfter)
}

// CAPIEventID is the event_id for a lead's event: the same every time it is
// built, so a resend is recognised by Meta as the same event.
func CAPIEventID(leadID int64, eventName string, testMode bool) string {
	mode := "live"
	if testMode {
		mode = "test"
	}
	slug := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		default:
			return '-'
		}
	}, eventName)
	return fmt.Sprintf("crm-%d-%s-%s", leadID, slug, mode)
}

// CAPIEventQuery filters the delivery log.
type CAPIEventQuery struct {
	Status   CAPIStatus // "" for any
	LeadID   int64      // 0 for any
	From, To *time.Time // on when the event was queued; To is exclusive
	Limit    int
}

// ---------------------------------------------------------------------------
// Meta Leads dashboard
// ---------------------------------------------------------------------------

// MetaLeadQuery is GET /api/meta/leads's filters. Zero values mean "any".
type MetaLeadQuery struct {
	From, To    *time.Time // on the lead's Meta submission time; To is exclusive
	AdAccountID string
	PageID      string
	FormID      string
	CampaignID  string
	AdID        string
	Source      Source
	Quality     Quality
	ScoreMin    *int // on the effective quality score
	ScoreMax    *int
	Stage       Stage
	// Qualification narrows by qualification status (read from the stage).
	Qualification QualificationStatus
	Owner         OwnerFilter
	// CAPIStatus is the latest event's status, or "none" for leads that have
	// sent nothing.
	CAPIStatus string

	Page     int
	PageSize int
}

// Offset converts the 1-based page into a SQL OFFSET.
func (q MetaLeadQuery) Offset() int { return (q.Page - 1) * q.PageSize }

// MetaLeadSummary is the dashboard's cards, over every lead the filters
// match (not just the page shown).
type MetaLeadSummary struct {
	Total int `json:"total"`
	// Qualified counts leads at Qualified or any later stage, Won included:
	// a lead that went on to be won was qualified on the way.
	Qualified   int `json:"qualified"`
	Converted   int `json:"converted"`   // Won
	Unqualified int `json:"unqualified"` // Lost
	// ConversionRate is Converted / Total, in percent; null with no leads.
	ConversionRate *float64 `json:"conversion_rate"`
}

// MetaLeadRow is one row of the dashboard's list.
type MetaLeadRow struct {
	Lead        Lead            `json:"lead"`
	Attribution MetaAttribution `json:"attribution"`
	// EffectiveScore is the override if there is one, else the rules' score.
	EffectiveScore *int `json:"effective_score"`
	// CAPIStatus is the latest event's status; "" when none was sent.
	CAPIStatus CAPIStatus `json:"capi_status"`
}

// MetaFilterOption is one choice in a dashboard dropdown.
type MetaFilterOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
	Count int    `json:"count"`
}

// MetaFilterOptions are the dropdowns' choices, from the stored leads.
type MetaFilterOptions struct {
	AdAccounts []MetaFilterOption `json:"ad_accounts"`
	Pages      []MetaFilterOption `json:"pages"`
	Forms      []MetaFilterOption `json:"forms"`
	Campaigns  []MetaFilterOption `json:"campaigns"`
	Ads        []MetaFilterOption `json:"ads"`
}

// MetaLeadsReport is GET /api/meta/leads.
type MetaLeadsReport struct {
	Summary MetaLeadSummary   `json:"summary"`
	Leads   []MetaLeadRow     `json:"leads"`
	Options MetaFilterOptions `json:"options"`
}
