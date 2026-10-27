package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"crm-lead-management/backend/internal/model"
)

// Accounts and sessions. Password hashes are written here and read back only
// by LoginRecord, for the password check; they never reach a response.

// ErrLastAdmin means the change would leave no active admin who can log in. -> 409
var ErrLastAdmin = errors.New("store: the last active admin cannot be removed, demoted or switched off")

// LoginRecord is what a login checks.
type LoginRecord struct {
	Viewer        model.Viewer
	PasswordHash  string // "" when no password is set
	Active        bool
	SignupPending bool // signed themselves up; waiting for an admin
}

// LoginRecordByEmail finds a member by email for a login, or ErrNotFound.
func (s *Store) LoginRecordByEmail(ctx context.Context, email string) (LoginRecord, error) {
	var (
		r    LoginRecord
		role string
		hash *string
	)
	err := s.queryRow(ctx, `
		SELECT id, name, email, role, active, password_hash, signup_pending
		FROM team_members WHERE lower(email) = lower($1)`, email).
		Scan(&r.Viewer.MemberID, &r.Viewer.Name, &r.Viewer.Email, &role, &r.Active, &hash, &r.SignupPending)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, fmt.Errorf("login lookup: %w", err)
	}
	r.Viewer.Role = model.Role(role)
	if hash != nil {
		r.PasswordHash = *hash
	}
	return r, nil
}

// PasswordHash returns a member's password hash ("" when none), for checking
// the current password before a change.
func (s *Store) PasswordHash(ctx context.Context, memberID int64) (string, error) {
	var hash *string
	err := s.queryRow(ctx, `SELECT password_hash FROM team_members WHERE id = $1`, memberID).Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("read password hash: %w", err)
	}
	if hash == nil {
		return "", nil
	}
	return *hash, nil
}

// CreateSession stores a new session for a member and stamps their login.
func (s *Store) CreateSession(ctx context.Context, memberID int64, tokenHash []byte, ip, userAgent string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		INSERT INTO sessions (token_hash, member_id, expires_at, ip, user_agent)
		VALUES ($1, $2, now() + $3::interval, $4, $5)`,
		tokenHash, memberID, model.SessionMaxAge.String(), truncateRunes(ip, 64), truncateRunes(userAgent, 300)); err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE team_members SET last_login_at = now() WHERE id = $1`, memberID); err != nil {
		return fmt.Errorf("stamp login: %w", err)
	}
	// Tidy: sessions that ended long ago, for everyone.
	if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE expires_at < now() - interval '1 day'`); err != nil {
		return fmt.Errorf("prune sessions: %w", err)
	}
	return tx.Commit(ctx)
}

// SessionViewer returns who a session token (by its hash) belongs to, or
// ErrNotFound when it is unknown, expired, idle too long, or its member was
// switched off. A live session's last use is moved on, at most once a minute.
func (s *Store) SessionViewer(ctx context.Context, tokenHash []byte) (model.Viewer, error) {
	var (
		v        model.Viewer
		role     string
		lastSeen time.Time
		id       int64
	)
	err := s.queryRow(ctx, `
		SELECT s.id, s.last_seen_at, m.id, m.name, m.email, m.role
		FROM sessions s JOIN team_members m ON m.id = s.member_id
		WHERE s.token_hash = $1
		  AND s.expires_at > now()
		  AND s.last_seen_at > now() - $2::interval
		  AND m.active AND m.password_hash IS NOT NULL`,
		tokenHash, model.SessionIdle.String()).
		Scan(&id, &lastSeen, &v.MemberID, &v.Name, &v.Email, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, fmt.Errorf("read session: %w", err)
	}
	v.Role = model.Role(role)
	if time.Since(lastSeen) > time.Minute {
		if _, err := s.pool.Exec(ctx, `UPDATE sessions SET last_seen_at = now() WHERE id = $1`, id); err != nil {
			return v, fmt.Errorf("touch session: %w", err)
		}
	}
	return v, nil
}

// DeleteSession ends one session (logout). Unknown tokens are fine.
func (s *Store) DeleteSession(ctx context.Context, tokenHash []byte) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// DeleteSessionsOf ends a member's sessions, keeping one (the caller's own,
// after changing their own password) when keep is not nil.
func (s *Store) DeleteSessionsOf(ctx context.Context, memberID int64, keep []byte) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE member_id = $1 AND ($2::bytea IS NULL OR token_hash <> $2)`,
		memberID, keep)
	if err != nil {
		return fmt.Errorf("end sessions: %w", err)
	}
	return nil
}

// SetPasswordHash stores a member's new password hash and ends their other
// sessions (all of them unless keep is given).
func (s *Store) SetPasswordHash(ctx context.Context, memberID int64, hash string, keep []byte) error {
	tag, err := s.pool.Exec(ctx, `UPDATE team_members SET password_hash = $2 WHERE id = $1`, memberID, hash)
	if err != nil {
		return fmt.Errorf("set password: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return s.DeleteSessionsOf(ctx, memberID, keep)
}

// UpdateAccount changes a member's role or whether they may log in. Switching
// someone off ends their sessions. The last active admin with a password can
// be neither demoted nor switched off, so the CRM always has a way in.
func (s *Store) UpdateAccount(ctx context.Context, memberID int64, in model.AccountInput) (model.TeamMember, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.TeamMember{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	// Serialise account changes, so two admins demoting each other at once
	// cannot both pass the last-admin check.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('team_members:accounts', 0))`); err != nil {
		return model.TeamMember{}, fmt.Errorf("lock accounts: %w", err)
	}
	var role string
	var active bool
	err = tx.QueryRow(ctx, `SELECT role, active FROM team_members WHERE id = $1`, memberID).Scan(&role, &active)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.TeamMember{}, ErrNotFound
	}
	if err != nil {
		return model.TeamMember{}, fmt.Errorf("read account: %w", err)
	}
	newRole, newActive := model.Role(role), active
	if in.Role != nil {
		newRole = *in.Role
	}
	if in.Active != nil {
		newActive = *in.Active
	}
	if model.Role(role) == model.RoleAdmin && active && (newRole != model.RoleAdmin || !newActive) {
		if err := lastAdminCheckTx(ctx, tx, memberID); err != nil {
			return model.TeamMember{}, err
		}
	}
	// Switching someone on is also how a sign-up is approved.
	if _, err := tx.Exec(ctx, `
		UPDATE team_members
		SET role = $2, active = $3, signup_pending = signup_pending AND NOT $3
		WHERE id = $1`,
		memberID, string(newRole), newActive); err != nil {
		return model.TeamMember{}, fmt.Errorf("update account: %w", err)
	}
	if !newActive || newRole != model.Role(role) {
		// A changed role takes effect at once: end their sessions.
		if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE member_id = $1`, memberID); err != nil {
			return model.TeamMember{}, fmt.Errorf("end sessions: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return model.TeamMember{}, fmt.Errorf("commit account: %w", err)
	}
	return s.GetTeamMember(ctx, memberID)
}

// lastAdminCheckTx refuses when no other active admin with a password exists.
func lastAdminCheckTx(ctx context.Context, tx pgx.Tx, except int64) error {
	var others int
	err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM team_members
		WHERE role = 'admin' AND active AND password_hash IS NOT NULL AND id <> $1`, except).Scan(&others)
	if err != nil {
		return fmt.Errorf("count admins: %w", err)
	}
	if others == 0 {
		return ErrLastAdmin
	}
	return nil
}

// CanDeleteMember refuses to delete the last active admin.
func (s *Store) CanDeleteMember(ctx context.Context, memberID int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	var role string
	var active bool
	err = tx.QueryRow(ctx, `SELECT role, active FROM team_members WHERE id = $1`, memberID).Scan(&role, &active)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("read account: %w", err)
	}
	if model.Role(role) == model.RoleAdmin && active {
		return lastAdminCheckTx(ctx, tx, memberID)
	}
	return nil
}

// UpsertAccount creates a member with a role and password, or gives an
// existing member (matched by email) that role and password. Used by the
// command-line tool that makes the first admin.
func (s *Store) UpsertAccount(ctx context.Context, name, email string, role model.Role, hash string) (model.TeamMember, bool, error) {
	var (
		id      int64
		created bool
	)
	err := s.queryRow(ctx, `
		INSERT INTO team_members (name, email, role, password_hash, active)
		VALUES ($1, lower($2), $3, $4, true)
		ON CONFLICT (lower(email)) DO UPDATE
		SET role = EXCLUDED.role, password_hash = EXCLUDED.password_hash, active = true
		RETURNING id, (xmax = 0)`, name, email, string(role), hash).Scan(&id, &created)
	if err != nil {
		return model.TeamMember{}, false, fmt.Errorf("save account: %w", err)
	}
	if err := s.DeleteSessionsOf(ctx, id, nil); err != nil {
		return model.TeamMember{}, false, err
	}
	m, err := s.GetTeamMember(ctx, id)
	return m, created, err
}

// CreateSignup records someone signing themselves up: a Sales member who can
// log in at once. As Sales they see only leads assigned to them, which for a
// new account is none. An email that already belongs to a member is refused
// (ErrDuplicateMemberEmail) and that member left untouched, so nobody can take
// over an existing account by signing up with its email.
func (s *Store) CreateSignup(ctx context.Context, name, email, hash string) (model.Viewer, error) {
	v := model.Viewer{Name: name, Role: model.RoleSales}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO team_members (name, email, role, password_hash, active, signup_pending)
		VALUES ($1, lower($2), 'sales', $3, true, false)
		ON CONFLICT (lower(email)) DO NOTHING
		RETURNING id, email`, name, email, hash).Scan(&v.MemberID, &v.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrDuplicateMemberEmail
	}
	if err != nil {
		return v, fmt.Errorf("sign up: %w", err)
	}
	return v, nil
}

// CreatePasswordReset stores a reset token (by its hash) for a member, good
// for model.PasswordResetTTL, and cancels any earlier unused one.
func (s *Store) CreatePasswordReset(ctx context.Context, memberID int64, tokenHash []byte) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE password_resets SET used_at = now() WHERE member_id = $1 AND used_at IS NULL`, memberID); err != nil {
		return fmt.Errorf("cancel old resets: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO password_resets (token_hash, member_id, expires_at) VALUES ($1, $2, now() + $3::interval)`,
		tokenHash, memberID, model.PasswordResetTTL.String()); err != nil {
		return fmt.Errorf("create reset: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM password_resets WHERE expires_at < now() - interval '7 days'`); err != nil {
		return fmt.Errorf("prune resets: %w", err)
	}
	return tx.Commit(ctx)
}

// UsePasswordReset sets a new password with a reset token: once, before it
// expires, for a member still allowed to log in. Every session of theirs
// ends. ErrNotFound when the token is unknown, used or expired.
func (s *Store) UsePasswordReset(ctx context.Context, tokenHash []byte, newHash string) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	var id, memberID int64
	err = tx.QueryRow(ctx, `
		SELECT r.id, r.member_id
		FROM password_resets r JOIN team_members m ON m.id = r.member_id
		WHERE r.token_hash = $1 AND r.used_at IS NULL AND r.expires_at > now() AND m.active
		FOR UPDATE OF r`, tokenHash).Scan(&id, &memberID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("read reset: %w", err)
	}
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`UPDATE password_resets SET used_at = now() WHERE id = $1`, []any{id}},
		{`UPDATE team_members SET password_hash = $2 WHERE id = $1`, []any{memberID, newHash}},
		{`DELETE FROM sessions WHERE member_id = $1`, []any{memberID}},
	} {
		if _, err := tx.Exec(ctx, q.sql, q.args...); err != nil {
			return 0, fmt.Errorf("reset password: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit reset: %w", err)
	}
	return memberID, nil
}
