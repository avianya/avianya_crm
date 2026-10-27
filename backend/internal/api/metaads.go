package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"crm-lead-management/backend/internal/metaads"
	"crm-lead-management/backend/internal/model"
)

// The Meta Ads integration's endpoints. Anything that reads or changes
// credentials, calls Meta on the CRM's behalf or overrides a score needs the
// admin key (see requireAdmin); the dashboard and the delivery log do not,
// like every other screen in the app, and carry no secret.

// ---------------------------------------------------------------------------
// Settings (admin)
// ---------------------------------------------------------------------------

// GET /api/meta/settings
func (h *Handler) metaSettings(c *gin.Context) {
	rt, err := h.metaAds.Runtime(c.Request.Context())
	if err != nil {
		h.respondStoreError(c, err, "meta settings")
		return
	}
	respondData(c, http.StatusOK, h.metaAds.View(rt, len(h.cfg.SecretsKey) > 0))
}

// PUT /api/meta/settings {"config": {...}, "secrets": {...}}
//
// A blank secret keeps the saved one, so the page never has to hold a token
// it was not just given. Turning test mode off needs a passed connection test.
func (h *Handler) saveMetaSettings(c *gin.Context) {
	var in model.MetaSettingsInput
	if !bindJSON(c, &in) {
		return
	}
	in.Normalize()
	if errs := in.Validate(); errs.Any() {
		respondValidationError(c, errs)
		return
	}
	ctx := c.Request.Context()
	if _, err := h.store.SaveMetaSettings(ctx, in.Config, in.Secrets); err != nil {
		h.respondStoreError(c, err, "save meta settings")
		return
	}
	h.logger.Info("meta settings saved", "credentials_replaced", !in.Secrets.Empty())
	replaced := map[string]any{}
	for _, name := range model.SecretNames {
		replaced[name] = replacedOrKept(in.Secrets.Get(name))
	}
	h.audit(c, "meta.settings_saved", map[string]any{
		"page_id": in.Config.PageID, "app_id": in.Config.AppID, "form_ids": in.Config.FormIDs,
		"capi_enabled": in.Config.CAPIEnabled, "test_mode": in.Config.TestMode, "credentials": replaced,
	})
	h.metaSettings(c)
}

// DELETE /api/meta/settings/credentials
//
// Disconnect: every saved credential is forgotten and conversion events stop.
func (h *Handler) disconnectMeta(c *gin.Context) {
	if err := h.store.DisconnectMeta(c.Request.Context()); err != nil {
		h.respondStoreError(c, err, "disconnect meta")
		return
	}
	h.logger.Info("meta disconnected")
	h.audit(c, "meta.disconnected", nil)
	h.metaSettings(c)
}

// POST /api/meta/settings/test
//
// Real calls to Meta with the saved settings; the outcome becomes the status.
func (h *Handler) testMetaConnection(c *gin.Context) {
	result, err := h.metaAds.TestConnection(c.Request.Context())
	if err != nil {
		h.respondStoreError(c, err, "test meta connection")
		return
	}
	respondData(c, http.StatusOK, result)
}

// POST /api/meta/sync
func (h *Handler) syncMetaLeads(c *gin.Context) {
	result, err := h.metaAds.Sync(c.Request.Context())
	if err != nil {
		if metaads.IsNotReady(err) {
			respondError(c, http.StatusConflict, codeConflict, err.Error())
			return
		}
		h.respondStoreError(c, err, "sync meta leads")
		return
	}
	respondData(c, http.StatusOK, result)
}

// GET /api/meta/admin-check: 204 when the admin key is right, so the page can
// check a key before using it.
func (h *Handler) adminCheck(c *gin.Context) { c.Status(http.StatusNoContent) }

// ---------------------------------------------------------------------------
// Quality rules
// ---------------------------------------------------------------------------

// GET /api/meta/quality-rules (anyone may read how scores are worked out)
func (h *Handler) qualityRules(c *gin.Context) {
	rules, err := h.store.GetQualityRules(c.Request.Context())
	if err != nil {
		h.respondStoreError(c, err, "quality rules")
		return
	}
	respondData(c, http.StatusOK, rules)
}

// PUT /api/meta/quality-rules (admin): saves and rescores every imported lead.
func (h *Handler) saveQualityRules(c *gin.Context) {
	var in model.QualityRules
	if !bindJSON(c, &in) {
		return
	}
	in.Normalize()
	if in.Rules == nil {
		in.Rules = []model.QualityRule{}
	}
	if errs := in.Validate(); errs.Any() {
		respondValidationError(c, errs)
		return
	}
	rescored, err := h.store.SaveQualityRules(c.Request.Context(), in)
	if err != nil {
		h.respondStoreError(c, err, "save quality rules")
		return
	}
	h.audit(c, "quality.rules_saved", map[string]any{"rules": len(in.Rules), "rescored": rescored})
	respondData(c, http.StatusOK, gin.H{"rules": in, "rescored": rescored})
}

// ---------------------------------------------------------------------------
// Lead quality
// ---------------------------------------------------------------------------

// PATCH /api/leads/:id/qualification {"reason": "..."}
func (h *Handler) updateQualification(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}
	var in model.QualificationInput
	if !bindJSON(c, &in) {
		return
	}
	in.Normalize()
	if errs := in.Validate(); errs.Any() {
		respondValidationError(c, errs)
		return
	}
	lead, err := h.store.UpdateQualificationReason(c.Request.Context(), id, in.Reason)
	if err != nil {
		h.respondStoreError(c, err, "update qualification")
		return
	}
	respondData(c, http.StatusOK, lead)
}

// PUT /api/leads/:id/quality-override {"score": 0-100, "reason": "..."} (admin)
func (h *Handler) overrideQuality(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}
	var in model.QualityOverrideInput
	if !bindJSON(c, &in) {
		return
	}
	in.Normalize()
	if errs := in.Validate(); errs.Any() {
		respondValidationError(c, errs)
		return
	}
	lead, err := h.store.SetQualityOverride(c.Request.Context(), id, *in.Score, in.Reason)
	if err != nil {
		h.respondStoreError(c, err, "override quality")
		return
	}
	h.audit(c, "quality.override_set", map[string]any{"lead_id": id, "score": *in.Score})
	respondData(c, http.StatusOK, lead)
}

// DELETE /api/leads/:id/quality-override (admin)
func (h *Handler) clearQualityOverride(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}
	lead, err := h.store.ClearQualityOverride(c.Request.Context(), id)
	if err != nil {
		h.respondStoreError(c, err, "clear quality override")
		return
	}
	respondData(c, http.StatusOK, lead)
}

// ---------------------------------------------------------------------------
// Dashboard
// ---------------------------------------------------------------------------

// GET /api/meta/leads?from=&to=&ad_account=&page_id=&form_id=&campaign_id=
// &ad_id=&source=&quality=&score_min=&score_max=&stage=&owner=&capi_status=
// &page=&page_size=
func (h *Handler) metaLeads(c *gin.Context) {
	q, errs := parseMetaLeadQuery(c, h.location)
	if errs.Any() {
		respondValidationError(c, errs)
		return
	}
	report, total, err := h.store.MetaLeads(c.Request.Context(), q)
	if err != nil {
		h.respondStoreError(c, err, "meta leads")
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data":       report,
		"pagination": model.NewPageMeta(model.LeadQuery{Page: q.Page, PageSize: q.PageSize}, total),
	})
}

// parseMetaLeadQuery reads the dashboard's filters. Dates are whole days
// (2026-10-01) in the CRM's time zone; to is inclusive of that day.
func parseMetaLeadQuery(c *gin.Context, loc *time.Location) (model.MetaLeadQuery, model.ValidationErrors) {
	errs := model.ValidationErrors{}
	q := model.MetaLeadQuery{Page: 1, PageSize: 25}
	if loc == nil {
		loc = time.UTC
	}
	day := func(key string) *time.Time {
		raw := strings.TrimSpace(c.Query(key))
		if raw == "" {
			return nil
		}
		t, err := time.ParseInLocation("2006-01-02", raw, loc)
		if err != nil {
			errs[key] = "must be a date like 2026-10-01"
			return nil
		}
		return &t
	}
	q.From = day("from")
	if to := day("to"); to != nil {
		next := to.AddDate(0, 0, 1)
		q.To = &next
	}
	if q.From != nil && q.To != nil && !q.From.Before(*q.To) {
		errs["to"] = "must be on or after from"
	}

	text := func(key string) string {
		v := strings.TrimSpace(c.Query(key))
		if v == "all" {
			return ""
		}
		return v
	}
	q.AdAccountID = strings.TrimPrefix(text("ad_account"), "act_")
	q.PageID, q.FormID, q.CampaignID, q.AdID = text("page_id"), text("form_id"), text("campaign_id"), text("ad_id")

	if raw := text("source"); raw != "" {
		if s := model.Source(raw); s.Valid() {
			q.Source = s
		} else {
			errs["source"] = fmt.Sprintf("%q is not a known source", raw)
		}
	}
	if raw := text("quality"); raw != "" {
		if v := model.Quality(raw); v.Valid() {
			q.Quality = v
		} else {
			errs["quality"] = "must be good, average, poor or not_evaluated"
		}
	}
	if raw := text("stage"); raw != "" {
		if s := model.Stage(raw); s.Valid() {
			q.Stage = s
		} else {
			errs["stage"] = fmt.Sprintf("%q is not a known stage", raw)
		}
	}
	if raw := text("qualification"); raw != "" {
		if v := model.QualificationStatus(raw); v.Valid() {
			q.Qualification = v
		} else {
			errs["qualification"] = "must be pending, qualified or not_qualified"
		}
	}
	score := func(key string) *int {
		raw := text(key)
		if raw == "" {
			return nil
		}
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 || n > 100 {
			errs[key] = "must be a whole number from 0 to 100"
			return nil
		}
		return &n
	}
	q.ScoreMin, q.ScoreMax = score("score_min"), score("score_max")
	if q.ScoreMin != nil && q.ScoreMax != nil && *q.ScoreMin > *q.ScoreMax {
		errs["score_max"] = "must be at least score_min"
	}
	if owner, ok := parseOwnerParam(c); ok {
		q.Owner = owner
	} else {
		errs["owner"] = ownerParamError
	}
	if raw := text("capi_status"); raw != "" {
		if raw != "none" && !model.CAPIStatus(raw).Valid() {
			errs["capi_status"] = "must be none, pending, sent, failed or retry_scheduled"
		} else {
			q.CAPIStatus = raw
		}
	}
	if page, err := strconv.Atoi(c.Query("page")); err == nil && page > 1 {
		q.Page = min(page, model.MaxPage)
	}
	if size, err := strconv.Atoi(c.Query("page_size")); err == nil && size > 0 {
		q.PageSize = min(size, model.MaxPageSize)
	}
	return q, errs
}

// ---------------------------------------------------------------------------
// Conversion events
// ---------------------------------------------------------------------------

// GET /api/meta/capi/events?status=&lead_id=
//
// The delivery log. Errors are Meta's own messages; no token is ever in them.
func (h *Handler) capiEvents(c *gin.Context) {
	q := model.CAPIEventQuery{Limit: 200}
	if raw := strings.TrimSpace(c.Query("status")); raw != "" && raw != "all" {
		if !model.CAPIStatus(raw).Valid() {
			respondValidationError(c, model.ValidationErrors{"status": "must be pending, sent, failed or retry_scheduled"})
			return
		}
		q.Status = model.CAPIStatus(raw)
	}
	if raw := strings.TrimSpace(c.Query("lead_id")); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			respondValidationError(c, model.ValidationErrors{"lead_id": "must be a lead id"})
			return
		}
		q.LeadID = id
	}
	for key, dst := range map[string]**time.Time{"from": &q.From, "to": &q.To} {
		raw := strings.TrimSpace(c.Query(key))
		if raw == "" {
			continue
		}
		t, err := time.ParseInLocation("2006-01-02", raw, h.location)
		if err != nil {
			respondValidationError(c, model.ValidationErrors{key: "must be a date like 2026-10-01"})
			return
		}
		if key == "to" {
			t = t.AddDate(0, 0, 1) // to is inclusive of that day
		}
		*dst = &t
	}
	ctx := c.Request.Context()
	events, err := h.store.ListCAPIEvents(ctx, q)
	if err != nil {
		h.respondStoreError(c, err, "list conversion events")
		return
	}
	counts, err := h.store.CAPIStatusCounts(ctx)
	if err != nil {
		h.respondStoreError(c, err, "count conversion events")
		return
	}
	respondData(c, http.StatusOK, gin.H{"events": events, "counts": counts})
}

// POST /api/meta/capi/events/:id/retry (admin)
func (h *Handler) retryCAPIEvent(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}
	event, err := h.store.RetryCAPIEvent(c.Request.Context(), id)
	if err != nil {
		h.respondStoreError(c, err, "retry conversion event")
		return
	}
	respondData(c, http.StatusOK, event)
}

// POST /api/meta/capi/events/retry-failed (admin)
func (h *Handler) retryFailedCAPIEvents(c *gin.Context) {
	n, err := h.store.RetryFailedCAPIEvents(c.Request.Context())
	if err != nil {
		h.respondStoreError(c, err, "retry failed conversion events")
		return
	}
	respondData(c, http.StatusOK, gin.H{"requeued": n})
}
