package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"crm-lead-management/backend/internal/model"
)

func closeReason(r string) *model.CloseReason {
	cr := model.CloseReason(r)
	return &cr
}

// latestNote is the newest timeline entry's text.
func latestNote(t *testing.T, st *Store, leadID int64) string {
	t.Helper()
	activities, err := st.ListActivities(context.Background(), leadID)
	if err != nil {
		t.Fatalf("ListActivities: %v", err)
	}
	if len(activities) == 0 {
		t.Fatal("no timeline entries")
	}
	return activities[0].Note
}

func wantFieldError(t *testing.T, err error, field string) {
	t.Helper()
	var fieldErr *FieldError
	if !errors.As(err, &fieldErr) || fieldErr.Field != field {
		t.Fatalf("err = %v, want a FieldError on %s", err, field)
	}
}

func TestLosingADealNeedsAReason(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	lead := createTestLead(t, st, uniqueInput(t, "close-stage"))

	// No reason: refused, and nothing changes.
	_, err := st.UpdateLeadStage(ctx, lead.ID, model.StageInput{Stage: model.StageLost})
	wantFieldError(t, err, "close_reason")
	got, _ := st.GetLead(ctx, lead.ID)
	if got.Stage != model.StageNew {
		t.Fatalf("a refused loss moved the lead to %s", got.Stage)
	}

	// With one: saved on the lead and written on the timeline.
	lost, err := st.UpdateLeadStage(ctx, lead.ID, model.StageInput{Stage: model.StageLost,
		CloseInput: model.CloseInput{Reason: closeReason("competitor"), Note: "Went with Acme"}})
	if err != nil {
		t.Fatalf("lose with a reason: %v", err)
	}
	if lost.CloseReason == nil || *lost.CloseReason != "competitor" || lost.CloseNote == nil || *lost.CloseNote != "Went with Acme" {
		t.Errorf("reason on lead = %v %v", lost.CloseReason, lost.CloseNote)
	}
	if note := latestNote(t, st, lead.ID); note != "Stage changed from New to Lost. Reason: Went with a competitor (Went with Acme)." {
		t.Errorf("timeline = %q", note)
	}

	// The same stage with a new reason changes just the reason, on the record.
	changed, err := st.UpdateLeadStage(ctx, lead.ID, model.StageInput{Stage: model.StageLost,
		CloseInput: model.CloseInput{Reason: closeReason("price")}})
	if err != nil {
		t.Fatalf("change reason: %v", err)
	}
	if *changed.CloseReason != "price" || changed.CloseNote != nil {
		t.Errorf("changed reason = %v %v, want price with no note", changed.CloseReason, changed.CloseNote)
	}
	if note := latestNote(t, st, lead.ID); note != "Lost reason changed to: Price too high." {
		t.Errorf("timeline = %q", note)
	}
	history, _ := st.ListStageHistory(ctx, lead.ID)
	if len(history) != 2 {
		t.Errorf("a reason change is not a stage move, but history has %d rows", len(history))
	}

	// Reopening clears it.
	reopened, err := st.UpdateLeadStage(ctx, lead.ID, model.StageInput{Stage: model.StageNegotiation})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if reopened.CloseReason != nil || reopened.CloseNote != nil {
		t.Errorf("reopened lead still has reason %v %v", reopened.CloseReason, reopened.CloseNote)
	}

	// Winning is one click: no reason needed.
	won, err := st.UpdateLeadStage(ctx, lead.ID, model.StageInput{Stage: model.StageWon})
	if err != nil || won.CloseReason != nil {
		t.Fatalf("win without a reason = %v, %v", won.CloseReason, err)
	}
	if note := latestNote(t, st, lead.ID); note != "Stage changed from Negotiation to Won." {
		t.Errorf("timeline = %q", note)
	}
}

func TestEditAndCreateFollowTheSameRule(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	in := uniqueInput(t, "close-edit")
	lead := createTestLead(t, st, in)

	// An edit into Lost with no reason is refused, and rolls the whole edit back.
	edit := in
	edit.Stage, edit.Name = model.StageLost, "Renamed In A Refused Edit"
	_, err := st.UpdateLead(ctx, lead.ID, edit)
	wantFieldError(t, err, "close_reason")
	if got, _ := st.GetLead(ctx, lead.ID); got.Name != lead.Name {
		t.Errorf("a refused edit renamed the lead to %q", got.Name)
	}

	edit.CloseInput = model.CloseInput{Reason: closeReason("no_budget")}
	if _, err := st.UpdateLead(ctx, lead.ID, edit); err != nil {
		t.Fatalf("edit into Lost with a reason: %v", err)
	}

	// Editing something else on a lost lead keeps its reason.
	edit.CloseInput = model.CloseInput{}
	edit.Phone = "+91 91111 11111"
	kept, err := st.UpdateLead(ctx, lead.ID, edit)
	if err != nil {
		t.Fatalf("edit a lost lead: %v", err)
	}
	if kept.CloseReason == nil || *kept.CloseReason != "no_budget" {
		t.Errorf("reason after an unrelated edit = %v, want no_budget kept", kept.CloseReason)
	}

	// Creating a lead straight into Lost needs a reason too.
	fresh := uniqueInput(t, "close-create")
	fresh.Stage = model.StageLost
	fresh.Normalize()
	_, err = st.CreateLead(ctx, fresh)
	wantFieldError(t, err, "close_reason")

	fresh.CloseInput = model.CloseInput{Reason: closeReason("timing"), Note: "Next financial year"}
	created := createTestLead(t, st, fresh)
	if created.CloseReason == nil || *created.CloseReason != "timing" {
		t.Errorf("created lost lead reason = %v", created.CloseReason)
	}
	if note := latestNote(t, st, created.ID); !strings.HasSuffix(note, "Reason: Bad timing (Next financial year).") {
		t.Errorf("timeline = %q", note)
	}
}

func TestReportsBreakDownCloseReasons(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	count := func(reasons []model.ReasonCount, code string) (int, int64) {
		for _, r := range reasons {
			if (r.Reason == nil && code == "") || (r.Reason != nil && string(*r.Reason) == code) {
				return r.Count, r.Value
			}
		}
		return 0, 0
	}

	before, err := st.Reports(ctx)
	if err != nil {
		t.Fatalf("Reports: %v", err)
	}
	priceBefore, priceValueBefore := count(before.LossReasons, "price")
	wonNoneBefore, _ := count(before.WinReasons, "")

	value := int64(120_000)
	for i, suffix := range []string{"report-price-a", "report-price-b"} {
		in := uniqueInput(t, suffix)
		in.Stage, in.CloseInput = model.StageLost, model.CloseInput{Reason: closeReason("price")}
		if i == 0 {
			in.DealValue = &value // the other has no value: counts as 0
		}
		createTestLead(t, st, in)
	}
	won := uniqueInput(t, "report-won-none")
	won.Stage = model.StageWon
	createTestLead(t, st, won)

	after, err := st.Reports(ctx)
	if err != nil {
		t.Fatalf("Reports: %v", err)
	}
	priceAfter, priceValueAfter := count(after.LossReasons, "price")
	if priceAfter-priceBefore != 2 || priceValueAfter-priceValueBefore != value {
		t.Errorf("price losses went %d -> %d (value %d -> %d), want +2 and +%d",
			priceBefore, priceAfter, priceValueBefore, priceValueAfter, value)
	}
	if wonNone, _ := count(after.WinReasons, ""); wonNone-wonNoneBefore != 1 {
		t.Errorf("wins with no reason went %d -> %d, want +1", wonNoneBefore, wonNone)
	}
	for _, r := range after.LossReasons {
		if r.Label == "" {
			t.Errorf("reason %v has no label", r.Reason)
		}
	}
}
