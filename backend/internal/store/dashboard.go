package store

import (
	"context"
	"fmt"
	"math"

	"crm-lead-management/backend/internal/model"
)

// DashboardSummary builds the aggregate view for GET /api/dashboard.
//
// Three queries, each doing its aggregation in the database rather than pulling
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
	rows, err := s.pool.Query(ctx, `
		SELECT stage::text, COUNT(*)
		FROM leads
		GROUP BY stage`)
	if err != nil {
		return summary, fmt.Errorf("dashboard stage counts: %w", err)
	}
	defer rows.Close()

	counts := make(map[model.Stage]int, len(model.Stages))
	for rows.Next() {
		var (
			stage string
			count int
		)
		if err := rows.Scan(&stage, &count); err != nil {
			return summary, fmt.Errorf("scan stage count: %w", err)
		}
		counts[model.Stage(stage)] = count
	}
	if err := rows.Err(); err != nil {
		return summary, fmt.Errorf("iterate stage counts: %w", err)
	}

	// Iterating model.Stages (not the map) guarantees the response is always in
	// lifecycle order. Map iteration order in Go is deliberately randomised, so
	// ranging over `counts` here would shuffle the chart on every request.
	summary.ByStage = make([]model.StageCount, 0, len(model.Stages))
	for _, stage := range model.Stages {
		count := counts[stage] // missing key yields 0, which is exactly what we want
		summary.ByStage = append(summary.ByStage, model.StageCount{
			Stage: stage,
			Label: stage.Label(),
			Count: count,
		})

		summary.TotalLeads += count
		switch stage {
		case model.StageWon:
			summary.WonCount = count
		case model.StageLost:
			summary.LostCount = count
		default:
			summary.OpenCount += count
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
	if err := s.pool.QueryRow(ctx, `
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

	recentRows, err := s.pool.Query(ctx, recentSQL)
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

	return summary, nil
}
