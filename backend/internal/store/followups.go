package store

import (
	"context"
	"fmt"
	"strings"

	"crm-lead-management/backend/internal/model"
)

// FollowUps builds the Follow-up Center: open tasks due by a week from today,
// split into overdue / today / this week, and the open leads that are going
// cold, have no next step planned, or are stuck in their stage.
//
// The filter applies to the task's assignee and the lead's owner, so "Asha"
// shows the calls Asha has to make and the leads Asha is responsible for.
// Dates are cut in the database's time zone, like every task status.
func (s *Store) FollowUps(ctx context.Context, f model.OwnerFilter) (model.FollowUpSummary, error) {
	var summary model.FollowUpSummary

	var weekEnd string
	if err := s.queryRow(ctx,
		`SELECT CURRENT_DATE::text, (CURRENT_DATE + 7)::text`,
	).Scan(&summary.Today, &weekEnd); err != nil {
		return summary, fmt.Errorf("follow-ups: read today: %w", err)
	}

	// --- tasks --------------------------------------------------------------
	// Only the viewer's leads' tasks (see scope.go).
	taskConds := []string{openTask(""), "due_date <= $1::date", "lead_id IN (SELECT id FROM leads)"}
	taskArgs := []any{weekEnd}
	switch {
	case f.OwnerID != nil:
		taskArgs = append(taskArgs, *f.OwnerID)
		taskConds = append(taskConds, fmt.Sprintf("assignee_id = $%d", len(taskArgs)))
	case f.Unowned:
		taskConds = append(taskConds, "assignee_id IS NULL")
	}
	taskArgs = append(taskArgs, model.MaxFollowUpTasks)

	taskSQL := fmt.Sprintf(`
		SELECT %s FROM tasks
		WHERE %s
		ORDER BY due_date, start_time NULLS FIRST, id
		LIMIT $%d`, taskColumns, strings.Join(taskConds, " AND "), len(taskArgs))

	tasks, err := s.tasksWithLeads(ctx, taskSQL, taskArgs...)
	if err != nil {
		return summary, fmt.Errorf("follow-ups: %w", err)
	}
	summary.Overdue, summary.DueToday, summary.ThisWeek = model.SplitFollowUpTasks(tasks, summary.Today)

	// --- leads --------------------------------------------------------------
	leadConds := []string{`stage NOT IN ('won', 'lost')`}
	var leadArgs []any
	switch {
	case f.OwnerID != nil:
		leadArgs = append(leadArgs, *f.OwnerID)
		leadConds = append(leadConds, fmt.Sprintf("owner_id = $%d", len(leadArgs)))
	case f.Unowned:
		leadConds = append(leadConds, "owner_id IS NULL")
	}
	leadArgs = append(leadArgs, model.MaxFollowUpLeads)

	leadSQL := fmt.Sprintf(`
		SELECT %s FROM leads
		WHERE %s
		ORDER BY updated_at DESC, id DESC
		LIMIT $%d`, leadColumns, strings.Join(leadConds, " AND "), len(leadArgs))

	rows, err := s.query(ctx, leadSQL, leadArgs...)
	if err != nil {
		return summary, fmt.Errorf("follow-ups: list open leads: %w", err)
	}
	defer rows.Close()

	leads := make([]model.Lead, 0, 32)
	for rows.Next() {
		lead, err := scanLead(rows)
		if err != nil {
			return summary, fmt.Errorf("follow-ups: scan lead: %w", err)
		}
		leads = append(leads, lead)
	}
	if err := rows.Err(); err != nil {
		return summary, fmt.Errorf("follow-ups: iterate leads: %w", err)
	}

	// Health and "has an open task" for every open lead, in one query.
	if err := s.attachInsights(ctx, leads); err != nil {
		return summary, err
	}
	summary.GoingCold, summary.NoNextStep, summary.Stuck = model.SplitAttentionLeads(leads)

	return summary, nil
}
