package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"crm-lead-management/backend/internal/model"
)

// GET /api/notifications
//
// The latest reminders, newest first, with the unread count. The bell polls it.
func (h *Handler) listNotifications(c *gin.Context) {
	list, err := h.store.ListNotifications(c.Request.Context(), model.MaxNotificationsListed)
	if err != nil {
		h.respondStoreError(c, err, "list notifications")
		return
	}
	respondData(c, http.StatusOK, list)
}

// GET /api/notifications/settings
//
// Whether reminder emails can go out, and from which address. Credentials are
// never included.
func (h *Handler) reminderSettings(c *gin.Context) {
	respondData(c, http.StatusOK, model.ReminderSettings{
		EmailConfigured: h.mailer.Configured(),
		From:            h.mailer.From(),
		Timezone:        h.location.String(),
		DigestHour:      h.cfg.DigestHour,
	})
}

// POST /api/notifications/:id/read
func (h *Handler) markNotificationRead(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}
	if err := h.store.MarkNotificationRead(c.Request.Context(), id); err != nil {
		h.respondStoreError(c, err, "mark notification read")
		return
	}
	c.Status(http.StatusNoContent)
}

// POST /api/deal-alerts/:id/read
func (h *Handler) markDealAlertRead(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}
	if err := h.store.MarkDealAlertRead(c.Request.Context(), id); err != nil {
		h.respondStoreError(c, err, "mark deal alert read")
		return
	}
	c.Status(http.StatusNoContent)
}

// POST /api/notifications/read-all (reminders and deal alerts alike)
func (h *Handler) markAllNotificationsRead(c *gin.Context) {
	if err := h.store.MarkAllNotificationsRead(c.Request.Context()); err != nil {
		h.respondStoreError(c, err, "mark all notifications read")
		return
	}
	c.Status(http.StatusNoContent)
}

// POST /api/notifications/:id/retry
//
// Queues a failed or skipped email again. The reminder job sends it on its next
// tick, re-checking everything first, so the response is the queued state, not
// a claim that it was delivered.
func (h *Handler) retryNotificationEmail(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}
	n, err := h.store.RetryReminderEmail(c.Request.Context(), id)
	if err != nil {
		h.respondStoreError(c, err, "retry reminder email")
		return
	}
	respondData(c, http.StatusOK, n)
}

// GET /api/leads/:id/next-steps
func (h *Handler) nextSteps(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}
	steps, err := h.store.LeadNextSteps(c.Request.Context(), id, time.Now(), h.location)
	if err != nil {
		h.respondStoreError(c, err, "next steps")
		return
	}
	respondData(c, http.StatusOK, steps)
}
