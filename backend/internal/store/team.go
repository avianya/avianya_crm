package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"crm-lead-management/backend/internal/model"
)

// teamColumns never includes password_hash: whether one is set is all that
// leaves the database.
const teamColumns = `id, name, email, email_reminders, default_reminder_minutes, daily_digest,
	role, active, password_hash IS NOT NULL, last_login_at, signup_pending, created_at`

func scanMember(row rowScanner) (model.TeamMember, error) {
	var (
		m    model.TeamMember
		role string
	)
	err := row.Scan(&m.ID, &m.Name, &m.Email, &m.EmailReminders, &m.DefaultReminderMinutes, &m.DailyDigest,
		&role, &m.Active, &m.HasLogin, &m.LastLoginAt, &m.SignupPending, &m.CreatedAt)
	m.Role = model.Role(role)
	return m, err
}

// ListTeam returns every team member, by name. Never nil.
func (s *Store) ListTeam(ctx context.Context) ([]model.TeamMember, error) {
	rows, err := s.query(ctx,
		fmt.Sprintf(`SELECT %s FROM team_members ORDER BY lower(name), id`, teamColumns))
	if err != nil {
		return nil, fmt.Errorf("list team: %w", err)
	}
	defer rows.Close()

	members := make([]model.TeamMember, 0, 8)
	for rows.Next() {
		m, err := scanMember(rows)
		if err != nil {
			return nil, fmt.Errorf("scan team member: %w", err)
		}
		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate team: %w", err)
	}
	if err := s.attachLastDigests(ctx, members); err != nil {
		return nil, err
	}
	return members, nil
}

// GetTeamMember returns one member, or ErrNotFound.
func (s *Store) GetTeamMember(ctx context.Context, id int64) (model.TeamMember, error) {
	m, err := scanMember(s.queryRow(ctx,
		fmt.Sprintf(`SELECT %s FROM team_members WHERE id = $1`, teamColumns), id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.TeamMember{}, ErrNotFound
		}
		return model.TeamMember{}, fmt.Errorf("get team member %d: %w", id, err)
	}
	return m, nil
}

// CreateTeamMember adds a member. Returns ErrDuplicateMemberEmail when the
// email is taken.
func (s *Store) CreateTeamMember(ctx context.Context, in model.TeamMemberInput) (model.TeamMember, error) {
	sql := fmt.Sprintf(`
		INSERT INTO team_members (name, email, email_reminders, default_reminder_minutes, daily_digest)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING %s`, teamColumns)

	m, err := scanMember(s.queryRow(ctx, sql,
		in.Name, in.Email, *in.EmailReminders, in.DefaultReminderMinutes, *in.DailyDigest))
	if err != nil {
		if isUniqueViolation(err, "team_members_email_lower_key") {
			return model.TeamMember{}, ErrDuplicateMemberEmail
		}
		return model.TeamMember{}, fmt.Errorf("insert team member: %w", err)
	}
	return m, nil
}

// UpdateTeamMember replaces a member's fields.
func (s *Store) UpdateTeamMember(ctx context.Context, id int64, in model.TeamMemberInput) (model.TeamMember, error) {
	sql := fmt.Sprintf(`
		UPDATE team_members
		SET name = $2, email = $3, email_reminders = $4, default_reminder_minutes = $5, daily_digest = $6
		WHERE id = $1
		RETURNING %s`, teamColumns)

	m, err := scanMember(s.queryRow(ctx, sql,
		id, in.Name, in.Email, *in.EmailReminders, in.DefaultReminderMinutes, *in.DailyDigest))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.TeamMember{}, ErrNotFound
		}
		if isUniqueViolation(err, "team_members_email_lower_key") {
			return model.TeamMember{}, ErrDuplicateMemberEmail
		}
		return model.TeamMember{}, fmt.Errorf("update team member %d: %w", id, err)
	}
	return m, nil
}

// DeleteTeamMember removes a member. Their leads and events are left unowned
// and unassigned (ON DELETE SET NULL), never deleted.
func (s *Store) DeleteTeamMember(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM team_members WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete team member %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
