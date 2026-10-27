package store

import (
	"context"
	"fmt"
	"sort"

	"crm-lead-management/backend/internal/model"
)

// Pipeline builds the board: one column per stage, in lifecycle order, each
// with its true totals and up to model.PipelineColumnLimit cards.
//
// One query does the grouping. Window functions partitioned by stage give every
// row its column's count and value alongside a per-column rank, so capping the
// cards does not cost the header its real totals.
func (s *Store) Pipeline(ctx context.Context) ([]model.PipelineColumn, error) {
	sql := fmt.Sprintf(`
		SELECT %s, column_count, column_value
		FROM (
			SELECT *,
			       COUNT(*)                     OVER w AS column_count,
			       COALESCE(SUM(deal_value) OVER w, 0) AS column_value,
			       ROW_NUMBER() OVER (PARTITION BY stage
			                          ORDER BY updated_at DESC, id DESC) AS card_rank
			FROM leads
			WINDOW w AS (PARTITION BY stage)
		) ranked
		WHERE card_rank <= $1`, leadColumns)

	rows, err := s.query(ctx, sql, model.PipelineColumnLimit)
	if err != nil {
		return nil, fmt.Errorf("pipeline: %w", err)
	}
	defer rows.Close()

	type totals struct {
		count int
		value int64
	}
	byStage := make(map[model.Stage]totals, len(model.Stages))
	leads := make([]model.Lead, 0, 64)

	for rows.Next() {
		var t totals
		lead, err := scanLead(rows, &t.count, &t.value)
		if err != nil {
			return nil, fmt.Errorf("scan pipeline lead: %w", err)
		}
		byStage[lead.Stage] = t
		leads = append(leads, lead)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pipeline: %w", err)
	}

	if err := s.attachInsights(ctx, leads); err != nil {
		return nil, err
	}

	// Iterate model.Stages, not the map, so columns come out in lifecycle order
	// and empty stages still get a column to drop cards into.
	columns := make([]model.PipelineColumn, 0, len(model.Stages))
	index := make(map[model.Stage]int, len(model.Stages))
	for _, stage := range model.Stages {
		t := byStage[stage]
		probability := stage.WinProbability()
		index[stage] = len(columns)
		columns = append(columns, model.PipelineColumn{
			Stage:       stage,
			Label:       stage.Label(),
			Probability: probability,
			Count:       t.count,
			Value:       t.value,
			Weighted:    t.value * int64(probability) / 100,
			Leads:       make([]model.Lead, 0, t.count),
		})
	}
	for _, lead := range leads {
		col := &columns[index[lead.Stage]]
		col.Leads = append(col.Leads, lead)
	}

	// Open columns: best score first, so the most promising deal is always at
	// the top. Ties (and closed columns, which have no score) fall back to the
	// most recently updated.
	for i := range columns {
		cards := columns[i].Leads
		sort.SliceStable(cards, func(a, b int) bool {
			sa, sb := scoreOf(cards[a]), scoreOf(cards[b])
			if sa != sb {
				return sa > sb
			}
			return cards[a].UpdatedAt.After(cards[b].UpdatedAt)
		})
	}

	return columns, nil
}

// scoreOf is a lead's score total, or -1 for an unscored (closed) lead.
func scoreOf(lead model.Lead) int {
	if lead.Score == nil {
		return -1
	}
	return lead.Score.Total
}
