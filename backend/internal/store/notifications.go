package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"crm-lead-management/backend/internal/model"
)

// Reminders move through three steps, each a single statement so that two API
// servers running the reminder job at once cannot double up:
//
//  1. RaiseDueReminders turns every reminder that has come due into a
//     notification row. UNIQUE (task_id, remind_at) makes that exactly-once.
//  2. ClaimReminderEmails takes a batch of emails to send, marking them
//     "sending" with a short lease. FOR UPDATE SKIP LOCKED means two servers
//     never claim the same row.
//  3. FinishReminderEmail records what happened: sent, failed (retried later
//     with back-off) or skipped.
//
// If a server dies mid-send, the lease runs out and the email is claimed again,
// so a reminder can in rare cases arrive twice -- but it is never silently lost.

// reminderLease is how long a claimed email is reserved for the server that
// claimed it. Sending takes seconds; this only matters if that server dies.
const reminderLease = 2 * time.Minute

// reminderCatchUp is how late a reminder may still be raised, e.g. after the
// server was down. Older ones are dropped rather than sent hours late.
const reminderCatchUp = "1 day"

// RaiseDueReminders creates a notification for each reminder that has come
// due, and returns how many it created.
//
// The email recipient is fixed here: the event's assignee, else the lead's
// owner. Emails that cannot go out are recorded as skipped with the reason, so
// the bell can say why rather than leaving the user guessing.
//
// A reminder whose event has already started (or, all day, whose day is over)
// is not raised: reminding someone about the past helps nobody.
func (s *Store) RaiseDueReminders(ctx context.Context, emailConfigured bool) (int, error) {
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO notifications
		       (task_id, remind_at, recipient_id, recipient_email,
		        email_status, email_detail, email_next_attempt_at)
		SELECT t.id, t.remind_at, m.id, m.email,
		       CASE
		           WHEN m.id IS NULL           THEN 'skipped'
		           WHEN NOT m.email_reminders THEN 'skipped'
		           WHEN NOT $1                 THEN 'skipped'
		           ELSE 'pending'
		       END,
		       CASE
		           WHEN m.id IS NULL           THEN 'Nobody is assigned to this event or owns the lead.'
		           WHEN NOT m.email_reminders THEN 'Email reminders are turned off for ' || m.name || '.'
		           WHEN NOT $1                 THEN 'Email is not set up on the server (SMTP settings are missing).'
		           ELSE ''
		       END,
		       now()
		FROM tasks t
		JOIN leads l ON l.id = t.lead_id
		LEFT JOIN team_members m ON m.id = COALESCE(t.assignee_id, l.owner_id)
		WHERE t.remind_at IS NOT NULL
		  AND t.remind_at <= now()
		  AND t.remind_at >  now() - interval '`+reminderCatchUp+`'
		  AND (t.due_date + COALESCE(t.start_time, TIME '23:59'))::timestamptz > now()
		ON CONFLICT (task_id, remind_at) DO NOTHING`, emailConfigured)
	if err != nil {
		return 0, fmt.Errorf("raise due reminders: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// ClaimReminderEmails reserves up to limit emails that are due to be sent (or
// retried) and returns their notification ids.
//
// An email still "sending" after its lease has expired belongs to a server
// that died mid-send; it is claimed again, unless it has used up its attempts,
// in which case it is marked failed.
func (s *Store) ClaimReminderEmails(ctx context.Context, limit int) ([]int64, error) {
	if _, err := s.pool.Exec(ctx, `
		UPDATE notifications
		SET email_status = 'failed',
		    email_detail = 'The server stopped while sending this email.',
		    email_next_attempt_at = NULL
		WHERE email_status = 'sending'
		  AND email_next_attempt_at <= now()
		  AND email_attempts >= $1`, model.MaxEmailAttempts); err != nil {
		return nil, fmt.Errorf("expire stuck reminder emails: %w", err)
	}

	rows, err := s.query(ctx, `
		UPDATE notifications n
		SET email_status = 'sending',
		    email_attempts = n.email_attempts + 1,
		    email_next_attempt_at = now() + make_interval(secs => $3)
		WHERE n.id IN (
		    SELECT id FROM notifications
		    WHERE email_status IN ('pending', 'failed', 'sending')
		      AND email_next_attempt_at <= now()
		      AND email_attempts < $1
		    ORDER BY email_next_attempt_at
		    LIMIT $2
		    FOR UPDATE SKIP LOCKED
		)
		RETURNING n.id`, model.MaxEmailAttempts, limit, reminderLease.Seconds())
	if err != nil {
		return nil, fmt.Errorf("claim reminder emails: %w", err)
	}
	defer rows.Close()

	ids := make([]int64, 0, limit)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan claimed id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ReminderJob loads a claimed email with the event, lead and recipient as they
// are now, for SkipReason to re-check before anything is sent.
func (s *Store) ReminderJob(ctx context.Context, notificationID int64) (model.ReminderJob, error) {
	sql := fmt.Sprintf(`
		SELECT %s,
		       n.id, n.remind_at, n.recipient_id, m.name, m.email, COALESCE(m.email_reminders, false),
		       COALESCE(t.assignee_id, l.owner_id),
		       l.id, l.name, l.company, l.email, l.phone, l.stage::text
		FROM notifications n
		JOIN tasks t ON t.id = n.task_id
		JOIN leads l ON l.id = t.lead_id
		LEFT JOIN team_members m ON m.id = n.recipient_id
		WHERE n.id = $1`, taskColumnsQualified)

	var (
		job   model.ReminderJob
		stage string
	)
	task, err := scanTask(s.queryRow(ctx, sql, notificationID),
		&job.NotificationID, &job.RemindAt, &job.RecipientID, &job.RecipientName, &job.RecipientEmail,
		&job.RecipientEmailOn, &job.CurrentRecipientID,
		&job.Lead.ID, &job.Lead.Name, &job.Lead.Company, &job.Lead.Email, &job.Lead.Phone, &stage)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.ReminderJob{}, ErrNotFound
		}
		return model.ReminderJob{}, fmt.Errorf("load reminder %d: %w", notificationID, err)
	}
	job.Task = task
	job.Lead.Stage = model.Stage(stage)
	return job, nil
}

// EmailOutcome is what happened to one claimed email.
type EmailOutcome struct {
	Status model.EmailStatus // sent, failed or skipped
	Detail string
	SentTo string // the address it went to, when sent
}

// FinishReminderEmail records an outcome. A failure is scheduled for retry
// with back-off until it has used up model.MaxEmailAttempts. Only an email
// still marked "sending" is updated, so a late finish after the lease was
// taken over cannot overwrite the newer attempt.
func (s *Store) FinishReminderEmail(ctx context.Context, notificationID int64, out EmailOutcome) error {
	var err error
	switch out.Status {
	case model.EmailSent:
		_, err = s.pool.Exec(ctx, `
			UPDATE notifications
			SET email_status = 'sent', email_sent_at = now(), email_next_attempt_at = NULL,
			    recipient_email = $2, email_detail = $3
			WHERE id = $1 AND email_status = 'sending'`,
			notificationID, out.SentTo, out.Detail)

	case model.EmailFailed:
		_, err = s.pool.Exec(ctx, `
			UPDATE notifications
			SET email_status = 'failed',
			    email_detail = $2,
			    email_next_attempt_at = CASE
			        WHEN email_attempts >= $3 THEN NULL
			        ELSE now() + make_interval(secs => ($4::int[])[LEAST(email_attempts, cardinality($4::int[]))])
			    END
			WHERE id = $1 AND email_status = 'sending'`,
			notificationID, out.Detail, model.MaxEmailAttempts, model.RetryDelaySeconds())

	default:
		_, err = s.pool.Exec(ctx, `
			UPDATE notifications
			SET email_status = 'skipped', email_detail = $2, email_next_attempt_at = NULL
			WHERE id = $1 AND email_status = 'sending'`,
			notificationID, out.Detail)
	}
	if err != nil {
		return fmt.Errorf("record reminder email %d: %w", notificationID, err)
	}
	return nil
}

// RetryReminderEmail queues a failed or skipped email to be tried again now,
// with a fresh set of attempts. The recipient is looked up again, so fixing a
// missing assignee or owner and then retrying sends it to them. It only queues
// the email: the send itself still re-checks everything, so retrying a reminder
// for a cancelled or past event records why it was skipped.
func (s *Store) RetryReminderEmail(ctx context.Context, notificationID int64) (model.Notification, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE notifications n
		SET email_status = 'pending',
		    email_attempts = 0,
		    email_next_attempt_at = now(),
		    email_detail = '',
		    recipient_id = COALESCE(t.assignee_id, l.owner_id)
		FROM tasks t
		JOIN leads l ON l.id = t.lead_id
		WHERE n.id = $1
		  AND t.id = n.task_id
		  AND n.email_status IN ('failed', 'skipped')`, notificationID)
	if err != nil {
		return model.Notification{}, fmt.Errorf("retry reminder email %d: %w", notificationID, err)
	}
	if tag.RowsAffected() == 0 {
		// Either no such notification, or it is not in a retryable state.
		n, err := s.GetNotification(ctx, notificationID)
		if err != nil {
			return model.Notification{}, err
		}
		return n, ErrNotRetryable
	}
	return s.GetNotification(ctx, notificationID)
}

// ErrNotRetryable means the email is already sent, queued or being sent. -> 409
var ErrNotRetryable = errors.New("store: the email is not in a state that can be retried")

// ---------------------------------------------------------------------------
// The bell
// ---------------------------------------------------------------------------

func notificationSelect(where string) string {
	return fmt.Sprintf(`
		SELECT n.id, n.remind_at,
		       t.id, t.title, t.type, %s, t.due_date::text, to_char(t.start_time, 'HH24:MI'), t.duration_minutes,
		       l.id, l.name, l.company,
		       n.recipient_id, m.name, n.recipient_email,
		       n.created_at, n.read_at,
		       n.email_status, n.email_attempts, n.email_next_attempt_at, n.email_sent_at, n.email_detail
		FROM notifications n
		JOIN tasks t ON t.id = n.task_id
		JOIN leads l ON l.id = t.lead_id
		LEFT JOIN team_members m ON m.id = n.recipient_id
		%s`, taskStatusSQL("t."), where)
}

func scanNotification(row rowScanner) (model.Notification, error) {
	var (
		n                          model.Notification
		taskType, status, emailSta string
	)
	err := row.Scan(&n.ID, &n.RemindAt,
		&n.TaskID, &n.TaskTitle, &taskType, &status, &n.DueDate, &n.StartTime, &n.DurationMinutes,
		&n.LeadID, &n.LeadName, &n.LeadCompany,
		&n.RecipientID, &n.RecipientName, &n.RecipientEmail,
		&n.CreatedAt, &n.ReadAt,
		&emailSta, &n.EmailAttempts, &n.EmailNextAttemptAt, &n.EmailSentAt, &n.EmailDetail)
	n.TaskType = model.TaskType(taskType)
	n.TaskStatus = model.TaskStatus(status)
	n.EmailStatus = model.EmailStatus(emailSta)
	return n, err
}

// ListNotifications returns the latest notifications, newest first, and how
// many are unread in total.
func (s *Store) ListNotifications(ctx context.Context, limit int) (model.NotificationList, error) {
	list := model.NotificationList{Items: make([]model.Notification, 0, limit), DealAlerts: []model.DealAlert{}}

	rows, err := s.query(ctx,
		notificationSelect(`ORDER BY n.created_at DESC, n.id DESC LIMIT $1`), limit)
	if err != nil {
		return list, fmt.Errorf("list notifications: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return list, fmt.Errorf("scan notification: %w", err)
		}
		list.Items = append(list.Items, n)
	}
	if err := rows.Err(); err != nil {
		return list, fmt.Errorf("iterate notifications: %w", err)
	}

	if err := s.queryRow(ctx,
		`SELECT COUNT(*) FROM notifications n JOIN tasks t ON t.id = n.task_id JOIN leads l ON l.id = t.lead_id
		 WHERE n.read_at IS NULL`).Scan(&list.Unread); err != nil {
		return list, fmt.Errorf("count unread notifications: %w", err)
	}

	alerts, alertsUnread, err := s.listDealAlerts(ctx, model.MaxDealAlertsListed)
	if err != nil {
		return list, err
	}
	list.DealAlerts, list.AlertsUnread = alerts, alertsUnread
	return list, nil
}

// GetNotification returns one notification, or ErrNotFound.
func (s *Store) GetNotification(ctx context.Context, id int64) (model.Notification, error) {
	n, err := scanNotification(s.queryRow(ctx, notificationSelect(`WHERE n.id = $1`), id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Notification{}, ErrNotFound
		}
		return model.Notification{}, fmt.Errorf("get notification %d: %w", id, err)
	}
	return n, nil
}

// MarkNotificationRead marks one notification read. Already-read is fine.
func (s *Store) MarkNotificationRead(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE notifications SET read_at = COALESCE(read_at, now()) WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("mark notification %d read: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkAllNotificationsRead marks every unread notification and deal alert read.
//
// A viewer who sees only their own leads marks only those leads' reminders and
// alerts; reads are scoped by scope.go, but this is a write and says so itself.
func (s *Store) MarkAllNotificationsRead(ctx context.Context) error {
	own, args := "", []any{}
	if v, ok := ViewerFrom(ctx); ok && !v.SeesAllLeads() {
		own, args = " AND lead_id IN (SELECT id FROM leads WHERE owner_id = $1)", []any{v.MemberID}
	}
	if _, err := s.pool.Exec(ctx,
		`UPDATE notifications SET read_at = now() WHERE read_at IS NULL
		   AND task_id IN (SELECT id FROM tasks WHERE true`+own+`)`, args...); err != nil {
		return fmt.Errorf("mark all notifications read: %w", err)
	}
	if _, err := s.pool.Exec(ctx,
		`UPDATE deal_alerts SET read_at = now() WHERE read_at IS NULL`+own, args...); err != nil {
		return fmt.Errorf("mark all deal alerts read: %w", err)
	}
	return nil
}

// SessionTimezone is the time zone database sessions run in -- APP_TIMEZONE
// when set, else the server's default. The API uses it for anything it
// formats itself, so its idea of "3:00 PM" matches the database's.
func (s *Store) SessionTimezone(ctx context.Context) (string, error) {
	var tz string
	err := s.queryRow(ctx, `SELECT current_setting('TimeZone')`).Scan(&tz)
	return tz, err
}

// LeadNextSteps works out a lead's suggested next steps from its record,
// events and timeline.
func (s *Store) LeadNextSteps(ctx context.Context, leadID int64, now time.Time, loc *time.Location) ([]model.NextStep, error) {
	lead, err := s.GetLead(ctx, leadID) // with timeline and insights
	if err != nil {
		return nil, err
	}
	tasks, err := s.ListTasks(ctx, leadID)
	if err != nil {
		return nil, err
	}
	return model.NextSteps(model.NextStepsInput{
		Lead:       lead,
		Tasks:      tasks,
		Activities: lead.Activities,
		Now:        now,
		Location:   loc,
	}), nil
}
