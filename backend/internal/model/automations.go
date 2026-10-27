package model

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// This file is automation rules: "when a new lead comes in, plan this task for
// it". The rules are matched and their task titles filled in here; the store
// runs them inside the transaction that creates the lead.

// AutomationTrigger is what makes a rule run. Mirrors the
// automation_rules_trigger_valid CHECK.
type AutomationTrigger string

const TriggerLeadCreated AutomationTrigger = "lead_created"

// AutomationTriggers lists every valid trigger.
var AutomationTriggers = []AutomationTrigger{TriggerLeadCreated}

func (t AutomationTrigger) Valid() bool {
	for _, allowed := range AutomationTriggers {
		if t == allowed {
			return true
		}
	}
	return false
}

// AutomationRule is one row of automation_rules, with its run history summed up.
type AutomationRule struct {
	ID      int64             `json:"id"`
	Name    string            `json:"name"`
	Enabled bool              `json:"enabled"`
	Trigger AutomationTrigger `json:"trigger"`

	// Conditions. Empty Sources means any source; nil MinDealValue means any
	// deal value, including none.
	Sources      []Source `json:"sources"`
	MinDealValue *int64   `json:"min_deal_value"`

	// The task to plan.
	TaskTitle  string   `json:"task_title"`
	TaskType   TaskType `json:"task_type"`
	DueInDays  int      `json:"due_in_days"`
	AssigneeID *int64   `json:"assignee_id"` // nil: the lead's owner

	RunCount  int        `json:"run_count"`
	LastRunAt *time.Time `json:"last_run_at"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// Limits, mirrored by the automation_rules CHECK constraints.
const (
	maxRuleNameLen = 120
	MaxDueInDays   = 365
)

// Matches reports whether the rule's conditions hold for the lead. Whether the
// rule is enabled, and its trigger, are the caller's business.
func (r AutomationRule) Matches(lead Lead) bool {
	if len(r.Sources) > 0 && !containsSource(r.Sources, lead.Source) {
		return false
	}
	if r.MinDealValue != nil && (lead.DealValue == nil || *lead.DealValue < *r.MinDealValue) {
		return false
	}
	return true
}

func containsSource(list []Source, s Source) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

// TaskTitleFor fills in the rule's task title for a lead. A field with nothing
// to fill in is left blank; should that leave no title at all, the rule's name
// stands in, so the task can always be saved.
func (r AutomationRule) TaskTitleFor(lead Lead, ownerName string) string {
	title, _ := Render(r.TaskTitle, ValuesFor(lead, ownerName))
	title = strings.Join(strings.Fields(title), " ")
	if title == "" {
		title = r.Name
	}
	if utf8.RuneCountInString(title) > maxTaskTitleLen {
		title = string([]rune(title)[:maxTaskTitleLen])
	}
	return title
}

// DueLabel says when a task due in days days is due, for the timeline.
func DueLabel(days int) string {
	switch days {
	case 0:
		return "due today"
	case 1:
		return "due tomorrow"
	default:
		return fmt.Sprintf("due in %d days", days)
	}
}

// AutomationNote is the timeline entry for a rule that planned a task.
func AutomationNote(ruleName string, taskType TaskType, title string, dueInDays int) string {
	return fmt.Sprintf("Automation %q planned a %s: %q, %s.",
		ruleName, strings.ToLower(taskType.Label()), title, DueLabel(dueInDays))
}

// ---------------------------------------------------------------------------
// Input
// ---------------------------------------------------------------------------

// AutomationRuleInput is the body of POST /api/automations and PUT
// /api/automations/:id.
type AutomationRuleInput struct {
	Name         string            `json:"name"`
	Enabled      *bool             `json:"enabled"` // optional; defaults to true
	Trigger      AutomationTrigger `json:"trigger"` // optional; defaults to "lead_created"
	Sources      []Source          `json:"sources"`
	MinDealValue *int64            `json:"min_deal_value"`
	TaskTitle    string            `json:"task_title"`
	TaskType     TaskType          `json:"task_type"` // optional; defaults to "todo"
	DueInDays    int               `json:"due_in_days"`
	AssigneeID   *int64            `json:"assignee_id"`
}

// Normalize trims the text, fills in the defaults and drops repeated sources.
func (in *AutomationRuleInput) Normalize() {
	in.Name = strings.TrimSpace(in.Name)
	if in.Enabled == nil {
		enabled := true
		in.Enabled = &enabled
	}
	in.Trigger = AutomationTrigger(strings.ToLower(strings.TrimSpace(string(in.Trigger))))
	if in.Trigger == "" {
		in.Trigger = TriggerLeadCreated
	}

	sources := make([]Source, 0, len(in.Sources))
	for _, s := range in.Sources {
		s = Source(strings.ToLower(strings.TrimSpace(string(s))))
		if !containsSource(sources, s) {
			sources = append(sources, s)
		}
	}
	in.Sources = sources

	in.TaskTitle = strings.TrimSpace(in.TaskTitle)
	in.TaskType = TaskType(strings.ToLower(strings.TrimSpace(string(in.TaskType))))
	if in.TaskType == "" {
		in.TaskType = TaskTodo
	}
}

// Validate mirrors the automation_rules CHECK constraints, and rejects unknown
// {{fields}} in the task title as the email templates do.
func (in AutomationRuleInput) Validate() ValidationErrors {
	errs := ValidationErrors{}

	switch {
	case in.Name == "":
		errs["name"] = "is required"
	case utf8.RuneCountInString(in.Name) > maxRuleNameLen:
		errs["name"] = fmt.Sprintf("must be %d characters or fewer", maxRuleNameLen)
	}

	if !in.Trigger.Valid() {
		errs["trigger"] = fmt.Sprintf("%q is not a trigger; the only one is %s", in.Trigger, TriggerLeadCreated)
	}

	for _, s := range in.Sources {
		if !s.Valid() {
			errs["sources"] = fmt.Sprintf("%q is not a lead source", s)
			break
		}
	}

	if in.MinDealValue != nil && *in.MinDealValue < 0 {
		errs["min_deal_value"] = "must be zero or more"
	}

	switch {
	case in.TaskTitle == "":
		errs["task_title"] = "is required"
	case utf8.RuneCountInString(in.TaskTitle) > maxTaskTitleLen:
		errs["task_title"] = fmt.Sprintf("must be %d characters or fewer", maxTaskTitleLen)
	default:
		if unknown := UnknownFields(in.TaskTitle); len(unknown) > 0 {
			errs["task_title"] = fmt.Sprintf("{{%s}} is not a field", unknown[0])
		}
	}

	if !in.TaskType.Valid() {
		errs["task_type"] = fmt.Sprintf("%q is not one of %s", in.TaskType, joinTaskTypes(TaskTypes))
	}

	if in.DueInDays < 0 || in.DueInDays > MaxDueInDays {
		errs["due_in_days"] = fmt.Sprintf("must be between 0 and %d", MaxDueInDays)
	}

	if in.AssigneeID != nil && *in.AssigneeID <= 0 {
		errs["assignee_id"] = "must be a team member's id"
	}
	return errs
}
