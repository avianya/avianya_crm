package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"crm-lead-management/backend/internal/model"
)

// GET /api/follow-ups?owner=
//
// The Follow-up Center: overdue, today's and this week's open tasks, plus the
// open leads going cold or with no next step. ?owner= narrows to one person
// (a member's id) or to nobody ("none"), as on the leads list.
func (h *Handler) followUps(c *gin.Context) {
	owner, ok := parseOwnerParam(c)
	if !ok {
		respondValidationError(c, model.ValidationErrors{"owner": ownerParamError})
		return
	}

	summary, err := h.store.FollowUps(c.Request.Context(), owner)
	if err != nil {
		h.respondStoreError(c, err, "follow-ups")
		return
	}

	respondData(c, http.StatusOK, summary)
}
