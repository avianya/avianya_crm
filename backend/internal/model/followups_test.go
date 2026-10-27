package model

import (
	"testing"
	"time"
)

func TestSplitFollowUpTasks(t *testing.T) {
	task := func(id int64, due string, status TaskStatus) TaskWithLead {
		return TaskWithLead{Task: Task{ID: id, DueDate: due, Status: status}}
	}
	items := []TaskWithLead{
		task(1, "2026-10-08", TaskOverdue),
		task(2, "2026-10-10", TaskOverdue), // a call at 10:00, now past
		task(3, "2026-10-10", TaskUpcoming),
		task(4, "2026-10-13", TaskUpcoming),
	}

	overdue, today, week := SplitFollowUpTasks(items, "2026-10-10")
	ids := func(list []TaskWithLead) []int64 {
		out := []int64{}
		for _, item := range list {
			out = append(out, item.Task.ID)
		}
		return out
	}
	want := map[string][2][]int64{
		"overdue": {ids(overdue), {1, 2}},
		"today":   {ids(today), {3}},
		"week":    {ids(week), {4}},
	}
	for name, pair := range want {
		got, exp := pair[0], pair[1]
		if len(got) != len(exp) {
			t.Errorf("%s = %v, want %v", name, got, exp)
			continue
		}
		for i := range got {
			if got[i] != exp[i] {
				t.Errorf("%s = %v, want %v", name, got, exp)
				break
			}
		}
	}

	// Empty input still gives non-nil lists, so the JSON has [] not null.
	o, d, w := SplitFollowUpTasks(nil, "2026-10-10")
	if o == nil || d == nil || w == nil {
		t.Error("empty buckets must be non-nil")
	}
}

func TestSplitAttentionLeads(t *testing.T) {
	lead := func(id int64, stage Stage, days int, hasTask bool, score int) Lead {
		l := Lead{ID: id, Stage: stage, DaysSinceTouch: days, HasOpenTask: hasTask}
		l.Health = HealthFor(stage, days)
		// For this test, time in stage equals time since contact.
		now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
		l.ApplyStageAge(now.Add(-time.Duration(days)*24*time.Hour), now)
		if !stage.IsClosed() {
			l.Score = &LeadScore{Total: score}
		}
		return l
	}
	leads := []Lead{
		lead(1, StageQualified, 10, true, 50),    // cooling (7..14): 10/14 = 0.71
		lead(2, StageNegotiation, 6, true, 70),   // cooling (3..7): 6/7 = 0.86, ranks first
		lead(3, StageNew, 0, false, 20),          // active, but nothing planned
		lead(4, StageContacted, 1, false, 60),    // active, nothing planned, better score
		lead(5, StageWon, 90, false, 0),          // closed: ignored entirely
		lead(6, StageProposalSent, 30, false, 5), // cold and nothing planned: in both
	}

	cold, none, stuck := SplitAttentionLeads(leads)

	gotCold := []int64{}
	for _, l := range cold {
		gotCold = append(gotCold, l.ID)
	}
	// 6: 30/10 = 3.0, then 2: 0.86, then 1: 0.71.
	if want := []int64{6, 2, 1}; !equalIDs(gotCold, want) {
		t.Errorf("going cold = %v, want %v", gotCold, want)
	}

	gotNone := []int64{}
	for _, l := range none {
		gotNone = append(gotNone, l.ID)
	}
	if want := []int64{4, 3, 6}; !equalIDs(gotNone, want) {
		t.Errorf("no next step = %v, want %v (best score first)", gotNone, want)
	}

	gotStuck := []int64{}
	for _, l := range stuck {
		gotStuck = append(gotStuck, l.ID)
	}
	// 6: 30 days in Proposal Sent (limit 10) = 3.0; 1: 10 in Qualified (limit
	// 14) is not stuck yet; 2: 6 in Negotiation (limit 14) is not either.
	if want := []int64{6}; !equalIDs(gotStuck, want) {
		t.Errorf("stuck = %v, want %v", gotStuck, want)
	}
}

func equalIDs(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
