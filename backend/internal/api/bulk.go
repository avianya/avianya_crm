package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"crm-lead-management/backend/internal/model"
	"crm-lead-management/backend/internal/store"
)

// POST /api/leads/bulk
//
// Applies one change (owner, stage, source or delete) to each of the listed
// leads, through the same store method as the single-lead endpoints, so every
// lead gets its own timeline entry. Each lead is its own transaction: one that
// fails, say a lead moved to Lost that needs a reason, is reported and the
// rest still change. Always 200 once the input is valid; see Failed.
func (h *Handler) bulkUpdateLeads(c *gin.Context) {
	var in model.BulkInput
	if !bindJSON(c, &in) {
		return
	}
	in.Normalize()
	if errs := in.Validate(); errs.Any() {
		respondValidationError(c, errs)
		return
	}

	ctx := c.Request.Context()
	// Reassigning and deleting are for admins and managers.
	viewer := viewerOf(c)
	if !viewer.CanAssign() && (in.Action == model.BulkOwner || in.Action == model.BulkDelete) {
		respondError(c, http.StatusForbidden, codeForbidden, "Your role does not allow reassigning or deleting leads.")
		return
	}
	stage := model.StageInput{Stage: in.Stage, CloseInput: in.CloseInput}
	result := model.BulkResult{Action: in.Action, Failed: []model.BulkFailure{}}

	for _, id := range in.IDs {
		var err error
		// A lead the viewer may not see is reported exactly like one that does
		// not exist, and left alone.
		if !viewer.SeesAllLeads() {
			visible, verr := h.store.LeadVisible(ctx, id)
			if verr != nil {
				h.respondStoreError(c, verr, "bulk update")
				return
			}
			if !visible {
				result.Failed = append(result.Failed, model.BulkFailure{ID: id, Message: h.bulkFailureMessage(store.ErrNotFound, in.Action, id)})
				continue
			}
		}
		switch in.Action {
		case model.BulkOwner:
			_, err = h.store.UpdateLeadOwner(ctx, id, in.OwnerID)
		case model.BulkStage:
			var lead model.Lead
			if lead, err = h.store.UpdateLeadStage(ctx, id, stage); err == nil {
				h.sendThankYou(lead)
			}
		case model.BulkSource:
			err = h.store.UpdateLeadSource(ctx, id, in.Source)
		case model.BulkDelete:
			err = h.store.DeleteLead(ctx, id)
		}

		if err == nil {
			result.Changed++
			continue
		}
		if ctx.Err() != nil {
			// The person gave up waiting; stop rather than change leads nobody
			// will hear about.
			return
		}
		result.Failed = append(result.Failed, model.BulkFailure{ID: id, Message: h.bulkFailureMessage(err, in.Action, id)})
	}

	respondData(c, http.StatusOK, result)
}

// bulkFailureMessage says in a sentence why one lead could not be changed.
func (h *Handler) bulkFailureMessage(err error, action model.BulkAction, id int64) string {
	var fieldErr *store.FieldError
	switch {
	case errors.Is(err, store.ErrNotFound):
		return "This lead no longer exists."
	case errors.As(err, &fieldErr):
		field := strings.ReplaceAll(fieldErr.Field, "_", " ")
		return strings.ToUpper(field[:1]) + field[1:] + " " + fieldErr.Message + "."
	default:
		h.logger.Error("bulk lead change", "action", string(action), "lead_id", id, "error", err.Error())
		return "Something went wrong on our side."
	}
}
