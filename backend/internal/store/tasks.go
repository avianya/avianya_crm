package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"crm-lead-management/backend/internal/model"
)

// taskColumnList is the column list every task query selects, in the order
// scanTask reads it, with each column prefixed by p (a table alias or "").
//
// due_date is cast to text so it arrives as "2006-01-02" -- a calendar date
// with no time zone attached, which is exactly what model.Task.DueDate wants.
// start_time likewise arrives as "15:04"; to_char of NULL is NULL, which is how
// an all-day task comes back.
//
// The last column is the status, see taskStatusSQL.
func taskColumnList(p string) string {
	return fmt.Sprintf(`%[1]sid, %[1]slead_id, %[1]stitle, %[1]stype, %[1]sdue_date::text,
		to_char(%[1]sstart_time, 'HH24:MI'), %[1]sduration_minutes, %[1]scompleted_at, %[1]screated_at,
		%[1]snotes, %[1]sassignee_id, %[1]sreminder_minutes, %[1]sremind_at, %[1]scancelled_at,
		%[2]s`, p, taskStatusSQL(p))
}

// taskStatusSQL works out a task's model.TaskStatus. Overdue means the event
// is over: a timed one once its end has passed, an all-day one once its day
// has. "Now" and the dates are read in the session time zone (APP_TIMEZONE).
func taskStatusSQL(p string) string {
	return fmt.Sprintf(`CASE
			WHEN %[1]scompleted_at IS NOT NULL THEN 'completed'
			WHEN %[1]scancelled_at IS NOT NULL THEN 'cancelled'
			WHEN COALESCE(
			         (%[1]sdue_date + %[1]sstart_time) + make_interval(mins => COALESCE(%[1]sduration_minutes, 0)),
			         (%[1]sdue_date + 1)::timestamp
			     ) < localtimestamp THEN 'overdue'
			ELSE 'upcoming'
		END`, p)
}

var (
	taskColumns = taskColumnList("")

	// Same list, qualified with the "t" table alias, for queries that join.
	taskColumnsQualified = taskColumnList("t.")
)

// openTask is the condition for an event still to be done: neither completed
// nor cancelled. Written once because every "what is still planned" query --
// task lists, the dashboard, a lead's "next step planned" -- must agree on it.
func openTask(p string) string {
	return fmt.Sprintf("%[1]scompleted_at IS NULL AND %[1]scancelled_at IS NULL", p)
}

// scanTask reads one task. extra appends destinations for trailing columns,
// the same trick scanLead uses.
func scanTask(row rowScanner, extra ...any) (model.Task, error) {
	var (
		task             model.Task
		taskType, status string
	)

	dest := []any{
		&task.ID, &task.LeadID, &task.Title, &taskType, &task.DueDate,
		&task.StartTime, &task.DurationMinutes, // nil for an all-day task
		&task.CompletedAt, // *time.Time: NULL while the task is open
		&task.CreatedAt,
		&task.Notes, &task.AssigneeID, &task.ReminderMinutes, &task.RemindAt,
		&task.CancelledAt, &status,
	}
	dest = append(dest, extra...)

	if err := row.Scan(dest...); err != nil {
		return model.Task{}, err
	}
	task.Type = model.TaskType(taskType)
	task.Status = model.TaskStatus(status)
	return task, nil
}

// tasksWithLeadNames runs a query that selects taskColumnsQualified followed by
// l.name and l.company, and returns the tasks with LeadName and LeadCompany
// filled in -- the shape the dashboard's task lists use. Never returns nil, so
// an empty list encodes as [] rather than null.
func (s *Store) tasksWithLeadNames(ctx context.Context, sql string) ([]model.Task, error) {
	rows, err := s.query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tasks := make([]model.Task, 0, 10)
	for rows.Next() {
		var leadName, leadCompany string
		task, err := scanTask(rows, &leadName, &leadCompany)
		if err != nil {
			return nil, fmt.Errorf("scan task: %w", err)
		}
		task.LeadName, task.LeadCompany = leadName, leadCompany
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tasks: %w", err)
	}
	return tasks, nil
}

// ListTasks returns a lead's tasks: open ones first, soonest due first, then
// the completed ones, most recently completed first.
//
// Unlike ListActivities this checks the lead exists, because an empty task list
// is a normal answer and would otherwise hide a wrong id.
func (s *Store) ListTasks(ctx context.Context, leadID int64) ([]model.Task, error) {
	var exists bool
	if err := s.queryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM leads WHERE id = $1)`, leadID,
	).Scan(&exists); err != nil {
		return nil, fmt.Errorf("check lead %d exists: %w", leadID, err)
	}
	if !exists {
		return nil, ErrNotFound
	}

	sql := fmt.Sprintf(`
		SELECT %s
		FROM tasks
		WHERE lead_id = $1
		ORDER BY completed_at IS NOT NULL OR cancelled_at IS NOT NULL,
		         due_date,
		         start_time NULLS FIRST,
		         id`, taskColumns)

	rows, err := s.query(ctx, sql, leadID)
	if err != nil {
		return nil, fmt.Errorf("list tasks for lead %d: %w", leadID, err)
	}
	defer rows.Close()

	tasks := make([]model.Task, 0, 4)
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("scan task: %w", err)
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tasks: %w", err)
	}

	return tasks, nil
}

// ListAllTasks returns tasks across every lead, each with its lead (including
// health and score) attached.
//
// Open tasks come soonest due first, capped at model.MaxOpenTasksListed;
// completed ones latest first, capped at model.MaxCompletedTasksListed.
//
// The leads are fetched in a second query by id rather than joined in, so the
// lead columns are scanned by the same scanLead every other lead read uses, and
// a lead with several tasks is read once.
func (s *Store) ListAllTasks(ctx context.Context, completed bool) ([]model.TaskWithLead, error) {
	where, order, limit := openTask(""), "due_date, start_time NULLS FIRST, id", model.MaxOpenTasksListed
	if completed {
		where, order, limit = "completed_at IS NOT NULL", "completed_at DESC, id DESC", model.MaxCompletedTasksListed
	}

	// where and order are fixed strings chosen above, never user input.
	sql := fmt.Sprintf(`SELECT %s FROM tasks WHERE %s AND lead_id IN (SELECT id FROM leads) ORDER BY %s LIMIT $1`,
		taskColumns, where, order)
	return s.tasksWithLeads(ctx, sql, limit)
}

// ListTasksBetween returns every task due from `from` to `to` inclusive --
// open, completed and cancelled -- each with its lead. It backs the calendar,
// which needs a whole visible range rather than the latest few completed.
func (s *Store) ListTasksBetween(ctx context.Context, from, to string) ([]model.TaskWithLead, error) {
	sql := fmt.Sprintf(`
		SELECT %s FROM tasks
		WHERE due_date BETWEEN $1::date AND $2::date
		  AND lead_id IN (SELECT id FROM leads)
		ORDER BY due_date, start_time NULLS FIRST, id
		LIMIT $3`, taskColumns)
	return s.tasksWithLeads(ctx, sql, from, to, model.MaxRangeTasksListed)
}

// tasksWithLeads runs a query selecting taskColumns and attaches each task's
// lead, read in one batch.
func (s *Store) tasksWithLeads(ctx context.Context, sql string, args ...any) ([]model.TaskWithLead, error) {
	rows, err := s.query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()

	tasks := make([]model.Task, 0, 16)
	leadIDs := make([]int64, 0, 16)
	seen := make(map[int64]bool)
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("scan task: %w", err)
		}
		tasks = append(tasks, task)
		if !seen[task.LeadID] {
			seen[task.LeadID] = true
			leadIDs = append(leadIDs, task.LeadID)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tasks: %w", err)
	}

	items := make([]model.TaskWithLead, 0, len(tasks))
	if len(tasks) == 0 {
		return items, nil
	}

	leadSQL := fmt.Sprintf(`SELECT %s FROM leads WHERE id = ANY($1::bigint[])`, leadColumns)
	leadRows, err := s.query(ctx, leadSQL, leadIDs)
	if err != nil {
		return nil, fmt.Errorf("leads for tasks: %w", err)
	}
	defer leadRows.Close()

	leads := make([]model.Lead, 0, len(leadIDs))
	for leadRows.Next() {
		lead, err := scanLead(leadRows)
		if err != nil {
			return nil, fmt.Errorf("scan task lead: %w", err)
		}
		leads = append(leads, lead)
	}
	if err := leadRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate task leads: %w", err)
	}

	if err := s.attachInsights(ctx, leads); err != nil {
		return nil, err
	}

	byID := make(map[int64]model.Lead, len(leads))
	for _, lead := range leads {
		byID[lead.ID] = lead
	}

	for _, task := range tasks {
		// A lead deleted between the two queries took its tasks with it
		// (ON DELETE CASCADE), so skipping the orphan matches the database.
		if lead, ok := byID[task.LeadID]; ok {
			items = append(items, model.TaskWithLead{Task: task, Lead: lead})
		}
	}

	return items, nil
}

// CreateTask adds a follow-up to a lead. As with CreateActivity, a missing lead
// surfaces as a foreign-key violation and is translated into ErrNotFound; an
// unknown assignee is a field error.
//
// With no assignee given, the event goes to the lead's owner, so reminders
// reach whoever is responsible for the lead without anyone having to pick.
func (s *Store) CreateTask(ctx context.Context, leadID int64, in model.TaskInput) (model.Task, error) {
	sql := fmt.Sprintf(`
		INSERT INTO tasks (lead_id, title, type, due_date, start_time, duration_minutes,
		                   notes, assignee_id, reminder_minutes)
		VALUES ($1, $2, $3, $4::date, NULLIF($5, '')::time, $6,
		        $7, COALESCE($8, (SELECT owner_id FROM leads WHERE id = $1)), $9)
		RETURNING %s`, taskColumns)

	task, err := scanTask(s.queryRow(ctx, sql,
		leadID, in.Title, string(in.Type), in.DueDate, in.StartTime, in.DurationMinutes,
		in.Notes, in.AssigneeID, in.ReminderMinutes))
	if err != nil {
		if isForeignKeyViolationOn(err, "tasks_assignee_id_fkey") {
			return model.Task{}, unknownMember("assignee_id")
		}
		if isForeignKeyViolation(err) {
			return model.Task{}, ErrNotFound
		}
		return model.Task{}, fmt.Errorf("insert task for lead %d: %w", leadID, err)
	}

	return task, nil
}

// UpdateTask replaces a task's editable fields, as the edit form does.
// Completion and cancelling are left alone; they have their own endpoint
// because both also write to the timeline. The trigger recomputes the
// reminder, so a moved event is reminded about at its new time. Returns
// ErrNotFound for an unknown id.
func (s *Store) UpdateTask(ctx context.Context, taskID int64, in model.TaskInput) (model.Task, error) {
	sql := fmt.Sprintf(`
		UPDATE tasks
		SET title = $2,
		    type = $3,
		    due_date = $4::date,
		    start_time = NULLIF($5, '')::time,
		    duration_minutes = $6,
		    notes = $7,
		    assignee_id = $8,
		    reminder_minutes = $9
		WHERE id = $1
		RETURNING %s`, taskColumns)

	task, err := scanTask(s.queryRow(ctx, sql,
		taskID, in.Title, string(in.Type), in.DueDate, in.StartTime, in.DurationMinutes,
		in.Notes, in.AssigneeID, in.ReminderMinutes))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Task{}, ErrNotFound
		}
		if isForeignKeyViolationOn(err, "tasks_assignee_id_fkey") {
			return model.Task{}, unknownMember("assignee_id")
		}
		return model.Task{}, fmt.Errorf("update task %d: %w", taskID, err)
	}

	return task, nil
}

// RescheduleTask moves a task to a new date and time slot, as when it is
// dragged on the calendar. Returns ErrNotFound for an unknown id.
func (s *Store) RescheduleTask(ctx context.Context, taskID int64, in model.TaskScheduleInput) (model.Task, error) {
	sql := fmt.Sprintf(`
		UPDATE tasks
		SET due_date = $2::date,
		    start_time = NULLIF($3, '')::time,
		    duration_minutes = $4
		WHERE id = $1
		RETURNING %s`, taskColumns)

	task, err := scanTask(s.queryRow(ctx, sql,
		taskID, in.DueDate, in.StartTime, in.DurationMinutes))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Task{}, ErrNotFound
		}
		return model.Task{}, fmt.Errorf("reschedule task %d: %w", taskID, err)
	}

	return task, nil
}

// SetTaskCompleted marks a task done or reopens it.
//
// Completing an open task also writes an entry to the lead's timeline, in the
// same transaction, so the activity log is a record of the follow-up actually
// happening: a call, email or meeting for those task types, task_completed for
// a to-do. The activity is returned so the caller can show
// it without re-reading the whole timeline; it is nil when nothing was logged.
//
// Reopening does not delete that entry: the task really was marked done at that
// moment, and the timeline is history, not current state. Completing a task that
// is already complete is a no-op, so a double click cannot log it twice.
func (s *Store) SetTaskCompleted(ctx context.Context, taskID int64, completed bool) (model.Task, *model.Activity, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.Task{}, nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// "prev" sees the row as it was before the update, which is how we know
	// whether this call is the one that actually completed it. FOR UPDATE makes
	// a second, concurrent request wait for this one and then see the task as
	// already done, so two quick clicks cannot both log the completion.
	update := fmt.Sprintf(`
		WITH prev AS (
			SELECT id, completed_at FROM tasks WHERE id = $1 FOR UPDATE
		)
		UPDATE tasks t
		SET completed_at = CASE
			WHEN NOT $2 THEN NULL
			ELSE COALESCE(prev.completed_at, now())
		END,
		    -- Completing a cancelled event means it happened after all.
		    cancelled_at = CASE WHEN $2 THEN NULL ELSE t.cancelled_at END
		FROM prev
		WHERE t.id = prev.id
		RETURNING %s, prev.completed_at IS NULL AS was_open`, taskColumnsQualified)

	var wasOpen bool
	task, err := scanTask(tx.QueryRow(ctx, update, taskID, completed), &wasOpen)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Task{}, nil, ErrNotFound
		}
		return model.Task{}, nil, fmt.Errorf("update task %d: %w", taskID, err)
	}

	var logged *model.Activity
	if completed && wasOpen {
		sql := fmt.Sprintf(`
			INSERT INTO activities (lead_id, type, note)
			VALUES ($1, $2::activity_type, $3)
			RETURNING %s`, activityColumns)

		// A finished call, email or meeting is logged as that contact, with the
		// task's title as the note; a plain to-do as "Completed task: ...".
		activityType := task.Type.CompletionActivity()
		note := fmt.Sprintf("Completed task: %s", task.Title)
		if activityType != model.ActivityTaskCompleted {
			note = task.Title
		}
		activity, err := scanActivity(tx.QueryRow(ctx, sql,
			task.LeadID, string(activityType), note))
		if err != nil {
			return model.Task{}, nil, fmt.Errorf("log completion of task %d: %w", taskID, err)
		}
		logged = &activity
	}

	if err := tx.Commit(ctx); err != nil {
		return model.Task{}, nil, fmt.Errorf("commit task update: %w", err)
	}

	return task, logged, nil
}

// SetTaskCancelled cancels an event or restores a cancelled one.
//
// Cancelling writes a task_cancelled entry to the timeline in the same
// transaction, so the history shows the event was called off rather than it
// silently disappearing; restoring leaves that entry, as reopening a completed
// task does. A completed event cannot be cancelled (ErrTaskCompleted), and
// cancelling twice logs once. The trigger clears the reminder on cancel and
// brings it back on restore.
func (s *Store) SetTaskCancelled(ctx context.Context, taskID int64, cancelled bool) (model.Task, *model.Activity, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.Task{}, nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var completed, wasCancelled bool
	err = tx.QueryRow(ctx, `
		SELECT completed_at IS NOT NULL, cancelled_at IS NOT NULL
		FROM tasks WHERE id = $1 FOR UPDATE`, taskID).Scan(&completed, &wasCancelled)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Task{}, nil, ErrNotFound
		}
		return model.Task{}, nil, fmt.Errorf("read task %d: %w", taskID, err)
	}
	if cancelled && completed {
		return model.Task{}, nil, ErrTaskCompleted
	}

	update := fmt.Sprintf(`
		UPDATE tasks
		SET cancelled_at = CASE WHEN $2 THEN COALESCE(cancelled_at, now()) ELSE NULL END
		WHERE id = $1
		RETURNING %s`, taskColumns)

	task, err := scanTask(tx.QueryRow(ctx, update, taskID, cancelled))
	if err != nil {
		return model.Task{}, nil, fmt.Errorf("cancel task %d: %w", taskID, err)
	}

	var logged *model.Activity
	if cancelled && !wasCancelled {
		sql := fmt.Sprintf(`
			INSERT INTO activities (lead_id, type, note)
			VALUES ($1, 'task_cancelled', $2)
			RETURNING %s`, activityColumns)
		note := fmt.Sprintf("Cancelled %s: %s", strings.ToLower(task.Type.Label()), task.Title)
		activity, err := scanActivity(tx.QueryRow(ctx, sql, task.LeadID, note))
		if err != nil {
			return model.Task{}, nil, fmt.Errorf("log cancellation of task %d: %w", taskID, err)
		}
		logged = &activity
	}

	if err := tx.Commit(ctx); err != nil {
		return model.Task{}, nil, fmt.Errorf("commit task cancel: %w", err)
	}
	return task, logged, nil
}

// DeleteTask removes a task. Returns ErrNotFound when nothing was deleted.
func (s *Store) DeleteTask(ctx context.Context, taskID int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM tasks WHERE id = $1`, taskID)
	if err != nil {
		return fmt.Errorf("delete task %d: %w", taskID, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
