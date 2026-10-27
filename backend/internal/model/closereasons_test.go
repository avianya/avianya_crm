package model

import (
	"strings"
	"testing"
)

func reason(r string) *CloseReason {
	cr := CloseReason(r)
	return &cr
}

func TestCloseInputValidate(t *testing.T) {
	cases := []struct {
		name     string
		in       CloseInput
		stage    Stage
		entering bool
		field    string // "" means valid
	}{
		{"losing needs a reason", CloseInput{}, StageLost, true, "close_reason"},
		{"already lost needs none", CloseInput{}, StageLost, false, ""},
		{"winning needs none", CloseInput{}, StageWon, true, ""},
		{"a lost reason", CloseInput{Reason: reason("competitor")}, StageLost, true, ""},
		{"a won reason", CloseInput{Reason: reason("relationship")}, StageWon, true, ""},
		{"a won reason on a loss", CloseInput{Reason: reason("relationship")}, StageLost, true, "close_reason"},
		{"a lost reason on a win", CloseInput{Reason: reason("no_budget")}, StageWon, true, "close_reason"},
		{"made up", CloseInput{Reason: reason("aliens")}, StageLost, true, "close_reason"},
		{"other needs a note", CloseInput{Reason: reason("other")}, StageLost, true, "close_note"},
		{"other with a note", CloseInput{Reason: reason("other"), Note: "Company shut down"}, StageLost, true, ""},
		{"a note needs a reason", CloseInput{Note: "why"}, StageWon, true, "close_reason"},
		{"long note", CloseInput{Reason: reason("price"), Note: strings.Repeat("n", 501)}, StageLost, true, "close_note"},
		{"open stage takes none", CloseInput{Reason: reason("price")}, StageQualified, true, "close_reason"},
		{"open stage, none given", CloseInput{}, StageNegotiation, true, ""},
	}
	for _, tc := range cases {
		errs := tc.in.Validate(tc.stage, tc.entering)
		if tc.field == "" {
			if errs.Any() {
				t.Errorf("%s: want valid, got %v", tc.name, errs)
			}
			continue
		}
		if errs[tc.field] == "" {
			t.Errorf("%s: want an error on %s, got %v", tc.name, tc.field, errs)
		}
	}
}

func TestCloseInputNormalize(t *testing.T) {
	in := CloseInput{Reason: reason("  "), Note: "  spaced  "}
	in.Normalize()
	if in.Reason != nil || in.Note != "spaced" {
		t.Errorf("Normalize = %+v, want no reason and a trimmed note", in)
	}
	if (CloseInput{}).NotePtr() != nil {
		t.Error("an empty note should be stored as NULL")
	}
}

func TestReasonListsAreComplete(t *testing.T) {
	for _, list := range [][]ReasonOption{LostReasons, WonReasons} {
		seen := map[CloseReason]bool{}
		for _, r := range list {
			if r.Label == "" || seen[r.Value] {
				t.Errorf("reason %q is unlabelled or listed twice", r.Value)
			}
			seen[r.Value] = true
		}
		if !seen[ReasonOther] {
			t.Error(`every list needs "other" for what the list does not cover`)
		}
	}
	if ReasonsFor(StageQualified) != nil {
		t.Error("open stages have no reasons")
	}
}

func TestReasonText(t *testing.T) {
	note := "Competitor offered 20% off"
	cases := []struct {
		stage  Stage
		reason *CloseReason
		note   *string
		want   string
	}{
		{StageLost, reason("price"), nil, "Price too high"},
		{StageLost, reason("price"), &note, "Price too high (Competitor offered 20% off)"},
		{StageWon, reason("price"), nil, "Price / value"}, // the same code reads per stage
		{StageWon, nil, nil, ""},
	}
	for _, tc := range cases {
		if got := ReasonText(tc.stage, tc.reason, tc.note); got != tc.want {
			t.Errorf("ReasonText = %q, want %q", got, tc.want)
		}
	}
	if got := ReasonCountLabel(StageLost, nil); got != "No reason recorded" {
		t.Errorf("unrecorded label = %q", got)
	}
}

func TestStageInputValidatesTheReason(t *testing.T) {
	if errs := (StageInput{Stage: StageLost, CloseInput: CloseInput{Reason: reason("aliens")}}).Validate(); errs["close_reason"] == "" {
		t.Errorf("an unknown reason should be rejected, got %v", errs)
	}
	// Whether one is required depends on the lead's current stage: the store's job.
	if errs := (StageInput{Stage: StageLost}).Validate(); errs.Any() {
		t.Errorf("StageInput alone cannot know the lead is entering Lost, got %v", errs)
	}
}
