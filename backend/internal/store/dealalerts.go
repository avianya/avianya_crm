package store

import (
	"context"
	"fmt"

	"crm-lead-management/backend/internal/model"
)

// stageEnteredSQLFor is when the lead aliased alias entered its current stage:
// its latest stage history entry, falling back to when it was created. The
// (lead_id, changed_at, id) index serves the MAX directly.
func stageEnteredSQLFor(alias string) string {
	return fmt.Sprintf(`COALESCE(
		(SELECT MAX(h.changed_at) FROM lead_stage_history h WHERE h.lead_id = %[1]s.id),
		%[1]s.created_at)`, alias)
}

// RaiseStuckDealAlerts raises an alert for every open deal whose current stint
// in its stage has just passed the stage's limit, and returns how many were
// raised. A stint that already has an alert is skipped, so this is safe to run
// on every reminder tick.
func (s *Store) RaiseStuckDealAlerts(ctx context.Context) (int, error) {
	stages, days := model.StuckThresholds()

	sql := fmt.Sprintf(`
		WITH thresholds AS (
			SELECT * FROM unnest($1::text[], $2::int[]) AS t(stage, limit_days)
		),
		current_stint AS (
			SELECT l.id, l.stage, th.limit_days, %s AS entered
			FROM leads l
			JOIN thresholds th ON th.stage = l.stage::text
		)
		INSERT INTO deal_alerts (lead_id, stage, stage_entered_at, limit_days)
		SELECT id, stage, entered, limit_days
		FROM current_stint
		WHERE now() - entered >= make_interval(days => limit_days)
		ON CONFLICT (lead_id, stage_entered_at) DO NOTHING`, stageEnteredSQLFor("l"))

	tag, err := s.pool.Exec(ctx, sql, stages, days)
	if err != nil {
		return 0, fmt.Errorf("raise stuck deal alerts: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// listDealAlerts returns the latest alerts, still-stuck ones first, and how
// many unread alerts are still stuck. An alert is resolved once its lead has
// entered another stage since -- moved on, or been won or lost.
func (s *Store) listDealAlerts(ctx context.Context, limit int) ([]model.DealAlert, int, error) {
	sql := fmt.Sprintf(`
		WITH alerts AS (
			SELECT a.id, a.lead_id, l.name, l.company, l.owner_id,
			       a.stage::text AS stage, a.stage_entered_at, a.limit_days,
			       a.created_at, a.read_at,
			       %s IS DISTINCT FROM a.stage_entered_at AS resolved,
			       (SELECT MIN(h.changed_at) FROM lead_stage_history h
			         WHERE h.lead_id = a.lead_id AND h.changed_at > a.stage_entered_at) AS left_at
			FROM deal_alerts a
			JOIN leads l ON l.id = a.lead_id
		)
		SELECT id, lead_id, name, company, owner_id, stage, stage_entered_at, limit_days,
		       created_at, read_at, resolved,
		       floor(extract(epoch FROM COALESCE(left_at, now()) - stage_entered_at) / 86400)::int,
		       COUNT(*) FILTER (WHERE read_at IS NULL AND NOT resolved) OVER ()
		FROM alerts
		ORDER BY resolved, created_at DESC, id DESC
		LIMIT $1`, stageEnteredSQLFor("l"))

	rows, err := s.query(ctx, sql, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("list deal alerts: %w", err)
	}
	defer rows.Close()

	alerts := make([]model.DealAlert, 0, 8)
	unread := 0
	for rows.Next() {
		var (
			a     model.DealAlert
			stage string
		)
		if err := rows.Scan(&a.ID, &a.LeadID, &a.LeadName, &a.LeadCompany, &a.OwnerID,
			&stage, &a.StageEnteredAt, &a.LimitDays, &a.CreatedAt, &a.ReadAt, &a.Resolved,
			&a.DaysInStage, &unread); err != nil {
			return nil, 0, fmt.Errorf("scan deal alert: %w", err)
		}
		a.Stage = model.Stage(stage)
		alerts = append(alerts, a)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate deal alerts: %w", err)
	}

	// The window count is taken before LIMIT, so it covers every alert, and
	// with no alerts at all it stays 0.
	return alerts, unread, nil
}

// MarkDealAlertRead marks one alert read. Already-read is fine.
func (s *Store) MarkDealAlertRead(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE deal_alerts SET read_at = COALESCE(read_at, now()) WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("mark deal alert %d read: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
