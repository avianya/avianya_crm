// Package api holds the HTTP layer: routing, middleware, request parsing and
// response shaping. It is the only package that imports Gin -- swap the web
// framework and nothing under model or store has to change.
package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"crm-lead-management/backend/internal/model"
	"crm-lead-management/backend/internal/store"
)

// Every response from this API has one of exactly two shapes, so a client can
// write one success path and one error path and be done:
//
//	success  {"data": ...}                        (+ "pagination" on lists)
//	failure  {"error": {"code","message","details"}}
//
// The alternative -- returning a bare object here and a bare array there and a
// plain string on error -- is what makes frontends full of defensive type checks.
type dataResponse struct {
	Data any `json:"data"`
}

type listResponse struct {
	Data       any            `json:"data"`
	Pagination model.PageMeta `json:"pagination"`
}

type apiError struct {
	// Code is a stable, machine-readable identifier. Clients should branch on
	// this, never on the wording of Message.
	Code string `json:"code"`

	// Message is a human-readable summary, safe to show a user.
	Message string `json:"message"`

	// Details carries per-field problems on validation failures. Omitted when
	// empty so simple errors stay small.
	Details map[string]string `json:"details,omitempty"`
}

type errorResponse struct {
	Error apiError `json:"error"`
}

// Error codes returned by this API.
const (
	codeBadRequest      = "bad_request"
	codeValidationError = "validation_error"
	codeNotFound        = "not_found"
	codeConflict        = "conflict"
	codeInternalError   = "internal_error"

	// The admin key is missing or wrong (401/403), or not set up (503).
	codeAdminRequired = "admin_required"

	// Not logged in (401), or logged in without the role for this (403).
	codeUnauthenticated = "unauthenticated"
	codeForbidden       = "forbidden"

	// SECRETS_ENCRYPTION_KEY is not set, so a credential cannot be saved.
	codeEncryptionNotSetUp = "encryption_not_set_up"

	// Meta refused or could not be reached; the message is Meta's own.
	codeMetaError = "meta_error"

	// Too many requests from one caller (429).
	codeRateLimited = "rate_limited"
)

// respondData writes a successful single-object (or single-value) response.
func respondData(c *gin.Context, status int, data any) {
	c.JSON(status, dataResponse{Data: data})
}

// respondList writes a successful list response with its pagination block.
func respondList(c *gin.Context, data any, pagination model.PageMeta) {
	c.JSON(http.StatusOK, listResponse{Data: data, Pagination: pagination})
}

// respondError writes an error response.
//
// c.AbortWithStatusJSON stops any remaining handlers in the chain from running,
// which matters because writing a body does not by itself end the request in Gin.
func respondError(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, errorResponse{Error: apiError{Code: code, Message: message}})
}

// respondValidationError writes a 422 listing every field that failed.
//
// 422 Unprocessable Content rather than 400: the request was syntactically valid
// JSON that the server understood, it just described something we refuse to
// store. Reserving 400 for malformed input lets a client tell "your JSON is
// broken" apart from "your email is missing".
func respondValidationError(c *gin.Context, errs model.ValidationErrors) {
	c.AbortWithStatusJSON(http.StatusUnprocessableEntity, errorResponse{
		Error: apiError{
			Code:    codeValidationError,
			Message: "The submitted data is invalid.",
			Details: errs,
		},
	})
}

// respondStoreError maps an error from the store layer onto the right HTTP
// status.
//
// Centralising this means every handler's error path is one line, and there is a
// single place to look when a status code is wrong. Anything unrecognised is
// treated as a bug in our own code: log the real cause, tell the client only
// that something broke.
func (h *Handler) respondStoreError(c *gin.Context, err error, action string) {
	var fieldErr *store.FieldError
	switch {
	case errors.Is(err, store.ErrNotFound):
		respondError(c, http.StatusNotFound, codeNotFound, "The requested resource was not found.")

	case errors.Is(err, store.ErrDuplicateEmail):
		respondError(c, http.StatusConflict, codeConflict, "A lead with this email address already exists.")

	case errors.Is(err, store.ErrDuplicateMemberEmail):
		respondError(c, http.StatusConflict, codeConflict, "A team member with this email address already exists.")

	case errors.Is(err, store.ErrNotRetryable):
		respondError(c, http.StatusConflict, codeConflict, "This email has already been sent or is queued to send.")

	case errors.Is(err, store.ErrTaskCompleted):
		respondError(c, http.StatusConflict, codeConflict, "This event is already completed. Reopen it before cancelling.")

	case errors.Is(err, store.ErrActivityLocked):
		respondError(c, http.StatusConflict, codeConflict,
			"Only notes, calls, emails and meetings you logged can be edited or deleted. This entry was recorded automatically.")

	case errors.Is(err, store.ErrNoEncryptionKey):
		respondError(c, http.StatusServiceUnavailable, codeEncryptionNotSetUp,
			"Credentials cannot be saved: set SECRETS_ENCRYPTION_KEY on the server and restart it.")

	case errors.Is(err, store.ErrMetaNotTested):
		respondError(c, http.StatusConflict, codeConflict,
			"Run Test Connection successfully before turning test mode off.")

	case errors.Is(err, store.ErrLastAdmin):
		respondError(c, http.StatusConflict, codeConflict,
			"This is the last active admin who can log in. Make someone else an admin first.")

	case errors.Is(err, store.ErrCAPINotRetryable):
		respondError(c, http.StatusConflict, codeConflict, "Only a failed or scheduled event can be retried.")

	case errors.As(err, &fieldErr):
		respondValidationError(c, model.ValidationErrors{fieldErr.Field: fieldErr.Message})

	default:
		// The full error, with its wrapped chain, goes to the server log where
		// developers can see it...
		h.logger.Error("unhandled store error",
			slog.String("action", action),
			slog.String("path", c.Request.URL.Path),
			slog.String("error", err.Error()),
		)

		// ...and never to the client, where it would leak SQL and schema details.
		// Development mode is the one exception, purely to save you a trip to the
		// terminal while building.
		message := "Something went wrong on our side. Please try again."
		if h.cfg.IsDevelopment() {
			message = err.Error()
		}
		respondError(c, http.StatusInternalServerError, codeInternalError, message)
	}
}
