package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"crm-lead-management/backend/internal/model"
)

const adLeadColumns = `
	a.id, a.provider, a.external_id, a.form_id, a.campaign, a.ad, a.is_test,
	a.status, a.error, a.lead_id, COALESCE(l.name, ''), a.answers, a.received_at, a.attribution`

func scanAdLead(row rowScanner) (model.AdLead, error) {
	var (
		r                    model.AdLead
		provider, status     string
		answers, attribution []byte
	)
	err := row.Scan(&r.ID, &provider, &r.ExternalID, &r.FormID, &r.Campaign, &r.Ad, &r.IsTest,
		&status, &r.Error, &r.LeadID, &r.LeadName, &answers, &r.ReceivedAt, &attribution)
	if err != nil {
		return model.AdLead{}, err
	}
	r.Provider, r.Status = model.AdProvider(provider), model.AdLeadStatus(status)
	if err := json.Unmarshal(answers, &r.Answers); err != nil || r.Answers == nil {
		r.Answers = []model.AdAnswer{}
	}
	if len(attribution) > 0 {
		var a model.MetaAttribution
		if json.Unmarshal(attribution, &a) == nil {
			r.Attribution = &a
		}
	}
	return r, nil
}

// ListAdLeads returns the latest submissions received, newest first. Never nil.
func (s *Store) ListAdLeads(ctx context.Context, limit int) ([]model.AdLead, error) {
	rows, err := s.query(ctx, fmt.Sprintf(`
		SELECT %s FROM ad_leads a LEFT JOIN leads l ON l.id = a.lead_id
		ORDER BY a.received_at DESC, a.id DESC
		LIMIT $1`, adLeadColumns), limit)
	if err != nil {
		return nil, fmt.Errorf("list ad leads: %w", err)
	}
	records, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.AdLead, error) {
		return scanAdLead(row)
	})
	if err != nil {
		return nil, fmt.Errorf("scan ad leads: %w", err)
	}
	if records == nil {
		records = []model.AdLead{}
	}
	return records, nil
}

// GetAdLead returns one submission, or ErrNotFound.
func (s *Store) GetAdLead(ctx context.Context, id int64) (model.AdLead, error) {
	r, err := scanAdLead(s.queryRow(ctx, fmt.Sprintf(`
		SELECT %s FROM ad_leads a LEFT JOIN leads l ON l.id = a.lead_id
		WHERE a.id = $1`, adLeadColumns), id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.AdLead{}, ErrNotFound
		}
		return model.AdLead{}, fmt.Errorf("get ad lead %d: %w", id, err)
	}
	return r, nil
}

// AdLeadReceived reports whether a submission was already taken in (anything
// but a failure), so a sync can skip it without the work of taking it again.
func (s *Store) AdLeadReceived(ctx context.Context, provider model.AdProvider, externalID string) (bool, error) {
	prev, found, err := adLeadByExternalID(ctx, s.pool, provider, externalID)
	return found && prev.Status != model.AdLeadFailed, err
}

func adLeadByExternalID(ctx context.Context, q querier, provider model.AdProvider, externalID string) (model.AdLead, bool, error) {
	r, err := scanAdLead(q.QueryRow(ctx, fmt.Sprintf(`
		SELECT %s FROM ad_leads a LEFT JOIN leads l ON l.id = a.lead_id
		WHERE a.provider = $1 AND a.external_id = $2`, adLeadColumns), string(provider), externalID))
	if errors.Is(err, pgx.ErrNoRows) {
		return model.AdLead{}, false, nil
	}
	if err != nil {
		return model.AdLead{}, false, fmt.Errorf("find ad lead: %w", err)
	}
	return r, true, nil
}

// lockSubmissionTx holds a lock on one submission until tx ends. The webhook,
// a resend of it and a manual sync can all bring the same lead at once; the
// lock makes the second wait and then see the first one's record, instead of
// both creating a lead.
func lockSubmissionTx(ctx context.Context, tx pgx.Tx, provider model.AdProvider, externalID string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
		"ad_lead:"+string(provider)+":"+externalID)
	if err != nil {
		return fmt.Errorf("lock ad lead: %w", err)
	}
	return nil
}

// ReceiveAdLead takes in one form submission: as a new lead (automation rules
// run, as for any new lead), as a note on the lead that already has its email,
// as a test that becomes no lead, or as a failure saying why. The outcome is
// recorded either way, and returned.
//
// A new lead gets its quality score from the quality rules, and a Meta one its
// attribution, in the same transaction as the lead itself.
//
// A submission already taken in is not taken again: the platforms resend a
// webhook they think was missed, and a resend must not make a second lead. One
// that failed is tried again, which is how Retry works.
//
// A database error is returned as an error, with nothing recorded, so the
// webhook can answer 500 and the platform tries again later.
func (s *Store) ReceiveAdLead(ctx context.Context, sub model.AdSubmission) (model.AdLead, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.AdLead{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := lockSubmissionTx(ctx, tx, sub.Provider, sub.ExternalID); err != nil {
		return model.AdLead{}, err
	}
	if prev, found, err := adLeadByExternalID(ctx, tx, sub.Provider, sub.ExternalID); err != nil {
		return model.AdLead{}, err
	} else if found && prev.Status != model.AdLeadFailed {
		return prev, nil
	}

	status, problem, leadID, err := takeAdLeadTx(ctx, tx, sub)
	if err != nil {
		return model.AdLead{}, err
	}
	id, err := recordAdLeadTx(ctx, tx, sub, status, problem, leadID)
	if err != nil {
		return model.AdLead{}, err
	}
	// Scored once the submission (whose answers the rules read) is recorded.
	if status == model.AdLeadCreated {
		if err := scoreNewLeadTx(ctx, tx, &model.Lead{ID: *leadID}); err != nil {
			return model.AdLead{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return model.AdLead{}, fmt.Errorf("commit ad lead: %w", err)
	}
	return s.GetAdLead(ctx, id)
}

// takeAdLeadTx decides what a submission becomes, and makes it.
func takeAdLeadTx(ctx context.Context, tx pgx.Tx, sub model.AdSubmission) (model.AdLeadStatus, string, *int64, error) {
	if sub.IsTest {
		return model.AdLeadTest, "", nil, nil
	}
	in, problem := sub.LeadInput()
	if problem != "" {
		return model.AdLeadFailed, problem, nil, nil
	}

	// A savepoint, so a lead that cannot be created (its email is already a
	// lead) rolls back just that attempt and the rest of the work carries on.
	sp, err := tx.Begin(ctx)
	if err != nil {
		return "", "", nil, fmt.Errorf("savepoint: %w", err)
	}
	lead, err := createLeadTx(ctx, sp, in, true, func(ctx context.Context, tx pgx.Tx, lead *model.Lead) error {
		if sub.Attribution != nil {
			if err := insertAttributionTx(ctx, tx, lead.ID, *sub.Attribution); err != nil {
				return err
			}
			// The raw lead is the first stage Meta's CRM integration expects.
			if err := queueConversionTx(ctx, tx, lead.ID, model.StageNew); err != nil {
				return err
			}
		}
		return nil
	})
	var fieldErr *FieldError
	switch {
	case err == nil:
		if err := sp.Commit(ctx); err != nil {
			return "", "", nil, fmt.Errorf("release savepoint: %w", err)
		}
		return model.AdLeadCreated, "", &lead.ID, nil
	case errors.Is(err, ErrDuplicateEmail):
		_ = sp.Rollback(ctx)
		leadID, err := noteRepeatAdLeadTx(ctx, tx, sub, in.Email)
		if err != nil {
			return "", "", nil, err
		}
		return model.AdLeadDuplicate, "", &leadID, nil
	case errors.As(err, &fieldErr):
		_ = sp.Rollback(ctx)
		return model.AdLeadFailed, fmt.Sprintf("The lead could not be saved: %s %s.", fieldErr.Field, fieldErr.Message), nil, nil
	default:
		return "", "", nil, err
	}
}

// RecordAdLeadFailure records a submission that could not even be read, such
// as a Meta lead whose answers could not be fetched, so it shows on the
// Integrations page and can be retried.
func (s *Store) RecordAdLeadFailure(ctx context.Context, sub model.AdSubmission, problem string) (model.AdLead, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.AdLead{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := lockSubmissionTx(ctx, tx, sub.Provider, sub.ExternalID); err != nil {
		return model.AdLead{}, err
	}
	if prev, found, err := adLeadByExternalID(ctx, tx, sub.Provider, sub.ExternalID); err != nil {
		return model.AdLead{}, err
	} else if found && prev.Status != model.AdLeadFailed {
		return prev, nil
	}
	id, err := recordAdLeadTx(ctx, tx, sub, model.AdLeadFailed, problem, nil)
	if err != nil {
		return model.AdLead{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return model.AdLead{}, fmt.Errorf("commit ad lead failure: %w", err)
	}
	return s.GetAdLead(ctx, id)
}

// noteRepeatAdLeadTx puts a repeat submission on the timeline of the lead
// that already has its email, so its owner sees the renewed interest. A lead
// with no Meta attribution yet gets this submission's.
func noteRepeatAdLeadTx(ctx context.Context, tx pgx.Tx, sub model.AdSubmission, email string) (int64, error) {
	var leadID int64
	err := tx.QueryRow(ctx, `SELECT id FROM leads WHERE lower(email) = lower($1)`, email).Scan(&leadID)
	if err != nil {
		return 0, fmt.Errorf("find lead for repeat ad lead: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO activities (lead_id, type, note) VALUES ($1, $2::activity_type, $3)`,
		leadID, string(model.ActivityAdForm), sub.AgainNote()); err != nil {
		return 0, fmt.Errorf("note repeat ad lead: %w", err)
	}
	if sub.Attribution != nil {
		if err := insertAttributionTx(ctx, tx, leadID, *sub.Attribution); err != nil {
			return 0, err
		}
	}
	return leadID, nil
}

// recordAdLeadTx writes the outcome, replacing an earlier failed attempt at
// the same submission. Returns the record's id.
func recordAdLeadTx(ctx context.Context, tx pgx.Tx, sub model.AdSubmission, status model.AdLeadStatus, problem string, leadID *int64) (int64, error) {
	answers := sub.Answers
	if answers == nil {
		answers = []model.AdAnswer{}
	}
	raw, err := json.Marshal(answers)
	if err != nil {
		return 0, fmt.Errorf("encode ad lead answers: %w", err)
	}
	var attribution []byte
	if sub.Attribution != nil {
		if attribution, err = json.Marshal(sub.Attribution); err != nil {
			return 0, fmt.Errorf("encode ad lead attribution: %w", err)
		}
	}

	var id int64
	err = tx.QueryRow(ctx, `
		INSERT INTO ad_leads (provider, external_id, form_id, campaign, ad, is_test, status, error, lead_id, answers, attribution)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (provider, external_id) DO UPDATE
		SET form_id = EXCLUDED.form_id, campaign = EXCLUDED.campaign, ad = EXCLUDED.ad,
		    is_test = EXCLUDED.is_test, status = EXCLUDED.status, error = EXCLUDED.error,
		    lead_id = EXCLUDED.lead_id,
		    -- A retry whose answers could not be fetched keeps the ones it had.
		    answers = CASE WHEN EXCLUDED.answers = '[]'::jsonb THEN ad_leads.answers ELSE EXCLUDED.answers END,
		    attribution = COALESCE(EXCLUDED.attribution, ad_leads.attribution),
		    received_at = now()
		RETURNING id`,
		string(sub.Provider), sub.ExternalID, sub.FormID, sub.Campaign, sub.Ad, sub.IsTest,
		string(status), problem, leadID, raw, attribution,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("record ad lead: %w", err)
	}
	return id, nil
}
