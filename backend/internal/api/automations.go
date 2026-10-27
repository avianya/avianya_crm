package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"crm-lead-management/backend/internal/model"
)

// GET /api/automations
func (h *Handler) listAutomationRules(c *gin.Context) {
	rules, err := h.store.ListAutomationRules(c.Request.Context())
	if err != nil {
		h.respondStoreError(c, err, "list automation rules")
		return
	}
	respondData(c, http.StatusOK, rules)
}

// bindAutomationRule reads and checks a rule from the body. It writes the
// error response itself and returns ok=false on any problem.
func bindAutomationRule(c *gin.Context) (in model.AutomationRuleInput, ok bool) {
	if !bindJSON(c, &in) {
		return in, false
	}
	in.Normalize()
	if errs := in.Validate(); errs.Any() {
		respondValidationError(c, errs)
		return in, false
	}
	return in, true
}

// POST /api/automations
func (h *Handler) createAutomationRule(c *gin.Context) {
	in, ok := bindAutomationRule(c)
	if !ok {
		return
	}
	rule, err := h.store.CreateAutomationRule(c.Request.Context(), in)
	if err != nil {
		h.respondStoreError(c, err, "create automation rule")
		return
	}
	respondData(c, http.StatusCreated, rule)
}

// PUT /api/automations/:id — also how a rule is switched on or off.
func (h *Handler) updateAutomationRule(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}
	in, ok := bindAutomationRule(c)
	if !ok {
		return
	}
	rule, err := h.store.UpdateAutomationRule(c.Request.Context(), id, in)
	if err != nil {
		h.respondStoreError(c, err, "update automation rule")
		return
	}
	respondData(c, http.StatusOK, rule)
}

// DELETE /api/automations/:id
func (h *Handler) deleteAutomationRule(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}
	if err := h.store.DeleteAutomationRule(c.Request.Context(), id); err != nil {
		h.respondStoreError(c, err, "delete automation rule")
		return
	}
	c.Status(http.StatusNoContent)
}
