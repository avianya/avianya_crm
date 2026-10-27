package reminders

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"crm-lead-management/backend/internal/model"
	"crm-lead-management/backend/internal/store"
)

// fakeMailer records what would have been sent.
type fakeMailer struct {
	sent []sentMail
}

type sentMail struct{ to, subject, body string }

func (f *fakeMailer) Configured() bool { return true }
func (f *fakeMailer) Send(_ context.Context, to, subject, body string) error {
	f.sent = append(f.sent, sentMail{to, subject, body})
	return nil
}

// sendDigest is tested directly, with a hand-made claim, rather than through
// ClaimDailyDigests: claiming takes every member in the database, and the
// store package's tests share it and run at the same time.
func TestSendDigestEmailsOnlyWhenThereIsNews(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set - skipping database integration tests")
	}
	ctx := context.Background()
	st, err := store.New(ctx, dsn, "")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(st.Close)

	stamp := time.Now().UnixNano()
	newMember := func(name string) model.TeamMember {
		in := model.TeamMemberInput{Name: name, Email: fmt.Sprintf("digest-run-%d-%s@example.test", stamp, strings.Fields(name)[0])}
		in.Normalize()
		m, err := st.CreateTeamMember(ctx, in)
		if err != nil {
			t.Fatalf("CreateTeamMember: %v", err)
		}
		t.Cleanup(func() { _ = st.DeleteTeamMember(ctx, m.ID) })
		return m
	}
	busy := newMember("Busy Person")
	idle := newMember("Idle Person")

	// Busy owns a lead with a call that was due yesterday.
	leadIn := model.LeadInput{
		Name: "Digest Lead", Company: "Digest Co",
		Email:   fmt.Sprintf("digest-lead-%d@example.test", stamp),
		OwnerID: &busy.ID,
	}
	leadIn.Normalize()
	lead, err := st.CreateLead(ctx, leadIn)
	if err != nil {
		t.Fatalf("CreateLead: %v", err)
	}
	t.Cleanup(func() { _ = st.DeleteLead(ctx, lead.ID) })

	yesterday := time.Now().AddDate(0, 0, -1).Format(model.DateLayout)
	taskIn := model.TaskInput{Title: "Call back about pricing", Type: model.TaskCall, DueDate: yesterday}
	taskIn.Normalize()
	if _, err := st.CreateTask(ctx, lead.ID, taskIn); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	mailer := &fakeMailer{}
	r := New(st, mailer, time.Local, "http://crm.test", 0, slog.New(slog.NewTextHandler(io.Discard, nil)))

	today := time.Now().Format(model.DateLayout)
	r.sendDigest(ctx, store.DigestClaim{Member: busy, Date: today})
	r.sendDigest(ctx, store.DigestClaim{Member: idle, Date: today})

	if len(mailer.sent) != 1 {
		t.Fatalf("sent %d emails, want 1 (only Busy has news): %+v", len(mailer.sent), mailer.sent)
	}
	got := mailer.sent[0]
	if got.to != busy.Email {
		t.Errorf("sent to %q, want %q", got.to, busy.Email)
	}
	if !strings.Contains(got.subject, "1 overdue") {
		t.Errorf("subject = %q, want it to count 1 overdue", got.subject)
	}
	for _, want := range []string{
		"Good morning Busy,",
		"Call: Call back about pricing, Digest Lead (Digest Co), due " + yesterday,
		fmt.Sprintf("http://crm.test/follow-ups?owner=%d", busy.ID),
	} {
		if !strings.Contains(got.body, want) {
			t.Errorf("body is missing %q:\n%s", want, got.body)
		}
	}
}
