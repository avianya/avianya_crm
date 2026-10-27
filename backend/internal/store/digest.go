package store

import (
	"context"
	"fmt"

	"crm-lead-management/backend/internal/model"
)

// DigestClaim is one daily summary the reminder job has claimed and must now
// send (or record as skipped).
type DigestClaim struct {
	Member model.TeamMember
	Date   string // the day it is for, "2006-01-02"
}

// ClaimDailyDigests claims today's summary for every member who wants one and
// has not had it yet, once the local hour has reached fromHour. It also
// re-claims a failed one (up to model.MaxDigestAttempts, at most every 15
// minutes) and one stuck in 'sending' for over 10 minutes, after a crash.
//
// Claiming is an INSERT ... ON CONFLICT on (member, day), so two servers can
// never both send the same person's summary.
func (s *Store) ClaimDailyDigests(ctx context.Context, fromHour int) ([]DigestClaim, error) {
	rows, err := s.query(ctx, fmt.Sprintf(`
		INSERT INTO daily_digests (member_id, digest_date, status, attempts)
		SELECT id, CURRENT_DATE, 'sending', 1
		FROM team_members
		WHERE daily_digest
		  AND extract(hour FROM localtimestamp) >= $1
		ON CONFLICT (member_id, digest_date) DO UPDATE
		SET status = 'sending',
		    attempts = daily_digests.attempts + 1,
		    updated_at = now()
		WHERE (daily_digests.status = 'failed'
		       AND daily_digests.attempts < %d
		       AND daily_digests.updated_at < now() - interval '15 minutes')
		   OR (daily_digests.status = 'sending'
		       AND daily_digests.updated_at < now() - interval '10 minutes')
		RETURNING member_id, digest_date::text`, model.MaxDigestAttempts), fromHour)
	if err != nil {
		return nil, fmt.Errorf("claim daily digests: %w", err)
	}

	type claimed struct {
		memberID int64
		date     string
	}
	var ids []claimed
	for rows.Next() {
		var c claimed
		if err := rows.Scan(&c.memberID, &c.date); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan digest claim: %w", err)
		}
		ids = append(ids, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate digest claims: %w", err)
	}

	claims := make([]DigestClaim, 0, len(ids))
	for _, c := range ids {
		member, err := s.GetTeamMember(ctx, c.memberID)
		if err != nil {
			// Removed in the meantime: their row went with them (CASCADE).
			continue
		}
		claims = append(claims, DigestClaim{Member: member, Date: c.date})
	}
	return claims, nil
}

// FinishDailyDigest records how a claimed summary went.
func (s *Store) FinishDailyDigest(ctx context.Context, memberID int64, date string, status model.DigestStatus, detail string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE daily_digests
		SET status = $3, detail = $4, updated_at = now()
		WHERE member_id = $1 AND digest_date = $2::date`,
		memberID, date, string(status), detail)
	if err != nil {
		return fmt.Errorf("finish daily digest for member %d: %w", memberID, err)
	}
	return nil
}

// attachLastDigests fills in each member's most recent summary, in one query.
func (s *Store) attachLastDigests(ctx context.Context, members []model.TeamMember) error {
	if len(members) == 0 {
		return nil
	}
	rows, err := s.query(ctx, `
		SELECT DISTINCT ON (member_id)
		       member_id, digest_date::text, status, attempts, detail, updated_at
		FROM daily_digests
		ORDER BY member_id, digest_date DESC`)
	if err != nil {
		return fmt.Errorf("last digests: %w", err)
	}
	defer rows.Close()

	last := make(map[int64]*model.DigestRecord)
	for rows.Next() {
		var (
			id     int64
			r      model.DigestRecord
			status string
		)
		if err := rows.Scan(&id, &r.Date, &status, &r.Attempts, &r.Detail, &r.UpdatedAt); err != nil {
			return fmt.Errorf("scan last digest: %w", err)
		}
		r.Status = model.DigestStatus(status)
		last[id] = &r
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate last digests: %w", err)
	}

	for i := range members {
		members[i].LastDigest = last[members[i].ID]
	}
	return nil
}
