package metaads

import (
	"context"
	"fmt"
	"time"

	"crm-lead-management/backend/internal/meta"
	"crm-lead-management/backend/internal/model"
	"crm-lead-management/backend/internal/store"
)

// The Conversions API half: its status, its connection test, a test event,
// and the sender that delivers the outbox.
//
// What is sent follows Meta's Conversion Leads payload specification:
// action_source "system_generated"; custom_data event_source "crm" and
// lead_event_source the CRM's name; user_data the lead's 15-17 digit Meta
// lead id as a number, plus its email and phone hashed with SHA-256; and an
// event_time no more than seven days old and after the lead was created.
// Nothing else about the person leaves the CRM.

const (
	sendBatch = 50
	// sendLease is how long a claimed event is kept from other senders.
	sendLease = 2 * time.Minute
	// sendInterval is how often due events are looked for.
	sendInterval = 30 * time.Second
	// rescoreInterval is how often every lead's quality is worked out again,
	// as a safety net under the per-change refreshes.
	rescoreInterval = 15 * time.Minute
	// defaultSendTimeout bounds one call to the Conversions API.
	defaultSendTimeout = 20 * time.Second
)

// CAPIUntested is the status of a configured, enabled integration nothing has
// been tested or sent with yet.
const CAPIUntested model.CAPIHealth = "untested"

// CAPIHealth is the Conversions API's status for the settings page.
func CAPIHealth(rt Runtime) model.CAPIHealth {
	switch {
	case rt.Config.DatasetID == "" || rt.Secrets.CAPIToken == "":
		return model.CAPINotConfigured
	case !rt.Config.CAPIEnabled:
		return model.CAPIDisabled
	case rt.Record.CAPIHealth != "":
		return rt.Record.CAPIHealth
	default:
		return CAPIUntested
	}
}

// healthFor reads a Conversions API failure as a status.
func healthFor(err error) model.CAPIHealth {
	e, ok := meta.AsError(err)
	if !ok {
		return model.CAPITemporarilyUnavailable
	}
	switch e.Kind {
	case meta.KindAuth:
		return model.CAPIInvalidCredentials
	case meta.KindPermission:
		return model.CAPIPermissionDenied
	case meta.KindInvalid:
		// The dataset id is wrong, or the token cannot see it.
		return model.CAPIInvalidCredentials
	default:
		return model.CAPITemporarilyUnavailable
	}
}

func errorCode(err error) int {
	if e, ok := meta.AsError(err); ok {
		return e.Code
	}
	return 0
}

func (s *Service) recordHealth(ctx context.Context, health model.CAPIHealth, detail string) {
	if err := s.store.RecordCAPIHealth(ctx, health, detail); err != nil {
		s.logger.Error("record capi health", "error", err.Error())
	}
}

// CAPITestResult is POST /api/integrations/meta-capi/test.
type CAPITestResult struct {
	OK      bool             `json:"ok"`
	Status  model.CAPIHealth `json:"status"`
	Detail  string           `json:"detail"`
	Dataset string           `json:"dataset"` // its name in Meta, when found
}

// TestCAPI checks the token can see the dataset, with a real call, and
// stores the outcome as the status. It sends no event.
func (s *Service) TestCAPI(ctx context.Context) (CAPITestResult, error) {
	rt, err := s.Runtime(ctx)
	if err != nil {
		return CAPITestResult{}, err
	}
	if rt.Config.DatasetID == "" {
		return CAPITestResult{}, &ErrNotReady{"Add the Pixel / Dataset ID first."}
	}
	if rt.Secrets.CAPIToken == "" {
		return CAPITestResult{}, &ErrNotReady{"Add the Conversions API access token first."}
	}
	callCtx, cancel := context.WithTimeout(ctx, s.sendTimeout)
	defer cancel()
	obj, err := s.client.Object(callCtx, rt.Secrets.CAPIToken, rt.Config.DatasetID, "id,name")
	if err != nil {
		health := healthFor(err)
		s.recordHealth(ctx, health, err.Error())
		return CAPITestResult{Status: health, Detail: err.Error()}, nil
	}
	detail := fmt.Sprintf("The token can use dataset %v.", obj["name"])
	s.recordHealth(ctx, model.CAPIConnected, detail)
	return CAPITestResult{OK: true, Status: model.CAPIConnected, Detail: detail, Dataset: fmt.Sprint(obj["name"])}, nil
}

// TestEventResult is POST /api/integrations/meta-capi/test-event.
type TestEventResult struct {
	EventName      string `json:"event_name"`
	EventID        string `json:"event_id"`
	TestEventCode  string `json:"test_event_code"`
	EventsReceived int    `json:"events_received"`
	FBTraceID      string `json:"fbtrace_id"`
	// UsedLeadID: the event carried a real Meta lead id from the CRM; without
	// one it carries only a hashed test email address.
	UsedLeadID bool `json:"used_lead_id"`
}

// testEmail identifies a test event when the CRM has no Meta lead yet. It is
// a reserved example address, so it can never match a real person.
const testEmail = "capi-test@example.com"

// SendTestEvent sends one event with the test event code, so it shows in
// Events Manager's Test Events tab and never counts as a conversion. It
// refuses without a test code for exactly that reason. Meta rejects an event
// whose lead_id is not a real one, so the test uses the newest Meta lead's id
// when there is one, and otherwise only a hashed example email.
func (s *Service) SendTestEvent(ctx context.Context) (TestEventResult, error) {
	rt, err := s.Runtime(ctx)
	if err != nil {
		return TestEventResult{}, err
	}
	switch {
	case rt.Config.DatasetID == "":
		return TestEventResult{}, &ErrNotReady{"Add the Pixel / Dataset ID first."}
	case rt.Secrets.CAPIToken == "":
		return TestEventResult{}, &ErrNotReady{"Add the Conversions API access token first."}
	case rt.Config.TestEventCode == "":
		return TestEventResult{}, &ErrNotReady{"Add the test event code from Events Manager > Test Events first, so the test never counts as a real conversion."}
	}

	name := "Lead"
	if mapped, ok := rt.Config.EventFor(model.StageNew); ok {
		name = mapped
	}
	now := s.now()
	ev := meta.LeadEvent{
		EventName: name, EventID: fmt.Sprintf("crm-test-%d", now.UnixNano()), EventTime: now,
		Email: testEmail, CountryCode: rt.Config.PhoneCountryCode, CRMName: rt.Config.CRMName,
	}
	result := TestEventResult{EventName: name, EventID: ev.EventID, TestEventCode: rt.Config.TestEventCode}
	leadID, email, ok, err := s.store.LatestMetaLead(ctx)
	if err != nil {
		return result, err
	}
	if ok {
		ev.MetaLeadID, ev.Email, result.UsedLeadID = leadID, email, true
	}

	callCtx, cancel := context.WithTimeout(ctx, s.sendTimeout)
	defer cancel()
	res, err := s.client.SendEvents(callCtx, rt.Secrets.CAPIToken, rt.Config.DatasetID, []meta.ServerEvent{ev.ServerEvent()}, rt.Config.TestEventCode)
	if err != nil {
		s.recordHealth(ctx, healthFor(err), err.Error())
		return result, err
	}
	s.recordHealth(ctx, model.CAPIConnected, "The test event was accepted.")
	result.EventsReceived, result.FBTraceID = res.EventsReceived, res.FBTraceID
	return result, nil
}

// SendDue sends every conversion event that is due. Nothing is sent while
// conversion feedback is off or not set up: the events wait as Pending. Each
// event goes in its own request, so one bad event cannot fail the others.
//
// Transient failures (Meta down, a timeout, a rate limit) are retried with
// backoff, up to model.MaxCAPIAttempts. A bad token, a missing permission or
// an event Meta refuses is not retried: retrying cannot fix it, so it fails
// until a person fixes the cause and retries it. Returns how many were sent.
func (s *Service) SendDue(ctx context.Context) (int, error) {
	rt, err := s.Runtime(ctx)
	if err != nil {
		return 0, err
	}
	if !rt.Config.CAPIEnabled || !rt.CAPIReady() {
		return 0, nil
	}
	due, err := s.store.ClaimDueCAPIEvents(ctx, sendBatch, sendLease)
	if err != nil {
		return 0, err
	}

	sent := 0
	for i, d := range due {
		ev := d.Event
		// Consent and spam are checked again: either may have changed since
		// the event was queued.
		if d.OptOut || (d.Spam && ev.Stage != model.StageNew) {
			reason := "Not sent: the lead opted out of sharing with Meta."
			if !d.OptOut {
				reason = "Not sent: the lead was confirmed as spam."
			}
			if err := s.store.CancelCAPIEvents(ctx, []int64{ev.ID}, reason); err != nil {
				return sent, err
			}
			continue
		}
		// Each event goes as it was queued: one queued in test mode is only
		// ever a test event, even if test mode has been turned off since.
		testCode := ""
		if ev.TestMode {
			if rt.Config.TestEventCode == "" {
				s.fail(ctx, ev, "This is a test event but no test event code is set. Add one, then retry.", 0, "", nil)
				continue
			}
			testCode = rt.Config.TestEventCode
		}
		now := s.now()
		server := meta.LeadEvent{
			EventName: ev.EventName, EventID: ev.EventID, EventTime: meta.EventTimeFor(ev.EventTime, now),
			MetaLeadID: ev.MetaLeadID, Email: d.Email, Phone: d.Phone,
			CountryCode: rt.Config.PhoneCountryCode, CRMName: rt.Config.CRMName,
		}.ServerEvent()

		callCtx, cancel := context.WithTimeout(ctx, s.sendTimeout)
		res, err := s.client.SendEvents(callCtx, rt.Secrets.CAPIToken, rt.Config.DatasetID, []meta.ServerEvent{server}, testCode)
		cancel()
		if err == nil {
			if err := s.store.MarkCAPISent(ctx, []int64{ev.ID}, res.FBTraceID); err != nil {
				return sent, err
			}
			if rt.Record.CAPIHealth != model.CAPIConnected {
				s.recordHealth(ctx, model.CAPIConnected, "Events are being accepted.")
				rt.Record.CAPIHealth = model.CAPIConnected
			}
			sent++
			continue
		}

		e, _ := meta.AsError(err)
		trace := ""
		if e != nil {
			trace = e.FBTraceID
		}
		code := errorCode(err)
		if e != nil && e.Retryable() {
			s.recordHealth(ctx, model.CAPITemporarilyUnavailable, err.Error())
			if ev.Attempts+1 >= model.MaxCAPIAttempts {
				s.fail(ctx, ev, fmt.Sprintf("%v (gave up after %d attempts)", err, ev.Attempts+1), code, trace, nil)
				continue
			}
			retryAt := now.Add(model.CAPIBackoff(ev.Attempts+1, e.RetryAfter))
			s.fail(ctx, ev, err.Error(), code, trace, &retryAt)
			if e.Kind == meta.KindRateLimit {
				// Everything else would hit the same limit: put it back, without
				// counting an attempt it never had.
				return sent, s.store.DeferCAPIEvents(ctx, eventIDs(due[i+1:]), retryAt)
			}
			continue
		}
		s.fail(ctx, ev, err.Error(), code, trace, nil)
		if e != nil && (e.Kind == meta.KindAuth || e.Kind == meta.KindPermission) {
			// The token is the problem; every other event would fail the same
			// way. Fail them now, so fixing the token and retrying sends all.
			s.recordHealth(ctx, healthFor(err), err.Error())
			s.noteTokenProblem(ctx, err)
			return sent, s.store.FailCAPIEvents(ctx, eventIDs(due[i+1:]), "Not sent: "+err.Error(), code)
		}
	}
	return sent, nil
}

func eventIDs(ds []store.CAPIDelivery) []int64 {
	ids := make([]int64, len(ds))
	for i, d := range ds {
		ids[i] = d.Event.ID
	}
	return ids
}

func (s *Service) fail(ctx context.Context, ev model.CAPIEvent, problem string, code int, trace string, retryAt *time.Time) {
	if err := s.store.MarkCAPIAttemptFailed(ctx, ev.ID, problem, code, trace, retryAt); err != nil {
		s.logger.Error("record conversion event failure", "event_id", ev.ID, "error", err.Error())
	}
}

// Run sends due conversion events, and now and then rescores every lead,
// until ctx is cancelled.
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(sendInterval)
	defer ticker.Stop()
	lastRescore := time.Time{}
	for {
		if n, err := s.SendDue(ctx); err != nil {
			if ctx.Err() == nil {
				s.logger.Error("send conversion events", "error", err.Error())
			}
		} else if n > 0 {
			s.logger.Info("conversion events sent", "count", n)
		}
		if time.Since(lastRescore) >= rescoreInterval {
			if _, err := s.store.RescoreLeads(ctx); err != nil && ctx.Err() == nil {
				s.logger.Error("rescore leads", "error", err.Error())
			}
			lastRescore = time.Now()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
