package api

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"crm-lead-management/backend/internal/mail"
	"crm-lead-management/backend/internal/model"
)

// GET /api/email-templates
func (h *Handler) listEmailTemplates(c *gin.Context) {
	templates, err := h.store.ListEmailTemplates(c.Request.Context())
	if err != nil {
		h.respondStoreError(c, err, "list email templates")
		return
	}
	respondData(c, http.StatusOK, templates)
}

// POST /api/email-templates
func (h *Handler) createEmailTemplate(c *gin.Context) {
	var in model.EmailTemplateInput
	if !bindJSON(c, &in) {
		return
	}
	in.Normalize()
	if errs := in.Validate(); errs.Any() {
		respondValidationError(c, errs)
		return
	}

	template, err := h.store.CreateEmailTemplate(c.Request.Context(), in)
	if err != nil {
		h.respondStoreError(c, err, "create email template")
		return
	}
	respondData(c, http.StatusCreated, template)
}

// PUT /api/email-templates/:id
func (h *Handler) updateEmailTemplate(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}
	var in model.EmailTemplateInput
	if !bindJSON(c, &in) {
		return
	}
	in.Normalize()
	if errs := in.Validate(); errs.Any() {
		respondValidationError(c, errs)
		return
	}

	template, err := h.store.UpdateEmailTemplate(c.Request.Context(), id, in)
	if err != nil {
		h.respondStoreError(c, err, "update email template")
		return
	}
	respondData(c, http.StatusOK, template)
}

// DELETE /api/email-templates/:id
func (h *Handler) deleteEmailTemplate(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}
	if err := h.store.DeleteEmailTemplate(c.Request.Context(), id); err != nil {
		h.respondStoreError(c, err, "delete email template")
		return
	}
	c.Status(http.StatusNoContent)
}

// renderLeadEmail reads an EmailInput and fills it in for the lead in the URL.
// It writes the error response itself and returns ok=false on any problem.
func (h *Handler) renderLeadEmail(c *gin.Context) (in model.EmailInput, email model.RenderedEmail, ownerEmail string, ok bool) {
	id, ok := parseIDParam(c)
	if !ok {
		return in, email, "", false
	}
	if !bindJSON(c, &in) {
		return in, email, "", false
	}
	in.Normalize()
	if errs := in.Validate(); errs.Any() {
		respondValidationError(c, errs)
		return in, email, "", false
	}

	lead, ownerName, ownerEmail, err := h.store.LeadForEmail(c.Request.Context(), id)
	if err != nil {
		h.respondStoreError(c, err, "render lead email")
		return in, email, "", false
	}

	email = model.RenderEmail(in, lead, ownerName)
	if errs := email.Validate(); errs.Any() {
		respondValidationError(c, errs)
		return in, email, "", false
	}
	return in, email, ownerEmail, true
}

// POST /api/leads/:id/email/preview
//
// Fills in an email for the lead without sending or logging anything, so the
// person can check it first.
func (h *Handler) previewLeadEmail(c *gin.Context) {
	_, email, _, ok := h.renderLeadEmail(c)
	if !ok {
		return
	}
	respondData(c, http.StatusOK, email)
}

// POST /api/leads/:id/emails
//
// With "send": true, sends the filled-in email from the server and logs it on
// the timeline; replies go to the lead's owner. With "send": false it is only
// logged, for an email sent from the person's own email app. Either way it is
// an ordinary email activity, so it counts as contact for health and score.
//
// Nothing is logged when sending fails: the timeline must never claim an
// email went out that did not.
func (h *Handler) sendLeadEmail(c *gin.Context) {
	in, email, ownerEmail, ok := h.renderLeadEmail(c)
	if !ok {
		return
	}
	leadID, _ := parseIDParam(c) // already validated by renderLeadEmail

	if in.Send {
		if !h.mailer.Configured() {
			respondError(c, http.StatusConflict, codeConflict,
				"Email is not set up on the server, so it cannot send. Open it in your email app instead, or add the SMTP settings to backend/.env.")
			return
		}

		// Inside the server's 30s write timeout, as for the test email.
		ctx, cancel := context.WithTimeout(c.Request.Context(), 25*time.Second)
		defer cancel()
		err := h.mailer.SendMessage(ctx, mail.Message{
			To:      email.To,
			ReplyTo: ownerEmail,
			Subject: email.Subject,
			Body:    email.Body,
		})
		if err != nil {
			h.logger.Warn("lead email failed", "lead_id", leadID, "error", err.Error())
			respondError(c, http.StatusBadGateway, "email_failed", "The email could not be sent: "+err.Error())
			return
		}
	}

	defer h.refreshQuality(c, leadID) // an email logged counts as a follow-up
	activity, err := h.store.CreateActivity(c.Request.Context(), leadID, model.ActivityInput{
		Type: model.ActivityEmail,
		Note: email.TimelineNote(in.Send),
	})
	if err != nil {
		if in.Send {
			// The email has gone; only the record of it failed. Say exactly that,
			// so nobody sends it a second time.
			h.logger.Error("lead email sent but not logged", "lead_id", leadID, "error", err.Error())
			respondError(c, http.StatusInternalServerError, codeInternalError,
				"The email was sent, but it could not be added to the timeline. Log it by hand rather than sending it again.")
			return
		}
		h.respondStoreError(c, err, "log lead email")
		return
	}

	respondData(c, http.StatusCreated, gin.H{
		"activity": activity,
		"email":    email,
		"sent":     in.Send,
	})
}
