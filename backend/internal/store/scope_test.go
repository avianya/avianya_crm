package store

import (
	"context"
	"strings"
	"testing"

	"crm-lead-management/backend/internal/model"
)

func TestScopeSQL(t *testing.T) {
	sales := WithViewer(context.Background(), model.Viewer{MemberID: 7, Role: model.RoleSales})
	manager := WithViewer(context.Background(), model.Viewer{MemberID: 8, Role: model.RoleManager})
	system := context.Background()

	// A sales viewer's read gets the scoping CTE, with their id as a new arg.
	sql, args := scopeSQL(sales, `SELECT COUNT(*) FROM leads WHERE stage = $1`, []any{"new"})
	if !strings.HasPrefix(sql, "WITH leads AS (SELECT * FROM leads WHERE owner_id = $2) SELECT") || len(args) != 2 || args[1] != int64(7) {
		t.Errorf("select: %q %v", sql, args)
	}

	// A query that already has a WITH gets the CTE added to its list.
	sql, _ = scopeSQL(sales, "WITH x AS (SELECT 1) SELECT * FROM leads, x", nil)
	if !strings.HasPrefix(sql, "WITH leads AS (SELECT * FROM leads WHERE owner_id = $1), x AS") {
		t.Errorf("with: %q", sql)
	}

	// Writes are never rewritten, even ones that start WITH.
	for _, w := range []string{
		"UPDATE leads SET stage = 'new' WHERE id = $1",
		"WITH prev AS (SELECT 1) UPDATE leads l SET stage = 'won'",
		"INSERT INTO leads (name) VALUES ($1)",
		"DELETE FROM leads WHERE id = $1",
	} {
		if got, _ := scopeSQL(sales, w, nil); got != w {
			t.Errorf("write rewritten: %q", got)
		}
	}

	// Managers, admins and the server itself see everything.
	for _, ctx := range []context.Context{manager, system} {
		if got, _ := scopeSQL(ctx, "SELECT * FROM leads", nil); got != "SELECT * FROM leads" {
			t.Errorf("unscoped viewer rewritten: %q", got)
		}
	}
}

func TestScopeSQLAgainstTheDatabase(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	m, _, err := st.UpsertAccount(ctx, "Scope Test", "scope-test-"+t.Name()+"@example.test", model.RoleSales, "x")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = st.pool.Exec(ctx, `DELETE FROM team_members WHERE id = $1`, m.ID) })

	in := uniqueInput(t, "scope-mine")
	in.OwnerID = &m.ID
	mine := createTestLead(t, st, in)
	theirs := createTestLead(t, st, uniqueInput(t, "scope-theirs"))

	as := WithViewer(ctx, model.Viewer{MemberID: m.ID, Role: model.RoleSales})
	if ok, _ := st.LeadVisible(as, mine.ID); !ok {
		t.Error("own lead not visible")
	}
	if ok, _ := st.LeadVisible(as, theirs.ID); ok {
		t.Error("someone else's lead visible")
	}
	if _, err := st.GetLead(as, theirs.ID); err != ErrNotFound {
		t.Errorf("GetLead of someone else's lead: %v, want ErrNotFound", err)
	}
	leads, total, err := st.ListLeads(as, model.LeadQuery{Search: "Store Test scope", Page: 1, PageSize: 50, SortBy: "created_at", SortDir: "desc"})
	if err != nil || total != 1 || len(leads) != 1 || leads[0].ID != mine.ID {
		t.Errorf("list = %d leads, total %d, %v", len(leads), total, err)
	}
	// No viewer: the server sees both.
	if ok, _ := st.LeadVisible(ctx, theirs.ID); !ok {
		t.Error("the server itself cannot see a lead")
	}
}
