// Package config loads runtime configuration from environment variables.
//
// Everything the server needs to run lives in one struct that is built once at
// startup. Nothing else in the codebase calls os.Getenv, so there is exactly one
// place to look when you want to know what is configurable.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
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

	// AllowedOrigins are the browser origins permitted by CORS. The React dev
	// server runs on a different port from the API, which makes every frontend
	// request cross-origin, so this has to include it.
	AllowedOrigins []string

	// ShutdownTimeout bounds how long we wait for in-flight requests to finish
	// when the process is asked to stop.
	ShutdownTimeout time.Duration
}

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
		ShutdownTimeout: 10 * time.Second,
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
