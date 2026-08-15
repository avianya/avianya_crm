package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"crm-lead-management/backend/internal/model"
)

// Handlers in this file all follow the same four steps:
//
//	1. parse and validate the input   -> 400 / 422 on failure
//	2. call one store method          -> the store owns all SQL
//	3. map any error to a status code -> respondStoreError
//	4. write the success envelope
//
// Keeping that shape uniform is what makes the layer skimmable: once you have
// read one handler you can predict the rest.

// ---------------------------------------------------------------------------
// GET /api/leads
// ---------------------------------------------------------------------------

func (h *Handler) listLeads(c *gin.Context) {
	query, errs := parseLeadQuery(c)
	if errs.Any() {
		respondValidationError(c, errs)
		return
	}

	leads, total, err := h.store.ListLeads(c.Request.Context(), query)
	if err != nil {
		h.respondStoreError(c, err, "list leads")
		return
	}

	respondList(c, leads, model.NewPageMeta(query, total))
}

// parseLeadQuery reads the filter/sort/page parameters off the URL.
//
// Two different policies here, on purpose:
//
//   - A bad *filter* value is an error. ?stage=bananas almost certainly means the
//     caller has a bug, and silently returning all leads would hide it.
//   - A bad *pagination* value is clamped. ?page=0 or ?page_size=99999 has an
//     obvious sane interpretation, and a 422 in the middle of browsing a list is
//     a worse experience than quietly showing page 1.
func parseLeadQuery(c *gin.Context) (model.LeadQuery, model.ValidationErrors) {
	errs := model.ValidationErrors{}

	q := model.LeadQuery{
		// "search" is the documented name; "q" is accepted as a short alias
		// because it is what people reach for by habit.
		Search:   strings.TrimSpace(firstNonEmpty(c.Query("search"), c.Query("q"))),
		SortBy:   "created_at",
		SortDir:  "desc",
		Page:     1,
		PageSize: model.DefaultPageSize,
	}

	if raw := strings.TrimSpace(c.Query("stage")); raw != "" && raw != "all" {
		stage := model.Stage(raw)
		if !stage.Valid() {
			errs["stage"] = fmt.Sprintf("%q is not a known stage", raw)
		} else {
			q.Stage = stage
		}
	}

	if raw := strings.TrimSpace(c.Query("source")); raw != "" && raw != "all" {
		source := model.Source(raw)
		if !source.Valid() {
			errs["source"] = fmt.Sprintf("%q is not a known source", raw)
		} else {
			q.Source = source
		}
	}

	if raw := strings.TrimSpace(c.Query("sort_by")); raw != "" {
		if !model.SortableLeadFields[raw] {
			errs["sort_by"] = fmt.Sprintf("%q is not a sortable field", raw)
		} else {
			q.SortBy = raw
		}
	}

	if raw := strings.ToLower(strings.TrimSpace(c.Query("sort_dir"))); raw != "" {
		if raw != "asc" && raw != "desc" {
			errs["sort_dir"] = `must be "asc" or "desc"`
		} else {
			q.SortDir = raw
		}
	}

	if page, err := strconv.Atoi(c.Query("page")); err == nil && page > 1 {
		q.Page = page
	}

	if size, err := strconv.Atoi(c.Query("page_size")); err == nil && size > 0 {
		// Capped so one request cannot ask the database for a million rows.
		q.PageSize = min(size, model.MaxPageSize)
	}

	return q, errs
}

// ---------------------------------------------------------------------------
// POST /api/leads
// ---------------------------------------------------------------------------

func (h *Handler) createLead(c *gin.Context) {
	var in model.LeadInput
	if !bindJSON(c, &in) {
		return
	}

	in.Normalize()
	if errs := in.Validate(); errs.Any() {
		respondValidationError(c, errs)
		return
	}

	lead, err := h.store.CreateLead(c.Request.Context(), in)
	if err != nil {
		h.respondStoreError(c, err, "create lead")
		return
	}

	// 201 Created plus a Location header pointing at the new resource: the
	// conventional REST answer to "what did you just make, and where is it?"
	c.Header("Location", fmt.Sprintf("/api/leads/%d", lead.ID))
	respondData(c, http.StatusCreated, lead)
}

// ---------------------------------------------------------------------------
// GET /api/leads/:id
// ---------------------------------------------------------------------------

func (h *Handler) getLead(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}

	// Returns the lead with its activities already attached, so the detail
	// screen renders from a single request.
	lead, err := h.store.GetLead(c.Request.Context(), id)
	if err != nil {
		h.respondStoreError(c, err, "get lead")
		return
	}

	respondData(c, http.StatusOK, lead)
}

// ---------------------------------------------------------------------------
// PUT /api/leads/:id
// ---------------------------------------------------------------------------

// updateLead replaces a lead's editable fields.
//
// PUT means "make the resource look like this", so every required field must be
// present -- omitting company blanks it rather than leaving it alone. That is the
// correct reading of PUT, and it is why the edit form loads the current values
// before submitting.
func (h *Handler) updateLead(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}

	var in model.LeadInput
	if !bindJSON(c, &in) {
		return
	}

	in.Normalize()
	if errs := in.Validate(); errs.Any() {
		respondValidationError(c, errs)
		return
	}

	lead, err := h.store.UpdateLead(c.Request.Context(), id, in)
	if err != nil {
		h.respondStoreError(c, err, "update lead")
		return
	}

	respondData(c, http.StatusOK, lead)
}

// ---------------------------------------------------------------------------
// PATCH /api/leads/:id/stage
// ---------------------------------------------------------------------------

// updateLeadStage moves a lead along the pipeline. The store records the move on
// the lead's timeline in the same transaction.
func (h *Handler) updateLeadStage(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}

	var in model.StageInput
	if !bindJSON(c, &in) {
		return
	}

	if errs := in.Validate(); errs.Any() {
		respondValidationError(c, errs)
		return
	}

	lead, err := h.store.UpdateLeadStage(c.Request.Context(), id, in.Stage)
	if err != nil {
		h.respondStoreError(c, err, "update lead stage")
		return
	}

	respondData(c, http.StatusOK, lead)
}

// ---------------------------------------------------------------------------
// DELETE /api/leads/:id
// ---------------------------------------------------------------------------

func (h *Handler) deleteLead(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}

	if err := h.store.DeleteLead(c.Request.Context(), id); err != nil {
		h.respondStoreError(c, err, "delete lead")
		return
	}

	// 204 No Content: the delete succeeded and there is nothing meaningful to
	// send back. Status() rather than JSON() because a 204 must have an empty body.
	c.Status(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Activities
// ---------------------------------------------------------------------------

// GET /api/leads/:id/activities
//
// The timeline also arrives inside GET /api/leads/:id; this exists so the detail
// screen can refresh just the timeline after logging something, without re-fetching
// the whole lead.
func (h *Handler) listActivities(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}

	activities, err := h.store.ListActivities(c.Request.Context(), id)
	if err != nil {
		h.respondStoreError(c, err, "list activities")
		return
	}

	respondData(c, http.StatusOK, activities)
}

// POST /api/leads/:id/activities
func (h *Handler) createActivity(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}

	var in model.ActivityInput
	if !bindJSON(c, &in) {
		return
	}

	in.Normalize()
	if errs := in.Validate(); errs.Any() {
		respondValidationError(c, errs)
		return
	}

	activity, err := h.store.CreateActivity(c.Request.Context(), id, in)
	if err != nil {
		// A missing lead surfaces as a foreign-key violation, which the store
		// has already translated into ErrNotFound -> 404.
		h.respondStoreError(c, err, "create activity")
		return
	}

	respondData(c, http.StatusCreated, activity)
}

// ---------------------------------------------------------------------------
// Small shared helpers
// ---------------------------------------------------------------------------

// parseIDParam reads the :id path segment as a positive int64.
//
// The bool return is the Go idiom for "did this work": on failure the response
// has already been written, so the caller just returns.
func parseIDParam(c *gin.Context) (int64, bool) {
	raw := c.Param("id")

	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		respondError(c, http.StatusBadRequest, codeBadRequest,
			fmt.Sprintf("%q is not a valid lead id.", raw))
		return 0, false
	}

	return id, true
}

// bindJSON decodes the request body into dst, reporting a 400 if it is not
// usable JSON. Like parseIDParam, false means "response already sent".
func bindJSON(c *gin.Context, dst any) bool {
	if err := c.ShouldBindJSON(dst); err != nil {
		respondError(c, http.StatusBadRequest, codeBadRequest,
			"Request body must be valid JSON: "+err.Error())
		return false
	}
	return true
}

// firstNonEmpty returns the first argument that is not the empty string.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
