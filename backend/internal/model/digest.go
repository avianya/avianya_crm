package model

import (
	"fmt"
	"strings"
	"time"
)

// This file is the daily summary email: each morning, every team member who
// wants one gets a short list of what needs them today -- built from the same
// FollowUpSummary as the Follow-up Center, filtered to them.

// DigestStatus is where a day's summary stands. Mirrors the
// daily_digests_status_valid CHECK.
type DigestStatus string

const (
	DigestSending DigestStatus = "sending"
	DigestSent    DigestStatus = "sent"
	DigestFailed  DigestStatus = "failed"  // retried up to MaxDigestAttempts
	DigestSkipped DigestStatus = "skipped" // nothing to report that day
)

// MaxDigestAttempts is how many times a day's summary is tried.
const MaxDigestAttempts = 3

// DigestRecord is one row of daily_digests, shown on the Team page.
type DigestRecord struct {
	Date      string       `json:"date"` // "2006-01-02"
	Status    DigestStatus `json:"status"`
	Attempts  int          `json:"attempts"`
	Detail    string       `json:"detail"`
	UpdatedAt time.Time    `json:"updated_at"`
}

// digestListCap is how many items each section of the email lists before
// "...and N more".
const digestListCap = 10

// DigestHasNews reports whether a summary is worth sending: something overdue,
// due today, stuck, going cold or without a next step. "This week" alone is not
// news; it is mentioned when there is other news.
func DigestHasNews(s FollowUpSummary) bool {
	return len(s.Overdue)+len(s.DueToday)+len(s.Stuck)+len(s.GoingCold)+len(s.NoNextStep) > 0
}

// DigestEmailData is everything a summary email says.
type DigestEmailData struct {
	RecipientName string
	Summary       FollowUpSummary
	// FollowUpsURL opens the Follow-up Center filtered to the recipient.
	FollowUpsURL string
	Location     *time.Location
}

// DigestEmail builds the subject and plain-text body of a daily summary.
func DigestEmail(d DigestEmailData) (subject, body string) {
	s := d.Summary
	loc := d.Location
	if loc == nil {
		loc = time.Local
	}

	dayText := s.Today
	if day, err := time.ParseInLocation(DateLayout, s.Today, loc); err == nil {
		dayText = day.Format("Mon 2 Jan")
	}

	// The subject carries the counts, so the inbox alone says how busy the day is.
	var counts []string
	if n := len(s.Overdue); n > 0 {
		counts = append(counts, fmt.Sprintf("%d overdue", n))
	}
	if n := len(s.DueToday); n > 0 {
		counts = append(counts, fmt.Sprintf("%d due today", n))
	}
	if n := len(s.Stuck); n > 0 {
		counts = append(counts, countNoun(n, "stuck deal", "stuck deals"))
	}
	if n := len(s.GoingCold); n > 0 {
		counts = append(counts, fmt.Sprintf("%d going cold", n))
	}
	subject = "Your day in Pipeline CRM, " + dayText
	if len(counts) > 0 {
		subject += ": " + strings.Join(counts, ", ")
	} else {
		subject += ": all clear"
	}

	var b strings.Builder
	greeting := "Good morning"
	if d.RecipientName != "" {
		greeting += " " + firstName(d.RecipientName)
	}
	fmt.Fprintf(&b, "%s,\n\nHere is what needs you today.\n", greeting)

	if !DigestHasNews(s) {
		b.WriteString("\nNothing is overdue or due today, no deal is stuck, and every open lead has a next step. Nice work.\n")
	}

	writeTasks(&b, "OVERDUE", s.Overdue, s.Today)
	writeTasks(&b, "DUE TODAY", s.DueToday, s.Today)

	writeLeads(&b, "STUCK DEALS", s.Stuck, func(l Lead) string {
		return fmt.Sprintf("%d days in %s (flagged after %d)", l.DaysInStage, l.Stage.Label(), l.StuckLimitDays)
	})
	writeLeads(&b, "GOING COLD", s.GoingCold, func(l Lead) string {
		return fmt.Sprintf("%s, %s", countNoun(l.DaysSinceTouch, "day since contact", "days since contact"), l.Stage.Label())
	})

	if n := len(s.NoNextStep); n > 0 {
		fmt.Fprintf(&b, "\nNO NEXT STEP\n  %s nothing planned.\n", countNoun(n, "open lead has", "open leads have"))
	}
	if n := len(s.ThisWeek); n > 0 && DigestHasNews(s) {
		fmt.Fprintf(&b, "\nCOMING UP\n  %s due in the next 7 days.\n", countNoun(n, "follow-up", "follow-ups"))
	}

	fmt.Fprintf(&b, "\nOpen your follow-ups: %s\n", d.FollowUpsURL)
	b.WriteString("\n-- \nSent by Pipeline CRM each morning. Turn it off on the Team page.\n")

	return subject, b.String()
}

func writeTasks(b *strings.Builder, title string, items []TaskWithLead, today string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "\n%s (%d)\n", title, len(items))
	for i, item := range items {
		if i == digestListCap {
			fmt.Fprintf(b, "  ...and %d more\n", len(items)-digestListCap)
			break
		}
		t, l := item.Task, item.Lead
		when := ""
		switch {
		case t.DueDate < today:
			when = "due " + t.DueDate
		case t.StartTime != nil:
			when = "at " + *t.StartTime
		}
		line := fmt.Sprintf("  - %s: %s, %s (%s)", t.Type.Label(), t.Title, l.Name, l.Company)
		if when != "" {
			line += ", " + when
		}
		b.WriteString(line + "\n")
	}
}

func writeLeads(b *strings.Builder, title string, leads []Lead, detail func(Lead) string) {
	if len(leads) == 0 {
		return
	}
	fmt.Fprintf(b, "\n%s (%d)\n", title, len(leads))
	for i, l := range leads {
		if i == digestListCap {
			fmt.Fprintf(b, "  ...and %d more\n", len(leads)-digestListCap)
			break
		}
		fmt.Fprintf(b, "  - %s (%s): %s\n", l.Name, l.Company, detail(l))
	}
}

// countNoun is "1 stuck deal" or "3 stuck deals".
func countNoun(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
