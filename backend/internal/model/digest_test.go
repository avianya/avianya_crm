package model

import (
	"fmt"
	"strings"
	"testing"
)

func TestDigestEmail(t *testing.T) {
	start := "15:00"
	s := FollowUpSummary{
		Today: "2026-10-10",
		Overdue: []TaskWithLead{{
			Task: Task{Title: "Send revised quote", Type: TaskEmail, DueDate: "2026-10-08"},
			Lead: Lead{Name: "Ravi Kumar", Company: "Acme"},
		}},
		DueToday: []TaskWithLead{{
			Task: Task{Title: "Pricing call", Type: TaskCall, DueDate: "2026-10-10", StartTime: &start},
			Lead: Lead{Name: "Meera Shah", Company: "Globex"},
		}},
		Stuck:    []Lead{{Name: "Ravi Kumar", Company: "Acme", Stage: StageProposalSent, DaysInStage: 12, StuckLimitDays: 10}},
		ThisWeek: []TaskWithLead{{Task: Task{Title: "Demo"}}},
	}

	subject, body := DigestEmail(DigestEmailData{
		RecipientName: "Asha Verma",
		Summary:       s,
		FollowUpsURL:  "http://localhost:5173/follow-ups?owner=3",
	})

	if want := "Your day in Pipeline CRM, Sat 10 Oct: 1 overdue, 1 due today, 1 stuck deal"; subject != want {
		t.Errorf("subject = %q, want %q", subject, want)
	}
	for _, want := range []string{
		"Good morning Asha,",
		"OVERDUE (1)\n  - Email: Send revised quote, Ravi Kumar (Acme), due 2026-10-08",
		"DUE TODAY (1)\n  - Call: Pricing call, Meera Shah (Globex), at 15:00",
		"STUCK DEALS (1)\n  - Ravi Kumar (Acme): 12 days in Proposal Sent (flagged after 10)",
		"COMING UP\n  1 follow-up due in the next 7 days.",
		"Open your follow-ups: http://localhost:5173/follow-ups?owner=3",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body is missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "GOING COLD") || strings.Contains(body, "NO NEXT STEP") {
		t.Errorf("empty sections were included:\n%s", body)
	}
}

func TestDigestEmailCapsLongLists(t *testing.T) {
	var cold []Lead
	for i := range 13 {
		cold = append(cold, Lead{Name: fmt.Sprintf("Lead %d", i), Company: "Co", Stage: StageQualified, DaysSinceTouch: 9})
	}
	_, body := DigestEmail(DigestEmailData{Summary: FollowUpSummary{Today: "2026-10-10", GoingCold: cold}})

	if !strings.Contains(body, "GOING COLD (13)") || !strings.Contains(body, "...and 3 more") {
		t.Errorf("long list not capped:\n%s", body)
	}
	if strings.Contains(body, "Lead 10 ") || strings.Contains(body, "Lead 12") {
		t.Errorf("listed past the cap:\n%s", body)
	}
	if !strings.Contains(body, "Good morning,") {
		t.Errorf("no-name greeting wrong:\n%s", body)
	}
}

func TestDigestHasNews(t *testing.T) {
	if DigestHasNews(FollowUpSummary{}) {
		t.Error("an empty summary has news")
	}
	// Something due later in the week alone is not worth an email.
	if DigestHasNews(FollowUpSummary{ThisWeek: []TaskWithLead{{}}}) {
		t.Error("this week alone counts as news")
	}
	if !DigestHasNews(FollowUpSummary{NoNextStep: []Lead{{}}}) {
		t.Error("a lead without a next step is not news")
	}

	subject, body := DigestEmail(DigestEmailData{Summary: FollowUpSummary{Today: "2026-10-10"}})
	if !strings.HasSuffix(subject, ": all clear") || !strings.Contains(body, "Nice work.") {
		t.Errorf("all-clear email wrong: %q\n%s", subject, body)
	}
}
