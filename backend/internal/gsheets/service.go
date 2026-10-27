package gsheets

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"crm-lead-management/backend/internal/config"
	"crm-lead-management/backend/internal/model"
	"crm-lead-management/backend/internal/store"
)

// Errors the API words for people.
var (
	ErrNotConfigured = errors.New("Google Sheets is not set up on the server yet: GOOGLE_OAUTH_CLIENT_ID and GOOGLE_OAUTH_CLIENT_SECRET are missing.")
	ErrNotConnected  = errors.New("Connect a Google account first.")
	ErrNoSheet       = errors.New("Choose a spreadsheet and tab in Configure first.")
	ErrSyncRunning   = errors.New("A sync is already running. Wait for it to finish.")
	ErrConsent       = errors.New("Google did not give the CRM access to your files. Connect again and leave \"See, edit, create and delete only the specific Google Drive files you use with this app\" ticked.")
	ErrNoRefresh     = errors.New("Google did not return a long-lived sign-in. Remove the CRM's access at myaccount.google.com/permissions, then connect again.")
	ErrNotAdmin      = errors.New("The admin who connected Google is no longer an active admin, so syncing has stopped. An admin must connect Google again.")
)

// chunk is how many rows go in one write to Google.
const chunk = 500

// Service is the Google Sheets export.
type Service struct {
	store  *store.Store
	client *Client
	cfg    config.GoogleSheetsConfig
	logger *slog.Logger
	loc    *time.Location

	// running makes syncs one at a time.
	running sync.Mutex

	mu      sync.Mutex
	access  string
	expires time.Time
	forHash string // which refresh token the cached access token came from
}

// New returns the service.
func New(st *store.Store, cfg config.GoogleSheetsConfig, logger *slog.Logger, loc *time.Location) *Service {
	return &Service{store: st, client: NewClient(cfg), cfg: cfg, logger: logger, loc: loc}
}

// Client returns the Google client, for tests to shorten its backoff.
func (s *Service) Client() *Client { return s.client }

// --- connecting ----------------------------------------------------------------

// StartConnect returns Google's consent page for the admin. The state is
// stored hashed and tied to them, so only they can finish this sign-in.
func (s *Service) StartConnect(ctx context.Context, viewer model.Viewer) (string, error) {
	if !s.cfg.Configured() {
		return "", ErrNotConfigured
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("make oauth state: %w", err)
	}
	state := base64.RawURLEncoding.EncodeToString(b)
	verifier, challenge, err := NewPKCE()
	if err != nil {
		return "", err
	}
	if err := s.store.CreateOAuthState(ctx, HashState(state), viewer.MemberID, verifier); err != nil {
		return "", err
	}
	return s.client.AuthURL(state, challenge, ""), nil
}

// HashState is how a state is stored.
func HashState(state string) []byte {
	sum := sha256.Sum256([]byte(state))
	return sum[:]
}

// FinishConnect completes the sign-in Google sent the admin back from.
func (s *Service) FinishConnect(ctx context.Context, viewer model.Viewer, state, code string) (string, error) {
	verifier, err := s.store.TakeOAuthState(ctx, HashState(state), viewer.MemberID)
	if err != nil {
		return "", err
	}
	tok, err := s.client.Exchange(ctx, code, verifier)
	if err != nil {
		return "", err
	}
	if !tok.HasScope(DriveFileScope) {
		_ = s.client.Revoke(ctx, tok.AccessToken)
		return "", ErrConsent
	}
	if tok.RefreshToken == "" {
		return "", ErrNoRefresh
	}
	email, err := s.client.Email(ctx, tok.AccessToken)
	if err != nil {
		return "", fmt.Errorf("read the Google account: %w", err)
	}
	// A previous sign-in is replaced; tell Google to forget it.
	if prev, err := s.store.GetSheets(ctx); err == nil && prev.RefreshToken != "" && prev.RefreshToken != tok.RefreshToken {
		_ = s.client.Revoke(ctx, prev.RefreshToken)
	}
	if err := s.store.SaveSheetsConnection(ctx, tok.RefreshToken, email, viewer.MemberID); err != nil {
		return "", err
	}
	s.cache(tok.AccessToken, tok.ExpiresIn, tok.RefreshToken)
	return email, nil
}

// Disconnect revokes the sign-in at Google and forgets it here. Automatic
// sync stops with it. A revoke Google refuses still disconnects.
func (s *Service) Disconnect(ctx context.Context) error {
	rec, err := s.store.GetSheets(ctx)
	if err != nil {
		return err
	}
	if rec.RefreshToken != "" {
		if err := s.client.Revoke(ctx, rec.RefreshToken); err != nil {
			s.logger.Warn("google sheets: revoke failed; forgetting the token anyway", slog.String("error", err.Error()))
		}
	}
	s.mu.Lock()
	s.access, s.forHash, s.expires = "", "", time.Time{}
	s.mu.Unlock()
	return s.store.DisconnectSheets(ctx)
}

// --- access tokens -------------------------------------------------------------

func tokenHash(refresh string) string {
	sum := sha256.Sum256([]byte(refresh))
	return hex.EncodeToString(sum[:8])
}

func (s *Service) cache(access string, expiresIn int, refresh string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.access = access
	s.expires = time.Now().Add(time.Duration(expiresIn)*time.Second - time.Minute)
	s.forHash = tokenHash(refresh)
}

// AccessToken returns a working access token, refreshing it when needed. A
// refused refresh token marks the integration as errored.
func (s *Service) AccessToken(ctx context.Context) (string, time.Time, model.SheetsRecord, error) {
	rec, err := s.store.GetSheets(ctx)
	if err != nil {
		return "", time.Time{}, rec, err
	}
	if !s.cfg.Configured() {
		return "", time.Time{}, rec, ErrNotConfigured
	}
	if rec.TokenLocked {
		return "", time.Time{}, rec, store.ErrNoEncryptionKey
	}
	if !rec.Connected() {
		return "", time.Time{}, rec, ErrNotConnected
	}
	s.mu.Lock()
	if s.access != "" && s.forHash == tokenHash(rec.RefreshToken) && time.Now().Before(s.expires) {
		access, expires := s.access, s.expires
		s.mu.Unlock()
		return access, expires, rec, nil
	}
	s.mu.Unlock()

	tok, err := s.client.Refresh(ctx, rec.RefreshToken)
	if err != nil {
		if errors.Is(err, ErrRevoked) {
			_ = s.store.SetSheetsStatus(ctx, err.Error())
		}
		return "", time.Time{}, rec, err
	}
	s.cache(tok.AccessToken, tok.ExpiresIn, rec.RefreshToken)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.access, s.expires, rec, nil
}

// --- choosing a sheet ----------------------------------------------------------

// Spreadsheets lists the spreadsheets the CRM may open.
func (s *Service) Spreadsheets(ctx context.Context) ([]Spreadsheet, error) {
	access, _, _, err := s.AccessToken(ctx)
	if err != nil {
		return nil, err
	}
	return s.client.ListSpreadsheets(ctx, access)
}

// Spreadsheet returns one spreadsheet's name and tabs.
func (s *Service) Spreadsheet(ctx context.Context, id string) (Spreadsheet, error) {
	access, _, _, err := s.AccessToken(ctx)
	if err != nil {
		return Spreadsheet{}, err
	}
	return s.client.GetSpreadsheet(ctx, access, id)
}

// CreateSpreadsheet makes a new spreadsheet, with a "Leads" tab, for the CRM.
func (s *Service) CreateSpreadsheet(ctx context.Context, name string) (Spreadsheet, error) {
	access, _, _, err := s.AccessToken(ctx)
	if err != nil {
		return Spreadsheet{}, err
	}
	return s.client.CreateSpreadsheet(ctx, access, name, "Leads")
}

// SaveConfig checks the spreadsheet can be opened, makes a new tab if one was
// named, and saves the choices.
func (s *Service) SaveConfig(ctx context.Context, in model.SheetsConfigInput) (model.SheetsConfig, error) {
	access, _, rec, err := s.AccessToken(ctx)
	if err != nil {
		return model.SheetsConfig{}, err
	}
	sheet, err := s.client.GetSpreadsheet(ctx, access, in.SpreadsheetID)
	if err != nil {
		return model.SheetsConfig{}, err
	}
	var tab string
	if in.SheetTitle == "" {
		// A new tab; one already called that is used as it is.
		if tab = findFold(sheet.Tabs, in.NewSheetTitle); tab == "" {
			tab = in.NewSheetTitle
			if err := s.client.AddTab(ctx, access, sheet.ID, tab); err != nil {
				return model.SheetsConfig{}, err
			}
		}
	} else if tab = findFold(sheet.Tabs, in.SheetTitle); tab == "" {
		return model.SheetsConfig{}, &store.FieldError{Field: "sheet_title", Message: "is not a tab in this spreadsheet"}
	}

	cfg := rec.Config
	if cfg.SpreadsheetID != sheet.ID || cfg.SheetTitle != tab {
		cfg.LastHash = ""
	}
	cfg.SpreadsheetID, cfg.SpreadsheetName, cfg.SpreadsheetURL = sheet.ID, sheet.Name, sheet.URL
	cfg.SheetTitle = tab
	if !equalStrings(cfg.Columns, in.Columns) || !equalStages(cfg.Stages, in.Stages) {
		cfg.LastHash = ""
	}
	cfg.Columns, cfg.Stages = in.Columns, in.Stages
	cfg.AutoSync, cfg.AutoSyncMinutes = in.AutoSync, in.AutoSyncMinutes
	if err := s.store.SaveSheetsConfig(ctx, cfg); err != nil {
		return model.SheetsConfig{}, err
	}
	return cfg, nil
}

// TestResult is what Test Connection found.
type TestResult struct {
	OK          bool   `json:"ok"`
	Message     string `json:"message"`
	Account     string `json:"account"`
	Spreadsheet string `json:"spreadsheet"`
	Tab         string `json:"tab"`
	LeadRows    int    `json:"lead_rows"` // CRM rows already in the tab
}

// Test signs in, opens the spreadsheet and tab, and checks the CRM may write
// to the tab, without writing anything. The outcome becomes the status.
func (s *Service) Test(ctx context.Context) TestResult {
	res := s.test(ctx)
	problem := ""
	if !res.OK {
		problem = res.Message
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		_ = s.store.SetSheetsStatus(ctx, problem)
	}
	return res
}

func (s *Service) test(ctx context.Context) TestResult {
	access, _, rec, err := s.AccessToken(ctx)
	res := TestResult{Account: rec.Config.AccountEmail}
	if err != nil {
		res.Message = err.Error()
		return res
	}
	if !rec.Config.Ready() {
		res.OK, res.Message = true, "Signed in to Google. Now choose a spreadsheet and tab in Configure."
		return res
	}
	sheet, err := s.client.GetSpreadsheet(ctx, access, rec.Config.SpreadsheetID)
	if err != nil {
		res.Message = err.Error()
		return res
	}
	res.Spreadsheet, res.Tab = sheet.Name, rec.Config.SheetTitle
	if !containsFold(sheet.Tabs, rec.Config.SheetTitle) {
		res.Message = fmt.Sprintf("The tab %q is no longer in %q. Choose a tab in Configure.", rec.Config.SheetTitle, sheet.Name)
		return res
	}
	header, colA, empty, err := s.readTab(ctx, access, rec.Config)
	if err != nil {
		res.Message = err.Error()
		return res
	}
	plan, err := MakePlan(header, colA, empty, rec.Config.Columns, nil)
	if err != nil {
		res.Message = err.Error()
		return res
	}
	_ = plan
	res.LeadRows = countIDs(colA)
	res.OK = true
	res.Message = fmt.Sprintf("Connected. The CRM can write to %q in %q.", rec.Config.SheetTitle, sheet.Name)
	return res
}

// readTab reads row 1 and column A, and, when row 1 is blank, whether the
// whole tab is empty.
func (s *Service) readTab(ctx context.Context, access string, cfg model.SheetsConfig) (header, colA []string, empty bool, err error) {
	headerRows, err := s.client.GetValues(ctx, access, cfg.SpreadsheetID, A1(cfg.SheetTitle, "1:1"))
	if err != nil {
		return nil, nil, false, err
	}
	if len(headerRows) > 0 {
		header = headerRows[0]
	}
	colRows, err := s.client.GetValues(ctx, access, cfg.SpreadsheetID, A1(cfg.SheetTitle, "A:A"))
	if err != nil {
		return nil, nil, false, err
	}
	for _, r := range colRows {
		if len(r) > 0 {
			colA = append(colA, r[0])
		} else {
			colA = append(colA, "")
		}
	}
	if allBlank(header) && len(colA) == 0 {
		all, err := s.client.GetValues(ctx, access, cfg.SpreadsheetID, "'"+strings.ReplaceAll(cfg.SheetTitle, "'", "''")+"'")
		if err != nil {
			return nil, nil, false, err
		}
		empty = true
		for _, r := range all {
			if !allBlank(r) {
				empty = false
				break
			}
		}
	}
	return header, colA, empty, nil
}

func countIDs(colA []string) int {
	n := 0
	for i, v := range colA {
		if i == 0 {
			continue
		}
		if _, err := fmt.Sscanf(strings.TrimSpace(v), "%d", new(int64)); err == nil {
			n++
		}
	}
	return n
}

// --- syncing -------------------------------------------------------------------

// Sync exports the leads to the sheet and records it in the history.
// trigger is manual, auto or retry. An automatic sync with nothing new since
// the last one writes nothing and records nothing (skipped is true).
func (s *Service) Sync(ctx context.Context, trigger string, startedBy, retryOf *int64) (result model.SheetSync, skipped bool, err error) {
	if !s.running.TryLock() {
		return result, false, ErrSyncRunning
	}
	defer s.running.Unlock()
	release, ok, err := s.store.LockSheetSync(ctx)
	if err != nil {
		return result, false, err
	}
	if !ok {
		return result, false, ErrSyncRunning
	}
	defer release()

	access, _, rec, err := s.AccessToken(ctx)
	if err != nil {
		return result, false, err
	}
	cfg := rec.Config
	if !cfg.Ready() {
		return result, false, ErrNoSheet
	}

	// Syncs run as the admin who connected Google, with their access to leads.
	owner, err := s.store.MemberViewer(ctx, cfg.ConfiguredBy)
	if err != nil || !owner.IsAdmin() {
		_ = s.store.SetSheetsStatus(ctx, ErrNotAdmin.Error())
		return result, false, ErrNotAdmin
	}
	leads, err := s.store.ExportLeads(store.WithViewer(ctx, owner), cfg.Stages)
	if err != nil {
		return result, false, err
	}
	rows := make([]Row, len(leads))
	for i, l := range leads {
		rows[i] = Row{LeadID: l.ID, Values: l.SheetRow(cfg.Columns, s.loc)}
	}
	hash := fingerprint(cfg, rows)
	if trigger == "auto" && hash == cfg.LastHash {
		return result, true, nil
	}

	id, err := s.store.StartSheetSync(ctx, trigger, startedBy, retryOf)
	if err != nil {
		return result, false, err
	}
	result = model.SheetSync{ID: id, Trigger: trigger, StartedAt: time.Now(), LeadsTotal: len(rows)}
	s.write(ctx, access, cfg, rows, &result)

	// Recorded even if the request was cancelled, so history never shows a
	// sync running that is not.
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := s.store.FinishSheetSync(finishCtx, result); err != nil {
		return result, false, err
	}
	switch result.Status {
	case "success":
		_ = s.store.SetSheetsHash(finishCtx, hash)
		_ = s.store.SetSheetsStatus(finishCtx, "")
	case "failed":
		_ = s.store.SetSheetsStatus(finishCtx, result.Error)
	}
	if saved, err := s.store.GetSheetSync(finishCtx, id); err == nil {
		result = saved
	}
	return result, false, nil
}

// write carries out a sync, filling in result's counts, status and error.
func (s *Service) write(ctx context.Context, access string, cfg model.SheetsConfig, rows []Row, result *model.SheetSync) {
	fail := func(err error) {
		result.Status = "failed"
		result.RowsFailed = len(rows) - result.RowsUpdated - result.RowsAdded
		result.Error = err.Error()
	}
	header, colA, empty, err := s.readTab(ctx, access, cfg)
	if err != nil {
		fail(err)
		return
	}
	plan, err := MakePlan(header, colA, empty, cfg.Columns, rows)
	if err != nil {
		fail(err)
		return
	}
	lastCol := ColumnName(len(plan.Header))

	var problems []string
	// The header goes with the first batch of updates.
	batch := []RangeValues{{Range: A1(cfg.SheetTitle, "A1:"+lastCol+"1"), Values: [][]string{plan.Header}}}
	pending := 0
	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := s.client.BatchUpdate(ctx, access, cfg.SpreadsheetID, batch); err != nil {
			result.RowsFailed += pending
			problems = append(problems, err.Error())
		} else {
			result.RowsUpdated += pending
		}
		batch, pending = nil, 0
	}
	for _, u := range plan.Updates {
		batch = append(batch, RangeValues{
			Range:  A1(cfg.SheetTitle, fmt.Sprintf("A%d:%s%d", u.Row, lastCol, u.Row)),
			Values: [][]string{u.Values},
		})
		pending++
		if len(batch) >= chunk {
			flush()
		}
	}
	flush()

	for start := 0; start < len(plan.Appends); start += chunk {
		part := plan.Appends[start:min(start+chunk, len(plan.Appends))]
		if err := s.client.Append(ctx, access, cfg.SpreadsheetID, cfg.SheetTitle, part); err != nil {
			result.RowsFailed += len(part)
			problems = append(problems, err.Error())
		} else {
			result.RowsAdded += len(part)
		}
	}

	if len(plan.DuplicateIDs) > 0 {
		problems = append(problems, fmt.Sprintf("%d lead id(s) appear on more than one row; only the first row of each was updated.", len(plan.DuplicateIDs)))
	}
	switch {
	case result.RowsFailed == 0:
		result.Status = "success"
	case result.RowsUpdated+result.RowsAdded > 0:
		result.Status = "partial"
	default:
		result.Status = "failed"
	}
	result.Error = strings.Join(dedupe(problems), " ")
}

// fingerprint identifies what a sync would write.
func fingerprint(cfg model.SheetsConfig, rows []Row) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%v\n", cfg.SpreadsheetID, cfg.SheetTitle, cfg.Columns)
	for _, r := range rows {
		for _, v := range r.Values {
			h.Write([]byte(v))
			h.Write([]byte{0})
		}
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Run syncs automatically, when switched on, every chosen interval, until ctx
// ends. A sync that finds nothing new writes nothing.
func (s *Service) Run(ctx context.Context) {
	if err := s.store.FailStaleSheetSyncs(ctx); err != nil {
		s.logger.Warn("google sheets: tidy history", slog.String("error", err.Error()))
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.autoSync(ctx)
		}
	}
}

func (s *Service) autoSync(ctx context.Context) {
	if !s.cfg.Configured() {
		return
	}
	rec, err := s.store.GetSheets(ctx)
	if err != nil || !rec.Connected() || !rec.Config.Ready() || !rec.Config.AutoSync {
		return
	}
	last, err := s.store.LastSheetSyncAt(ctx)
	if err != nil || time.Since(last) < time.Duration(rec.Config.AutoSyncMinutes)*time.Minute {
		return
	}
	syncCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	result, skipped, err := s.Sync(syncCtx, "auto", nil, nil)
	switch {
	case errors.Is(err, ErrSyncRunning) || skipped:
	case err != nil:
		s.logger.Warn("google sheets: automatic sync", slog.String("error", err.Error()))
	case result.Status != "success":
		s.logger.Warn("google sheets: automatic sync had problems",
			slog.Int64("sync_id", result.ID), slog.String("status", result.Status))
	}
}

// --- small helpers -------------------------------------------------------------

func containsFold(list []string, s string) bool {
	for _, v := range list {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalStages(a, b []model.Stage) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func dedupe(list []string) []string {
	out := []string{}
	for _, v := range list {
		if !containsFold(out, v) {
			out = append(out, v)
		}
	}
	return out
}

// findFold returns the tab in list that matches s ignoring case, as Google
// spells it, or "" for none.
func findFold(list []string, s string) string {
	for _, v := range list {
		if strings.EqualFold(v, s) {
			return v
		}
	}
	return ""
}

// CheckReady reports why a sync cannot start, without starting one: the API
// checks this before running a sync in the background.
func (s *Service) CheckReady(ctx context.Context) error {
	_, _, rec, err := s.AccessToken(ctx)
	if err != nil {
		return err
	}
	if !rec.Config.Ready() {
		return ErrNoSheet
	}
	owner, err := s.store.MemberViewer(ctx, rec.Config.ConfiguredBy)
	if err != nil || !owner.IsAdmin() {
		return ErrNotAdmin
	}
	if !s.running.TryLock() {
		return ErrSyncRunning
	}
	s.running.Unlock()
	return nil
}
