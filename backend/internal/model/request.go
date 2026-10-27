package model

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// ---------------------------------------------------------------------------
// Validation plumbing
// ---------------------------------------------------------------------------

// ValidationErrors maps a field name to a human-readable problem with it, e.g.
// {"email": "must be a valid email address"}.
//
// It implements the built-in error interface (a single Error() string method),
// so a set of field problems can be passed around as an ordinary Go error while
// still carrying enough structure for the API to return a useful 422 body.
type ValidationErrors map[string]string

// Error satisfies the error interface.
func (v ValidationErrors) Error() string {
	if len(v) == 0 {
		return "no validation errors"
	}
	parts := make([]string, 0, len(v))
	for field, msg := range v {
		parts = append(parts, field+": "+msg)
	}
	return strings.Join(parts, "; ")
}

// Any reports whether anything failed validation.
func (v ValidationErrors) Any() bool { return len(v) > 0 }

// These intentionally mirror the CHECK constraints in db/schema.sql. If the two
// ever drift, the database rejects a row the API thought was fine and the user
// sees a 500 instead of a helpful message -- so they are kept side by side.
//
// Lengths are counted in characters, as Postgres's char_length does, never in
// bytes: a 40-letter Devanagari name is 120 bytes of UTF-8.
var (
	// Go's \s is ASCII-only, while Postgres's [:space:] also matches Unicode
	// spaces such as the no-break space that sneaks in when an address is
	// pasted from a document. \v, \p{Z} and \x{85} close that gap, so such an
	// address gets a field error here instead of a 500 from the CHECK.
	emailPattern = regexp.MustCompile(`^[^@\s\v\p{Z}\x{85}]+@[^@\s\v\p{Z}\x{85}]+\.[^@\s\v\p{Z}\x{85}]+$`)
	phonePattern = regexp.MustCompile(`^[0-9+()./\s-]{6,24}$`)
)

const (
	maxNameLen  = 120
	maxEmailLen = 254
	maxNotesLen = 5000
	maxNoteLen  = 2000

	maxTaskTitleLen = 200

	// MaxDealValue matches the leads_deal_value_range CHECK: one lakh crore
	// rupees, far above any real deal, but low enough to catch a slipped finger.
	MaxDealValue int64 = 1_000_000_000_000
)

// DateLayout is the wire format for calendar dates such as a task's due date.
const DateLayout = "2006-01-02"

// ---------------------------------------------------------------------------
// Create / update a lead
// ---------------------------------------------------------------------------

// LeadInput is the request body for both POST /api/leads and PUT /api/leads/:id.
//
// PUT is treated as a full replacement of the editable fields (proper REST
// semantics) rather than a partial patch, which is why one struct serves both:
// the required fields are required in both cases.
type LeadInput struct {
	Name    string `json:"name"`
	Company string `json:"company"`
	Email   string `json:"email"`
	Phone   string `json:"phone"`
	Source  Source `json:"source"`
	Stage   Stage  `json:"stage"`
	Notes   string `json:"notes"`

	// DealValue is optional: null (or omitted) means "not estimated yet".
	DealValue *int64 `json:"deal_value"`

	// OwnerID is optional: null (or omitted) means unowned.
	OwnerID *int64 `json:"owner_id"`

	// close_reason and close_note, for a Won or Lost lead. Required when the
	// lead becomes Lost; see CloseInput.Validate and the store.
	CloseInput
}

// Normalize cleans up the input before validation: whitespace is trimmed,
// email is lower-cased so the case-insensitive unique index behaves
// predictably, and the two optional enums fall back to sensible defaults.
//
// The pointer receiver (*LeadInput) is what lets this method mutate the struct.
// A value receiver would edit a copy and silently do nothing.
func (in *LeadInput) Normalize() {
	in.Name = strings.TrimSpace(in.Name)
	in.Company = strings.TrimSpace(in.Company)
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.Phone = strings.TrimSpace(in.Phone)
	in.Notes = strings.TrimSpace(in.Notes)

	if in.Source == "" {
		in.Source = SourceOther
	}
	if in.Stage == "" {
		in.Stage = StageNew
	}
	in.CloseInput.Normalize()
}

// Validate returns one entry per problem found. An empty result means the input
// is safe to hand to the database.
func (in LeadInput) Validate() ValidationErrors {
	errs := ValidationErrors{}

	switch {
	case in.Name == "":
		errs["name"] = "is required"
	case utf8.RuneCountInString(in.Name) > maxNameLen:
		errs["name"] = "must be 120 characters or fewer"
	}

	switch {
	case in.Company == "":
		errs["company"] = "is required"
	case utf8.RuneCountInString(in.Company) > maxNameLen:
		errs["company"] = "must be 120 characters or fewer"
	}

	switch {
	case in.Email == "":
		errs["email"] = "is required"
	case utf8.RuneCountInString(in.Email) > maxEmailLen:
		errs["email"] = "must be 254 characters or fewer"
	case !emailPattern.MatchString(in.Email):
		errs["email"] = "must be a valid email address"
	}

	// Phone is optional, but if given it has to look like a phone number.
	if in.Phone != "" && !phonePattern.MatchString(in.Phone) {
		errs["phone"] = "must be 6-24 characters using digits and + ( ) - . / space"
	}

	if !in.Source.Valid() {
		errs["source"] = "must be one of: " + joinSources(Sources)
	}
	if !in.Stage.Valid() {
		errs["stage"] = "must be one of: " + joinStages(Stages)
	} else {
		// The shape of the reason only: whether one is required depends on the
		// stage the lead is moving from, which the store checks.
		for field, msg := range in.CloseInput.Validate(in.Stage, false) {
			errs[field] = msg
		}
	}

	if utf8.RuneCountInString(in.Notes) > maxNotesLen {
		errs["notes"] = "must be 5000 characters or fewer"
	}

	if in.DealValue != nil && (*in.DealValue < 0 || *in.DealValue > MaxDealValue) {
		errs["deal_value"] = "must be between 0 and 1000000000000"
	}

	if in.OwnerID != nil && *in.OwnerID <= 0 {
		errs["owner_id"] = "must be a team member's id"
	}

	return errs
}

// PhonePtr converts the empty string to nil so an omitted phone is stored as SQL
// NULL rather than as an empty string. "Absent" and "empty" should not be two
// different ways of saying the same thing in the database.
func (in LeadInput) PhonePtr() *string {
	if in.Phone == "" {
		return nil
	}
	p := in.Phone
	return &p
}

// ---------------------------------------------------------------------------
// Inline stage change
// ---------------------------------------------------------------------------

// StageInput is the body of PATCH /api/leads/:id/stage, the endpoint behind the
// list view's inline stage dropdown. A dedicated endpoint means the UI can move
// a lead along the pipeline without having to resend every other field.
//
// Moving to Won or Lost can carry close_reason and close_note; sending them
// with the stage the lead is already in changes just the reason.
type StageInput struct {
	Stage Stage `json:"stage"`
	CloseInput
}

// Validate checks the requested stage exists, and the shape of any reason.
// Whether a reason is required depends on the stage the lead is moving from,
// which the store checks.
func (in StageInput) Validate() ValidationErrors {
	errs := ValidationErrors{}
	if in.Stage == "" {
		errs["stage"] = "is required"
	} else if !in.Stage.Valid() {
		errs["stage"] = "must be one of: " + joinStages(Stages)
	} else {
		for field, msg := range in.CloseInput.Validate(in.Stage, false) {
			errs[field] = msg
		}
	}
	return errs
}

// ---------------------------------------------------------------------------
// Inline owner and notes changes
// ---------------------------------------------------------------------------

// OwnerInput is the body of PATCH /api/leads/:id/owner: {"owner_id": 3} gives
// the lead to member 3, {"owner_id": null} leaves it unowned.
//
// The key must be present. An empty body is rejected rather than read as
// "unassign", so a client bug cannot quietly strip a lead of its owner.
type OwnerInput struct {
	OwnerID *int64
	present bool
}

// UnmarshalJSON records whether owner_id was sent at all, which a plain
// *int64 field cannot tell apart from an explicit null.
func (in *OwnerInput) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	value, ok := raw["owner_id"]
	if !ok {
		return nil
	}
	in.present = true
	return json.Unmarshal(value, &in.OwnerID)
}

// Validate checks owner_id was sent and, if not null, looks like an id.
func (in OwnerInput) Validate() ValidationErrors {
	errs := ValidationErrors{}
	switch {
	case !in.present:
		errs["owner_id"] = "is required (null for no owner)"
	case in.OwnerID != nil && *in.OwnerID <= 0:
		errs["owner_id"] = "must be a team member's id"
	}
	return errs
}

// NotesInput is the body of PATCH /api/leads/:id/notes, which edits the lead's
// notes without resending (and possibly overwriting) every other field.
type NotesInput struct {
	Notes string `json:"notes"`
}

// Normalize trims the notes, as LeadInput does.
func (in *NotesInput) Normalize() { in.Notes = strings.TrimSpace(in.Notes) }

// Validate checks the length. Empty is allowed: it clears the notes.
func (in NotesInput) Validate() ValidationErrors {
	errs := ValidationErrors{}
	if utf8.RuneCountInString(in.Notes) > maxNotesLen {
		errs["notes"] = "must be 5000 characters or fewer"
	}
	return errs
}

// ---------------------------------------------------------------------------
// Log an activity
// ---------------------------------------------------------------------------

// ActivityInput is the body of POST /api/leads/:id/activities.
type ActivityInput struct {
	Type ActivityType `json:"type"`
	Note string       `json:"note"`
}

// Normalize trims the note and defaults an omitted type to a plain note.
func (in *ActivityInput) Normalize() {
	in.Note = strings.TrimSpace(in.Note)
	if in.Type == "" {
		in.Type = ActivityNote
	}
}

// Validate checks the note is present and the type is one a user may create.
func (in ActivityInput) Validate() ValidationErrors {
	errs := ValidationErrors{}

	switch {
	case in.Note == "":
		errs["note"] = "is required"
	case utf8.RuneCountInString(in.Note) > maxNoteLen:
		errs["note"] = "must be 2000 characters or fewer"
	}

	// 'created' and 'stage_change' are written by the server, so a client
	// asking for one is rejected -- otherwise the timeline could be faked.
	if !in.Type.ValidForUser() {
		errs["type"] = "must be one of: " + joinActivityTypes(UserActivityTypes)
	}

	return errs
}

// ActivityUpdateInput is the body of PUT /api/activities/:id: the corrected
// text. The type and time stay as they were logged.
type ActivityUpdateInput struct {
	Note string `json:"note"`
}

// Normalize trims the note.
func (in *ActivityUpdateInput) Normalize() { in.Note = strings.TrimSpace(in.Note) }

// Validate applies the same rules as logging the entry did.
func (in ActivityUpdateInput) Validate() ValidationErrors {
	errs := ValidationErrors{}
	switch {
	case in.Note == "":
		errs["note"] = "is required"
	case utf8.RuneCountInString(in.Note) > maxNoteLen:
		errs["note"] = "must be 2000 characters or fewer"
	}
	return errs
}

// ---------------------------------------------------------------------------
// Tasks
// ---------------------------------------------------------------------------

// TaskInput is the body of POST /api/leads/:id/tasks and PUT /api/tasks/:id.
type TaskInput struct {
	Title   string   `json:"title"`
	Type    TaskType `json:"type"`     // optional; defaults to "todo"
	DueDate string   `json:"due_date"` // "2006-01-02"

	// Optional time slot. Omit both for an all-day task; a start time without a
	// duration defaults to DefaultTaskDuration.
	StartTime       string `json:"start_time"` // "15:04"
	DurationMinutes *int   `json:"duration_minutes"`

	// Notes is the agenda or meeting details. Optional.
	Notes string `json:"notes"`

	// AssigneeID is who the event is for. On create, null falls back to the
	// lead's owner; on edit it is taken as given, so null unassigns.
	AssigneeID *int64 `json:"assignee_id"`

	// ReminderMinutes is one of ReminderOptions, or null for no reminder.
	ReminderMinutes *int `json:"reminder_minutes"`
}

const maxTaskNotesLen = 2000

// TimeLayout is the wire format for a task's start time.
const TimeLayout = "15:04"

// Bounds for a task's time slot, in minutes.
const (
	DefaultTaskDuration = 30
	MinTaskDuration     = 5
	MaxTaskDuration     = 1440
)

// Normalize trims the text fields and fills in the default duration when only
// a start time was given.
func (in *TaskInput) Normalize() {
	in.Title = strings.TrimSpace(in.Title)
	in.Type = TaskType(strings.ToLower(strings.TrimSpace(string(in.Type))))
	if in.Type == "" {
		in.Type = TaskTodo
	}
	in.DueDate = strings.TrimSpace(in.DueDate)
	in.StartTime, in.DurationMinutes = normalizeSlot(in.StartTime, in.DurationMinutes)
	in.Notes = strings.TrimSpace(in.Notes)
}

// normalizeSlot trims the start time and fills in the default duration when
// only a start time was given.
func normalizeSlot(startTime string, duration *int) (string, *int) {
	startTime = strings.TrimSpace(startTime)
	if startTime != "" && duration == nil {
		d := DefaultTaskDuration
		duration = &d
	}
	return startTime, duration
}

// Validate checks the title is present and the due date is a real date.
//
// A due date in the past is allowed on purpose: logging "should have called
// them yesterday" is a legitimate thing to want on the overdue list.
func (in TaskInput) Validate() ValidationErrors {
	errs := ValidationErrors{}

	switch {
	case in.Title == "":
		errs["title"] = "is required"
	case utf8.RuneCountInString(in.Title) > maxTaskTitleLen:
		errs["title"] = "must be 200 characters or fewer"
	}

	if !in.Type.Valid() {
		errs["type"] = fmt.Sprintf("%q is not one of %s", in.Type, joinTaskTypes(TaskTypes))
	}

	if utf8.RuneCountInString(in.Notes) > maxTaskNotesLen {
		errs["notes"] = "must be 2000 characters or fewer"
	}

	if in.AssigneeID != nil && *in.AssigneeID <= 0 {
		errs["assignee_id"] = "must be a team member's id"
	}

	if in.ReminderMinutes != nil && !ValidReminder(*in.ReminderMinutes) {
		errs["reminder_minutes"] = "must be 5, 15, 30 or 1440 minutes, or null for no reminder"
	}

	validateSchedule(errs, in.DueDate, in.StartTime, in.DurationMinutes)
	return errs
}

func joinTaskTypes(items []TaskType) string {
	out := make([]string, len(items))
	for i, t := range items {
		out[i] = string(t)
	}
	return strings.Join(out, ", ")
}

// validateSchedule checks a due date and optional time slot, adding any
// problems to errs. Shared by task creation and rescheduling.
func validateSchedule(errs ValidationErrors, dueDate, startTime string, duration *int) {
	switch {
	case dueDate == "":
		errs["due_date"] = "is required"
	default:
		if _, err := time.Parse(DateLayout, dueDate); err != nil {
			errs["due_date"] = "must be a date in YYYY-MM-DD format"
		}
	}

	if startTime != "" {
		if _, err := time.Parse(TimeLayout, startTime); err != nil {
			errs["start_time"] = "must be a time in HH:MM format"
		}
	}

	if duration != nil {
		switch {
		case startTime == "":
			errs["duration_minutes"] = "needs a start time"
		case *duration < MinTaskDuration || *duration > MaxTaskDuration:
			errs["duration_minutes"] = fmt.Sprintf("must be between %d and %d minutes", MinTaskDuration, MaxTaskDuration)
		}
	}
}

// TaskScheduleInput is the body of PUT /api/tasks/:id/schedule: where the task
// now sits on the calendar. It replaces the whole slot, so leaving start_time
// out makes the task all-day.
type TaskScheduleInput struct {
	DueDate         string `json:"due_date"`
	StartTime       string `json:"start_time"`
	DurationMinutes *int   `json:"duration_minutes"`
}

// Normalize trims the fields and defaults the duration, as TaskInput does.
func (in *TaskScheduleInput) Normalize() {
	in.DueDate = strings.TrimSpace(in.DueDate)
	in.StartTime, in.DurationMinutes = normalizeSlot(in.StartTime, in.DurationMinutes)
}

// Validate checks the date and slot.
func (in TaskScheduleInput) Validate() ValidationErrors {
	errs := ValidationErrors{}
	validateSchedule(errs, in.DueDate, in.StartTime, in.DurationMinutes)
	return errs
}

// TaskStatusInput is the body of PATCH /api/tasks/:id: either
// {"completed": true|false} or {"cancelled": true|false}. Pointers so that a
// missing field is an error rather than silently meaning "false".
type TaskStatusInput struct {
	Completed *bool `json:"completed"`
	Cancelled *bool `json:"cancelled"`
}

// Validate checks exactly one of the two flags was sent.
func (in TaskStatusInput) Validate() ValidationErrors {
	errs := ValidationErrors{}
	switch {
	case in.Completed == nil && in.Cancelled == nil:
		errs["completed"] = "send completed or cancelled"
	case in.Completed != nil && in.Cancelled != nil:
		errs["cancelled"] = "send completed or cancelled, not both"
	}
	return errs
}

// ---------------------------------------------------------------------------
// List query
// ---------------------------------------------------------------------------

// Pagination defaults and bounds for the leads list.
const (
	// DefaultPageSize is deliberately small: it keeps the first paint fast and
	// means the pagination controls are exercised by realistic data volumes
	// rather than only appearing once someone has a few hundred leads.
	DefaultPageSize = 10

	// MaxPageSize caps what a client can ask for, so no single request can pull
	// the whole table.
	MaxPageSize = 100

	// MaxPage caps the page number, so page * page_size can never overflow
	// into a negative OFFSET that Postgres rejects with a 500.
	MaxPage = 1_000_000
)

// SortableLeadFields is the allow-list of values accepted by ?sort_by=.
//
// An allow-list is not optional here: sort columns cannot be passed to Postgres
// as bind parameters, so they end up interpolated into the SQL string. Checking
// the value against a fixed set is what keeps that from being a SQL injection
// hole.
var SortableLeadFields = map[string]bool{
	"created_at": true,
	"updated_at": true,
	"name":       true,
	"company":    true,
	"stage":      true, // sorts in lifecycle order, thanks to the Postgres enum
	"deal_value": true, // unvalued leads always sort last, see orderClause
	SortByScore:  true, // not a column: sorted in Go, see LeadQuery.NeedsScores
}

// SortByScore sorts by lead score. The score is worked out in Go on every read
// and never stored, so it cannot go in an ORDER BY; the store sorts these in Go.
const SortByScore = "score"

// LeadStatuses is the allow-list for ?status=. "open" is every stage before Won
// or Lost (the Deals page's main view); "closed" is Won or Lost.
var LeadStatuses = map[string]bool{
	"open":   true,
	"closed": true,
}

// LeadQuery is the parsed, validated form of the query string on
// GET /api/leads.
type LeadQuery struct {
	// Search matches case-insensitively against name OR company. Empty means
	// "no search".
	Search string

	// Stage and Source are filters; the zero value "" means "any".
	Stage  Stage
	Source Source

	// Status narrows to a group of stages: one of LeadStatuses, or "" for any.
	// It combines with Stage by AND, like every other filter.
	Status string

	// OwnerID narrows to one member's leads; Unowned to leads with no owner.
	// At most one is set (?owner=3 or ?owner=none).
	OwnerID *int64
	Unowned bool

	// Band narrows to leads whose score is in one band; "" means any. Closed
	// leads have no score, so a band filter never matches them.
	Band ScoreBand

	// Quality narrows to one lead quality (see quality.go); "" means any.
	Quality Quality

	// IDs narrows to these leads; empty means any.
	IDs []int64

	SortBy  string // one of SortableLeadFields
	SortDir string // "asc" or "desc"

	Page     int // 1-based
	PageSize int
}

// Offset converts the 1-based page number into a SQL OFFSET.
func (q LeadQuery) Offset() int { return (q.Page - 1) * q.PageSize }

// NeedsScores reports whether the query sorts or filters by score, which only
// Go can work out: such a query has to score every matching lead before it can
// pick a page.
func (q LeadQuery) NeedsScores() bool { return q.SortBy == SortByScore || q.Band != "" }

// MaxScoredLeads caps how many leads a NeedsScores query will load and score.
// Far above what this CRM holds; it is there so a runaway table degrades into a
// partial list instead of an out-of-memory server.
const MaxScoredLeads = 10_000

// PageMeta is the pagination block attached to list responses so the client
// knows how many pages exist without having to guess.
type PageMeta struct {
	Page       int `json:"page"`
	PageSize   int `json:"page_size"`
	TotalItems int `json:"total_items"`
	TotalPages int `json:"total_pages"`
}

// NewPageMeta computes the pagination block for a page of results.
func NewPageMeta(q LeadQuery, totalItems int) PageMeta {
	totalPages := 0
	if totalItems > 0 {
		// Integer ceiling division: how many pages of PageSize fit totalItems.
		totalPages = (totalItems + q.PageSize - 1) / q.PageSize
	}
	return PageMeta{
		Page:       q.Page,
		PageSize:   q.PageSize,
		TotalItems: totalItems,
		TotalPages: totalPages,
	}
}

// ---------------------------------------------------------------------------
// Small helpers for building "must be one of: ..." messages
// ---------------------------------------------------------------------------

func joinStages(items []Stage) string {
	out := make([]string, len(items))
	for i, s := range items {
		out[i] = string(s)
	}
	return strings.Join(out, ", ")
}

func joinSources(items []Source) string {
	out := make([]string, len(items))
	for i, s := range items {
		out[i] = string(s)
	}
	return strings.Join(out, ", ")
}

func joinActivityTypes(items []ActivityType) string {
	out := make([]string, len(items))
	for i, t := range items {
		out[i] = string(t)
	}
	return strings.Join(out, ", ")
}
