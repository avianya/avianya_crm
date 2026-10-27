package model

import (
	"strings"
	"testing"
)

func TestAdSubmissionLeadInput(t *testing.T) {
	sub := AdSubmission{
		Provider: ProviderMeta,
		FormID:   "123",
		Campaign: "Diwali offer",
		Answers: []AdAnswer{
			{Key: "first_name", Label: "First name", Value: "Asha"},
			{Key: "last_name", Label: "Last name", Value: "Rao"},
			{Key: "EMAIL", Label: "Email", Value: " Asha@Kestrel.in "},
			{Key: "phone_number", Label: "Phone number", Value: "+91 98765 43210"},
			{Key: "job_title", Label: "Job title", Value: "CTO"},
			{Key: "budget?", Label: "Budget?", Value: ""}, // unanswered: left out
		},
	}
	in, problem := sub.LeadInput()
	if problem != "" {
		t.Fatalf("problem: %s", problem)
	}
	if in.Name != "Asha Rao" || in.Email != "asha@kestrel.in" || in.Phone != "+91 98765 43210" ||
		in.Company != NotProvided || in.Source != SourceMetaAds || in.Stage != StageNew {
		t.Errorf("lead = %+v", in)
	}
	want := "Came in from a Meta lead form (form 123, campaign Diwali offer).\n\nOther answers:\nJob title: CTO"
	if in.Notes != want {
		t.Errorf("notes =\n%q\nwant\n%q", in.Notes, want)
	}

	// Google's column ids, a company, and a phone the CRM would refuse.
	sub = AdSubmission{
		Provider: ProviderGoogle,
		Answers: []AdAnswer{
			{Key: "FULL_NAME", Label: "Full Name", Value: "Vik Iyer"},
			{Key: "EMAIL", Label: "User Email", Value: "vik@iyer.test"},
			{Key: "COMPANY_NAME", Label: "Company Name", Value: "Iyer & Sons"},
			{Key: "PHONE_NUMBER", Label: "User Phone", Value: "call me"},
		},
	}
	in, problem = sub.LeadInput()
	if problem != "" || in.Name != "Vik Iyer" || in.Company != "Iyer & Sons" || in.Source != SourceGoogleAds ||
		in.Phone != "" || !strings.HasSuffix(in.Notes, "Phone as given: call me") {
		t.Errorf("google lead = %+v, problem %q", in, problem)
	}

	// No email: refused, saying why. No name: the email stands in.
	if _, problem := (AdSubmission{Answers: []AdAnswer{{Key: "full_name", Value: "Pia"}}}).LeadInput(); !strings.Contains(problem, "no email") {
		t.Errorf("no email: problem = %q", problem)
	}
	in, problem = AdSubmission{Answers: []AdAnswer{{Key: "email", Value: "pia@x.test"}}}.LeadInput()
	if problem != "" || in.Name != "pia@x.test" {
		t.Errorf("no name: %+v, %q", in, problem)
	}
}

func TestLabelFromKey(t *testing.T) {
	for key, want := range map[string]string{"job_title": "Job title", "FULL_NAME": "Full name", "city": "City", "": ""} {
		if got := LabelFromKey(key); got != want {
			t.Errorf("LabelFromKey(%q) = %q, want %q", key, got, want)
		}
	}
}
