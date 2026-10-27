package store

import (
	"context"
	"fmt"
	"time"

	"crm-lead-management/backend/internal/model"
)

// touchTypes is model.TouchActivityTypes as plain strings, ready to bind as a
// text[] parameter.
func touchTypes() []string {
	return activityTypeStrings(model.TouchActivityTypes)
}

// conversationTypes is model.ConversationActivityTypes, likewise.
func conversationTypes() []string {
	return activityTypeStrings(model.ConversationActivityTypes)
}

func activityTypeStrings(types []model.ActivityType) []string {
	out := make([]string, len(types))
	for i, t := range types {
		out[i] = string(t)
	}
	return out
}

// lastTouchSQL is the "when did we last actually talk to this lead" expression
// for a lead aliased l, with touchTypes() bound at parameter $param. It falls
// back to created_at, so a brand-new lead starts its health clock the day it
// arrives.
//
// The (lead_id, created_at DESC) index on activities serves the MAX directly.
func lastTouchSQL(param int) string {
	return fmt.Sprintf(`COALESCE(
		(SELECT MAX(a.created_at)
		   FROM activities a
		  WHERE a.lead_id = l.id
		    AND a.type::text = ANY($%d::text[])),
		l.created_at)`, param)
}

// attachInsights fills in health and score for a batch of leads in one query,
// however many leads there are -- not one query per lead.
//
// It works on the slice in place, so callers can pass a page of results, a
// single-element slice, or the dashboard's short lists alike.
func (s *Store) attachInsights(ctx context.Context, leads []model.Lead) error {
	if len(leads) == 0 {
		return nil
	}

	ids := make([]int64, len(leads))
	for i, lead := range leads {
		ids[i] = lead.ID
	}

	// The engagement window is counted back from the database clock, the same
	// clock that stamped the activities.
	sql := `
		SELECT l.id,
		       ` + lastTouchSQL(2) + `,
		       EXISTS (SELECT 1 FROM tasks t
		                WHERE t.lead_id = l.id AND ` + openTask("t.") + `),
		       ` + stageEnteredSQLFor("l") + `,
		       (SELECT COUNT(*) FROM activities a
		         WHERE a.lead_id = l.id
		           AND a.type::text = ANY($3::text[])
		           AND a.created_at >= now() - make_interval(days => $4)),
		       (SELECT COUNT(*) FROM tasks t
		         WHERE t.lead_id = l.id AND ` + taskStatusSQL("t.") + ` = 'overdue')
		FROM leads l
		WHERE l.id = ANY($1::bigint[])`

	rows, err := s.query(ctx, sql, ids, touchTypes(), conversationTypes(), model.EngagementWindowDays)
	if err != nil {
		return fmt.Errorf("lead insights: %w", err)
	}
	defer rows.Close()

	found := make(map[int64]model.LeadSignals, len(leads))

	for rows.Next() {
		var (
			id  int64
			sig model.LeadSignals
		)
		if err := rows.Scan(&id, &sig.LastTouch, &sig.HasOpenTask, &sig.StageEnteredAt,
			&sig.RecentConversations, &sig.OverdueTasks); err != nil {
			return fmt.Errorf("scan lead insights: %w", err)
		}
		found[id] = sig
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate lead insights: %w", err)
	}

	// One clock reading for the whole batch, so two leads touched at the same
	// moment cannot land either side of a threshold.
	now := time.Now()
	for i := range leads {
		sig, ok := found[leads[i].ID]
		if !ok {
			// Deleted between the two queries. Fall back to created_at rather
			// than leaving the fields blank.
			sig.LastTouch = leads[i].CreatedAt
			sig.StageEnteredAt = leads[i].CreatedAt
		}
		leads[i].ApplyInsights(sig, now)
	}

	// Each lead's latest conversion event, for the CAPI status column.
	return s.attachMetaSummaries(ctx, leads)
}

// attachInsightsOne is attachInsights for a single lead.
func (s *Store) attachInsightsOne(ctx context.Context, lead *model.Lead) error {
	batch := []model.Lead{*lead}
	if err := s.attachInsights(ctx, batch); err != nil {
		return err
	}
	*lead = batch[0]
	return nil
}
