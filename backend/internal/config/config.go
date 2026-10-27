// Package config loads runtime configuration from environment variables.
//
// Everything the server needs to run lives in one struct that is built once at
// startup. Nothing else in the codebase calls os.Getenv, so there is exactly one
// place to look when you want to know what is configurable.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"

	"crm-lead-management/backend/internal/secrets"
)

// Config is the fully resolved configuration for one run of the server.
type Config struct {
	// Env is "development" or "production". It only changes logging verbosity
	// and how much error detail is returned to clients.
	Env string

	// Port is the TCP port the HTTP server listens on.
	Port string

	// DatabaseURL is a libpq-style connection string, e.g.
	// postgres://user:pass@localhost:5432/crm?sslmode=disable
	DatabaseURL string

	// Timezone is the IANA zone (e.g. "Asia/Kolkata") every database session
	// runs in. It decides what CURRENT_DATE means for "due today" and
	// "overdue", and where report weeks start. Empty keeps the database's own
	// setting, which is UTC in the postgres Docker image -- a day behind India
	// until 05:30.
	Timezone string

	// AllowedOrigins are the browser origins permitted by CORS. The React dev
	// server runs on a different port from the API, which makes every frontend
	// request cross-origin, so this has to include it.
	AllowedOrigins []string

	// ShutdownTimeout bounds how long we wait for in-flight requests to finish
	// when the process is asked to stop.
	ShutdownTimeout time.Duration

	// BaseURL is where the web app is opened, used for the "open the lead"
	// link in reminder emails.
	BaseURL string

	// DigestHour is the hour of the day (0-23, in Timezone) from which each
	// team member's daily summary email goes out. DIGEST_HOUR, default 8.
	DigestHour int

	// SMTP is the mail server reminder emails are sent through. Leaving
	// SMTP_HOST empty turns email off; reminders then appear in the app only.
	SMTP SMTPConfig

	// PublicAPIURL is where Meta and Google can reach this server from the
	// internet, e.g. https://crm.example.com, to show the webhook addresses
	// to paste into them. PUBLIC_API_URL; optional.
	PublicAPIURL string

	// Meta is the Meta Lead Ads webhook; GoogleLeadsKey checks Google Ads
	// lead form webhooks. Unset, that webhook refuses everything.
	Meta           MetaConfig
	GoogleLeadsKey string

	// AdminAPIKey guards the integration settings, which hold credentials:
	// the CRM has no login, so those endpoints ask for this key instead
	// (X-Admin-Key). ADMIN_API_KEY; empty refuses every such request.
	AdminAPIKey string

	// SecretsKey encrypts the credentials saved from the Meta Ads settings
	// page (see package secrets). SECRETS_ENCRYPTION_KEY, 32 bytes as base64
	// or hex; empty means none can be saved.
	SecretsKey []byte

	// GoogleSheets is the OAuth app leads are exported to Google Sheets
	// with. Unset, the Google Sheets card says what to configure.
	GoogleSheets GoogleSheetsConfig
}

// MetaConfig holds the Meta app's settings for Lead Ads. The secret and the
// token never leave the server: they are not logged and no endpoint returns
// them.
type MetaConfig struct {
	AppSecret       string // checks that a webhook call really came from Meta
	VerifyToken     string // any phrase, typed into the app's webhook settings too
	PageAccessToken string // reads the answers of each lead (leads_retrieval)
	GraphURL        string // Graph API base, e.g. https://graph.facebook.com/v23.0
}

// Configured reports whether Meta leads can be received.
func (m MetaConfig) Configured() bool {
	return m.AppSecret != "" && m.VerifyToken != "" && m.PageAccessToken != ""
}

// GoogleSheetsCallbackPath is the route Google sends the admin back to after
// consent. The router registers exactly this path, and GOOGLE_OAUTH_REDIRECT_URL
// must end with it.
const GoogleSheetsCallbackPath = "/api/integrations/google-sheets/callback"

// GoogleSheetsConfig is the Google Cloud OAuth client the Google Sheets
// export signs in with. The client secret never leaves the server: it is not
// logged and no endpoint returns it.
type GoogleSheetsConfig struct {
	ClientID     string // GOOGLE_OAUTH_CLIENT_ID
	ClientSecret string // GOOGLE_OAUTH_CLIENT_SECRET
	// RedirectURL is where Google sends the admin back, exactly as entered
	// under "Authorized redirect URIs" in Google Cloud. GOOGLE_OAUTH_REDIRECT_URL.
	RedirectURL string
	// PickerAPIKey and ProjectNumber turn on "Choose from Google Drive", which
	// gives the CRM access to one existing spreadsheet. Optional: without
	// them a new spreadsheet can still be created. GOOGLE_PICKER_API_KEY and
	// GOOGLE_CLOUD_PROJECT_NUMBER.
	PickerAPIKey  string
	ProjectNumber string

	// Google's endpoints. Tests point them at a fake server.
	AuthURL     string
	TokenURL    string
	RevokeURL   string
	UserInfoURL string
	SheetsURL   string
	DriveURL    string
}

// Configured reports whether an admin can connect a Google account.
func (g GoogleSheetsConfig) Configured() bool {
	return g.ClientID != "" && g.ClientSecret != "" && g.RedirectURL != ""
}

// PickerConfigured reports whether "Choose from Google Drive" can be offered.
func (g GoogleSheetsConfig) PickerConfigured() bool {
	return g.PickerAPIKey != "" && g.ProjectNumber != ""
}

// SMTPConfig holds the outgoing mail settings. The password never leaves the
// server: it is not logged and no endpoint returns it.
type SMTPConfig struct {
	Host     string
	Port     string
	Username string
	Password string
	From     string // e.g. "Pipeline CRM <you@gmail.com>"
}

// Configured reports whether enough is set to try sending.
func (s SMTPConfig) Configured() bool { return s.Host != "" && s.From != "" }

// IsDevelopment reports whether the server is running in development mode.
func (c Config) IsDevelopment() bool { return c.Env == "development" }

// Load reads configuration from the environment.
//
// A .env file, if present in the working directory, is loaded first as a
// convenience for local development. Real environment variables always win over
// the file, which is what you want in Docker and CI.
//
// It returns an error rather than exiting, so main can decide how to report it.
func Load() (Config, error) {
	// A missing .env is not an error -- in production there simply isn't one.
	_ = godotenv.Load()

	cfg := Config{
		Env:             envOr("APP_ENV", "development"),
		Port:            envOr("PORT", "8080"),
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		Timezone:        strings.TrimSpace(os.Getenv("APP_TIMEZONE")),
		ShutdownTimeout: 10 * time.Second,
		BaseURL:         strings.TrimRight(envOr("APP_BASE_URL", "http://localhost:5173"), "/"),
		SMTP: SMTPConfig{
			Host:     strings.TrimSpace(os.Getenv("SMTP_HOST")),
			Port:     envOr("SMTP_PORT", "587"),
			Username: strings.TrimSpace(os.Getenv("SMTP_USERNAME")),
			// Not trimmed of inner spaces: Google shows app passwords in groups
			// of four ("abcd efgh ..."), and the mail package strips those.
			Password: strings.TrimSpace(os.Getenv("SMTP_PASSWORD")),
			From:     strings.TrimSpace(os.Getenv("SMTP_FROM")),
		},
		PublicAPIURL: strings.TrimRight(strings.TrimSpace(os.Getenv("PUBLIC_API_URL")), "/"),
		Meta: MetaConfig{
			AppSecret:       strings.TrimSpace(os.Getenv("META_APP_SECRET")),
			VerifyToken:     strings.TrimSpace(os.Getenv("META_VERIFY_TOKEN")),
			PageAccessToken: strings.TrimSpace(os.Getenv("META_PAGE_ACCESS_TOKEN")),
		},
		GoogleLeadsKey: strings.TrimSpace(os.Getenv("GOOGLE_LEADS_KEY")),
		AdminAPIKey:    strings.TrimSpace(os.Getenv("ADMIN_API_KEY")),
	}

	if raw := strings.TrimSpace(os.Getenv("SECRETS_ENCRYPTION_KEY")); raw != "" {
		key, err := secrets.ParseKey(raw)
		if err != nil {
			return Config{}, fmt.Errorf("SECRETS_ENCRYPTION_KEY %w", err)
		}
		cfg.SecretsKey = key
	}
	// A short admin key is guessable; refuse it rather than look protected.
	if cfg.AdminAPIKey != "" && len(cfg.AdminAPIKey) < 16 {
		return Config{}, fmt.Errorf("ADMIN_API_KEY must be at least 16 characters; generate one with: openssl rand -hex 24")
	}

	// Without a version, Graph API calls use the app's own default version,
	// which Meta keeps current; META_GRAPH_VERSION (e.g. v23.0) pins one.
	cfg.Meta.GraphURL = "https://graph.facebook.com"
	if v := strings.Trim(strings.TrimSpace(os.Getenv("META_GRAPH_VERSION")), "/"); v != "" {
		cfg.Meta.GraphURL += "/" + v
	}

	cfg.GoogleSheets = GoogleSheetsConfig{
		ClientID:      strings.TrimSpace(os.Getenv("GOOGLE_OAUTH_CLIENT_ID")),
		ClientSecret:  strings.TrimSpace(os.Getenv("GOOGLE_OAUTH_CLIENT_SECRET")),
		RedirectURL:   envOr("GOOGLE_OAUTH_REDIRECT_URL", "http://localhost:"+cfg.Port+GoogleSheetsCallbackPath),
		PickerAPIKey:  strings.TrimSpace(os.Getenv("GOOGLE_PICKER_API_KEY")),
		ProjectNumber: strings.TrimSpace(os.Getenv("GOOGLE_CLOUD_PROJECT_NUMBER")),
		AuthURL:       "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:      "https://oauth2.googleapis.com/token",
		RevokeURL:     "https://oauth2.googleapis.com/revoke",
		UserInfoURL:   "https://openidconnect.googleapis.com/v1/userinfo",
		SheetsURL:     "https://sheets.googleapis.com/v4",
		DriveURL:      "https://www.googleapis.com/drive/v3",
	}

	if err := checkRedirectURL(cfg.GoogleSheets.RedirectURL); err != nil {
		return Config{}, err
	}

	cfg.DigestHour = 8
	if raw := strings.TrimSpace(os.Getenv("DIGEST_HOUR")); raw != "" {
		hour, err := strconv.Atoi(raw)
		if err != nil || hour < 0 || hour > 23 {
			return Config{}, fmt.Errorf("DIGEST_HOUR must be a whole hour from 0 to 23, got %q", raw)
		}
		cfg.DigestHour = hour
	}

	// The From address defaults to the login, which is what Gmail requires.
	if cfg.SMTP.From == "" {
		cfg.SMTP.From = cfg.SMTP.Username
	}

	origins := envOr("CORS_ALLOWED_ORIGINS", "http://localhost:5173,http://127.0.0.1:5173")
	for _, o := range strings.Split(origins, ",") {
		if o = strings.TrimSpace(o); o != "" {
			cfg.AllowedOrigins = append(cfg.AllowedOrigins, o)
		}
	}

	// Fail loudly and immediately on missing required config. A server that
	// starts up and only breaks on the first database request is much harder to
	// debug than one that refuses to start.
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required (copy .env.example to .env and fill it in)")
	}

	return cfg, nil
}

// envOr returns the value of the environment variable key, or def if it is unset
// or empty.
func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// checkRedirectURL makes sure GOOGLE_OAUTH_REDIRECT_URL can work: Google
// compares it character for character with the client's "Authorized redirect
// URIs", and sends the admin to it, so it must be a full http(s) address of
// this server's callback route. One that is not would only fail later, at
// Google, as redirect_uri_mismatch or a 404.
func checkRedirectURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("GOOGLE_OAUTH_REDIRECT_URL must be a full http(s) address, e.g. http://localhost:8080%s, got %q", GoogleSheetsCallbackPath, raw)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("GOOGLE_OAUTH_REDIRECT_URL must not have a ?query or #fragment, got %q", raw)
	}
	if u.Path != GoogleSheetsCallbackPath {
		return fmt.Errorf("GOOGLE_OAUTH_REDIRECT_URL must end with %s, the route this server answers on; got path %q in %q", GoogleSheetsCallbackPath, u.Path, raw)
	}
	return nil
}
