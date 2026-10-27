package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"crm-lead-management/backend/internal/model"
)

// createTestMember adds a team member with a unique email and removes them
// when the test ends.
func createTestMember(t *testing.T, st *Store, name string) model.TeamMember {
	t.Helper()

	in := model.TeamMemberInput{
		Name:  name,
		Email: fmt.Sprintf("owner-test-%d@example.test", time.Now().UnixNano()),
	}
	in.Normalize()
	member, err := st.CreateTeamMember(context.Background(), in)
	if err != nil {
		t.Fatalf("CreateTeamMember: %v", err)
	}
	t.Cleanup(func() { _ = st.DeleteTeamMember(context.Background(), member.ID) })
	return member
}

// ownerNotes is the text of a lead's owner_change entries, oldest first.
func ownerNotes(activities []model.Activity) []string {
	var out []string
	for i := len(activities) - 1; i >= 0; i-- {
		if activities[i].Type == model.ActivityOwnerChange {
			out = append(out, activities[i].Note)
		}
	}
	return out
}

func TestUpdateLeadOwnerLogsEveryHandover(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	asha := createTestMember(t, st, "Asha Owner")
	ravi := createTestMember(t, st, "Ravi Owner")
	lead := createTestLead(t, st, uniqueInput(t, "owner-handover"))

	steps := []*int64{&asha.ID, &asha.ID, &ravi.ID, nil}
	var got model.Lead
	for _, owner := range steps {
		var err error
		if got, err = st.UpdateLeadOwner(ctx, lead.ID, owner); err != nil {
			t.Fatalf("UpdateLeadOwner(%v): %v", owner, err)
		}
	}

	if got.OwnerID != nil {
		t.Errorf("owner after unassigning = %d, want none", *got.OwnerID)
	}
	// Giving Asha a lead she already owns is not a handover.
	want := []string{
		"Owner set to Asha Owner.",
		"Owner changed from Asha Owner to Ravi Owner.",
		"Owner removed (was Ravi Owner).",
	}
	if notes := ownerNotes(got.Activities); !equalStrings(notes, want) {
		t.Errorf("owner entries = %q, want %q", notes, want)
	}
}

func TestUpdateLeadOwnerErrors(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	lead := createTestLead(t, st, uniqueInput(t, "owner-errors"))

	missing := int64(999_999_999)
	_, err := st.UpdateLeadOwner(ctx, lead.ID, &missing)
	var fieldErr *FieldError
	if !errors.As(err, &fieldErr) || fieldErr.Field != "owner_id" {
		t.Errorf("unknown member: err = %v, want a FieldError on owner_id", err)
	}

	if _, err := st.UpdateLeadOwner(ctx, missing, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing lead: err = %v, want ErrNotFound", err)
	}
}

func TestFullEditLogsOwnerChange(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	asha := createTestMember(t, st, "Asha Editor")
	lead := createTestLead(t, st, uniqueInput(t, "owner-edit"))

	in := uniqueInput(t, "owner-edit")
	in.OwnerID = &asha.ID
	in.Normalize()
	updated, err := st.UpdateLead(ctx, lead.ID, in)
	if err != nil {
		t.Fatalf("UpdateLead: %v", err)
	}

	// Saving the form again without touching the owner logs nothing more.
	if updated, err = st.UpdateLead(ctx, lead.ID, in); err != nil {
		t.Fatalf("UpdateLead (unchanged): %v", err)
	}

	want := []string{"Owner set to Asha Editor."}
	if notes := ownerNotes(updated.Activities); !equalStrings(notes, want) {
		t.Errorf("owner entries = %q, want %q", notes, want)
	}
}

func TestListLeadsFiltersByOwner(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	asha := createTestMember(t, st, "Asha Filter")
	marker := fmt.Sprintf("ownerfilter%d", time.Now().UnixNano())

	owned := uniqueInput(t, marker+"-a")
	owned.OwnerID = &asha.ID
	ownedLead := createTestLead(t, st, owned)
	unownedLead := createTestLead(t, st, uniqueInput(t, marker+"-b"))

	base := model.LeadQuery{Search: marker, SortBy: "created_at", SortDir: "asc", Page: 1, PageSize: 10}

	cases := []struct {
		name  string
		query func(q *model.LeadQuery)
		want  []int64
	}{
		{"any owner", func(q *model.LeadQuery) {}, []int64{ownedLead.ID, unownedLead.ID}},
		{"one member", func(q *model.LeadQuery) { q.OwnerID = &asha.ID }, []int64{ownedLead.ID}},
		{"unowned", func(q *model.LeadQuery) { q.Unowned = true }, []int64{unownedLead.ID}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := base
			tc.query(&q)
			leads, total, err := st.ListLeads(ctx, q)
			if err != nil {
				t.Fatalf("ListLeads: %v", err)
			}
			if total != len(tc.want) || len(leads) != len(tc.want) {
				t.Fatalf("got %d leads (total %d), want %d", len(leads), total, len(tc.want))
			}
			for i, id := range tc.want {
				if leads[i].ID != id {
					t.Errorf("lead %d = id %d, want %d", i, leads[i].ID, id)
				}
			}
		})
	}
}

func TestUpdateLeadNotesChangesOnlyNotes(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	lead := createTestLead(t, st, uniqueInput(t, "notes-only"))

	updated, err := st.UpdateLeadNotes(ctx, lead.ID, "Prefers WhatsApp.\nBudget approved.")
	if err != nil {
		t.Fatalf("UpdateLeadNotes: %v", err)
	}
	if updated.Notes != "Prefers WhatsApp.\nBudget approved." {
		t.Errorf("notes = %q", updated.Notes)
	}
	if updated.Name != lead.Name || updated.Email != lead.Email || updated.Stage != lead.Stage {
		t.Errorf("other fields changed: %+v", updated)
	}

	if _, err := st.UpdateLeadNotes(ctx, 999_999_999, "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing lead: err = %v, want ErrNotFound", err)
	}
}

func TestEditAndDeleteTimelineEntries(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	lead := createTestLead(t, st, uniqueInput(t, "edit-activity"))

	call, err := st.CreateActivity(ctx, lead.ID, model.ActivityInput{Type: model.ActivityCall, Note: "Called, no answer"})
	if err != nil {
		t.Fatalf("CreateActivity: %v", err)
	}
	if call.EditedAt != nil {
		t.Errorf("new entry already has edited_at")
	}

	// Saving the same text is not an edit.
	same, err := st.UpdateActivity(ctx, call.ID, "Called, no answer")
	if err != nil {
		t.Fatalf("UpdateActivity (same text): %v", err)
	}
	if same.EditedAt != nil {
		t.Errorf("unchanged text set edited_at")
	}

	edited, err := st.UpdateActivity(ctx, call.ID, "Called, left a voicemail")
	if err != nil {
		t.Fatalf("UpdateActivity: %v", err)
	}
	if edited.Note != "Called, left a voicemail" || edited.EditedAt == nil {
		t.Errorf("edited = %+v, want new note and edited_at set", edited)
	}
	if edited.Type != model.ActivityCall || !edited.CreatedAt.Equal(call.CreatedAt) {
		t.Errorf("type or time changed: %+v", edited)
	}

	// The "Lead created" entry was written by the server and is locked.
	fetched, err := st.GetLead(ctx, lead.ID)
	if err != nil {
		t.Fatalf("GetLead: %v", err)
	}
	var created model.Activity
	for _, a := range fetched.Activities {
		if a.Type == model.ActivityCreated {
			created = a
		}
	}
	if _, err := st.UpdateActivity(ctx, created.ID, "rewritten"); !errors.Is(err, ErrActivityLocked) {
		t.Errorf("edit system entry: err = %v, want ErrActivityLocked", err)
	}
	if err := st.DeleteActivity(ctx, created.ID); !errors.Is(err, ErrActivityLocked) {
		t.Errorf("delete system entry: err = %v, want ErrActivityLocked", err)
	}

	if err := st.DeleteActivity(ctx, call.ID); err != nil {
		t.Fatalf("DeleteActivity: %v", err)
	}
	if err := st.DeleteActivity(ctx, call.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete twice: err = %v, want ErrNotFound", err)
	}
	if _, err := st.UpdateActivity(ctx, call.ID, "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("edit deleted: err = %v, want ErrNotFound", err)
	}
}
