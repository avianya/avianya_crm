// Package reminders is the background job that turns due event reminders into
// notifications and emails them to the right person.
//
// It runs inside the API server rather than in the browser, so reminders fire
// whether or not anyone has the app open. All coordination happens in the
// database (see store/notifications.go), so running two servers is safe.
package reminders

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"crm-lead-management/backend/internal/model"
	"crm-lead-management/backend/internal/store"
)

// Interval is how often the job looks for due reminders. A 5-minute reminder
// therefore goes out between 5 minutes and 4.5 minutes before the start.
const Interval = 30 * time.Second

// batchSize caps the emails sent per tick, so one tick stays short.
const batchSize = 20

// Mailer is what the job needs from internal/mail, as an interface so tests
// can stand in for a real server.
type Mailer interface {
	Configured() bool
	Send(ctx context.Context, to, subject, body string) error
}

// Runner is the reminder job.
type Runner struct {
	store    *store.Store
	mailer   Mailer
	location *time.Location
	baseURL  string
	logger   *slog.Logger

	// digestHour is the local hour from which daily summaries go out.
	digestHour int
}

// New builds a Runner. location is the time zone times in emails are written
// in; baseURL is the web app's address, for the link to the lead; digestHour
// is the local hour from which daily summaries are sent.
func New(st *store.Store, mailer Mailer, location *time.Location, baseURL string, digestHour int, logger *slog.Logger) *Runner {
	return &Runner{store: st, mailer: mailer, location: location, baseURL: baseURL, digestHour: digestHour, logger: logger}
}

// Run ticks until ctx is cancelled. A failed tick is logged and the next one
// tries again; nothing here can stop the API server.
func (r *Runner) Run(ctx context.Context) {
	r.logger.Info("reminder job started",
		slog.Duration("interval", Interval),
		slog.Bool("email", r.mailer.Configured()),
	)

	ticker := time.NewTicker(Interval)
	defer ticker.Stop()

	for {
		r.Tick(ctx)
		select {
		case <-ctx.Done():
			r.logger.Info("reminder job stopped")
			return
		case <-ticker.C:
		}
	}
}

// Tick does one round: raise stuck-deal alerts and due reminders, then send
// the emails that are due.
func (r *Runner) Tick(ctx context.Context) {
	// Independent of reminders: a failure here is logged and the reminders
	// still go out.
	if alerts, err := r.store.RaiseStuckDealAlerts(ctx); err != nil {
		if ctx.Err() == nil {
			r.logger.Error("raise stuck deal alerts failed", slog.String("error", err.Error()))
		}
	} else if alerts > 0 {
		r.logger.Info("stuck deal alerts raised", slog.Int("count", alerts))
	}

	r.sendDailyDigests(ctx)

	raised, err := r.store.RaiseDueReminders(ctx, r.mailer.Configured())
	if err != nil {
		if ctx.Err() == nil {
			r.logger.Error("raise reminders failed", slog.String("error", err.Error()))
		}
		return
	}
	if raised > 0 {
		r.logger.Info("reminders raised", slog.Int("count", raised))
	}

	ids, err := r.store.ClaimReminderEmails(ctx, batchSize)
	if err != nil {
		if ctx.Err() == nil {
			r.logger.Error("claim reminder emails failed", slog.String("error", err.Error()))
		}
		return
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return // shutting down; the lease will expire and another tick resumes
		}
		r.sendOne(ctx, id)
	}
}

// sendOne re-checks one claimed reminder against the current state of the
// event, then sends it or records why not.
func (r *Runner) sendOne(ctx context.Context, id int64) {
	log := r.logger.With(slog.Int64("notification_id", id))

	job, err := r.store.ReminderJob(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return // the event was deleted, taking its notification with it
		}
		log.Error("load reminder failed", slog.String("error", err.Error()))
		r.finish(ctx, id, store.EmailOutcome{Status: model.EmailFailed, Detail: "Could not load the event: " + err.Error()})
		return
	}

	if reason := job.SkipReason(time.Now(), r.location); reason != "" {
		log.Info("reminder email skipped", slog.String("reason", reason))
		r.finish(ctx, id, store.EmailOutcome{Status: model.EmailSkipped, Detail: reason})
		return
	}
	if !r.mailer.Configured() {
		r.finish(ctx, id, store.EmailOutcome{Status: model.EmailSkipped,
			Detail: "Email is not set up on the server (SMTP settings are missing)."})
		return
	}

	subject, body := model.ReminderEmail(model.ReminderEmailData{
		RecipientName: *job.RecipientName,
		Task:          job.Task,
		Lead:          job.Lead,
		LeadURL:       fmt.Sprintf("%s/leads/%d", r.baseURL, job.Lead.ID),
		Location:      r.location,
	})

	to := *job.RecipientEmail
	if err := r.mailer.Send(ctx, to, subject, body); err != nil {
		log.Warn("reminder email failed", slog.String("to", to), slog.String("error", err.Error()))
		r.finish(ctx, id, store.EmailOutcome{Status: model.EmailFailed, Detail: err.Error()})
		return
	}

	log.Info("reminder email sent", slog.String("to", to))
	r.finish(ctx, id, store.EmailOutcome{Status: model.EmailSent, SentTo: to, Detail: "Sent to " + to})
}

func (r *Runner) finish(ctx context.Context, id int64, out store.EmailOutcome) {
	if err := r.store.FinishReminderEmail(ctx, id, out); err != nil && ctx.Err() == nil {
		r.logger.Error("record reminder email failed",
			slog.Int64("notification_id", id), slog.String("error", err.Error()))
	}
}

// sendDailyDigests sends today's summary to each member who wants one and has
// not had it, once the digest hour has come. With no mail server set up it
// does nothing, so the summaries go out on the first tick after one is.
func (r *Runner) sendDailyDigests(ctx context.Context) {
	if !r.mailer.Configured() {
		return
	}
	claims, err := r.store.ClaimDailyDigests(ctx, r.digestHour)
	if err != nil {
		if ctx.Err() == nil {
			r.logger.Error("claim daily digests failed", slog.String("error", err.Error()))
		}
		return
	}
	for _, claim := range claims {
		if ctx.Err() != nil {
			return // shutting down; the claim is retried once it goes stale
		}
		r.sendDigest(ctx, claim)
	}
}

// sendDigest builds one member's summary and sends it, or records why not.
func (r *Runner) sendDigest(ctx context.Context, claim store.DigestClaim) {
	m := claim.Member
	log := r.logger.With(slog.Int64("member_id", m.ID))

	finish := func(status model.DigestStatus, detail string) {
		if err := r.store.FinishDailyDigest(ctx, m.ID, claim.Date, status, detail); err != nil && ctx.Err() == nil {
			log.Error("record daily digest failed", slog.String("error", err.Error()))
		}
	}

	summary, err := r.store.FollowUps(ctx, model.OwnerFilter{OwnerID: &m.ID})
	if err != nil {
		log.Error("build daily digest failed", slog.String("error", err.Error()))
		finish(model.DigestFailed, "Could not read the follow-ups: "+err.Error())
		return
	}
	if !model.DigestHasNews(summary) {
		finish(model.DigestSkipped, "Nothing was overdue, due, stuck or going cold.")
		return
	}

	subject, body := model.DigestEmail(model.DigestEmailData{
		RecipientName: m.Name,
		Summary:       summary,
		FollowUpsURL:  fmt.Sprintf("%s/follow-ups?owner=%d", r.baseURL, m.ID),
		Location:      r.location,
	})
	if err := r.mailer.Send(ctx, m.Email, subject, body); err != nil {
		log.Warn("daily digest failed", slog.String("to", m.Email), slog.String("error", err.Error()))
		finish(model.DigestFailed, err.Error())
		return
	}
	log.Info("daily digest sent", slog.String("to", m.Email))
	finish(model.DigestSent, "Sent to "+m.Email)
}
