package model

import (
	"regexp"
	"strings"
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
var (
	emailPattern = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
	phonePattern = regexp.MustCompile(`^[0-9+()./\s-]{6,24}$`)
)

const (
	maxNameLen  = 120
	maxEmailLen = 254
	maxNotesLen = 5000
	maxNoteLen  = 2000
)

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
}

// Validate returns one entry per problem found. An empty result means the input
// is safe to hand to the database.
func (in LeadInput) Validate() ValidationErrors {
	errs := ValidationErrors{}

	switch {
	case in.Name == "":
		errs["name"] = "is required"
	case len(in.Name) > maxNameLen:
		errs["name"] = "must be 120 characters or fewer"
	}

	switch {
	case in.Company == "":
		errs["company"] = "is required"
	case len(in.Company) > maxNameLen:
		errs["company"] = "must be 120 characters or fewer"
	}

	switch {
	case in.Email == "":
		errs["email"] = "is required"
	case len(in.Email) > maxEmailLen:
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
	}

	if len(in.Notes) > maxNotesLen {
		errs["notes"] = "must be 5000 characters or fewer"
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
type StageInput struct {
	Stage Stage `json:"stage"`
}

// Validate checks the requested stage exists.
func (in StageInput) Validate() ValidationErrors {
	errs := ValidationErrors{}
	if in.Stage == "" {
		errs["stage"] = "is required"
	} else if !in.Stage.Valid() {
		errs["stage"] = "must be one of: " + joinStages(Stages)
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
	case len(in.Note) > maxNoteLen:
		errs["note"] = "must be 2000 characters or fewer"
	}

	// 'created' and 'stage_change' are written by the server, so a client
	// asking for one is rejected -- otherwise the timeline could be faked.
	if !in.Type.ValidForUser() {
		errs["type"] = "must be one of: " + joinActivityTypes(UserActivityTypes)
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

	SortBy  string // one of SortableLeadFields
	SortDir string // "asc" or "desc"

	Page     int // 1-based
	PageSize int
}

// Offset converts the 1-based page number into a SQL OFFSET.
func (q LeadQuery) Offset() int { return (q.Page - 1) * q.PageSize }

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
