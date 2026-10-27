package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"crm-lead-management/backend/internal/meta"
	"crm-lead-management/backend/internal/metaads"
	"crm-lead-management/backend/internal/model"
	"crm-lead-management/backend/internal/secrets"
)

// Settings > Marketing Integrations > Meta CAPI. The Conversions API half of
// the Meta integration, on the same stored settings as the lead-ads half
// (/api/meta/settings): saving one never touches the other's fields or
// credentials. Every route here needs the admin key.

// capiView is GET /api/integrations/meta-capi. It carries no secret: the
// access token is only ever described (set, masked, where from).
type capiView struct {
	DatasetID        string             `json:"dataset_id"`
	AppID            string             `json:"app_id"`
	TestEventCode    string             `json:"test_event_code"`
	TestMode         bool               `json:"test_mode"`
	Enabled          bool               `json:"enabled"`
	EventMapping     []model.StageEvent `json:"event_mapping"`
	CRMName          string             `json:"crm_name"`
	PhoneCountryCode string             `json:"phone_country_code"`

	AccessToken     model.SecretState `json:"access_token"`
	Status          model.CAPIHealth  `json:"status"`
	StatusDetail    string            `json:"status_detail"`
	StatusAt        *time.Time        `json:"status_at"`
	TokenRotatedAt  *time.Time        `json:"token_rotated_at"`
	EncryptionReady bool              `json:"encryption_ready"`
	SecretsLocked   bool              `json:"secrets_locked"`
	// Counts are the delivery log's totals by status.
	Counts map[model.CAPIStatus]int `json:"counts"`
}

func (h *Handler) capiViewFor(c *gin.Context) (capiView, bool) {
	ctx := c.Request.Context()
	rt, err := h.metaAds.Runtime(ctx)
	if err != nil {
		h.respondStoreError(c, err, "meta capi settings")
		return capiView{}, false
	}
	counts, err := h.store.CAPIStatusCounts(ctx)
	if err != nil {
		h.respondStoreError(c, err, "count conversion events")
		return capiView{}, false
	}
	cfg := rt.Config
	token := model.SecretState{}
	if rt.Secrets.CAPIToken != "" {
		token = model.SecretState{Set: true, Source: rt.Sources["capi_token"], Hint: rt.Record.SecretHints["capi_token"]}
		if token.Hint == "" {
			token.Hint = secrets.Mask(rt.Secrets.CAPIToken)
		}
	}
	return capiView{
		DatasetID: cfg.DatasetID, AppID: cfg.AppID, TestEventCode: cfg.TestEventCode, TestMode: cfg.TestMode,
		Enabled: cfg.CAPIEnabled, EventMapping: cfg.EventMapping, CRMName: cfg.CRMName,
		PhoneCountryCode: cfg.PhoneCountryCode,
		AccessToken:      token,
		Status:           metaads.CAPIHealth(rt),
		StatusDetail:     rt.Record.CAPIHealthDetail,
		StatusAt:         rt.Record.CAPIHealthAt,
		TokenRotatedAt:   rt.Record.CAPITokenRotatedAt,
		EncryptionReady:  len(h.cfg.SecretsKey) > 0,
		SecretsLocked:    rt.Record.SecretsLocked,
		Counts:           counts,
	}, true
}

// GET /api/integrations/meta-capi
func (h *Handler) getMetaCAPI(c *gin.Context) {
	if view, ok := h.capiViewFor(c); ok {
		respondData(c, http.StatusOK, view)
	}
}

// capiInput is PUT /api/integrations/meta-capi. access_token is optional: left
// empty, the saved token is kept.
type capiInput struct {
	DatasetID        string             `json:"dataset_id"`
	AppID            string             `json:"app_id"`
	TestEventCode    string             `json:"test_event_code"`
	TestMode         bool               `json:"test_mode"`
	Enabled          bool               `json:"enabled"`
	EventMapping     []model.StageEvent `json:"event_mapping"`
	CRMName          string             `json:"crm_name"`
	PhoneCountryCode string             `json:"phone_country_code"`
	AccessToken      string             `json:"access_token"`
}

// PUT /api/integrations/meta-capi
//
// Saves the Conversions API settings onto the stored ones, leaving the
// lead-ads settings and every other credential as they are.
func (h *Handler) saveMetaCAPI(c *gin.Context) {
	var in capiInput
	if !bindJSON(c, &in) {
		return
	}
	ctx := c.Request.Context()
	rt, err := h.metaAds.Runtime(ctx)
	if err != nil {
		h.respondStoreError(c, err, "meta capi settings")
		return
	}

	cfg := rt.Config
	cfg.DatasetID, cfg.AppID, cfg.TestEventCode, cfg.TestMode = in.DatasetID, in.AppID, in.TestEventCode, in.TestMode
	cfg.CAPIEnabled, cfg.EventMapping, cfg.CRMName, cfg.PhoneCountryCode = in.Enabled, in.EventMapping, in.CRMName, in.PhoneCountryCode
	full := model.MetaSettingsInput{Config: cfg, Secrets: model.MetaSecrets{CAPIToken: in.AccessToken}}
	full.Normalize()
	errs := full.Validate()
	if full.Config.DatasetID == "" {
		errs["config.dataset_id"] = "is required: the Meta Pixel / Dataset ID events are sent to"
	}
	if full.Secrets.CAPIToken == "" && rt.Secrets.CAPIToken == "" {
		errs["secrets.capi_token"] = "is required: no Conversions API access token is saved yet"
	}
	if errs.Any() {
		respondValidationError(c, errs)
		return
	}
	if _, err := h.store.SaveMetaSettings(ctx, full.Config, full.Secrets); err != nil {
		h.respondStoreError(c, err, "save meta capi settings")
		return
	}
	h.audit(c, "meta_capi.settings_saved", map[string]any{
		"dataset_id": full.Config.DatasetID, "app_id": full.Config.AppID, "enabled": full.Config.CAPIEnabled,
		"test_mode": full.Config.TestMode, "event_mapping": full.Config.EventMapping,
		"access_token": replacedOrKept(full.Secrets.CAPIToken),
	})
	h.getMetaCAPI(c)
}

// PUT /api/integrations/meta-capi/token {"access_token": "..."}
//
// Update Access Token: replaces just the token (a rotation), which resets the
// status until it is tested or used.
func (h *Handler) rotateMetaCAPIToken(c *gin.Context) {
	var in struct {
		AccessToken string `json:"access_token"`
	}
	if !bindJSON(c, &in) {
		return
	}
	in.AccessToken = strings.TrimSpace(in.AccessToken)
	if in.AccessToken == "" || len(in.AccessToken) > 2000 || strings.ContainsAny(in.AccessToken, " \t\r\n") {
		respondValidationError(c, model.ValidationErrors{"access_token": "must be a single token with no spaces"})
		return
	}
	ctx := c.Request.Context()
	rt, err := h.metaAds.Runtime(ctx)
	if err != nil {
		h.respondStoreError(c, err, "meta capi settings")
		return
	}
	if _, err := h.store.SaveMetaSettings(ctx, rt.Config, model.MetaSecrets{CAPIToken: in.AccessToken}); err != nil {
		h.respondStoreError(c, err, "rotate meta capi token")
		return
	}
	h.audit(c, "meta_capi.token_rotated", map[string]any{"access_token": "replaced"})
	h.getMetaCAPI(c)
}

// POST /api/integrations/meta-capi/disable
//
// Disable Integration: stops sending. Queued events wait as Pending, and the
// token stays saved, so enabling again carries on where it stopped.
func (h *Handler) disableMetaCAPI(c *gin.Context) {
	ctx := c.Request.Context()
	rt, err := h.metaAds.Runtime(ctx)
	if err != nil {
		h.respondStoreError(c, err, "meta capi settings")
		return
	}
	cfg := rt.Config
	cfg.CAPIEnabled = false
	if _, err := h.store.SaveMetaSettings(ctx, cfg, model.MetaSecrets{}); err != nil {
		h.respondStoreError(c, err, "disable meta capi")
		return
	}
	h.audit(c, "meta_capi.disabled", nil)
	h.getMetaCAPI(c)
}

// POST /api/integrations/meta-capi/test: a real call that sends no event.
func (h *Handler) testMetaCAPI(c *gin.Context) {
	result, err := h.metaAds.TestCAPI(c.Request.Context())
	if err != nil {
		h.respondMetaError(c, err, "test meta capi")
		return
	}
	h.audit(c, "meta_capi.tested", map[string]any{"ok": result.OK, "status": result.Status})
	respondData(c, http.StatusOK, result)
}

// POST /api/integrations/meta-capi/test-event: one event with the test event
// code, which Meta shows only under Events Manager > Test Events.
func (h *Handler) sendMetaCAPITestEvent(c *gin.Context) {
	result, err := h.metaAds.SendTestEvent(c.Request.Context())
	if err != nil {
		h.audit(c, "meta_capi.test_event", map[string]any{"ok": false})
		h.respondMetaError(c, err, "send meta capi test event")
		return
	}
	h.audit(c, "meta_capi.test_event", map[string]any{"ok": true, "event_name": result.EventName, "used_lead_id": result.UsedLeadID})
	respondData(c, http.StatusOK, result)
}

// GET /api/integrations/meta-capi/audit: who changed what. No secrets in it.
func (h *Handler) metaCAPIAudit(c *gin.Context) {
	entries, err := h.store.ListAudit(c.Request.Context(), 100)
	if err != nil {
		h.respondStoreError(c, err, "audit log")
		return
	}
	respondData(c, http.StatusOK, entries)
}

// respondMetaError answers for a failed call to Meta: what is not set up is
// a 409, Meta's own refusal a 502 with Meta's message (never a token), and
// anything else the usual store error.
func (h *Handler) respondMetaError(c *gin.Context, err error, action string) {
	if metaads.IsNotReady(err) {
		respondError(c, http.StatusConflict, codeConflict, err.Error())
		return
	}
	if _, ok := meta.AsError(err); ok {
		respondError(c, http.StatusBadGateway, codeMetaError, err.Error())
		return
	}
	h.respondStoreError(c, err, action)
}

// ---------------------------------------------------------------------------
// Lead flags and quality refresh
// ---------------------------------------------------------------------------

// PATCH /api/leads/:id/flags {"spam_confirmed": bool, "meta_share_opt_out": bool}
//
// Not admin-only: whoever works the lead is the one who learns it is spam, or
// is asked not to share their details.
func (h *Handler) updateLeadFlags(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}
	var in model.LeadFlagsInput
	if !bindJSON(c, &in) {
		return
	}
	if in.SpamConfirmed == nil && in.MetaShareOptOut == nil {
		respondValidationError(c, model.ValidationErrors{"spam_confirmed": "send spam_confirmed or meta_share_opt_out"})
		return
	}
	lead, err := h.store.SetLeadFlags(c.Request.Context(), id, in)
	if err != nil {
		h.respondStoreError(c, err, "update lead flags")
		return
	}
	respondData(c, http.StatusOK, lead)
}

// refreshQuality works out these leads' quality again after a change that
// can move it. A failure is logged, not returned: the change itself has been
// saved, and the background rescore will catch up.
func (h *Handler) refreshQuality(c *gin.Context, ids ...int64) {
	if err := h.store.RefreshQuality(c.Request.Context(), ids...); err != nil {
		h.logger.Error("refresh lead quality", "error", err.Error())
	}
}

// withFreshQuality refreshes a lead's quality and returns it re-read, so the
// response shows the quality the change led to.
func (h *Handler) withFreshQuality(c *gin.Context, lead model.Lead) model.Lead {
	h.refreshQuality(c, lead.ID)
	fresh, err := h.store.GetLead(c.Request.Context(), lead.ID)
	if err != nil {
		return lead
	}
	return fresh
}

// audit records an admin action, with who did it and from where.
func (h *Handler) audit(c *gin.Context, action string, detail map[string]any) {
	if err := h.store.Audit(c.Request.Context(), action, actorOf(c), detail); err != nil {
		h.logger.Error("audit log", "action", action, "error", err.Error())
	}
}

func replacedOrKept(secret string) string {
	if secret != "" {
		return "replaced"
	}
	return "kept"
}
