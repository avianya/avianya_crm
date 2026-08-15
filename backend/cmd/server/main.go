// Command server is the entry point for the CRM API.
//
// It does one job: build every dependency in the right order, hand them to the
// router, and serve HTTP until told to stop. All the actual behaviour lives in
// internal/ -- this file is the wiring diagram, and should stay short enough to
// read in one sitting.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"crm-lead-management/backend/internal/api"
	"crm-lead-management/backend/internal/config"
	"crm-lead-management/backend/internal/store"
)

func main() {
	// main stays a three-liner so that every startup failure takes the same
	// path. run() returns errors instead of calling os.Exit itself, which means
	// deferred cleanup (closing the database pool) actually runs -- os.Exit
	// skips deferred functions entirely.
	if err := run(); err != nil {
		slog.Error("server exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := newLogger(cfg)
	slog.SetDefault(logger)

	logger.Info("starting CRM API",
		slog.String("env", cfg.Env),
		slog.String("port", cfg.Port),
	)

	// notifyContext gives us a context that is cancelled when the OS asks the
	// process to stop: Ctrl-C (SIGINT) in a terminal, or SIGTERM from Docker or
	// a process manager. Everything downstream can then wind down in order.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Connect to Postgres before serving anything. If the database is down we
	// want to fail here, loudly, not on a user's first request.
	db, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	logger.Info("connected to database")

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: api.NewRouter(cfg, db, logger),

		// Timeouts, because the defaults are "wait forever". Without them a
		// handful of slow or malicious clients can hold connections open
		// indefinitely and starve everyone else.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// ListenAndServe blocks, so it runs in its own goroutine and reports back
	// over a channel. That leaves this goroutine free to wait on the shutdown
	// signal below.
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("listening", slog.String("addr", "http://localhost:"+cfg.Port))
		serverErrors <- srv.ListenAndServe()
	}()

	// select blocks until one of these two things happens first.
	select {
	case err := <-serverErrors:
		// ErrServerClosed is the expected result of a deliberate Shutdown, so it
		// is not a failure. Anything else -- most often "port already in use" --
		// is.
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil

	case <-ctx.Done():
		logger.Info("shutdown signal received", slog.Duration("grace_period", cfg.ShutdownTimeout))

		// Graceful shutdown: stop accepting new connections, then give in-flight
		// requests a bounded window to finish. Without this, Ctrl-C during a
		// write could abandon a half-finished request.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			// Requests outlasted the grace period; drop them rather than hang.
			_ = srv.Close()
			return err
		}

		logger.Info("shutdown complete")
		return nil
	}
}

// newLogger builds the application logger.
//
// Development gets human-readable text; production gets JSON, which is what log
// aggregators (CloudWatch, Loki, Datadog) can actually index and search.
func newLogger(cfg config.Config) *slog.Logger {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}

	if cfg.IsDevelopment() {
		opts.Level = slog.LevelDebug
		return slog.New(slog.NewTextHandler(os.Stdout, opts))
	}

	return slog.New(slog.NewJSONHandler(os.Stdout, opts))
}
