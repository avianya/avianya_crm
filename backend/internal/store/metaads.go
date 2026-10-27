package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"crm-lead-management/backend/internal/model"
	"crm-lead-management/backend/internal/secrets"
)

// The Meta Ads integration's storage: its settings (credentials sealed), the
// lead quality rules, each Meta lead's attribution, and the outbox of
// conversion events for the Conversions API.

var (
	// ErrNoEncryptionKey means a credential was to be saved but there is no
	// SECRETS_ENCRYPTION_KEY to seal it with. -> 503
	ErrNoEncryptionKey = errors.New("store: SECRETS_ENCRYPTION_KEY is not set, so credentials cannot be saved")

	// ErrMetaNotTested means live mode was asked for before a connection
	// test passed. -> 409
	ErrMetaNotTested = errors.New("store: test the connection before turning test mode off")

	// ErrCAPINotRetryable means the event is already sent, or queued. -> 409
	ErrCAPINotRetryable = errors.New("store: only a failed or scheduled event can be retried")
)

const metaProvider = "meta"

// secretsContext ties the sealed blob to this row, so it cannot be opened if
// copied anywhere else.
const secretsContext = "integration_settings:meta"

// querier is what a pool and a transaction have in common, for reads that run
// either inside or outside one.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// ---------------------------------------------------------------------------
// Settings
// ---------------------------------------------------------------------------

// GetMetaSettings returns the saved settings, or the defaults when nothing has
// been saved. Credentials that cannot be opened (no key, or a changed one)
// come back empty with SecretsLocked set, rather than as an error: the rest of
// the page still works, and saving new ones fixes it.
func (s *Store) GetMetaSettings(ctx context.Context) (model.MetaSettingsRecord, error) {
	return s.metaSettings(ctx, s.pool)
}

func (s *Store) metaSettings(ctx context.Context, q querier) (model.MetaSettingsRecord, error) {
	rec := model.MetaSettingsRecord{Config: model.DefaultMetaConfig(), Status: model.MetaNotConnected, SecretHints: map[string]string{}}
	var (
		config, hints      []byte
		sealed             []byte
		status, capiHealth string
	)
	err := q.QueryRow(ctx, `
		SELECT config, secrets, secret_hints, status, last_tested_at, last_sync_at, last_sync_count,
		       last_error, last_error_at, updated_at,
		       COALESCE(capi_status, ''), capi_status_detail, capi_status_at, capi_token_rotated_at
		FROM integration_settings WHERE provider = $1`, metaProvider).
		Scan(&config, &sealed, &hints, &status, &rec.LastTestedAt, &rec.LastSyncAt, &rec.LastSyncCount,
			&rec.LastError, &rec.LastErrorAt, &rec.UpdatedAt,
			&capiHealth, &rec.CAPIHealthDetail, &rec.CAPIHealthAt, &rec.CAPITokenRotatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return rec, nil
	}
	if err != nil {
		return rec, fmt.Errorf("get meta settings: %w", err)
	}

	// Decoded over the defaults, so a setting added later starts at its default.
	if err := json.Unmarshal(config, &rec.Config); err != nil {
		return rec, fmt.Errorf("decode meta settings: %w", err)
	}
	if rec.Config.FormIDs == nil {
		rec.Config.FormIDs = []string{}
	}
	if rec.Config.EventMapping == nil {
		rec.Config.EventMapping = []model.StageEvent{}
	}
	_ = json.Unmarshal(hints, &rec.SecretHints)
	rec.Status = model.MetaStatus(status)
	rec.CAPIHealth = model.CAPIHealth(capiHealth)

	if len(sealed) > 0 {
		plain, err := s.box.Open(sealed, secretsContext)
		if err != nil {
			rec.SecretsLocked = true
			return rec, nil
		}
		if err := json.Unmarshal(plain, &rec.Secrets); err != nil {
			rec.SecretsLocked = true
		}
	}
	return rec, nil
}

// SaveMetaSettings stores the settings. replace holds only the credentials to
// change (blank ones keep what is saved). Turning test mode off while sending
// conversion events needs a connection test to have passed first.
func (s *Store) SaveMetaSettings(ctx context.Context, cfg model.MetaConfig, replace model.MetaSecrets) (model.MetaSettingsRecord, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.MetaSettingsRecord{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Serialise saves: two at once would each merge into the same old secrets.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('integration_settings:meta', 0))`); err != nil {
		return model.MetaSettingsRecord{}, fmt.Errorf("lock meta settings: %w", err)
	}
	prev, err := s.metaSettings(ctx, tx)
	if err != nil {
		return model.MetaSettingsRecord{}, err
	}

	// Either connection test counts: the lead-ads one or the Conversions API one.
	tested := prev.Status == model.MetaConnected || prev.CAPIHealth == model.CAPIConnected
	if cfg.CAPIEnabled && !cfg.TestMode && !tested {
		return model.MetaSettingsRecord{}, ErrMetaNotTested
	}

	configJSON, err := json.Marshal(cfg)
	if err != nil {
		return model.MetaSettingsRecord{}, fmt.Errorf("encode meta settings: %w", err)
	}

	if replace.Empty() {
		// Credentials untouched: leave the sealed blob as it is, which works
		// even without the key.
		_, err = tx.Exec(ctx, `
			INSERT INTO integration_settings (provider, config) VALUES ($1, $2)
			ON CONFLICT (provider) DO UPDATE SET config = EXCLUDED.config, updated_at = now()`,
			metaProvider, configJSON)
	} else {
		if s.box == nil {
			return model.MetaSettingsRecord{}, ErrNoEncryptionKey
		}
		// Locked secrets read as empty, so replacing some of them keeps none
		// of the unreadable rest; the page says which still need entering.
		merged := prev.Secrets.Merge(replace)
		plain, err := json.Marshal(merged)
		if err != nil {
			return model.MetaSettingsRecord{}, fmt.Errorf("encode meta secrets: %w", err)
		}
		sealed, err := s.box.Seal(plain, secretsContext)
		if err != nil {
			return model.MetaSettingsRecord{}, fmt.Errorf("seal meta secrets: %w", err)
		}
		hints := map[string]string{}
		for _, name := range model.SecretNames {
			if v := merged.Get(name); v != "" {
				hints[name] = secrets.Mask(v)
			}
		}
		hintsJSON, _ := json.Marshal(hints)
		// A new Conversions API token is a rotation: its health is unknown
		// until it is tested or used.
		rotated := replace.CAPIToken != "" && replace.CAPIToken != prev.Secrets.CAPIToken
		_, err = tx.Exec(ctx, `
			INSERT INTO integration_settings (provider, config, secrets, secret_hints, capi_token_rotated_at)
			VALUES ($1, $2, $3, $4, CASE WHEN $5 THEN now() END)
			ON CONFLICT (provider) DO UPDATE
			SET config = EXCLUDED.config, secrets = EXCLUDED.secrets, secret_hints = EXCLUDED.secret_hints,
			    capi_token_rotated_at = CASE WHEN $5 THEN now() ELSE integration_settings.capi_token_rotated_at END,
			    capi_status = CASE WHEN $5 THEN NULL ELSE integration_settings.capi_status END,
			    capi_status_detail = CASE WHEN $5 THEN '' ELSE integration_settings.capi_status_detail END,
			    updated_at = now()`,
			metaProvider, configJSON, sealed, hintsJSON, rotated)
		if err != nil {
			return model.MetaSettingsRecord{}, fmt.Errorf("save meta settings: %w", err)
		}
	}
	if err != nil {
		return model.MetaSettingsRecord{}, fmt.Errorf("save meta settings: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return model.MetaSettingsRecord{}, fmt.Errorf("commit meta settings: %w", err)
	}
	return s.GetMetaSettings(ctx)
}

// DisconnectMeta forgets every saved credential, turns conversion events off
// and marks the integration not connected. IDs and the event mapping are kept,
// so reconnecting is just entering the tokens again. Events already queued
// stay pending until it is reconnected.
func (s *Store) DisconnectMeta(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE integration_settings
		SET secrets = NULL, secret_hints = '{}',
		    config = jsonb_set(config, '{capi_enabled}', 'false'),
		    status = 'not_connected', last_error = '', last_error_at = NULL, updated_at = now()
		WHERE provider = $1`, metaProvider)
	if err != nil {
		return fmt.Errorf("disconnect meta: %w", err)
	}
	return nil
}

// RecordMetaTest stores a Test Connection's outcome as the status.
func (s *Store) RecordMetaTest(ctx context.Context, ok bool, problem string) error {
	status := model.MetaConnected
	if !ok {
		status = model.MetaError
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO integration_settings (provider, status, last_tested_at, last_error, last_error_at)
		VALUES ($1, $2, now(), $3, CASE WHEN $3 = '' THEN NULL ELSE now() END)
		ON CONFLICT (provider) DO UPDATE
		SET status = EXCLUDED.status, last_tested_at = EXCLUDED.last_tested_at,
		    last_error = EXCLUDED.last_error, last_error_at = EXCLUDED.last_error_at`,
		metaProvider, string(status), problem)
	if err != nil {
		return fmt.Errorf("record meta test: %w", err)
	}
	return nil
}

// RecordMetaSync stores a sync's outcome. A clean one moves last_sync_at to
// when it started (so nothing submitted during it is skipped next time); one
// with a problem keeps the old time and records the error.
func (s *Store) RecordMetaSync(ctx context.Context, startedAt time.Time, count int, problem string) error {
	var err error
	if problem == "" {
		_, err = s.pool.Exec(ctx, `
			INSERT INTO integration_settings (provider, last_sync_at, last_sync_count) VALUES ($1, $2, $3)
			ON CONFLICT (provider) DO UPDATE
			SET last_sync_at = EXCLUDED.last_sync_at, last_sync_count = EXCLUDED.last_sync_count`,
			metaProvider, startedAt, count)
	} else {
		err = s.RecordMetaError(ctx, problem)
	}
	if err != nil {
		return fmt.Errorf("record meta sync: %w", err)
	}
	return nil
}

// RecordMetaError notes a failure talking to Meta (a revoked token, a missing
// permission) so the settings page shows it, and marks the status error.
func (s *Store) RecordMetaError(ctx context.Context, problem string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO integration_settings (provider, status, last_error, last_error_at)
		VALUES ($1, 'error', $2, now())
		ON CONFLICT (provider) DO UPDATE
		SET status = 'error', last_error = EXCLUDED.last_error, last_error_at = EXCLUDED.last_error_at`,
		metaProvider, truncateRunes(problem, 1000))
	if err != nil {
		return fmt.Errorf("record meta error: %w", err)
	}
	return nil
}

// RecordCAPIHealth stores what Meta last said about the Conversions API
// token and dataset.
func (s *Store) RecordCAPIHealth(ctx context.Context, health model.CAPIHealth, detail string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO integration_settings (provider, capi_status, capi_status_detail, capi_status_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (provider) DO UPDATE
		SET capi_status = EXCLUDED.capi_status, capi_status_detail = EXCLUDED.capi_status_detail,
		    capi_status_at = EXCLUDED.capi_status_at`,
		metaProvider, string(health), truncateRunes(detail, 1000))
	if err != nil {
		return fmt.Errorf("record capi health: %w", err)
	}
	return nil
}

// Audit records an admin action on the integration. detail must never hold a
// secret: say "replaced", not the value.
func (s *Store) Audit(ctx context.Context, action, actor string, detail map[string]any) error {
	if detail == nil {
		detail = map[string]any{}
	}
	raw, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("encode audit detail: %w", err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO audit_log (action, actor, detail) VALUES ($1, $2, $3)`,
		action, actor, raw); err != nil {
		return fmt.Errorf("write audit log: %w", err)
	}
	return nil
}

// AuditEntry is one audit_log row.
type AuditEntry struct {
	ID        int64          `json:"id"`
	Action    string         `json:"action"`
	Actor     string         `json:"actor"`
	Detail    map[string]any `json:"detail"`
	CreatedAt time.Time      `json:"created_at"`
}

// ListAudit returns the latest audit entries, newest first.
func (s *Store) ListAudit(ctx context.Context, limit int) ([]AuditEntry, error) {
	rows, err := s.query(ctx, `
		SELECT id, action, actor, detail, created_at FROM audit_log ORDER BY created_at DESC, id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list audit log: %w", err)
	}
	entries, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (AuditEntry, error) {
		var e AuditEntry
		err := row.Scan(&e.ID, &e.Action, &e.Actor, &e.Detail, &e.CreatedAt)
		return e, err
	})
	if err != nil {
		return nil, fmt.Errorf("scan audit log: %w", err)
	}
	if entries == nil {
		entries = []AuditEntry{}
	}
	return entries, nil
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// ---------------------------------------------------------------------------
// Attribution
// ---------------------------------------------------------------------------

// insertAttributionTx records where a Meta lead came from. A lead keeps its
// first attribution, and a Meta lead id belongs to one lead: either conflict
// means there is nothing to add.
func insertAttributionTx(ctx context.Context, tx pgx.Tx, leadID int64, a model.MetaAttribution) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO meta_lead_attribution (lead_id, meta_lead_id, page_id, form_id, ad_account_id,
		    campaign_id, campaign_name, adset_id, adset_name, ad_id, ad_name, is_organic, meta_created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT DO NOTHING`,
		leadID, a.MetaLeadID, a.PageID, a.FormID, a.AdAccountID, a.CampaignID, a.CampaignName,
		a.AdsetID, a.AdsetName, a.AdID, a.AdName, a.IsOrganic, a.MetaCreatedAt)
	if err != nil {
		return fmt.Errorf("record attribution of lead %d: %w", leadID, err)
	}
	return nil
}

const attributionColumns = `
	m.lead_id, m.meta_lead_id, m.page_id, m.form_id, m.ad_account_id, m.campaign_id, m.campaign_name,
	m.adset_id, m.adset_name, m.ad_id, m.ad_name, m.is_organic, m.meta_created_at, m.created_at`

func attributionDest(a *model.MetaAttribution) []any {
	return []any{&a.LeadID, &a.MetaLeadID, &a.PageID, &a.FormID, &a.AdAccountID, &a.CampaignID, &a.CampaignName,
		&a.AdsetID, &a.AdsetName, &a.AdID, &a.AdName, &a.IsOrganic, &a.MetaCreatedAt, &a.CreatedAt}
}

// metaAttribution returns a lead's attribution, or nil for a lead not from Meta.
func (s *Store) metaAttribution(ctx context.Context, leadID int64) (*model.MetaAttribution, error) {
	var a model.MetaAttribution
	err := s.queryRow(ctx, `SELECT `+attributionColumns+` FROM meta_lead_attribution m WHERE m.lead_id = $1`, leadID).
		Scan(attributionDest(&a)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get attribution of lead %d: %w", leadID, err)
	}
	return &a, nil
}
