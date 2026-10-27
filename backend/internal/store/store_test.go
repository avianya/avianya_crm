package store

import (
	"context"
	"errors"
	"os"
	"testing"

	"crm-lead-management/backend/internal/model"
)

// Integration tests for the storage layer. Unlike the model tests these need a
// real PostgreSQL database, because the behaviour worth verifying here *is* the
// database behaviour: the ON DELETE CASCADE, the unique index, the transaction
// that keeps a stage change and its timeline entry together.
//
// They are skipped unless TEST_DATABASE_URL is set, so `go test ./...` still
// passes on a machine with no database:
//
//	export TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5432/crm_leads_test?sslmode=disable'
//	psql "$TEST_DATABASE_URL" -f ../../../db/schema.sql
//	go test ./internal/store/ -v
//
// Point this at a THROWAWAY database. Each test cleans up the rows it created,
// but you do not want tests anywhere near data you care about.

// newTestStore connects to the test database, or skips the test if there isn't one.
func newTestStore(t *testing.T) *Store {
	t.Helper() // failures get reported at the caller's line, not inside this helper

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set - skipping database integration tests")
	}

	st, err := New(context.Background(), dsn, "")
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}

	// t.Cleanup runs when this test (and its subtests) finish, pass or fail.
	// Cleaner than a defer, because it works even from inside a helper.
	t.Cleanup(st.Close)

	return st
}

// createTestLead inserts a lead and registers its deletion as cleanup, so tests
// never leave rows behind for the next run to trip over.
func createTestLead(t *testing.T, st *Store, in model.LeadInput) model.Lead {
	t.Helper()

	in.Normalize()
	lead, err := st.CreateLead(context.Background(), in)
	if err != nil {
		t.Fatalf("CreateLead: %v", err)
	}

	t.Cleanup(func() {
		// Ignore ErrNotFound: some tests delete the lead themselves.
		_ = st.DeleteLead(context.Background(), lead.ID)
	})

	return lead
}

// uniqueInput builds a valid lead input with an email unlikely to collide with
// seed data or a previous run.
func uniqueInput(t *testing.T, suffix string) model.LeadInput {
	t.Helper()
	return model.LeadInput{
		Name:    "Store Test " + suffix,
		Company: "Store Test Co " + suffix,
		Email:   "store-test-" + suffix + "@example.test",
		Phone:   "+91 90000 00000",
		Source:  model.SourceReferral,
		Stage:   model.StageNew,
		Notes:   "created by store_test.go",
	}
}

func TestCreateLeadWritesOpeningTimelineEntry(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	lead := createTestLead(t, st, uniqueInput(t, "create"))

	if lead.ID == 0 {
		t.Fatal("expected a database-assigned id, got 0")
	}
	if lead.CreatedAt.IsZero() {
		t.Error("expected created_at to be set by the database default")
	}

	// The point of the transaction in CreateLead: a new lead always arrives with
	// its "created" entry already on the timeline.
	activities, err := st.ListActivities(ctx, lead.ID)
	if err != nil {
		t.Fatalf("ListActivities: %v", err)
	}
	if len(activities) != 1 {
		t.Fatalf("expected exactly 1 activity, got %d", len(activities))
	}
	if activities[0].Type != model.ActivityCreated {
		t.Errorf("activity type = %q, want %q", activities[0].Type, model.ActivityCreated)
	}
}

func TestCreateLeadRejectsDuplicateEmail(t *testing.T) {
	st := newTestStore(t)

	in := uniqueInput(t, "dupe")
	createTestLead(t, st, in)

	// Same email, different case: the unique index is on lower(email), so this
	// must still collide.
	second := in
	second.Name = "Someone Else"
	second.Email = "STORE-TEST-DUPE@EXAMPLE.TEST"
	second.Normalize()

	_, err := st.CreateLead(context.Background(), second)

	// errors.Is, not ==, because the error may be wrapped on its way up.
	if !errors.Is(err, ErrDuplicateEmail) {
		t.Fatalf("expected ErrDuplicateEmail, got %v", err)
	}
}

func TestUpdateLeadStageRecordsTheMove(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	lead := createTestLead(t, st, uniqueInput(t, "stage"))

	updated, err := st.UpdateLeadStage(ctx, lead.ID, model.StageInput{Stage: model.StageQualified})
	if err != nil {
		t.Fatalf("UpdateLeadStage: %v", err)
	}
	if updated.Stage != model.StageQualified {
		t.Errorf("stage = %q, want %q", updated.Stage, model.StageQualified)
	}

	// The trigger on the leads table should have advanced updated_at.
	if !updated.UpdatedAt.After(lead.UpdatedAt) {
		t.Errorf("updated_at did not advance: %v -> %v", lead.UpdatedAt, updated.UpdatedAt)
	}

	activities, err := st.ListActivities(ctx, lead.ID)
	if err != nil {
		t.Fatalf("ListActivities: %v", err)
	}
	// created + stage_change, newest first.
	if len(activities) != 2 {
		t.Fatalf("expected 2 activities, got %d", len(activities))
	}
	if activities[0].Type != model.ActivityStageChange {
		t.Errorf("newest activity type = %q, want %q", activities[0].Type, model.ActivityStageChange)
	}
	want := "Stage changed from New to Qualified."
	if activities[0].Note != want {
		t.Errorf("note = %q, want %q", activities[0].Note, want)
	}
}

func TestUpdateLeadStageToSameStageAddsNothing(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	lead := createTestLead(t, st, uniqueInput(t, "nochange"))

	if _, err := st.UpdateLeadStage(ctx, lead.ID, model.StageInput{Stage: model.StageNew}); err != nil {
		t.Fatalf("UpdateLeadStage: %v", err)
	}

	activities, err := st.ListActivities(ctx, lead.ID)
	if err != nil {
		t.Fatalf("ListActivities: %v", err)
	}
	// Still just the "created" entry: re-selecting the current stage in the UI
	// must not spam the timeline.
	if len(activities) != 1 {
		t.Fatalf("expected 1 activity after a no-op stage change, got %d", len(activities))
	}
}

func TestGetLeadNotFound(t *testing.T) {
	st := newTestStore(t)

	_, err := st.GetLead(context.Background(), 999_999_999)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestDeleteLeadCascadesActivities(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	lead := createTestLead(t, st, uniqueInput(t, "cascade"))

	if _, err := st.CreateActivity(ctx, lead.ID, model.ActivityInput{
		Type: model.ActivityCall,
		Note: "Called about the proposal.",
	}); err != nil {
		t.Fatalf("CreateActivity: %v", err)
	}

	if err := st.DeleteLead(ctx, lead.ID); err != nil {
		t.Fatalf("DeleteLead: %v", err)
	}

	// The foreign key's ON DELETE CASCADE should have taken the activities with
	// it -- no orphan timeline rows, and no cleanup code in Go to forget.
	// Counted directly: ListActivities now reports a deleted lead as not found.
	var remaining int
	if err := st.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM activities WHERE lead_id = $1`, lead.ID).Scan(&remaining); err != nil {
		t.Fatalf("count activities after delete: %v", err)
	}
	if remaining != 0 {
		t.Errorf("expected activities to cascade away, %d remain", remaining)
	}
	if _, err := st.ListActivities(ctx, lead.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("ListActivities after delete: got %v, want ErrNotFound", err)
	}

	// Deleting again is a 404, not a silent success.
	if err := st.DeleteLead(ctx, lead.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete: expected ErrNotFound, got %v", err)
	}
}

func TestCreateActivityForMissingLeadIsNotFound(t *testing.T) {
	st := newTestStore(t)

	_, err := st.CreateActivity(context.Background(), 999_999_999, model.ActivityInput{
		Type: model.ActivityNote,
		Note: "orphan",
	})

	// The foreign key rejects this; the store translates 23503 into ErrNotFound.
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestListLeadsFiltersAndPaginates(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	// A distinctive company name so the search assertions cannot be satisfied by
	// seed data that happens to be in the database.
	const marker = "Zzyzx Integration Ltd"

	for _, suffix := range []string{"list-a", "list-b", "list-c"} {
		in := uniqueInput(t, suffix)
		in.Company = marker
		in.Stage = model.StageQualified
		createTestLead(t, st, in)
	}

	t.Run("search matches company", func(t *testing.T) {
		leads, total, err := st.ListLeads(ctx, model.LeadQuery{
			Search: marker, SortBy: "created_at", SortDir: "desc",
			Page: 1, PageSize: 10,
		})
		if err != nil {
			t.Fatalf("ListLeads: %v", err)
		}
		if total != 3 {
			t.Errorf("total = %d, want 3", total)
		}
		if len(leads) != 3 {
			t.Errorf("returned %d leads, want 3", len(leads))
		}
	})

	t.Run("stage filter narrows results", func(t *testing.T) {
		leads, _, err := st.ListLeads(ctx, model.LeadQuery{
			Search: marker, Stage: model.StageWon,
			SortBy: "created_at", SortDir: "desc", Page: 1, PageSize: 10,
		})
		if err != nil {
			t.Fatalf("ListLeads: %v", err)
		}
		if len(leads) != 0 {
			t.Errorf("expected no 'won' leads for this marker, got %d", len(leads))
		}
	})

	t.Run("pagination reports the full total", func(t *testing.T) {
		leads, total, err := st.ListLeads(ctx, model.LeadQuery{
			Search: marker, SortBy: "created_at", SortDir: "desc",
			Page: 1, PageSize: 2,
		})
		if err != nil {
			t.Fatalf("ListLeads: %v", err)
		}
		if len(leads) != 2 {
			t.Errorf("returned %d leads, want a page of 2", len(leads))
		}
		// The COUNT(*) OVER() total must describe all matches, not just this page.
		if total != 3 {
			t.Errorf("total = %d, want 3", total)
		}
	})

	t.Run("search wildcards are escaped", func(t *testing.T) {
		// A bare "%" would be a match-everything wildcard if it were not escaped.
		_, total, err := st.ListLeads(ctx, model.LeadQuery{
			Search: "%", SortBy: "created_at", SortDir: "desc", Page: 1, PageSize: 10,
		})
		if err != nil {
			t.Fatalf("ListLeads: %v", err)
		}
		if total != 0 {
			t.Errorf("searching for a literal %% matched %d leads, want 0", total)
		}
	})

	// Last: it adds a fourth marker lead, which the totals above do not expect.
	t.Run("status filter groups stages", func(t *testing.T) {
		won := uniqueInput(t, "list-won")
		won.Company = marker
		won.Stage = model.StageWon
		createTestLead(t, st, won)

		for status, want := range map[string]int{"open": 3, "closed": 1, "": 4} {
			leads, total, err := st.ListLeads(ctx, model.LeadQuery{
				Search: marker, Status: status,
				SortBy: "created_at", SortDir: "desc", Page: 1, PageSize: 10,
			})
			if err != nil {
				t.Fatalf("ListLeads(status=%q): %v", status, err)
			}
			if total != want {
				t.Errorf("status=%q: total = %d, want %d", status, total, want)
			}
			for _, l := range leads {
				if (status == "open" && l.Stage.IsClosed()) || (status == "closed" && !l.Stage.IsClosed()) {
					t.Errorf("status=%q returned a %s lead", status, l.Stage)
				}
			}
		}
	})
}

func TestDashboardSummaryCoversEveryStage(t *testing.T) {
	st := newTestStore(t)

	summary, err := st.DashboardSummary(context.Background())
	if err != nil {
		t.Fatalf("DashboardSummary: %v", err)
	}

	// Every stage must be present, in lifecycle order, so the chart can render
	// empty bars without knowing the stage list itself.
	if len(summary.ByStage) != len(model.Stages) {
		t.Fatalf("ByStage has %d entries, want %d", len(summary.ByStage), len(model.Stages))
	}
	for i, want := range model.Stages {
		if summary.ByStage[i].Stage != want {
			t.Errorf("ByStage[%d] = %q, want %q", i, summary.ByStage[i].Stage, want)
		}
	}

	// The parts must add up to the whole.
	sum := 0
	for _, sc := range summary.ByStage {
		sum += sc.Count
	}
	if sum != summary.TotalLeads {
		t.Errorf("stage counts sum to %d but total_leads = %d", sum, summary.TotalLeads)
	}
	if summary.OpenCount+summary.WonCount+summary.LostCount != summary.TotalLeads {
		t.Errorf("open+won+lost = %d, want total_leads = %d",
			summary.OpenCount+summary.WonCount+summary.LostCount, summary.TotalLeads)
	}
	if summary.ConversionRate < 0 || summary.ConversionRate > 100 {
		t.Errorf("conversion_rate = %v, want 0..100", summary.ConversionRate)
	}
	if len(summary.RecentLeads) > 5 {
		t.Errorf("RecentLeads has %d entries, want at most 5", len(summary.RecentLeads))
	}
}
