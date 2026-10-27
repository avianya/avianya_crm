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

// stageWinProbability is the chance, in percent, that a deal sitting in each
// stage ends up Won. It drives the weighted forecast on the dashboard.
//
// These are fixed, published numbers rather than a model trained on history:
// the point is that anyone can look at the forecast and reproduce it by hand.
// GET /api/meta exposes them so the UI can show the working.
var stageWinProbability = map[Stage]int{
	StageNew:          10,
	StageContacted:    20,
	StageQualified:    40,
	StageProposalSent: 60,
	StageNegotiation:  80,
	StageWon:          100,
	StageLost:         0,
}

// WinProbability returns the stage's win probability as a whole percentage.
func (s Stage) WinProbability() int { return stageWinProbability[s] }

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

	// Leads from ad lead forms, which arrive by webhook (see adleads.go).
	SourceMetaAds   Source = "meta_ads"
	SourceGoogleAds Source = "google_ads"
)

// Sources lists every source, in the order the UI dropdown shows them.
var Sources = []Source{
	SourceWebsite,
	SourceReferral,
	SourceLinkedIn,
	SourceColdCall,
	SourceMetaAds,
	SourceGoogleAds,
	SourceOther,
}

var sourceLabels = map[Source]string{
	SourceWebsite:   "Website",
	SourceReferral:  "Referral",
	SourceLinkedIn:  "LinkedIn",
	SourceColdCall:  "Cold Call",
	SourceMetaAds:   "Meta Ads",
	SourceGoogleAds: "Google Ads",
	SourceOther:     "Other",
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

	// ActivityTaskCompleted is written when a follow-up task is ticked off, so
	// the timeline shows the follow-up actually happened.
	ActivityTaskCompleted ActivityType = "task_completed"

	// ActivityTaskCancelled is written when a scheduled event is cancelled.
	ActivityTaskCancelled ActivityType = "task_cancelled"

	// ActivityOwnerChange is written when a lead is given to another owner.
	ActivityOwnerChange ActivityType = "owner_change"

	// ActivityAutomation is written when an automation rule plans a task.
	ActivityAutomation ActivityType = "automation"

	// ActivityAdForm is written when an existing lead fills in an ad lead form
	// again.
	ActivityAdForm ActivityType = "ad_form"

	// ActivityQualityChange is written when someone overrides a lead's
	// quality score, or removes the override.
	ActivityQualityChange ActivityType = "quality_change"

	// ActivityBooking is written when the lead books a meeting with their
	// booking link; ActivityFeedback when a won customer sends feedback.
	ActivityBooking  ActivityType = "booking"
	ActivityFeedback ActivityType = "feedback"

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
	ID      int64   `json:"id"`
	Name    string  `json:"name"`
	Company string  `json:"company"`
	Email   string  `json:"email"`
	Phone   *string `json:"phone"` // pointer: nil marshals to JSON null, meaning "not provided"
	Source  Source  `json:"source"`
	Stage   Stage   `json:"stage"`
	Notes   string  `json:"notes"`

	// DealValue is the expected deal size in whole rupees. nil means nobody has
	// estimated it yet, which the forecast treats differently from a real 0.
	DealValue *int64 `json:"deal_value"`

	// OwnerID is the team member responsible for the lead; nil when unowned.
	// New events on the lead are assigned to the owner unless told otherwise.
	OwnerID *int64 `json:"owner_id"`

	// CloseReason and CloseNote say why a Won or Lost deal closed (see
	// closereasons.go). Both are nil for an open lead, and for a closed one
	// nobody gave a reason for.
	CloseReason *CloseReason `json:"close_reason"`
	CloseNote   *string      `json:"close_note"`

	// Lead quality (see quality.go). Embedded, so its fields sit at the top
	// level of the JSON like every other lead field.
	LeadQuality

	// The lead's latest conversion event sent to Meta, if any.
	LeadMetaSummary

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Derived on every read by ApplyInsights (see insights.go); never stored.
	//
	// LastTouchAt is the last real contact -- a call, email, meeting, note or
	// completed task -- falling back to when the lead was created.
	LastTouchAt    time.Time  `json:"last_touch_at"`
	DaysSinceTouch int        `json:"days_since_touch"`
	Health         Health     `json:"health"`
	HasOpenTask    bool       `json:"has_open_task"`
	Score          *LeadScore `json:"score"` // nil for Won and Lost leads

	// Time in the current stage (see stuck.go). StuckLimitDays is 0 and Stuck
	// false for Won and Lost.
	StageEnteredAt time.Time `json:"stage_entered_at"`
	DaysInStage    int       `json:"days_in_stage"`
	StuckLimitDays int       `json:"stuck_limit_days"`
	Stuck          bool      `json:"stuck"`

	// Activities is populated only by the single-lead endpoint. `omitempty`
	// keeps it out of the JSON entirely in list responses, so the payload for
	// 50 leads does not carry 50 empty arrays.
	Activities []Activity `json:"activities,omitempty"`

	// StageHistory, oldest first, is likewise only on the single-lead endpoint.
	StageHistory []StageHistoryEntry `json:"stage_history,omitempty"`

	// Also only on the single-lead endpoint: where a Meta lead came from, and
	// the conversion events sent back to Meta for it, newest first.
	MetaAttribution *MetaAttribution `json:"meta_attribution,omitempty"`
	CAPIEvents      []CAPIEvent      `json:"capi_events,omitempty"`
}

// StageHistoryEntry is one row of lead_stage_history: the lead entered ToStage
// at ChangedAt. FromStage is nil on a lead's first entry, which is the stage it
// was created in.
type StageHistoryEntry struct {
	ID        int64     `json:"id"`
	FromStage *Stage    `json:"from_stage"`
	ToStage   Stage     `json:"to_stage"`
	ChangedAt time.Time `json:"changed_at"`
}

// Activity is one row of the activities table: a timestamped event on a lead.
type Activity struct {
	ID        int64        `json:"id"`
	LeadID    int64        `json:"lead_id"`
	Type      ActivityType `json:"type"`
	Note      string       `json:"note"`
	CreatedAt time.Time    `json:"created_at"`

	// EditedAt is set once someone has corrected the note; nil if never edited.
	EditedAt *time.Time `json:"edited_at"`
}

// TaskType is what kind of follow-up a task is. Mirrors the tasks.type CHECK.
type TaskType string

const (
	TaskTodo     TaskType = "todo"
	TaskCall     TaskType = "call"
	TaskEmail    TaskType = "email"
	TaskMeeting  TaskType = "meeting"
	TaskDemo     TaskType = "demo"
	TaskFollowUp TaskType = "follow_up"
)

// TaskTypes lists every valid task type.
var TaskTypes = []TaskType{TaskTodo, TaskCall, TaskEmail, TaskMeeting, TaskDemo, TaskFollowUp}

var taskTypeLabels = map[TaskType]string{
	TaskTodo:     "To-do",
	TaskCall:     "Call",
	TaskEmail:    "Email",
	TaskMeeting:  "Meeting",
	TaskDemo:     "Demo",
	TaskFollowUp: "Follow-up",
}

// Label is the display name, e.g. "Follow-up".
func (t TaskType) Label() string {
	if l, ok := taskTypeLabels[t]; ok {
		return l
	}
	return string(t)
}

// Valid reports whether t is one of TaskTypes.
func (t TaskType) Valid() bool {
	for _, known := range TaskTypes {
		if t == known {
			return true
		}
	}
	return false
}

// CompletionActivity is the timeline entry written when a task of this type is
// completed. A finished call is logged as a call, so the timeline reads as the
// history of contact it is; a demo counts as a meeting; a plain to-do or
// follow-up is logged as task_completed.
func (t TaskType) CompletionActivity() ActivityType {
	switch t {
	case TaskCall:
		return ActivityCall
	case TaskEmail:
		return ActivityEmail
	case TaskMeeting, TaskDemo:
		return ActivityMeeting
	default:
		return ActivityTaskCompleted
	}
}

// TaskStatus is where an event stands. It is worked out on every read, never
// stored: Upcoming turns into Overdue on its own as time passes.
type TaskStatus string

const (
	TaskUpcoming  TaskStatus = "upcoming"
	TaskOverdue   TaskStatus = "overdue"
	TaskCompleted TaskStatus = "completed"
	TaskCancelled TaskStatus = "cancelled"
)

// ReminderOptions are the reminder lead times offered, in minutes before the
// start. Mirrors the tasks_reminder_valid CHECK.
var ReminderOptions = []int{5, 15, 30, 1440}

// ValidReminder reports whether minutes is one of ReminderOptions.
func ValidReminder(minutes int) bool {
	for _, option := range ReminderOptions {
		if minutes == option {
			return true
		}
	}
	return false
}

// AllDayReminderBase is the time of day an all-day event's reminder counts
// back from, matching the tasks_set_remind_at trigger.
const AllDayReminderBase = "09:00"

// Task is one row of the tasks table: a dated follow-up on a lead. With a time
// slot, an assignee and a reminder it is what the UI calls an event.
type Task struct {
	ID     int64    `json:"id"`
	LeadID int64    `json:"lead_id"`
	Title  string   `json:"title"`
	Type   TaskType `json:"type"`

	// DueDate is a calendar date, "2006-01-02". A plain string rather than a
	// time.Time because a due date has no time of day or time zone -- "due on
	// the 10th" must not turn into the 9th for someone west of the server.
	DueDate string `json:"due_date"`

	// StartTime ("15:04") and DurationMinutes give the task a slot on the
	// calendar, like a meeting. Both nil means an all-day task.
	StartTime       *string `json:"start_time"`
	DurationMinutes *int    `json:"duration_minutes"`

	// CompletedAt is nil while the task is open.
	CompletedAt *time.Time `json:"completed_at"`
	CreatedAt   time.Time  `json:"created_at"`

	// Notes holds the agenda or meeting details.
	Notes string `json:"notes"`

	// AssigneeID is the team member the event is for, who gets its reminders.
	AssigneeID *int64 `json:"assignee_id"`

	// ReminderMinutes is how long before the start to remind; nil for none.
	// RemindAt is when that is, nil once the event is completed or cancelled.
	ReminderMinutes *int       `json:"reminder_minutes"`
	RemindAt        *time.Time `json:"remind_at"`

	// CancelledAt is set when the event was called off.
	CancelledAt *time.Time `json:"cancelled_at"`

	// Status is derived from the dates above and the clock.
	Status TaskStatus `json:"status"`

	// LeadName and LeadCompany are filled in only where tasks from many leads
	// are listed together (the dashboard), so each row can say whose it is.
	LeadName    string `json:"lead_name,omitempty"`
	LeadCompany string `json:"lead_company,omitempty"`
}

// TaskWithLead is one row of GET /api/tasks: a task plus the lead it belongs
// to, with that lead's health and score, so the task list can rank follow-ups
// by how much the lead matters.
type TaskWithLead struct {
	Task Task `json:"task"`
	Lead Lead `json:"lead"`
}

// Limits for GET /api/tasks. Open tasks are a working list and should all be
// visible; completed ones are history, so only the most recent are returned.
const (
	MaxOpenTasksListed      = 500
	MaxCompletedTasksListed = 50

	// MaxTodayTasksListed caps the dashboard's agenda; more than this in one
	// day is a list, not an agenda, and the Schedule page shows the rest.
	MaxTodayTasksListed = 25

	// MaxRangeTasksListed caps one calendar range, and MaxRangeDays how long a
	// range may be (a month view with its spill-over weeks is 42 days).
	MaxRangeTasksListed = 2000
	MaxRangeDays        = 62
)

// ---------------------------------------------------------------------------
// Dashboard
// ---------------------------------------------------------------------------

// StageCount is one bar of the "leads by stage" chart, plus the money in it.
type StageCount struct {
	Stage Stage  `json:"stage"`
	Label string `json:"label"`
	Count int    `json:"count"`

	// Value is the sum of deal values in this stage. Leads with no value
	// contribute nothing; UnvaluedCount says how many of those there are.
	Value         int64 `json:"value"`
	UnvaluedCount int   `json:"unvalued_count"`

	// Probability is the stage's win probability in percent, and Weighted is
	// Value x Probability -- the stage's share of the forecast.
	Probability int   `json:"probability"`
	Weighted    int64 `json:"weighted"`
}

// PipelineColumn is one stage column of the board returned by GET /api/pipeline.
type PipelineColumn struct {
	Stage       Stage  `json:"stage"`
	Label       string `json:"label"`
	Probability int    `json:"probability"`

	// Count, Value and Weighted cover every lead in the stage, even when Leads
	// is capped, so the column header is always the true total.
	Count    int   `json:"count"`
	Value    int64 `json:"value"`
	Weighted int64 `json:"weighted"`

	// Leads holds at most PipelineColumnLimit cards. Open stages are ordered by
	// score (best first); Won and Lost by most recently updated.
	Leads []Lead `json:"leads"`
}

// PipelineColumnLimit caps the cards per column. A column with hundreds of
// cards is unusable on a board anyway; the leads list is for browsing those.
const PipelineColumnLimit = 50

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

	// PipelineValue is the total deal value of open leads; WeightedForecast is
	// the same total with each lead scaled by its stage's win probability.
	// WonValue is what has already closed.
	PipelineValue    int64 `json:"pipeline_value"`
	WeightedForecast int64 `json:"weighted_forecast"`
	WonValue         int64 `json:"won_value"`

	// DueTasks is every open task due today or earlier, most overdue first,
	// capped at 10. OverdueTasks and DueTodayTasks are the uncapped counts.
	DueTasks      []Task `json:"due_tasks"`
	OverdueTasks  int    `json:"overdue_tasks"`
	DueTodayTasks int    `json:"due_today_tasks"`

	// TodayTasks is the day's agenda: every task due today, open or done,
	// timed ones by start time and then the all-day ones.
	TodayTasks []Task `json:"today_tasks"`

	// OpenLeadsWithoutTask counts open leads that have no open task at all:
	// leads nobody has planned a next step for.
	OpenLeadsWithoutTask int `json:"open_leads_without_task"`

	// CoolingLeads and ColdLeads count open leads by health. GoingCold is the
	// five open leads furthest past their stage's cooling threshold, i.e. the
	// ones to call first.
	CoolingLeads int    `json:"cooling_leads"`
	ColdLeads    int    `json:"cold_leads"`
	GoingCold    []Lead `json:"going_cold"`

	// StuckCount counts open deals past their stage's time limit; StuckDeals
	// is the five furthest past it (see stuck.go).
	StuckCount int    `json:"stuck_count"`
	StuckDeals []Lead `json:"stuck_deals"`

	// HotCount counts open leads in the Hot band; HottestLeads is the five
	// highest-scoring open leads, whatever their band.
	HotCount     int    `json:"hot_count"`
	HottestLeads []Lead `json:"hottest_leads"`
}
