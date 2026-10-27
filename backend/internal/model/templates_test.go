package model

import (
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestUnknownFields(t *testing.T) {
	cases := []struct {
		text string
		want []string
	}{
		{"Hi {{first_name}} at {{ company }}", nil},
		{"Hi {{frist_name}}, {{frist_name}} and {{Company}}", []string{"frist_name", "Company"}},
		{"Empty {{}} braces", []string{""}},
		{"No fields, just { single } braces", nil},
	}
	for _, tc := range cases {
		if got := UnknownFields(tc.text); !slices.Equal(got, tc.want) {
			t.Errorf("UnknownFields(%q) = %q, want %q", tc.text, got, tc.want)
		}
	}
}

func TestRenderFillsFieldsAndReportsMissing(t *testing.T) {
	value := int64(250_000)
	lead := Lead{Name: "Asha  Rao", Company: "Kestrel", Email: "asha@kestrel.in", DealValue: &value}

	got, missing := Render("Hi {{first_name}} ({{ name }}), {{company}}: {{deal_value}}. {{owner_name}}",
		ValuesFor(lead, ""))
	if want := "Hi Asha (Asha  Rao), Kestrel: ₹2,50,000. "; got != want {
		t.Errorf("Render = %q, want %q", got, want)
	}
	if !slices.Equal(missing, []string{"owner_name"}) {
		t.Errorf("missing = %q, want [owner_name]", missing)
	}

	// Unknown fields are left as written, for Validate to reject.
	if got, _ := Render("{{nope}}", ValuesFor(lead, "Ravi")); got != "{{nope}}" {
		t.Errorf("unknown field rendered as %q, want it left alone", got)
	}
}

func TestFormatRupees(t *testing.T) {
	cases := map[int64]string{
		0:             "₹0",
		999:           "₹999",
		1_000:         "₹1,000",
		25_000:        "₹25,000",
		250_000:       "₹2,50,000",
		12_345_678:    "₹1,23,45,678",
		1_000_000_000: "₹1,00,00,00,000",
		-50_000:       "-₹50,000",
	}
	for value, want := range cases {
		if got := FormatRupees(value); got != want {
			t.Errorf("FormatRupees(%d) = %q, want %q", value, got, want)
		}
	}
}

func TestEmailTemplateInputValidate(t *testing.T) {
	ok := EmailTemplateInput{Name: "Intro", Subject: "Hi {{first_name}}", Body: "Hello from {{owner_name}}"}
	if errs := ok.Validate(); errs.Any() {
		t.Fatalf("valid template rejected: %v", errs)
	}

	cases := []struct {
		name  string
		in    EmailTemplateInput
		field string
	}{
		{"no name", EmailTemplateInput{Subject: "s", Body: "b"}, "name"},
		{"no subject", EmailTemplateInput{Name: "n", Body: "b"}, "subject"},
		{"no body", EmailTemplateInput{Name: "n", Subject: "s"}, "body"},
		{"two-line subject", EmailTemplateInput{Name: "n", Subject: "a\nb", Body: "b"}, "subject"},
		{"long subject", EmailTemplateInput{Name: "n", Subject: strings.Repeat("s", 201), Body: "b"}, "subject"},
		{"long body", EmailTemplateInput{Name: "n", Subject: "s", Body: strings.Repeat("b", 1751)}, "body"},
		{"typo in subject", EmailTemplateInput{Name: "n", Subject: "Hi {{frist_name}}", Body: "b"}, "subject"},
		{"typo in body", EmailTemplateInput{Name: "n", Subject: "s", Body: "{{compnay}}"}, "body"},
	}
	for _, tc := range cases {
		errs := tc.in.Validate()
		if errs[tc.field] == "" {
			t.Errorf("%s: want an error on %q, got %v", tc.name, tc.field, errs)
		}
	}

	// The message names the typo and lists the real fields.
	msg := EmailTemplateInput{Name: "n", Subject: "s", Body: "{{compnay}}"}.Validate()["body"]
	if !strings.Contains(msg, "{{compnay}}") || !strings.Contains(msg, "{{company}}") {
		t.Errorf("unknown-field message %q should name the typo and the known fields", msg)
	}
}

func TestRenderedEmailFitsTheTimeline(t *testing.T) {
	// The longest email a template allows, logged, must fit one timeline
	// entry (activities_note_not_blank caps notes at 2000 characters).
	longest := RenderedEmail{Subject: strings.Repeat("s", maxEmailSubjectLen), Body: strings.Repeat("b", maxEmailBodyLen)}
	for _, sent := range []bool{true, false} {
		if n := utf8.RuneCountInString(longest.TimelineNote(sent)); n > maxNoteLen {
			t.Errorf("timeline note is %d characters (sent=%v), over the %d limit", n, sent, maxNoteLen)
		}
	}

	// A field can push a full template over the limit once filled in.
	lead := Lead{Name: "A", Company: strings.Repeat("c", 120)}
	in := EmailInput{Subject: "s", Body: strings.Repeat("b", maxEmailBodyLen-11) + "{{company}}"}
	email := RenderEmail(in, lead, "")
	if errs := email.Validate(); errs["body"] == "" {
		t.Errorf("a body over the limit once filled in should be rejected, got %v", errs)
	}

	note := RenderEmail(EmailInput{Subject: "Hi {{first_name}}", Body: "Hello"}, Lead{Name: "Asha"}, "").TimelineNote(true)
	if want := "Sent from the CRM · Subject: Hi Asha\n\nHello"; note != want {
		t.Errorf("timeline note = %q, want %q", note, want)
	}
}

func TestEveryTemplateFieldFillsIn(t *testing.T) {
	value := int64(10_000)
	lead := Lead{Name: "Asha Rao", Company: "Kestrel", Email: "asha@kestrel.in", DealValue: &value}
	values := ValuesFor(lead, "Ravi")
	for _, f := range TemplateFields {
		if values[f.Key] == "" {
			t.Errorf("field %q has no value for a fully filled-in lead", f.Key)
		}
		if f.Label == "" || f.Example == "" {
			t.Errorf("field %q needs a label and an example for the editor", f.Key)
		}
	}
	if len(values) != len(TemplateFields) {
		t.Errorf("ValuesFor fills %d fields, TemplateFields lists %d", len(values), len(TemplateFields))
	}
}
