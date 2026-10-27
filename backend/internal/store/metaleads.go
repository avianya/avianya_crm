package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"crm-lead-management/backend/internal/model"
)

// The Meta Leads dashboard: leads that came from Meta, filtered, with totals
// worked out by the database over every match (not just the page shown).

// metaLeadsFrom is the dashboard's row source. latest is each lead's most
// recent conversion event, for the CAPI status filter and column.
const metaLeadsFrom = `
	FROM leads l
	JOIN meta_lead_attribution m ON m.lead_id = l.id
	LEFT JOIN LATERAL (
		SELECT status FROM capi_events c
		WHERE c.lead_id = l.id
		ORDER BY c.created_at DESC, c.id DESC
		LIMIT 1
	) latest ON true`

// metaSubmittedAt is when the person filled in the form: Meta's own time when
// known, else when the CRM received it.
const metaSubmittedAt = `COALESCE(m.meta_created_at, m.created_at)`

// metaEffectiveScore is the quality score that counts: an override beats the
// rules' score.
const metaEffectiveScore = `COALESCE(l.quality_override, l.quality_score)`

// metaLeadsWhere turns the filters into a WHERE clause and its arguments.
// Every value is a bind parameter; nothing from the query string is spliced in.
func metaLeadsWhere(q model.MetaLeadQuery) (string, []any) {
	var (
		conds []string
		args  []any
	)
	add := func(cond string, value any) {
		args = append(args, value)
		conds = append(conds, fmt.Sprintf(cond, len(args)))
	}
	if q.From != nil {
		add(metaSubmittedAt+" >= $%d", *q.From)
	}
	if q.To != nil {
		add(metaSubmittedAt+" < $%d", *q.To)
	}
	if q.AdAccountID != "" {
		add("m.ad_account_id = $%d", q.AdAccountID)
	}
	if q.PageID != "" {
		add("m.page_id = $%d", q.PageID)
	}
	if q.FormID != "" {
		add("m.form_id = $%d", q.FormID)
	}
	if q.CampaignID != "" {
		add("m.campaign_id = $%d", q.CampaignID)
	}
	if q.AdID != "" {
		add("m.ad_id = $%d", q.AdID)
	}
	if q.Source != "" {
		add("l.source = $%d::lead_source", string(q.Source))
	}
	if q.Quality != "" {
		add("l.quality = $%d", string(q.Quality))
	}
	if q.ScoreMin != nil {
		add(metaEffectiveScore+" >= $%d", *q.ScoreMin)
	}
	if q.ScoreMax != nil {
		add(metaEffectiveScore+" <= $%d", *q.ScoreMax)
	}
	if q.Stage != "" {
		add("l.stage = $%d::lead_stage", string(q.Stage))
	}
	// Fixed SQL per value: q.Qualification was checked against the allow-list.
	switch q.Qualification {
	case model.QualificationQualified:
		conds = append(conds, "l.stage IN ('qualified', 'proposal_sent', 'negotiation', 'won')")
	case model.QualificationNotQualified:
		conds = append(conds, "l.stage = 'lost'")
	case model.QualificationPending:
		conds = append(conds, "l.stage IN ('new', 'contacted')")
	}
	if q.Owner.OwnerID != nil {
		add("l.owner_id = $%d", *q.Owner.OwnerID)
	} else if q.Owner.Unowned {
		conds = append(conds, "l.owner_id IS NULL")
	}
	switch q.CAPIStatus {
	case "":
	case "none":
		conds = append(conds, "latest.status IS NULL")
	default:
		add("latest.status = $%d", q.CAPIStatus)
	}
	if len(conds) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(conds, " AND "), args
}

// MetaLeads returns the dashboard: totals over every match, one page of rows
// (newest submission first) and the filter dropdowns' choices.
func (s *Store) MetaLeads(ctx context.Context, q model.MetaLeadQuery) (model.MetaLeadsReport, int, error) {
	where, args := metaLeadsWhere(q)

	var report model.MetaLeadsReport
	sum := &report.Summary
	err := s.queryRow(ctx, `
		SELECT COUNT(*),
		       COUNT(*) FILTER (WHERE l.stage IN ('qualified', 'proposal_sent', 'negotiation', 'won')),
		       COUNT(*) FILTER (WHERE l.stage = 'won'),
		       COUNT(*) FILTER (WHERE l.stage = 'lost')
		`+metaLeadsFrom+` `+where, args...).
		Scan(&sum.Total, &sum.Qualified, &sum.Converted, &sum.Unqualified)
	if err != nil {
		return report, 0, fmt.Errorf("meta leads summary: %w", err)
	}
	if sum.Total > 0 {
		rate := float64(sum.Converted) * 100 / float64(sum.Total)
		sum.ConversionRate = &rate
	}

	pageArgs := append(append([]any{}, args...), q.PageSize, q.Offset())
	rows, err := s.query(ctx, fmt.Sprintf(`
		SELECT %s, %s, latest.status
		%s %s
		ORDER BY %s DESC, l.id DESC
		LIMIT $%d OFFSET $%d`,
		leadColumnsQualified, attributionColumns, metaLeadsFrom, where, metaSubmittedAt,
		len(args)+1, len(args)+2), pageArgs...)
	if err != nil {
		return report, 0, fmt.Errorf("meta leads: %w", err)
	}
	report.Leads, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.MetaLeadRow, error) {
		var (
			r      model.MetaLeadRow
			status *string
		)
		lead, err := scanLead(row, append(attributionDest(&r.Attribution), &status)...)
		if err != nil {
			return r, err
		}
		r.Lead = lead
		if status != nil {
			r.CAPIStatus = model.CAPIStatus(*status)
		}
		return r, nil
	})
	if err != nil {
		return report, 0, fmt.Errorf("scan meta leads: %w", err)
	}
	if report.Leads == nil {
		report.Leads = []model.MetaLeadRow{}
	}

	// Health and the pipeline score, as on every other list of leads.
	leads := make([]model.Lead, len(report.Leads))
	for i, r := range report.Leads {
		leads[i] = r.Lead
	}
	if err := s.attachInsights(ctx, leads); err != nil {
		return report, 0, err
	}
	for i := range report.Leads {
		report.Leads[i].Lead = leads[i]
		lead := leads[i]
		if lead.QualityOverride != nil {
			report.Leads[i].EffectiveScore = lead.QualityOverride
		} else {
			report.Leads[i].EffectiveScore = lead.QualityScore
		}
	}

	if report.Options, err = s.metaFilterOptions(ctx); err != nil {
		return report, 0, err
	}
	return report, sum.Total, nil
}

// metaFilterOptions lists every ad account, page, form, campaign and ad that
// has brought in a lead, with how many, for the dashboard's dropdowns. Names
// where Meta gave one, else the id.
func (s *Store) metaFilterOptions(ctx context.Context) (model.MetaFilterOptions, error) {
	var opts model.MetaFilterOptions
	for _, f := range []struct {
		dst      *[]model.MetaFilterOption
		id, name string
	}{
		{&opts.AdAccounts, "ad_account_id", "''"},
		{&opts.Pages, "page_id", "''"},
		{&opts.Forms, "form_id", "''"},
		{&opts.Campaigns, "campaign_id", "campaign_name"},
		{&opts.Ads, "ad_id", "ad_name"},
	} {
		// The column names come from the fixed list above, never from input.
		rows, err := s.query(ctx, fmt.Sprintf(`
			SELECT %[1]s, MAX(%[2]s), COUNT(*)
			FROM meta_lead_attribution
			WHERE %[1]s <> '' AND lead_id IN (SELECT id FROM leads)
			GROUP BY %[1]s
			ORDER BY COUNT(*) DESC, %[1]s
			LIMIT 200`, f.id, f.name))
		if err != nil {
			return opts, fmt.Errorf("meta filter options: %w", err)
		}
		*f.dst, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.MetaFilterOption, error) {
			var o model.MetaFilterOption
			var name string
			err := row.Scan(&o.Value, &name, &o.Count)
			o.Label = name
			if o.Label == "" {
				o.Label = o.Value
			}
			return o, err
		})
		if err != nil {
			return opts, fmt.Errorf("scan meta filter options: %w", err)
		}
		if *f.dst == nil {
			*f.dst = []model.MetaFilterOption{}
		}
	}
	return opts, nil
}
