package model

import (
	"testing"
	"time"
)

func TestApplyStageAge(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	daysAgo := func(d float64) time.Time { return now.Add(-time.Duration(d * float64(24*time.Hour))) }

	cases := []struct {
		name      string
		stage     Stage
		entered   time.Time
		wantDays  int
		wantLimit int
		wantStuck bool
	}{
		{"fresh proposal", StageProposalSent, daysAgo(2), 2, 10, false},
		{"proposal one hour short", StageProposalSent, daysAgo(9.96), 9, 10, false},
		{"proposal at the limit", StageProposalSent, daysAgo(10), 10, 10, true},
		{"long negotiation", StageNegotiation, daysAgo(20), 20, 14, true},
		{"new lead a week old", StageNew, daysAgo(7), 7, 7, true},
		{"won is never stuck", StageWon, daysAgo(400), 400, 0, false},
		{"lost is never stuck", StageLost, daysAgo(400), 400, 0, false},
		{"clock skew", StageQualified, now.Add(time.Hour), 0, 14, false},
	}
	for _, tc := range cases {
		lead := Lead{Stage: tc.stage}
		lead.ApplyStageAge(tc.entered, now)
		if lead.DaysInStage != tc.wantDays || lead.StuckLimitDays != tc.wantLimit || lead.Stuck != tc.wantStuck {
			t.Errorf("%s: days=%d limit=%d stuck=%v, want %d %d %v", tc.name,
				lead.DaysInStage, lead.StuckLimitDays, lead.Stuck, tc.wantDays, tc.wantLimit, tc.wantStuck)
		}
		if !lead.StageEnteredAt.Equal(tc.entered) {
			t.Errorf("%s: entered = %v", tc.name, lead.StageEnteredAt)
		}
	}
}

func TestStuckThresholdsCoverEveryOpenStage(t *testing.T) {
	stages, days := StuckThresholds()
	if len(stages) != len(days) {
		t.Fatalf("%d stages but %d limits", len(stages), len(days))
	}
	got := map[string]bool{}
	for i, s := range stages {
		got[s] = true
		if days[i] <= 0 {
			t.Errorf("%s has limit %d", s, days[i])
		}
	}
	for _, s := range Stages {
		if got[string(s)] == s.IsClosed() {
			t.Errorf("stage %s: has limit %v, closed %v", s, got[string(s)], s.IsClosed())
		}
	}
}
