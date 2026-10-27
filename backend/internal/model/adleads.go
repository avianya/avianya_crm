package model

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// This file is leads from ad lead forms: Meta Lead Ads (Facebook and
// Instagram) and Google Ads lead forms. Each platform sends a webhook when
// someone fills in a form; the api package reads the platform's format into an
// AdSubmission, and LeadInput here turns that into a CRM lead.

// AdProvider is the platform a form lives on. Mirrors ad_leads_provider_valid.
type AdProvider string

const (
	ProviderMeta   AdProvider = "meta"
	ProviderGoogle AdProvider = "google"
)

// Valid reports whether p is a known provider.
func (p AdProvider) Valid() bool { return p == ProviderMeta || p == ProviderGoogle }

// Label is the platform's name, e.g. "Meta".
func (p AdProvider) Label() string {
	if p == ProviderGoogle {
		return "Google"
	}
	return "Meta"
}

// Source is the lead source a provider's leads get.
func (p AdProvider) Source() Source {
	if p == ProviderGoogle {
		return SourceGoogleAds
	}
	return SourceMetaAds
}

// AdAnswer is one answer on a form. Key is the platform's id for the question
// (Meta's field name, Google's column id); Label is what to show for it.
type AdAnswer struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Value string `json:"value"`
}

// AdSubmission is one filled-in form, whichever platform it came from.
type AdSubmission struct {
	Provider   AdProvider
	ExternalID string // Meta's leadgen_id, Google's lead_id
	FormID     string
	Campaign   string // name where the platform gives one, else its id
	Ad         string
	IsTest     bool // the platform's own test submission
	Answers    []AdAnswer
	// Attribution is where a Meta lead came from, for meta_lead_attribution;
	// nil for Google and for test submissions.
	Attribution *MetaAttribution
}

// The questions every form builder offers, by csvKey of the platform's key.
// Anything else goes into the lead's notes.
var (
	adNameKeys      = []string{"fullname", "name"}
	adFirstNameKeys = []string{"firstname"}
	adLastNameKeys  = []string{"lastname"}
	adEmailKeys     = []string{"email", "emailaddress", "workemail"}
	adPhoneKeys     = []string{"phonenumber", "phone", "workphonenumber", "mobilenumber", "mobile"}
	adCompanyKeys   = []string{"companyname", "company", "organization", "organisation", "businessname"}
)

// NotProvided is the company a lead gets when the form did not ask for one;
// the CRM requires a company, and this is easy to filter for and fix later.
const NotProvided = "Not provided"

// LeadInput builds the lead a submission becomes. problem is non-empty when it
// cannot become one, saying why in a sentence for the Integrations page.
func (s AdSubmission) LeadInput() (in LeadInput, problem string) {
	var first, last string
	used := map[int]bool{}
	pick := func(keys []string) string {
		for i, a := range s.Answers {
			if used[i] || strings.TrimSpace(a.Value) == "" {
				continue
			}
			for _, k := range keys {
				if csvKey(a.Key) == k {
					used[i] = true
					return strings.TrimSpace(a.Value)
				}
			}
		}
		return ""
	}

	in.Name = pick(adNameKeys)
	first, last = pick(adFirstNameKeys), pick(adLastNameKeys)
	if in.Name == "" {
		in.Name = strings.TrimSpace(first + " " + last)
	}
	in.Email = pick(adEmailKeys)
	in.Phone = pick(adPhoneKeys)
	in.Company = pick(adCompanyKeys)
	if in.Company == "" {
		in.Company = NotProvided
	}
	in.Source = s.Provider.Source()

	if in.Email == "" {
		return in, "The form gave no email address, which every CRM lead needs. Add an email question to the form."
	}
	if in.Name == "" {
		in.Name = in.Email // better than refusing someone who left their name out
	}

	var other []string
	for i, a := range s.Answers {
		if !used[i] && strings.TrimSpace(a.Value) != "" {
			other = append(other, fmt.Sprintf("%s: %s", a.Label, strings.TrimSpace(a.Value)))
		}
	}
	in.Notes = s.notes(other)

	in.Normalize()
	// A phone the CRM would refuse is kept in the notes rather than losing
	// the whole lead over it.
	if errs := in.Validate(); errs["phone"] != "" {
		in.Notes = truncateRunes(in.Notes+"\nPhone as given: "+in.Phone, maxNotesLen)
		in.Phone = ""
	}
	if errs := in.Validate(); errs.Any() {
		var parts []string
		for _, field := range []string{"name", "company", "email", "notes"} {
			if msg, ok := errs[field]; ok {
				parts = append(parts, fmt.Sprintf("%s %s", field, msg))
			}
		}
		return in, "The lead could not be saved: " + strings.Join(parts, "; ") + "."
	}
	return in, ""
}

// notes says where the lead came from, then lists the answers that have no
// field of their own.
func (s AdSubmission) notes(other []string) string {
	where := fmt.Sprintf("Came in from a %s lead form", s.Provider.Label())
	var details []string
	if s.FormID != "" {
		details = append(details, "form "+s.FormID)
	}
	if s.Campaign != "" {
		details = append(details, "campaign "+s.Campaign)
	}
	if s.Ad != "" {
		details = append(details, "ad "+s.Ad)
	}
	if len(details) > 0 {
		where += " (" + strings.Join(details, ", ") + ")"
	}
	where += "."
	if len(other) > 0 {
		where += "\n\nOther answers:\n" + strings.Join(other, "\n")
	}
	return truncateRunes(where, maxNotesLen)
}

// AgainNote is the timeline entry for an existing lead that filled in a form
// again.
func (s AdSubmission) AgainNote() string {
	note := fmt.Sprintf("Filled in a %s lead form again", s.Provider.Label())
	if s.Campaign != "" {
		note += " (campaign " + s.Campaign + ")"
	}
	return truncateRunes(note+".", 2000)
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// LabelFromKey makes a question label from a platform key: "job_title" and
// "JOB_TITLE" both become "Job title".
func LabelFromKey(key string) string {
	words := strings.Fields(strings.ToLower(strings.NewReplacer("_", " ", "-", " ").Replace(key)))
	if len(words) == 0 {
		return key
	}
	label := strings.Join(words, " ")
	return strings.ToUpper(label[:1]) + label[1:]
}

// ---------------------------------------------------------------------------
// Records
// ---------------------------------------------------------------------------

// AdLeadStatus is what became of a submission. Mirrors ad_leads_status_valid.
type AdLeadStatus string

const (
	AdLeadCreated   AdLeadStatus = "created"
	AdLeadDuplicate AdLeadStatus = "duplicate"
	AdLeadTest      AdLeadStatus = "test"
	AdLeadFailed    AdLeadStatus = "failed"
)

// AdLead is one row of ad_leads: a submission received, and what became of it.
type AdLead struct {
	ID         int64        `json:"id"`
	Provider   AdProvider   `json:"provider"`
	ExternalID string       `json:"external_id"`
	FormID     string       `json:"form_id"`
	Campaign   string       `json:"campaign"`
	Ad         string       `json:"ad"`
	IsTest     bool         `json:"is_test"`
	Status     AdLeadStatus `json:"status"`
	Error      string       `json:"error"`
	LeadID     *int64       `json:"lead_id"`
	LeadName   string       `json:"lead_name"` // "" when no lead, or it was deleted
	Answers    []AdAnswer   `json:"answers"`
	ReceivedAt time.Time    `json:"received_at"`

	Attribution *MetaAttribution `json:"attribution"`
}

// Submission rebuilds the submission a record was made from, to retry it.
func (r AdLead) Submission() AdSubmission {
	return AdSubmission{
		Provider: r.Provider, ExternalID: r.ExternalID, FormID: r.FormID,
		Campaign: r.Campaign, Ad: r.Ad, IsTest: r.IsTest, Answers: r.Answers,
		Attribution: r.Attribution,
	}
}

// SampleSubmission is a made-up submission for the Integrations page's "Send
// test lead": it goes through every step a real one does, but as a test it
// never becomes a lead.
func SampleSubmission(provider AdProvider, now time.Time) AdSubmission {
	return AdSubmission{
		Provider:   provider,
		ExternalID: fmt.Sprintf("test-%d", now.UnixNano()),
		FormID:     "test form",
		Campaign:   "Test campaign",
		IsTest:     true,
		Answers: []AdAnswer{
			{Key: "full_name", Label: "Full name", Value: "Asha Test"},
			{Key: "email", Label: "Email", Value: "asha.test@example.com"},
			{Key: "phone_number", Label: "Phone number", Value: "+91 98765 43210"},
			{Key: "city", Label: "City", Value: "Pune"},
		},
	}
}
