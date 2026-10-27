package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"crm-lead-management/backend/internal/model"
)

// Lead quality storage. Every lead's quality is worked out by one function,
// refreshQuality, from its own fields, its form answers and its timeline; and
// every change that can move it (a new lead, a logged call, a completed
// meeting, a spam flag, an override, new rules) calls it. The result is stored
// so lists can filter and sort on it.

// dbtx is what a pool and a transaction have in common.
type dbtx interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

const qualityRulesKey = "lead_quality_rules"

// GetQualityRules returns the saved rules, or the defaults.
func (s *Store) GetQualityRules(ctx context.Context) (model.QualityRules, error) {
	return qualityRules(ctx, s.pool)
}

func qualityRules(ctx context.Context, q querier) (model.QualityRules, error) {
	var raw []byte
	err := q.QueryRow(ctx, `SELECT value FROM app_settings WHERE key = $1`, qualityRulesKey).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.DefaultQualityRules(), nil
	}
	if err != nil {
		return model.QualityRules{}, fmt.Errorf("get quality rules: %w", err)
	}
	// Decoded over the defaults, so a setting added later starts at its default.
	rules := model.DefaultQualityRules()
	rules.Rules = nil
	if err := json.Unmarshal(raw, &rules); err != nil {
		return model.QualityRules{}, fmt.Errorf("decode quality rules: %w", err)
	}
	rules.Normalize()
	return rules, nil
}

// SaveQualityRules stores the rules and rescores every lead, so no lead shows
// a score from rules that no longer exist. Returns how many leads changed.
func (s *Store) SaveQualityRules(ctx context.Context, rules model.QualityRules) (int, error) {
	raw, err := json.Marshal(rules)
	if err != nil {
		return 0, fmt.Errorf("encode quality rules: %w", err)
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO app_settings (key, value) VALUES ($1, $2)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`,
		qualityRulesKey, raw); err != nil {
		return 0, fmt.Errorf("save quality rules: %w", err)
	}
	return s.RescoreLeads(ctx)
}

// RescoreLeads works out every lead's quality again. Returns how many changed.
func (s *Store) RescoreLeads(ctx context.Context) (int, error) {
	rules, err := s.GetQualityRules(ctx)
	if err != nil {
		return 0, err
	}
	results, err := refreshQuality(ctx, s.pool, rules, nil)
	if err != nil {
		return 0, err
	}
	changed := 0
	for _, r := range results {
		if r.changed {
			changed++
		}
	}
	return changed, nil
}

// RefreshQuality works out the quality of these leads again, after something
// that can move it (a logged call, a completed meeting, an edit).
func (s *Store) RefreshQuality(ctx context.Context, ids ...int64) error {
	if len(ids) == 0 {
		return nil
	}
	rules, err := s.GetQualityRules(ctx)
	if err != nil {
		return err
	}
	_, err = refreshQuality(ctx, s.pool, rules, ids)
	return err
}

type qualityResult struct {
	id      int64
	quality model.Quality
	score   *int
	hits    []model.QualityHit
	reason  string
	changed bool
}

// refreshQuality scores the leads with these ids (every lead for nil) and
// stores what changed. quality_updated_at moves only when something did.
func refreshQuality(ctx context.Context, db dbtx, rules model.QualityRules, ids []int64) ([]qualityResult, error) {
	rows, err := db.Query(ctx, `
		SELECT l.id, l.name, l.email, COALESCE(l.phone, ''), l.company, l.spam_confirmed,
		       l.quality_override, l.quality_override_reason,
		       (SELECT answers FROM ad_leads a
		         WHERE a.lead_id = l.id AND a.status = 'created'
		         ORDER BY a.received_at LIMIT 1),
		       (SELECT COUNT(*) FROM activities a
		         WHERE a.lead_id = l.id AND a.type IN ('call', 'email', 'meeting')),
		       (SELECT COUNT(*) FROM activities a WHERE a.lead_id = l.id AND a.type = 'meeting')
		       + (SELECT COUNT(*) FROM tasks t
		           WHERE t.lead_id = l.id AND t.type IN ('meeting', 'demo') AND t.completed_at IS NOT NULL)
		FROM leads l
		WHERE $1::bigint[] IS NULL OR l.id = ANY($1)`, ids)
	if err != nil {
		return nil, fmt.Errorf("leads to score: %w", err)
	}
	var results []qualityResult
	for rows.Next() {
		var (
			id             int64
			in             model.QualityInput
			override       *int
			overrideReason *string
			answers        []byte
		)
		if err := rows.Scan(&id, &in.Name, &in.Email, &in.Phone, &in.Company, &in.Spam,
			&override, &overrideReason, &answers, &in.Conversations, &in.Meetings); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan lead to score: %w", err)
		}
		if answers != nil {
			_ = json.Unmarshal(answers, &in.Answers)
		}
		a := rules.Assess(in)
		quality, reason := rules.Final(a, override, overrideReason, in.Spam)
		results = append(results, qualityResult{id: id, quality: quality, score: a.Score, hits: a.Hits, reason: reason})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("leads to score: %w", err)
	}

	for i, r := range results {
		hits, err := json.Marshal(r.hits)
		if err != nil {
			return nil, fmt.Errorf("encode quality breakdown: %w", err)
		}
		tag, err := db.Exec(ctx, `
			UPDATE leads
			SET quality = $2, quality_score = $3, quality_breakdown = $4, quality_reason = $5,
			    quality_updated_at = now()
			WHERE id = $1
			  AND (quality IS DISTINCT FROM $2 OR quality_score IS DISTINCT FROM $3
			       OR quality_breakdown IS DISTINCT FROM $4::jsonb OR quality_reason IS DISTINCT FROM $5)`,
			r.id, string(r.quality), r.score, hits, r.reason)
		if err != nil {
			return nil, fmt.Errorf("store quality of lead %d: %w", r.id, err)
		}
		results[i].changed = tag.RowsAffected() > 0
	}
	return results, nil
}

// scoreNewLeadTx scores a lead as it is created, in its own transaction, and
// fills the result in on lead.
func scoreNewLeadTx(ctx context.Context, tx pgx.Tx, lead *model.Lead) error {
	rules, err := qualityRules(ctx, tx)
	if err != nil {
		return err
	}
	results, err := refreshQuality(ctx, tx, rules, []int64{lead.ID})
	if err != nil || len(results) == 0 {
		return err
	}
	r := results[0]
	lead.Quality, lead.QualityScore, lead.QualityBreakdown, lead.QualityReason = r.quality, r.score, r.hits, r.reason
	return nil
}

// SetQualityOverride replaces a lead's quality score with one given by hand,
// for a reason, and logs it on the timeline. Returns the updated lead.
func (s *Store) SetQualityOverride(ctx context.Context, id int64, score int, reason string) (model.Lead, error) {
	return s.changeQuality(ctx, id, func(tx pgx.Tx) (string, error) {
		var previous *int
		err := tx.QueryRow(ctx, `
			WITH prev AS (SELECT id, quality_override FROM leads WHERE id = $1 FOR UPDATE)
			UPDATE leads l
			SET quality_override = $2, quality_override_reason = $3, quality_override_at = now()
			FROM prev WHERE l.id = prev.id
			RETURNING prev.quality_override`, id, score, reason).Scan(&previous)
		if err != nil {
			return "", err
		}
		if previous != nil {
			return fmt.Sprintf("Quality score changed by hand from %d to %d: %s", *previous, score, reason), nil
		}
		return fmt.Sprintf("Quality score set by hand to %d: %s", score, reason), nil
	})
}

// ClearQualityOverride goes back to the rules' score, and logs it.
func (s *Store) ClearQualityOverride(ctx context.Context, id int64) (model.Lead, error) {
	return s.changeQuality(ctx, id, func(tx pgx.Tx) (string, error) {
		var previous *int
		err := tx.QueryRow(ctx, `
			WITH prev AS (SELECT id, quality_override FROM leads WHERE id = $1 FOR UPDATE)
			UPDATE leads l
			SET quality_override = NULL, quality_override_reason = NULL, quality_override_at = NULL
			FROM prev WHERE l.id = prev.id
			RETURNING prev.quality_override`, id).Scan(&previous)
		if err != nil || previous == nil {
			return "", err // nothing to clear: no timeline entry
		}
		return fmt.Sprintf("Quality score override of %d removed; back to the rules' score.", *previous), nil
	})
}

// SetLeadFlags confirms (or clears) spam and opts the lead out of (or back
// into) sharing with Meta. Either cancels the conversion events that must no
// longer be sent: an opt-out every unsent one, spam the Qualified and later
// ones (a spam lead is still a lead, but never a qualified one).
func (s *Store) SetLeadFlags(ctx context.Context, id int64, in model.LeadFlagsInput) (model.Lead, error) {
	return s.changeQuality(ctx, id, func(tx pgx.Tx) (string, error) {
		var spam, optOut bool
		err := tx.QueryRow(ctx, `SELECT spam_confirmed, meta_share_opt_out FROM leads WHERE id = $1 FOR UPDATE`, id).
			Scan(&spam, &optOut)
		if err != nil {
			return "", err
		}
		var notes []string
		if in.SpamConfirmed != nil && *in.SpamConfirmed != spam {
			spam = *in.SpamConfirmed
			if spam {
				notes = append(notes, "Confirmed as invalid or spam.")
			} else {
				notes = append(notes, "No longer marked as spam.")
			}
		}
		if in.MetaShareOptOut != nil && *in.MetaShareOptOut != optOut {
			optOut = *in.MetaShareOptOut
			if optOut {
				notes = append(notes, "Opted out of sharing with Meta.")
			} else {
				notes = append(notes, "Sharing with Meta allowed again.")
			}
		}
		if len(notes) == 0 {
			return "", nil
		}
		if _, err := tx.Exec(ctx, `UPDATE leads SET spam_confirmed = $2, meta_share_opt_out = $3 WHERE id = $1`,
			id, spam, optOut); err != nil {
			return "", err
		}
		if optOut {
			err = cancelCAPIEventsTx(ctx, tx, id, false, "Not sent: the lead opted out of sharing with Meta.")
		} else if spam {
			err = cancelCAPIEventsTx(ctx, tx, id, true, "Not sent: the lead was confirmed as spam.")
		}
		if err != nil {
			return "", err
		}
		return strings.Join(notes, " "), nil
	})
}

// changeQuality runs change in a transaction, logs the note it returns (if
// any) on the timeline, rescores the lead and returns it.
func (s *Store) changeQuality(ctx context.Context, id int64, change func(pgx.Tx) (string, error)) (model.Lead, error) {
	rules, err := s.GetQualityRules(ctx)
	if err != nil {
		return model.Lead{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.Lead{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	note, err := change(tx)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Lead{}, ErrNotFound
	}
	if err != nil {
		return model.Lead{}, fmt.Errorf("change quality of lead %d: %w", id, err)
	}
	if note != "" {
		if err := insertActivityTx(ctx, tx, id, model.ActivityQualityChange, truncateRunes(note, 2000)); err != nil {
			return model.Lead{}, err
		}
	}
	if _, err := refreshQuality(ctx, tx, rules, []int64{id}); err != nil {
		return model.Lead{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return model.Lead{}, fmt.Errorf("commit quality change: %w", err)
	}
	return s.GetLead(ctx, id)
}

// UpdateQualificationReason replaces the salesperson's note on why the lead
// is, or is not, qualified.
func (s *Store) UpdateQualificationReason(ctx context.Context, id int64, reason string) (model.Lead, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE leads SET qualification_reason = $2 WHERE id = $1`, id, reason)
	if err != nil {
		return model.Lead{}, fmt.Errorf("update qualification reason of lead %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return model.Lead{}, ErrNotFound
	}
	return s.GetLead(ctx, id)
}
