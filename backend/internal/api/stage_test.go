package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"crm-lead-management/backend/internal/model"
)

func TestStageEndpointAsksWhyADealWasLost(t *testing.T) {
	router, st := newTestAPI(t)
	ctx := context.Background()

	suffix := fmt.Sprint(time.Now().UnixNano())
	in := model.LeadInput{Name: "Stage Test", Company: "Stage Co " + suffix,
		Email: "stage-" + suffix + "@example.test", Stage: model.StageNegotiation}
	in.Normalize()
	lead, err := st.CreateLead(ctx, in)
	if err != nil {
		t.Fatalf("CreateLead: %v", err)
	}
	t.Cleanup(func() { _ = st.DeleteLead(ctx, lead.ID) })
	path := fmt.Sprintf("/api/leads/%d/stage", lead.ID)

	code, body := call(t, router, "PATCH", path, map[string]any{"stage": "lost"}, nil)
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, `"close_reason"`) {
		t.Errorf("lost without a reason = %d %s, want 422 on close_reason", code, body)
	}

	code, body = call(t, router, "PATCH", path, map[string]any{"stage": "lost", "close_reason": "other"}, nil)
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, `"close_note"`) {
		t.Errorf(`"other" without a note = %d %s, want 422 on close_note`, code, body)
	}

	var lost model.Lead
	code, body = call(t, router, "PATCH", path,
		map[string]any{"stage": "lost", "close_reason": "no_decision", "close_note": "  Went quiet after the demo  "}, &lost)
	if code != http.StatusOK {
		t.Fatalf("lost with a reason = %d %s", code, body)
	}
	if lost.CloseReason == nil || *lost.CloseReason != "no_decision" || lost.CloseNote == nil || *lost.CloseNote != "Went quiet after the demo" {
		t.Errorf("lead = %v %v", lost.CloseReason, lost.CloseNote)
	}

	// The reason lists reach the UI through /api/meta.
	var meta struct {
		LostReasons []model.ReasonOption `json:"lost_reasons"`
		WonReasons  []model.ReasonOption `json:"won_reasons"`
	}
	call(t, router, "GET", "/api/meta", nil, &meta)
	if len(meta.LostReasons) != len(model.LostReasons) || len(meta.WonReasons) != len(model.WonReasons) {
		t.Errorf("meta reasons = %d lost, %d won", len(meta.LostReasons), len(meta.WonReasons))
	}
}
