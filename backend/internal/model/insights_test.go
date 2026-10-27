package model

import (
	"slices"
	"testing"
	"time"
)

func TestHealthForUsesEachStagesOwnClock(t *testing.T) {
	cases := []struct {
		stage Stage
		days  int
		want  Health
	}{
		// Negotiation: cooling at 3, cold at 7.
		{StageNegotiation, 0, HealthActive},
		{StageNegotiation, 2, HealthActive},
		{StageNegotiation, 3, HealthCooling},
		{StageNegotiation, 6, HealthCooling},
		{StageNegotiation, 7, HealthCold},

		// Qualified is more patient: cooling at 7, cold at 14. The same 7
		// silent days that make a Negotiation lead cold are only cooling here.
		{StageQualified, 6, HealthActive},
		{StageQualified, 7, HealthCooling},
		{StageQualified, 14, HealthCold},

		// Closed leads cannot go cold, however long it has been.
		{StageWon, 365, HealthClosed},
		{StageLost, 0, HealthClosed},
	}

	for _, tc := range cases {
		if got := HealthFor(tc.stage, tc.days); got != tc.want {
			t.Errorf("HealthFor(%s, %d) = %q, want %q", tc.stage, tc.days, got, tc.want)
		}
	}
}

func TestEveryOpenStageHasThresholds(t *testing.T) {
	for _, stage := range Stages {
		th, ok := stage.Thresholds()
		if stage.IsClosed() {
			if ok {
				t.Errorf("%s is closed but has health thresholds", stage)
			}
			continue
		}
		if !ok {
			t.Errorf("open stage %s has no health thresholds", stage)
			continue
		}
		if th.CoolingDays <= 0 || th.ColdDays <= th.CoolingDays {
			t.Errorf("%s thresholds %+v: want 0 < cooling < cold", stage, th)
		}
	}
}

func TestDaysBetween(t *testing.T) {
	base := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		now  time.Time
		want int
	}{
		{base, 0},
		{base.Add(23 * time.Hour), 0}, // not a whole day yet
		{base.Add(24 * time.Hour), 1},
		{base.Add(-time.Hour), 0}, // clock skew never goes negative
	}
	for _, tc := range cases {
		if got := DaysBetween(base, tc.now); got != tc.want {
			t.Errorf("DaysBetween(base, %v) = %d, want %d", tc.now, got, tc.want)
		}
	}
}

// factorPoints returns the points one factor earned, failing if it is missing.
func factorPoints(t *testing.T, score *LeadScore, key string) int {
	t.Helper()
	for _, f := range score.Factors {
		if f.Key == key {
			return f.Points
		}
	}
	t.Fatalf("score has no %q factor: %+v", key, score.Factors)
	return 0
}

func TestScoreForAddsUpItsFactors(t *testing.T) {
	phone := "+91 90000 00000"
	value := int64(100_000)

	perfect := Lead{Stage: StageNegotiation, Source: SourceReferral, Phone: &phone, Notes: "context",
		DealValue: &value, Health: HealthActive}
	score := ScoreFor(perfect, LeadSignals{HasOpenTask: true, RecentConversations: 4})
	if score == nil || score.Total != 100 {
		t.Fatalf("a lead that ticks every box should score 100, got %+v", score)
	}
	if len(score.Penalties) != 0 {
		t.Errorf("a perfect lead has penalties %+v", score.Penalties)
	}

	// The maxima must add up to 100 too, or the scale means nothing.
	maxSum := 0
	for _, f := range score.Factors {
		maxSum += f.Max
		if f.Points != f.Max {
			t.Errorf("factor %q earned %d of %d on a perfect lead", f.Key, f.Points, f.Max)
		}
	}
	if maxSum != 100 {
		t.Errorf("factor maxima sum to %d, want 100", maxSum)
	}

	// Cold call is the weakest source, so a bare New lead keeps only its 2.
	bare := Lead{Stage: StageNew, Source: SourceColdCall, Health: HealthCold}
	if got := ScoreFor(bare, LeadSignals{}); got == nil || got.Total != 2 {
		t.Errorf("a bare, cold, New cold-call lead should score 2, got %+v", got)
	}

	// Cooling earns partial credit for recent contact.
	bare.Health = HealthCooling
	cooling := ScoreFor(bare, LeadSignals{})
	if want := 2 + scoreRecentContactCooling; cooling.Total != want {
		t.Errorf("cooling New lead scored %d, want %d", cooling.Total, want)
	}
}

func TestScoreForEngagementTiers(t *testing.T) {
	cases := []struct{ conversations, want int }{
		{0, 0}, {1, 5}, {2, 10}, {3, 10}, {4, 15}, {12, 15},
	}
	for _, tc := range cases {
		score := ScoreFor(Lead{Stage: StageQualified}, LeadSignals{RecentConversations: tc.conversations})
		if got := factorPoints(t, score, "engagement"); got != tc.want {
			t.Errorf("%d conversations: engagement = %d, want %d", tc.conversations, got, tc.want)
		}
	}
}

func TestScoreForEverySourceIsScored(t *testing.T) {
	for _, source := range Sources {
		points, ok := sourceScorePoints[source]
		if !ok {
			t.Errorf("source %s has no score points", source)
			continue
		}
		if points <= 0 || points > scoreSourceMax {
			t.Errorf("source %s scores %d, want 1..%d", source, points, scoreSourceMax)
		}
	}
	if sourceScorePoints[SourceReferral] != scoreSourceMax {
		t.Error("a referral should earn the full source points")
	}
}

func TestScoreForPenalties(t *testing.T) {
	value := int64(50_000)
	// 23 (Proposal) + 20 (active) + 10 (2 conversations) + 7 (website) + 10
	// (next step) + 10 (deal value) = 80 before penalties.
	lead := Lead{Stage: StageProposalSent, Source: SourceWebsite, DealValue: &value, Health: HealthActive}
	sig := LeadSignals{HasOpenTask: true, RecentConversations: 2}

	if got := ScoreFor(lead, sig).Total; got != 80 {
		t.Fatalf("baseline score = %d, want 80", got)
	}

	cases := []struct {
		name    string
		overdue int
		stuck   bool
		want    int
		keys    []string
	}{
		{"one overdue task", 1, false, 75, []string{"overdue_tasks"}},
		{"two overdue tasks", 2, false, 70, []string{"overdue_tasks"}},
		{"overdue penalty is capped", 5, false, 70, []string{"overdue_tasks"}},
		{"stuck", 0, true, 70, []string{"stuck"}},
		{"both", 3, true, 60, []string{"overdue_tasks", "stuck"}},
	}
	for _, tc := range cases {
		l := lead
		l.Stuck, l.DaysInStage = tc.stuck, 12
		s := sig
		s.OverdueTasks = tc.overdue

		score := ScoreFor(l, s)
		if score.Total != tc.want {
			t.Errorf("%s: total = %d, want %d", tc.name, score.Total, tc.want)
		}
		if len(score.Penalties) != len(tc.keys) {
			t.Errorf("%s: penalties = %+v, want keys %v", tc.name, score.Penalties, tc.keys)
			continue
		}
		for i, p := range score.Penalties {
			if p.Key != tc.keys[i] || p.Points >= 0 || p.Label == "" {
				t.Errorf("%s: penalty %d = %+v, want key %q with negative points and a label",
					tc.name, i, p, tc.keys[i])
			}
		}
	}
}

func TestScoreForNeverGoesBelowZero(t *testing.T) {
	lead := Lead{Stage: StageNew, Source: SourceColdCall, Health: HealthCold, Stuck: true, DaysInStage: 30}
	score := ScoreFor(lead, LeadSignals{OverdueTasks: 4})
	if score.Total != 0 {
		t.Errorf("total = %d, want 0 (2 points less 20 of penalties, floored)", score.Total)
	}
}

func TestBandFor(t *testing.T) {
	cases := []struct {
		total int
		want  ScoreBand
	}{
		{100, BandHot}, {70, BandHot},
		{69, BandWarm}, {40, BandWarm},
		{39, BandCold}, {0, BandCold},
	}
	for _, tc := range cases {
		if got := BandFor(tc.total); got != tc.want {
			t.Errorf("BandFor(%d) = %q, want %q", tc.total, got, tc.want)
		}
	}

	// ScoreFor fills the band in from the final, penalised total.
	lead := Lead{Stage: StageNew, Source: SourceColdCall, Health: HealthCold}
	if got := ScoreFor(lead, LeadSignals{}).Band; got != BandCold {
		t.Errorf("a 2-point lead is %q, want cold", got)
	}
}

func TestScoreBandIsValid(t *testing.T) {
	for _, band := range ScoreBands {
		if !band.IsValid() {
			t.Errorf("%q should be valid", band)
		}
	}
	for _, bad := range []ScoreBand{"", "HOT", "lukewarm"} {
		if bad.IsValid() {
			t.Errorf("%q should not be valid", bad)
		}
	}
}

// scored builds a lead with just an id and, unless total is negative, a score.
func scored(id int64, total int) Lead {
	lead := Lead{ID: id, Stage: StageQualified}
	if total >= 0 {
		lead.Score = &LeadScore{Total: total, Band: BandFor(total)}
	} else {
		lead.Stage = StageWon
	}
	return lead
}

func ids(leads []Lead) []int64 {
	out := make([]int64, len(leads))
	for i, l := range leads {
		out[i] = l.ID
	}
	return out
}

func TestSortLeadsByScore(t *testing.T) {
	// 4 and 5 are closed (no score); 2 and 3 tie on 50.
	build := func() []Lead {
		return []Lead{scored(1, 20), scored(4, -1), scored(2, 50), scored(5, -1), scored(3, 50), scored(6, 90)}
	}

	desc := build()
	SortLeadsByScore(desc, "desc")
	if got, want := ids(desc), []int64{6, 3, 2, 1, 5, 4}; !slices.Equal(got, want) {
		t.Errorf("desc order = %v, want %v", got, want)
	}

	asc := build()
	SortLeadsByScore(asc, "asc")
	if got, want := ids(asc), []int64{1, 2, 3, 6, 4, 5}; !slices.Equal(got, want) {
		t.Errorf("asc order = %v, want %v (closed leads still last)", got, want)
	}
}

func TestFilterByBand(t *testing.T) {
	leads := []Lead{scored(1, 85), scored(2, 55), scored(3, -1), scored(4, 72), scored(5, 10)}

	cases := map[ScoreBand][]int64{
		BandHot:  {1, 4},
		BandWarm: {2},
		BandCold: {5}, // closed lead 3 is in no band
	}
	for band, want := range cases {
		if got := ids(FilterByBand(leads, band)); !slices.Equal(got, want) {
			t.Errorf("FilterByBand(%s) = %v, want %v", band, got, want)
		}
	}
}

func TestLeadQueryNeedsScores(t *testing.T) {
	cases := []struct {
		q    LeadQuery
		want bool
	}{
		{LeadQuery{SortBy: "created_at"}, false},
		{LeadQuery{SortBy: SortByScore}, true},
		{LeadQuery{SortBy: "name", Band: BandWarm}, true},
	}
	for _, tc := range cases {
		if got := tc.q.NeedsScores(); got != tc.want {
			t.Errorf("%+v NeedsScores = %v, want %v", tc.q, got, tc.want)
		}
	}
}

func TestScoreForClosedLeadsIsNil(t *testing.T) {
	for _, stage := range []Stage{StageWon, StageLost} {
		if got := ScoreFor(Lead{Stage: stage, Health: HealthClosed}, LeadSignals{}); got != nil {
			t.Errorf("%s lead got a score %+v, want nil", stage, got)
		}
	}
}

func TestApplyInsights(t *testing.T) {
	now := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)
	lead := Lead{Stage: StageProposalSent, Source: SourceLinkedIn} // cooling at 4, cold at 10; stuck at 10

	lead.ApplyInsights(LeadSignals{
		LastTouch:           now.Add(-5 * 24 * time.Hour),
		StageEnteredAt:      now.Add(-11 * 24 * time.Hour),
		HasOpenTask:         true,
		RecentConversations: 1,
		OverdueTasks:        1,
	}, now)

	if lead.DaysSinceTouch != 5 {
		t.Errorf("DaysSinceTouch = %d, want 5", lead.DaysSinceTouch)
	}
	if lead.Health != HealthCooling {
		t.Errorf("Health = %q, want cooling", lead.Health)
	}
	if !lead.HasOpenTask {
		t.Error("HasOpenTask = false, want true")
	}
	if !lead.Stuck || lead.DaysInStage != 11 {
		t.Errorf("Stuck = %v after %d days, want stuck after 11", lead.Stuck, lead.DaysInStage)
	}
	// 23 (Proposal Sent) + 8 (cooling) + 5 (one conversation) + 5 (LinkedIn)
	// + 10 (next step) = 51, less 5 (overdue) and 10 (stuck) = 36. The stuck
	// penalty proves stage age is worked out before the score.
	if lead.Score == nil || lead.Score.Total != 36 {
		t.Errorf("Score = %+v, want total 36", lead.Score)
	}
}
