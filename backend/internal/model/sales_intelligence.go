package model

// SalesIntelligenceReport brings together Lead Scoring, Funnel Analytics,
// and Lead Source ROI into one unified intelligence response.
type SalesIntelligenceReport struct {
	Summary   IntelligenceSummary `json:"summary"`
	Scoring   ScoringIntelligence `json:"scoring"`
	Funnel    FunnelAnalytics     `json:"funnel"`
	SourceROI SourceROIReport     `json:"source_roi"`
}

// IntelligenceSummary gives executive-level KPIs across the sales pipeline.
type IntelligenceSummary struct {
	TotalLeads             int     `json:"total_leads"`
	OpenLeads              int     `json:"open_leads"`
	WonLeads               int     `json:"won_leads"`
	LostLeads              int     `json:"lost_leads"`
	TotalPipelineValue     int64   `json:"total_pipeline_value"`
	TotalWonRevenue        int64   `json:"total_won_revenue"`
	OverallConversionRate  float64 `json:"overall_conversion_rate"`
	AverageDealSize        int64   `json:"average_deal_size"`
	AverageSalesCycleDays  float64 `json:"average_sales_cycle_days"`
	AverageLeadScore       float64 `json:"average_lead_score"`
	OverallChannelROI      float64 `json:"overall_channel_roi"`
	OverallRevenueMultiple float64 `json:"overall_revenue_multiple"`
}

// ScoringIntelligence holds lead scoring distribution, top intent deals,
// factor weights, and at-risk pipeline deals.
type ScoringIntelligence struct {
	AverageScore  float64              `json:"average_score"`
	HotCount      int                  `json:"hot_count"`
	WarmCount     int                  `json:"warm_count"`
	ColdCount     int                  `json:"cold_count"`
	HotValue      int64                `json:"hot_value"`
	WarmValue     int64                `json:"warm_value"`
	ColdValue     int64                `json:"cold_value"`
	BandBreakdown []ScoreBandBreakdown `json:"band_breakdown"`
	FactorWeights []ScoringFactorRule  `json:"factor_weights"`
	TopLeads      []Lead               `json:"top_leads"`
	AtRiskDeals   []Lead               `json:"at_risk_deals"`
}

// ScoreBandBreakdown aggregates counts, values, and conversion probabilities by band.
type ScoreBandBreakdown struct {
	Band               ScoreBand `json:"band"`
	Label              string    `json:"label"`
	Count              int       `json:"count"`
	Percentage         float64   `json:"percentage"`
	TotalValue         int64     `json:"total_value"`
	AverageProbability int       `json:"average_probability"`
}

// ScoringFactorRule explains the weights of the scoring engine.
type ScoringFactorRule struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	MaxPoints   int    `json:"max_points"`
	Description string `json:"description"`
}

// FunnelAnalytics details stage progression, conversion, drop-offs, and velocity.
type FunnelAnalytics struct {
	Stages                   []FunnelStageMetric `json:"stages"`
	TotalFunnelEntries       int                 `json:"total_funnel_entries"`
	OverallConversionRate    float64             `json:"overall_conversion_rate"`
	AvgSalesCycleDays        float64             `json:"avg_sales_cycle_days"`
	BottleneckStage          string              `json:"bottleneck_stage"`
	BottleneckReason         string              `json:"bottleneck_reason"`
	BottleneckRecommendation string              `json:"bottleneck_recommendation"`
}

// FunnelStageMetric represents one tier of the sales funnel.
type FunnelStageMetric struct {
	Stage                Stage   `json:"stage"`
	Label                string  `json:"label"`
	EnteredCount         int     `json:"entered_count"`
	CurrentCount         int     `json:"current_count"`
	CurrentValue         int64   `json:"current_value"`
	ConversionRate       float64 `json:"conversion_rate"`
	CumulativeConversion float64 `json:"cumulative_conversion"`
	DropOffCount         int     `json:"drop_off_count"`
	DropOffRate          float64 `json:"drop_off_rate"`
	AvgDurationDays      float64 `json:"avg_duration_days"`
}

// SourceROIReport tracks profitability, CAC, and ROI for every acquisition channel.
type SourceROIReport struct {
	Sources              []SourceROIMetric `json:"sources"`
	TotalAcquisitionCost int64             `json:"total_acquisition_cost"`
	TotalWonRevenue      int64             `json:"total_won_revenue"`
	TotalNetProfit       int64             `json:"total_net_profit"`
	OverallROI           float64           `json:"overall_roi"`
	OverallRevenueMulti  float64           `json:"overall_revenue_multiple"`
	TopPerformingSource  string            `json:"top_performing_source"`
	LowestCACSource      string            `json:"lowest_cac_source"`
}

// SourceROIMetric holds granular unit economics per source.
type SourceROIMetric struct {
	Source               Source  `json:"source"`
	Label                string  `json:"label"`
	Leads                int     `json:"leads"`
	ShareOfLeads         float64 `json:"share_of_leads"`
	Won                  int     `json:"won"`
	Lost                 int     `json:"lost"`
	Open                 int     `json:"open"`
	WinRate              float64 `json:"win_rate"`
	WonValue             int64   `json:"won_value"`
	PipelineValue        int64   `json:"pipeline_value"`
	AvgDealSize          int64   `json:"avg_deal_size"`
	BenchmarkCostPerLead int64   `json:"benchmark_cost_per_lead"`
	TotalAcquisitionCost int64   `json:"total_acquisition_cost"`
	NetProfit            int64   `json:"net_profit"`
	ROI                  float64 `json:"roi"`
	RevenueMultiple      float64 `json:"revenue_multiple"`
	CAC                  int64   `json:"cac"`
	EfficiencyTier       string  `json:"efficiency_tier"`
}
