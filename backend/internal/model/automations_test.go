package model

import (
	"strings"
	"testing"
)

func TestAutomationRuleMatches(t *testing.T) {
	big, small := int64(500_000), int64(10_000)
	min := int64(100_000)

	cases := []struct {
		name string
		rule AutomationRule
		lead Lead
		want bool
	}{
		{"no conditions", AutomationRule{}, Lead{Source: SourceOther}, true},
		{"listed source", AutomationRule{Sources: []Source{SourceWebsite, SourceReferral}}, Lead{Source: SourceReferral}, true},
		{"unlisted source", AutomationRule{Sources: []Source{SourceWebsite}}, Lead{Source: SourceLinkedIn}, false},
		{"deal big enough", AutomationRule{MinDealValue: &min}, Lead{DealValue: &big}, true},
		{"deal too small", AutomationRule{MinDealValue: &min}, Lead{DealValue: &small}, false},
		{"no deal value", AutomationRule{MinDealValue: &min}, Lead{}, false},
	}
	for _, tc := range cases {
		if got := tc.rule.Matches(tc.lead); got != tc.want {
			t.Errorf("%s: Matches = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestAutomationTaskTitleFor(t *testing.T) {
	lead := Lead{Name: "Asha Rao", Company: "Kestrel"}

	rule := AutomationRule{Name: "Intro", TaskTitle: "Call {{first_name}} at {{company}} for {{owner_name}}"}
	if got, want := rule.TaskTitleFor(lead, ""), "Call Asha at Kestrel for"; got != want {
		t.Errorf("title = %q, want %q", got, want)
	}

	// Nothing to fill in falls back to the rule's name.
	rule = AutomationRule{Name: "Intro", TaskTitle: "{{owner_name}}"}
	if got := rule.TaskTitleFor(lead, ""); got != "Intro" {
		t.Errorf("blank title = %q, want the rule name", got)
	}

	// A long company name cannot push the title past the task limit.
	lead.Company = strings.Repeat("x", 300)
	rule = AutomationRule{Name: "Intro", TaskTitle: "Call {{company}}"}
	if got := rule.TaskTitleFor(lead, ""); len([]rune(got)) != maxTaskTitleLen {
		t.Errorf("long title has %d characters, want %d", len([]rune(got)), maxTaskTitleLen)
	}
}

func TestAutomationRuleInputValidate(t *testing.T) {
	in := AutomationRuleInput{Name: "  Intro  ", TaskTitle: " Call {{first_name}} ", Sources: []Source{"Website", "website"}}
	in.Normalize()
	if errs := in.Validate(); errs.Any() {
		t.Fatalf("valid input rejected: %v", errs)
	}
	if in.Name != "Intro" || in.TaskType != TaskTodo || in.Trigger != TriggerLeadCreated || in.Enabled == nil || !*in.Enabled {
		t.Errorf("defaults not filled in: %+v", in)
	}
	if len(in.Sources) != 1 || in.Sources[0] != SourceWebsite {
		t.Errorf("sources = %v, want [website]", in.Sources)
	}

	negative := int64(-1)
	bad := AutomationRuleInput{
		Trigger:      "stage_changed",
		Sources:      []Source{"fax"},
		MinDealValue: &negative,
		TaskTitle:    "Call {{frist_name}}",
		TaskType:     "lunch",
		DueInDays:    400,
	}
	bad.Normalize()
	errs := bad.Validate()
	for _, field := range []string{"name", "trigger", "sources", "min_deal_value", "task_title", "task_type", "due_in_days"} {
		if _, ok := errs[field]; !ok {
			t.Errorf("expected an error on %s, got %v", field, errs)
		}
	}
}

func TestAutomationNote(t *testing.T) {
	got := AutomationNote("Intro call", TaskCall, "Call Asha", 1)
	if want := `Automation "Intro call" planned a call: "Call Asha", due tomorrow.`; got != want {
		t.Errorf("note = %q, want %q", got, want)
	}
}
