package store

import (
	"context"
	"fmt"
	"math"

	"crm-lead-management/backend/internal/model"
)

// Reports builds the trends for GET /api/reports.
func (s *Store) Reports(ctx context.Context) (model.ReportSummary, error) {
	var (
		report model.ReportSummary
		err    error
	)

	if report.LeadsPerWeek, err = s.leadsPerWeek(ctx); err != nil {
		return report, err
	}
	if report.BySource, err = s.sourceReport(ctx); err != nil {
		return report, err
	}
	if report.StageDurations, err = s.stageDurations(ctx); err != nil {
		return report, err
	}
	if report.LossReasons, err = s.closeReasons(ctx, model.StageLost); err != nil {
		return report, err
	}
	if report.WinReasons, err = s.closeReasons(ctx, model.StageWon); err != nil {
		return report, err
	}
	return report, nil
}

// closeReasons counts the leads now in a closed stage by why they closed,
// most deals first (then most value). Deals with no reason are one row of
// their own. Never nil.
func (s *Store) closeReasons(ctx context.Context, stage model.Stage) ([]model.ReasonCount, error) {
	rows, err := s.query(ctx, `
		SELECT close_reason, COUNT(*), COALESCE(SUM(deal_value), 0)::bigint
		FROM leads
		WHERE stage = $1::lead_stage
		GROUP BY close_reason
		ORDER BY COUNT(*) DESC, 3 DESC, close_reason NULLS LAST`, string(stage))
	if err != nil {
		return nil, fmt.Errorf("%s reasons: %w", stage, err)
	}
	defer rows.Close()

	reasons := []model.ReasonCount{}
	for rows.Next() {
		var r model.ReasonCount
		if err := rows.Scan(&r.Reason, &r.Count, &r.Value); err != nil {
			return nil, fmt.Errorf("scan %s reason: %w", stage, err)
		}
		r.Label = model.ReasonCountLabel(stage, r.Reason)
		reasons = append(reasons, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s reasons: %w", stage, err)
	}
	return reasons, nil
}

// leadsPerWeek counts new leads in each of the last model.ReportWeeks weeks.
//
// generate_series produces every week whether or not anything happened in it,
// and the LEFT JOIN keeps the empty ones -- a chart that silently skips quiet
// weeks makes a slow month look like steady growth.
func (s *Store) leadsPerWeek(ctx context.Context) ([]model.WeekCount, error) {
	rows, err := s.query(ctx, `
		SELECT week::date::text, COUNT(l.id)
		FROM generate_series(
		         date_trunc('week', now()) - ($1 - 1) * interval '1 week',
		         date_trunc('week', now()),
		         interval '1 week') AS week
		LEFT JOIN leads l
		       ON l.created_at >= week
		      AND l.created_at <  week + interval '1 week'
		GROUP BY week
		ORDER BY week`, model.ReportWeeks)
	if err != nil {
		return nil, fmt.Errorf("leads per week: %w", err)
	}
	defer rows.Close()

	weeks := make([]model.WeekCount, 0, model.ReportWeeks)
	for rows.Next() {
		var w model.WeekCount
		if err := rows.Scan(&w.WeekStart, &w.Count); err != nil {
			return nil, fmt.Errorf("scan week: %w", err)
		}
		weeks = append(weeks, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate weeks: %w", err)
	}
	return weeks, nil
}

// sourceReport is lead count, closes and win rate for every source.
func (s *Store) sourceReport(ctx context.Context) ([]model.SourceReport, error) {
	rows, err := s.query(ctx, `
		SELECT source::text,
		       COUNT(*),
		       COUNT(*) FILTER (WHERE stage = 'won'),
		       COUNT(*) FILTER (WHERE stage = 'lost'),
		       COALESCE(SUM(deal_value) FILTER (WHERE stage = 'won'), 0)
		FROM leads
		GROUP BY source`)
	if err != nil {
		return nil, fmt.Errorf("source report: %w", err)
	}
	defer rows.Close()

	found := make(map[model.Source]model.SourceReport, len(model.Sources))
	for rows.Next() {
		var (
			source string
			r      model.SourceReport
		)
		if err := rows.Scan(&source, &r.Leads, &r.Won, &r.Lost, &r.WonValue); err != nil {
			return nil, fmt.Errorf("scan source report: %w", err)
		}
		found[model.Source(source)] = r
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate source report: %w", err)
	}

	// Every source, in dropdown order, zeros included.
	report := make([]model.SourceReport, 0, len(model.Sources))
	for _, source := range model.Sources {
		r := found[source]
		r.Source, r.Label = source, source.Label()
		if closed := r.Won + r.Lost; closed > 0 {
			rate := roundTo1(float64(r.Won) / float64(closed) * 100)
			r.WinRate = &rate
		}
		report = append(report, r)
	}
	return report, nil
}

// stageDurations works out how long leads stay in each open stage, from
// lead_stage_history.
//
// Each history row starts a stint in to_stage; the lead's next row ends it.
// Only finished stints count -- a lead that entered Negotiation yesterday has
// not "spent one day" there, it is still going -- and time in Won or Lost is
// never counted, since that is just time since closing.
func (s *Store) stageDurations(ctx context.Context) ([]model.StageDuration, error) {
	rows, err := s.query(ctx, `
		SELECT to_stage::text,
		       AVG(EXTRACT(EPOCH FROM (left_at - changed_at)) / 86400)::float8,
		       COUNT(*)
		FROM (
			SELECT to_stage, changed_at,
			       LEAD(changed_at) OVER (PARTITION BY lead_id ORDER BY changed_at, id) AS left_at
			FROM lead_stage_history
			WHERE lead_id IN (SELECT id FROM leads)
		) stints
		WHERE left_at IS NOT NULL
		  AND to_stage NOT IN ('won', 'lost')
		GROUP BY to_stage`)
	if err != nil {
		return nil, fmt.Errorf("stage durations: %w", err)
	}
	defer rows.Close()

	type totals struct {
		avgDays float64
		samples int
	}
	byStage := make(map[model.Stage]totals, len(model.Stages))
	for rows.Next() {
		var (
			stage string
			t     totals
		)
		if err := rows.Scan(&stage, &t.avgDays, &t.samples); err != nil {
			return nil, fmt.Errorf("scan stage duration: %w", err)
		}
		byStage[model.Stage(stage)] = t
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate stage durations: %w", err)
	}

	// Every open stage, in lifecycle order, including ones with no data yet.
	durations := make([]model.StageDuration, 0, len(model.Stages))
	for _, stage := range model.Stages {
		if stage.IsClosed() {
			continue
		}
		t := byStage[stage]
		d := model.StageDuration{Stage: stage, Label: stage.Label(), Samples: t.samples}
		if t.samples > 0 {
			avg := roundTo1(t.avgDays)
			d.AvgDays = &avg
		}
		durations = append(durations, d)
	}
	return durations, nil
}

func roundTo1(v float64) float64 { return math.Round(v*10) / 10 }
