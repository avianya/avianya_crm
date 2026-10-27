package api

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"crm-lead-management/backend/internal/metaads"
	"crm-lead-management/backend/internal/model"
)

// The ad lead form webhooks. Meta and Google call these from their servers,
// not from a browser, so they carry no session; each instead proves it came
// from the platform (Meta signs the body with the app secret, Google sends the
// key typed into the form's settings) and is refused otherwise. Meta's
// credentials are the ones saved on the Meta Ads settings page, or else those
// in backend/.env.

// maxWebhookBytes bounds a webhook body; real ones are a few kilobytes.
const maxWebhookBytes = 1 << 20

// ---------------------------------------------------------------------------
// Meta
// ---------------------------------------------------------------------------

// GET /api/webhooks/meta
//
// Meta's check when the webhook is saved in the app: it sends the verify
// token, and expects hub.challenge back as the body.
func (h *Handler) verifyMetaWebhook(c *gin.Context) {
	rt, err := h.metaAds.Runtime(c.Request.Context())
	if err != nil {
		h.logger.Error("meta settings", "error", err.Error())
		c.String(http.StatusInternalServerError, "The settings could not be read.")
		return
	}
	token := rt.Secrets.VerifyToken
	if token == "" {
		c.String(http.StatusServiceUnavailable, "Meta leads are not set up: no webhook verify token is saved.")
		return
	}
	if c.Query("hub.mode") != "subscribe" || !equalSecret(c.Query("hub.verify_token"), token) {
		c.String(http.StatusForbidden, "The verify token does not match the one saved.")
		return
	}
	c.String(http.StatusOK, c.Query("hub.challenge"))
}

// metaWebhook is the body Meta sends for the "leadgen" field of a Page.
type metaWebhook struct {
	Object string `json:"object"`
	Entry  []struct {
		ID      string `json:"id"` // the Page
		Changes []struct {
			Field string `json:"field"`
			Value struct {
				LeadgenID   string      `json:"leadgen_id"`
				FormID      string      `json:"form_id"`
				AdID        string      `json:"ad_id"`
				PageID      string      `json:"page_id"`
				CreatedTime json.Number `json:"created_time"`
			} `json:"value"`
		} `json:"changes"`
	} `json:"entry"`
}

// POST /api/webhooks/meta
//
// Meta says only that a lead exists; its answers are then read from the Graph
// API with the Page access token. Each lead is recorded whatever happens, so a
// failed fetch shows on the Integrations page and can be retried. Answers 200
// once everything is recorded, and 500 on a database error so Meta resends.
// Once a Page ID is saved, leads from any other Page are ignored.
func (h *Handler) receiveMetaWebhook(c *gin.Context) {
	ctx := c.Request.Context()
	rt, err := h.metaAds.Runtime(ctx)
	if err != nil {
		h.logger.Error("meta settings", "error", err.Error())
		c.String(http.StatusInternalServerError, "The settings could not be read; please resend.")
		return
	}
	if !rt.LeadsReady() {
		c.String(http.StatusServiceUnavailable, "Meta leads are not set up.")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, maxWebhookBytes))
	if err != nil {
		c.String(http.StatusBadRequest, "The body could not be read.")
		return
	}
	if !validMetaSignature(body, c.GetHeader("X-Hub-Signature-256"), rt.Secrets.AppSecret) {
		h.logger.Warn("meta webhook with a bad signature")
		c.String(http.StatusForbidden, "The signature does not match the app secret.")
		return
	}

	var hook metaWebhook
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&hook); err != nil {
		c.String(http.StatusBadRequest, "The body is not JSON.")
		return
	}

	for _, entry := range hook.Entry {
		for _, change := range entry.Changes {
			v := change.Value
			if change.Field != "leadgen" || v.LeadgenID == "" {
				continue
			}
			pageID := firstNonEmpty(v.PageID, entry.ID)
			if rt.Config.PageID != "" && pageID != rt.Config.PageID {
				h.logger.Info("meta lead from another page ignored", "page_id", pageID)
				continue
			}
			created, _ := v.CreatedTime.Int64()
			notice := metaads.Notice{LeadgenID: v.LeadgenID, FormID: v.FormID, AdID: v.AdID, PageID: pageID, CreatedTime: created}
			if _, err := h.metaAds.TakeLead(ctx, rt, notice); err != nil {
				h.logger.Error("meta lead", "leadgen_id", v.LeadgenID, "error", err.Error())
				c.String(http.StatusInternalServerError, "The lead could not be saved; please resend.")
				return
			}
		}
	}
	c.String(http.StatusOK, "ok")
}

// validMetaSignature checks X-Hub-Signature-256: "sha256=" and the hex HMAC of
// the body, keyed with the app secret.
func validMetaSignature(body []byte, header, secret string) bool {
	if secret == "" {
		return false
	}
	sig, ok := strings.CutPrefix(header, "sha256=")
	if !ok {
		return false
	}
	got, err := hex.DecodeString(sig)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}

// equalSecret compares two secrets in constant time, so the time taken does
// not give away how much of a guess was right.
func equalSecret(got, want string) bool {
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// ---------------------------------------------------------------------------
// Google
// ---------------------------------------------------------------------------

// googleLead is the body Google Ads sends for a lead form webhook. The ids
// are numbers in the JSON, kept as json.Number so none is rounded.
type googleLead struct {
	LeadID         string      `json:"lead_id"`
	GoogleKey      string      `json:"google_key"`
	IsTest         bool        `json:"is_test"`
	FormID         json.Number `json:"form_id"`
	CampaignID     json.Number `json:"campaign_id"`
	CreativeID     json.Number `json:"creative_id"`
	UserColumnData []struct {
		ColumnID    string `json:"column_id"`
		ColumnName  string `json:"column_name"`
		StringValue string `json:"string_value"`
	} `json:"user_column_data"`
}

// POST /api/webhooks/google
//
// Google sends the answers with the lead, so it is taken in straight away.
// Google's "Send test data" button sends is_test, which is recorded but makes
// no lead.
func (h *Handler) receiveGoogleWebhook(c *gin.Context) {
	if h.cfg.GoogleLeadsKey == "" {
		c.String(http.StatusServiceUnavailable, "Google leads are not set up: GOOGLE_LEADS_KEY is empty.")
		return
	}
	var hook googleLead
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, maxWebhookBytes))
	decoder.UseNumber()
	if err := decoder.Decode(&hook); err != nil {
		c.String(http.StatusBadRequest, "The body is not JSON.")
		return
	}
	if !equalSecret(hook.GoogleKey, h.cfg.GoogleLeadsKey) {
		h.logger.Warn("google webhook with a wrong key")
		c.String(http.StatusForbidden, "The key does not match GOOGLE_LEADS_KEY.")
		return
	}
	if hook.LeadID == "" {
		c.String(http.StatusBadRequest, "lead_id is missing.")
		return
	}

	sub := model.AdSubmission{
		Provider:   model.ProviderGoogle,
		ExternalID: hook.LeadID,
		FormID:     hook.FormID.String(),
		Campaign:   hook.CampaignID.String(),
		Ad:         hook.CreativeID.String(),
		IsTest:     hook.IsTest,
	}
	for _, col := range hook.UserColumnData {
		label := col.ColumnName
		if label == "" {
			label = model.LabelFromKey(col.ColumnID)
		}
		key := col.ColumnID
		if key == "" {
			key = col.ColumnName
		}
		sub.Answers = append(sub.Answers, model.AdAnswer{Key: key, Label: label, Value: col.StringValue})
	}

	if _, err := h.store.ReceiveAdLead(c.Request.Context(), sub); err != nil {
		h.logger.Error("google lead", "lead_id", hook.LeadID, "error", err.Error())
		c.String(http.StatusInternalServerError, "The lead could not be saved; please resend.")
		return
	}
	c.JSON(http.StatusOK, gin.H{})
}
