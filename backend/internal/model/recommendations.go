package model

import (
	"fmt"
	"sort"
	"time"
)

// This file holds the "suggested next steps" shown on a lead. Like health and
// score, they are worked out on every read from what the CRM actually knows --
// the lead, its events and its timeline -- and never stored.
//
// The wording only states what the records show. The CRM cannot see a mailbox,
// so it never says a lead "has not replied"; it says no reply has been logged.

// NextStepKind identifies which rule produced a suggestion.
type NextStepKind string

const (
	StepOverdue      NextStepKind = "overdue"       // an event's time has passed
	StepPrepare      NextStepKind = "prepare"       // an event is coming up soon
	StepFirstContact NextStepKind = "first_contact" // nobody has contacted the lead yet
	StepNoReply      NextStepKind = "no_reply"      // an email, and nothing logged since
	StepFollowUp     NextStepKind = "follow_up"     // contact happened, nothing planned next
	StepPlan         NextStepKind = "plan"          // nothing planned at all
	StepReview       NextStepKind = "review"        // no contact for too long
)

// Priorities, highest first.
const (
	PriorityHigh   = "high"
	PriorityMedium = "medium"
	PriorityLow    = "low"
)

// TaskSuggestion pre-fills the new event form when a suggestion is acted on.
type TaskSuggestion struct {
	Type    TaskType `json:"type"`
	Title   string   `json:"title"`
	DueDate string   `json:"due_date"`
}

// NextStep is one suggestion. TaskID points at the event it is about, when
// there is one; Suggest is set when the right move is to schedule something.
type NextStep struct {
	Kind     NextStepKind    `json:"kind"`
	Priority string          `json:"priority"`
	Title    string          `json:"title"`
	Detail   string          `json:"detail"`
	TaskID   *int64          `json:"task_id,omitempty"`
	Suggest  *TaskSuggestion `json:"suggest,omitempty"`
}

// NextStepsInput is everything the rules look at. Now and Location are passed
// in, so tests can fix the clock and the time zone.
type NextStepsInput struct {
	Lead       Lead // with insights applied
	Tasks      []Task
	Activities []Activity
	Now        time.Time
	Location   *time.Location
}

// MaxNextSteps caps the list: a page of advice is advice nobody reads.
const MaxNextSteps = 4

// soonWindow is how far ahead "coming up" looks.
const soonWindow = 24 * time.Hour

// noReplyAfterDays is how long after an email, with nothing logged since, the
// lead is worth chasing.
const noReplyAfterDays = 3

// contactTypes are the timeline entries that are actual contact with the lead.
var contactTypes = map[ActivityType]bool{
	ActivityCall:    true,
	ActivityEmail:   true,
	ActivityMeeting: true,
}

// NextSteps returns the suggestions for a lead, most urgent first.
func NextSteps(in NextStepsInput) []NextStep {
	loc := in.Location
	if loc == nil {
		loc = time.Local
	}
	now := in.Now.In(loc)
	today := now.Format(DateLayout)
	lead := in.Lead

	var steps []NextStep
	hasUpcoming := false

	// --- events: overdue first, then the next one coming up ---------------
	var overdue []Task
	var soonest *Task
	var soonestAt time.Time
	for i := range in.Tasks {
		t := in.Tasks[i]
		switch t.Status {
		case TaskOverdue:
			overdue = append(overdue, t)
		case TaskUpcoming:
			hasUpcoming = true
			at, ok := eventStart(t, loc)
			if !ok || at.Sub(now) > soonWindow {
				continue
			}
			if soonest == nil || at.Before(soonestAt) {
				soonest, soonestAt = &in.Tasks[i], at
			}
		}
	}

	sort.Slice(overdue, func(i, j int) bool { return overdue[i].DueDate < overdue[j].DueDate })
	for i, t := range overdue {
		if i == 2 { // two is enough to make the point
			break
		}
		id := t.ID
		steps = append(steps, NextStep{
			Kind:     StepOverdue,
			Priority: PriorityHigh,
			Title:    fmt.Sprintf("Overdue: %s", t.Title),
			Detail: fmt.Sprintf("This %s was due %s. Mark it done if it happened, or reschedule it.",
				lowerLabel(t.Type), dueDescription(t.DueDate, now, loc)),
			TaskID: &id,
		})
	}

	if soonest != nil {
		id := soonest.ID
		priority := PriorityMedium
		if soonestAt.Sub(now) <= time.Hour {
			priority = PriorityHigh
		}
		steps = append(steps, NextStep{
			Kind:     StepPrepare,
			Priority: priority,
			Title:    fmt.Sprintf("Prepare for the %s %s", lowerLabel(soonest.Type), startsIn(*soonest, soonestAt, now)),
			Detail:   PrepAdvice(soonest.Type, lead.Name, lead.Company),
			TaskID:   &id,
		})
	}

	// Won and Lost leads need no chasing.
	if lead.Stage.IsClosed() {
		return finish(steps)
	}

	// --- what happened last, and is anything planned next? -----------------
	var last *Activity
	for i := range in.Activities {
		a := in.Activities[i]
		if !contactTypes[a.Type] {
			continue
		}
		if last == nil || a.CreatedAt.After(last.CreatedAt) {
			last = &in.Activities[i]
		}
	}

	if !hasUpcoming && len(overdue) == 0 {
		switch {
		case last == nil:
			steps = append(steps, NextStep{
				Kind:     StepFirstContact,
				Priority: PriorityHigh,
				Title:    "Make first contact",
				Detail: fmt.Sprintf("%s was added %s and no call, email or meeting has been logged yet.",
					lead.Name, ago(lead.CreatedAt, now)),
				Suggest: &TaskSuggestion{Type: TaskCall, Title: "Intro call with " + lead.Name, DueDate: today},
			})

		case last.Type == ActivityEmail && DaysBetween(last.CreatedAt, now) >= noReplyAfterDays:
			steps = append(steps, NextStep{
				Kind:     StepNoReply,
				Priority: PriorityMedium,
				Title:    "No reply logged since your last email",
				Detail: fmt.Sprintf("The last email was logged %s and no call or meeting has been logged since. Send a follow-up email or schedule a call.",
					ago(last.CreatedAt, now)),
				Suggest: &TaskSuggestion{Type: TaskCall, Title: "Follow-up call with " + lead.Name, DueDate: addDays(today, 1)},
			})

		case last.Type == ActivityCall || last.Type == ActivityMeeting:
			steps = append(steps, NextStep{
				Kind:     StepFollowUp,
				Priority: PriorityMedium,
				Title:    "Schedule the next step",
				Detail: fmt.Sprintf("The last %s was %s and nothing is planned next.",
					activityNoun(last.Type), ago(last.CreatedAt, now)),
				Suggest: &TaskSuggestion{Type: TaskFollowUp, Title: "Follow up with " + lead.Name, DueDate: addDays(today, 2)},
			})

		default:
			steps = append(steps, NextStep{
				Kind:     StepPlan,
				Priority: PriorityLow,
				Title:    "Plan the next step",
				Detail:   "Nothing is scheduled for this lead.",
				Suggest:  &TaskSuggestion{Type: TaskFollowUp, Title: "Follow up with " + lead.Name, DueDate: addDays(today, 2)},
			})
		}
	}

	// --- silence -------------------------------------------------------------
	if lead.Health == HealthCooling || lead.Health == HealthCold {
		priority := PriorityMedium
		if lead.Health == HealthCold {
			priority = PriorityHigh
		}
		limits, _ := lead.Stage.Thresholds()
		steps = append(steps, NextStep{
			Kind:     StepReview,
			Priority: priority,
			Title:    "Review this lead's status",
			Detail: fmt.Sprintf("No contact for %d days. Leads in %s start cooling after %d days - check it is still active, or move it to Lost.",
				lead.DaysSinceTouch, lead.Stage.Label(), limits.CoolingDays),
		})
	}

	return finish(steps)
}

// finish orders the suggestions by priority, keeping the rules' order within
// a priority, and caps the list.
func finish(steps []NextStep) []NextStep {
	rank := map[string]int{PriorityHigh: 0, PriorityMedium: 1, PriorityLow: 2}
	sort.SliceStable(steps, func(i, j int) bool { return rank[steps[i].Priority] < rank[steps[j].Priority] })
	if len(steps) > MaxNextSteps {
		steps = steps[:MaxNextSteps]
	}
	if steps == nil {
		steps = []NextStep{}
	}
	return steps
}

// eventStart is when an event begins: its start time, or 09:00 for an all-day
// event (the same base the reminder uses).
func eventStart(t Task, loc *time.Location) (time.Time, bool) {
	clock := AllDayReminderBase
	if t.StartTime != nil {
		clock = *t.StartTime
	}
	at, err := time.ParseInLocation(DateLayout+" "+TimeLayout, t.DueDate+" "+clock, loc)
	return at, err == nil
}

// startsIn phrases when an event starts: "in 25 minutes", "today at 3:00 PM",
// "tomorrow at 9:30 AM", "today" for an all-day event.
func startsIn(t Task, at, now time.Time) string {
	if t.StartTime == nil {
		if t.DueDate == now.Format(DateLayout) {
			return "today"
		}
		return "tomorrow"
	}
	until := at.Sub(now)
	switch {
	case until <= 0:
		return "now"
	case until < time.Hour:
		minutes := int(until.Minutes())
		if minutes < 1 {
			minutes = 1
		}
		return fmt.Sprintf("in %d minute%s", minutes, plural(minutes))
	case at.Format(DateLayout) == now.Format(DateLayout):
		return "today at " + at.Format("3:04 PM")
	default:
		return "tomorrow at " + at.Format("3:04 PM")
	}
}

// dueDescription phrases a past due date: "yesterday", "3 days ago".
func dueDescription(dueDate string, now time.Time, loc *time.Location) string {
	due, err := time.ParseInLocation(DateLayout, dueDate, loc)
	if err != nil {
		return "on " + dueDate
	}
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	days := int(todayStart.Sub(due).Hours() / 24)
	switch {
	case days <= 0:
		return "earlier today"
	case days == 1:
		return "yesterday"
	default:
		return fmt.Sprintf("%d days ago", days)
	}
}

// ago phrases how long ago something happened, in whole days.
func ago(then, now time.Time) string {
	switch days := DaysBetween(then, now); days {
	case 0:
		return "today"
	case 1:
		return "yesterday"
	default:
		return fmt.Sprintf("%d days ago", days)
	}
}

func activityNoun(t ActivityType) string {
	if t == ActivityMeeting {
		return "meeting"
	}
	return "call"
}

func lowerLabel(t TaskType) string {
	switch t {
	case TaskTodo:
		return "to-do"
	case TaskFollowUp:
		return "follow-up"
	default:
		return string(t)
	}
}

func addDays(isoDate string, days int) string {
	d, err := time.Parse(DateLayout, isoDate)
	if err != nil {
		return isoDate
	}
	return d.AddDate(0, 0, days).Format(DateLayout)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
