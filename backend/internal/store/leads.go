package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"crm-lead-management/backend/internal/model"
)

// The column list is written once and reused by every SELECT that returns a
// lead, so all of them stay in the same order as scanLead expects.
//
// source and stage are cast to ::text because they are Postgres enum types.
// Casting server-side means the Go code only ever deals in plain strings and
// does not need pgx to know anything about our custom types.
const leadColumns = `
	id, name, company, email, phone,
	source::text, stage::text,
	notes, created_at, updated_at`

// Same list, qualified with the "l" table alias, for queries that join.
const leadColumnsQualified = `
	l.id, l.name, l.company, l.email, l.phone,
	l.source::text, l.stage::text,
	l.notes, l.created_at, l.updated_at`

// rowScanner is the one method pgx.Row and pgx.Rows have in common, which lets
// scanLead serve both single-row and multi-row queries.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanLead reads one lead from a row.
//
// extra lets a caller append destinations for additional trailing columns (the
// list query selects a window-function total alongside each row). Everything is
// scanned in one Scan call because that is the only way pgx exposes a row.
func scanLead(row rowScanner, extra ...any) (model.Lead, error) {
	var (
		lead          model.Lead
		source, stage string // scanned as text, then converted to our typed enums
	)

	dest := []any{
		&lead.ID, &lead.Name, &lead.Company, &lead.Email,
		&lead.Phone, // *string: a SQL NULL phone becomes a nil pointer
		&source, &stage,
		&lead.Notes, &lead.CreatedAt, &lead.UpdatedAt,
	}
	dest = append(dest, extra...)

	if err := row.Scan(dest...); err != nil {
		return model.Lead{}, err
	}

	lead.Source = model.Source(source)
	lead.Stage = model.Stage(stage)
	return lead, nil
}

// ---------------------------------------------------------------------------
// List
// ---------------------------------------------------------------------------

// ListLeads returns one page of leads matching the query, together with the
// total number of matches across all pages.
//
// The total comes from COUNT(*) OVER() -- a window function evaluated after
// WHERE but before LIMIT -- so filtering, paging and counting are all done in a
// single round trip instead of the usual two queries.
func (s *Store) ListLeads(ctx context.Context, q model.LeadQuery) ([]model.Lead, int, error) {
	// Conditions are collected as strings and their values as args, so the
	// placeholder numbers ($1, $2, ...) always line up with the slice. User
	// input only ever travels as a bind parameter, never as SQL text.
	var (
		conditions []string
		args       []any
	)

	if q.Search != "" {
		args = append(args, containsPattern(q.Search))
		conditions = append(conditions, fmt.Sprintf(
			`(name ILIKE $%d ESCAPE '\' OR company ILIKE $%d ESCAPE '\')`,
			len(args), len(args)))
	}
	if q.Stage != "" {
		args = append(args, string(q.Stage))
		conditions = append(conditions, fmt.Sprintf(`stage = $%d::lead_stage`, len(args)))
	}
	if q.Source != "" {
		args = append(args, string(q.Source))
		conditions = append(conditions, fmt.Sprintf(`source = $%d::lead_source`, len(args)))
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	args = append(args, q.PageSize, q.Offset())
	limitPlaceholder, offsetPlaceholder := len(args)-1, len(args)

	sql := fmt.Sprintf(`
		SELECT %s, COUNT(*) OVER() AS total_items
		FROM leads
		%s
		ORDER BY %s
		LIMIT $%d OFFSET $%d`,
		leadColumns, where, orderClause(q), limitPlaceholder, offsetPlaceholder)

	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list leads: %w", err)
	}
	// Rows hold a database connection until closed. defer guarantees the
	// connection returns to the pool even if we bail out mid-loop.
	defer rows.Close()

	// Non-nil empty slice so an empty result marshals to [] rather than null.
	leads := make([]model.Lead, 0, q.PageSize)
	total := 0

	for rows.Next() {
		lead, err := scanLead(rows, &total)
		if err != nil {
			return nil, 0, fmt.Errorf("scan lead: %w", err)
		}
		leads = append(leads, lead)
	}
	// rows.Err() reports failures that happened during iteration -- a dropped
	// connection halfway through, for example. Skipping this check is a classic
	// way to silently return a partial result set as if it were complete.
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate leads: %w", err)
	}

	return leads, total, nil
}

// orderClause builds the ORDER BY for the list query.
//
// The column name cannot be a bind parameter, so it is interpolated -- which is
// safe only because it is checked against model.SortableLeadFields first. The
// direction is likewise reduced to one of two literal strings.
//
// id is appended as a tiebreaker: without it, rows with equal sort keys (two
// leads created in the same millisecond, or every lead in the same stage) could
// come back in a different order on each query and pagination would drop or
// duplicate rows between pages.
func orderClause(q model.LeadQuery) string {
	column := "created_at"
	if model.SortableLeadFields[q.SortBy] {
		column = q.SortBy
	}

	direction := "DESC"
	if strings.EqualFold(q.SortDir, "asc") {
		direction = "ASC"
	}

	return fmt.Sprintf("%s %s, id %s", column, direction, direction)
}

// ---------------------------------------------------------------------------
// Get one
// ---------------------------------------------------------------------------

// GetLead returns a single lead with its full activity timeline attached.
//
// It returns ErrNotFound if no lead has that id.
func (s *Store) GetLead(ctx context.Context, id int64) (model.Lead, error) {
	sql := fmt.Sprintf(`SELECT %s FROM leads WHERE id = $1`, leadColumns)

	lead, err := scanLead(s.pool.QueryRow(ctx, sql, id))
	if err != nil {
		// pgx returns this specific error when a QueryRow matched nothing.
		// Translating it here means handlers never import pgx.
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Lead{}, ErrNotFound
		}
		return model.Lead{}, fmt.Errorf("get lead %d: %w", id, err)
	}

	activities, err := s.ListActivities(ctx, id)
	if err != nil {
		return model.Lead{}, err
	}
	lead.Activities = activities

	return lead, nil
}

// ---------------------------------------------------------------------------
// Create
// ---------------------------------------------------------------------------

// CreateLead inserts a lead and its opening timeline entry.
//
// Both writes happen in one transaction: a lead whose timeline does not start
// with a "created" event would be a half-finished record, so either both rows
// land or neither does.
func (s *Store) CreateLead(ctx context.Context, in model.LeadInput) (model.Lead, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.Lead{}, fmt.Errorf("begin tx: %w", err)
	}
	// Rollback on every exit path. It is a no-op once Commit has succeeded, so
	// this single deferred call covers every error return below -- no need to
	// remember a rollback at each one.
	defer tx.Rollback(ctx)

	insert := fmt.Sprintf(`
		INSERT INTO leads (name, company, email, phone, source, stage, notes)
		VALUES ($1, $2, $3, $4, $5::lead_source, $6::lead_stage, $7)
		RETURNING %s`, leadColumns)

	lead, err := scanLead(tx.QueryRow(ctx, insert,
		in.Name, in.Company, in.Email, in.PhonePtr(),
		string(in.Source), string(in.Stage), in.Notes))
	if err != nil {
		if isUniqueViolation(err, "leads_email_lower_key") {
			return model.Lead{}, ErrDuplicateEmail
		}
		return model.Lead{}, fmt.Errorf("insert lead: %w", err)
	}

	note := fmt.Sprintf("Lead created from %s.", in.Source.Label())
	if err := insertActivityTx(ctx, tx, lead.ID, model.ActivityCreated, note); err != nil {
		return model.Lead{}, err
	}

	// A lead created directly into a later stage should say so on its timeline.
	if lead.Stage != model.StageNew {
		note := fmt.Sprintf("Stage set to %s.", lead.Stage.Label())
		if err := insertActivityTx(ctx, tx, lead.ID, model.ActivityStageChange, note); err != nil {
			return model.Lead{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return model.Lead{}, fmt.Errorf("commit create lead: %w", err)
	}

	return lead, nil
}

// ---------------------------------------------------------------------------
// Update
// ---------------------------------------------------------------------------

// UpdateLead replaces the editable fields of a lead.
//
// If the stage changed as part of the update, a stage_change activity is
// appended so the timeline stays a complete history. Detecting that needs the
// old value, which the CTE below captures: "prev" is evaluated against the
// pre-update snapshot, so prev.stage is the stage as it was.
//
// Returns ErrNotFound if the id does not exist, ErrDuplicateEmail on an email
// collision with another lead.
func (s *Store) UpdateLead(ctx context.Context, id int64, in model.LeadInput) (model.Lead, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.Lead{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	update := fmt.Sprintf(`
		WITH prev AS (
			SELECT id, stage FROM leads WHERE id = $1
		)
		UPDATE leads l
		SET name    = $2,
		    company = $3,
		    email   = $4,
		    phone   = $5,
		    source  = $6::lead_source,
		    stage   = $7::lead_stage,
		    notes   = $8
		FROM prev
		WHERE l.id = prev.id
		RETURNING %s, prev.stage::text AS previous_stage`, leadColumnsQualified)

	var previousStage string
	lead, err := scanLead(tx.QueryRow(ctx, update,
		id, in.Name, in.Company, in.Email, in.PhonePtr(),
		string(in.Source), string(in.Stage), in.Notes), &previousStage)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Lead{}, ErrNotFound
		}
		if isUniqueViolation(err, "leads_email_lower_key") {
			return model.Lead{}, ErrDuplicateEmail
		}
		return model.Lead{}, fmt.Errorf("update lead %d: %w", id, err)
	}

	if old := model.Stage(previousStage); old != lead.Stage {
		if err := insertStageChangeTx(ctx, tx, lead.ID, old, lead.Stage); err != nil {
			return model.Lead{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return model.Lead{}, fmt.Errorf("commit update lead: %w", err)
	}

	// The timeline may have just gained an entry, so re-read it for the response.
	activities, err := s.ListActivities(ctx, lead.ID)
	if err != nil {
		return model.Lead{}, err
	}
	lead.Activities = activities

	return lead, nil
}

// UpdateLeadStage moves a lead to a new stage and records the move.
//
// This backs the inline stage dropdown in the leads list. Any stage may follow
// any other -- real sales work goes backwards ("they went quiet, back to
// Contacted") and reopens closed deals, so the lifecycle is presented as the
// happy path in the UI rather than enforced as a state machine.
func (s *Store) UpdateLeadStage(ctx context.Context, id int64, stage model.Stage) (model.Lead, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.Lead{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	update := fmt.Sprintf(`
		WITH prev AS (
			SELECT id, stage FROM leads WHERE id = $1
		)
		UPDATE leads l
		SET stage = $2::lead_stage
		FROM prev
		WHERE l.id = prev.id
		RETURNING %s, prev.stage::text AS previous_stage`, leadColumnsQualified)

	var previousStage string
	lead, err := scanLead(tx.QueryRow(ctx, update, id, string(stage)), &previousStage)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Lead{}, ErrNotFound
		}
		return model.Lead{}, fmt.Errorf("update stage of lead %d: %w", id, err)
	}

	if old := model.Stage(previousStage); old != lead.Stage {
		if err := insertStageChangeTx(ctx, tx, lead.ID, old, lead.Stage); err != nil {
			return model.Lead{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return model.Lead{}, fmt.Errorf("commit stage change: %w", err)
	}

	return lead, nil
}

// ---------------------------------------------------------------------------
// Delete
// ---------------------------------------------------------------------------

// DeleteLead removes a lead. Its activities go with it, via the ON DELETE
// CASCADE on the foreign key.
//
// Returns ErrNotFound when nothing was deleted, so DELETE on a missing id is a
// 404 rather than a misleading 204.
func (s *Store) DeleteLead(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM leads WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete lead %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
