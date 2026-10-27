package store

import (
	"context"
	"errors"
	"slices"
	"testing"

	"crm-lead-management/backend/internal/model"
)

// Integration tests for tasks, the deal-value forecast and lead health. Same rules as
// store_test.go: skipped without TEST_DATABASE_URL, and every row is cleaned up.

func createTestTask(t *testing.T, st *Store, leadID int64, title, due string) model.Task {
	t.Helper()

	in := model.TaskInput{Title: title, DueDate: due}
	in.Normalize()
	task, err := st.CreateTask(context.Background(), leadID, in)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	// No explicit cleanup: deleting the lead cascades to its tasks.
	return task
}

func TestCompletingATaskLogsItOnceOnTheTimeline(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	lead := createTestLead(t, st, uniqueInput(t, "task-complete"))
	task := createTestTask(t, st, lead.ID, "Send the quote", "2026-01-15")

	if task.CompletedAt != nil {
		t.Fatal("a new task should be open")
	}

	done, activity, err := st.SetTaskCompleted(ctx, task.ID, true)
	if err != nil {
		t.Fatalf("SetTaskCompleted: %v", err)
	}
	if done.CompletedAt == nil {
		t.Error("expected completed_at to be set")
	}
	if activity == nil || activity.Type != model.ActivityTaskCompleted {
		t.Fatalf("expected a task_completed activity, got %+v", activity)
	}

	// A second "complete" (a double click) must not log the task twice, and must
	// not move the completion time.
	again, activity, err := st.SetTaskCompleted(ctx, task.ID, true)
	if err != nil {
		t.Fatalf("SetTaskCompleted again: %v", err)
	}
	if activity != nil {
		t.Errorf("completing an already-complete task logged %+v", activity)
	}
	if !again.CompletedAt.Equal(*done.CompletedAt) {
		t.Errorf("completed_at moved from %v to %v", done.CompletedAt, again.CompletedAt)
	}

	activities, err := st.ListActivities(ctx, lead.ID)
	if err != nil {
		t.Fatalf("ListActivities: %v", err)
	}
	logged := 0
	for _, a := range activities {
		if a.Type == model.ActivityTaskCompleted {
			logged++
		}
	}
	if logged != 1 {
		t.Errorf("found %d task_completed activities, want 1", logged)
	}

	// Reopening clears completed_at and logs nothing new.
	reopened, activity, err := st.SetTaskCompleted(ctx, task.ID, false)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if reopened.CompletedAt != nil || activity != nil {
		t.Errorf("reopen: completed_at=%v activity=%+v, want both nil", reopened.CompletedAt, activity)
	}
}

func TestTasksForMissingLeadOrTaskAreNotFound(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	const missing = int64(1 << 62)

	if _, err := st.CreateTask(ctx, missing, model.TaskInput{Title: "x", DueDate: "2026-01-01"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("CreateTask on missing lead: got %v, want ErrNotFound", err)
	}
	if _, err := st.ListTasks(ctx, missing); !errors.Is(err, ErrNotFound) {
		t.Errorf("ListTasks on missing lead: got %v, want ErrNotFound", err)
	}
	if _, _, err := st.SetTaskCompleted(ctx, missing, true); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetTaskCompleted on missing task: got %v, want ErrNotFound", err)
	}
	if err := st.DeleteTask(ctx, missing); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteTask on missing task: got %v, want ErrNotFound", err)
	}
}

func TestDeletingALeadDeletesItsTasks(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	lead := createTestLead(t, st, uniqueInput(t, "task-cascade"))
	task := createTestTask(t, st, lead.ID, "Follow up", "2026-01-15")

	if err := st.DeleteLead(ctx, lead.ID); err != nil {
		t.Fatalf("DeleteLead: %v", err)
	}
	if err := st.DeleteTask(ctx, task.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("task survived its lead: DeleteTask returned %v", err)
	}
}

func TestDashboardForecastAndFollowUps(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	before, err := st.DashboardSummary(ctx)
	if err != nil {
		t.Fatalf("DashboardSummary: %v", err)
	}

	// A valued lead in Proposal Sent (60%) with an overdue task, and an unvalued
	// lead in New with no task at all.
	value := int64(100_000)
	in := uniqueInput(t, "forecast-valued")
	in.Stage = model.StageProposalSent
	in.DealValue = &value
	valued := createTestLead(t, st, in)
	createTestTask(t, st, valued.ID, "Chase the signature", "2000-01-01")

	createTestLead(t, st, uniqueInput(t, "forecast-unvalued"))

	after, err := st.DashboardSummary(ctx)
	if err != nil {
		t.Fatalf("DashboardSummary: %v", err)
	}

	if got := after.PipelineValue - before.PipelineValue; got != value {
		t.Errorf("pipeline_value grew by %d, want %d", got, value)
	}
	if got := after.WeightedForecast - before.WeightedForecast; got != 60_000 {
		t.Errorf("weighted_forecast grew by %d, want 60000 (100000 x 60%%)", got)
	}
	if got := after.OverdueTasks - before.OverdueTasks; got != 1 {
		t.Errorf("overdue_tasks grew by %d, want 1", got)
	}
	// Only the unvalued lead has no open task.
	if got := after.OpenLeadsWithoutTask - before.OpenLeadsWithoutTask; got != 1 {
		t.Errorf("open_leads_without_task grew by %d, want 1", got)
	}

	// The weighted column must add up to the headline forecast.
	var sum int64
	for _, sc := range after.ByStage {
		if !sc.Stage.IsClosed() {
			sum += sc.Weighted
		}
	}
	if sum != after.WeightedForecast {
		t.Errorf("weighted per stage sums to %d, but weighted_forecast = %d", sum, after.WeightedForecast)
	}

	// Due tasks carry their lead's name, so the dashboard can say whose they are.
	found := false
	for _, task := range after.DueTasks {
		if task.LeadID == valued.ID {
			found = true
			if task.LeadName != valued.Name || task.LeadCompany != valued.Company {
				t.Errorf("due task lead = %q/%q, want %q/%q",
					task.LeadName, task.LeadCompany, valued.Name, valued.Company)
			}
		}
	}
	if !found && len(after.DueTasks) < 10 {
		t.Error("the overdue task is missing from due_tasks")
	}
}

func TestLeadHealthFollowsRealContact(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	in := uniqueInput(t, "health")
	in.Stage = model.StageNegotiation // cooling at 3 days, cold at 7
	lead := createTestLead(t, st, in)

	// Pretend the lead, and everything on its timeline so far, is 30 days old.
	if _, err := st.pool.Exec(ctx, `
		UPDATE leads SET created_at = now() - interval '30 days' WHERE id = $1;`, lead.ID); err != nil {
		t.Fatalf("backdate lead: %v", err)
	}
	if _, err := st.pool.Exec(ctx, `
		UPDATE activities SET created_at = now() - interval '30 days' WHERE lead_id = $1;`, lead.ID); err != nil {
		t.Fatalf("backdate activities: %v", err)
	}

	got, err := st.GetLead(ctx, lead.ID)
	if err != nil {
		t.Fatalf("GetLead: %v", err)
	}
	if got.Health != model.HealthCold || got.DaysSinceTouch != 30 {
		t.Fatalf("health = %q after %d days, want cold after 30", got.Health, got.DaysSinceTouch)
	}

	summary, err := st.DashboardSummary(ctx)
	if err != nil {
		t.Fatalf("DashboardSummary: %v", err)
	}
	if summary.ColdLeads < 1 {
		t.Errorf("cold_leads = %d, want at least 1", summary.ColdLeads)
	}
	if len(summary.GoingCold) > 5 {
		t.Errorf("going_cold has %d leads, want at most 5", len(summary.GoingCold))
	}

	// A stage change is bookkeeping, not contact: still cold.
	moved, err := st.UpdateLeadStage(ctx, lead.ID, model.StageInput{Stage: model.StageProposalSent})
	if err != nil {
		t.Fatalf("UpdateLeadStage: %v", err)
	}
	if moved.Health != model.HealthCold {
		t.Errorf("after a stage change health = %q, want still cold", moved.Health)
	}

	// Logging a call is contact: active again.
	if _, err := st.CreateActivity(ctx, lead.ID, model.ActivityInput{Type: model.ActivityCall, Note: "Caught up"}); err != nil {
		t.Fatalf("CreateActivity: %v", err)
	}
	got, err = st.GetLead(ctx, lead.ID)
	if err != nil {
		t.Fatalf("GetLead: %v", err)
	}
	if got.Health != model.HealthActive || got.DaysSinceTouch != 0 {
		t.Errorf("after a call health = %q (%d days), want active (0 days)", got.Health, got.DaysSinceTouch)
	}
	if got.Score == nil {
		t.Fatal("open lead has no score")
	}
}

func TestLeadScoreCountsConversationsAndOverdueTasks(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	lead := createTestLead(t, st, uniqueInput(t, "score-signals"))

	// A note is contact, but not a conversation.
	for _, typ := range []model.ActivityType{model.ActivityCall, model.ActivityEmail, model.ActivityNote} {
		if _, err := st.CreateActivity(ctx, lead.ID, model.ActivityInput{Type: typ, Note: "score test"}); err != nil {
			t.Fatalf("CreateActivity(%s): %v", typ, err)
		}
	}
	createTestTask(t, st, lead.ID, "Long overdue", "2026-01-15")

	got, err := st.GetLead(ctx, lead.ID)
	if err != nil {
		t.Fatalf("GetLead: %v", err)
	}
	if got.Score == nil {
		t.Fatal("open lead has no score")
	}

	// Two conversations (call, email) earn the middle engagement tier.
	for _, f := range got.Score.Factors {
		if f.Key == "engagement" && f.Points != 10 {
			t.Errorf("engagement = %d, want 10 for two conversations", f.Points)
		}
	}
	if len(got.Score.Penalties) != 1 || got.Score.Penalties[0].Key != "overdue_tasks" ||
		got.Score.Penalties[0].Points != -5 {
		t.Errorf("penalties = %+v, want one overdue_tasks penalty of -5", got.Score.Penalties)
	}
}

func TestListLeadsSortsAndFiltersByScore(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	// Every lead here is new, so each is active (+20) with no conversations
	// or task; uniqueInput gives a referral (+10), phone (+3) and notes (+2).
	value := int64(300_000)
	hot := uniqueInput(t, "band-hot")
	hot.Stage, hot.DealValue = model.StageNegotiation, &value // 30+20+10+10+3+2 = 75
	warm := uniqueInput(t, "band-warm")
	warm.Stage = model.StageQualified // 15+20+10+3+2 = 50
	cold := uniqueInput(t, "band-cold")
	cold.Stage = model.StageNew // 0+20+10+3+2 = 35
	won := uniqueInput(t, "band-won")
	won.Stage = model.StageWon // no score at all

	hotLead := createTestLead(t, st, hot)
	warmLead := createTestLead(t, st, warm)
	coldLead := createTestLead(t, st, cold)
	wonLead := createTestLead(t, st, won)

	// The search keeps other tests' leads out of the results.
	base := model.LeadQuery{Search: "Store Test band-", SortBy: model.SortByScore, SortDir: "desc", Page: 1, PageSize: 3}

	page1, total, err := st.ListLeads(ctx, base)
	if err != nil {
		t.Fatalf("ListLeads by score: %v", err)
	}
	if total != 4 {
		t.Errorf("total = %d, want 4", total)
	}
	gotIDs := func(leads []model.Lead) []int64 {
		out := make([]int64, len(leads))
		for i, l := range leads {
			out[i] = l.ID
		}
		return out
	}
	if got, want := gotIDs(page1), []int64{hotLead.ID, warmLead.ID, coldLead.ID}; !slices.Equal(got, want) {
		t.Errorf("page 1 = %v, want hot, warm, cold %v", got, want)
	}
	for i, wantBand := range []model.ScoreBand{model.BandHot, model.BandWarm, model.BandCold} {
		if i < len(page1) && (page1[i].Score == nil || page1[i].Score.Band != wantBand) {
			t.Errorf("page 1 lead %d score = %+v, want band %s", i, page1[i].Score, wantBand)
		}
	}

	page2q := base
	page2q.Page = 2
	page2, _, err := st.ListLeads(ctx, page2q)
	if err != nil {
		t.Fatalf("ListLeads page 2: %v", err)
	}
	if got, want := gotIDs(page2), []int64{wonLead.ID}; !slices.Equal(got, want) {
		t.Errorf("page 2 = %v, want the unscored won lead %v last", got, want)
	}

	// A band filter combines with the other filters and keeps the column sort.
	warmOnly := model.LeadQuery{Search: "Store Test band-", Band: model.BandWarm, SortBy: "name", SortDir: "asc", Page: 1, PageSize: 10}
	got, total, err := st.ListLeads(ctx, warmOnly)
	if err != nil {
		t.Fatalf("ListLeads band=warm: %v", err)
	}
	if total != 1 || len(got) != 1 || got[0].ID != warmLead.ID {
		t.Errorf("band=warm = %v (total %d), want only %d", gotIDs(got), total, warmLead.ID)
	}

	// A page past the end is empty, not null, and still reports the total.
	pastEnd := base
	pastEnd.Page = 9
	empty, total, err := st.ListLeads(ctx, pastEnd)
	if err != nil {
		t.Fatalf("ListLeads past end: %v", err)
	}
	if empty == nil || len(empty) != 0 || total != 4 {
		t.Errorf("past the end = %v (total %d), want [] with total 4", empty, total)
	}

	// The dashboard's hottest list is ordered by score and never closed leads.
	summary, err := st.DashboardSummary(ctx)
	if err != nil {
		t.Fatalf("DashboardSummary: %v", err)
	}
	if summary.HotCount < 1 {
		t.Errorf("hot_count = %d, want at least our hot lead", summary.HotCount)
	}
	for i, lead := range summary.HottestLeads {
		if lead.Score == nil {
			t.Fatalf("hottest lead %d (%d) has no score", i, lead.ID)
		}
		if i > 0 && lead.Score.Total > summary.HottestLeads[i-1].Score.Total {
			t.Errorf("hottest leads out of order at %d", i)
		}
	}
}

func TestPipelineColumnsAndTotals(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	before, err := st.Pipeline(ctx)
	if err != nil {
		t.Fatalf("Pipeline: %v", err)
	}

	value := int64(200_000)
	in := uniqueInput(t, "pipeline")
	in.Stage = model.StageQualified // 40%
	in.DealValue = &value
	lead := createTestLead(t, st, in)

	after, err := st.Pipeline(ctx)
	if err != nil {
		t.Fatalf("Pipeline: %v", err)
	}

	// Every stage is a column, in lifecycle order, even when empty -- the board
	// needs somewhere to drop a card.
	if len(after) != len(model.Stages) {
		t.Fatalf("got %d columns, want %d", len(after), len(model.Stages))
	}
	for i, stage := range model.Stages {
		if after[i].Stage != stage {
			t.Errorf("column %d = %q, want %q", i, after[i].Stage, stage)
		}
	}

	q := 2 // index of Qualified
	if got := after[q].Count - before[q].Count; got != 1 {
		t.Errorf("qualified count grew by %d, want 1", got)
	}
	if got := after[q].Value - before[q].Value; got != value {
		t.Errorf("qualified value grew by %d, want %d", got, value)
	}
	if after[q].Weighted != after[q].Value*40/100 {
		t.Errorf("qualified weighted = %d, want value x 40%% = %d", after[q].Weighted, after[q].Value*40/100)
	}

	found := false
	for _, card := range after[q].Leads {
		if card.ID == lead.ID {
			found = true
			if card.Score == nil || card.Health == "" {
				t.Error("pipeline card is missing its score or health")
			}
		}
	}
	if !found && len(after[q].Leads) < model.PipelineColumnLimit {
		t.Error("new lead is missing from its column")
	}

	// Cards in an open column are best score first.
	cards := after[q].Leads
	for i := 1; i < len(cards); i++ {
		if scoreOf(cards[i-1]) < scoreOf(cards[i]) {
			t.Errorf("card %d (score %d) is above card %d (score %d)",
				i-1, scoreOf(cards[i-1]), i, scoreOf(cards[i]))
		}
	}
}

func TestListAllTasksAttachesLeadsAndSplitsByStatus(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	lead := createTestLead(t, st, uniqueInput(t, "all-tasks"))
	open := createTestTask(t, st, lead.ID, "Open one", "2000-01-02")
	done := createTestTask(t, st, lead.ID, "Done one", "2000-01-01")
	if _, _, err := st.SetTaskCompleted(ctx, done.ID, true); err != nil {
		t.Fatalf("SetTaskCompleted: %v", err)
	}

	find := func(items []model.TaskWithLead, id int64) *model.TaskWithLead {
		for i := range items {
			if items[i].Task.ID == id {
				return &items[i]
			}
		}
		return nil
	}

	openItems, err := st.ListAllTasks(ctx, false)
	if err != nil {
		t.Fatalf("ListAllTasks(open): %v", err)
	}
	item := find(openItems, open.ID)
	if item == nil {
		t.Fatal("open task missing from the open list")
	}
	if item.Lead.ID != lead.ID || item.Lead.Name != lead.Name {
		t.Errorf("attached lead = %d %q, want %d %q", item.Lead.ID, item.Lead.Name, lead.ID, lead.Name)
	}
	if item.Lead.Health == "" || item.Lead.Score == nil {
		t.Error("attached lead is missing health or score")
	}
	if !item.Lead.HasOpenTask {
		t.Error("attached lead should report an open task")
	}
	if find(openItems, done.ID) != nil {
		t.Error("completed task appeared in the open list")
	}

	doneItems, err := st.ListAllTasks(ctx, true)
	if err != nil {
		t.Fatalf("ListAllTasks(completed): %v", err)
	}
	if find(doneItems, done.ID) == nil {
		t.Error("completed task missing from the completed list")
	}
	if find(doneItems, open.ID) != nil {
		t.Error("open task appeared in the completed list")
	}
}

func TestReportsShapeAndThisWeek(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	before, err := st.Reports(ctx)
	if err != nil {
		t.Fatalf("Reports: %v", err)
	}

	in := uniqueInput(t, "reports")
	in.Source = model.SourceLinkedIn
	in.Stage = model.StageWon
	createTestLead(t, st, in)

	after, err := st.Reports(ctx)
	if err != nil {
		t.Fatalf("Reports: %v", err)
	}

	// Every week is present, zeros included, oldest first; this week is last.
	if len(after.LeadsPerWeek) != model.ReportWeeks {
		t.Fatalf("got %d weeks, want %d", len(after.LeadsPerWeek), model.ReportWeeks)
	}
	last := model.ReportWeeks - 1
	if got := after.LeadsPerWeek[last].Count - before.LeadsPerWeek[last].Count; got != 1 {
		t.Errorf("this week's count grew by %d, want 1", got)
	}

	if len(after.BySource) != len(model.Sources) {
		t.Fatalf("got %d sources, want %d", len(after.BySource), len(model.Sources))
	}
	for _, r := range after.BySource {
		if r.Source != model.SourceLinkedIn {
			continue
		}
		if r.WinRate == nil || r.Won < 1 {
			t.Errorf("LinkedIn after a won lead: won=%d win_rate=%v", r.Won, r.WinRate)
		}
	}

	// One row per open stage.
	open := 0
	for _, stage := range model.Stages {
		if !stage.IsClosed() {
			open++
		}
	}
	if len(after.StageDurations) != open {
		t.Errorf("got %d stage durations, want %d", len(after.StageDurations), open)
	}
}
