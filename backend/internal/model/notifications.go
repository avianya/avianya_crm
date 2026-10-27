package model

import (
	"fmt"
	"strings"
	"time"
)

// EmailStatus is where a reminder's email stands. Mirrors the
// notifications_email_status_valid CHECK.
type EmailStatus string

const (
	EmailPending EmailStatus = "pending" // waiting to be sent
	EmailSending EmailStatus = "sending" // claimed by the reminder job
	EmailSent    EmailStatus = "sent"
	EmailFailed  EmailStatus = "failed"  // will be retried until MaxEmailAttempts
	EmailSkipped EmailStatus = "skipped" // deliberately not sent; EmailDetail says why
)

// MaxEmailAttempts is how many times a reminder email is tried before it is
// left as failed for someone to retry by hand.
const MaxEmailAttempts = 5

// retryDelays is the wait after each failed attempt: quick at first for a blip,
// then backing off so a broken mail server is not hammered.
var retryDelays = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour}

// RetryDelaySeconds is retryDelays in whole seconds, for the database to pick
// from by attempt number.
func RetryDelaySeconds() []int {
	out := make([]int, len(retryDelays))
	for i, d := range retryDelays {
		out[i] = int(d.Seconds())
	}
	return out
}

// RetryDelay is how long to wait before the next try, after `attempts` tries
// have failed.
func RetryDelay(attempts int) time.Duration {
	if attempts < 1 {
		return retryDelays[0]
	}
	if attempts > len(retryDelays) {
		return retryDelays[len(retryDelays)-1]
	}
	return retryDelays[attempts-1]
}

// Notification is one reminder that has come due: what the bell lists, and the
// delivery record of its email.
type Notification struct {
	ID       int64     `json:"id"`
	RemindAt time.Time `json:"remind_at"`

	TaskID          int64      `json:"task_id"`
	TaskTitle       string     `json:"task_title"`
	TaskType        TaskType   `json:"task_type"`
	TaskStatus      TaskStatus `json:"task_status"`
	DueDate         string     `json:"due_date"`
	StartTime       *string    `json:"start_time"`
	DurationMinutes *int       `json:"duration_minutes"`

	LeadID      int64  `json:"lead_id"`
	LeadName    string `json:"lead_name"`
	LeadCompany string `json:"lead_company"`

	// RecipientName is nil when the event had nobody to send to, or the
	// member has since been removed.
	RecipientID    *int64  `json:"recipient_id"`
	RecipientName  *string `json:"recipient_name"`
	RecipientEmail *string `json:"recipient_email"`

	CreatedAt time.Time  `json:"created_at"`
	ReadAt    *time.Time `json:"read_at"`

	EmailStatus        EmailStatus `json:"email_status"`
	EmailAttempts      int         `json:"email_attempts"`
	EmailNextAttemptAt *time.Time  `json:"email_next_attempt_at"`
	EmailSentAt        *time.Time  `json:"email_sent_at"`
	EmailDetail        string      `json:"email_detail"`
}

// NotificationList is GET /api/notifications: the latest reminders plus how
// many are unread in total.
type NotificationList struct {
	Items  []Notification `json:"items"`
	Unread int            `json:"unread"`

	// DealAlerts are stuck-deal alerts, still-stuck ones first, then newest.
	// AlertsUnread counts unread alerts whose deal is still stuck.
	DealAlerts   []DealAlert `json:"deal_alerts"`
	AlertsUnread int         `json:"alerts_unread"`
}

// MaxNotificationsListed caps the bell's list.
const MaxNotificationsListed = 30

// ReminderSettings is GET /api/notifications/settings: enough for the UI to say
// whether reminder emails can actually go out. Never includes credentials.
type ReminderSettings struct {
	EmailConfigured bool   `json:"email_configured"`
	From            string `json:"from"`
	Timezone        string `json:"timezone"`

	// DigestHour is the local hour from which daily summaries are sent.
	DigestHour int `json:"digest_hour"`
}

// ---------------------------------------------------------------------------
// The reminder email
// ---------------------------------------------------------------------------

// ReminderEmailData is everything a reminder email says.
type ReminderEmailData struct {
	RecipientName string
	Task          Task
	Lead          Lead
	LeadURL       string
	Location      *time.Location
}

// PrepAdvice is the suggested next action before an event of type t, shared by
// the reminder email and the lead page's recommendations.
func PrepAdvice(t TaskType, leadName, company string) string {
	switch t {
	case TaskCall:
		return fmt.Sprintf("Before the call, re-read the latest entries on %s's timeline and decide the one thing you need from this call.", leadName)
	case TaskMeeting:
		return fmt.Sprintf("Confirm the agenda with %s and check where the deal stands in the pipeline.", leadName)
	case TaskDemo:
		return fmt.Sprintf("Tailor the demo to what %s has told you they need, and test your setup beforehand.", company)
	case TaskEmail:
		return "Draft the email now so it goes out on time."
	case TaskFollowUp:
		return fmt.Sprintf("Check the last contact on %s's timeline, then follow up on what was agreed.", leadName)
	default:
		return "Get it done, then tick it off so it is recorded on the timeline."
	}
}

// FormatEventTime describes when an event happens, e.g.
// "Friday 10 October 2026, 3:00 PM - 3:30 PM (IST)" or
// "Friday 10 October 2026 (all day)".
func FormatEventTime(task Task, loc *time.Location) string {
	if loc == nil {
		loc = time.Local
	}
	day, err := time.ParseInLocation(DateLayout, task.DueDate, loc)
	if err != nil {
		return task.DueDate
	}
	date := day.Format("Monday 2 January 2006")

	if task.StartTime == nil {
		return date + " (all day)"
	}
	start, err := time.ParseInLocation(DateLayout+" "+TimeLayout, task.DueDate+" "+*task.StartTime, loc)
	if err != nil {
		return date + ", " + *task.StartTime
	}
	text := date + ", " + start.Format("3:04 PM")
	if task.DurationMinutes != nil {
		end := start.Add(time.Duration(*task.DurationMinutes) * time.Minute)
		text += " - " + end.Format("3:04 PM")
	}
	return text + " (" + start.Format("MST") + ")"
}

// ReminderLeadTime phrases a reminder setting, e.g. "15 minutes" or "1 day".
func ReminderLeadTime(minutes int) string {
	switch {
	case minutes == 1440:
		return "1 day"
	case minutes%60 == 0:
		return fmt.Sprintf("%d hours", minutes/60)
	default:
		return fmt.Sprintf("%d minutes", minutes)
	}
}

// ReminderEmail builds the subject and plain-text body of a reminder.
//
// Plain text on purpose: it renders the same in every mail client, cannot
// carry tracking or remote content, and is what a reminder needs to be.
func ReminderEmail(d ReminderEmailData) (subject, body string) {
	t, lead := d.Task, d.Lead
	when := FormatEventTime(t, d.Location)

	subject = fmt.Sprintf("Reminder: %s with %s (%s) - %s", t.Type.Label(), lead.Name, lead.Company, when)

	var b strings.Builder
	greeting := "Hi"
	if d.RecipientName != "" {
		greeting = "Hi " + firstName(d.RecipientName)
	}
	fmt.Fprintf(&b, "%s,\n\n", greeting)
	if t.ReminderMinutes != nil {
		fmt.Fprintf(&b, "This is your %s reminder for:\n\n", ReminderLeadTime(*t.ReminderMinutes))
	} else {
		b.WriteString("This is your reminder for:\n\n")
	}

	fmt.Fprintf(&b, "  %s: %s\n", t.Type.Label(), t.Title)
	fmt.Fprintf(&b, "  When: %s\n", when)

	b.WriteString("\nLead\n")
	fmt.Fprintf(&b, "  %s, %s\n", lead.Name, lead.Company)
	fmt.Fprintf(&b, "  Email: %s\n", lead.Email)
	if lead.Phone != nil {
		fmt.Fprintf(&b, "  Phone: %s\n", *lead.Phone)
	}
	fmt.Fprintf(&b, "  Stage: %s\n", lead.Stage.Label())

	if t.Notes != "" {
		b.WriteString("\nNotes / agenda\n")
		for _, line := range strings.Split(t.Notes, "\n") {
			fmt.Fprintf(&b, "  %s\n", strings.TrimRight(line, "\r"))
		}
	}

	b.WriteString("\nSuggested next action\n")
	fmt.Fprintf(&b, "  %s\n", PrepAdvice(t.Type, lead.Name, lead.Company))

	fmt.Fprintf(&b, "\nOpen the lead: %s\n", d.LeadURL)
	b.WriteString("\n-- \nSent by Pipeline CRM. Change your reminder settings on the Team page.\n")

	return subject, b.String()
}

// firstName is the first word of a name, for a friendlier greeting.
func firstName(name string) string {
	if fields := strings.Fields(name); len(fields) > 0 {
		return fields[0]
	}
	return name
}

// ---------------------------------------------------------------------------
// Sending
// ---------------------------------------------------------------------------

// ReminderJob is one reminder email the reminder job has claimed, with the
// event and lead as they are now -- which may differ from when the reminder
// was raised.
type ReminderJob struct {
	NotificationID int64
	RemindAt       time.Time // the reminder this notification was raised for

	// The member the reminder was raised for. Name and Email are their current
	// details, nil if they have been removed since.
	RecipientID      *int64
	RecipientName    *string
	RecipientEmail   *string
	RecipientEmailOn bool

	// CurrentRecipientID is who the event is for now: its assignee, else the
	// lead's owner.
	CurrentRecipientID *int64

	Task Task
	Lead Lead
}

// SkipReason says why a claimed reminder email must not go out after all, or
// "" when it should. Everything is re-checked at send time, so an event that
// was completed, cancelled, moved or reassigned in the meantime -- or a member
// who turned email off -- never gets a stale or misdirected reminder.
func (j ReminderJob) SkipReason(now time.Time, loc *time.Location) string {
	if loc == nil {
		loc = time.Local
	}
	t := j.Task
	switch {
	case t.Status == TaskCompleted:
		return "The event was completed before the reminder went out."
	case t.Status == TaskCancelled:
		return "The event was cancelled."
	case t.RemindAt == nil:
		return "The reminder was turned off."
	case !t.RemindAt.Equal(j.RemindAt):
		return "The event was moved; its reminder will go out for the new time."
	case j.RecipientID == nil:
		return "Nobody is assigned to this event or owns the lead."
	case j.RecipientName == nil || j.RecipientEmail == nil:
		return "The team member was removed."
	case j.CurrentRecipientID == nil || *j.CurrentRecipientID != *j.RecipientID:
		return "The event was reassigned before the reminder went out."
	case !j.RecipientEmailOn:
		return fmt.Sprintf("Email reminders are turned off for %s.", *j.RecipientName)
	}

	if t.StartTime != nil {
		if start, ok := eventStart(t, loc); ok && !now.In(loc).Before(start) {
			return "The event had already started."
		}
	} else if t.DueDate < now.In(loc).Format(DateLayout) {
		return "The event's day has passed."
	}
	return ""
}
