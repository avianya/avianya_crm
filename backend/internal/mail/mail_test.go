package mail

import (
	"context"
	"errors"
	"mime"
	"net/mail"
	"strings"
	"testing"

	"crm-lead-management/backend/internal/config"
)

func TestBuildMessage(t *testing.T) {
	from, _ := mail.ParseAddress("Pipeline CRM <crm@example.com>")
	to, _ := mail.ParseAddress("ravi@example.com")

	raw, err := buildMessage(from, to, nil, "Reminder: Call with Aarav Shāh", "Hi Ravi,\nLine two\n", true)
	if err != nil {
		t.Fatal(err)
	}
	msg := string(raw)

	for _, want := range []string{
		"From: \"Pipeline CRM\" <crm@example.com>\r\n",
		"To: <ravi@example.com>\r\n",
		"Subject: =?utf-8?q?",
		"Content-Type: text/plain; charset=utf-8\r\n",
		"Content-Transfer-Encoding: quoted-printable\r\n",
		"Auto-Submitted: auto-generated\r\n",
		"\r\n\r\nHi Ravi,\r\nLine two\r\n",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message is missing %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "Reply-To:") {
		t.Errorf("no reply-to was given, but the message has one:\n%s", msg)
	}

	// The parsed message must round-trip to the original subject.
	parsed, err := mail.ReadMessage(strings.NewReader(msg))
	if err != nil {
		t.Fatal(err)
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(parsed.Header.Get("Subject"))
	if err != nil || subject != "Reminder: Call with Aarav Shāh" {
		t.Errorf("subject round-trip = %q, %v", subject, err)
	}
}

func TestBuildMessageWrittenByAPerson(t *testing.T) {
	from, _ := mail.ParseAddress("Pipeline CRM <crm@example.com>")
	to, _ := mail.ParseAddress("asha@kestrel.in")
	replyTo, _ := mail.ParseAddress("Ravi Kumar <ravi@example.com>")

	raw, err := buildMessage(from, to, replyTo, "Following up", "Hi Asha,\n", false)
	if err != nil {
		t.Fatal(err)
	}
	msg := string(raw)

	if !strings.Contains(msg, "Reply-To: \"Ravi Kumar\" <ravi@example.com>\r\n") {
		t.Errorf("message is missing the reply-to:\n%s", msg)
	}
	if strings.Contains(msg, "Auto-Submitted:") {
		t.Errorf("an email written by a person must not be marked auto-generated:\n%s", msg)
	}
}

func TestSendRejectsInjectionAndMissingConfig(t *testing.T) {
	ctx := context.Background()

	if err := New(config.SMTPConfig{}).Send(ctx, "a@b.co", "x", "y"); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("unconfigured: got %v", err)
	}

	s := New(config.SMTPConfig{Host: "127.0.0.1", Port: "1", From: "crm@example.com"})
	if err := s.Send(ctx, "a@b.co", "hi\r\nBcc: victim@example.com", "y"); err == nil ||
		!strings.Contains(err.Error(), "line break") {
		t.Errorf("subject injection: got %v", err)
	}
	if err := s.Send(ctx, "a@b.co\r\nBcc: victim@example.com", "hi", "y"); err == nil ||
		!strings.Contains(err.Error(), "not a valid address") {
		t.Errorf("recipient injection: got %v", err)
	}
}

func TestAppPasswordSpacesRemoved(t *testing.T) {
	s := New(config.SMTPConfig{Password: "abcd efgh ijkl mnop"})
	if s.cfg.Password != "abcdefghijklmnop" {
		t.Errorf("password = %q", s.cfg.Password)
	}
}
