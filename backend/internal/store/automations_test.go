package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"crm-lead-management/backend/internal/model"
)

// automationDealValue is a deal value no other test uses. Every rule these
// tests create requires it, so leads created by other tests running at the
// same time against the same database never trigger them.
const automationDealValue int64 = 987_654_321

func createTestRule(t *testing.T, st *Store, in model.AutomationRuleInput) model.AutomationRule {
	t.Helper()

	if in.MinDealValue == nil {
		v := automationDealValue
		in.MinDealValue = &v
	}
	in.Name = fmt.Sprintf("%s %d", in.Name, time.Now().UnixNano())
	in.Normalize()
	if errs := in.Validate(); errs.Any() {
		t.Fatalf("invalid rule: %v", errs)
	}
	rule, err := st.CreateAutomationRule(context.Background(), in)
	if err != nil {
		t.Fatalf("CreateAutomationRule: %v", err)
	}
	t.Cleanup(func() { _ = st.DeleteAutomationRule(context.Background(), rule.ID) })
	return rule
}

func automationLeadInput(t *testing.T, suffix string, source model.Source) model.LeadInput {
	in := uniqueInput(t, fmt.Sprintf("automation-%s-%d", suffix, time.Now().UnixNano()))
	in.Name = "Asha Rao"
	in.Source = source
	v := automationDealValue
	in.DealValue = &v
	return in
}

func TestNewLeadRunsMatchingAutomationRules(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	owner := createTestMember(t, st, "Ravi Kumar")
	other := createTestMember(t, st, "Meera Shah")

	toOwner := createTestRule(t, st, model.AutomationRuleInput{
		Name:      "Intro call",
		TaskTitle: "Intro call with {{first_name}} ({{owner_name}})",
		TaskType:  model.TaskCall,
		DueInDays: 2,
	})
	toOther := createTestRule(t, st, model.AutomationRuleInput{
		Name:       "Website reply",
		Sources:    []model.Source{model.SourceWebsite},
		TaskTitle:  "Reply to {{name}}",
		TaskType:   model.TaskEmail,
		AssigneeID: &other.ID,
	})
	off := false
	createTestRule(t, st, model.AutomationRuleInput{
		Name: "Switched off", Enabled: &off, TaskTitle: "Should not appear",
	})
	tooBig := automationDealValue + 1
	createTestRule(t, st, model.AutomationRuleInput{
		Name: "Bigger deals", MinDealValue: &tooBig, TaskTitle: "Should not appear either",
	})

	in := automationLeadInput(t, "match", model.SourceWebsite)
	in.OwnerID = &owner.ID
	lead := createTestLead(t, st, in)

	tasks, err := st.ListTasks(ctx, lead.ID)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("got %d tasks, want 2 (the two matching, enabled rules): %+v", len(tasks), tasks)
	}

	byTitle := map[string]model.Task{}
	for _, task := range tasks {
		byTitle[task.Title] = task
	}

	call, ok := byTitle["Intro call with Asha (Ravi Kumar)"]
	if !ok {
		t.Fatalf("no intro call task; titles: %v", byTitle)
	}
	if call.Type != model.TaskCall {
		t.Errorf("intro call type = %q, want call", call.Type)
	}
	if call.AssigneeID == nil || *call.AssigneeID != owner.ID {
		t.Errorf("intro call assignee = %v, want the lead owner %d", call.AssigneeID, owner.ID)
	}
	var wantDue string
	if err := st.pool.QueryRow(ctx, `SELECT (CURRENT_DATE + 2)::text`).Scan(&wantDue); err != nil {
		t.Fatal(err)
	}
	if call.DueDate != wantDue {
		t.Errorf("intro call due %s, want %s", call.DueDate, wantDue)
	}

	reply, ok := byTitle["Reply to Asha Rao"]
	if !ok {
		t.Fatalf("no website reply task; titles: %v", byTitle)
	}
	if reply.AssigneeID == nil || *reply.AssigneeID != other.ID {
		t.Errorf("reply assignee = %v, want the rule's assignee %d", reply.AssigneeID, other.ID)
	}

	// Each run is on the timeline.
	activities, err := st.ListActivities(ctx, lead.ID)
	if err != nil {
		t.Fatalf("ListActivities: %v", err)
	}
	automated := 0
	for _, a := range activities {
		if a.Type == model.ActivityAutomation {
			automated++
		}
	}
	if automated != 2 {
		t.Errorf("got %d automation timeline entries, want 2", automated)
	}

	// And counted on the rule.
	rules, err := st.ListAutomationRules(ctx)
	if err != nil {
		t.Fatalf("ListAutomationRules: %v", err)
	}
	for _, r := range rules {
		if (r.ID == toOwner.ID || r.ID == toOther.ID) && (r.RunCount != 1 || r.LastRunAt == nil) {
			t.Errorf("rule %q run_count = %d, last_run_at = %v; want 1 and set", r.Name, r.RunCount, r.LastRunAt)
		}
	}

	// A source the website rule does not list gets only the intro call.
	referral := createTestLead(t, st, automationLeadInput(t, "referral", model.SourceReferral))
	tasks, err = st.ListTasks(ctx, referral.ID)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 1 || tasks[0].Title != "Intro call with Asha ()" {
		t.Errorf("referral lead tasks = %+v, want only the intro call, with the blank owner left out", tasks)
	}
	if tasks[0].AssigneeID != nil {
		t.Errorf("unowned lead's task assignee = %v, want nobody", *tasks[0].AssigneeID)
	}
}

func TestAutomationRuleErrors(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	rule := createTestRule(t, st, model.AutomationRuleInput{Name: "Unique", TaskTitle: "Call"})

	dup := model.AutomationRuleInput{Name: rule.Name, TaskTitle: "Call"}
	dup.Normalize()
	_, err := st.CreateAutomationRule(ctx, dup)
	wantFieldError(t, err, "name")

	missing := int64(1 << 40)
	bad := model.AutomationRuleInput{Name: "No such member " + rule.Name, TaskTitle: "Call", AssigneeID: &missing}
	bad.Normalize()
	_, err = st.CreateAutomationRule(ctx, bad)
	wantFieldError(t, err, "assignee_id")

	if _, err := st.UpdateAutomationRule(ctx, missing, dup); err != ErrNotFound {
		t.Errorf("update of a missing rule: err = %v, want ErrNotFound", err)
	}
	if err := st.DeleteAutomationRule(ctx, missing); err != ErrNotFound {
		t.Errorf("delete of a missing rule: err = %v, want ErrNotFound", err)
	}
}
