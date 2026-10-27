package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"crm-lead-management/backend/internal/model"
)

// GET /api/lead-assignment
func (h *Handler) leadAssignment(c *gin.Context) {
	setting, err := h.store.GetLeadAssignment(c.Request.Context())
	if err != nil {
		h.respondStoreError(c, err, "get lead assignment")
		return
	}
	respondData(c, http.StatusOK, setting)
}

// PUT /api/lead-assignment {"enabled", "member_ids"}: also how it is switched
// on or off.
func (h *Handler) saveLeadAssignment(c *gin.Context) {
	var in model.LeadAssignmentInput
	if !bindJSON(c, &in) {
		return
	}
	in.Normalize()
	if errs := in.Validate(); errs.Any() {
		respondValidationError(c, errs)
		return
	}
	setting, err := h.store.SaveLeadAssignment(c.Request.Context(), in)
	if err != nil {
		h.respondStoreError(c, err, "save lead assignment")
		return
	}
	respondData(c, http.StatusOK, setting)
}
