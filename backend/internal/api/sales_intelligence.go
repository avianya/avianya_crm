package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// salesIntelligence returns full analytics for Lead Scoring, Sales Funnel, and Source ROI.
func (h *Handler) salesIntelligence(c *gin.Context) {
	report, err := h.store.SalesIntelligence(c.Request.Context())
	if err != nil {
		h.respondStoreError(c, err, "sales intelligence")
		return
	}
	respondData(c, http.StatusOK, report)
}

// seedIntelligence populates reliable sample lead, activity, and stage history data.
func (h *Handler) seedIntelligence(c *gin.Context) {
	if err := h.store.SeedIntelligenceData(c.Request.Context()); err != nil {
		h.respondStoreError(c, err, "seed intelligence")
		return
	}

	report, err := h.store.SalesIntelligence(c.Request.Context())
	if err != nil {
		respondData(c, http.StatusOK, gin.H{
			"message": "Demo data seeded successfully.",
		})
		return
	}

	respondData(c, http.StatusOK, gin.H{
		"message":     "Sales intelligence dataset seeded successfully with realistic leads, funnel history, and activity touches.",
		"total_leads": report.Summary.TotalLeads,
		"report":      report,
	})
}
