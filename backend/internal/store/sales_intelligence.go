package store

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"crm-lead-management/backend/internal/model"
)

// SalesIntelligence aggregates lead scoring, funnel progression, and source ROI.
func (s *Store) SalesIntelligence(ctx context.Context) (model.SalesIntelligenceReport, error) {
	var report model.SalesIntelligenceReport

	// 1. Fetch Funnel Analytics
	funnel, err := s.funnelAnalytics(ctx)
	if err != nil {
		return report, fmt.Errorf("funnel analytics: %w", err)
	}
	report.Funnel = funnel

	// 2. Fetch Lead Scoring Intelligence
	scoring, err := s.scoringIntelligence(ctx)
	if err != nil {
		return report, fmt.Errorf("scoring intelligence: %w", err)
	}
	report.Scoring = scoring

	// 3. Fetch Lead Source ROI
	sourceROI, err := s.sourceROIReport(ctx)
	if err != nil {
		return report, fmt.Errorf("source roi report: %w", err)
	}
	report.SourceROI = sourceROI

	// 4. Build Executive Summary
	var (
		totalLeads     int
		wonCount       int
		lostCount      int
		openCount      int
		pipelineValue  int64
		wonValue       int64
		totalCycleDays float64
		wonWithCycle   int
	)

	// Summary query for lead status and cycle duration
	rows, err := s.query(ctx, `
		SELECT l.stage::text,
		       COUNT(*),
		       COALESCE(SUM(l.deal_value), 0),
		       COALESCE(AVG(EXTRACT(EPOCH FROM (h.changed_at - l.created_at)) / 86400) FILTER (WHERE l.stage = 'won'), 0)::float8,
		       COUNT(*) FILTER (WHERE l.stage = 'won' AND h.changed_at IS NOT NULL)
		FROM leads l
		LEFT JOIN (
			SELECT lead_id, MAX(changed_at) AS changed_at
			FROM lead_stage_history
			WHERE to_stage = 'won'
			GROUP BY lead_id
		) h ON h.lead_id = l.id
		GROUP BY l.stage`)
	if err != nil {
		return report, fmt.Errorf("summary counts: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			stage    string
			count    int
			val      int64
			avgCycle float64
			wonCycle int
		)
		if err := rows.Scan(&stage, &count, &val, &avgCycle, &wonCycle); err != nil {
			return report, fmt.Errorf("scan summary counts: %w", err)
		}
		totalLeads += count
		switch model.Stage(stage) {
		case model.StageWon:
			wonCount = count
			wonValue = val
			totalCycleDays = avgCycle
			wonWithCycle = wonCycle
		case model.StageLost:
			lostCount = count
		default:
			openCount += count
			pipelineValue += val
		}
	}
	if err := rows.Err(); err != nil {
		return report, fmt.Errorf("iterate summary counts: %w", err)
	}

	conversionRate := 0.0
	if closed := wonCount + lostCount; closed > 0 {
		conversionRate = roundTo1(float64(wonCount) / float64(closed) * 100)
	}

	avgDealSize := int64(0)
	if wonCount > 0 {
		avgDealSize = wonValue / int64(wonCount)
	}

	avgCycle := 0.0
	if wonWithCycle > 0 && totalCycleDays > 0 {
		avgCycle = roundTo1(totalCycleDays)
	} else if len(funnel.Stages) > 0 {
		// Sum of open stage durations as surrogate
		for _, st := range funnel.Stages {
			avgCycle += st.AvgDurationDays
		}
		avgCycle = roundTo1(avgCycle)
	}

	report.Summary = model.IntelligenceSummary{
		TotalLeads:             totalLeads,
		OpenLeads:              openCount,
		WonLeads:               wonCount,
		LostLeads:              lostCount,
		TotalPipelineValue:     pipelineValue,
		TotalWonRevenue:        wonValue,
		OverallConversionRate:  conversionRate,
		AverageDealSize:        avgDealSize,
		AverageSalesCycleDays:  avgCycle,
		AverageLeadScore:       scoring.AverageScore,
		OverallChannelROI:      sourceROI.OverallROI,
		OverallRevenueMultiple: sourceROI.OverallRevenueMulti,
	}

	return report, nil
}

// scoringIntelligence computes scoring breakdown, top leads, and at-risk deals.
func (s *Store) scoringIntelligence(ctx context.Context) (model.ScoringIntelligence, error) {
	var scoring model.ScoringIntelligence

	scoring.FactorWeights = []model.ScoringFactorRule{
		{Key: "stage", Label: "Pipeline Stage Progress", MaxPoints: 30, Description: "Reflects deal maturity: Contacted (8), Qualified (15), Proposal Sent (23), Negotiation (30)"},
		{Key: "recent_contact", Label: "Contact Recency", MaxPoints: 20, Description: "Full points for contact within stage threshold; partial for cooling; 0 for cold"},
		{Key: "engagement", Label: "30-Day Conversation Depth", MaxPoints: 15, Description: "Rewards multiple calls, emails, and meetings logged in the last 30 days"},
		{Key: "next_step", Label: "Next Step Planned", MaxPoints: 10, Description: "Awards 10 points when an active follow-up task is scheduled"},
		{Key: "deal_value", Label: "Estimated Deal Value", MaxPoints: 10, Description: "Awards 10 points when deal size has been quantified"},
		{Key: "source", Label: "Source Propensity", MaxPoints: 10, Description: "Rewards high-converting channels: Referral (10), Website (7), LinkedIn (5), Other (3), Cold Call (2)"},
		{Key: "phone", Label: "Direct Phone on File", MaxPoints: 3, Description: "Direct phone contact captured for multi-channel outreach"},
		{Key: "notes", Label: "Context Notes Logged", MaxPoints: 2, Description: "Discovery and qualification notes recorded on file"},
	}

	// Fetch all open leads
	leads, err := s.fetchOpenLeads(ctx)
	if err != nil {
		return scoring, err
	}

	if err := s.attachInsights(ctx, leads); err != nil {
		return scoring, err
	}

	if len(leads) == 0 {
		return scoring, nil
	}

	totalScore := 0
	scoredCount := 0

	for _, lead := range leads {
		if lead.Score == nil {
			continue
		}
		score := lead.Score.Total
		totalScore += score
		scoredCount++

		val := int64(0)
		if lead.DealValue != nil {
			val = *lead.DealValue
		}

		switch lead.Score.Band {
		case model.BandHot:
			scoring.HotCount++
			scoring.HotValue += val
		case model.BandWarm:
			scoring.WarmCount++
			scoring.WarmValue += val
		case model.BandCold:
			scoring.ColdCount++
			scoring.ColdValue += val
		}

		// Check for at-risk deals: high value (>₹400,000) or late stage that is cooling/cold/stuck/penalized
		isLateStage := lead.Stage == model.StageProposalSent || lead.Stage == model.StageNegotiation
		isAtRisk := lead.Health == model.HealthCooling || lead.Health == model.HealthCold || lead.Stuck || len(lead.Score.Penalties) > 0
		if (val >= 400000 || isLateStage) && isAtRisk {
			scoring.AtRiskDeals = append(scoring.AtRiskDeals, lead)
		}
	}

	if scoredCount > 0 {
		scoring.AverageScore = roundTo1(float64(totalScore) / float64(scoredCount))
	}

	// Calculate band breakdown percentages
	totalOpen := len(leads)
	scoring.BandBreakdown = []model.ScoreBandBreakdown{
		{
			Band:               model.BandHot,
			Label:              "Hot (70-100)",
			Count:              scoring.HotCount,
			Percentage:         roundTo1(float64(scoring.HotCount) / float64(totalOpen) * 100),
			TotalValue:         scoring.HotValue,
			AverageProbability: 80,
		},
		{
			Band:               model.BandWarm,
			Label:              "Warm (40-69)",
			Count:              scoring.WarmCount,
			Percentage:         roundTo1(float64(scoring.WarmCount) / float64(totalOpen) * 100),
			TotalValue:         scoring.WarmValue,
			AverageProbability: 45,
		},
		{
			Band:               model.BandCold,
			Label:              "Cold (0-39)",
			Count:              scoring.ColdCount,
			Percentage:         roundTo1(float64(scoring.ColdCount) / float64(totalOpen) * 100),
			TotalValue:         scoring.ColdValue,
			AverageProbability: 15,
		},
	}

	// Sort leads by score descending, then deal value descending
	sort.Slice(leads, func(i, j int) bool {
		scoreI := 0
		if leads[i].Score != nil {
			scoreI = leads[i].Score.Total
		}
		scoreJ := 0
		if leads[j].Score != nil {
			scoreJ = leads[j].Score.Total
		}
		if scoreI != scoreJ {
			return scoreI > scoreJ
		}
		valI := int64(0)
		if leads[i].DealValue != nil {
			valI = *leads[i].DealValue
		}
		valJ := int64(0)
		if leads[j].DealValue != nil {
			valJ = *leads[j].DealValue
		}
		return valI > valJ
	})

	limit := 8
	if len(leads) < limit {
		limit = len(leads)
	}
	scoring.TopLeads = leads[:limit]

	return scoring, nil
}

// fetchOpenLeads queries all leads that are not Won or Lost.
func (s *Store) fetchOpenLeads(ctx context.Context) ([]model.Lead, error) {
	rows, err := s.query(ctx, `
		SELECT id, name, company, email, phone, source::text, stage::text,
		       notes, deal_value, owner_id, close_reason, close_note,
		       created_at, updated_at
		FROM leads
		WHERE stage NOT IN ('won', 'lost')
		ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("fetch open leads: %w", err)
	}
	defer rows.Close()

	var leads []model.Lead
	for rows.Next() {
		var (
			l           model.Lead
			sourceStr   string
			stageStr    string
			ownerID     *int64
			closeReason *string
			closeNote   *string
		)
		if err := rows.Scan(
			&l.ID, &l.Name, &l.Company, &l.Email, &l.Phone, &sourceStr, &stageStr,
			&l.Notes, &l.DealValue, &ownerID, &closeReason, &closeNote,
			&l.CreatedAt, &l.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan open lead: %w", err)
		}
		l.Source = model.Source(sourceStr)
		l.Stage = model.Stage(stageStr)
		l.OwnerID = ownerID
		if closeReason != nil {
			cr := model.CloseReason(*closeReason)
			l.CloseReason = &cr
		}
		l.CloseNote = closeNote
		leads = append(leads, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate open leads: %w", err)
	}
	return leads, nil
}

// funnelAnalytics analyzes conversion rates, drop-offs, and durations through stages.
func (s *Store) funnelAnalytics(ctx context.Context) (model.FunnelAnalytics, error) {
	var funnel model.FunnelAnalytics

	// Stages that make up the active progressive pipeline
	funnelStages := []model.Stage{
		model.StageNew,
		model.StageContacted,
		model.StageQualified,
		model.StageProposalSent,
		model.StageNegotiation,
		model.StageWon,
	}

	// 1. Count distinct leads that entered each stage
	enteredRows, err := s.query(ctx, `
		SELECT to_stage::text, COUNT(DISTINCT lead_id)
		FROM lead_stage_history
		WHERE lead_id IN (SELECT id FROM leads)
		GROUP BY to_stage`)
	if err != nil {
		return funnel, fmt.Errorf("funnel entered counts: %w", err)
	}
	defer enteredRows.Close()

	enteredMap := make(map[model.Stage]int)
	for enteredRows.Next() {
		var (
			stage string
			count int
		)
		if err := enteredRows.Scan(&stage, &count); err != nil {
			return funnel, fmt.Errorf("scan entered: %w", err)
		}
		enteredMap[model.Stage(stage)] = count
	}

	// 2. Count current leads and values per stage
	currentRows, err := s.query(ctx, `
		SELECT stage::text, COUNT(*), COALESCE(SUM(deal_value), 0)
		FROM leads
		GROUP BY stage`)
	if err != nil {
		return funnel, fmt.Errorf("funnel current counts: %w", err)
	}
	defer currentRows.Close()

	currentMap := make(map[model.Stage]struct {
		count int
		value int64
	})
	for currentRows.Next() {
		var (
			stage string
			count int
			value int64
		)
		if err := currentRows.Scan(&stage, &count, &value); err != nil {
			return funnel, fmt.Errorf("scan current: %w", err)
		}
		currentMap[model.Stage(stage)] = struct {
			count int
			value int64
		}{count: count, value: value}
	}

	// 3. Get stage durations
	durations, err := s.stageDurations(ctx)
	if err != nil {
		return funnel, fmt.Errorf("funnel stage durations: %w", err)
	}
	durationMap := make(map[model.Stage]float64)
	for _, d := range durations {
		if d.AvgDays != nil {
			durationMap[d.Stage] = *d.AvgDays
		}
	}

	topFunnel := enteredMap[model.StageNew]
	if topFunnel == 0 {
		// Fallback to total leads count if history is sparse
		topFunnel = currentMap[model.StageNew].count
	}
	funnel.TotalFunnelEntries = topFunnel

	maxDropOffRate := 0.0
	bottleneckStage := ""
	longestDuration := 0.0
	slowestStage := ""

	var previousEntered int
	for i, stage := range funnelStages {
		entered := enteredMap[stage]
		curr := currentMap[stage]

		// Ensure entered is at least current count
		if curr.count > entered {
			entered = curr.count
		}

		convRate := 100.0
		if i > 0 && previousEntered > 0 {
			convRate = roundTo1(float64(entered) / float64(previousEntered) * 100)
			if convRate > 100.0 {
				convRate = 100.0
			}
		}

		cumConv := 100.0
		if topFunnel > 0 {
			cumConv = roundTo1(float64(entered) / float64(topFunnel) * 100)
			if cumConv > 100.0 {
				cumConv = 100.0
			}
		}

		dropOffCount := 0
		dropOffRate := 0.0
		if i < len(funnelStages)-1 && entered > 0 {
			nextStage := funnelStages[i+1]
			nextEntered := enteredMap[nextStage]
			if entered > nextEntered {
				dropOffCount = entered - nextEntered - curr.count
				if dropOffCount < 0 {
					dropOffCount = 0
				}
				dropOffRate = roundTo1(float64(dropOffCount) / float64(entered) * 100)
			}
		}

		avgDuration := durationMap[stage]
		if avgDuration > longestDuration && stage != model.StageWon {
			longestDuration = avgDuration
			slowestStage = stage.Label()
		}

		if dropOffRate > maxDropOffRate {
			maxDropOffRate = dropOffRate
			bottleneckStage = stage.Label()
		}

		funnel.Stages = append(funnel.Stages, model.FunnelStageMetric{
			Stage:                stage,
			Label:                stage.Label(),
			EnteredCount:         entered,
			CurrentCount:         curr.count,
			CurrentValue:         curr.value,
			ConversionRate:       convRate,
			CumulativeConversion: cumConv,
			DropOffCount:         dropOffCount,
			DropOffRate:          dropOffRate,
			AvgDurationDays:      avgDuration,
		})

		previousEntered = entered
	}

	if topFunnel > 0 {
		wonEntered := enteredMap[model.StageWon]
		funnel.OverallConversionRate = roundTo1(float64(wonEntered) / float64(topFunnel) * 100)
	}

	if bottleneckStage != "" {
		funnel.BottleneckStage = bottleneckStage
		funnel.BottleneckReason = fmt.Sprintf("High deal attrition: %.1f%% of prospects drop out after %s.", maxDropOffRate, bottleneckStage)
		funnel.BottleneckRecommendation = fmt.Sprintf("Audit prospect objections during %s. Streamline approval workflows and standardize qualification criteria to protect deal velocity.", bottleneckStage)
	} else if slowestStage != "" {
		funnel.BottleneckStage = slowestStage
		funnel.BottleneckReason = fmt.Sprintf("Pipeline friction: deals spend an average of %.1f days in %s.", longestDuration, slowestStage)
		funnel.BottleneckRecommendation = fmt.Sprintf("Introduce fast-track templates and automated reminders for deals entering %s to accelerate time-to-close.", slowestStage)
	} else {
		funnel.BottleneckStage = "Proposal Sent"
		funnel.BottleneckReason = "Deals typically require scope revisions and security reviews."
		funnel.BottleneckRecommendation = "Standardize proposal templates with pre-approved pricing tiers to compress review cycles."
	}

	return funnel, nil
}

// sourceROIReport computes revenue, CAC, net margin, and ROI multiples across lead sources.
func (s *Store) sourceROIReport(ctx context.Context) (model.SourceROIReport, error) {
	var report model.SourceROIReport

	// Industry-standard benchmark Acquisition Cost Per Lead (in whole INR)
	benchmarkCPL := map[model.Source]int64{
		model.SourceWebsite:   2500, // SEO / Google Ads / Content
		model.SourceReferral:  1000, // Partner reward / Client referral program
		model.SourceLinkedIn:  3500, // Sponsored InMail / ABM ads
		model.SourceColdCall:  1800, // SDR dialer time & verified B2B phone data
		model.SourceMetaAds:   900,  // Facebook / Instagram lead form ads
		model.SourceGoogleAds: 1400, // Google Ads lead form extensions
		model.SourceOther:     1500, // Events / PR / Directories
	}

	type srcAgg struct {
		leads    int
		won      int
		lost     int
		wonValue int64
		pipeVal  int64
	}

	rows, err := s.query(ctx, `
		SELECT source::text,
		       COUNT(*),
		       COUNT(*) FILTER (WHERE stage = 'won'),
		       COUNT(*) FILTER (WHERE stage = 'lost'),
		       COALESCE(SUM(deal_value) FILTER (WHERE stage = 'won'), 0),
		       COALESCE(SUM(deal_value) FILTER (WHERE stage NOT IN ('won', 'lost')), 0)
		FROM leads
		GROUP BY source`)
	if err != nil {
		return report, fmt.Errorf("source roi query: %w", err)
	}
	defer rows.Close()

	found := make(map[model.Source]srcAgg)
	totalLeads := 0

	for rows.Next() {
		var (
			source string
			a      srcAgg
		)
		if err := rows.Scan(&source, &a.leads, &a.won, &a.lost, &a.wonValue, &a.pipeVal); err != nil {
			return report, fmt.Errorf("scan source roi: %w", err)
		}
		found[model.Source(source)] = a
		totalLeads += a.leads
	}

	var (
		totalCost    int64
		totalWonVal  int64
		bestROI      = -99999.0
		topSource    = ""
		lowestCAC    = int64(math.MaxInt64)
		lowestSource = ""
	)

	for _, source := range model.Sources {
		a := found[source]
		cpl := benchmarkCPL[source]
		cost := int64(a.leads) * cpl
		netProfit := a.wonValue - cost

		winRate := 0.0
		if closed := a.won + a.lost; closed > 0 {
			winRate = roundTo1(float64(a.won) / float64(closed) * 100)
		}

		roi := 0.0
		revenueMultiple := 0.0
		if cost > 0 {
			roi = roundTo1(float64(netProfit) / float64(cost) * 100)
			revenueMultiple = roundTo1(float64(a.wonValue) / float64(cost))
		}

		cac := int64(0)
		if a.won > 0 {
			cac = cost / int64(a.won)
			if cac < lowestCAC {
				lowestCAC = cac
				lowestSource = source.Label()
			}
		}

		share := 0.0
		if totalLeads > 0 {
			share = roundTo1(float64(a.leads) / float64(totalLeads) * 100)
		}

		avgDeal := int64(0)
		if a.won > 0 {
			avgDeal = a.wonValue / int64(a.won)
		}

		tier := "Nurture Channel"
		if revenueMultiple >= 8.0 || roi >= 700 {
			tier = "Tier 1: Exceptional (High ROI)"
		} else if revenueMultiple >= 3.0 || roi >= 200 {
			tier = "Tier 2: Strong Margin"
		} else if a.won > 0 {
			tier = "Tier 3: Moderate Efficiency"
		}

		if a.won > 0 && roi > bestROI {
			bestROI = roi
			topSource = source.Label()
		}

		totalCost += cost
		totalWonVal += a.wonValue

		report.Sources = append(report.Sources, model.SourceROIMetric{
			Source:               source,
			Label:                source.Label(),
			Leads:                a.leads,
			ShareOfLeads:         share,
			Won:                  a.won,
			Lost:                 a.lost,
			Open:                 a.leads - (a.won + a.lost),
			WinRate:              winRate,
			WonValue:             a.wonValue,
			PipelineValue:        a.pipeVal,
			AvgDealSize:          avgDeal,
			BenchmarkCostPerLead: cpl,
			TotalAcquisitionCost: cost,
			NetProfit:            netProfit,
			ROI:                  roi,
			RevenueMultiple:      revenueMultiple,
			CAC:                  cac,
			EfficiencyTier:       tier,
		})
	}

	report.TotalAcquisitionCost = totalCost
	report.TotalWonRevenue = totalWonVal
	report.TotalNetProfit = totalWonVal - totalCost
	if totalCost > 0 {
		report.OverallROI = roundTo1(float64(report.TotalNetProfit) / float64(totalCost) * 100)
		report.OverallRevenueMulti = roundTo1(float64(totalWonVal) / float64(totalCost))
	}
	report.TopPerformingSource = topSource
	report.LowestCACSource = lowestSource

	return report, nil
}

// SeedIntelligenceData executes db/intelligence_seed.sql to populate sample intelligence data.
func (s *Store) SeedIntelligenceData(ctx context.Context) error {
	// Look for db/intelligence_seed.sql relative to current dir or executable
	candidates := []string{
		"db/intelligence_seed.sql",
		"../db/intelligence_seed.sql",
		"../../db/intelligence_seed.sql",
	}

	var sqlBytes []byte
	var readErr error

	for _, path := range candidates {
		sqlBytes, readErr = os.ReadFile(path)
		if readErr == nil {
			break
		}
	}

	if readErr != nil {
		// Try finding relative to executable
		if exe, err := os.Executable(); err == nil {
			dir := filepath.Dir(exe)
			for _, rel := range []string{"db/intelligence_seed.sql", "../db/intelligence_seed.sql", "../../db/intelligence_seed.sql"} {
				p := filepath.Join(dir, rel)
				sqlBytes, readErr = os.ReadFile(p)
				if readErr == nil {
					break
				}
			}
		}
	}

	if len(sqlBytes) == 0 {
		return fmt.Errorf("could not locate db/intelligence_seed.sql: %w", readErr)
	}

	sqlText := string(sqlBytes)

	// Strip BEGIN and COMMIT if present since Exec will handle transactions
	cleanSQL := strings.TrimSpace(sqlText)

	_, err := s.pool.Exec(ctx, cleanSQL)
	if err != nil {
		return fmt.Errorf("execute intelligence seed: %w", err)
	}
	return nil
}
