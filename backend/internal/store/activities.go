package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"crm-lead-management/backend/internal/model"
)

const activityColumns = `id, lead_id, type::text, note, created_at, edited_at`

// scanActivity reads one activity row.
func scanActivity(row rowScanner) (model.Activity, error) {
	var (
		activity     model.Activity
		activityType string
	)

	if err := row.Scan(
		&activity.ID, &activity.LeadID, &activityType,
		&activity.Note, &activity.CreatedAt, &activity.EditedAt,
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

	rows, err := s.query(ctx, sql, leadID)
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

	// Every lead's timeline starts with its "created" entry, so an empty one
	// almost always means a wrong id. Only then is it worth a second query.
	if len(activities) == 0 {
		var exists bool
		if err := s.queryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM leads WHERE id = $1)`, leadID,
		).Scan(&exists); err != nil {
			return nil, fmt.Errorf("check lead %d exists: %w", leadID, err)
		}
		if !exists {
			return nil, ErrNotFound
		}
	}

	return activities, nil
}

// ListStageHistory returns the stages a lead has been in, oldest first.
func (s *Store) ListStageHistory(ctx context.Context, leadID int64) ([]model.StageHistoryEntry, error) {
	rows, err := s.query(ctx, `
		SELECT id, from_stage::text, to_stage::text, changed_at
		FROM lead_stage_history
		WHERE lead_id = $1
		ORDER BY changed_at, id`, leadID)
	if err != nil {
		return nil, fmt.Errorf("list stage history for lead %d: %w", leadID, err)
	}
	defer rows.Close()

	history := make([]model.StageHistoryEntry, 0, 4)
	for rows.Next() {
		var (
			entry model.StageHistoryEntry
			from  *string
			to    string
		)
		if err := rows.Scan(&entry.ID, &from, &to, &entry.ChangedAt); err != nil {
			return nil, fmt.Errorf("scan stage history: %w", err)
		}
		if from != nil {
			stage := model.Stage(*from)
			entry.FromStage = &stage
		}
		entry.ToStage = model.Stage(to)
		history = append(history, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate stage history: %w", err)
	}
	return history, nil
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

	activity, err := scanActivity(s.queryRow(ctx, sql, leadID, string(in.Type), in.Note))
	if err != nil {
		if isForeignKeyViolation(err) {
			return model.Activity{}, ErrNotFound
		}
		return model.Activity{}, fmt.Errorf("insert activity for lead %d: %w", leadID, err)
	}

	return activity, nil
}

// editableActivityTypes are the entries a user may correct or remove: the ones
// a user can log. Anything the server wrote is a record and stays as it is.
func editableActivityTypes() []string {
	out := make([]string, len(model.UserActivityTypes))
	for i, t := range model.UserActivityTypes {
		out[i] = string(t)
	}
	return out
}

// UpdateActivity replaces the text of a note, call, email or meeting. Its type
// and time stay as logged; edited_at records that it was changed, and is left
// alone when the text is the same.
//
// Returns ErrNotFound for a missing id, ErrActivityLocked for a system entry.
func (s *Store) UpdateActivity(ctx context.Context, id int64, note string) (model.Activity, error) {
	sql := fmt.Sprintf(`
		UPDATE activities
		SET edited_at = CASE WHEN note IS DISTINCT FROM $2 THEN now() ELSE edited_at END,
		    note = $2
		WHERE id = $1 AND type::text = ANY($3)
		RETURNING %s`, activityColumns)

	activity, err := scanActivity(s.queryRow(ctx, sql, id, note, editableActivityTypes()))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Activity{}, s.lockedOrMissingActivity(ctx, id)
		}
		return model.Activity{}, fmt.Errorf("update activity %d: %w", id, err)
	}
	return activity, nil
}

// DeleteActivity removes a note, call, email or meeting, with the same rules
// as UpdateActivity.
func (s *Store) DeleteActivity(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM activities WHERE id = $1 AND type::text = ANY($2)`,
		id, editableActivityTypes())
	if err != nil {
		return fmt.Errorf("delete activity %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return s.lockedOrMissingActivity(ctx, id)
	}
	return nil
}

// lockedOrMissingActivity explains why an edit or delete touched no row: the
// entry exists but is a system one, or it does not exist.
func (s *Store) lockedOrMissingActivity(ctx context.Context, id int64) error {
	var exists bool
	if err := s.queryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM activities WHERE id = $1)`, id,
	).Scan(&exists); err != nil {
		return fmt.Errorf("check activity %d exists: %w", id, err)
	}
	if exists {
		return ErrActivityLocked
	}
	return ErrNotFound
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

// insertStageChangeTx records a stage transition twice, in one transaction: as
// a timeline entry in the lead's own words ("Stage changed from Qualified to
// Proposal Sent.") and as a structured lead_stage_history row, which is what
// reports and the stage history card read.
//
// suffix is appended to the timeline note: the close reason, when there is one.
func insertStageChangeTx(ctx context.Context, tx pgx.Tx, leadID int64, from, to model.Stage, suffix string) error {
	note := fmt.Sprintf("Stage changed from %s to %s.", from.Label(), to.Label()) + suffix
	if err := insertActivityTx(ctx, tx, leadID, model.ActivityStageChange, note); err != nil {
		return err
	}
	if err := insertStageHistoryTx(ctx, tx, leadID, &from, to); err != nil {
		return err
	}
	// A won customer gets a thank-you page to rate the deal and refer a friend.
	if to == model.StageWon {
		if err := ensureCustomerLinkTx(ctx, tx, leadID, model.LinkFeedback); err != nil {
			return err
		}
	}
	// Every stage change passes through here, so this is the one place a
	// conversion event for Meta can be queued from.
	return queueConversionTx(ctx, tx, leadID, to)
}

// insertStageHistoryTx writes one lead_stage_history row. from is nil for the
// stage a lead is created in.
func insertStageHistoryTx(ctx context.Context, tx pgx.Tx, leadID int64, from *model.Stage, to model.Stage) error {
	var fromText *string
	if from != nil {
		s := string(*from)
		fromText = &s
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO lead_stage_history (lead_id, from_stage, to_stage)
		VALUES ($1, $2::lead_stage, $3::lead_stage)`,
		leadID, fromText, string(to))
	if err != nil {
		return fmt.Errorf("insert stage history for lead %d: %w", leadID, err)
	}
	return nil
}

// insertOwnerChangeTx writes the timeline entry for a lead changing hands,
// naming both people as they are called now. from and to are owner ids, nil
// for nobody; the caller only calls this when they differ.
func insertOwnerChangeTx(ctx context.Context, tx pgx.Tx, leadID int64, from, to *int64) error {
	var fromName, toName *string
	if err := tx.QueryRow(ctx, `
		SELECT (SELECT name FROM team_members WHERE id = $1),
		       (SELECT name FROM team_members WHERE id = $2)`,
		from, to,
	).Scan(&fromName, &toName); err != nil {
		return fmt.Errorf("look up owner names for lead %d: %w", leadID, err)
	}

	var note string
	switch {
	case fromName == nil && toName != nil:
		note = fmt.Sprintf("Owner set to %s.", *toName)
	case fromName != nil && toName == nil:
		note = fmt.Sprintf("Owner removed (was %s).", *fromName)
	case fromName != nil && toName != nil:
		note = fmt.Sprintf("Owner changed from %s to %s.", *fromName, *toName)
	default:
		note = "Owner removed."
	}
	return insertActivityTx(ctx, tx, leadID, model.ActivityOwnerChange, note)
}

// sameOwner reports whether two optional owner ids are the same person.
func sameOwner(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
