package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"crm-lead-management/backend/internal/model"
)

// Round-robin lead assignment (see model/assignment.go). The setting lives in
// app_settings; autoAssignTx reads it with FOR UPDATE, so two leads arriving
// at the same moment still go to two different people in turn.

const leadAssignmentKey = "lead_assignment"

// GetLeadAssignment returns the saved setting, or "off" if never saved.
func (s *Store) GetLeadAssignment(ctx context.Context) (model.LeadAssignment, error) {
	setting, _, err := leadAssignment(ctx, s.pool, false)
	return setting, err
}

// leadAssignment reads the setting, locking its row when lock is set. found
// is false when it was never saved.
func leadAssignment(ctx context.Context, q querier, lock bool) (setting model.LeadAssignment, found bool, err error) {
	query := `SELECT value FROM app_settings WHERE key = $1`
	if lock {
		query += ` FOR UPDATE`
	}
	var raw []byte
	err = q.QueryRow(ctx, query, leadAssignmentKey).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.LeadAssignment{MemberIDs: []int64{}}, false, nil
	}
	if err != nil {
		return model.LeadAssignment{}, false, fmt.Errorf("get lead assignment: %w", err)
	}
	if err := json.Unmarshal(raw, &setting); err != nil {
		return model.LeadAssignment{}, false, fmt.Errorf("decode lead assignment: %w", err)
	}
	if setting.MemberIDs == nil {
		setting.MemberIDs = []int64{}
	}
	return setting, true, nil
}

// SaveLeadAssignment stores the setting. Whose turn it is carries over, so
// switching it off and on again does not start the rotation from the top.
func (s *Store) SaveLeadAssignment(ctx context.Context, in model.LeadAssignmentInput) (model.LeadAssignment, error) {
	var known int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM team_members WHERE id = ANY($1)`, in.MemberIDs,
	).Scan(&known); err != nil {
		return model.LeadAssignment{}, fmt.Errorf("check rotation members: %w", err)
	}
	if known != len(in.MemberIDs) {
		return model.LeadAssignment{}, unknownMember("member_ids")
	}

	current, _, err := leadAssignment(ctx, s.pool, false)
	if err != nil {
		return model.LeadAssignment{}, err
	}
	setting := model.LeadAssignment{
		Enabled:      in.Enabled,
		MemberIDs:    in.MemberIDs,
		LastMemberID: current.LastMemberID,
	}
	if err := saveLeadAssignment(ctx, s.pool, setting); err != nil {
		return model.LeadAssignment{}, err
	}
	return setting, nil
}

func saveLeadAssignment(ctx context.Context, q dbtx, setting model.LeadAssignment) error {
	raw, err := json.Marshal(setting)
	if err != nil {
		return fmt.Errorf("encode lead assignment: %w", err)
	}
	if _, err := q.Exec(ctx, `
		INSERT INTO app_settings (key, value) VALUES ($1, $2)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`,
		leadAssignmentKey, raw); err != nil {
		return fmt.Errorf("save lead assignment: %w", err)
	}
	return nil
}

// autoAssignTx gives a new lead nobody was chosen for to the next person in
// the rotation, and says so on its timeline. It does nothing when the
// rotation is off, or everyone in it is switched off.
func autoAssignTx(ctx context.Context, tx pgx.Tx, lead *model.Lead) error {
	if lead.OwnerID != nil {
		return nil
	}
	setting, found, err := leadAssignment(ctx, tx, true)
	if err != nil || !found || !setting.Enabled || len(setting.MemberIDs) == 0 {
		return err
	}

	var last int64
	if setting.LastMemberID != nil {
		last = *setting.LastMemberID
	}
	// The first active member after the last one, wrapping round to the start.
	var next int64
	var name string
	err = tx.QueryRow(ctx, `
		SELECT id, name FROM team_members
		WHERE id = ANY($1) AND active
		ORDER BY id <= $2, id
		LIMIT 1`, setting.MemberIDs, last).Scan(&next, &name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("pick next in rotation: %w", err)
	}

	if _, err := tx.Exec(ctx, `UPDATE leads SET owner_id = $2 WHERE id = $1`, lead.ID, next); err != nil {
		return fmt.Errorf("auto-assign lead %d: %w", lead.ID, err)
	}
	lead.OwnerID = &next

	note := fmt.Sprintf("Assigned to %s automatically (taking turns).", name)
	if err := insertActivityTx(ctx, tx, lead.ID, model.ActivityOwnerChange, note); err != nil {
		return err
	}

	setting.LastMemberID = &next
	return saveLeadAssignment(ctx, tx, setting)
}
