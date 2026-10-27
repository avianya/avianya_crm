package model

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// This file is the Google Sheets export: which lead fields can become
// columns, the saved settings, and the history of syncs. Package gsheets
// talks to Google; the store keeps the settings and reads the leads.

// SheetField is a lead field that can be a spreadsheet column.
type SheetField struct {
	Key   string `json:"key"`
	Label string `json:"label"` // the column's header in the sheet
	// Default fields are ticked until the admin chooses.
	Default bool `json:"default"`
}

// SheetFieldLeadID is the stable key every row is matched on. It is always
// the first column, so a sync updates a lead's row instead of adding another.
const SheetFieldLeadID = "lead_id"

// SheetFields are the fields on offer, in their default order. Notes are off
// by default: they can hold personal details nobody asked to export.
var SheetFields = []SheetField{
	{SheetFieldLeadID, "CRM Lead ID", true},
	{"name", "Name", true},
	{"company", "Company", true},
	{"email", "Email", true},
	{"phone", "Phone", true},
	{"source", "Lead Source", true},
	{"stage", "Status", true},
	{"owner", "Assigned To", true},
	{"next_follow_up", "Next Follow-up", true},
	{"created_at", "Created", true},
	{"updated_at", "Last Updated", false},
	{"deal_value", "Deal Value", false},
	{"notes", "Notes", false},
}

// SheetFieldLabel returns a field's column header, or "" for an unknown key.
func SheetFieldLabel(key string) string {
	for _, f := range SheetFields {
		if f.Key == key {
			return f.Label
		}
	}
	return ""
}

// IsSheetFieldLabel reports whether a header cell is one the CRM writes.
func IsSheetFieldLabel(label string) bool {
	for _, f := range SheetFields {
		if f.Label == label {
			return true
		}
	}
	return false
}

// DefaultSheetColumns are the columns before the admin picks any.
func DefaultSheetColumns() []string {
	cols := []string{}
	for _, f := range SheetFields {
		if f.Default {
			cols = append(cols, f.Key)
		}
	}
	return cols
}

// SheetStatus mirrors integration_settings.status for this provider.
type SheetStatus string

const (
	SheetNotConnected SheetStatus = "not_connected"
	SheetConnected    SheetStatus = "connected"
	SheetError        SheetStatus = "error"
)

// Auto-sync intervals on offer, in minutes.
var SheetAutoSyncOptions = []int{5, 15, 30, 60}

// SheetsConfig is the saved, non-secret part of the settings.
type SheetsConfig struct {
	AccountEmail string `json:"account_email"`
	// ConfiguredBy is the admin who connected Google. Syncs run with their
	// access to leads, and stop if they are no longer an admin.
	ConfiguredBy int64 `json:"configured_by"`

	SpreadsheetID   string `json:"spreadsheet_id"`
	SpreadsheetName string `json:"spreadsheet_name"`
	SpreadsheetURL  string `json:"spreadsheet_url"`
	SheetTitle      string `json:"sheet_title"`

	Columns []string `json:"columns"`
	// Stages narrows the export; empty means every stage.
	Stages []Stage `json:"stages"`

	AutoSync        bool `json:"auto_sync"`
	AutoSyncMinutes int  `json:"auto_sync_minutes"`

	// LastHash fingerprints what was last written, so an automatic sync with
	// nothing new to write does not call Google at all.
	LastHash string `json:"last_hash"`
}

// DefaultSheetsConfig is the config before anything was saved.
func DefaultSheetsConfig() SheetsConfig {
	return SheetsConfig{Columns: DefaultSheetColumns(), Stages: []Stage{}, AutoSyncMinutes: 15}
}

// Ready reports whether a sheet has been chosen to sync to.
func (c SheetsConfig) Ready() bool { return c.SpreadsheetID != "" && c.SheetTitle != "" }

// SheetsRecord is the whole saved row. RefreshToken is opened from the sealed
// secrets and never leaves the server.
type SheetsRecord struct {
	Config        SheetsConfig
	RefreshToken  string
	TokenLocked   bool // a token is saved but cannot be decrypted
	Status        SheetStatus
	LastSyncAt    *time.Time
	LastSyncCount *int
	LastError     string
	LastErrorAt   *time.Time
}

// Connected reports whether there is a usable Google sign-in.
func (r SheetsRecord) Connected() bool { return r.RefreshToken != "" }

// SheetSync is one sync_history row.
type SheetSync struct {
	ID          int64      `json:"id"`
	Trigger     string     `json:"trigger"` // manual | auto | retry
	Status      string     `json:"status"`  // running | success | partial | failed
	StartedAt   time.Time  `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at"`
	LeadsTotal  int        `json:"leads_total"`
	RowsUpdated int        `json:"rows_updated"`
	RowsAdded   int        `json:"rows_added"`
	RowsFailed  int        `json:"rows_failed"`
	Error       string     `json:"error"`
	StartedBy   string     `json:"started_by"` // "" for automatic syncs
	RetryOf     *int64     `json:"retry_of"`
}

// SheetsSettings is GET /api/integrations/google-sheets: everything the card
// and the Configure dialog show. Nothing secret.
type SheetsSettings struct {
	// Set up on the server: the OAuth client and the encryption key.
	OAuthConfigured bool     `json:"oauth_configured"`
	EncryptionReady bool     `json:"encryption_ready"`
	PickerAvailable bool     `json:"picker_available"`
	RedirectURL     string   `json:"redirect_url"`
	MissingEnv      []string `json:"missing_env"`

	Status       SheetStatus `json:"status"`
	AccountEmail string      `json:"account_email"`
	// TokenLocked: a sign-in is saved but SECRETS_ENCRYPTION_KEY cannot open it.
	TokenLocked bool `json:"token_locked"`

	SpreadsheetID   string   `json:"spreadsheet_id"`
	SpreadsheetName string   `json:"spreadsheet_name"`
	SpreadsheetURL  string   `json:"spreadsheet_url"`
	SheetTitle      string   `json:"sheet_title"`
	Columns         []string `json:"columns"`
	Stages          []Stage  `json:"stages"`
	AutoSync        bool     `json:"auto_sync"`
	AutoSyncMinutes int      `json:"auto_sync_minutes"`

	LastSyncAt    *time.Time `json:"last_sync_at"`
	LastSyncCount *int       `json:"last_sync_count"`
	LastError     string     `json:"last_error"`
	LastErrorAt   *time.Time `json:"last_error_at"`

	Fields           []SheetField `json:"fields"`
	AutoSyncOptions  []int        `json:"auto_sync_options"`
	ConfiguredByName string       `json:"configured_by_name"`
}

// SheetsConfigInput is PUT /api/integrations/google-sheets: the sheet to
// sync to and how. A blank SheetTitle with NewSheetTitle set makes a new tab.
type SheetsConfigInput struct {
	SpreadsheetID   string   `json:"spreadsheet_id"`
	SheetTitle      string   `json:"sheet_title"`
	NewSheetTitle   string   `json:"new_sheet_title"`
	Columns         []string `json:"columns"`
	Stages          []Stage  `json:"stages"`
	AutoSync        bool     `json:"auto_sync"`
	AutoSyncMinutes int      `json:"auto_sync_minutes"`
}

const maxSheetTitleLen = 100

func (in *SheetsConfigInput) Normalize() {
	in.SpreadsheetID = strings.TrimSpace(in.SpreadsheetID)
	in.SheetTitle = strings.TrimSpace(in.SheetTitle)
	in.NewSheetTitle = strings.TrimSpace(in.NewSheetTitle)

	// The lead id always leads, once; unknown and repeated keys go.
	cols := []string{SheetFieldLeadID}
	for _, c := range in.Columns {
		c = strings.TrimSpace(c)
		if c == SheetFieldLeadID || SheetFieldLabel(c) == "" || containsString(cols, c) {
			continue
		}
		cols = append(cols, c)
	}
	in.Columns = cols

	stages := []Stage{}
	for _, s := range in.Stages {
		if !containsStage(stages, s) {
			stages = append(stages, s)
		}
	}
	in.Stages = stages
	if in.AutoSyncMinutes == 0 {
		in.AutoSyncMinutes = 15
	}
}

func (in SheetsConfigInput) Validate() ValidationErrors {
	errs := ValidationErrors{}
	// Spreadsheet ids are letters, digits, - and _.
	if in.SpreadsheetID == "" {
		errs["spreadsheet_id"] = "choose a spreadsheet"
	} else if strings.Trim(in.SpreadsheetID, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_") != "" {
		errs["spreadsheet_id"] = "is not a spreadsheet id"
	}
	switch {
	case in.SheetTitle == "" && in.NewSheetTitle == "":
		errs["sheet_title"] = "choose a tab, or name a new one"
	case utf8.RuneCountInString(in.SheetTitle) > maxSheetTitleLen || utf8.RuneCountInString(in.NewSheetTitle) > maxSheetTitleLen:
		errs["sheet_title"] = fmt.Sprintf("must be %d characters or fewer", maxSheetTitleLen)
	}
	if len(in.Columns) < 2 {
		errs["columns"] = "choose at least one field besides the lead id"
	}
	for _, s := range in.Stages {
		if !s.Valid() {
			errs["stages"] = fmt.Sprintf("%q is not a stage", s)
			break
		}
	}
	if !containsInt(SheetAutoSyncOptions, in.AutoSyncMinutes) {
		errs["auto_sync_minutes"] = "must be one of 5, 15, 30 or 60"
	}
	return errs
}

func containsStage(list []Stage, s Stage) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func containsInt(list []int, n int) bool {
	for _, v := range list {
		if v == n {
			return true
		}
	}
	return false
}

// ExportLead is one lead as the export reads it.
type ExportLead struct {
	ID           int64
	Name         string
	Company      string
	Email        string
	Phone        string
	Source       Source
	Stage        Stage
	OwnerName    string
	DealValue    *int64
	NextFollowUp string // "2006-01-02", or "" when nothing is planned
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Notes        string
}

// SheetRow is the lead as a row of these columns. Every value is plain text,
// written RAW, so a name starting with "=" is never run as a formula.
func (l ExportLead) SheetRow(columns []string, loc *time.Location) []string {
	row := make([]string, len(columns))
	for i, col := range columns {
		switch col {
		case SheetFieldLeadID:
			row[i] = strconv.FormatInt(l.ID, 10)
		case "name":
			row[i] = l.Name
		case "company":
			row[i] = l.Company
		case "email":
			row[i] = l.Email
		case "phone":
			row[i] = l.Phone
		case "source":
			row[i] = l.Source.Label()
		case "stage":
			row[i] = l.Stage.Label()
		case "owner":
			row[i] = l.OwnerName
		case "deal_value":
			if l.DealValue != nil {
				row[i] = strconv.FormatInt(*l.DealValue, 10)
			}
		case "next_follow_up":
			row[i] = l.NextFollowUp
		case "created_at":
			row[i] = l.CreatedAt.In(loc).Format("2006-01-02 15:04")
		case "updated_at":
			row[i] = l.UpdatedAt.In(loc).Format("2006-01-02 15:04")
		case "notes":
			row[i] = l.Notes
		}
	}
	return row
}
