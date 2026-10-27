package store

import (
	"context"
	"testing"

	"crm-lead-management/backend/internal/model"
)

// claimedFor reports whether a claim list includes the member, and the day.
func claimedFor(claims []DigestClaim, memberID int64) (string, bool) {
	for _, c := range claims {
		if c.Member.ID == memberID {
			return c.Date, true
		}
	}
	return "", false
}

func lastDigestOf(t *testing.T, st *Store, memberID int64) *model.DigestRecord {
	t.Helper()
	team, err := st.ListTeam(context.Background())
	if err != nil {
		t.Fatalf("ListTeam: %v", err)
	}
	for _, m := range team {
		if m.ID == memberID {
			return m.LastDigest
		}
	}
	t.Fatalf("member %d not in the team list", memberID)
	return nil
}

func TestDailyDigestIsClaimedOncePerDay(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	member := createTestMember(t, st, "Digest Daily")
	optedOut := createTestMember(t, st, "Digest Opted Out")
	off := false
	if _, err := st.UpdateTeamMember(ctx, optedOut.ID, model.TeamMemberInput{
		Name: optedOut.Name, Email: optedOut.Email, EmailReminders: &off, DailyDigest: &off,
	}); err != nil {
		t.Fatalf("turn digest off: %v", err)
	}

	if got := lastDigestOf(t, st, member.ID); got != nil {
		t.Fatalf("new member already has a digest: %+v", got)
	}

	// Before the hour: nothing. (No local hour reaches 24.)
	claims, err := st.ClaimDailyDigests(ctx, 24)
	if err != nil {
		t.Fatalf("ClaimDailyDigests(24): %v", err)
	}
	if _, ok := claimedFor(claims, member.ID); ok {
		t.Fatal("claimed before the digest hour")
	}

	claims, err = st.ClaimDailyDigests(ctx, 0)
	if err != nil {
		t.Fatalf("ClaimDailyDigests: %v", err)
	}
	date, ok := claimedFor(claims, member.ID)
	if !ok {
		t.Fatal("member's digest was not claimed")
	}
	if _, ok := claimedFor(claims, optedOut.ID); ok {
		t.Error("claimed a digest for someone who turned it off")
	}

	// A second tick the same day claims nothing while it is being sent.
	again, err := st.ClaimDailyDigests(ctx, 0)
	if err != nil {
		t.Fatalf("ClaimDailyDigests (again): %v", err)
	}
	if _, ok := claimedFor(again, member.ID); ok {
		t.Fatal("claimed twice in one day")
	}

	// A failure is not retried at once...
	if err := st.FinishDailyDigest(ctx, member.ID, date, model.DigestFailed, "mail server said no"); err != nil {
		t.Fatalf("FinishDailyDigest: %v", err)
	}
	again, _ = st.ClaimDailyDigests(ctx, 0)
	if _, ok := claimedFor(again, member.ID); ok {
		t.Fatal("failed digest retried immediately")
	}

	// ...but is after 15 minutes, as attempt 2.
	if _, err := st.pool.Exec(ctx, `
		UPDATE daily_digests SET updated_at = now() - interval '20 minutes'
		WHERE member_id = $1`, member.ID); err != nil {
		t.Fatalf("age the failure: %v", err)
	}
	again, _ = st.ClaimDailyDigests(ctx, 0)
	if _, ok := claimedFor(again, member.ID); !ok {
		t.Fatal("failed digest not retried after 15 minutes")
	}
	if got := lastDigestOf(t, st, member.ID); got == nil || got.Attempts != 2 || got.Status != model.DigestSending {
		t.Errorf("after retry claim: %+v, want attempt 2, sending", got)
	}

	if err := st.FinishDailyDigest(ctx, member.ID, date, model.DigestSent, "Sent to x"); err != nil {
		t.Fatalf("FinishDailyDigest: %v", err)
	}
	got := lastDigestOf(t, st, member.ID)
	if got == nil || got.Status != model.DigestSent || got.Date != date || got.Detail != "Sent to x" {
		t.Errorf("last digest = %+v, want sent today", got)
	}

	// Sent is final for the day.
	if _, err := st.pool.Exec(ctx, `
		UPDATE daily_digests SET updated_at = now() - interval '2 hours'
		WHERE member_id = $1`, member.ID); err != nil {
		t.Fatalf("age the send: %v", err)
	}
	again, _ = st.ClaimDailyDigests(ctx, 0)
	if _, ok := claimedFor(again, member.ID); ok {
		t.Error("a sent digest was claimed again")
	}
}

func TestTeamMemberDailyDigestDefaultsOn(t *testing.T) {
	st := newTestStore(t)
	member := createTestMember(t, st, "Digest Default")
	if !member.DailyDigest {
		t.Error("new member has the daily summary off")
	}
}
