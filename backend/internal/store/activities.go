package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"crm-lead-management/backend/internal/model"
)

const activityColumns = `id, lead_id, type::text, note, created_at`

// scanActivity reads one activity row.
func scanActivity(row rowScanner) (model.Activity, error) {
	var (
		activity     model.Activity
		activityType string
	)

	if err := row.Scan(
		&activity.ID, &activity.LeadID, &activityType,
		&activity.Note, &activity.CreatedAt,
	); err != nil {
		return model.Activity{}, err
	}

	activity.Type = model.ActivityType(activityType)
	return activity, nil
}

// ListActivities returns a lead's timeline, newest first.
//
// The (lead_id, created_at DESC) index means Postgres can walk this straight out
// of the index in the requested order, with no sort step.
func (s *Store) ListActivities(ctx context.Context, leadID int64) ([]model.Activity, error) {
	sql := fmt.Sprintf(`
		SELECT %s
		FROM activities
		WHERE lead_id = $1
		ORDER BY created_at DESC, id DESC`, activityColumns)

	rows, err := s.pool.Query(ctx, sql, leadID)
	if err != nil {
		return nil, fmt.Errorf("list activities for lead %d: %w", leadID, err)
	}
	defer rows.Close()

	activities := make([]model.Activity, 0, 8)
	for rows.Next() {
		activity, err := scanActivity(rows)
		if err != nil {
			return nil, fmt.Errorf("scan activity: %w", err)
		}
		activities = append(activities, activity)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate activities: %w", err)
	}

	return activities, nil
}

// CreateActivity logs a user-supplied note or event against a lead.
//
// There is no "does this lead exist?" SELECT beforehand. The foreign key already
// guarantees it, and checking first would be both a wasted round trip and a race
// (the lead could be deleted between the check and the insert). Instead the
// constraint violation is translated into ErrNotFound.
func (s *Store) CreateActivity(ctx context.Context, leadID int64, in model.ActivityInput) (model.Activity, error) {
	sql := fmt.Sprintf(`
		INSERT INTO activities (lead_id, type, note)
		VALUES ($1, $2::activity_type, $3)
		RETURNING %s`, activityColumns)

	activity, err := scanActivity(s.pool.QueryRow(ctx, sql, leadID, string(in.Type), in.Note))
	if err != nil {
		if isForeignKeyViolation(err) {
			return model.Activity{}, ErrNotFound
		}
		return model.Activity{}, fmt.Errorf("insert activity for lead %d: %w", leadID, err)
	}

	return activity, nil
}

// ---------------------------------------------------------------------------
// Internal helpers used by the lead writes
// ---------------------------------------------------------------------------

// insertActivityTx writes a timeline row inside an existing transaction.
//
// It takes pgx.Tx rather than the pool because its callers -- CreateLead,
// UpdateLead, UpdateLeadStage -- need the activity to be committed atomically
// with the change it describes.
func insertActivityTx(ctx context.Context, tx pgx.Tx, leadID int64, activityType model.ActivityType, note string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO activities (lead_id, type, note)
		VALUES ($1, $2::activity_type, $3)`,
		leadID, string(activityType), note)
	if err != nil {
		return fmt.Errorf("insert %s activity for lead %d: %w", activityType, leadID, err)
	}
	return nil
}

// insertStageChangeTx records a stage transition in the lead's own words, e.g.
// "Stage changed from Qualified to Proposal Sent." Using the display labels keeps
// the timeline readable straight out of the database.
func insertStageChangeTx(ctx context.Context, tx pgx.Tx, leadID int64, from, to model.Stage) error {
	note := fmt.Sprintf("Stage changed from %s to %s.", from.Label(), to.Label())
	return insertActivityTx(ctx, tx, leadID, model.ActivityStageChange, note)
}
