package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"crm-lead-management/backend/internal/model"
	"crm-lead-management/backend/internal/store"
)

// Customer links: the booking page and the thank-you page a lead opens
// without logging in (see model/customerlinks.go). The routes under
// /api/public/ are found by their token alone.

// linkURL is where the app shows a link of this kind.
func (h *Handler) linkURL(link *model.CustomerLink) {
	if link != nil {
		link.URL = fmt.Sprintf("%s/%s/%s", h.cfg.BaseURL, link.Kind, link.Token)
	}
}

// GET /api/leads/:id/links
func (h *Handler) customerLinks(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}
	links, err := h.store.CustomerLinks(c.Request.Context(), id)
	if err != nil {
		h.respondStoreError(c, err, "customer links")
		return
	}
	h.linkURL(links.Booking)
	h.linkURL(links.Feedback)
	respondData(c, http.StatusOK, links)
}

// POST /api/leads/:id/links/:kind: makes the link, or keeps the one there is.
func (h *Handler) createCustomerLink(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}
	kind := c.Param("kind")
	if !model.ValidLinkKind(kind) {
		respondValidationError(c, model.ValidationErrors{"kind": "must be booking or feedback"})
		return
	}
	if err := h.store.CreateCustomerLink(c.Request.Context(), id, kind); err != nil {
		h.respondStoreError(c, err, "create customer link")
		return
	}
	h.customerLinks(c)
}

// sendThankYou emails a lead that was just won their thank-you link, once,
// if email is set up and it was never sent. It runs after the response, so a
// slow mail server does not hold up the stage change.
func (h *Handler) sendThankYou(lead model.Lead) {
	if lead.Stage != model.StageWon || !h.mailer.Configured() {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		t, ok, err := h.store.ClaimThankYou(ctx, lead.ID)
		if err != nil || !ok {
			if err != nil {
				h.logger.Error("thank-you email", slog.Int64("lead_id", lead.ID), slog.String("error", err.Error()))
			}
			return
		}
		from := "the team"
		if t.OwnerName != "" {
			from = t.OwnerName
		}
		link := fmt.Sprintf("%s/%s/%s", h.cfg.BaseURL, model.LinkFeedback, t.Token)
		body := fmt.Sprintf("Hi %s,\n\nThank you for choosing us. It was a pleasure working with you.\n\n"+
			"Could you tell us how we did? It takes a minute:\n\n%s\n\n"+
			"If you know someone we could help too, you can pass on their details on the same page.\n\n"+
			"Thank you,\n%s\n", firstWord(t.LeadName), link, from)
		if err := h.mailer.Send(ctx, t.Email, "Thank you! How did we do?", body); err != nil {
			h.logger.Error("send thank-you email", slog.Int64("lead_id", lead.ID), slog.String("error", err.Error()))
			if err := h.store.ReleaseThankYou(ctx, lead.ID); err != nil {
				h.logger.Error("release thank-you", slog.Int64("lead_id", lead.ID), slog.String("error", err.Error()))
			}
		}
	}()
}

func firstWord(s string) string {
	for i, r := range s {
		if r == ' ' {
			return s[:i]
		}
	}
	return s
}

// --- public pages ------------------------------------------------------------

// GET /api/public/booking/:token
func (h *Handler) bookingPage(c *gin.Context) {
	page, err := h.store.BookingPage(c.Request.Context(), c.Param("token"))
	if err != nil {
		h.respondLinkError(c, err, "booking page")
		return
	}
	respondData(c, http.StatusOK, page)
}

// POST /api/public/booking/:token {"date", "time", "note"}
func (h *Handler) bookMeeting(c *gin.Context) {
	var in model.BookingInput
	if !bindJSON(c, &in) {
		return
	}
	in.Normalize()
	if errs := in.Validate(); errs.Any() {
		respondValidationError(c, errs)
		return
	}
	booked, err := h.store.BookMeeting(c.Request.Context(), c.Param("token"), in)
	if err != nil {
		h.respondLinkError(c, err, "book meeting")
		return
	}
	respondData(c, http.StatusCreated, booked)
}

// GET /api/public/feedback/:token
func (h *Handler) feedbackPage(c *gin.Context) {
	page, err := h.store.FeedbackPage(c.Request.Context(), c.Param("token"))
	if err != nil {
		h.respondLinkError(c, err, "feedback page")
		return
	}
	respondData(c, http.StatusOK, page)
}

// POST /api/public/feedback/:token {"rating", "comment", "referral"}
func (h *Handler) giveFeedback(c *gin.Context) {
	var in model.FeedbackInput
	if !bindJSON(c, &in) {
		return
	}
	in.Normalize()
	if errs := in.Validate(); errs.Any() {
		respondValidationError(c, errs)
		return
	}
	if err := h.store.GiveFeedback(c.Request.Context(), c.Param("token"), in); err != nil {
		h.respondLinkError(c, err, "give feedback")
		return
	}
	respondData(c, http.StatusCreated, gin.H{"message": "Thank you for your feedback."})
}

// respondLinkError words errors for someone who is not logged in.
func (h *Handler) respondLinkError(c *gin.Context, err error, action string) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		respondError(c, http.StatusNotFound, codeNotFound, "This link is not valid. Please ask for a new one.")
	case errors.Is(err, store.ErrAlreadyGiven):
		respondError(c, http.StatusConflict, codeConflict, "You have already sent your feedback. Thank you!")
	default:
		h.respondStoreError(c, err, action)
	}
}
