package store

import (
	"context"
	"encoding/json"
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
	notes, deal_value, created_at, updated_at, owner_id,
	close_reason, close_note,
	quality, quality_score, quality_breakdown, quality_override,
	quality_override_reason, quality_override_at, qualification_reason,
	quality_reason, quality_updated_at, spam_confirmed, meta_share_opt_out`

// Same list, qualified with the "l" table alias, for queries that join.
const leadColumnsQualified = `
	l.id, l.name, l.company, l.email, l.phone,
	l.source::text, l.stage::text,
	l.notes, l.deal_value, l.created_at, l.updated_at, l.owner_id,
	l.close_reason, l.close_note,
	l.quality, l.quality_score, l.quality_breakdown, l.quality_override,
	l.quality_override_reason, l.quality_override_at, l.qualification_reason,
	l.quality_reason, l.quality_updated_at, l.spam_confirmed, l.meta_share_opt_out`

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
		lead             model.Lead
		source, stage    string // scanned as text, then converted to our typed enums
		quality          string
		qualityBreakdown []byte // JSONB; NULL until the quality rules have run
	)

	dest := []any{
		&lead.ID, &lead.Name, &lead.Company, &lead.Email,
		&lead.Phone, // *string: a SQL NULL phone becomes a nil pointer
		&source, &stage,
		&lead.Notes,
		&lead.DealValue, // *int64: a SQL NULL deal value becomes a nil pointer
		&lead.CreatedAt, &lead.UpdatedAt,
		&lead.OwnerID,                      // *int64: NULL when nobody owns the lead
		&lead.CloseReason, &lead.CloseNote, // NULL unless Won or Lost with a reason
		&quality, &lead.QualityScore, &qualityBreakdown, &lead.QualityOverride,
		&lead.QualityOverrideReason, &lead.QualityOverrideAt, &lead.QualificationReason,
		&lead.QualityReason, &lead.QualityUpdatedAt, &lead.SpamConfirmed, &lead.MetaShareOptOut,
	}
	dest = append(dest, extra...)

	if err := row.Scan(dest...); err != nil {
		return model.Lead{}, err
	}

	lead.Source = model.Source(source)
	lead.Stage = model.Stage(stage)
	lead.Quality = model.Quality(quality)
	lead.QualificationStatus = model.QualificationFor(lead.Stage)
	lead.QualityBreakdown = []model.QualityHit{}
	if len(qualityBreakdown) > 0 {
		if err := json.Unmarshal(qualityBreakdown, &lead.QualityBreakdown); err != nil {
			return model.Lead{}, fmt.Errorf("decode quality breakdown of lead %d: %w", lead.ID, err)
		}
	}
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
	// Fixed SQL, no user input: q.Status was checked against model.LeadStatuses.
	switch q.Status {
	case "open":
		conditions = append(conditions, `stage NOT IN ('won', 'lost')`)
	case "closed":
		conditions = append(conditions, `stage IN ('won', 'lost')`)
	}

	if q.Quality != "" {
		args = append(args, string(q.Quality))
		conditions = append(conditions, fmt.Sprintf(`quality = $%d`, len(args)))
	}
	if q.OwnerID != nil {
		args = append(args, *q.OwnerID)
		conditions = append(conditions, fmt.Sprintf(`owner_id = $%d`, len(args)))
	}
	if q.Unowned {
		conditions = append(conditions, `owner_id IS NULL`)
	}
	if len(q.IDs) > 0 {
		args = append(args, q.IDs)
		conditions = append(conditions, fmt.Sprintf(`id = ANY($%d)`, len(args)))
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	if q.NeedsScores() {
		return s.listLeadsByScore(ctx, q, where, args)
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

	rows, err := s.query(ctx, sql, args...)
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

	// COUNT(*) OVER() only arrives on returned rows, so a page past the end
	// (the last lead on it was just deleted) would report 0 leads in total.
	// Count separately then, so the client can step back to the real last page.
	if len(leads) == 0 && q.Page > 1 {
		countSQL := fmt.Sprintf(`SELECT COUNT(*) FROM leads %s`, where)
		if err := s.queryRow(ctx, countSQL, args[:len(args)-2]...).Scan(&total); err != nil {
			return nil, 0, fmt.Errorf("count leads: %w", err)
		}
	}

	// Health and score for the whole page in one extra query.
	if err := s.attachInsights(ctx, leads); err != nil {
		return nil, 0, err
	}

	return leads, total, nil
}

// listLeadsByScore is ListLeads for a query that sorts or filters by score.
// The score is not a column, so the database cannot sort or filter on it, nor
// page by it: this loads every lead matching the other filters (up to
// model.MaxScoredLeads), scores them all, and picks the page in Go.
//
// Keeping one scoring formula, in Go, is worth the extra work: a copy in SQL
// would sooner or later disagree with the score the lead page shows.
func (s *Store) listLeadsByScore(ctx context.Context, q model.LeadQuery, where string, args []any) ([]model.Lead, int, error) {
	// A band filter keeps the requested column order; a score sort replaces
	// it, so the base order there is only for a stable, predictable load.
	order := "created_at DESC, id DESC"
	if q.SortBy != model.SortByScore {
		order = orderClause(q)
	}

	leads, err := s.loadScoredLeads(ctx, where, args, order)
	if err != nil {
		return nil, 0, err
	}

	if q.Band != "" {
		leads = model.FilterByBand(leads, q.Band)
	}
	if q.SortBy == model.SortByScore {
		model.SortLeadsByScore(leads, q.SortDir)
	}

	total := len(leads)
	start := min(q.Offset(), total)
	end := min(start+q.PageSize, total)

	// A fresh slice, so the page does not keep the whole list alive, and so
	// an empty page is [] rather than null.
	page := make([]model.Lead, end-start)
	copy(page, leads[start:end])
	return page, total, nil
}

// loadScoredLeads loads every lead matching where (up to model.MaxScoredLeads),
// in the given order, with health and score attached.
func (s *Store) loadScoredLeads(ctx context.Context, where string, args []any, order string) ([]model.Lead, error) {
	args = append(args, model.MaxScoredLeads)
	sql := fmt.Sprintf(`SELECT %s FROM leads %s ORDER BY %s LIMIT $%d`,
		leadColumns, where, order, len(args))

	rows, err := s.query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("load scored leads: %w", err)
	}
	leads, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.Lead, error) {
		return scanLead(row)
	})
	if err != nil {
		return nil, fmt.Errorf("scan scored leads: %w", err)
	}

	if err := s.attachInsights(ctx, leads); err != nil {
		return nil, err
	}
	return leads, nil
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

	// Postgres puts NULLs first in a DESC sort, which would bury the biggest
	// deals under every lead nobody has valued yet. Unvalued goes last either way.
	nulls := ""
	if column == "deal_value" {
		nulls = " NULLS LAST"
	}

	return fmt.Sprintf("%s %s%s, id %s", column, direction, nulls, direction)
}

// ---------------------------------------------------------------------------
// Get one
// ---------------------------------------------------------------------------

// GetLead returns a single lead with its full activity timeline attached.
//
// It returns ErrNotFound if no lead has that id.
func (s *Store) GetLead(ctx context.Context, id int64) (model.Lead, error) {
	sql := fmt.Sprintf(`SELECT %s FROM leads WHERE id = $1`, leadColumns)

	lead, err := scanLead(s.queryRow(ctx, sql, id))
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

	if lead.StageHistory, err = s.ListStageHistory(ctx, id); err != nil {
		return model.Lead{}, err
	}

	if lead.MetaAttribution, err = s.metaAttribution(ctx, id); err != nil {
		return model.Lead{}, err
	}
	if lead.MetaAttribution != nil {
		if lead.CAPIEvents, err = s.ListCAPIEvents(ctx, model.CAPIEventQuery{LeadID: id, Limit: 50}); err != nil {
			return model.Lead{}, err
		}
	}

	if err := s.attachInsightsOne(ctx, &lead); err != nil {
		return model.Lead{}, err
	}

	return lead, nil
}

// ---------------------------------------------------------------------------
// Create
// ---------------------------------------------------------------------------

// CreateLead inserts a lead and its opening timeline entry.
//
// Both writes happen in one transaction: a lead whose timeline does not start
// with a "created" event would be a half-finished record, so either both rows
// land or neither does. The automation rules run in it too.
func (s *Store) CreateLead(ctx context.Context, in model.LeadInput) (model.Lead, error) {
	return s.createLead(ctx, in, true, nil)
}

// ImportLead is CreateLead for a lead read from an import file, where running
// the automation rules is the importer's choice: importing an old list should
// not plan a task for every lead in it.
func (s *Store) ImportLead(ctx context.Context, in model.LeadInput, runAutomations bool) (model.Lead, error) {
	return s.createLead(ctx, in, runAutomations, nil)
}

// leadHook writes more about a new lead in its own transaction, and may fill
// in what it wrote on the lead returned. Ad leads use it for their
// attribution and quality score, so a lead never exists without them.
type leadHook func(ctx context.Context, tx pgx.Tx, lead *model.Lead) error

func (s *Store) createLead(ctx context.Context, in model.LeadInput, runAutomations bool, after leadHook) (model.Lead, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.Lead{}, fmt.Errorf("begin tx: %w", err)
	}
	// Rollback on every exit path. It is a no-op once Commit has succeeded, so
	// this single deferred call covers every error return below -- no need to
	// remember a rollback at each one.
	defer tx.Rollback(ctx)

	lead, err := createLeadTx(ctx, tx, in, runAutomations, after)
	if err != nil {
		return model.Lead{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return model.Lead{}, fmt.Errorf("commit create lead: %w", err)
	}

	if err := s.attachInsightsOne(ctx, &lead); err != nil {
		return model.Lead{}, err
	}

	return lead, nil
}

// createLeadTx is createLead inside a transaction the caller owns, for when
// the new lead is one step of something larger (taking in an ad lead).
func createLeadTx(ctx context.Context, tx pgx.Tx, in model.LeadInput, runAutomations bool, after leadHook) (model.Lead, error) {
	// A lead created straight into Lost is entering it, so it needs a reason
	// like any other loss.
	if err := requireLostReason(in.Stage, true, in.CloseInput.Reason); err != nil {
		return model.Lead{}, err
	}

	insert := fmt.Sprintf(`
		INSERT INTO leads (name, company, email, phone, source, stage, notes, deal_value, owner_id,
		                   close_reason, close_note)
		VALUES ($1, $2, $3, $4, $5::lead_source, $6::lead_stage, $7, $8, $9, $10, $11)
		RETURNING %s`, leadColumns)

	lead, err := scanLead(tx.QueryRow(ctx, insert,
		in.Name, in.Company, in.Email, in.PhonePtr(),
		string(in.Source), string(in.Stage), in.Notes, in.DealValue, in.OwnerID,
		in.CloseInput.Reason, in.CloseInput.NotePtr()))
	if err != nil {
		if isUniqueViolation(err, "leads_email_lower_key") {
			return model.Lead{}, ErrDuplicateEmail
		}
		if isForeignKeyViolationOn(err, "leads_owner_id_fkey") {
			return model.Lead{}, unknownMember("owner_id")
		}
		return model.Lead{}, fmt.Errorf("insert lead: %w", err)
	}

	note := fmt.Sprintf("Lead created from %s.", in.Source.Label())
	if err := insertActivityTx(ctx, tx, lead.ID, model.ActivityCreated, note); err != nil {
		return model.Lead{}, err
	}

	if err := insertStageHistoryTx(ctx, tx, lead.ID, nil, lead.Stage); err != nil {
		return model.Lead{}, err
	}

	// A lead created directly into a later stage should say so on its timeline.
	if lead.Stage != model.StageNew {
		note := fmt.Sprintf("Stage set to %s.", lead.Stage.Label()) +
			reasonSuffix(lead.Stage, lead.CloseReason, lead.CloseNote)
		if err := insertActivityTx(ctx, tx, lead.ID, model.ActivityStageChange, note); err != nil {
			return model.Lead{}, err
		}
	}

	// Before the automation rules, so the tasks they plan for "the lead's
	// owner" go to whoever the rotation picked. An import only joins the
	// rotation when the importer chose to run automations.
	if runAutomations {
		if err := autoAssignTx(ctx, tx, &lead); err != nil {
			return model.Lead{}, err
		}
	}

	if after != nil {
		if err := after(ctx, tx, &lead); err != nil {
			return model.Lead{}, err
		}
	}

	// In the same transaction, so a lead never exists without the tasks its
	// automation rules promised.
	if runAutomations {
		if err := runAutomationsTx(ctx, tx, model.TriggerLeadCreated, lead); err != nil {
			return model.Lead{}, err
		}
	}

	return lead, nil
}

// ExistingLeadEmails returns which of emails (lower-cased) already belong to a
// lead, as a set.
func (s *Store) ExistingLeadEmails(ctx context.Context, emails []string) (map[string]bool, error) {
	rows, err := s.query(ctx,
		`SELECT lower(email) FROM leads WHERE lower(email) = ANY($1)`, emails)
	if err != nil {
		return nil, fmt.Errorf("existing lead emails: %w", err)
	}
	found, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("scan existing lead emails: %w", err)
	}
	set := make(map[string]bool, len(found))
	for _, email := range found {
		set[email] = true
	}
	return set, nil
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
			SELECT id, stage, owner_id, close_reason, close_note FROM leads WHERE id = $1 FOR UPDATE
		)
		UPDATE leads l
		SET name    = $2,
		    company = $3,
		    email   = $4,
		    phone   = $5,
		    source  = $6::lead_source,
		    stage   = $7::lead_stage,
		    notes   = $8,
		    deal_value = $9,
		    owner_id = $10,
		    %s
		FROM prev
		WHERE l.id = prev.id
		RETURNING %s, prev.stage::text AS previous_stage, prev.owner_id AS previous_owner,
		          prev.close_reason, prev.close_note`, closeReasonSet(7, 11, 12, 13), leadColumnsQualified)

	var (
		previousStage      string
		previousOwner      *int64
		previousReason     *model.CloseReason
		previousReasonNote *string
	)
	lead, err := scanLead(tx.QueryRow(ctx, update,
		id, in.Name, in.Company, in.Email, in.PhonePtr(),
		string(in.Source), string(in.Stage), in.Notes, in.DealValue, in.OwnerID,
		in.CloseInput.Reason, in.CloseInput.NotePtr(), in.CloseInput.Given()),
		&previousStage, &previousOwner, &previousReason, &previousReasonNote)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Lead{}, ErrNotFound
		}
		if isUniqueViolation(err, "leads_email_lower_key") {
			return model.Lead{}, ErrDuplicateEmail
		}
		if isForeignKeyViolationOn(err, "leads_owner_id_fkey") {
			return model.Lead{}, unknownMember("owner_id")
		}
		return model.Lead{}, fmt.Errorf("update lead %d: %w", id, err)
	}

	old := model.Stage(previousStage)
	if err := requireLostReason(lead.Stage, old != lead.Stage, lead.CloseReason); err != nil {
		return model.Lead{}, err // rolls the whole edit back
	}
	if err := recordStageOrReasonTx(ctx, tx, lead, old, previousReason, previousReasonNote); err != nil {
		return model.Lead{}, err
	}

	if !sameOwner(previousOwner, lead.OwnerID) {
		if err := insertOwnerChangeTx(ctx, tx, lead.ID, previousOwner, lead.OwnerID); err != nil {
			return model.Lead{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return model.Lead{}, fmt.Errorf("commit update lead: %w", err)
	}

	// The timeline and stage history may have just gained an entry, so re-read
	// them for the response.
	activities, err := s.ListActivities(ctx, lead.ID)
	if err != nil {
		return model.Lead{}, err
	}
	lead.Activities = activities

	if lead.StageHistory, err = s.ListStageHistory(ctx, lead.ID); err != nil {
		return model.Lead{}, err
	}

	if err := s.attachInsightsOne(ctx, &lead); err != nil {
		return model.Lead{}, err
	}

	return lead, nil
}

// UpdateLeadStage moves a lead to a new stage and records the move.
//
// This backs the inline stage dropdown in the leads list. Any stage may follow
// any other -- real sales work goes backwards ("they went quiet, back to
// Contacted") and reopens closed deals, so the lifecycle is presented as the
// happy path in the UI rather than enforced as a state machine.
//
// Moving into Lost needs a close reason (a FieldError on close_reason without
// one). Sending a reason with the stage the lead is already in changes just
// the reason, which is logged on the timeline too.
func (s *Store) UpdateLeadStage(ctx context.Context, id int64, in model.StageInput) (model.Lead, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.Lead{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	update := fmt.Sprintf(`
		WITH prev AS (
			SELECT id, stage, close_reason, close_note FROM leads WHERE id = $1 FOR UPDATE
		)
		UPDATE leads l
		SET stage = $2::lead_stage,
		    %s
		FROM prev
		WHERE l.id = prev.id
		RETURNING %s, prev.stage::text AS previous_stage, prev.close_reason, prev.close_note`,
		closeReasonSet(2, 3, 4, 5), leadColumnsQualified)

	var (
		previousStage      string
		previousReason     *model.CloseReason
		previousReasonNote *string
	)
	lead, err := scanLead(tx.QueryRow(ctx, update,
		id, string(in.Stage), in.CloseInput.Reason, in.CloseInput.NotePtr(), in.CloseInput.Given()),
		&previousStage, &previousReason, &previousReasonNote)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Lead{}, ErrNotFound
		}
		return model.Lead{}, fmt.Errorf("update stage of lead %d: %w", id, err)
	}

	old := model.Stage(previousStage)
	if err := requireLostReason(lead.Stage, old != lead.Stage, lead.CloseReason); err != nil {
		return model.Lead{}, err
	}
	if err := recordStageOrReasonTx(ctx, tx, lead, old, previousReason, previousReasonNote); err != nil {
		return model.Lead{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return model.Lead{}, fmt.Errorf("commit stage change: %w", err)
	}

	// The stage drives both health thresholds and score, so recompute them.
	if err := s.attachInsightsOne(ctx, &lead); err != nil {
		return model.Lead{}, err
	}

	return lead, nil
}

// UpdateLeadSource changes where a lead came from, and nothing else. Like a
// source change in the full edit, it is not a timeline event. ErrNotFound for
// a missing lead.
func (s *Store) UpdateLeadSource(ctx context.Context, id int64, source model.Source) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE leads
		SET source = $2::lead_source
		WHERE id = $1`, id, string(source))
	if err != nil {
		return fmt.Errorf("update source of lead %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateLeadOwner gives a lead to another team member, or to nobody when
// ownerID is nil, and logs the handover on the timeline in the same
// transaction. Setting the owner it already has changes nothing.
//
// Returns the lead with its timeline, ErrNotFound for a missing lead, or a
// FieldError on owner_id for a member that does not exist.
func (s *Store) UpdateLeadOwner(ctx context.Context, id int64, ownerID *int64) (model.Lead, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.Lead{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var previousOwner, newOwner *int64
	err = tx.QueryRow(ctx, `
		WITH prev AS (
			SELECT id, owner_id FROM leads WHERE id = $1 FOR UPDATE
		)
		UPDATE leads l
		SET owner_id = $2
		FROM prev
		WHERE l.id = prev.id
		RETURNING prev.owner_id, l.owner_id`, id, ownerID).Scan(&previousOwner, &newOwner)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Lead{}, ErrNotFound
		}
		if isForeignKeyViolationOn(err, "leads_owner_id_fkey") {
			return model.Lead{}, unknownMember("owner_id")
		}
		return model.Lead{}, fmt.Errorf("update owner of lead %d: %w", id, err)
	}

	if !sameOwner(previousOwner, newOwner) {
		if err := insertOwnerChangeTx(ctx, tx, id, previousOwner, newOwner); err != nil {
			return model.Lead{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return model.Lead{}, fmt.Errorf("commit owner change: %w", err)
	}

	return s.GetLead(ctx, id)
}

// UpdateLeadNotes replaces a lead's notes and nothing else, so editing them
// from the lead page cannot overwrite a change someone else just made to
// another field. Returns the lead with its timeline, or ErrNotFound.
func (s *Store) UpdateLeadNotes(ctx context.Context, id int64, notes string) (model.Lead, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE leads SET notes = $2 WHERE id = $1`, id, notes)
	if err != nil {
		return model.Lead{}, fmt.Errorf("update notes of lead %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return model.Lead{}, ErrNotFound
	}
	return s.GetLead(ctx, id)
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
