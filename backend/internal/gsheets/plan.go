package gsheets

import (
	"errors"
	"strconv"
	"strings"

	"crm-lead-management/backend/internal/model"
)

// ErrTabInUse: the tab holds data the CRM did not write. The CRM leaves it
// alone rather than write over it.
var ErrTabInUse = errors.New("This tab already has other data in it, so the CRM will not write to it. Choose an empty tab, or name a new one in Configure.")

// Row is one lead's row, keyed by its CRM lead id.
type Row struct {
	LeadID int64
	Values []string // the first value is the lead id
}

// RowWrite is a lead row to write over, at its 1-based row number.
type RowWrite struct {
	Row    int
	Values []string
}

// Plan is what one sync writes.
type Plan struct {
	Header  []string   // written to row 1
	Updates []RowWrite // leads already in the sheet
	Appends [][]string // leads new to the sheet, added below
	// DuplicateIDs are lead ids found on more than one row: the first row is
	// updated, the others left as they are.
	DuplicateIDs []int64
}

// MakePlan works out a sync from the tab's row 1 (header), column A
// (existing, row 1 first), whether the whole tab is empty, and the rows to
// export.
//
// The CRM owns a tab whose A1 is its lead-id header, or an empty tab. It then
// updates the rows whose column A is a lead id, and adds rows for the rest.
// Rows it did not write (anything else in column A) are never touched, and
// neither are cells to the right of the CRM's columns. When the admin chose
// fewer columns than last time, the CRM's old columns are blanked rather than
// left showing stale values.
func MakePlan(header, columnA []string, tabEmpty bool, columns []string, rows []Row) (Plan, error) {
	empty := tabEmpty && len(columnA) == 0 && allBlank(header)
	ours := len(header) > 0 && strings.TrimSpace(header[0]) == model.SheetFieldLabel(model.SheetFieldLeadID)
	if !empty && !ours {
		return Plan{}, ErrTabInUse
	}

	labels := make([]string, len(columns))
	for i, c := range columns {
		labels[i] = model.SheetFieldLabel(c)
	}
	// How many of the old header's leading cells are CRM columns.
	oldWidth := 0
	for oldWidth < len(header) && model.IsSheetFieldLabel(strings.TrimSpace(header[oldWidth])) {
		oldWidth++
	}
	width := max(len(labels), oldWidth)

	plan := Plan{Header: pad(labels, width)}

	existing := map[int64]int{}
	for i := 1; i < len(columnA); i++ { // row 1 is the header
		id, err := strconv.ParseInt(strings.TrimSpace(columnA[i]), 10, 64)
		if err != nil || id <= 0 {
			continue
		}
		if _, seen := existing[id]; seen {
			plan.DuplicateIDs = append(plan.DuplicateIDs, id)
			continue
		}
		existing[id] = i + 1
	}

	for _, r := range rows {
		if at, ok := existing[r.LeadID]; ok {
			plan.Updates = append(plan.Updates, RowWrite{Row: at, Values: pad(r.Values, width)})
		} else {
			plan.Appends = append(plan.Appends, r.Values)
		}
	}
	return plan, nil
}

func allBlank(cells []string) bool {
	for _, c := range cells {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}

func pad(values []string, width int) []string {
	if len(values) >= width {
		return values
	}
	out := make([]string, width)
	copy(out, values)
	return out
}

// ColumnName is a 1-based column number as a letter: 1 is A, 27 is AA.
func ColumnName(n int) string {
	name := ""
	for n > 0 {
		n--
		name = string(rune('A'+n%26)) + name
		n /= 26
	}
	return name
}
