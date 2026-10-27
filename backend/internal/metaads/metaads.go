// Package metaads is the Meta Ads integration's behaviour: which credentials
// are in force, taking in a Meta lead, Sync Now, Test Connection, and sending
// queued conversion events to the Conversions API.
//
// Storage is the store package's; HTTP is the api package's; the Graph API
// calls are the meta package's. This package is what joins them.
package metaads

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"crm-lead-management/backend/internal/config"
	"crm-lead-management/backend/internal/meta"
	"crm-lead-management/backend/internal/model"
	"crm-lead-management/backend/internal/secrets"
	"crm-lead-management/backend/internal/store"
)

// ErrNotReady means something the action needs is not set up; its message
// says what, and is safe to show.
type ErrNotReady struct{ Reason string }

func (e *ErrNotReady) Error() string { return e.Reason }

// IsNotReady reports whether err is an ErrNotReady.
func IsNotReady(err error) bool {
	var e *ErrNotReady
	return errors.As(err, &e)
}

// Service carries what every Meta action needs.
type Service struct {
	store     *store.Store
	env       config.MetaConfig // credentials from backend/.env, the fallback
	publicURL string
	client    *meta.Client
	logger    *slog.Logger
	now       func() time.Time
	// sendTimeout bounds one Conversions API call.
	sendTimeout time.Duration
}

// New builds the service. Graph API calls go to cfg.Meta.GraphURL.
func New(st *store.Store, cfg config.Config, logger *slog.Logger) *Service {
	return &Service{
		store:     st,
		env:       cfg.Meta,
		publicURL: cfg.PublicAPIURL,
		client:    meta.NewClient(cfg.Meta.GraphURL),
		logger:    logger,
		now:       time.Now,

		sendTimeout: defaultSendTimeout,
	}
}

// ---------------------------------------------------------------------------
// Settings in force
// ---------------------------------------------------------------------------

// Runtime is the settings in force: what the settings page saved, with each
// credential it did not save taken from backend/.env, so an install set up
// before the page existed keeps working.
type Runtime struct {
	Record  model.MetaSettingsRecord
	Config  model.MetaConfig
	Secrets model.MetaSecrets
	// Sources says where each credential came from: "saved", "env" or "".
	Sources map[string]string
}

// Runtime reads the settings in force.
func (s *Service) Runtime(ctx context.Context) (Runtime, error) {
	rec, err := s.store.GetMetaSettings(ctx)
	if err != nil {
		return Runtime{}, err
	}
	env := model.MetaSecrets{PageAccessToken: s.env.PageAccessToken, AppSecret: s.env.AppSecret, VerifyToken: s.env.VerifyToken}
	rt := Runtime{Record: rec, Config: rec.Config, Sources: map[string]string{}}
	for _, name := range model.SecretNames {
		switch {
		case rec.Secrets.Get(name) != "":
			rt.Sources[name] = "saved"
		case env.Get(name) != "":
			rt.Sources[name] = "env"
		default:
			rt.Sources[name] = ""
		}
	}
	rt.Secrets = env.Merge(rec.Secrets) // saved ones win
	return rt, nil
}

// LeadsReady: leads can be received and read.
func (rt Runtime) LeadsReady() bool {
	return rt.Secrets.PageAccessToken != "" && rt.Secrets.AppSecret != "" && rt.Secrets.VerifyToken != ""
}

// CAPIReady: conversion events can be sent.
func (rt Runtime) CAPIReady() bool { return rt.Secrets.CAPIToken != "" && rt.Config.DatasetID != "" }

// Missing lists what is still to fill in, in the page's words.
func (rt Runtime) Missing() []string {
	missing := []string{}
	add := func(cond bool, what string) {
		if cond {
			missing = append(missing, what)
		}
	}
	add(rt.Secrets.PageAccessToken == "", "Page access token")
	add(rt.Secrets.AppSecret == "", "App secret")
	add(rt.Secrets.VerifyToken == "", "Webhook verify token")
	add(rt.Config.PageID == "", "Facebook Page ID")
	add(rt.Config.CAPIEnabled && rt.Config.DatasetID == "", "Pixel / Dataset ID")
	add(rt.Config.CAPIEnabled && rt.Secrets.CAPIToken == "", "Conversions API access token")
	return missing
}

// View is the settings page's data: everything but the secrets themselves.
func (s *Service) View(rt Runtime, encryptionReady bool) model.MetaSettingsView {
	view := model.MetaSettingsView{
		Config:          rt.Config,
		Secrets:         map[string]model.SecretState{},
		Status:          rt.Record.Status,
		LastTestedAt:    rt.Record.LastTestedAt,
		LastSyncAt:      rt.Record.LastSyncAt,
		LastSyncCount:   rt.Record.LastSyncCount,
		LastError:       rt.Record.LastError,
		LastErrorAt:     rt.Record.LastErrorAt,
		EncryptionReady: encryptionReady,
		SecretsLocked:   rt.Record.SecretsLocked,
		LeadsReady:      rt.LeadsReady(),
		CAPIReady:       rt.CAPIReady(),
		Missing:         rt.Missing(),
	}
	if s.publicURL != "" {
		view.WebhookURL = s.publicURL + "/api/webhooks/meta"
	}
	for _, name := range model.SecretNames {
		state := model.SecretState{Source: rt.Sources[name]}
		switch state.Source {
		case "saved":
			state.Set, state.Hint = true, rt.Record.SecretHints[name]
		case "env":
			state.Set, state.Hint = true, secrets.Mask(rt.Secrets.Get(name))
		}
		view.Secrets[name] = state
	}
	return view
}

// ---------------------------------------------------------------------------
// Taking in a lead
// ---------------------------------------------------------------------------

// leadFetchTimeout bounds reading one lead, inside the time Meta waits for a
// webhook to answer.
const leadFetchTimeout = 10 * time.Second

// Notice is what the webhook says about a lead: Meta sends only ids, and the
// answers are read from the Graph API.
type Notice struct {
	LeadgenID, FormID, AdID, PageID string
	CreatedTime                     int64 // unix seconds; 0 when not given
}

// TakeLead reads one lead's answers and takes it in, or records why it could
// not be read so it can be retried. A token problem is also noted on the
// settings page, where whoever can fix it will see it.
func (s *Service) TakeLead(ctx context.Context, rt Runtime, n Notice) (model.AdLead, error) {
	fetchCtx, cancel := context.WithTimeout(ctx, leadFetchTimeout)
	defer cancel()
	ml, err := s.client.GetLead(fetchCtx, rt.Secrets.PageAccessToken, n.LeadgenID)
	if err != nil {
		s.logger.Warn("meta lead fetch", "leadgen_id", n.LeadgenID, "error", err.Error())
		s.noteTokenProblem(ctx, err)
		sub := model.AdSubmission{Provider: model.ProviderMeta, ExternalID: n.LeadgenID, FormID: n.FormID, Ad: n.AdID}
		sub.Attribution = &model.MetaAttribution{
			MetaLeadID: n.LeadgenID, PageID: n.PageID, FormID: n.FormID, AdID: n.AdID,
			AdAccountID: rt.Config.AdAccountID, MetaCreatedAt: unixTime(n.CreatedTime),
		}
		return s.store.RecordAdLeadFailure(ctx, sub, "Its answers could not be read from Meta: "+err.Error())
	}
	return s.store.ReceiveAdLead(ctx, Submission(ml, n.PageID, rt.Config.AdAccountID))
}

// RetryLead reads a failed Meta lead again, using what was recorded of it.
func (s *Service) RetryLead(ctx context.Context, rt Runtime, record model.AdLead) (model.AdLead, error) {
	n := Notice{LeadgenID: record.ExternalID, FormID: record.FormID, AdID: record.Ad}
	if a := record.Attribution; a != nil {
		n.PageID, n.AdID = a.PageID, a.AdID
		if a.MetaCreatedAt != nil {
			n.CreatedTime = a.MetaCreatedAt.Unix()
		}
	}
	return s.TakeLead(ctx, rt, n)
}

// Submission turns a lead read from the Graph API into what the store takes
// in, attribution and all.
func Submission(ml meta.Lead, pageID, adAccountID string) model.AdSubmission {
	sub := model.AdSubmission{
		Provider:   model.ProviderMeta,
		ExternalID: ml.ID,
		FormID:     ml.FormID,
		Campaign:   firstNonEmpty(ml.CampaignName, ml.CampaignID),
		Ad:         firstNonEmpty(ml.AdName, ml.AdID),
	}
	for _, field := range ml.FieldData {
		sub.Answers = append(sub.Answers, model.AdAnswer{
			Key:   field.Name,
			Label: model.LabelFromKey(field.Name),
			Value: strings.Join(field.Values, ", "),
		})
	}
	a := &model.MetaAttribution{
		MetaLeadID: ml.ID, PageID: pageID, FormID: ml.FormID, AdAccountID: adAccountID,
		CampaignID: ml.CampaignID, CampaignName: ml.CampaignName,
		AdsetID: ml.AdsetID, AdsetName: ml.AdsetName, AdID: ml.AdID, AdName: ml.AdName,
		IsOrganic: ml.IsOrganic,
	}
	if t := ml.Created(); !t.IsZero() {
		a.MetaCreatedAt = &t
	}
	sub.Attribution = a
	return sub
}

// noteTokenProblem puts a token or permission failure on the settings page.
// Anything else (Meta briefly down, one bad id) is not a settings problem.
func (s *Service) noteTokenProblem(ctx context.Context, err error) {
	if e, ok := meta.AsError(err); ok && (e.Kind == meta.KindAuth || e.Kind == meta.KindPermission) {
		if rerr := s.store.RecordMetaError(ctx, e.Error()); rerr != nil {
			s.logger.Error("record meta error", "error", rerr.Error())
		}
	}
}

func unixTime(sec int64) *time.Time {
	if sec <= 0 {
		return nil
	}
	t := time.Unix(sec, 0).UTC()
	return &t
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Sync Now
// ---------------------------------------------------------------------------

const (
	syncPageSize = 100
	// syncMaxPages caps one form's reading per sync: 2,000 leads.
	syncMaxPages = 20
	// syncLookback is how far back a first sync reads. Meta keeps lead form
	// submissions for 90 days.
	syncLookback = 90 * 24 * time.Hour
	// syncOverlap re-reads a little before the last sync, in case a lead
	// was being written as that sync read; repeats are skipped.
	syncOverlap = time.Hour
)

// Sync reads every listed form's leads submitted since the last clean sync
// and takes in any the webhook missed. It stops early on a rate limit or a
// token problem, saying so; what it took in before that is kept.
func (s *Service) Sync(ctx context.Context) (model.SyncResult, error) {
	result := model.SyncResult{Errors: []string{}}
	rt, err := s.Runtime(ctx)
	if err != nil {
		return result, err
	}
	if rt.Secrets.PageAccessToken == "" {
		return result, &ErrNotReady{"Add the Page access token before syncing."}
	}
	if len(rt.Config.FormIDs) == 0 {
		return result, &ErrNotReady{"Add at least one lead form ID before syncing."}
	}

	started := s.now()
	since := started.Add(-syncLookback)
	if last := rt.Record.LastSyncAt; last != nil && last.After(since) {
		since = last.Add(-syncOverlap)
	}

	stop := false
	for _, formID := range rt.Config.FormIDs {
		if stop {
			break
		}
		result.Forms++
		after := ""
		for page := 0; ; page++ {
			if page == syncMaxPages {
				result.Partial = true
				result.Errors = append(result.Errors, fmt.Sprintf("Form %s has more than %d new leads; sync again to read the rest.", formID, syncMaxPages*syncPageSize))
				break
			}
			callCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			got, err := s.client.FormLeads(callCtx, rt.Secrets.PageAccessToken, formID, since, after, syncPageSize)
			cancel()
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("Form %s: %v", formID, err))
				s.noteTokenProblem(ctx, err)
				if e, ok := meta.AsError(err); ok && e.Kind != meta.KindInvalid {
					// Rate limits and token problems affect every form.
					result.Partial, stop = true, true
				}
				break
			}
			for _, ml := range got.Leads {
				result.Fetched++
				if done, err := s.store.AdLeadReceived(ctx, model.ProviderMeta, ml.ID); err != nil {
					return result, err
				} else if done {
					result.Skipped++
					continue
				}
				record, err := s.store.ReceiveAdLead(ctx, Submission(ml, rt.Config.PageID, rt.Config.AdAccountID))
				if err != nil {
					return result, err
				}
				switch record.Status {
				case model.AdLeadCreated:
					result.Created++
				case model.AdLeadDuplicate:
					result.Repeated++
				case model.AdLeadFailed:
					result.Failed++
				}
			}
			if got.After == "" {
				break
			}
			after = got.After
		}
	}

	problem := ""
	if len(result.Errors) > 0 {
		problem = "Sync: " + strings.Join(result.Errors, " ")
	}
	if err := s.store.RecordMetaSync(ctx, started, result.Created+result.Repeated, problem); err != nil {
		return result, err
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// Test Connection
// ---------------------------------------------------------------------------

const testCallTimeout = 15 * time.Second

// TestConnection checks every saved setting against Meta with real calls,
// and stores the outcome as the connection status. A setting left empty is
// skipped, not failed; the test passes when at least one check ran and none
// failed.
func (s *Service) TestConnection(ctx context.Context) (model.ConnectionResult, error) {
	rt, err := s.Runtime(ctx)
	if err != nil {
		return model.ConnectionResult{}, err
	}
	var checks []model.ConnectionCheck
	run := func(key, label string, ready bool, notReady string, call func(context.Context) (string, error)) bool {
		check := model.ConnectionCheck{Key: key, Label: label}
		if !ready {
			check.Skipped, check.Detail = true, notReady
			checks = append(checks, check)
			return false
		}
		callCtx, cancel := context.WithTimeout(ctx, testCallTimeout)
		defer cancel()
		detail, err := call(callCtx)
		if err != nil {
			check.Detail = err.Error()
		} else {
			check.OK, check.Detail = true, detail
		}
		checks = append(checks, check)
		return check.OK
	}
	page, cfg, sec := rt.Secrets.PageAccessToken, rt.Config, rt.Secrets

	tokenOK := run("page_token", "Page access token", page != "", "Not set.", func(ctx context.Context) (string, error) {
		me, err := s.client.Object(ctx, page, "me", "id,name")
		if err != nil {
			return "", err
		}
		id, name := fmt.Sprint(me["id"]), fmt.Sprint(me["name"])
		if cfg.PageID != "" && id != cfg.PageID {
			return "", fmt.Errorf("this token is for %s (%s), not the Page ID %s. Use a Page access token for that Page", name, id, cfg.PageID)
		}
		return fmt.Sprintf("Works, for %s (%s).", name, id), nil
	})

	// GET /app with a token answers with the app that issued it.
	run("app", "Meta App ID", cfg.AppID != "" && tokenOK, "Needs the App ID and a working Page access token.", func(ctx context.Context) (string, error) {
		app, err := s.client.Object(ctx, page, "app", "id,name")
		if err != nil {
			return "", err
		}
		if id := fmt.Sprint(app["id"]); id != cfg.AppID {
			return "", fmt.Errorf("the Page token was issued by app %s (%v), not App ID %s. Use the App ID of the app the token was made with", id, app["name"], cfg.AppID)
		}
		return fmt.Sprintf("The Page token belongs to %v.", app["name"]), nil
	})

	run("app_secret", "App secret", sec.AppSecret != "" && tokenOK, "Needs the app secret and a working Page access token.", func(ctx context.Context) (string, error) {
		if _, err := s.client.CheckAppSecret(ctx, page, sec.AppSecret); err != nil {
			return "", fmt.Errorf("%v. Check the app secret, and that the Page token was made with the same app", err)
		}
		return "Matches the app the Page token belongs to.", nil
	})

	run("page", "Facebook Page", cfg.PageID != "" && page != "", "Needs the Page ID and the Page access token.", func(ctx context.Context) (string, error) {
		obj, err := s.client.Object(ctx, page, cfg.PageID, "id,name")
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Found: %v.", obj["name"]), nil
	})

	for _, formID := range cfg.FormIDs {
		run("form:"+formID, "Lead form "+formID, page != "", "Needs the Page access token.", func(ctx context.Context) (string, error) {
			form, err := s.client.Object(ctx, page, formID, "id,name,status")
			if err != nil {
				return "", err
			}
			// Reading one lead is what proves leads_retrieval and Leads Access.
			if _, err := s.client.FormLeads(ctx, page, formID, time.Time{}, "", 1); err != nil {
				return "", fmt.Errorf("the form is visible but its leads are not: %w. Grant leads_retrieval and check Leads Access in Business settings", err)
			}
			return fmt.Sprintf("%v (%v), and its leads can be read.", form["name"], form["status"]), nil
		})
	}

	run("dataset", "Pixel / Dataset", cfg.DatasetID != "" && sec.CAPIToken != "", "Needs the Dataset ID and the Conversions API token.", func(ctx context.Context) (string, error) {
		obj, err := s.client.Object(ctx, sec.CAPIToken, cfg.DatasetID, "id,name")
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Found: %v.", obj["name"]), nil
	})

	adToken := firstNonEmpty(sec.CAPIToken, page)
	run("ad_account", "Ad account", cfg.AdAccountID != "" && adToken != "", "Needs the Ad Account ID and a token.", func(ctx context.Context) (string, error) {
		obj, err := s.client.Object(ctx, adToken, "act_"+cfg.AdAccountID, "id,name,account_status")
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Found: %v.", obj["name"]), nil
	})

	verify := model.ConnectionCheck{Key: "verify_token", Label: "Webhook verify token"}
	if sec.VerifyToken == "" {
		verify.Skipped, verify.Detail = true, "Not set."
	} else {
		verify.OK, verify.Detail = true, "Set. Meta checks it when the webhook is saved in the app."
	}
	checks = append(checks, verify)

	result := model.ConnectionResult{OK: true, Checks: checks}
	ran, firstFailure := false, ""
	for _, c := range checks {
		if c.Skipped || c.Key == "verify_token" {
			continue
		}
		ran = true
		if !c.OK {
			result.OK = false
			if firstFailure == "" {
				firstFailure = c.Label + ": " + c.Detail
			}
		}
	}
	if !ran {
		result.OK, firstFailure = false, "Nothing to test yet: add a token and IDs first."
	}
	if err := s.store.RecordMetaTest(ctx, result.OK, firstFailure); err != nil {
		return result, err
	}
	return result, nil
}

// SetSendTimeout changes how long one Conversions API call may take. Tests
// use it to make a slow Meta time out quickly.
func (s *Service) SetSendTimeout(d time.Duration) { s.sendTimeout = d }
