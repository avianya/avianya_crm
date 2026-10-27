package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"crm-lead-management/backend/internal/model"
	"crm-lead-management/backend/internal/store"
)

// GET /api/leads/export
//
// Every lead matching the same filters and sort as GET /api/leads, all pages
// at once (up to model.MaxScoredLeads), as a CSV download.
func (h *Handler) exportLeads(c *gin.Context) {
	query, errs := parseLeadQuery(c)
	if errs.Any() {
		respondValidationError(c, errs)
		return
	}
	query.Page, query.PageSize = 1, model.MaxScoredLeads

	ctx := c.Request.Context()
	leads, _, err := h.store.ListLeads(ctx, query)
	if err != nil {
		h.respondStoreError(c, err, "export leads")
		return
	}
	members, err := h.store.ListTeam(ctx)
	if err != nil {
		h.respondStoreError(c, err, "export leads")
		return
	}
	owners := make(map[int64]model.TeamMember, len(members))
	for _, m := range members {
		owners[m.ID] = m
	}

	filename := fmt.Sprintf("leads-%s.csv", time.Now().In(h.location).Format("2006-01-02"))
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Status(http.StatusOK)
	if err := model.WriteLeadsCSV(c.Writer, leads, owners, h.location); err != nil {
		// Too late for an error response: the header has gone. The download
		// ends short, and the log says why.
		h.logger.Error("export leads", "error", err.Error())
	}
}

// POST /api/leads/import?dry_run=true|false&run_automations=true|false&columns=[...]
//
// The body is the CSV file itself. columns, optional, is a JSON array giving
// the field for each column of the file by position ("" for none), as the
// preview's column picker chose them; without it they are worked out from the
// header names. A dry run checks every row and reports
// what would happen without saving anything, for the preview; otherwise each
// valid, new lead is created. Rows with errors, and emails that already
// belong to a lead (or appear earlier in the file), are reported and skipped.
func (h *Handler) importLeads(c *gin.Context) {
	dryRun := c.Query("dry_run") == "true"
	runAutomations := c.Query("run_automations") == "true"

	var mapping []string
	if raw := c.Query("columns"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &mapping); err != nil || mapping == nil {
			respondValidationError(c, model.ValidationErrors{"columns": "must be a JSON array of field names"})
			return
		}
	}

	data, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, model.MaxImportBytes))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			respondValidationError(c, model.ValidationErrors{
				"file": fmt.Sprintf("is larger than %d MB; split it into smaller files", model.MaxImportBytes>>20),
			})
			return
		}
		respondError(c, http.StatusBadRequest, codeBadRequest, "The file could not be read.")
		return
	}

	ctx := c.Request.Context()
	members, err := h.store.ListTeam(ctx)
	if err != nil {
		h.respondStoreError(c, err, "import leads")
		return
	}

	parsed, err := model.ParseLeadCSV(data, members, mapping)
	if err != nil {
		if errors.Is(err, model.ErrImportFile) {
			msg := strings.TrimPrefix(err.Error(), model.ErrImportFile.Error()+": ")
			respondValidationError(c, model.ValidationErrors{"file": msg})
			return
		}
		h.respondStoreError(c, err, "import leads")
		return
	}

	result := model.ImportResult{
		DryRun:     dryRun,
		TotalRows:  len(parsed.Rows),
		Duplicates: []model.ImportProblem{},
		Errors:     []model.ImportProblem{},
		Headers:    parsed.Headers,
		Fields:     model.ImportFields(),
		Missing:    parsed.Missing,
		Sample:     parsed.Rows[0].Cells, // ParseLeadCSV refuses a file with no rows
	}
	if result.Missing == nil {
		result.Missing = []string{}
	}

	// Until every required field has a column, no row can be checked: the
	// preview says which are missing and the import is refused.
	if len(parsed.Missing) > 0 {
		if !dryRun {
			respondValidationError(c, model.ValidationErrors{
				"file": "choose a column for " + strings.Join(parsed.Missing, ", "),
			})
			return
		}
		respondData(c, http.StatusOK, result)
		return
	}

	problem := func(row model.ImportRow, messages ...string) model.ImportProblem {
		return model.ImportProblem{
			Line: row.Line, Name: row.Input.Name, Email: row.Input.Email, Messages: messages, Cells: row.Cells,
		}
	}

	var emails []string
	for _, row := range parsed.Rows {
		if row.Errors == nil {
			emails = append(emails, row.Input.Email)
		}
	}
	existing, err := h.store.ExistingLeadEmails(ctx, emails)
	if err != nil {
		h.respondStoreError(c, err, "import leads")
		return
	}

	firstLine := map[string]int{} // email -> the line it first appeared on
	var ready []model.ImportRow
	for _, row := range parsed.Rows {
		switch {
		case row.Errors != nil:
			result.Errors = append(result.Errors, problem(row, row.Errors...))
		case existing[row.Input.Email]:
			result.Duplicates = append(result.Duplicates, problem(row, "Already a lead with this email"))
		case firstLine[row.Input.Email] != 0:
			result.Duplicates = append(result.Duplicates,
				problem(row, fmt.Sprintf("Same email as row %d of this file", firstLine[row.Input.Email])))
		default:
			firstLine[row.Input.Email] = row.Line
			ready = append(ready, row)
		}
	}
	result.Ready = len(ready)

	if dryRun {
		respondData(c, http.StatusOK, result)
		return
	}

	for _, row := range ready {
		_, err := h.store.ImportLead(ctx, row.Input, runAutomations)
		var fieldErr *store.FieldError
		switch {
		case err == nil:
			result.Imported++
		case errors.Is(err, store.ErrDuplicateEmail):
			// Added by someone else since the check above.
			result.Duplicates = append(result.Duplicates, problem(row, "Already a lead with this email"))
		case errors.As(err, &fieldErr):
			result.Errors = append(result.Errors,
				problem(row, fmt.Sprintf("%s: %s", fieldErr.Field, fieldErr.Message)))
		default:
			h.logger.Error("import leads", "line", row.Line, "imported", result.Imported, "error", err.Error())
			respondError(c, http.StatusInternalServerError, codeInternalError, fmt.Sprintf(
				"The import stopped at row %d because of a server error. %d leads before it were imported; "+
					"import the file again to add the rest (the ones already in are skipped as duplicates).",
				row.Line, result.Imported))
			return
		}
	}
	respondData(c, http.StatusOK, result)
}
