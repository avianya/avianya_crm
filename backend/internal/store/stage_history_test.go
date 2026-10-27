package store

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"crm-lead-management/backend/internal/model"
)

// stagePath is a lead's history as "from>to" pairs, with "" for the first
// entry's missing from-stage -- compact enough to compare in one line.
func stagePath(history []model.StageHistoryEntry) []string {
	out := make([]string, len(history))
	for i, h := range history {
		from := ""
		if h.FromStage != nil {
			from = string(*h.FromStage)
		}
		out[i] = from + ">" + string(h.ToStage)
	}
	return out
}

func equalStrings(a, b []string) bool {
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

func TestStageHistoryRecordsEveryMove(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	lead := createTestLead(t, st, uniqueInput(t, "history"))

	if _, err := st.UpdateLeadStage(ctx, lead.ID, model.StageInput{Stage: model.StageContacted}); err != nil {
		t.Fatalf("UpdateLeadStage: %v", err)
	}
	// Moving to the stage it is already in is not a move.
	if _, err := st.UpdateLeadStage(ctx, lead.ID, model.StageInput{Stage: model.StageContacted}); err != nil {
		t.Fatalf("UpdateLeadStage (same stage): %v", err)
	}
	// A stage change made through the full edit form is recorded too, and the
	// response carries the fresh history.
	in := uniqueInput(t, "history")
	in.Stage = model.StageQualified
	in.Normalize()
	updated, err := st.UpdateLead(ctx, lead.ID, in)
	if err != nil {
		t.Fatalf("UpdateLead: %v", err)
	}

	want := []string{">new", "new>contacted", "contacted>qualified"}
	if got := stagePath(updated.StageHistory); !equalStrings(got, want) {
		t.Errorf("UpdateLead history = %v, want %v", got, want)
	}

	fetched, err := st.GetLead(ctx, lead.ID)
	if err != nil {
		t.Fatalf("GetLead: %v", err)
	}
	if got := stagePath(fetched.StageHistory); !equalStrings(got, want) {
		t.Errorf("GetLead history = %v, want %v", got, want)
	}

	// A lead created straight into a later stage starts its history there.
	later := uniqueInput(t, "history-later")
	later.Stage = model.StageProposalSent
	created := createTestLead(t, st, later)
	fetched, err = st.GetLead(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetLead: %v", err)
	}
	if got, want := stagePath(fetched.StageHistory), []string{">proposal_sent"}; !equalStrings(got, want) {
		t.Errorf("created-into-stage history = %v, want %v", got, want)
	}
}

func TestStageDurationsComeFromHistory(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	before, err := st.Reports(ctx)
	if err != nil {
		t.Fatalf("Reports: %v", err)
	}

	// Lead A: New 2 days, Contacted 3 days, Qualified 4 days, then Won, then
	// Lost -- time in Won is not a stint.
	a := createTestLead(t, st, uniqueInput(t, "durations-a"))
	for _, stage := range []model.Stage{model.StageContacted, model.StageQualified, model.StageWon, model.StageLost} {
		in := model.StageInput{Stage: stage}
		if stage == model.StageLost {
			reason := model.CloseReason("timing")
			in.CloseInput.Reason = &reason // losing a deal needs a reason
		}
		if _, err := st.UpdateLeadStage(ctx, a.ID, in); err != nil {
			t.Fatalf("UpdateLeadStage(%s): %v", stage, err)
		}
	}
	// Lead B: created into Proposal Sent, 3 days there, now in Negotiation
	// (unfinished, so not counted). No New stint at all.
	inB := uniqueInput(t, "durations-b")
	inB.Stage = model.StageProposalSent
	b := createTestLead(t, st, inB)
	if _, err := st.UpdateLeadStage(ctx, b.ID, model.StageInput{Stage: model.StageNegotiation}); err != nil {
		t.Fatalf("UpdateLeadStage: %v", err)
	}

	// Every row was written "now"; spread them out to known gaps.
	base := time.Now().Add(-60 * 24 * time.Hour)
	setDays := func(leadID int64, days ...float64) {
		t.Helper()
		history, err := st.ListStageHistory(ctx, leadID)
		if err != nil {
			t.Fatalf("ListStageHistory: %v", err)
		}
		if len(history) != len(days) {
			t.Fatalf("lead %d has %d history rows, want %d", leadID, len(history), len(days))
		}
		for i, h := range history {
			at := base.Add(time.Duration(days[i] * 24 * float64(time.Hour)))
			if _, err := st.pool.Exec(ctx,
				`UPDATE lead_stage_history SET changed_at = $1 WHERE id = $2`, at, h.ID); err != nil {
				t.Fatalf("set changed_at: %v", err)
			}
		}
	}
	setDays(a.ID, 0, 2, 5, 9, 30)
	setDays(b.ID, 0, 3)

	after, err := st.Reports(ctx)
	if err != nil {
		t.Fatalf("Reports: %v", err)
	}

	// Other rows in the test database share these averages, so check each
	// stage's change: one more sample, and the average moved as one more stint
	// of the expected length would move it.
	expect := map[model.Stage]struct {
		samples int
		days    float64
	}{
		model.StageNew:          {1, 2},
		model.StageContacted:    {1, 3},
		model.StageQualified:    {1, 4},
		model.StageProposalSent: {1, 3},
		model.StageNegotiation:  {0, 0},
	}
	if len(after.StageDurations) != len(expect) {
		t.Fatalf("got %d stage durations, want %d (open stages only)", len(after.StageDurations), len(expect))
	}
	for i, d := range after.StageDurations {
		prev := before.StageDurations[i]
		e := expect[d.Stage]
		if got := d.Samples - prev.Samples; got != e.samples {
			t.Errorf("%s: samples grew by %d, want %d", d.Stage, got, e.samples)
			continue
		}
		if e.samples == 0 {
			continue
		}
		prevAvg := 0.0
		if prev.AvgDays != nil {
			prevAvg = *prev.AvgDays
		}
		wantAvg := (prevAvg*float64(prev.Samples) + e.days) / float64(d.Samples)
		if d.AvgDays == nil || math.Abs(*d.AvgDays-wantAvg) > 0.1 {
			t.Errorf("%s: avg_days = %v, want about %.2f", d.Stage, d.AvgDays, wantAvg)
		}
	}
}

func TestDeletingALeadDeletesItsStageHistory(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	lead := createTestLead(t, st, uniqueInput(t, "history-delete"))
	if err := st.DeleteLead(ctx, lead.ID); err != nil {
		t.Fatalf("DeleteLead: %v", err)
	}
	var n int
	if err := st.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM lead_stage_history WHERE lead_id = $1`, lead.ID).Scan(&n); err != nil {
		t.Fatalf("count history: %v", err)
	}
	if n != 0 {
		t.Errorf("%d history rows left after deleting the lead, want 0", n)
	}
}

func TestListActivitiesForMissingLeadIsNotFound(t *testing.T) {
	st := newTestStore(t)

	if _, err := st.ListActivities(context.Background(), 1<<62); !errors.Is(err, ErrNotFound) {
		t.Errorf("ListActivities on a missing lead: got %v, want ErrNotFound", err)
	}
}
