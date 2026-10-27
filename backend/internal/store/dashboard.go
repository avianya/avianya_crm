package store

import (
	"context"
	"fmt"
	"math"

	"crm-lead-management/backend/internal/model"
)

// DashboardSummary builds the aggregate view for GET /api/dashboard.
//
// A handful of queries, each doing its aggregation in the database rather than pulling
// every lead into Go and counting in a loop. That difference stops mattering at
// 14 rows and starts mattering a lot at 140,000.
func (s *Store) DashboardSummary(ctx context.Context) (model.DashboardSummary, error) {
	summary := model.DashboardSummary{}

	// --- 1. counts per stage ------------------------------------------------
	//
	// GROUP BY only returns rows for stages that actually occur, so the result
	// is collected into a map and then expanded against model.Stages below.
	// That way every stage appears in the response -- including the ones with a
	// count of zero, which the chart needs in order to draw an empty bar.
	//
	// The same pass sums deal values, and counts the leads with no value so the
	// UI can say "3 leads not valued yet" instead of quietly under-forecasting.
	rows, err := s.query(ctx, `
		SELECT stage::text,
		       COUNT(*),
		       COALESCE(SUM(deal_value), 0),
		       COUNT(*) FILTER (WHERE deal_value IS NULL)
		FROM leads
		GROUP BY stage`)
	if err != nil {
		return summary, fmt.Errorf("dashboard stage counts: %w", err)
	}
	defer rows.Close()

	type stageTotals struct {
		count, unvalued int
		value           int64
	}

	totals := make(map[model.Stage]stageTotals, len(model.Stages))
	for rows.Next() {
		var (
			stage string
			t     stageTotals
		)
		if err := rows.Scan(&stage, &t.count, &t.value, &t.unvalued); err != nil {
			return summary, fmt.Errorf("scan stage count: %w", err)
		}
		totals[model.Stage(stage)] = t
	}
	if err := rows.Err(); err != nil {
		return summary, fmt.Errorf("iterate stage counts: %w", err)
	}

	// Iterating model.Stages (not the map) guarantees the response is always in
	// lifecycle order. Map iteration order in Go is deliberately randomised, so
	// ranging over `counts` here would shuffle the chart on every request.
	summary.ByStage = make([]model.StageCount, 0, len(model.Stages))
	for _, stage := range model.Stages {
		t := totals[stage] // missing key yields zeros, which is exactly what we want
		count := t.count

		// Integer maths on whole rupees: the forecast is an estimate, and a
		// rupee of truncation per stage is invisible next to that.
		probability := stage.WinProbability()
		weighted := t.value * int64(probability) / 100

		summary.ByStage = append(summary.ByStage, model.StageCount{
			Stage:         stage,
			Label:         stage.Label(),
			Count:         count,
			Value:         t.value,
			UnvaluedCount: t.unvalued,
			Probability:   probability,
			Weighted:      weighted,
		})

		summary.TotalLeads += count
		switch stage {
		case model.StageWon:
			summary.WonCount = count
			summary.WonValue = t.value
		case model.StageLost:
			summary.LostCount = count
		default:
			summary.OpenCount += count
			summary.PipelineValue += t.value
			summary.WeightedForecast += weighted
		}
	}

	// Conversion rate over *closed* deals only: open leads have not failed to
	// convert yet, so counting them in the denominator would make the number
	// look worse every time a new lead arrives.
	if closed := summary.WonCount + summary.LostCount; closed > 0 {
		rate := float64(summary.WonCount) / float64(closed) * 100
		summary.ConversionRate = math.Round(rate*10) / 10 // one decimal place
	}

	// --- 2. leads added in the last 7 days ----------------------------------
	if err := s.queryRow(ctx, `
		SELECT COUNT(*)
		FROM leads
		WHERE created_at >= now() - interval '7 days'`,
	).Scan(&summary.AddedLast7Days); err != nil {
		return summary, fmt.Errorf("dashboard recent count: %w", err)
	}

	// --- 3. the 5 most recently created leads -------------------------------
	recentSQL := fmt.Sprintf(`
		SELECT %s
		FROM leads
		ORDER BY created_at DESC, id DESC
		LIMIT 5`, leadColumns)

	recentRows, err := s.query(ctx, recentSQL)
	if err != nil {
		return summary, fmt.Errorf("dashboard recent leads: %w", err)
	}
	defer recentRows.Close()

	summary.RecentLeads = make([]model.Lead, 0, 5)
	for recentRows.Next() {
		lead, err := scanLead(recentRows)
		if err != nil {
			return summary, fmt.Errorf("scan recent lead: %w", err)
		}
		summary.RecentLeads = append(summary.RecentLeads, lead)
	}
	if err := recentRows.Err(); err != nil {
		return summary, fmt.Errorf("iterate recent leads: %w", err)
	}
	if err := s.attachInsights(ctx, summary.RecentLeads); err != nil {
		return summary, err
	}

	// --- 4. follow-up tasks -------------------------------------------------
	//
	// "Today" is the database's CURRENT_DATE, so every client agrees on what is
	// overdue regardless of the clock on the machine viewing the dashboard.
	// Cancelled events are not work to do, so they count nowhere.
	if err := s.queryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE due_date <  CURRENT_DATE),
		       COUNT(*) FILTER (WHERE due_date =  CURRENT_DATE)
		FROM tasks
		WHERE `+openTask("")+`
		  AND due_date <= CURRENT_DATE
		  AND lead_id IN (SELECT id FROM leads)`,
	).Scan(&summary.OverdueTasks, &summary.DueTodayTasks); err != nil {
		return summary, fmt.Errorf("dashboard task counts: %w", err)
	}

	dueSQL := fmt.Sprintf(`
		SELECT %s, l.name, l.company
		FROM tasks t
		JOIN leads l ON l.id = t.lead_id
		WHERE %s
		  AND t.due_date <= CURRENT_DATE
		ORDER BY t.due_date, t.start_time NULLS FIRST, t.id
		LIMIT 10`, taskColumnsQualified, openTask("t."))

	dueTasks, err := s.tasksWithLeadNames(ctx, dueSQL)
	if err != nil {
		return summary, fmt.Errorf("dashboard due tasks: %w", err)
	}
	summary.DueTasks = dueTasks

	// Today's agenda: everything due today, done or not, in the order the day
	// runs -- timed tasks by start time, then the all-day ones. Completed tasks
	// stay in the list so the card can show progress through the day.
	todaySQL := fmt.Sprintf(`
		SELECT %s, l.name, l.company
		FROM tasks t
		JOIN leads l ON l.id = t.lead_id
		WHERE t.due_date = CURRENT_DATE
		  AND t.cancelled_at IS NULL
		ORDER BY t.start_time NULLS LAST, t.id
		LIMIT %d`, taskColumnsQualified, model.MaxTodayTasksListed)

	todayTasks, err := s.tasksWithLeadNames(ctx, todaySQL)
	if err != nil {
		return summary, fmt.Errorf("dashboard today's tasks: %w", err)
	}
	summary.TodayTasks = todayTasks

	// --- 5. open leads with no next step ------------------------------------
	//
	// NOT EXISTS against the partial index on open tasks: an open lead nobody
	// has planned a follow-up for is the one most likely to go cold.
	if err := s.queryRow(ctx, `
		SELECT COUNT(*)
		FROM leads l
		WHERE l.stage NOT IN ('won', 'lost')
		  AND NOT EXISTS (
		      SELECT 1 FROM tasks t
		      WHERE t.lead_id = l.id AND `+openTask("t.")+`
		  )`,
	).Scan(&summary.OpenLeadsWithoutTask); err != nil {
		return summary, fmt.Errorf("dashboard leads without task: %w", err)
	}

	// --- 6. lead health -----------------------------------------------------
	if err := s.dashboardHealth(ctx, &summary); err != nil {
		return summary, err
	}

	if err := s.dashboardStuck(ctx, &summary); err != nil {
		return summary, err
	}

	if err := s.dashboardHottest(ctx, &summary); err != nil {
		return summary, err
	}

	return summary, nil
}

// dashboardHottest counts Hot open leads and picks the five highest scores.
// Scores are worked out in Go, so this scores every open lead, as a score sort
// on the leads list does.
func (s *Store) dashboardHottest(ctx context.Context, summary *model.DashboardSummary) error {
	leads, err := s.loadScoredLeads(ctx, `WHERE stage NOT IN ('won', 'lost')`, nil, "id")
	if err != nil {
		return fmt.Errorf("dashboard hottest leads: %w", err)
	}

	summary.HotCount = len(model.FilterByBand(leads, model.BandHot))

	model.SortLeadsByScore(leads, "desc")
	// A fresh slice, never nil, so an empty list is [] in the JSON.
	summary.HottestLeads = make([]model.Lead, min(5, len(leads)))
	copy(summary.HottestLeads, leads)
	return nil
}

// dashboardHealth counts open leads by health and picks the five going cold
// fastest.
//
// The per-stage thresholds live in Go (model.Stage.Thresholds) and are passed
// in as three parallel arrays that unnest() turns back into a small table. That
// keeps one definition of "cooling" for both this query and the per-lead badge,
// instead of a copy of the numbers hard-coded into the SQL.
//
// "Furthest gone" is days without contact divided by the stage's cold
// threshold, so 6 silent days in Negotiation (cold at 7) outranks 10 in
// Qualified (cold at 14).
func (s *Store) dashboardHealth(ctx context.Context, summary *model.DashboardSummary) error {
	var (
		stages            []string
		coolingDays, cold []int32
	)
	for _, stage := range model.Stages {
		if t, ok := stage.Thresholds(); ok {
			stages = append(stages, string(stage))
			coolingDays = append(coolingDays, int32(t.CoolingDays))
			cold = append(cold, int32(t.ColdDays))
		}
	}

	sql := fmt.Sprintf(`
		WITH thresholds AS (
			SELECT * FROM unnest($1::text[], $2::int[], $3::int[])
			         AS t(stage, cooling_days, cold_days)
		),
		silence AS (
			SELECT l.id,
			       th.cooling_days,
			       th.cold_days,
			       floor(extract(epoch FROM now() - %s) / 86400)::int AS days
			FROM leads l
			JOIN thresholds th ON th.stage = l.stage::text
		)
		SELECT COUNT(*) FILTER (WHERE days >= cooling_days AND days < cold_days),
		       COUNT(*) FILTER (WHERE days >= cold_days),
		       COALESCE(
		           (array_agg(id ORDER BY days::float / cold_days DESC, id)
		                FILTER (WHERE days >= cooling_days))[1:5],
		           '{}')
		FROM silence`, lastTouchSQL(4))

	var ids []int64
	if err := s.queryRow(ctx, sql, stages, coolingDays, cold, touchTypes()).
		Scan(&summary.CoolingLeads, &summary.ColdLeads, &ids); err != nil {
		return fmt.Errorf("dashboard lead health: %w", err)
	}

	summary.GoingCold = make([]model.Lead, 0, len(ids))
	if len(ids) == 0 {
		return nil
	}

	// array_agg kept the ranking; re-read the leads and put them back in it.
	rows, err := s.query(ctx,
		fmt.Sprintf(`SELECT %s FROM leads WHERE id = ANY($1::bigint[])`, leadColumns), ids)
	if err != nil {
		return fmt.Errorf("dashboard going-cold leads: %w", err)
	}
	defer rows.Close()

	byID := make(map[int64]model.Lead, len(ids))
	for rows.Next() {
		lead, err := scanLead(rows)
		if err != nil {
			return fmt.Errorf("scan going-cold lead: %w", err)
		}
		byID[lead.ID] = lead
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate going-cold leads: %w", err)
	}

	for _, id := range ids {
		if lead, ok := byID[id]; ok {
			summary.GoingCold = append(summary.GoingCold, lead)
		}
	}

	return s.attachInsights(ctx, summary.GoingCold)
}

// dashboardStuck counts open deals stuck in their stage and picks the five
// furthest past their stage's limit: 20 days in Negotiation (limit 14) ranks
// above 12 in Proposal Sent (limit 10).
func (s *Store) dashboardStuck(ctx context.Context, summary *model.DashboardSummary) error {
	stages, days := model.StuckThresholds()

	sql := fmt.Sprintf(`
		WITH thresholds AS (
			SELECT * FROM unnest($1::text[], $2::int[]) AS t(stage, limit_days)
		),
		stint AS (
			SELECT l.id, th.limit_days,
			       floor(extract(epoch FROM now() - %s) / 86400)::int AS days
			FROM leads l
			JOIN thresholds th ON th.stage = l.stage::text
		)
		SELECT COUNT(*) FILTER (WHERE days >= limit_days),
		       COALESCE(
		           (array_agg(id ORDER BY days::float / limit_days DESC, id)
		                FILTER (WHERE days >= limit_days))[1:5],
		           '{}')
		FROM stint`, stageEnteredSQLFor("l"))

	var ids []int64
	if err := s.queryRow(ctx, sql, stages, days).Scan(&summary.StuckCount, &ids); err != nil {
		return fmt.Errorf("dashboard stuck deals: %w", err)
	}

	summary.StuckDeals = make([]model.Lead, 0, len(ids))
	if len(ids) == 0 {
		return nil
	}

	rows, err := s.query(ctx,
		fmt.Sprintf(`SELECT %s FROM leads WHERE id = ANY($1::bigint[])`, leadColumns), ids)
	if err != nil {
		return fmt.Errorf("dashboard stuck deal leads: %w", err)
	}
	defer rows.Close()

	byID := make(map[int64]model.Lead, len(ids))
	for rows.Next() {
		lead, err := scanLead(rows)
		if err != nil {
			return fmt.Errorf("scan stuck deal: %w", err)
		}
		byID[lead.ID] = lead
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate stuck deals: %w", err)
	}

	for _, id := range ids {
		if lead, ok := byID[id]; ok {
			summary.StuckDeals = append(summary.StuckDeals, lead)
		}
	}
	return s.attachInsights(ctx, summary.StuckDeals)
}
