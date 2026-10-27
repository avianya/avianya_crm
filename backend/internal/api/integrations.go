package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"crm-lead-management/backend/internal/model"
)

// recentAdLeads is how many received submissions the Integrations page lists.
const recentAdLeads = 50

// integrationStatus says whether one platform is set up, and where its
// webhook is. Secrets themselves are never returned, only whether they are set.
type integrationStatus struct {
	Configured bool     `json:"configured"`
	Missing    []string `json:"missing"`     // the .env settings still empty
	WebhookURL string   `json:"webhook_url"` // "" until PUBLIC_API_URL is set
	Path       string   `json:"path"`        // the webhook's path on this server
}

func status(path string, publicURL string, required map[string]string, order []string) integrationStatus {
	st := integrationStatus{Path: path, Missing: []string{}}
	for _, name := range order {
		if required[name] == "" {
			st.Missing = append(st.Missing, name)
		}
	}
	st.Configured = len(st.Missing) == 0
	if publicURL != "" {
		st.WebhookURL = publicURL + path
	}
	return st
}

// GET /api/integrations
func (h *Handler) integrations(c *gin.Context) {
	recent, err := h.store.ListAdLeads(c.Request.Context(), recentAdLeads)
	if err != nil {
		h.respondStoreError(c, err, "list ad leads")
		return
	}
	// Meta's credentials may be saved on its settings page or in .env.
	rt, err := h.metaAds.Runtime(c.Request.Context())
	if err != nil {
		h.respondStoreError(c, err, "meta settings")
		return
	}
	meta := rt.Secrets
	respondData(c, http.StatusOK, gin.H{
		"public_url": h.cfg.PublicAPIURL,
		"meta": status("/api/webhooks/meta", h.cfg.PublicAPIURL, map[string]string{
			"META_APP_SECRET":        meta.AppSecret,
			"META_VERIFY_TOKEN":      meta.VerifyToken,
			"META_PAGE_ACCESS_TOKEN": meta.PageAccessToken,
		}, []string{"META_APP_SECRET", "META_VERIFY_TOKEN", "META_PAGE_ACCESS_TOKEN"}),
		"google": status("/api/webhooks/google", h.cfg.PublicAPIURL, map[string]string{
			"GOOGLE_LEADS_KEY": h.cfg.GoogleLeadsKey,
		}, []string{"GOOGLE_LEADS_KEY"}),
		"recent": recent,
	})
}

// POST /api/integrations/test {"provider": "meta" | "google"}
//
// Runs a made-up submission through the same steps a real one takes. As a
// test it is recorded but becomes no lead, whether or not the platform is set
// up: it shows the CRM side works before Meta or Google are connected.
func (h *Handler) sendTestAdLead(c *gin.Context) {
	var in struct {
		Provider model.AdProvider `json:"provider"`
	}
	if !bindJSON(c, &in) {
		return
	}
	if !in.Provider.Valid() {
		respondValidationError(c, model.ValidationErrors{"provider": `must be "meta" or "google"`})
		return
	}
	record, err := h.store.ReceiveAdLead(c.Request.Context(), model.SampleSubmission(in.Provider, time.Now()))
	if err != nil {
		h.respondStoreError(c, err, "test ad lead")
		return
	}
	respondData(c, http.StatusCreated, record)
}

// POST /api/ad-leads/:id/retry
//
// Tries a failed submission again: a Meta one whose answers never arrived is
// fetched again, any other is taken in again from the answers recorded (after
// the form or the CRM was fixed, say). Anything not failed is left alone.
func (h *Handler) retryAdLead(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	record, err := h.store.GetAdLead(ctx, id)
	if err != nil {
		h.respondStoreError(c, err, "retry ad lead")
		return
	}
	if record.Status != model.AdLeadFailed {
		respondError(c, http.StatusConflict, codeConflict, "Only a failed lead can be retried.")
		return
	}

	if record.Provider == model.ProviderMeta && len(record.Answers) == 0 {
		rt, rerr := h.metaAds.Runtime(ctx)
		if rerr != nil {
			h.respondStoreError(c, rerr, "meta settings")
			return
		}
		if rt.Secrets.PageAccessToken == "" {
			respondError(c, http.StatusConflict, codeConflict, "Meta leads are not set up, so its answers cannot be fetched.")
			return
		}
		record, err = h.metaAds.RetryLead(ctx, rt, record)
	} else {
		record, err = h.store.ReceiveAdLead(ctx, record.Submission())
	}
	if err != nil {
		h.respondStoreError(c, err, "retry ad lead")
		return
	}
	respondData(c, http.StatusOK, record)
}
