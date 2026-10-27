package model

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"
)

// This file holds the two derived signals shown on every lead: health (is this
// lead going cold?) and score (how promising is it?). Neither is stored. Both
// are recomputed from the lead, its timeline and its tasks on every read, so
// they can never drift out of date and changing a rule changes every lead at
// once.
//
// They are plain functions of their inputs -- no database, no clock reads --
// which is what lets insights_test.go pin down every rule exactly.

// ---------------------------------------------------------------------------
// Health
// ---------------------------------------------------------------------------

// Health says how recently a lead was actually contacted, relative to how
// quickly a lead in its stage ought to hear from us.
type Health string

const (
	HealthActive  Health = "active"
	HealthCooling Health = "cooling"
	HealthCold    Health = "cold"

	// HealthClosed is for Won and Lost leads, where "going cold" is meaningless.
	HealthClosed Health = "closed"
)

// HealthThresholds are the days without contact after which a lead turns
// Cooling and then Cold.
type HealthThresholds struct {
	CoolingDays int
	ColdDays    int
}

// stageHealthThresholds sets a different clock per stage. A lead in Negotiation
// that has not heard from us in a week is in real danger; a freshly Qualified
// lead mid-evaluation can go a fortnight. Late stages are deliberately the
// strictest, because that is where silence loses the most money.
var stageHealthThresholds = map[Stage]HealthThresholds{
	StageNew:          {CoolingDays: 3, ColdDays: 7},
	StageContacted:    {CoolingDays: 5, ColdDays: 10},
	StageQualified:    {CoolingDays: 7, ColdDays: 14},
	StageProposalSent: {CoolingDays: 4, ColdDays: 10},
	StageNegotiation:  {CoolingDays: 3, ColdDays: 7},
}

// Thresholds returns the health clock for an open stage. ok is false for Won
// and Lost, which have none.
func (s Stage) Thresholds() (HealthThresholds, bool) {
	t, ok := stageHealthThresholds[s]
	return t, ok
}

// TouchActivityTypes are the timeline entries that count as actually being in
// contact with the lead.
//
// stage_change is excluded on purpose: dragging a lead to "Proposal Sent" is
// bookkeeping, not contact, and counting it would let a lead look healthy
// without anyone having spoken to them.
var TouchActivityTypes = []ActivityType{
	ActivityNote,
	ActivityCall,
	ActivityEmail,
	ActivityMeeting,
	ActivityTaskCompleted,
}

// HealthFor classifies a lead from its stage and the whole days since it was
// last touched.
func HealthFor(stage Stage, daysSinceTouch int) Health {
	t, ok := stage.Thresholds()
	if !ok {
		return HealthClosed
	}
	switch {
	case daysSinceTouch >= t.ColdDays:
		return HealthCold
	case daysSinceTouch >= t.CoolingDays:
		return HealthCooling
	default:
		return HealthActive
	}
}

// DaysBetween is the number of whole days from then to now, never negative.
func DaysBetween(then, now time.Time) int {
	if now.Before(then) {
		return 0
	}
	return int(now.Sub(then).Hours() / 24)
}

// ---------------------------------------------------------------------------
// Score
// ---------------------------------------------------------------------------

// ScoreFactor is one line of a lead's score: what was checked, and how many of
// the available points it earned. Keys are stable identifiers the UI can use to
// suggest how to earn the missing points.
type ScoreFactor struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Points int    `json:"points"`
	Max    int    `json:"max"`
}

// ScorePenalty is a deduction for something going wrong on the lead. Points is
// negative. Only penalties that actually apply are listed.
type ScorePenalty struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Points int    `json:"points"`
}

// LeadScore is a 0-100 score with its full working. Total is always exactly
// the factor points plus the (negative) penalty points, floored at 0 -- there
// is no hidden adjustment.
type LeadScore struct {
	Total     int            `json:"total"`
	Band      ScoreBand      `json:"band"`
	Factors   []ScoreFactor  `json:"factors"`
	Penalties []ScorePenalty `json:"penalties"`
}

// ScoreBand groups scores into three plain words a salesperson can sort a day
// by: call the Hot ones first.
type ScoreBand string

const (
	BandHot  ScoreBand = "hot"
	BandWarm ScoreBand = "warm"
	BandCold ScoreBand = "cold"
)

// ScoreBands is every band, hottest first.
var ScoreBands = []ScoreBand{BandHot, BandWarm, BandCold}

// The lowest score in each band. The frontend colours scores with the same
// cut-offs.
const (
	hotFromScore  = 70
	warmFromScore = 40
)

// BandFor puts a 0-100 score in its band.
func BandFor(total int) ScoreBand {
	switch {
	case total >= hotFromScore:
		return BandHot
	case total >= warmFromScore:
		return BandWarm
	default:
		return BandCold
	}
}

// IsValid reports whether b is one of ScoreBands.
func (b ScoreBand) IsValid() bool {
	for _, band := range ScoreBands {
		if b == band {
			return true
		}
	}
	return false
}

// stageScorePoints rewards progress through the pipeline. It is the heaviest
// factor (30 of 100) because how far a lead has got is the strongest signal of
// whether it will close.
var stageScorePoints = map[Stage]int{
	StageNew:          0,
	StageContacted:    8,
	StageQualified:    15,
	StageProposalSent: 23,
	StageNegotiation:  30,
}

// sourceScorePoints rewards where the lead came from. A referral arrives with
// trust already earned; a cold call has to build it from nothing. An ad form
// is a raised hand too: Google's come from a search, like a website enquiry;
// Meta's are filled in from a feed, often with answers pre-filled.
var sourceScorePoints = map[Source]int{
	SourceReferral:  10,
	SourceWebsite:   7,
	SourceGoogleAds: 7,
	SourceLinkedIn:  5,
	SourceMetaAds:   5,
	SourceOther:     3,
	SourceColdCall:  2,
}

// ConversationActivityTypes are the timeline entries that count towards
// engagement: actual contact with the lead. Notes and completed tasks keep a
// lead healthy (TouchActivityTypes) but are written by us, not with the lead.
var ConversationActivityTypes = []ActivityType{
	ActivityCall,
	ActivityEmail,
	ActivityMeeting,
}

// EngagementWindowDays is how far back conversations are counted.
const EngagementWindowDays = 30

// Points for the remaining factors. Together with the 30 for stage they add up
// to exactly 100.
const (
	scoreRecentContactActive  = 20
	scoreRecentContactCooling = 8
	scoreEngagementMax        = 15
	scoreSourceMax            = 10
	scoreNextStep             = 10
	scoreDealValue            = 10
	scorePhone                = 3
	scoreNotes                = 2
)

// Penalties, subtracted from the factor points.
const (
	penaltyPerOverdueTask  = 5
	penaltyOverdueTasksMax = 10
	penaltyStuck           = 10
)

// engagementPoints scores the conversations in the engagement window: one is a
// start, a few is a real dialogue, four or more is an engaged buyer.
func engagementPoints(conversations int) int {
	switch {
	case conversations >= 4:
		return scoreEngagementMax
	case conversations >= 2:
		return 10
	case conversations == 1:
		return 5
	default:
		return 0
	}
}

// ScoreFor computes a lead's score. It returns nil for Won and Lost leads: a
// closed deal is not a prospect, and a score of 100 or 0 would only add noise.
//
// It reads the lead's derived Health, Stuck and DaysInStage, so those must be
// filled in first; ApplyInsights does it in the right order.
//
// The factors are things a salesperson can act on -- book a follow-up, log the
// call, estimate the deal -- so the score doubles as a to-do list rather than
// being a verdict nobody can influence. Penalties work the same way: each one
// names a problem that, once fixed, gives the points back.
func ScoreFor(lead Lead, sig LeadSignals) *LeadScore {
	if lead.Stage.IsClosed() {
		return nil
	}

	recent := 0
	switch lead.Health {
	case HealthActive:
		recent = scoreRecentContactActive
	case HealthCooling:
		recent = scoreRecentContactCooling
	}

	factors := []ScoreFactor{
		{Key: "stage", Label: "Pipeline stage", Points: stageScorePoints[lead.Stage], Max: 30},
		{Key: "recent_contact", Label: "Recent contact", Points: recent, Max: scoreRecentContactActive},
		{Key: "engagement", Label: "Conversations in the last 30 days", Points: engagementPoints(sig.RecentConversations), Max: scoreEngagementMax},
		{Key: "source", Label: "Lead source", Points: sourceScorePoints[lead.Source], Max: scoreSourceMax},
		{Key: "next_step", Label: "Next step planned", Points: pointsIf(sig.HasOpenTask, scoreNextStep), Max: scoreNextStep},
		{Key: "deal_value", Label: "Deal value estimated", Points: pointsIf(lead.DealValue != nil, scoreDealValue), Max: scoreDealValue},
		{Key: "phone", Label: "Phone number on file", Points: pointsIf(lead.Phone != nil, scorePhone), Max: scorePhone},
		{Key: "notes", Label: "Context notes written", Points: pointsIf(lead.Notes != "", scoreNotes), Max: scoreNotes},
	}

	// An empty slice, not nil, so the JSON is always [] and never null.
	penalties := []ScorePenalty{}
	if sig.OverdueTasks > 0 {
		penalties = append(penalties, ScorePenalty{
			Key:    "overdue_tasks",
			Label:  fmt.Sprintf("%d overdue task%s", sig.OverdueTasks, plural(sig.OverdueTasks)),
			Points: -min(sig.OverdueTasks*penaltyPerOverdueTask, penaltyOverdueTasksMax),
		})
	}
	if lead.Stuck {
		penalties = append(penalties, ScorePenalty{
			Key:    "stuck",
			Label:  fmt.Sprintf("Stuck in %s for %d days", lead.Stage.Label(), lead.DaysInStage),
			Points: -penaltyStuck,
		})
	}

	total := 0
	for _, f := range factors {
		total += f.Points
	}
	for _, p := range penalties {
		total += p.Points
	}

	total = max(total, 0)
	return &LeadScore{Total: total, Band: BandFor(total), Factors: factors, Penalties: penalties}
}

func pointsIf(condition bool, points int) int {
	if condition {
		return points
	}
	return 0
}

// ---------------------------------------------------------------------------
// Putting it together
// ---------------------------------------------------------------------------

// LeadSignals is what the store reads from a lead's timeline and tasks to work
// out its health, stage age and score.
type LeadSignals struct {
	// LastTouch is the last real contact (see TouchActivityTypes), falling
	// back to when the lead was created.
	LastTouch time.Time
	// StageEnteredAt is when the lead entered its current stage.
	StageEnteredAt time.Time
	HasOpenTask    bool
	// RecentConversations counts ConversationActivityTypes entries in the
	// last EngagementWindowDays days.
	RecentConversations int
	// OverdueTasks counts open tasks whose time has passed.
	OverdueTasks int
}

// ApplyInsights fills in all of a lead's derived fields: health, stage age and
// score, in that order, because the score depends on the other two. now is
// passed in rather than read, so tests can fix the clock.
func (l *Lead) ApplyInsights(sig LeadSignals, now time.Time) {
	l.LastTouchAt = sig.LastTouch
	l.DaysSinceTouch = DaysBetween(sig.LastTouch, now)
	l.HasOpenTask = sig.HasOpenTask
	l.Health = HealthFor(l.Stage, l.DaysSinceTouch)
	l.ApplyStageAge(sig.StageEnteredAt, now)
	l.Score = ScoreFor(*l, sig)
}

// ---------------------------------------------------------------------------
// Sorting and filtering by score
// ---------------------------------------------------------------------------

// FilterByBand keeps the leads whose score is in band, in their original
// order. Closed leads have no score, so they never match.
func FilterByBand(leads []Lead, band ScoreBand) []Lead {
	out := make([]Lead, 0, len(leads))
	for _, lead := range leads {
		if lead.Score != nil && lead.Score.Band == band {
			out = append(out, lead)
		}
	}
	return out
}

// SortLeadsByScore orders leads by score, highest first for "desc" and lowest first
// for "asc". Closed leads, which have no score, go last either way, like
// unvalued deals in a deal value sort. Equal scores fall back to id in the same
// direction, so pages never overlap or skip a lead.
func SortLeadsByScore(leads []Lead, dir string) {
	asc := strings.EqualFold(dir, "asc")
	slices.SortFunc(leads, func(a, b Lead) int {
		if a.Score == nil || b.Score == nil {
			// Scored before unscored; two unscored fall through to id.
			if c := cmp.Compare(scoredRank(a), scoredRank(b)); c != 0 {
				return c
			}
		} else if c := cmp.Compare(a.Score.Total, b.Score.Total); c != 0 {
			return directed(c, asc)
		}
		return directed(cmp.Compare(a.ID, b.ID), asc)
	})
}

func scoredRank(l Lead) int {
	if l.Score == nil {
		return 1
	}
	return 0
}

func directed(c int, asc bool) int {
	if asc {
		return c
	}
	return -c
}
