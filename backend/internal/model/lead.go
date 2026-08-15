// Package model holds the domain types shared by the storage and HTTP layers,
// plus the rules that define what a valid lead is.
//
// Deliberately dependency-free: it imports nothing from store or api. That keeps
// the dependency arrows pointing one way (api -> store -> model) and means these
// types can be unit-tested without a database or an HTTP server.
package model

import "time"

// ---------------------------------------------------------------------------
// Stage
// ---------------------------------------------------------------------------

// Stage is where a lead sits in the sales lifecycle.
//
// It is a distinct string type rather than a bare string so the compiler can
// tell a stage apart from any other piece of text. The values match the
// lead_stage enum in db/schema.sql exactly -- machine-readable snake_case over
// the wire, with display text supplied by Label.
type Stage string

const (
	StageNew          Stage = "new"
	StageContacted    Stage = "contacted"
	StageQualified    Stage = "qualified"
	StageProposalSent Stage = "proposal_sent"
	StageNegotiation  Stage = "negotiation"
	StageWon          Stage = "won"
	StageLost         Stage = "lost"
)

// Stages lists every stage in lifecycle order. The order matters: it drives the
// dashboard's bar chart and the step indicator in the UI, and it is the same
// order the labels are declared in the Postgres enum.
var Stages = []Stage{
	StageNew,
	StageContacted,
	StageQualified,
	StageProposalSent,
	StageNegotiation,
	StageWon,
	StageLost,
}

var stageLabels = map[Stage]string{
	StageNew:          "New",
	StageContacted:    "Contacted",
	StageQualified:    "Qualified",
	StageProposalSent: "Proposal Sent",
	StageNegotiation:  "Negotiation",
	StageWon:          "Won",
	StageLost:         "Lost",
}

// Valid reports whether s is a stage the system knows about. Anything arriving
// from an HTTP client is checked with this before it reaches SQL.
func (s Stage) Valid() bool {
	_, ok := stageLabels[s]
	return ok
}

// Label returns the human-readable form, e.g. "Proposal Sent".
func (s Stage) Label() string {
	if l, ok := stageLabels[s]; ok {
		return l
	}
	return string(s)
}

// IsClosed reports whether the stage is terminal -- the deal is done either way.
func (s Stage) IsClosed() bool { return s == StageWon || s == StageLost }

// ---------------------------------------------------------------------------
// Source
// ---------------------------------------------------------------------------

// Source is how the lead first reached us. Mirrors the lead_source enum.
type Source string

const (
	SourceWebsite  Source = "website"
	SourceReferral Source = "referral"
	SourceLinkedIn Source = "linkedin"
	SourceColdCall Source = "cold_call"
	SourceOther    Source = "other"
)

// Sources lists every source, in the order the UI dropdown shows them.
var Sources = []Source{
	SourceWebsite,
	SourceReferral,
	SourceLinkedIn,
	SourceColdCall,
	SourceOther,
}

var sourceLabels = map[Source]string{
	SourceWebsite:  "Website",
	SourceReferral: "Referral",
	SourceLinkedIn: "LinkedIn",
	SourceColdCall: "Cold Call",
	SourceOther:    "Other",
}

// Valid reports whether s is a known lead source.
func (s Source) Valid() bool {
	_, ok := sourceLabels[s]
	return ok
}

// Label returns the human-readable form, e.g. "Cold Call".
func (s Source) Label() string {
	if l, ok := sourceLabels[s]; ok {
		return l
	}
	return string(s)
}

// ---------------------------------------------------------------------------
// ActivityType
// ---------------------------------------------------------------------------

// ActivityType classifies a timeline entry. Mirrors the activity_type enum.
type ActivityType string

const (
	// ActivityCreated and ActivityStageChange are written by the server itself,
	// never accepted from a client, so the timeline is a trustworthy record of
	// what actually happened to the lead.
	ActivityCreated     ActivityType = "created"
	ActivityStageChange ActivityType = "stage_change"

	// The rest are what a user logs by hand.
	ActivityNote    ActivityType = "note"
	ActivityCall    ActivityType = "call"
	ActivityEmail   ActivityType = "email"
	ActivityMeeting ActivityType = "meeting"
)

// UserActivityTypes are the types a client is allowed to submit.
var UserActivityTypes = []ActivityType{
	ActivityNote,
	ActivityCall,
	ActivityEmail,
	ActivityMeeting,
}

// ValidForUser reports whether a client may create an activity of this type.
// The two system types are excluded on purpose.
func (t ActivityType) ValidForUser() bool {
	for _, allowed := range UserActivityTypes {
		if t == allowed {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Entities
// ---------------------------------------------------------------------------

// Lead is one row of the leads table.
//
// The `json:"..."` tags control the wire format. Go convention is CapitalCase
// field names (capitalised also means "exported", i.e. visible to other
// packages), while JSON convention is snake_case -- the tags bridge the two.
type Lead struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Company   string    `json:"company"`
	Email     string    `json:"email"`
	Phone     *string   `json:"phone"` // pointer: nil marshals to JSON null, meaning "not provided"
	Source    Source    `json:"source"`
	Stage     Stage     `json:"stage"`
	Notes     string    `json:"notes"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Activities is populated only by the single-lead endpoint. `omitempty`
	// keeps it out of the JSON entirely in list responses, so the payload for
	// 50 leads does not carry 50 empty arrays.
	Activities []Activity `json:"activities,omitempty"`
}

// Activity is one row of the activities table: a timestamped event on a lead.
type Activity struct {
	ID        int64        `json:"id"`
	LeadID    int64        `json:"lead_id"`
	Type      ActivityType `json:"type"`
	Note      string       `json:"note"`
	CreatedAt time.Time    `json:"created_at"`
}

// ---------------------------------------------------------------------------
// Dashboard
// ---------------------------------------------------------------------------

// StageCount is one bar of the "leads by stage" chart.
type StageCount struct {
	Stage Stage  `json:"stage"`
	Label string `json:"label"`
	Count int    `json:"count"`
}

// DashboardSummary is the aggregate view returned by GET /api/dashboard.
type DashboardSummary struct {
	TotalLeads int `json:"total_leads"`

	// ByStage always contains one entry per stage, including stages with a
	// count of zero. Returning the zeros is what lets the frontend render a
	// complete chart without knowing the list of stages itself.
	ByStage []StageCount `json:"by_stage"`

	WonCount  int `json:"won_count"`
	LostCount int `json:"lost_count"`

	// OpenCount is leads still in play: everything not Won or Lost.
	OpenCount int `json:"open_count"`

	// ConversionRate is won / (won + lost) as a percentage, rounded to one
	// decimal place. It is 0 when nothing has closed yet.
	ConversionRate float64 `json:"conversion_rate"`

	// AddedLast7Days supports the "momentum" widget on the dashboard.
	AddedLast7Days int `json:"added_last_7_days"`

	// RecentLeads is the 5 most recently created leads, newest first.
	RecentLeads []Lead `json:"recent_leads"`
}
