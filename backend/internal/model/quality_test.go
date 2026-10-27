package model

import (
	"strings"
	"testing"
)

func TestDefaultRulesAddUpAndValidate(t *testing.T) {
	rules := DefaultQualityRules()
	if errs := rules.Validate(); errs.Any() {
		t.Fatalf("default rules invalid: %v", errs)
	}
	total := 0
	for _, r := range rules.Rules {
		total += r.Points
	}
	if total != 100 {
		t.Errorf("default rules add up to %d, want 100", total)
	}
}

func TestMissingInformationIsNotPoor(t *testing.T) {
	rules := DefaultQualityRules()

	// Only an email: too little known to judge, so Not Evaluated -- not Poor.
	a := rules.Assess(QualityInput{Email: "asha@example.com"})
	if a.Quality != QualityNotEvaluated || a.Score != nil {
		t.Fatalf("email only = %s %v, want not evaluated", a.Quality, a.Score)
	}
	if !strings.Contains(a.Reason, "not known yet") {
		t.Errorf("reason does not say what is unknown: %q", a.Reason)
	}
	for _, h := range a.Hits {
		if h.RuleID != "valid_email" && h.Outcome != OutcomeUnknown {
			t.Errorf("rule %s with no data came out %s", h.RuleID, h.Outcome)
		}
	}

	// Nothing at all: still not Poor.
	if a := rules.Assess(QualityInput{}); a.Quality != QualityNotEvaluated {
		t.Errorf("empty lead = %s", a.Quality)
	}
}

func TestEvidenceDecidesTheBand(t *testing.T) {
	rules := DefaultQualityRules()

	// Valid email and phone: 40 of 40 decidable points -> Average.
	a := rules.Assess(QualityInput{Email: "asha@example.com", Phone: "+91 98765 43210"})
	if a.Quality != QualityAverage || *a.Score != 40 {
		t.Errorf("contact only = %s %v", a.Quality, a.Score)
	}

	// Invalid contact details are evidence: decided, and they score nothing.
	a = rules.Assess(QualityInput{Email: "not-an-email", Phone: "12"})
	if a.Quality != QualityPoor || *a.Score != 0 || !strings.Contains(a.Reason, "not valid email address") {
		t.Errorf("invalid contact = %s %v %q", a.Quality, a.Score, a.Reason)
	}

	// Everything there: Good.
	full := QualityInput{
		Email: "asha@acme.in", Phone: "+91 98765 43210",
		Answers:       []AdAnswer{{Key: "service", Value: "Website redesign"}, {Key: "budget", Value: "5-10 lakh"}},
		Conversations: 2, Meetings: 1,
	}
	a = rules.Assess(full)
	if a.Quality != QualityGood || *a.Score != 100 || !strings.HasPrefix(a.Reason, "Good (100/100)") {
		t.Errorf("complete lead = %s %v %q", a.Quality, a.Score, a.Reason)
	}
}

func TestGoodNeedsItsRequiredRules(t *testing.T) {
	rules := DefaultQualityRules() // valid_phone is required for Good
	in := QualityInput{
		Email:         "asha@acme.in",
		Answers:       []AdAnswer{{Key: "service", Value: "SEO"}, {Key: "budget", Value: "2 lakh"}},
		Conversations: 3, Meetings: 2,
	}
	a := rules.Assess(in)
	if *a.Score != 80 || a.Quality != QualityAverage || !strings.Contains(a.Reason, "Good once confirmed: valid phone number") {
		t.Errorf("no phone = %v %s %q", a.Score, a.Quality, a.Reason)
	}
}

func TestFitRulesPenaltiesAndClamp(t *testing.T) {
	rules := QualityRules{GoodFrom: 70, AverageFrom: 40, MinEvidence: 10, Rules: []QualityRule{
		{ID: "budget", Label: "Budget fits", Category: CategoryBudget, Field: FieldAnswer, AnswerKey: "What is your budget?", Op: OpOneOf, Value: "5-10 lakh, 10+ lakh", Points: 80},
		{ID: "student", Label: "Is a student", Category: CategoryService, Field: FieldAnswer, AnswerKey: "job_title", Op: OpContains, Value: "student", Points: -50},
		{ID: "city", Label: "In Pune", Category: CategoryOtherRule, Field: FieldAnswer, AnswerKey: "city", Op: OpEquals, Value: "pune", Points: 40},
	}}
	if errs := rules.Validate(); errs.Any() {
		t.Fatal(errs)
	}
	// Keys match ignoring case and punctuation; the sum is capped at 100.
	a := rules.Assess(QualityInput{Answers: []AdAnswer{
		{Key: "what_is_your_budget?", Value: "5-10 Lakh"}, {Key: "CITY", Value: "Pune"},
	}})
	if *a.Score != 100 {
		t.Errorf("score = %v, want capped 100", *a.Score)
	}
	// A budget that does not fit is evidence; a penalty takes points away; floor 0.
	a = rules.Assess(QualityInput{Answers: []AdAnswer{
		{Key: "what_is_your_budget?", Value: "Under 1 lakh"}, {Key: "job_title", Value: "Student"},
	}})
	if *a.Score != 0 || a.Quality != QualityPoor || a.Hits[0].Outcome != OutcomeFailed || a.Hits[1].Points != -50 {
		t.Errorf("poor fit = %v %s %+v", *a.Score, a.Quality, a.Hits)
	}
}

func TestFollowUpAndMeetingSignals(t *testing.T) {
	rules := DefaultQualityRules()
	base := QualityInput{Email: "a@acme.in", Phone: "+91 98765 43210"}
	for _, tc := range []struct {
		conv, meet int
		want       Outcome
	}{
		{0, 0, OutcomeUnknown}, // nothing has happened yet: proves nothing
		{1, 1, OutcomeMatched},
	} {
		in := base
		in.Conversations, in.Meetings = tc.conv, tc.meet
		for _, h := range rules.Assess(in).Hits {
			if (h.RuleID == "responded" || h.RuleID == "met") && h.Outcome != tc.want {
				t.Errorf("%d conversations, %d meetings: %s = %s", tc.conv, tc.meet, h.RuleID, h.Outcome)
			}
		}
	}
	at2 := QualityRule{ID: "x", Label: "x", Field: FieldConversations, Op: OpAtLeast, Value: "2", Points: 10}
	if got := at2.outcome(QualityInput{Conversations: 1}); got != OutcomeFailed {
		t.Errorf("1 of 2 conversations = %s", got)
	}
}

func TestSpamBeatsEverything(t *testing.T) {
	rules := DefaultQualityRules()
	in := QualityInput{Email: "a@acme.in", Phone: "+91 98765 43210", Conversations: 5, Meetings: 2, Spam: true}
	a := rules.Assess(in)
	if a.Quality != QualityPoor || *a.Score != 0 || !strings.Contains(a.Reason, "spam") {
		t.Errorf("spam = %s %v %q", a.Quality, a.Score, a.Reason)
	}
	override := 95
	if q, _ := rules.Final(a, &override, nil, true); q != QualityPoor {
		t.Errorf("an override lifted a spam lead to %s", q)
	}
}

func TestOverride(t *testing.T) {
	rules := DefaultQualityRules()
	a := rules.Assess(QualityInput{Email: "a@acme.in", Phone: "+91 98765 43210"})
	override, reason := 10, "Fake number"
	q, why := rules.Final(a, &override, &reason, false)
	if q != QualityPoor || why != "Set by hand to 10: Fake number." {
		t.Errorf("override = %s %q", q, why)
	}
	if q, _ := rules.Final(a, nil, nil, false); q != QualityAverage {
		t.Errorf("no override = %s", q)
	}
}

func TestQualificationStatusFollowsStage(t *testing.T) {
	for stage, want := range map[Stage]QualificationStatus{
		StageNew: QualificationPending, StageContacted: QualificationPending,
		StageQualified: QualificationQualified, StageNegotiation: QualificationQualified, StageWon: QualificationQualified,
		StageLost: QualificationNotQualified,
	} {
		if got := QualificationFor(stage); got != want {
			t.Errorf("%s -> %s, want %s", stage, got, want)
		}
	}
}

func TestQualityRulesValidation(t *testing.T) {
	bad := QualityRules{GoodFrom: 30, AverageFrom: 40, MinEvidence: 200, Rules: []QualityRule{
		{ID: "Bad Id", Label: "", Field: "zip", Op: "near", Points: 0},
		{ID: "a", Label: "A", Field: FieldAnswer, Op: OpEquals, Points: 5},
		{ID: "a", Label: "Dup", Field: FieldPhone, Op: OpBusinessEmail, Points: 500},
		{ID: "m", Label: "M", Field: FieldMeetings, Op: OpPresent, Points: -5, RequiredForGood: true},
		{ID: "n", Label: "N", Field: FieldConversations, Op: OpAtLeast, Value: "zero", Points: 5},
	}}
	bad.Normalize()
	errs := bad.Validate()
	for _, key := range []string{
		"good_from", "min_evidence", "rules[0].label", "rules[0].field", "rules[0].op", "rules[0].points",
		"rules[1].answer_key", "rules[1].value", "rules[2].id", "rules[2].op", "rules[2].points",
		"rules[3].op", "rules[3].required_for_good", "rules[4].value",
	} {
		if errs[key] == "" {
			t.Errorf("no error for %s (got %v)", key, errs)
		}
	}
}

func TestQualityOverrideInputValidation(t *testing.T) {
	score := 101
	in := QualityOverrideInput{Score: &score, Reason: "  "}
	in.Normalize()
	if errs := in.Validate(); errs["score"] == "" || errs["reason"] == "" {
		t.Errorf("errs = %v", errs)
	}
	score = 0
	in = QualityOverrideInput{Score: &score, Reason: "Spam submission"}
	if errs := in.Validate(); errs.Any() {
		t.Errorf("valid override refused: %v", errs)
	}
}
