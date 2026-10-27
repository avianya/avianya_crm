package model

import "time"

// This file is deal health by time in stage: a deal that has sat in one stage
// for too long is "stuck", whatever its contact history. Health (insights.go)
// asks "have we spoken to them lately?"; this asks "is the deal moving?". A
// lead can be called every week and still be stuck in Negotiation for months.

// stageStuckDays is how long a deal may sit in each open stage before it is
// flagged. Fixed, published numbers, like the win probabilities, so anyone can
// see why a deal was flagged.
var stageStuckDays = map[Stage]int{
	StageNew:          7,
	StageContacted:    10,
	StageQualified:    14,
	StageProposalSent: 10,
	StageNegotiation:  14,
}

// StuckAfterDays is how many days in this stage make a deal stuck. ok is false
// for Won and Lost, which cannot be stuck.
func (s Stage) StuckAfterDays() (days int, ok bool) {
	days, ok = stageStuckDays[s]
	return days, ok
}

// ApplyStageAge fills in how long the lead has been in its current stage and
// whether that makes it stuck. enteredAt is when it entered the stage (its
// latest stage history entry); now is passed in so tests can fix the clock.
func (l *Lead) ApplyStageAge(enteredAt, now time.Time) {
	l.StageEnteredAt = enteredAt
	l.DaysInStage = DaysBetween(enteredAt, now)
	limit, ok := l.Stage.StuckAfterDays()
	if !ok {
		l.StuckLimitDays, l.Stuck = 0, false
		return
	}
	l.StuckLimitDays = limit
	l.Stuck = l.DaysInStage >= limit
}

// DealAlert is one "this deal is stuck" alert in the bell. One is raised per
// stint in a stage, the first time the stint passes its limit; moving the
// deal on resolves it.
type DealAlert struct {
	ID          int64  `json:"id"`
	LeadID      int64  `json:"lead_id"`
	LeadName    string `json:"lead_name"`
	LeadCompany string `json:"lead_company"`
	OwnerID     *int64 `json:"owner_id"`

	Stage          Stage     `json:"stage"`
	StageEnteredAt time.Time `json:"stage_entered_at"`
	LimitDays      int       `json:"limit_days"`

	// DaysInStage is how long the stint has lasted so far, or lasted in all
	// once Resolved: the deal has since moved stage (or been won or lost).
	DaysInStage int  `json:"days_in_stage"`
	Resolved    bool `json:"resolved"`

	CreatedAt time.Time  `json:"created_at"`
	ReadAt    *time.Time `json:"read_at"`
}

// MaxDealAlertsListed caps the bell's alert list.
const MaxDealAlertsListed = 20

// StuckThresholds returns the open stages and their limits as two parallel
// slices, ready to bind as SQL arrays.
func StuckThresholds() (stages []string, days []int32) {
	for _, stage := range Stages {
		if limit, ok := stage.StuckAfterDays(); ok {
			stages = append(stages, string(stage))
			days = append(days, int32(limit))
		}
	}
	return stages, days
}
