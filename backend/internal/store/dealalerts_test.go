package store

import (
	"context"
	"errors"
	"testing"

	"crm-lead-management/backend/internal/model"
)

// alertFor finds the bell's alert for one lead, or fails.
func alertFor(t *testing.T, st *Store, leadID int64) model.DealAlert {
	t.Helper()
	list, err := st.ListNotifications(context.Background(), model.MaxNotificationsListed)
	if err != nil {
		t.Fatalf("ListNotifications: %v", err)
	}
	for _, a := range list.DealAlerts {
		if a.LeadID == leadID {
			return a
		}
	}
	t.Fatalf("no deal alert for lead %d among %d alerts", leadID, len(list.DealAlerts))
	return model.DealAlert{}
}

func countAlerts(t *testing.T, st *Store, leadID int64) int {
	t.Helper()
	var n int
	if err := st.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM deal_alerts WHERE lead_id = $1`, leadID).Scan(&n); err != nil {
		t.Fatalf("count alerts: %v", err)
	}
	return n
}

func TestStuckDealAlertLifecycle(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	in := uniqueInput(t, "stuck-deal")
	in.Stage = model.StageProposalSent // stuck after 10 days
	lead := createTestLead(t, st, in)

	// Not stuck on day one: nothing raised for it.
	if _, err := st.RaiseStuckDealAlerts(ctx); err != nil {
		t.Fatalf("RaiseStuckDealAlerts: %v", err)
	}
	if n := countAlerts(t, st, lead.ID); n != 0 {
		t.Fatalf("fresh deal has %d alerts", n)
	}

	// Pretend it entered Proposal Sent 12 days ago.
	if _, err := st.pool.Exec(ctx, `
		UPDATE leads SET created_at = now() - interval '12 days' WHERE id = $1;`, lead.ID); err != nil {
		t.Fatalf("backdate lead: %v", err)
	}
	if _, err := st.pool.Exec(ctx, `
		UPDATE lead_stage_history SET changed_at = now() - interval '12 days' WHERE lead_id = $1;`, lead.ID); err != nil {
		t.Fatalf("backdate history: %v", err)
	}

	got, err := st.GetLead(ctx, lead.ID)
	if err != nil {
		t.Fatalf("GetLead: %v", err)
	}
	if !got.Stuck || got.DaysInStage != 12 || got.StuckLimitDays != 10 {
		t.Errorf("lead stuck=%v days=%d limit=%d, want stuck after 12 of 10 days",
			got.Stuck, got.DaysInStage, got.StuckLimitDays)
	}

	// Raised once, however often the job runs.
	for range 3 {
		if _, err := st.RaiseStuckDealAlerts(ctx); err != nil {
			t.Fatalf("RaiseStuckDealAlerts: %v", err)
		}
	}
	if n := countAlerts(t, st, lead.ID); n != 1 {
		t.Fatalf("stuck deal has %d alerts, want 1", n)
	}

	alert := alertFor(t, st, lead.ID)
	if alert.Resolved || alert.Stage != model.StageProposalSent || alert.DaysInStage != 12 || alert.LimitDays != 10 {
		t.Errorf("alert = %+v, want unresolved, Proposal Sent, 12 of 10 days", alert)
	}
	if alert.LeadName != got.Name || alert.ReadAt != nil {
		t.Errorf("alert lead=%q read=%v", alert.LeadName, alert.ReadAt)
	}

	list, err := st.ListNotifications(ctx, model.MaxNotificationsListed)
	if err != nil {
		t.Fatalf("ListNotifications: %v", err)
	}
	if list.AlertsUnread < 1 {
		t.Errorf("alerts_unread = %d, want at least 1", list.AlertsUnread)
	}

	summary, err := st.DashboardSummary(ctx)
	if err != nil {
		t.Fatalf("DashboardSummary: %v", err)
	}
	if summary.StuckCount < 1 || len(summary.StuckDeals) > 5 {
		t.Errorf("dashboard stuck_count=%d with %d listed", summary.StuckCount, len(summary.StuckDeals))
	}

	// Reading it.
	if err := st.MarkDealAlertRead(ctx, alert.ID); err != nil {
		t.Fatalf("MarkDealAlertRead: %v", err)
	}
	if a := alertFor(t, st, lead.ID); a.ReadAt == nil {
		t.Error("alert still unread after MarkDealAlertRead")
	}
	if err := st.MarkDealAlertRead(ctx, 999_999_999); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing alert: err = %v, want ErrNotFound", err)
	}

	// Moving the deal on resolves the alert and freezes its day count; the
	// fresh stint in Negotiation is not stuck, so nothing new is raised.
	if _, err := st.UpdateLeadStage(ctx, lead.ID, model.StageInput{Stage: model.StageNegotiation}); err != nil {
		t.Fatalf("UpdateLeadStage: %v", err)
	}
	if _, err := st.RaiseStuckDealAlerts(ctx); err != nil {
		t.Fatalf("RaiseStuckDealAlerts: %v", err)
	}
	if n := countAlerts(t, st, lead.ID); n != 1 {
		t.Errorf("after moving on, %d alerts, want still 1", n)
	}
	resolved := alertFor(t, st, lead.ID)
	if !resolved.Resolved || resolved.DaysInStage != 12 {
		t.Errorf("after moving on: resolved=%v days=%d, want resolved after 12 days", resolved.Resolved, resolved.DaysInStage)
	}

	moved, err := st.GetLead(ctx, lead.ID)
	if err != nil {
		t.Fatalf("GetLead: %v", err)
	}
	if moved.Stuck || moved.DaysInStage != 0 {
		t.Errorf("after moving on: stuck=%v days=%d, want a fresh stint", moved.Stuck, moved.DaysInStage)
	}
}

func TestFollowUpsListsStuckDeals(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	owner := createTestMember(t, st, "Stuck Owner")
	in := uniqueInput(t, "stuck-followup")
	in.Stage = model.StageNegotiation // stuck after 14 days
	in.OwnerID = &owner.ID
	lead := createTestLead(t, st, in)
	if _, err := st.pool.Exec(ctx, `
		UPDATE lead_stage_history SET changed_at = now() - interval '15 days' WHERE lead_id = $1;`, lead.ID); err != nil {
		t.Fatalf("backdate history: %v", err)
	}

	summary, err := st.FollowUps(ctx, model.OwnerFilter{OwnerID: &owner.ID})
	if err != nil {
		t.Fatalf("FollowUps: %v", err)
	}
	if ids := leadIDs(summary.Stuck); len(ids) != 1 || ids[0] != lead.ID {
		t.Errorf("stuck = %v, want only lead %d", ids, lead.ID)
	}
}
