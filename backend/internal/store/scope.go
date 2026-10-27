package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"

	"crm-lead-management/backend/internal/model"
)

// Lead visibility. A request carries its viewer in the context (WithViewer,
// set by the API's login middleware). For a viewer who may only see their own
// leads, every read goes through scopeSQL, which puts
//
//	WITH leads AS (SELECT * FROM leads WHERE owner_id = $n)
//
// in front of the query. Inside that statement the name "leads" then means the
// viewer's leads only -- in the FROM, in every JOIN, in every subquery -- so
// the dashboard, reports, search, exports and the rest are scoped by one rule
// rather than by a WHERE clause remembered in a hundred places. (A plain
// WITH's own name is not visible inside its definition, so the inner "leads"
// is the real table.)
//
// No viewer in the context means the server itself is asking (the reminder
// job, the CAPI sender) and sees everything. Writes are not rewritten: the API
// checks a lead is visible before changing it.

type viewerKey struct{}

// WithViewer returns ctx carrying who is asking.
func WithViewer(ctx context.Context, v model.Viewer) context.Context {
	return context.WithValue(ctx, viewerKey{}, v)
}

// ViewerFrom returns the viewer in ctx, if any.
func ViewerFrom(ctx context.Context) (model.Viewer, bool) {
	v, ok := ctx.Value(viewerKey{}).(model.Viewer)
	return v, ok
}

// writes spots a statement that changes data; those are never rewritten.
var writes = regexp.MustCompile(`(?is)\b(insert\s+into|update\s+\w+(\s+\w+)?\s+set|delete\s+from)\b`)

// scopeSQL rewrites a read so "leads" means only the viewer's leads.
func scopeSQL(ctx context.Context, sql string, args []any) (string, []any) {
	v, ok := ViewerFrom(ctx)
	if !ok || v.SeesAllLeads() {
		return sql, args
	}
	trimmed := strings.TrimSpace(sql)
	lower := strings.ToLower(trimmed)
	if writes.MatchString(trimmed) || !(strings.HasPrefix(lower, "select") || strings.HasPrefix(lower, "with")) {
		return sql, args
	}
	args = append(append([]any{}, args...), v.MemberID)
	cte := fmt.Sprintf("leads AS (SELECT * FROM leads WHERE owner_id = $%d)", len(args))
	if strings.HasPrefix(lower, "with") {
		return "WITH " + cte + ", " + strings.TrimSpace(trimmed[len("with"):]), args
	}
	return "WITH " + cte + " " + trimmed, args
}

// query is pool.Query with the viewer's lead scope applied.
func (s *Store) query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	sql, args = scopeSQL(ctx, sql, args)
	return s.pool.Query(ctx, sql, args...)
}

// queryRow is pool.QueryRow with the viewer's lead scope applied.
func (s *Store) queryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	sql, args = scopeSQL(ctx, sql, args)
	return s.pool.QueryRow(ctx, sql, args...)
}

// LeadVisible reports whether the viewer in ctx may see the lead. A lead that
// does not exist is not visible either, so callers answer 404 for both and
// never reveal that a lead they may not see exists.
func (s *Store) LeadVisible(ctx context.Context, id int64) (bool, error) {
	var ok bool
	err := s.queryRow(ctx, `SELECT EXISTS (SELECT 1 FROM leads WHERE id = $1)`, id).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("lead visible: %w", err)
	}
	return ok, nil
}

// LeadOfTask returns the lead a task belongs to, or ErrNotFound.
func (s *Store) LeadOfTask(ctx context.Context, taskID int64) (int64, error) {
	return s.leadOf(ctx, `SELECT lead_id FROM tasks WHERE id = $1`, taskID)
}

// LeadOfActivity returns the lead a timeline entry belongs to, or ErrNotFound.
func (s *Store) LeadOfActivity(ctx context.Context, activityID int64) (int64, error) {
	return s.leadOf(ctx, `SELECT lead_id FROM activities WHERE id = $1`, activityID)
}

// LeadOfNotification returns the lead a reminder is about, or ErrNotFound.
func (s *Store) LeadOfNotification(ctx context.Context, id int64) (int64, error) {
	return s.leadOf(ctx, `SELECT t.lead_id FROM notifications n JOIN tasks t ON t.id = n.task_id WHERE n.id = $1`, id)
}

// LeadOfDealAlert returns the lead a stuck-deal alert is about, or ErrNotFound.
func (s *Store) LeadOfDealAlert(ctx context.Context, id int64) (int64, error) {
	return s.leadOf(ctx, `SELECT lead_id FROM deal_alerts WHERE id = $1`, id)
}

func (s *Store) leadOf(ctx context.Context, sql string, id int64) (int64, error) {
	var leadID int64
	if err := s.pool.QueryRow(ctx, sql, id).Scan(&leadID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, fmt.Errorf("find lead: %w", err)
	}
	return leadID, nil
}
