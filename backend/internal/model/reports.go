package model

// Types for GET /api/reports: trends over time rather than the dashboard's
// "right now" snapshot.

// WeekCount is one bar of the "new leads per week" chart.
type WeekCount struct {
	// WeekStart is the Monday the week begins on, "2006-01-02".
	WeekStart string `json:"week_start"`
	Count     int    `json:"count"`
}

// SourceReport is how one lead source performs from first touch to close.
type SourceReport struct {
	Source Source `json:"source"`
	Label  string `json:"label"`
	Leads  int    `json:"leads"`
	Won    int    `json:"won"`
	Lost   int    `json:"lost"`

	// WinRate is won / (won + lost) as a percentage with one decimal, or nil
	// when nothing from this source has closed yet -- "no data" is not 0%.
	WinRate  *float64 `json:"win_rate"`
	WonValue int64    `json:"won_value"`
}

// StageDuration is how long leads typically sit in one open stage before
// moving on.
type StageDuration struct {
	Stage Stage  `json:"stage"`
	Label string `json:"label"`

	// AvgDays is the mean length of finished stints in this stage, one decimal,
	// or nil when no lead has left the stage yet. Samples is how many stints the
	// average is over, so the UI can say how far to trust it.
	AvgDays *float64 `json:"avg_days"`
	Samples int      `json:"samples"`
}

// ReportSummary is the full response of GET /api/reports.
type ReportSummary struct {
	// LeadsPerWeek covers the last ReportWeeks weeks, oldest first, including
	// weeks with no new leads.
	LeadsPerWeek   []WeekCount     `json:"leads_per_week"`
	BySource       []SourceReport  `json:"by_source"`
	StageDurations []StageDuration `json:"stage_durations"`

	// LossReasons and WinReasons break the Lost and Won leads down by why they
	// closed, most deals first.
	LossReasons []ReasonCount `json:"loss_reasons"`
	WinReasons  []ReasonCount `json:"win_reasons"`
}

// ReasonCount is one reason in the win or loss breakdown.
type ReasonCount struct {
	// Reason is nil for deals closed with no reason recorded: lost before
	// reasons were asked for, or won with the reason skipped.
	Reason *CloseReason `json:"reason"`
	Label  string       `json:"label"`
	Count  int          `json:"count"`

	// Value is the total deal value of those deals in rupees; deals with no
	// value estimated count as 0.
	Value int64 `json:"value"`
}

// ReasonCountLabel is the label for a reason in the report, including the
// "no reason recorded" row.
func ReasonCountLabel(stage Stage, reason *CloseReason) string {
	if reason == nil {
		return "No reason recorded"
	}
	return ReasonLabel(stage, *reason)
}

// ReportWeeks is how many weeks the "new leads per week" chart covers.
const ReportWeeks = 12
