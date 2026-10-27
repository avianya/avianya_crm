package store

import (
	"context"
	"testing"

	"crm-lead-management/backend/internal/model"
)

// taskTitles lists the task titles in a bucket, in order.
func taskTitles(items []model.TaskWithLead) []string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = item.Task.Title
	}
	return out
}

func leadIDs(leads []model.Lead) []int64 {
	out := make([]int64, len(leads))
	for i, lead := range leads {
		out[i] = lead.ID
	}
	return out
}

func TestFollowUpsBucketsAndOwnerFilter(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	asha := createTestMember(t, st, "Asha FollowUp")
	ravi := createTestMember(t, st, "Ravi FollowUp")

	var yesterday, today, inThree, inTen string
	if err := st.pool.QueryRow(ctx, `
		SELECT (CURRENT_DATE - 1)::text, CURRENT_DATE::text,
		       (CURRENT_DATE + 3)::text, (CURRENT_DATE + 10)::text`,
	).Scan(&yesterday, &today, &inThree, &inTen); err != nil {
		t.Fatalf("read dates: %v", err)
	}

	// Asha's lead, with tasks spread across the week. New tasks go to the
	// lead's owner, so all four are assigned to Asha.
	busy := uniqueInput(t, "followup-busy")
	busy.OwnerID = &asha.ID
	busyLead := createTestLead(t, st, busy)
	createTestTask(t, st, busyLead.ID, "late call", yesterday)
	createTestTask(t, st, busyLead.ID, "today call", today)
	createTestTask(t, st, busyLead.ID, "midweek call", inThree)
	createTestTask(t, st, busyLead.ID, "far call", inTen) // beyond the week: left out
	done := createTestTask(t, st, busyLead.ID, "done call", yesterday)
	if _, _, err := st.SetTaskCompleted(ctx, done.ID, true); err != nil {
		t.Fatalf("complete task: %v", err)
	}

	// Asha's quiet lead: Negotiation, untouched for 20 days, nothing planned.
	quiet := uniqueInput(t, "followup-quiet")
	quiet.OwnerID = &asha.ID
	quiet.Stage = model.StageNegotiation
	quietLead := createTestLead(t, st, quiet)
	if _, err := st.pool.Exec(ctx,
		`UPDATE leads SET created_at = now() - interval '20 days' WHERE id = $1`, quietLead.ID); err != nil {
		t.Fatalf("backdate lead: %v", err)
	}
	if _, err := st.pool.Exec(ctx,
		`UPDATE activities SET created_at = now() - interval '20 days' WHERE lead_id = $1`, quietLead.ID); err != nil {
		t.Fatalf("backdate activities: %v", err)
	}

	// Asha's won deal: closed, so never on the attention lists.
	won := uniqueInput(t, "followup-won")
	won.OwnerID = &asha.ID
	won.Stage = model.StageWon
	wonLead := createTestLead(t, st, won)

	// Ravi's lead, which must not leak into Asha's view.
	ravis := uniqueInput(t, "followup-ravi")
	ravis.OwnerID = &ravi.ID
	raviLead := createTestLead(t, st, ravis)
	createTestTask(t, st, raviLead.ID, "ravi call", today)

	summary, err := st.FollowUps(ctx, model.OwnerFilter{OwnerID: &asha.ID})
	if err != nil {
		t.Fatalf("FollowUps: %v", err)
	}

	if summary.Today != today {
		t.Errorf("today = %q, want %q", summary.Today, today)
	}
	checks := []struct {
		name string
		got  []string
		want []string
	}{
		{"overdue", taskTitles(summary.Overdue), []string{"late call"}},
		{"due today", taskTitles(summary.DueToday), []string{"today call"}},
		{"this week", taskTitles(summary.ThisWeek), []string{"midweek call"}},
	}
	for _, c := range checks {
		if !equalStrings(c.got, c.want) {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}

	if ids := leadIDs(summary.GoingCold); len(ids) != 1 || ids[0] != quietLead.ID {
		t.Errorf("going cold = %v, want only the quiet lead %d", ids, quietLead.ID)
	}
	if ids := leadIDs(summary.NoNextStep); len(ids) != 1 || ids[0] != quietLead.ID {
		t.Errorf("no next step = %v, want only the quiet lead %d (busy has tasks, %d is won)",
			ids, quietLead.ID, wonLead.ID)
	}

	// Ravi sees only his own.
	summary, err = st.FollowUps(ctx, model.OwnerFilter{OwnerID: &ravi.ID})
	if err != nil {
		t.Fatalf("FollowUps (Ravi): %v", err)
	}
	if got := taskTitles(summary.DueToday); !equalStrings(got, []string{"ravi call"}) {
		t.Errorf("Ravi due today = %q", got)
	}
	if len(summary.Overdue) != 0 || len(summary.GoingCold) != 0 {
		t.Errorf("Ravi sees Asha's work: %d overdue, %d going cold", len(summary.Overdue), len(summary.GoingCold))
	}

	// Everyone's view includes both people's work.
	summary, err = st.FollowUps(ctx, model.OwnerFilter{})
	if err != nil {
		t.Fatalf("FollowUps (everyone): %v", err)
	}
	seen := map[string]bool{}
	for _, title := range taskTitles(summary.DueToday) {
		seen[title] = true
	}
	if !seen["today call"] || !seen["ravi call"] {
		t.Errorf("everyone's due today is missing a task: %v", taskTitles(summary.DueToday))
	}
}
