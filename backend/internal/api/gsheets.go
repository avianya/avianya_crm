package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"crm-lead-management/backend/internal/auth"
	"crm-lead-management/backend/internal/config"
	"crm-lead-management/backend/internal/gsheets"
	"crm-lead-management/backend/internal/model"
	"crm-lead-management/backend/internal/store"
)

// Google Sheets export (see package gsheets). Every route is admin-only: the
// export holds every lead, so only someone who may see every lead and change
// integrations may set it up or run it.

// sheetsCallbackPath is where Google sends the admin back. It is public in
// the sense that it skips authenticate, because it must answer with a
// redirect rather than JSON; it checks the session itself.
const sheetsCallbackPath = config.GoogleSheetsCallbackPath

// GET /api/integrations/google-sheets
func (h *Handler) sheetsSettings(c *gin.Context) {
	ctx := c.Request.Context()
	rec, err := h.store.GetSheets(ctx)
	if err != nil {
		h.respondStoreError(c, err, "google sheets settings")
		return
	}
	syncs, err := h.store.ListSheetSyncs(ctx, 20)
	if err != nil {
		h.respondStoreError(c, err, "google sheets history")
		return
	}
	g := h.cfg.GoogleSheets
	missing := []string{}
	for name, v := range map[string]string{
		"GOOGLE_OAUTH_CLIENT_ID": g.ClientID, "GOOGLE_OAUTH_CLIENT_SECRET": g.ClientSecret,
	} {
		if v == "" {
			missing = append(missing, name)
		}
	}
	if len(h.cfg.SecretsKey) == 0 {
		missing = append(missing, "SECRETS_ENCRYPTION_KEY")
	}
	byName := ""
	if rec.Config.ConfiguredBy != 0 {
		if v, err := h.store.MemberViewer(ctx, rec.Config.ConfiguredBy); err == nil {
			byName = v.Name
		}
	}
	status := rec.Status
	if !rec.Connected() && !rec.TokenLocked {
		status = model.SheetNotConnected
	}
	respondData(c, http.StatusOK, gin.H{
		"settings": model.SheetsSettings{
			OAuthConfigured:  g.Configured(),
			EncryptionReady:  len(h.cfg.SecretsKey) > 0,
			PickerAvailable:  g.PickerConfigured(),
			RedirectURL:      g.RedirectURL,
			MissingEnv:       sortedStrings(missing),
			Status:           status,
			AccountEmail:     rec.Config.AccountEmail,
			TokenLocked:      rec.TokenLocked,
			SpreadsheetID:    rec.Config.SpreadsheetID,
			SpreadsheetName:  rec.Config.SpreadsheetName,
			SpreadsheetURL:   rec.Config.SpreadsheetURL,
			SheetTitle:       rec.Config.SheetTitle,
			Columns:          rec.Config.Columns,
			Stages:           rec.Config.Stages,
			AutoSync:         rec.Config.AutoSync,
			AutoSyncMinutes:  rec.Config.AutoSyncMinutes,
			LastSyncAt:       rec.LastSyncAt,
			LastSyncCount:    rec.LastSyncCount,
			LastError:        rec.LastError,
			LastErrorAt:      rec.LastErrorAt,
			Fields:           model.SheetFields,
			AutoSyncOptions:  model.SheetAutoSyncOptions,
			ConfiguredByName: byName,
		},
		"syncs": syncs,
	})
}

func sortedStrings(list []string) []string {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j] < list[j-1]; j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
	return list
}

// POST /api/integrations/google-sheets/connect: Google's consent page to open.
func (h *Handler) sheetsConnect(c *gin.Context) {
	if len(h.cfg.SecretsKey) == 0 {
		h.respondStoreError(c, store.ErrNoEncryptionKey, "google sheets connect")
		return
	}
	// The sign-in belongs to a person, whose access syncs then run with.
	if viewerOf(c).MemberID == 0 {
		respondError(c, http.StatusConflict, codeConflict, "Log in as an admin to connect Google. The admin API key cannot.")
		return
	}
	u, err := h.sheets.StartConnect(c.Request.Context(), viewerOf(c))
	if err != nil {
		h.respondSheetsError(c, err, "google sheets connect")
		return
	}
	respondData(c, http.StatusOK, gin.H{"url": u})
}

// GET /api/integrations/google-sheets/callback?code=&state= (or &error=)
//
// Google sends the admin's browser here. The answer is always a redirect to
// the Integrations page, saying what happened, never the tokens.
func (h *Handler) sheetsCallback(c *gin.Context) {
	back := func(key, value string) {
		c.Redirect(http.StatusFound, h.cfg.BaseURL+"/integrations?"+url.Values{key: {value}}.Encode())
	}
	fail := func(message string) {
		// The reason only: never the code or state.
		h.logger.Warn("google sheets: sign-in not completed", slog.String("reason", message),
			slog.Bool("has_code", c.Query("code") != ""), slog.Bool("has_state", c.Query("state") != ""),
			slog.String("google_error", c.Query("error")))
		back("google_sheets_error", message)
	}

	// Who is this? The same session cookie the app uses; only an admin may
	// finish, and only the one who started (the state says who).
	token, err := c.Cookie(sessionCookie)
	if err != nil || token == "" {
		fail("Your CRM session ended while you were at Google. Log in and connect again.")
		return
	}
	viewer, err := h.store.SessionViewer(c.Request.Context(), auth.HashToken(token))
	if err != nil || !viewer.IsAdmin() {
		fail("Only an admin can connect Google Sheets. Log in as an admin and connect again.")
		return
	}

	if gErr := c.Query("error"); gErr != "" {
		// The state is still used up, so the link cannot be replayed.
		if state := c.Query("state"); state != "" {
			_, _ = h.store.TakeOAuthState(c.Request.Context(), gsheets.HashState(state), viewer.MemberID)
		}
		if gErr == "access_denied" {
			fail("Google access was not given (the consent screen was cancelled or declined). Nothing was connected.")
		} else {
			fail("Google returned an error (" + gErr + "). Nothing was connected.")
		}
		return
	}
	state, code := c.Query("state"), c.Query("code")
	if state == "" || code == "" {
		fail("Google's answer was incomplete. Connect again.")
		return
	}
	email, err := h.sheets.FinishConnect(c.Request.Context(), viewer, state, code)
	switch {
	case errors.Is(err, store.ErrOAuthState):
		fail("This sign-in link has expired or was already used. Connect again.")
		return
	case err != nil:
		h.logger.Warn("google sheets: connect failed", slog.String("error", err.Error()))
		fail(err.Error())
		return
	}
	_ = h.store.Audit(c.Request.Context(), "google_sheets.connected", viewer.Name, map[string]any{"account": email})
	back("google_sheets", "connected")
}

// POST /api/integrations/google-sheets/disconnect
func (h *Handler) sheetsDisconnect(c *gin.Context) {
	if err := h.sheets.Disconnect(c.Request.Context()); err != nil {
		h.respondSheetsError(c, err, "google sheets disconnect")
		return
	}
	_ = h.store.Audit(c.Request.Context(), "google_sheets.disconnected", viewerOf(c).Name, nil)
	h.sheetsSettings(c)
}

// POST /api/integrations/google-sheets/test
func (h *Handler) sheetsTest(c *gin.Context) {
	respondData(c, http.StatusOK, h.sheets.Test(c.Request.Context()))
}

// GET /api/integrations/google-sheets/spreadsheets: those the CRM may open.
func (h *Handler) sheetsList(c *gin.Context) {
	list, err := h.sheets.Spreadsheets(c.Request.Context())
	if err != nil {
		h.respondSheetsError(c, err, "list spreadsheets")
		return
	}
	respondData(c, http.StatusOK, list)
}

// GET /api/integrations/google-sheets/spreadsheets/:id: its name and tabs.
func (h *Handler) sheetsGet(c *gin.Context) {
	sheet, err := h.sheets.Spreadsheet(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.respondSheetsError(c, err, "get spreadsheet")
		return
	}
	respondData(c, http.StatusOK, sheet)
}

// POST /api/integrations/google-sheets/spreadsheets {"name"}: a new one.
func (h *Handler) sheetsCreate(c *gin.Context) {
	var in struct {
		Name string `json:"name"`
	}
	if !bindJSON(c, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len([]rune(in.Name)) > 100 {
		respondValidationError(c, model.ValidationErrors{"name": "must be 1 to 100 characters"})
		return
	}
	sheet, err := h.sheets.CreateSpreadsheet(c.Request.Context(), in.Name)
	if err != nil {
		h.respondSheetsError(c, err, "create spreadsheet")
		return
	}
	_ = h.store.Audit(c.Request.Context(), "google_sheets.spreadsheet_created", viewerOf(c).Name,
		map[string]any{"spreadsheet": sheet.Name})
	respondData(c, http.StatusCreated, sheet)
}

// PUT /api/integrations/google-sheets: the sheet, columns and auto-sync.
func (h *Handler) sheetsSave(c *gin.Context) {
	var in model.SheetsConfigInput
	if !bindJSON(c, &in) {
		return
	}
	in.Normalize()
	if errs := in.Validate(); errs.Any() {
		respondValidationError(c, errs)
		return
	}
	cfg, err := h.sheets.SaveConfig(c.Request.Context(), in)
	if err != nil {
		h.respondSheetsError(c, err, "save google sheets settings")
		return
	}
	_ = h.store.Audit(c.Request.Context(), "google_sheets.configured", viewerOf(c).Name, map[string]any{
		"spreadsheet": cfg.SpreadsheetName, "tab": cfg.SheetTitle, "columns": cfg.Columns,
		"auto_sync": cfg.AutoSync,
	})
	h.sheetsSettings(c)
}

// GET /api/integrations/google-sheets/picker-token: a short-lived access
// token for Google's file picker, held only in the admin's page while the
// picker is open. Only offered when the picker is set up.
func (h *Handler) sheetsPickerToken(c *gin.Context) {
	g := h.cfg.GoogleSheets
	if !g.PickerConfigured() {
		respondError(c, http.StatusConflict, codeConflict,
			"Choosing from Google Drive needs GOOGLE_PICKER_API_KEY and GOOGLE_CLOUD_PROJECT_NUMBER on the server.")
		return
	}
	access, expires, _, err := h.sheets.AccessToken(c.Request.Context())
	if err != nil {
		h.respondSheetsError(c, err, "picker token")
		return
	}
	c.Header("Cache-Control", "no-store")
	respondData(c, http.StatusOK, gin.H{
		"access_token": access, "expires_at": expires,
		"api_key": g.PickerAPIKey, "app_id": g.ProjectNumber,
	})
}

// POST /api/integrations/google-sheets/sync: starts a sync in the background
// and answers at once; the history shows it running, then how it went.
func (h *Handler) sheetsSync(c *gin.Context) {
	h.startSheetSync(c, "manual", nil)
}

// POST /api/integrations/google-sheets/syncs/:id/retry: a failed or partial
// sync, run again (in full, so nothing it missed is left out).
func (h *Handler) sheetsRetry(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}
	prev, err := h.store.GetSheetSync(c.Request.Context(), id)
	if err != nil {
		h.respondStoreError(c, err, "get sheet sync")
		return
	}
	if prev.Status != "failed" && prev.Status != "partial" {
		respondError(c, http.StatusConflict, codeConflict, "Only a failed or partial sync can be retried.")
		return
	}
	h.startSheetSync(c, "retry", &id)
}

func (h *Handler) startSheetSync(c *gin.Context, trigger string, retryOf *int64) {
	if err := h.sheets.CheckReady(c.Request.Context()); err != nil {
		h.respondSheetsError(c, err, "start sheet sync")
		return
	}
	by := viewerOf(c).MemberID
	var startedBy *int64
	if by != 0 {
		startedBy = &by
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		result, _, err := h.sheets.Sync(ctx, trigger, startedBy, retryOf)
		if err != nil {
			h.logger.Warn("google sheets: sync", slog.String("trigger", trigger), slog.String("error", err.Error()))
			return
		}
		h.logger.Info("google sheets: sync done", slog.Int64("sync_id", result.ID), slog.String("status", result.Status),
			slog.Int("updated", result.RowsUpdated), slog.Int("added", result.RowsAdded), slog.Int("failed", result.RowsFailed))
	}()
	respondData(c, http.StatusAccepted, gin.H{"message": "Sync started."})
}

// respondSheetsError words the export's errors for the page.
func (h *Handler) respondSheetsError(c *gin.Context, err error, action string) {
	var apiErr *gsheets.APIError
	switch {
	case errors.Is(err, gsheets.ErrNotConfigured), errors.Is(err, gsheets.ErrNotConnected),
		errors.Is(err, gsheets.ErrNoSheet), errors.Is(err, gsheets.ErrSyncRunning),
		errors.Is(err, gsheets.ErrRevoked), errors.Is(err, gsheets.ErrNoAccess),
		errors.Is(err, gsheets.ErrTabInUse), errors.Is(err, gsheets.ErrNotAdmin):
		respondError(c, http.StatusConflict, codeConflict, err.Error())
	case errors.As(err, &apiErr):
		h.logger.Warn("google sheets: google error", slog.String("action", action), slog.String("error", err.Error()))
		respondError(c, http.StatusBadGateway, "google_error", "Google could not do this: "+err.Error())
	default:
		h.respondStoreError(c, err, action)
	}
}
