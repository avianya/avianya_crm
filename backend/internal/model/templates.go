package model

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// This file is email templates: reusable emails with {{fields}} that fill in
// from the lead they are sent to. Filling in happens here, on the server, so
// the preview a salesperson checks is exactly what the lead receives.

// EmailTemplate is one row of email_templates.
type EmailTemplate struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Subject   string    `json:"subject"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Length limits, mirrored by the email_templates CHECK constraints. Subject
// and body are sized so a sent email always fits a timeline entry (2000
// characters) with its "Subject:" line.
const (
	maxTemplateNameLen = 120
	maxEmailSubjectLen = 200
	maxEmailBodyLen    = 1750
)

// ---------------------------------------------------------------------------
// Fields
// ---------------------------------------------------------------------------

// TemplateField is one {{field}} a template can use.
type TemplateField struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Example string `json:"example"` // what it looks like, for the template editor
}

// TemplateFields is every field, in the order the editor offers them.
var TemplateFields = []TemplateField{
	{Key: "first_name", Label: "First name", Example: "Asha"},
	{Key: "name", Label: "Full name", Example: "Asha Rao"},
	{Key: "company", Label: "Company", Example: "Kestrel Labs"},
	{Key: "email", Label: "Email", Example: "asha@kestrel.in"},
	{Key: "owner_name", Label: "Owner's name", Example: "Ravi Kumar"},
	{Key: "deal_value", Label: "Deal value", Example: "₹2,50,000"},
}

// fieldPattern matches anything in double braces, so a misspelled field is
// caught as well as a correct one. Spaces inside the braces are allowed.
var fieldPattern = regexp.MustCompile(`\{\{\s*([^{}]*?)\s*\}\}`)

func isTemplateField(key string) bool {
	for _, f := range TemplateFields {
		if f.Key == key {
			return true
		}
	}
	return false
}

// UnknownFields lists, once each and in order of appearance, the {{...}} in
// text that are not TemplateFields. Empty means every field is known.
func UnknownFields(text string) []string {
	var unknown []string
	for _, m := range fieldPattern.FindAllStringSubmatch(text, -1) {
		if !isTemplateField(m[1]) && !containsString(unknown, m[1]) {
			unknown = append(unknown, m[1])
		}
	}
	return unknown
}

func containsString(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

// TemplateValues are what the fields fill in with for one lead.
type TemplateValues map[string]string

// ValuesFor works out every field for a lead. ownerName is "" when the lead
// has no owner. A field with nothing to fill in is "".
func ValuesFor(lead Lead, ownerName string) TemplateValues {
	firstName := lead.Name
	if fields := strings.Fields(lead.Name); len(fields) > 0 {
		firstName = fields[0]
	}
	dealValue := ""
	if lead.DealValue != nil {
		dealValue = FormatRupees(*lead.DealValue)
	}
	return TemplateValues{
		"first_name": firstName,
		"name":       lead.Name,
		"company":    lead.Company,
		"email":      lead.Email,
		"owner_name": ownerName,
		"deal_value": dealValue,
	}
}

// Render fills in the fields of text. It returns the filled-in text and the
// fields that had nothing to fill in with (say {{owner_name}} on an unowned
// lead), which are left blank. Unknown fields are left exactly as written;
// Validate is what rejects them.
func Render(text string, values TemplateValues) (rendered string, missing []string) {
	rendered = fieldPattern.ReplaceAllStringFunc(text, func(match string) string {
		key := fieldPattern.FindStringSubmatch(match)[1]
		value, known := values[key]
		if !known {
			return match
		}
		if value == "" && !containsString(missing, key) {
			missing = append(missing, key)
		}
		return value
	})
	return rendered, missing
}

// FormatRupees writes whole rupees with Indian digit grouping: 250000 is
// "₹2,50,000". The last three digits are grouped, then every two.
func FormatRupees(value int64) string {
	sign := ""
	if value < 0 {
		sign, value = "-", -value
	}
	digits := strconv.FormatInt(value, 10)
	if len(digits) <= 3 {
		return sign + "₹" + digits
	}

	head, tail := digits[:len(digits)-3], digits[len(digits)-3:]
	var groups []string
	for len(head) > 2 {
		groups = append([]string{head[len(head)-2:]}, groups...)
		head = head[:len(head)-2]
	}
	groups = append([]string{head}, groups...)
	return sign + "₹" + strings.Join(groups, ",") + "," + tail
}

// ---------------------------------------------------------------------------
// Input
// ---------------------------------------------------------------------------

// EmailTemplateInput is the body of POST /api/email-templates and PUT
// /api/email-templates/:id.
type EmailTemplateInput struct {
	Name    string `json:"name"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// Normalize trims the name and subject. The body keeps its own spacing, bar
// trailing blank lines.
func (in *EmailTemplateInput) Normalize() {
	in.Name = strings.TrimSpace(in.Name)
	in.Subject = strings.TrimSpace(in.Subject)
	in.Body = strings.TrimRight(in.Body, " \t\r\n")
}

// Validate mirrors the email_templates CHECK constraints, and rejects unknown
// fields so a typo can never reach a lead as "Hi {{frist_name}}".
func (in EmailTemplateInput) Validate() ValidationErrors {
	errs := ValidationErrors{}

	switch {
	case in.Name == "":
		errs["name"] = "is required"
	case utf8.RuneCountInString(in.Name) > maxTemplateNameLen:
		errs["name"] = fmt.Sprintf("must be %d characters or fewer", maxTemplateNameLen)
	}
	validateEmailText(errs, in.Subject, in.Body)
	return errs
}

// EmailInput is the body of POST /api/leads/:id/emails and
// /api/leads/:id/email/preview: the email as written, fields and all.
type EmailInput struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`

	// Send says to send it from the server. False only logs it on the
	// timeline, for an email sent from the person's own email app.
	Send bool `json:"send"`
}

// Normalize trims the subject and trailing blank lines of the body.
func (in *EmailInput) Normalize() {
	in.Subject = strings.TrimSpace(in.Subject)
	in.Body = strings.TrimRight(in.Body, " \t\r\n")
}

// Validate checks the email as written, before its fields are filled in.
func (in EmailInput) Validate() ValidationErrors {
	errs := ValidationErrors{}
	validateEmailText(errs, in.Subject, in.Body)
	return errs
}

func validateEmailText(errs ValidationErrors, subject, body string) {
	switch {
	case subject == "":
		errs["subject"] = "is required"
	case utf8.RuneCountInString(subject) > maxEmailSubjectLen:
		errs["subject"] = fmt.Sprintf("must be %d characters or fewer", maxEmailSubjectLen)
	case strings.ContainsAny(subject, "\r\n"):
		errs["subject"] = "must be on one line"
	default:
		if unknown := UnknownFields(subject); len(unknown) > 0 {
			errs["subject"] = unknownFieldsMessage(unknown)
		}
	}

	switch {
	case body == "":
		errs["body"] = "is required"
	case utf8.RuneCountInString(body) > maxEmailBodyLen:
		errs["body"] = fmt.Sprintf("must be %d characters or fewer", maxEmailBodyLen)
	default:
		if unknown := UnknownFields(body); len(unknown) > 0 {
			errs["body"] = unknownFieldsMessage(unknown)
		}
	}
}

func unknownFieldsMessage(unknown []string) string {
	quoted := make([]string, len(unknown))
	for i, u := range unknown {
		quoted[i] = "{{" + u + "}}"
	}
	known := make([]string, len(TemplateFields))
	for i, f := range TemplateFields {
		known[i] = "{{" + f.Key + "}}"
	}
	return fmt.Sprintf("has unknown field%s %s; the fields are %s",
		plural(len(unknown)), strings.Join(quoted, ", "), strings.Join(known, ", "))
}

// ---------------------------------------------------------------------------
// The filled-in email
// ---------------------------------------------------------------------------

// RenderedEmail is an email with its fields filled in for one lead.
type RenderedEmail struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`

	// Missing lists fields that had nothing to fill in with and were left
	// blank, so the preview can say so before anything is sent.
	Missing []string `json:"missing"`
}

// RenderEmail fills in an email for a lead.
func RenderEmail(in EmailInput, lead Lead, ownerName string) RenderedEmail {
	values := ValuesFor(lead, ownerName)
	subject, missingSubject := Render(in.Subject, values)
	body, missingBody := Render(in.Body, values)

	missing := []string{}
	for _, key := range append(missingSubject, missingBody...) {
		if !containsString(missing, key) {
			missing = append(missing, key)
		}
	}
	return RenderedEmail{To: lead.Email, Subject: subject, Body: body, Missing: missing}
}

// Validate checks the filled-in email still fits: a long company name can
// push a full template over the limit.
func (e RenderedEmail) Validate() ValidationErrors {
	errs := ValidationErrors{}
	if utf8.RuneCountInString(e.Subject) > maxEmailSubjectLen {
		errs["subject"] = fmt.Sprintf("is %d characters once filled in; the limit is %d",
			utf8.RuneCountInString(e.Subject), maxEmailSubjectLen)
	}
	if utf8.RuneCountInString(e.Body) > maxEmailBodyLen {
		errs["body"] = fmt.Sprintf("is %d characters once filled in; the limit is %d",
			utf8.RuneCountInString(e.Body), maxEmailBodyLen)
	}
	return errs
}

// TimelineNote is how the email reads on the lead's timeline. sent says
// whether the CRM sent it, or it was only logged after being sent elsewhere.
func (e RenderedEmail) TimelineNote(sent bool) string {
	how := "Logged"
	if sent {
		how = "Sent from the CRM"
	}
	return fmt.Sprintf("%s · Subject: %s\n\n%s", how, e.Subject, e.Body)
}
