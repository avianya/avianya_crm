package model

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// This file is CSV export and import of leads. Export writes the columns the
// Leads list shows; import reads a spreadsheet's rows into LeadInputs, matching
// its column headers loosely ("Email address", "E-mail" and "email" are all the
// email), so a file exported here, or from most other tools, imports as is.

// Import limits: big enough for a real lead list, small enough that one
// request cannot tie up the server.
const (
	MaxImportBytes = 2 << 20
	MaxImportRows  = 5000
)

// ---------------------------------------------------------------------------
// Columns
// ---------------------------------------------------------------------------

// csvColumn is one column an import understands. Key names the LeadInput part
// it fills; Header is how export writes it; Aliases are other headers taken
// to mean the same, compared by csvKey.
type csvColumn struct {
	Key     string
	Header  string
	Aliases []string
}

var importColumns = []csvColumn{
	{"name", "Name", []string{"full name", "lead name", "contact name", "contact"}},
	{"company", "Company", []string{"company name", "organisation", "organization", "account", "business"}},
	{"email", "Email", []string{"email address", "e-mail", "mail", "email id"}},
	{"phone", "Phone", []string{"phone number", "mobile", "mobile number", "contact number", "telephone", "tel"}},
	{"source", "Source", []string{"lead source"}},
	{"stage", "Stage", []string{"lead stage", "deal stage", "status"}},
	{"deal_value", "Deal value", []string{"value", "amount", "deal amount", "deal size"}},
	{"owner", "Owner", []string{"lead owner", "assigned to", "owner name"}},
	{"owner_email", "Owner email", []string{"owner e-mail"}},
	{"close_reason", "Close reason", []string{"reason", "win/loss reason", "won/lost reason"}},
	{"close_note", "Close note", nil},
	{"notes", "Notes", []string{"note", "comments", "description"}},
}

// requiredImportColumns must all be in a file's header row.
var requiredImportColumns = []string{"name", "company", "email"}

// csvKey reduces a header or value to lowercase letters and digits, so that
// "Deal value (₹)", "deal_value" and "DEAL VALUE" compare equal.
func csvKey(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func columnFor(header string) (csvColumn, bool) {
	key := csvKey(header)
	if key == "" {
		return csvColumn{}, false
	}
	for _, col := range importColumns {
		if key == csvKey(col.Key) || key == csvKey(col.Header) {
			return col, true
		}
		for _, alias := range col.Aliases {
			if key == csvKey(alias) {
				return col, true
			}
		}
	}
	return csvColumn{}, false
}

func columnHeader(key string) string {
	if col, ok := columnByKey(key); ok {
		return col.Header
	}
	return key
}

// ---------------------------------------------------------------------------
// Export
// ---------------------------------------------------------------------------

// exportHeaders is the header row of an export. Every column but the last two
// is read back by import.
var exportHeaders = []string{
	"Name", "Company", "Email", "Phone", "Source", "Stage", "Deal value",
	"Owner", "Owner email", "Close reason", "Close note", "Notes",
	"Score band", "Created",
}

// WriteLeadsCSV writes leads as a CSV a spreadsheet opens directly: a UTF-8
// byte order mark first, so Excel reads ₹ and non-Latin names correctly.
// owners maps owner ids to members; created times are shown in loc.
func WriteLeadsCSV(w io.Writer, leads []Lead, owners map[int64]TeamMember, loc *time.Location) error {
	if _, err := io.WriteString(w, "\xef\xbb\xbf"); err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	if err := cw.Write(exportHeaders); err != nil {
		return err
	}

	for _, lead := range leads {
		phone, dealValue, ownerName, ownerEmail, reason, note, band := "", "", "", "", "", "", ""
		if lead.Phone != nil {
			phone = *lead.Phone
		}
		if lead.DealValue != nil {
			dealValue = strconv.FormatInt(*lead.DealValue, 10)
		}
		if lead.OwnerID != nil {
			if owner, ok := owners[*lead.OwnerID]; ok {
				ownerName, ownerEmail = owner.Name, owner.Email
			}
		}
		if lead.CloseReason != nil {
			reason = ReasonLabel(lead.Stage, *lead.CloseReason)
		}
		if lead.CloseNote != nil {
			note = *lead.CloseNote
		}
		if lead.Score != nil {
			band = BandLabel(lead.Score.Band)
		}
		created := lead.CreatedAt
		if loc != nil {
			created = created.In(loc)
		}

		record := []string{
			lead.Name, lead.Company, lead.Email, phone,
			lead.Source.Label(), lead.Stage.Label(), dealValue,
			ownerName, ownerEmail, reason, note, lead.Notes,
			band, created.Format("2006-01-02 15:04"),
		}
		for i, cell := range record {
			record[i] = spreadsheetSafe(cell)
		}
		if err := cw.Write(record); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// BandLabel is a score band as a word, e.g. "Hot".
func BandLabel(b ScoreBand) string {
	if b == "" {
		return ""
	}
	return strings.ToUpper(string(b[:1])) + string(b[1:])
}

// spreadsheetSafe stops a cell being run as a formula when the file is opened
// in a spreadsheet: one starting with = + - @ (or a tab or carriage return,
// which some spreadsheets skip first) gets a leading apostrophe. Import takes
// it off again, see unescapeCell.
func spreadsheetSafe(cell string) string {
	if cell != "" && strings.ContainsRune("=+-@\t\r", rune(cell[0])) {
		return "'" + cell
	}
	return cell
}

func unescapeCell(cell string) string {
	if len(cell) >= 2 && cell[0] == '\'' && strings.ContainsRune("=+-@\t\r", rune(cell[1])) {
		return cell[1:]
	}
	return cell
}

// ---------------------------------------------------------------------------
// Import
// ---------------------------------------------------------------------------

// ImportRow is one data row of an imported file: the lead it describes, or
// what is wrong with it. Line is the row's line in the file, counting the
// header as line 1, as a spreadsheet numbers its rows. Cells are the row as
// written, for the file of rows to fix.
type ImportRow struct {
	Line   int
	Cells  []string
	Input  LeadInput
	Errors []string
}

// ImportField is a lead field a column can be imported into, for the
// column picker.
type ImportField struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Required bool   `json:"required"`
}

// ImportFields lists every field, in the picker's order.
func ImportFields() []ImportField {
	fields := make([]ImportField, len(importColumns))
	for i, col := range importColumns {
		fields[i] = ImportField{Key: col.Key, Label: col.Header, Required: slices.Contains(requiredImportColumns, col.Key)}
	}
	return fields
}

// ImportHeader is one column of an import file and the field it goes into;
// Field is "" for a column that is not imported.
type ImportHeader struct {
	Name  string `json:"name"`
	Field string `json:"field"`
}

// LeadCSV is a parsed import file. While a required field has no column
// (Missing is not empty), Rows holds each row's cells only, unvalidated: the
// file cannot be imported until the columns are chosen.
type LeadCSV struct {
	Headers []ImportHeader
	Missing []string // labels of the required fields with no column
	Rows    []ImportRow
}

// ErrImportFile is a problem with the file as a whole rather than one row.
var ErrImportFile = errors.New("import file")

func fileError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrImportFile, fmt.Sprintf(format, args...))
}

// ParseLeadCSV reads an import file. members are the team, to match the Owner
// columns against. Each row is normalised and validated as a new lead would
// be; duplicates of existing leads are left for the caller to find.
//
// mapping chooses the field for each column by position ("" for none), as the
// column picker sends it; nil works the columns out from the header names,
// giving each field to the first column that names it.
//
// Comma- and semicolon-separated files are both accepted (Excel in some
// regions saves the latter), as is a leading byte order mark.
func ParseLeadCSV(data []byte, members []TeamMember, mapping []string) (LeadCSV, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))

	reader := csv.NewReader(bytes.NewReader(data))
	reader.Comma = sniffDelimiter(data)
	reader.FieldsPerRecord = -1 // short and long rows are handled below
	reader.LazyQuotes = true
	reader.TrimLeadingSpace = true

	header, err := reader.Read()
	if errors.Is(err, io.EOF) {
		return LeadCSV{}, fileError("the file is empty")
	}
	if err != nil {
		return LeadCSV{}, fileError("it could not be read as CSV (%v)", err)
	}

	var out LeadCSV
	index := map[string]int{} // field key -> position in the row
	if mapping != nil && len(mapping) != len(header) {
		return LeadCSV{}, fileError("its columns do not match the ones chosen; choose the file again")
	}
	for i, h := range header {
		field := ""
		if mapping != nil {
			field = mapping[i]
			if field != "" {
				if _, known := columnByKey(field); !known {
					return LeadCSV{}, fileError("%q is not a field", field)
				}
				if _, seen := index[field]; seen {
					return LeadCSV{}, fileError("%s is chosen for two columns", columnHeader(field))
				}
			}
		} else if col, ok := columnFor(h); ok {
			if _, seen := index[col.Key]; !seen {
				field = col.Key
			}
		}
		if field != "" {
			index[field] = i
		}
		out.Headers = append(out.Headers, ImportHeader{Name: strings.TrimSpace(h), Field: field})
	}

	for _, key := range requiredImportColumns {
		if _, ok := index[key]; !ok {
			out.Missing = append(out.Missing, columnHeader(key))
		}
	}

	owners := newOwnerMatcher(members)
	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return LeadCSV{}, fileError("it could not be read as CSV (%v)", err)
		}
		line, _ := reader.FieldPos(0)

		cell := func(key string) string {
			i, ok := index[key]
			if !ok || i >= len(record) {
				return ""
			}
			return strings.TrimSpace(unescapeCell(strings.TrimSpace(record[i])))
		}

		if blankRecord(record) {
			continue
		}
		if len(out.Rows) == MaxImportRows {
			return LeadCSV{}, fileError("it has more than %d leads; split it into smaller files", MaxImportRows)
		}
		row := ImportRow{Line: line, Cells: record}
		if len(out.Missing) == 0 {
			row = parseImportRow(line, cell, owners)
			row.Cells = record
		}
		out.Rows = append(out.Rows, row)
	}

	if len(out.Rows) == 0 {
		return LeadCSV{}, fileError("it has a header row but no leads")
	}
	return out, nil
}

func columnByKey(key string) (csvColumn, bool) {
	for _, col := range importColumns {
		if col.Key == key {
			return col, true
		}
	}
	return csvColumn{}, false
}

// sniffDelimiter picks ';' when the header row has semicolons and no commas.
func sniffDelimiter(data []byte) rune {
	first := data
	if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
		first = data[:i]
	}
	if bytes.Count(first, []byte(";")) > 0 && bytes.Count(first, []byte(",")) == 0 {
		return ';'
	}
	return ','
}

func blankRecord(record []string) bool {
	for _, cell := range record {
		if strings.TrimSpace(cell) != "" {
			return false
		}
	}
	return true
}

func parseImportRow(line int, cell func(string) string, owners ownerMatcher) ImportRow {
	row := ImportRow{Line: line}
	problems := map[string]string{}

	in := LeadInput{
		Name:    cell("name"),
		Company: cell("company"),
		Email:   cell("email"),
		Phone:   cell("phone"),
		Notes:   cell("notes"),
	}

	if raw := cell("source"); raw != "" {
		if source, ok := parseSource(raw); ok {
			in.Source = source
		} else {
			problems["source"] = fmt.Sprintf("%q is not a source (use %s)", raw, sourceLabelList())
		}
	}
	if raw := cell("stage"); raw != "" {
		if stage, ok := parseStage(raw); ok {
			in.Stage = stage
		} else {
			problems["stage"] = fmt.Sprintf("%q is not a stage (use %s)", raw, stageLabelList())
		}
	}
	if raw := cell("deal_value"); raw != "" {
		if value, ok := parseRupees(raw); ok {
			in.DealValue = &value
		} else {
			problems["deal_value"] = fmt.Sprintf("%q is not an amount in whole rupees", raw)
		}
	}

	ownerRaw, byEmail := cell("owner_email"), true
	if ownerRaw == "" {
		ownerRaw, byEmail = cell("owner"), false
	}
	if ownerRaw != "" {
		if id, msg := owners.match(ownerRaw, byEmail); msg != "" {
			problems["owner"] = msg
		} else {
			in.OwnerID = &id
		}
	}

	in.Normalize()

	if raw := cell("close_reason"); raw != "" {
		if reason, ok := parseReason(in.Stage, raw); ok {
			in.CloseInput.Reason = &reason
		} else if len(ReasonsFor(in.Stage)) == 0 {
			problems["close_reason"] = "only goes with a Won or Lost lead"
		} else {
			problems["close_reason"] = fmt.Sprintf("%q is not a reason for a %s lead", raw, in.Stage.Label())
		}
	}
	in.CloseInput.Note = cell("close_note")

	for field, msg := range in.Validate() {
		if _, already := problems[field]; !already {
			problems[field] = msg
		}
	}
	// A lead created straight into Lost is entering it, which needs a reason.
	if in.Stage == StageLost && in.CloseInput.Reason == nil && problems["close_reason"] == "" {
		problems["close_reason"] = "is required for a Lost lead"
	}

	row.Input = in
	row.Errors = describeProblems(problems)
	return row
}

// describeProblems turns field errors into sentences, in column order.
func describeProblems(problems map[string]string) []string {
	if len(problems) == 0 {
		return nil
	}
	order := map[string]int{}
	for i, col := range importColumns {
		order[col.Key] = i
	}
	fields := make([]string, 0, len(problems))
	for field := range problems {
		fields = append(fields, field)
	}
	sort.Slice(fields, func(a, b int) bool {
		oa, aok := order[fields[a]]
		ob, bok := order[fields[b]]
		if aok != bok {
			return aok
		}
		if oa != ob {
			return oa < ob
		}
		return fields[a] < fields[b]
	})

	out := make([]string, len(fields))
	for i, field := range fields {
		out[i] = columnHeader(field) + ": " + problems[field]
	}
	return out
}

func parseSource(raw string) (Source, bool) {
	key := csvKey(raw)
	for _, s := range Sources {
		if key == csvKey(string(s)) || key == csvKey(s.Label()) {
			return s, true
		}
	}
	return "", false
}

func parseStage(raw string) (Stage, bool) {
	key := csvKey(raw)
	for _, s := range Stages {
		if key == csvKey(string(s)) || key == csvKey(s.Label()) {
			return s, true
		}
	}
	return "", false
}

func parseReason(stage Stage, raw string) (CloseReason, bool) {
	key := csvKey(raw)
	for _, r := range ReasonsFor(stage) {
		if key == csvKey(string(r.Value)) || key == csvKey(r.Label) {
			return r.Value, true
		}
	}
	return "", false
}

func sourceLabelList() string {
	labels := make([]string, len(Sources))
	for i, s := range Sources {
		labels[i] = s.Label()
	}
	return strings.Join(labels, ", ")
}

func stageLabelList() string {
	labels := make([]string, len(Stages))
	for i, s := range Stages {
		labels[i] = s.Label()
	}
	return strings.Join(labels, ", ")
}

// parseRupees reads an amount like "250000", "2,50,000", "₹2,50,000" or
// "Rs. 2,50,000.00". Paise are not kept, so a fractional amount is refused
// rather than rounded.
func parseRupees(raw string) (int64, bool) {
	s := strings.ToLower(strings.TrimSpace(raw))
	for _, prefix := range []string{"₹", "inr", "rs.", "rs"} {
		s = strings.TrimSpace(strings.TrimPrefix(s, prefix))
	}
	s = strings.NewReplacer(",", "", " ", "", " ", "").Replace(s)
	if s == "" {
		return 0, false
	}
	if value, err := strconv.ParseInt(s, 10, 64); err == nil {
		return value, true
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f != math.Trunc(f) || math.Abs(f) > float64(MaxDealValue) {
		return 0, false
	}
	return int64(f), true
}

// ownerMatcher finds team members by email, or by name when that is unique.
type ownerMatcher struct {
	byEmail map[string]int64
	byName  map[string][]int64
}

func newOwnerMatcher(members []TeamMember) ownerMatcher {
	m := ownerMatcher{byEmail: map[string]int64{}, byName: map[string][]int64{}}
	for _, member := range members {
		m.byEmail[strings.ToLower(member.Email)] = member.ID
		name := strings.ToLower(strings.Join(strings.Fields(member.Name), " "))
		m.byName[name] = append(m.byName[name], member.ID)
	}
	return m
}

// match returns the member's id, or a message saying why there is none. An
// Owner cell may hold an email too; an Owner email cell only an email.
func (m ownerMatcher) match(raw string, emailOnly bool) (int64, string) {
	if id, ok := m.byEmail[strings.ToLower(raw)]; ok {
		return id, ""
	}
	if !emailOnly {
		ids := m.byName[strings.ToLower(strings.Join(strings.Fields(raw), " "))]
		switch len(ids) {
		case 1:
			return ids[0], ""
		case 0:
		default:
			return 0, fmt.Sprintf("%q matches more than one team member; use their email instead", raw)
		}
	}
	return 0, fmt.Sprintf("%q is not on the Team page; add them first, or leave it empty", raw)
}

// ---------------------------------------------------------------------------
// Result
// ---------------------------------------------------------------------------

// ImportProblem is a row that was not imported, and why.
type ImportProblem struct {
	Line     int      `json:"line"`
	Name     string   `json:"name"`
	Email    string   `json:"email"`
	Messages []string `json:"messages"`
	Cells    []string `json:"cells"` // the row as written in the file
}

// ImportResult is the response of POST /api/leads/import. With DryRun, Ready
// is how many leads would be imported and nothing was saved; without it,
// Imported is how many were.
type ImportResult struct {
	DryRun     bool            `json:"dry_run"`
	TotalRows  int             `json:"total_rows"`
	Ready      int             `json:"ready"`
	Imported   int             `json:"imported"`
	Duplicates []ImportProblem `json:"duplicates"`
	Errors     []ImportProblem `json:"errors"`

	// The file's columns and the field each goes into, the fields there are
	// to choose from, and the required ones no column goes into yet. While
	// Missing is not empty, no row was checked and Ready is 0.
	Headers []ImportHeader `json:"headers"`
	Fields  []ImportField  `json:"fields"`
	Missing []string       `json:"missing"`
	// Sample is the first row's cells, to show what each column holds.
	Sample []string `json:"sample"`
}
