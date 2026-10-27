package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"crm-lead-management/backend/internal/model"
)

// Conversion events for Meta's Conversions API: an outbox. A lead's lifecycle
// milestone (created, qualified, won ...) that the mapping says matters writes
// a row here in the same transaction as the change itself; the sender in
// package metaads delivers it, retrying with backoff.
//
// A milestone is one row, whatever happens after: the unique key on (lead,
// event name, mode) means a lead that leaves Qualified and comes back is not
// a second "Qualified Lead", and a retry is the same row -- and the same
// event_id, so Meta drops a duplicate if one ever gets through twice.

// queueConversionTx queues the events a lead entering stage must have sent,
// called inside the change's own transaction. Nothing is queued unless
// conversion feedback is on, the lead came from a Meta lead form (only those
// carry Meta's lead id), and the lead has not opted out of sharing with Meta.
// A lead confirmed as spam only ever sends the raw lead event, never a
// Qualified or later one.
func queueConversionTx(ctx context.Context, tx pgx.Tx, leadID int64, stage model.Stage) error {
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT config FROM integration_settings WHERE provider = $1`, metaProvider).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read conversion settings: %w", err)
	}
	cfg := model.DefaultMetaConfig()
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("decode conversion settings: %w", err)
	}
	if !cfg.CAPIEnabled {
		return nil
	}
	events := cfg.EventsReaching(stage)
	if len(events) == 0 {
		return nil
	}

	var (
		metaLeadID   string
		optOut, spam bool
	)
	err = tx.QueryRow(ctx, `
		SELECT m.meta_lead_id, l.meta_share_opt_out, l.spam_confirmed
		FROM meta_lead_attribution m JOIN leads l ON l.id = m.lead_id
		WHERE m.lead_id = $1`, leadID).Scan(&metaLeadID, &optOut, &spam)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // not a Meta lead: Meta has nothing to match it to
	}
	if err != nil {
		return fmt.Errorf("read attribution of lead %d: %w", leadID, err)
	}
	if optOut {
		return nil
	}

	for _, ev := range events {
		if spam && ev.Stage != model.StageNew {
			continue
		}
		// When it happened: the lead's creation for the raw lead, else when
		// it last entered that stage (now, for the stage being entered).
		_, err := tx.Exec(ctx, `
			INSERT INTO capi_events (lead_id, meta_lead_id, stage, event_name, event_id, event_time, test_mode)
			VALUES ($1, $2, $3::lead_stage, $4, $5,
			        CASE WHEN $3 = 'new' THEN (SELECT created_at FROM leads WHERE id = $1)
			             ELSE COALESCE((SELECT max(changed_at) FROM lead_stage_history
			                            WHERE lead_id = $1 AND to_stage = $3::lead_stage), now())
			        END,
			        $6)
			ON CONFLICT DO NOTHING`,
			leadID, metaLeadID, string(ev.Stage), ev.EventName,
			model.CAPIEventID(leadID, ev.EventName, cfg.TestMode), cfg.TestMode)
		if err != nil {
			return fmt.Errorf("queue conversion event for lead %d: %w", leadID, err)
		}
	}
	return nil
}

// cancelCAPIEventsTx cancels a lead's unsent events: all of them, or with
// afterLead only those past the raw lead stage.
func cancelCAPIEventsTx(ctx context.Context, tx pgx.Tx, leadID int64, afterLead bool, reason string) error {
	_, err := tx.Exec(ctx, `
		UPDATE capi_events SET status = 'cancelled', last_error = $3, updated_at = now()
		WHERE lead_id = $1 AND status IN ('pending', 'retry_scheduled', 'failed')
		  AND (NOT $2 OR stage <> 'new')`, leadID, afterLead, reason)
	if err != nil {
		return fmt.Errorf("cancel conversion events of lead %d: %w", leadID, err)
	}
	return nil
}

const capiEventColumns = `
	e.id, e.lead_id, COALESCE(l.name, ''), e.meta_lead_id, e.stage::text, e.event_name, e.event_id,
	e.event_time, e.test_mode, e.status, e.attempts, e.next_attempt_at, e.last_error, e.error_code,
	e.fbtrace_id, e.sent_at, e.created_at`

func scanCAPIEvent(row rowScanner, extra ...any) (model.CAPIEvent, error) {
	var (
		e             model.CAPIEvent
		stage, status string
		next          time.Time
	)
	dest := append([]any{&e.ID, &e.LeadID, &e.LeadName, &e.MetaLeadID, &stage, &e.EventName, &e.EventID,
		&e.EventTime, &e.TestMode, &status, &e.Attempts, &next, &e.LastError, &e.ErrorCode,
		&e.FBTraceID, &e.SentAt, &e.CreatedAt}, extra...)
	if err := row.Scan(dest...); err != nil {
		return model.CAPIEvent{}, err
	}
	e.Stage, e.Status = model.Stage(stage), model.CAPIStatus(status)
	if e.Status == model.CAPIRetryScheduled {
		e.NextAttemptAt = &next
	}
	return e, nil
}

// ListCAPIEvents returns the delivery log, newest first. It carries no
// customer data: no email, no phone, no payload.
func (s *Store) ListCAPIEvents(ctx context.Context, q model.CAPIEventQuery) ([]model.CAPIEvent, error) {
	limit := q.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.query(ctx, `
		SELECT `+capiEventColumns+`
		FROM capi_events e LEFT JOIN leads l ON l.id = e.lead_id
		WHERE ($1 = '' OR e.status = $1) AND ($2 = 0 OR e.lead_id = $2)
		  AND ($3::timestamptz IS NULL OR e.created_at >= $3)
		  AND ($4::timestamptz IS NULL OR e.created_at < $4)
		ORDER BY e.created_at DESC, e.id DESC
		LIMIT $5`, string(q.Status), q.LeadID, q.From, q.To, limit)
	if err != nil {
		return nil, fmt.Errorf("list conversion events: %w", err)
	}
	events, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.CAPIEvent, error) {
		return scanCAPIEvent(row)
	})
	if err != nil {
		return nil, fmt.Errorf("scan conversion events: %w", err)
	}
	if events == nil {
		events = []model.CAPIEvent{}
	}
	return events, nil
}

// CAPIStatusCounts counts events by status, for the delivery log's tabs.
func (s *Store) CAPIStatusCounts(ctx context.Context) (map[model.CAPIStatus]int, error) {
	rows, err := s.query(ctx, `SELECT status, COUNT(*) FROM capi_events GROUP BY status`)
	if err != nil {
		return nil, fmt.Errorf("count conversion events: %w", err)
	}
	defer rows.Close()
	counts := map[model.CAPIStatus]int{}
	for _, st := range model.CAPIStatuses {
		counts[st] = 0
	}
	for rows.Next() {
		var (
			status string
			n      int
		)
		if err := rows.Scan(&status, &n); err != nil {
			return nil, fmt.Errorf("scan conversion event count: %w", err)
		}
		counts[model.CAPIStatus(status)] = n
	}
	return counts, rows.Err()
}

// attachMetaSummaries fills in each lead's latest conversion event.
func (s *Store) attachMetaSummaries(ctx context.Context, leads []model.Lead) error {
	if len(leads) == 0 {
		return nil
	}
	ids := make([]int64, len(leads))
	for i, l := range leads {
		ids[i] = l.ID
	}
	rows, err := s.query(ctx, `
		SELECT DISTINCT ON (lead_id) lead_id, status, event_name, COALESCE(sent_at, updated_at), error_code
		FROM capi_events
		WHERE lead_id = ANY($1)
		ORDER BY lead_id, created_at DESC, id DESC`, ids)
	if err != nil {
		return fmt.Errorf("lead meta summaries: %w", err)
	}
	defer rows.Close()
	found := map[int64]model.LeadMetaSummary{}
	for rows.Next() {
		var (
			id     int64
			sum    model.LeadMetaSummary
			status string
			at     time.Time
		)
		if err := rows.Scan(&id, &status, &sum.MetaLastEventName, &at, &sum.MetaLastErrorCode); err != nil {
			return fmt.Errorf("scan lead meta summary: %w", err)
		}
		sum.MetaCAPIStatus, sum.MetaLastEventAt = model.CAPIStatus(status), &at
		found[id] = sum
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range leads {
		leads[i].LeadMetaSummary = found[leads[i].ID]
	}
	return nil
}

// CAPIDelivery is a claimed event with what is needed to send it: the lead's
// current email and phone, and whether it may still be sent.
type CAPIDelivery struct {
	Event  model.CAPIEvent
	Email  string
	Phone  string
	OptOut bool
	Spam   bool
}

// ClaimDueCAPIEvents takes up to limit events that are due, and pushes their
// next attempt lease into the future so a second sender running at the same
// time does not take them too. A sender that dies mid-send leaves them to be
// picked up again once the lease runs out.
func (s *Store) ClaimDueCAPIEvents(ctx context.Context, limit int, lease time.Duration) ([]CAPIDelivery, error) {
	rows, err := s.query(ctx, `
		WITH due AS (
			SELECT id FROM capi_events
			WHERE status IN ('pending', 'retry_scheduled') AND next_attempt_at <= now()
			ORDER BY next_attempt_at, event_time, id
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		), claimed AS (
			UPDATE capi_events e SET next_attempt_at = now() + $2::interval, updated_at = now()
			FROM due WHERE e.id = due.id
			RETURNING e.*
		)
		SELECT `+capiEventColumns+`, COALESCE(l.email, ''), COALESCE(l.phone, ''),
		       COALESCE(l.meta_share_opt_out, true), COALESCE(l.spam_confirmed, false)
		FROM claimed e LEFT JOIN leads l ON l.id = e.lead_id
		ORDER BY e.event_time, e.id`, limit, lease.String())
	if err != nil {
		return nil, fmt.Errorf("claim conversion events: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (CAPIDelivery, error) {
		var d CAPIDelivery
		var err error
		d.Event, err = scanCAPIEvent(row, &d.Email, &d.Phone, &d.OptOut, &d.Spam)
		return d, err
	})
}

// MarkCAPISent records events Meta accepted.
func (s *Store) MarkCAPISent(ctx context.Context, ids []int64, fbtraceID string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE capi_events
		SET status = 'sent', sent_at = now(), attempts = attempts + 1, last_error = '', error_code = NULL,
		    fbtrace_id = $2, updated_at = now()
		WHERE id = ANY($1)`, ids, fbtraceID)
	if err != nil {
		return fmt.Errorf("mark conversion events sent: %w", err)
	}
	return nil
}

// MarkCAPIAttemptFailed records a failed attempt: scheduled to try again at
// retryAt, or failed for good when retryAt is nil. code is Meta's error code
// (0 when Meta gave none, as for a timeout).
func (s *Store) MarkCAPIAttemptFailed(ctx context.Context, id int64, problem string, code int, fbtraceID string, retryAt *time.Time) error {
	status := model.CAPIFailed
	next := time.Now()
	if retryAt != nil {
		status, next = model.CAPIRetryScheduled, *retryAt
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE capi_events
		SET status = $2, attempts = attempts + 1, last_error = $3, error_code = NULLIF($4, 0),
		    fbtrace_id = $5, next_attempt_at = $6, updated_at = now()
		WHERE id = $1`, id, string(status), truncateRunes(problem, 1000), code, fbtraceID, next)
	if err != nil {
		return fmt.Errorf("mark conversion event %d failed: %w", id, err)
	}
	return nil
}

// DeferCAPIEvents moves claimed events that were not tried to later, without
// counting an attempt.
func (s *Store) DeferCAPIEvents(ctx context.Context, ids []int64, until time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := s.pool.Exec(ctx, `UPDATE capi_events SET next_attempt_at = $2, updated_at = now() WHERE id = ANY($1)`, ids, until)
	if err != nil {
		return fmt.Errorf("defer conversion events: %w", err)
	}
	return nil
}

// FailCAPIEvents marks claimed events that were not tried as failed, for a
// problem that would fail them all (a revoked token), without counting an
// attempt. Retrying them once it is fixed sends them.
func (s *Store) FailCAPIEvents(ctx context.Context, ids []int64, problem string, code int) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE capi_events SET status = 'failed', last_error = $2, error_code = NULLIF($3, 0), updated_at = now()
		WHERE id = ANY($1)`, ids, truncateRunes(problem, 1000), code)
	if err != nil {
		return fmt.Errorf("fail conversion events: %w", err)
	}
	return nil
}

// CancelCAPIEvents marks claimed events that must no longer be sent.
func (s *Store) CancelCAPIEvents(ctx context.Context, ids []int64, reason string) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE capi_events SET status = 'cancelled', last_error = $2, updated_at = now() WHERE id = ANY($1)`,
		ids, reason)
	if err != nil {
		return fmt.Errorf("cancel conversion events: %w", err)
	}
	return nil
}

// RetryFailedCAPIEvents puts every failed event back in the queue, for after
// a token has been fixed. Returns how many.
func (s *Store) RetryFailedCAPIEvents(ctx context.Context) (int, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE capi_events SET status = 'pending', attempts = 0, next_attempt_at = now(), updated_at = now()
		WHERE status = 'failed'`)
	if err != nil {
		return 0, fmt.Errorf("retry failed conversion events: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// RetryCAPIEvent puts a failed (or scheduled) event back in the queue to send
// now, with a fresh set of attempts. It is the same event, with the same
// event_id: a retry, not a new milestone.
func (s *Store) RetryCAPIEvent(ctx context.Context, id int64) (model.CAPIEvent, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE capi_events
		SET status = 'pending', attempts = 0, next_attempt_at = now(), updated_at = now()
		WHERE id = $1 AND status IN ('failed', 'retry_scheduled')`, id)
	if err != nil {
		return model.CAPIEvent{}, fmt.Errorf("retry conversion event %d: %w", id, err)
	}
	event, err := scanCAPIEvent(s.queryRow(ctx, `
		SELECT `+capiEventColumns+` FROM capi_events e LEFT JOIN leads l ON l.id = e.lead_id WHERE e.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return model.CAPIEvent{}, ErrNotFound
	}
	if err != nil {
		return model.CAPIEvent{}, fmt.Errorf("get conversion event %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return event, ErrCAPINotRetryable
	}
	return event, nil
}

// LatestMetaLead returns the most recently imported Meta lead's id and email
// that may be shared, for a test event that Meta can match. ok is false when
// there is none.
func (s *Store) LatestMetaLead(ctx context.Context) (metaLeadID, email string, ok bool, err error) {
	err = s.queryRow(ctx, `
		SELECT m.meta_lead_id, l.email
		FROM meta_lead_attribution m JOIN leads l ON l.id = m.lead_id
		WHERE NOT l.meta_share_opt_out
		ORDER BY m.created_at DESC LIMIT 1`).Scan(&metaLeadID, &email)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, fmt.Errorf("latest meta lead: %w", err)
	}
	return metaLeadID, email, true, nil
}
