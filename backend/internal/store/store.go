// Package store is the only package that talks to PostgreSQL.
//
// Every function takes a context.Context as its first argument. That is the Go
// convention for anything that does I/O: when the HTTP client hangs up, Gin
// cancels the request context, the cancellation flows down into pgx, and the
// query is abandoned instead of tying up a database connection for a response
// nobody is waiting for any more.
//
// SQL is written by hand rather than through an ORM. It is a few more lines, but
// the exact query that runs is right there in the file -- which matters for a
// system whose whole job is filtering, aggregating and paginating.
package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"crm-lead-management/backend/internal/secrets"
)

// Sentinel errors. The API layer compares against these with errors.Is to pick
// an HTTP status code, so handlers never need to know anything about Postgres
// error codes.
var (
	// ErrNotFound means the requested row does not exist. -> 404
	ErrNotFound = errors.New("store: resource not found")

	// ErrDuplicateEmail means another lead already uses that email. -> 409
	ErrDuplicateEmail = errors.New("store: a lead with this email already exists")

	// ErrDuplicateMemberEmail means another team member has that email. -> 409
	ErrDuplicateMemberEmail = errors.New("store: a team member with this email already exists")

	// ErrTaskCompleted means a completed event was asked to be cancelled. -> 409
	ErrTaskCompleted = errors.New("store: the event is already completed")

	// ErrActivityLocked means a server-written timeline entry (a stage or
	// owner change, a task update) was asked to be edited or deleted. -> 409
	ErrActivityLocked = errors.New("store: system timeline entries cannot be changed")
)

// FieldError is a problem with one input field that only the database could
// spot, such as a team member id that does not exist. -> 422 on that field.
type FieldError struct {
	Field   string
	Message string
}

func (e *FieldError) Error() string { return "store: " + e.Field + ": " + e.Message }

// unknownMember is the FieldError for a team member id that does not exist.
func unknownMember(field string) error {
	return &FieldError{Field: field, Message: "is not a team member (they may have been removed)"}
}

// Store owns the database connection pool and exposes one method per data
// operation the API needs.
type Store struct {
	pool *pgxpool.Pool

	// box seals the integration credentials; nil until SetSecretsBox, and
	// then saving a credential fails with ErrNoEncryptionKey.
	box *secrets.Box
}

// SetSecretsBox gives the store the key it seals credentials with. Called
// once at startup, before the store is used.
func (s *Store) SetSecretsBox(box *secrets.Box) { s.box = box }

// New parses the connection string, configures a pool and verifies the database
// is actually reachable before returning.
//
// Connecting eagerly at startup is a deliberate choice: it is far better to
// crash on boot with "cannot reach database" than to start up looking healthy
// and fail on the first user request.
//
// timezone, when not empty, is set on every connection so that CURRENT_DATE
// and now() agree with the users' calendar rather than the server's.
func New(ctx context.Context, databaseURL, timezone string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		// %w wraps the original error so callers can still inspect it with
		// errors.Is / errors.As while getting extra context in the message.
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}

	// Pool sizing for a small service. MaxConns must stay comfortably below
	// Postgres's max_connections (100 by default) so other clients -- psql, a
	// second instance of this server -- can still get in.
	cfg.MaxConns = 10
	cfg.MinConns = 1
	cfg.MaxConnLifetime = time.Hour
	cfg.MaxConnIdleTime = 30 * time.Minute
	cfg.HealthCheckPeriod = time.Minute
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second
	if timezone != "" {
		// Sent in the startup packet; an unknown zone fails the ping below.
		cfg.ConnConfig.RuntimeParams["timezone"] = timezone
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create connection pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := pool.Ping(pingCtx); err != nil {
		pool.Close() // don't leak the pool we just built
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return &Store{pool: pool}, nil
}

// Close releases every pooled connection. Call it on shutdown.
func (s *Store) Close() { s.pool.Close() }

// Ping checks the database is still reachable. Used by the health endpoint.
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

// ---------------------------------------------------------------------------
// Error translation
// ---------------------------------------------------------------------------

// isUniqueViolation reports whether err is a Postgres unique-constraint failure
// on the given index/constraint name.
//
// 23505 is the SQLSTATE code for unique_violation. errors.As is used instead of
// a type assertion because pgx wraps its errors, so the *pgconn.PgError may be
// several layers down.
func isUniqueViolation(err error, constraintName string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23505" && pgErr.ConstraintName == constraintName
}

// isForeignKeyViolation reports whether err is a Postgres foreign-key failure.
// SQLSTATE 23503. For this schema it means "the lead_id you referenced does not
// exist", which the API surfaces as a 404.
func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23503"
}

// isForeignKeyViolationOn is isForeignKeyViolation for one named constraint,
// for tables with more than one foreign key.
func isForeignKeyViolationOn(err error, constraintName string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23503" && pgErr.ConstraintName == constraintName
}

// ---------------------------------------------------------------------------
// Search helpers
// ---------------------------------------------------------------------------

// likeEscaper replaces the characters that are special inside a SQL LIKE/ILIKE
// pattern with escaped versions. Without this, a user searching for "50%" would
// have their "%" treated as a wildcard.
//
// Queries that use the output must declare ESCAPE '\' so Postgres knows what the
// escape character is.
var likeEscaper = strings.NewReplacer(
	`\`, `\\`,
	`%`, `\%`,
	`_`, `\_`,
)

// containsPattern turns a raw search term into a safe "%term%" ILIKE pattern.
func containsPattern(term string) string {
	return "%" + likeEscaper.Replace(term) + "%"
}
