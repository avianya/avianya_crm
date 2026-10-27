package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"crm-lead-management/backend/internal/model"
)

func TestBulkLeadActions(t *testing.T) {
	router, st := newTestAPI(t)
	ctx := context.Background()
	stamp := time.Now().UnixNano()

	memberIn := model.TeamMemberInput{Name: "Bulk Owner", Email: fmt.Sprintf("bulk-owner-%d@example.test", stamp)}
	memberIn.Normalize()
	member, err := st.CreateTeamMember(ctx, memberIn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.DeleteTeamMember(ctx, member.ID) })

	var ids []int64
	for i := range 3 {
		in := model.LeadInput{
			Name: fmt.Sprintf("Bulk %d", i), Company: "Bulk Co",
			Email: fmt.Sprintf("bulk-%d-%d@example.test", stamp, i),
		}
		in.Normalize()
		lead, err := st.CreateLead(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, lead.ID)
		t.Cleanup(func() { _ = st.DeleteLead(ctx, lead.ID) })
	}
	missing := int64(1 << 40)

	bulk := func(body map[string]any) model.BulkResult {
		t.Helper()
		var result model.BulkResult
		if code, raw := call(t, router, http.MethodPost, "/api/leads/bulk", body, &result); code != http.StatusOK {
			t.Fatalf("bulk %v: %d %s", body, code, raw)
		}
		return result
	}

	// Owner: every lead changes and logs the handover; a missing id is reported.
	result := bulk(map[string]any{"ids": append(ids, missing), "action": "owner", "owner_id": member.ID})
	if result.Changed != 3 || len(result.Failed) != 1 || result.Failed[0].ID != missing ||
		result.Failed[0].Message != "This lead no longer exists." {
		t.Errorf("owner result = %+v", result)
	}
	lead, err := st.GetLead(ctx, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if lead.OwnerID == nil || *lead.OwnerID != member.ID {
		t.Errorf("owner = %v, want %d", lead.OwnerID, member.ID)
	}
	logged := false
	for _, a := range lead.Activities {
		logged = logged || a.Type == model.ActivityOwnerChange
	}
	if !logged {
		t.Error("owner change not on the timeline")
	}

	// Stage: Lost without a reason fails for each lead; with one, all move.
	result = bulk(map[string]any{"ids": ids, "action": "stage", "stage": "lost"})
	if result.Changed != 0 || len(result.Failed) != 3 || !strings.HasPrefix(result.Failed[0].Message, "Close reason is required") {
		t.Errorf("lost without reason = %+v", result)
	}
	result = bulk(map[string]any{"ids": ids, "action": "stage", "stage": "lost", "close_reason": "timing"})
	if result.Changed != 3 || len(result.Failed) != 0 {
		t.Errorf("lost with reason = %+v", result)
	}
	if lead, _ := st.GetLead(ctx, ids[1]); lead.Stage != model.StageLost || lead.CloseReason == nil || *lead.CloseReason != "timing" {
		t.Errorf("lead after bulk lost = %s %v", lead.Stage, lead.CloseReason)
	}

	// Source.
	result = bulk(map[string]any{"ids": ids[:2], "action": "source", "source": "linkedin"})
	if result.Changed != 2 {
		t.Errorf("source result = %+v", result)
	}
	if lead, _ := st.GetLead(ctx, ids[0]); lead.Source != model.SourceLinkedIn {
		t.Errorf("source = %s, want linkedin", lead.Source)
	}

	// Bad input is a 422, not a partial change.
	if code, raw := call(t, router, http.MethodPost, "/api/leads/bulk",
		map[string]any{"ids": ids, "action": "source", "source": "fax"}, nil); code != http.StatusUnprocessableEntity {
		t.Errorf("bad source: %d %s", code, raw)
	}

	// Export of just the ticked leads.
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/leads/export?ids=%d,%d", ids[0], ids[2]), nil)
	req.Header.Set(adminKeyHeader, testAdminKey)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if lines := strings.Split(strings.TrimSpace(rec.Body.String()), "\n"); rec.Code != http.StatusOK || len(lines) != 3 {
		t.Errorf("export of 2 ids: %d, %d lines:\n%s", rec.Code, len(lines), rec.Body.String())
	}

	// Delete.
	result = bulk(map[string]any{"ids": ids[:1], "action": "delete"})
	if result.Changed != 1 {
		t.Errorf("delete result = %+v", result)
	}
	if _, err := st.GetLead(ctx, ids[0]); err == nil {
		t.Error("deleted lead still there")
	}
}
