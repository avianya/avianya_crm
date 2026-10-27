package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"crm-lead-management/backend/internal/model"
)

// GET /api/team
func (h *Handler) listTeam(c *gin.Context) {
	members, err := h.store.ListTeam(c.Request.Context())
	if err != nil {
		h.respondStoreError(c, err, "list team")
		return
	}
	respondData(c, http.StatusOK, members)
}

// POST /api/team
func (h *Handler) createTeamMember(c *gin.Context) {
	var in model.TeamMemberInput
	if !bindJSON(c, &in) {
		return
	}
	in.Normalize()
	if errs := in.Validate(); errs.Any() {
		respondValidationError(c, errs)
		return
	}

	member, err := h.store.CreateTeamMember(c.Request.Context(), in)
	if err != nil {
		h.respondStoreError(c, err, "create team member")
		return
	}
	respondData(c, http.StatusCreated, member)
}

// PUT /api/team/:id
func (h *Handler) updateTeamMember(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}
	var in model.TeamMemberInput
	if !bindJSON(c, &in) {
		return
	}
	in.Normalize()
	if errs := in.Validate(); errs.Any() {
		respondValidationError(c, errs)
		return
	}

	member, err := h.store.UpdateTeamMember(c.Request.Context(), id, in)
	if err != nil {
		h.respondStoreError(c, err, "update team member")
		return
	}
	respondData(c, http.StatusOK, member)
}

// DELETE /api/team/:id
//
// Their leads become unowned and their events unassigned; nothing else is
// deleted.
func (h *Handler) deleteTeamMember(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	if err := h.store.CanDeleteMember(ctx, id); err != nil {
		h.respondStoreError(c, err, "delete team member")
		return
	}
	if err := h.store.DeleteTeamMember(ctx, id); err != nil {
		h.respondStoreError(c, err, "delete team member")
		return
	}
	h.audit(c, "account.deleted", map[string]any{"member_id": id})
	c.Status(http.StatusNoContent)
}

// POST /api/team/:id/test-email
//
// Sends a real email to the member, so the SMTP settings can be checked before
// a reminder depends on them. Responds 200 only when the mail server accepted
// it; otherwise 502 with the server's reason (never the password).
func (h *Handler) sendTestEmail(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}
	member, err := h.store.GetTeamMember(c.Request.Context(), id)
	if err != nil {
		h.respondStoreError(c, err, "test email")
		return
	}
	if !h.mailer.Configured() {
		respondError(c, http.StatusConflict, codeConflict,
			"Email is not set up on the server. Add the SMTP settings to backend/.env and restart it.")
		return
	}

	// Inside the server's 30s write timeout, so a hanging mail server still
	// gets a proper error back to the browser.
	ctx, cancel := context.WithTimeout(c.Request.Context(), 25*time.Second)
	defer cancel()

	body := "Hi " + member.Name + ",\n\n" +
		"This is a test from Pipeline CRM. If you are reading it, reminder emails will reach you at this address.\n\n" +
		"-- \nSent from the Team page.\n"
	if err := h.mailer.Send(ctx, member.Email, "Pipeline CRM test email", body); err != nil {
		h.logger.Warn("test email failed", "to", member.Email, "error", err.Error())
		respondError(c, http.StatusBadGateway, "email_failed", "The email could not be sent: "+err.Error())
		return
	}

	respondData(c, http.StatusOK, gin.H{"sent_to": member.Email})
}

// POST /api/team/:id/digest
//
// Sends the member's daily summary now, as it stands, even when there is
// nothing to report -- so someone can see what it looks like. It does not
// count as that day's summary. 200 only when the mail server accepted it.
func (h *Handler) sendDigestNow(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}
	member, err := h.store.GetTeamMember(c.Request.Context(), id)
	if err != nil {
		h.respondStoreError(c, err, "send digest")
		return
	}
	if !h.mailer.Configured() {
		respondError(c, http.StatusConflict, codeConflict,
			"Email is not set up on the server. Add the SMTP settings to backend/.env and restart it.")
		return
	}

	summary, err := h.store.FollowUps(c.Request.Context(), model.OwnerFilter{OwnerID: &member.ID})
	if err != nil {
		h.respondStoreError(c, err, "send digest")
		return
	}
	subject, body := model.DigestEmail(model.DigestEmailData{
		RecipientName: member.Name,
		Summary:       summary,
		FollowUpsURL:  fmt.Sprintf("%s/follow-ups?owner=%d", h.cfg.BaseURL, member.ID),
		Location:      h.location,
	})

	// Inside the server's 30s write timeout, as for the test email.
	ctx, cancel := context.WithTimeout(c.Request.Context(), 25*time.Second)
	defer cancel()
	if err := h.mailer.Send(ctx, member.Email, subject, body); err != nil {
		h.logger.Warn("digest email failed", "to", member.Email, "error", err.Error())
		respondError(c, http.StatusBadGateway, "email_failed", "The email could not be sent: "+err.Error())
		return
	}

	respondData(c, http.StatusOK, gin.H{"sent_to": member.Email})
}
