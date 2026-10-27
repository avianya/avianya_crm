package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"crm-lead-management/backend/internal/model"
)

// Google Sheets export storage (see package gsheets). The settings are the
// integration_settings row for 'google_sheets'; its secrets hold the OAuth
// refresh token, sealed like Meta's.

const (
	sheetsProvider       = "google_sheets"
	sheetsSecretsContext = "integration_settings:google_sheets"
)

// ErrOAuthState: the state Google sent back is unknown, used, expired, or
// belongs to someone else.
var ErrOAuthState = errors.New("store: oauth state is not valid")

type sheetsSecrets struct {
	RefreshToken string `json:"refresh_token"`
}

// GetSheets returns the saved settings, or the defaults. A token that cannot
// be opened comes back empty with TokenLocked set.
func (s *Store) GetSheets(ctx context.Context) (model.SheetsRecord, error) {
	rec := model.SheetsRecord{Config: model.DefaultSheetsConfig(), Status: model.SheetNotConnected}
	var (
		cfg, sealed []byte
		status      string
	)
	err := s.pool.QueryRow(ctx, `
		SELECT config, secrets, status, last_sync_at, last_sync_count, last_error, last_error_at
		FROM integration_settings WHERE provider = $1`, sheetsProvider).
		Scan(&cfg, &sealed, &status, &rec.LastSyncAt, &rec.LastSyncCount, &rec.LastError, &rec.LastErrorAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return rec, nil
	}
	if err != nil {
		return rec, fmt.Errorf("get google sheets settings: %w", err)
	}
	if err := json.Unmarshal(cfg, &rec.Config); err != nil {
		return rec, fmt.Errorf("decode google sheets settings: %w", err)
	}
	if len(rec.Config.Columns) == 0 {
		rec.Config.Columns = model.DefaultSheetColumns()
	}
	if rec.Config.Stages == nil {
		rec.Config.Stages = []model.Stage{}
	}
	rec.Status = model.SheetStatus(status)
	if len(sealed) > 0 {
		plain, err := s.box.Open(sealed, sheetsSecretsContext)
		var sec sheetsSecrets
		if err != nil || json.Unmarshal(plain, &sec) != nil {
			rec.TokenLocked = true
		} else {
			rec.RefreshToken = sec.RefreshToken
		}
	}
	return rec, nil
}

// SaveSheetsConnection stores a new Google sign-in: the refresh token (sealed),
// the account and the admin who connected it. The chosen sheet is kept when
// the same account reconnects, and cleared for a different one, which cannot
// open it.
func (s *Store) SaveSheetsConnection(ctx context.Context, refreshToken, email string, memberID int64) error {
	if s.box == nil {
		return ErrNoEncryptionKey
	}
	plain, err := json.Marshal(sheetsSecrets{RefreshToken: refreshToken})
	if err != nil {
		return fmt.Errorf("encode google token: %w", err)
	}
	sealed, err := s.box.Seal(plain, sheetsSecretsContext)
	if err != nil {
		return fmt.Errorf("seal google token: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('integration_settings:google_sheets', 0))`); err != nil {
		return fmt.Errorf("lock google sheets settings: %w", err)
	}
	prev, err := s.GetSheets(ctx)
	if err != nil {
		return err
	}
	cfg := prev.Config
	if cfg.AccountEmail != email {
		fresh := model.DefaultSheetsConfig()
		fresh.Columns, fresh.Stages = cfg.Columns, cfg.Stages
		cfg = fresh
	}
	cfg.AccountEmail = email
	cfg.ConfiguredBy = memberID
	cfg.LastHash = ""
	raw, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode google sheets settings: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO integration_settings (provider, config, secrets, status, last_error, last_error_at)
		VALUES ($1, $2, $3, 'connected', '', NULL)
		ON CONFLICT (provider) DO UPDATE
		SET config = EXCLUDED.config, secrets = EXCLUDED.secrets, status = 'connected',
		    last_error = '', last_error_at = NULL, updated_at = now()`,
		sheetsProvider, raw, sealed); err != nil {
		return fmt.Errorf("save google connection: %w", err)
	}
	return tx.Commit(ctx)
}

// SaveSheetsConfig stores the sheet and sync choices. The sign-in is untouched.
func (s *Store) SaveSheetsConfig(ctx context.Context, cfg model.SheetsConfig) error {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode google sheets settings: %w", err)
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO integration_settings (provider, config) VALUES ($1, $2)
		ON CONFLICT (provider) DO UPDATE SET config = EXCLUDED.config, updated_at = now()`,
		sheetsProvider, raw); err != nil {
		return fmt.Errorf("save google sheets settings: %w", err)
	}
	return nil
}

// SetSheetsHash records the fingerprint of what was last written.
func (s *Store) SetSheetsHash(ctx context.Context, hash string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE integration_settings SET config = jsonb_set(config, '{last_hash}', to_jsonb($2::text))
		WHERE provider = $1`, sheetsProvider, hash)
	if err != nil {
		return fmt.Errorf("save sheets hash: %w", err)
	}
	return nil
}

// DisconnectSheets forgets the Google sign-in and turns automatic sync off.
// The column choices are kept; the spreadsheet is forgotten, since a new
// sign-in may not be able to open it. History is kept.
func (s *Store) DisconnectSheets(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE integration_settings
		SET secrets = NULL, status = 'not_connected', last_error = '', last_error_at = NULL,
		    config = config - 'spreadsheet_id' - 'spreadsheet_name' - 'spreadsheet_url' - 'sheet_title'
		             - 'account_email' - 'last_hash' || '{"auto_sync": false}'::jsonb,
		    updated_at = now()
		WHERE provider = $1`, sheetsProvider)
	if err != nil {
		return fmt.Errorf("disconnect google sheets: %w", err)
	}
	return nil
}

// SetSheetsStatus records the connection's health. A problem moves the status
// to error; none clears the last error.
func (s *Store) SetSheetsStatus(ctx context.Context, problem string) error {
	var err error
	if problem == "" {
		_, err = s.pool.Exec(ctx, `
			UPDATE integration_settings SET status = 'connected', last_error = '', last_error_at = NULL
			WHERE provider = $1 AND secrets IS NOT NULL`, sheetsProvider)
	} else {
		_, err = s.pool.Exec(ctx, `
			UPDATE integration_settings SET status = 'error', last_error = $2, last_error_at = now()
			WHERE provider = $1`, sheetsProvider, truncateRunes(problem, 1000))
	}
	if err != nil {
		return fmt.Errorf("record google sheets status: %w", err)
	}
	return nil
}

// --- OAuth state ---------------------------------------------------------------

// CreateOAuthState remembers a sign-in started by memberID, for ten minutes.
func (s *Store) CreateOAuthState(ctx context.Context, stateHash []byte, memberID int64, verifier string) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM google_oauth_states WHERE expires_at < now()`); err != nil {
		return fmt.Errorf("clear old oauth states: %w", err)
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO google_oauth_states (state_hash, member_id, code_verifier, expires_at)
		VALUES ($1, $2, $3, now() + interval '10 minutes')`, stateHash, memberID, verifier); err != nil {
		return fmt.Errorf("save oauth state: %w", err)
	}
	return nil
}

// TakeOAuthState uses up a state: it is deleted whatever happens, so it works
// once. ErrOAuthState unless it exists, has not expired and was started by
// memberID. Returns the PKCE verifier.
func (s *Store) TakeOAuthState(ctx context.Context, stateHash []byte, memberID int64) (string, error) {
	var owner int64
	var verifier string
	var expired bool
	err := s.pool.QueryRow(ctx, `
		DELETE FROM google_oauth_states WHERE state_hash = $1
		RETURNING member_id, code_verifier, expires_at < now()`, stateHash).Scan(&owner, &verifier, &expired)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (expired || owner != memberID)) {
		return "", ErrOAuthState
	}
	if err != nil {
		return "", fmt.Errorf("take oauth state: %w", err)
	}
	return verifier, nil
}

// MemberViewer returns a member as a viewer, if they may still log in.
func (s *Store) MemberViewer(ctx context.Context, id int64) (model.Viewer, error) {
	var v model.Viewer
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, email, role FROM team_members WHERE id = $1 AND active`, id).
		Scan(&v.MemberID, &v.Name, &v.Email, &v.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, fmt.Errorf("member viewer: %w", err)
	}
	return v, nil
}

// --- export --------------------------------------------------------------------

// ExportLeads reads the leads to export, oldest first, scoped to the viewer
// in ctx like every other read: a viewer who sees only their own leads
// exports only those. stages narrows it; empty means all.
func (s *Store) ExportLeads(ctx context.Context, stages []model.Stage) ([]model.ExportLead, error) {
	names := make([]string, len(stages))
	for i, st := range stages {
		names[i] = string(st)
	}
	rows, err := s.query(ctx, `
		SELECT l.id, l.name, l.company, l.email, COALESCE(l.phone, ''), l.source::text, l.stage::text,
		       COALESCE(m.name, ''), l.deal_value,
		       COALESCE(to_char((SELECT min(t.due_date) FROM tasks t
		                         WHERE t.lead_id = l.id AND t.completed_at IS NULL AND t.cancelled_at IS NULL),
		                        'YYYY-MM-DD'), ''),
		       l.created_at, l.updated_at, l.notes
		FROM leads l
		LEFT JOIN team_members m ON m.id = l.owner_id
		WHERE cardinality($1::text[]) = 0 OR l.stage::text = ANY($1)
		ORDER BY l.id`, names)
	if err != nil {
		return nil, fmt.Errorf("export leads: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.ExportLead, error) {
		var l model.ExportLead
		var source, stage string
		err := row.Scan(&l.ID, &l.Name, &l.Company, &l.Email, &l.Phone, &source, &stage,
			&l.OwnerName, &l.DealValue, &l.NextFollowUp, &l.CreatedAt, &l.UpdatedAt, &l.Notes)
		l.Source, l.Stage = model.Source(source), model.Stage(stage)
		return l, err
	})
}

// --- history -------------------------------------------------------------------

// StartSheetSync records a sync as running. startedBy is nil for automatic ones.
func (s *Store) StartSheetSync(ctx context.Context, trigger string, startedBy, retryOf *int64) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO sheet_syncs (trigger, started_by, retry_of) VALUES ($1, $2, $3) RETURNING id`,
		trigger, startedBy, retryOf).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("start sheet sync: %w", err)
	}
	return id, nil
}

// FinishSheetSync records how a sync ended. A sync that wrote everything
// also moves last_sync_at, which the card shows.
func (s *Store) FinishSheetSync(ctx context.Context, sync model.SheetSync) error {
	if _, err := s.pool.Exec(ctx, `
		UPDATE sheet_syncs
		SET status = $2, finished_at = now(), leads_total = $3, rows_updated = $4, rows_added = $5,
		    rows_failed = $6, error = $7
		WHERE id = $1`,
		sync.ID, sync.Status, sync.LeadsTotal, sync.RowsUpdated, sync.RowsAdded, sync.RowsFailed,
		truncateRunes(sync.Error, 2000)); err != nil {
		return fmt.Errorf("finish sheet sync: %w", err)
	}
	if sync.Status == "success" {
		if _, err := s.pool.Exec(ctx, `
			UPDATE integration_settings SET last_sync_at = now(), last_sync_count = $2
			WHERE provider = $1`, sheetsProvider, sync.LeadsTotal); err != nil {
			return fmt.Errorf("record sheet sync: %w", err)
		}
	}
	return nil
}

// FailStaleSheetSyncs marks syncs left running by a server that stopped
// mid-sync as failed, so history does not show them running forever.
func (s *Store) FailStaleSheetSyncs(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE sheet_syncs SET status = 'failed', finished_at = now(),
		       error = 'The server stopped during this sync. Retry it.'
		WHERE status = 'running' AND started_at < now() - interval '15 minutes'`)
	if err != nil {
		return fmt.Errorf("fail stale sheet syncs: %w", err)
	}
	return nil
}

const sheetSyncColumns = `
	y.id, y.trigger, y.status, y.started_at, y.finished_at, y.leads_total, y.rows_updated,
	y.rows_added, y.rows_failed, y.error, COALESCE(m.name, ''), y.retry_of`

func scanSheetSync(row pgx.CollectableRow) (model.SheetSync, error) {
	var y model.SheetSync
	err := row.Scan(&y.ID, &y.Trigger, &y.Status, &y.StartedAt, &y.FinishedAt, &y.LeadsTotal,
		&y.RowsUpdated, &y.RowsAdded, &y.RowsFailed, &y.Error, &y.StartedBy, &y.RetryOf)
	return y, err
}

// ListSheetSyncs returns the latest syncs, newest first.
func (s *Store) ListSheetSyncs(ctx context.Context, limit int) ([]model.SheetSync, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+sheetSyncColumns+`
		FROM sheet_syncs y LEFT JOIN team_members m ON m.id = y.started_by
		ORDER BY y.started_at DESC, y.id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list sheet syncs: %w", err)
	}
	list, err := pgx.CollectRows(rows, scanSheetSync)
	if err != nil {
		return nil, fmt.Errorf("scan sheet syncs: %w", err)
	}
	if list == nil {
		list = []model.SheetSync{}
	}
	return list, nil
}

// GetSheetSync returns one sync. ErrNotFound for an unknown id.
func (s *Store) GetSheetSync(ctx context.Context, id int64) (model.SheetSync, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+sheetSyncColumns+`
		FROM sheet_syncs y LEFT JOIN team_members m ON m.id = y.started_by WHERE y.id = $1`, id)
	if err != nil {
		return model.SheetSync{}, fmt.Errorf("get sheet sync: %w", err)
	}
	y, err := pgx.CollectExactlyOneRow(rows, scanSheetSync)
	if errors.Is(err, pgx.ErrNoRows) {
		return y, ErrNotFound
	}
	return y, err
}

// LastSheetSyncAt is when the last sync of any kind started, to pace
// automatic ones; zero if there was none.
func (s *Store) LastSheetSyncAt(ctx context.Context) (time.Time, error) {
	var at *time.Time
	if err := s.pool.QueryRow(ctx, `SELECT max(started_at) FROM sheet_syncs`).Scan(&at); err != nil {
		return time.Time{}, fmt.Errorf("last sheet sync: %w", err)
	}
	if at == nil {
		return time.Time{}, nil
	}
	return *at, nil
}

// LockSheetSync takes the database lock that keeps syncs one at a time, even
// across several servers. ok is false when another sync holds it. release
// must be called when ok.
func (s *Store) LockSheetSync(ctx context.Context) (release func(), ok bool, err error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("acquire connection: %w", err)
	}
	const key = `hashtextextended('sheet_syncs', 0)`
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(`+key+`)`).Scan(&ok); err != nil {
		conn.Release()
		return nil, false, fmt.Errorf("lock sheet sync: %w", err)
	}
	if !ok {
		conn.Release()
		return nil, false, nil
	}
	return func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.Exec(unlockCtx, `SELECT pg_advisory_unlock(`+key+`)`)
		conn.Release()
	}, true, nil
}
