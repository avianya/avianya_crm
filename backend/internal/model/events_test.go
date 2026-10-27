package model

import (
	"strings"
	"testing"
	"time"
)

// A fixed clock in a fixed zone: Saturday 10 October 2026, 10:00 in India.
var (
	ist     = time.FixedZone("IST", 5*3600+1800)
	testNow = time.Date(2026, 10, 10, 10, 0, 0, 0, ist)
)

func ptr[T any](v T) *T { return &v }

func openLead() Lead {
	lead := Lead{ID: 7, Name: "Asha Rao", Company: "Kestrel", Email: "asha@kestrel.in", Stage: StageQualified,
		CreatedAt: testNow.AddDate(0, 0, -10)}
	lead.ApplyInsights(LeadSignals{LastTouch: testNow.AddDate(0, 0, -1), StageEnteredAt: lead.CreatedAt}, testNow)
	return lead
}

func kinds(steps []NextStep) []NextStepKind {
	out := make([]NextStepKind, len(steps))
	for i, s := range steps {
		out[i] = s.Kind
	}
	return out
}

func hasKind(steps []NextStep, kind NextStepKind) bool {
	for _, s := range steps {
		if s.Kind == kind {
			return true
		}
	}
	return false
}

func TestNextStepsFirstContact(t *testing.T) {
	steps := NextSteps(NextStepsInput{Lead: openLead(), Now: testNow, Location: ist,
		Activities: []Activity{{Type: ActivityCreated, CreatedAt: testNow.AddDate(0, 0, -10)}}})
	if !hasKind(steps, StepFirstContact) {
		t.Fatalf("want first_contact, got %v", kinds(steps))
	}
	if steps[0].Suggest == nil || steps[0].Suggest.Type != TaskCall || steps[0].Suggest.DueDate != "2026-10-10" {
		t.Fatalf("want a call suggested today, got %+v", steps[0].Suggest)
	}
}

func TestNextStepsPrepareForSoonEvent(t *testing.T) {
	call := Task{ID: 3, Type: TaskCall, Title: "Pricing call", DueDate: "2026-10-10",
		StartTime: ptr("10:30"), DurationMinutes: ptr(30), Status: TaskUpcoming}
	steps := NextSteps(NextStepsInput{Lead: openLead(), Tasks: []Task{call}, Now: testNow, Location: ist})

	if len(steps) == 0 || steps[0].Kind != StepPrepare {
		t.Fatalf("want prepare first, got %v", kinds(steps))
	}
	if steps[0].Priority != PriorityHigh {
		t.Errorf("a call in 30 minutes should be high priority, got %s", steps[0].Priority)
	}
	if !strings.Contains(steps[0].Title, "in 30 minutes") {
		t.Errorf("title = %q", steps[0].Title)
	}
	if steps[0].TaskID == nil || *steps[0].TaskID != 3 {
		t.Errorf("want task 3 linked")
	}
	// Something is planned, so no "schedule" suggestion.
	if hasKind(steps, StepFollowUp) || hasKind(steps, StepPlan) || hasKind(steps, StepFirstContact) {
		t.Errorf("nothing to schedule when an event is planned, got %v", kinds(steps))
	}
}

func TestNextStepsEventFarAwayIsNotPrep(t *testing.T) {
	meeting := Task{ID: 4, Type: TaskMeeting, Title: "Review", DueDate: "2026-10-14",
		StartTime: ptr("11:00"), DurationMinutes: ptr(60), Status: TaskUpcoming}
	steps := NextSteps(NextStepsInput{Lead: openLead(), Tasks: []Task{meeting}, Now: testNow, Location: ist})
	if hasKind(steps, StepPrepare) {
		t.Fatalf("a meeting in 4 days needs no prep yet, got %v", kinds(steps))
	}
}

func TestNextStepsOverdue(t *testing.T) {
	late := Task{ID: 5, Type: TaskEmail, Title: "Send quote", DueDate: "2026-10-08", Status: TaskOverdue}
	steps := NextSteps(NextStepsInput{Lead: openLead(), Tasks: []Task{late}, Now: testNow, Location: ist})
	if len(steps) == 0 || steps[0].Kind != StepOverdue {
		t.Fatalf("want overdue first, got %v", kinds(steps))
	}
	if !strings.Contains(steps[0].Detail, "2 days ago") {
		t.Errorf("detail = %q", steps[0].Detail)
	}
}

func TestNextStepsFollowUpAfterMeeting(t *testing.T) {
	steps := NextSteps(NextStepsInput{Lead: openLead(), Now: testNow, Location: ist,
		Activities: []Activity{{Type: ActivityMeeting, CreatedAt: testNow.AddDate(0, 0, -1)}},
		Tasks:      []Task{{ID: 1, Type: TaskMeeting, Status: TaskCompleted, DueDate: "2026-10-09"}}})
	if !hasKind(steps, StepFollowUp) {
		t.Fatalf("want follow_up, got %v", kinds(steps))
	}
}

func TestNextStepsNoReplyAfterEmail(t *testing.T) {
	steps := NextSteps(NextStepsInput{Lead: openLead(), Now: testNow, Location: ist,
		Activities: []Activity{
			{Type: ActivityCall, CreatedAt: testNow.AddDate(0, 0, -9)},
			{Type: ActivityEmail, CreatedAt: testNow.AddDate(0, 0, -4)},
		}})
	if !hasKind(steps, StepNoReply) {
		t.Fatalf("want no_reply, got %v", kinds(steps))
	}
	for _, s := range steps {
		// The CRM cannot see a mailbox, so it must never claim the lead did
		// not reply -- only that no reply has been logged.
		if strings.Contains(s.Title+s.Detail, "has not replied") || strings.Contains(s.Title+s.Detail, "didn't reply") {
			t.Errorf("overclaims: %q / %q", s.Title, s.Detail)
		}
	}
}

func TestNextStepsRecentEmailIsNotNoReply(t *testing.T) {
	steps := NextSteps(NextStepsInput{Lead: openLead(), Now: testNow, Location: ist,
		Activities: []Activity{{Type: ActivityEmail, CreatedAt: testNow.AddDate(0, 0, -1)}}})
	if hasKind(steps, StepNoReply) {
		t.Fatalf("an email yesterday is too soon to chase, got %v", kinds(steps))
	}
}

func TestNextStepsReviewWhenCold(t *testing.T) {
	lead := openLead()
	lead.ApplyInsights(LeadSignals{LastTouch: testNow.AddDate(0, 0, -20), StageEnteredAt: lead.CreatedAt}, testNow) // Qualified goes cold at 14
	steps := NextSteps(NextStepsInput{Lead: lead, Now: testNow, Location: ist,
		Activities: []Activity{{Type: ActivityCall, CreatedAt: testNow.AddDate(0, 0, -20)}}})
	var review *NextStep
	for i := range steps {
		if steps[i].Kind == StepReview {
			review = &steps[i]
		}
	}
	if review == nil || review.Priority != PriorityHigh {
		t.Fatalf("want a high-priority review, got %+v", steps)
	}
}

func TestNextStepsClosedLeadOnlyEvents(t *testing.T) {
	lead := openLead()
	lead.Stage = StageWon
	lead.ApplyInsights(LeadSignals{LastTouch: testNow.AddDate(0, 0, -30), StageEnteredAt: lead.CreatedAt}, testNow)
	steps := NextSteps(NextStepsInput{Lead: lead, Now: testNow, Location: ist})
	if len(steps) != 0 {
		t.Fatalf("a won lead with nothing scheduled needs no steps, got %v", kinds(steps))
	}
}

// ---------------------------------------------------------------------------

func readyJob() ReminderJob {
	remind := time.Date(2026, 10, 10, 10, 15, 0, 0, ist)
	return ReminderJob{
		NotificationID:     1,
		RemindAt:           remind,
		RecipientID:        ptr(int64(2)),
		RecipientName:      ptr("Ravi"),
		RecipientEmail:     ptr("ravi@example.com"),
		RecipientEmailOn:   true,
		CurrentRecipientID: ptr(int64(2)),
		Task: Task{ID: 9, Type: TaskCall, Title: "Call", DueDate: "2026-10-10", StartTime: ptr("10:30"),
			DurationMinutes: ptr(30), ReminderMinutes: ptr(15), RemindAt: ptr(remind), Status: TaskUpcoming},
	}
}

func TestSkipReason(t *testing.T) {
	now := time.Date(2026, 10, 10, 10, 16, 0, 0, ist)
	if reason := readyJob().SkipReason(now, ist); reason != "" {
		t.Fatalf("ready job skipped: %s", reason)
	}

	cases := map[string]func(j *ReminderJob){
		"completed":     func(j *ReminderJob) { j.Task.Status = TaskCompleted },
		"cancelled":     func(j *ReminderJob) { j.Task.Status = TaskCancelled },
		"turned off":    func(j *ReminderJob) { j.Task.RemindAt = nil },
		"moved":         func(j *ReminderJob) { j.Task.RemindAt = ptr(j.RemindAt.Add(time.Hour)) },
		"no recipient":  func(j *ReminderJob) { j.RecipientID = nil },
		"removed":       func(j *ReminderJob) { j.RecipientName, j.RecipientEmail = nil, nil },
		"reassigned":    func(j *ReminderJob) { j.CurrentRecipientID = ptr(int64(3)) },
		"unassigned":    func(j *ReminderJob) { j.CurrentRecipientID = nil },
		"email off":     func(j *ReminderJob) { j.RecipientEmailOn = false },
		"already began": func(j *ReminderJob) { j.Task.StartTime = ptr("10:00") },
	}
	for name, change := range cases {
		job := readyJob()
		change(&job)
		if job.SkipReason(now, ist) == "" {
			t.Errorf("%s: should be skipped", name)
		}
	}
}

func TestReminderEmailContent(t *testing.T) {
	job := readyJob()
	job.Task.Notes = "Agenda: pricing\nNext: contract"
	lead := Lead{ID: 7, Name: "Asha Rao", Company: "Kestrel", Email: "asha@kestrel.in",
		Phone: ptr("+91 98765 43210"), Stage: StageNegotiation}

	subject, body := ReminderEmail(ReminderEmailData{
		RecipientName: "Ravi Kumar", Task: job.Task, Lead: lead,
		LeadURL: "http://localhost:5173/leads/7", Location: ist,
	})

	if !strings.Contains(subject, "Call with Asha Rao (Kestrel)") {
		t.Errorf("subject = %q", subject)
	}
	for _, want := range []string{
		"Hi Ravi,", "15 minutes reminder", "Call: Call",
		"Saturday 10 October 2026, 10:30 AM - 11:00 AM (IST)",
		"asha@kestrel.in", "+91 98765 43210", "Negotiation",
		"  Agenda: pricing\n  Next: contract", "Suggested next action",
		"http://localhost:5173/leads/7",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body is missing %q:\n%s", want, body)
		}
	}
}

func TestRetryDelayBacksOff(t *testing.T) {
	want := []time.Duration{time.Minute, time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour, time.Hour}
	for attempts, d := range want {
		if got := RetryDelay(attempts); got != d {
			t.Errorf("RetryDelay(%d) = %s, want %s", attempts, got, d)
		}
	}
	if got := RetryDelaySeconds(); len(got) != 4 || got[0] != 60 || got[3] != 3600 {
		t.Errorf("RetryDelaySeconds = %v", got)
	}
}

func TestTeamMemberInput(t *testing.T) {
	in := TeamMemberInput{Name: "  Ravi  ", Email: " Ravi@Example.com "}
	in.Normalize()
	if errs := in.Validate(); errs.Any() {
		t.Fatalf("valid member rejected: %v", errs)
	}
	if in.Email != "ravi@example.com" || in.EmailReminders == nil || !*in.EmailReminders {
		t.Errorf("normalize: %+v", in)
	}

	bad := TeamMemberInput{Name: "", Email: "nope", DefaultReminderMinutes: ptr(10)}
	bad.Normalize()
	errs := bad.Validate()
	for _, field := range []string{"name", "email", "default_reminder_minutes"} {
		if errs[field] == "" {
			t.Errorf("want an error on %s", field)
		}
	}
}

func TestTaskInputEventFields(t *testing.T) {
	in := TaskInput{Title: "Demo", Type: "DEMO", DueDate: "2026-10-12", ReminderMinutes: ptr(30),
		Notes: "  agenda  ", AssigneeID: ptr(int64(1))}
	in.Normalize()
	if errs := in.Validate(); errs.Any() {
		t.Fatalf("valid event rejected: %v", errs)
	}
	if in.Type != TaskDemo || in.Notes != "agenda" {
		t.Errorf("normalize: %+v", in)
	}

	bad := TaskInput{Title: "x", DueDate: "2026-10-12", ReminderMinutes: ptr(10),
		AssigneeID: ptr(int64(0)), Notes: strings.Repeat("é", 2001)}
	bad.Normalize()
	errs := bad.Validate()
	for _, field := range []string{"reminder_minutes", "assignee_id", "notes"} {
		if errs[field] == "" {
			t.Errorf("want an error on %s", field)
		}
	}
	if TaskDemo.CompletionActivity() != ActivityMeeting || TaskFollowUp.CompletionActivity() != ActivityTaskCompleted {
		t.Errorf("demo should log as a meeting, follow-up as task_completed")
	}
}

func TestTaskStatusInput(t *testing.T) {
	if !(TaskStatusInput{}).Validate().Any() {
		t.Error("empty body should be rejected")
	}
	if !(TaskStatusInput{Completed: ptr(true), Cancelled: ptr(true)}).Validate().Any() {
		t.Error("both flags should be rejected")
	}
	if (TaskStatusInput{Cancelled: ptr(true)}).Validate().Any() {
		t.Error("cancelled alone is valid")
	}
}
