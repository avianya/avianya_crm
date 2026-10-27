package api

import (
	"log/slog"
	"net/http"
	"net/url"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// corsMiddleware allows the React dev server, which runs on a different port, to
// call this API from the browser.
//
// Written by hand rather than pulled in as a dependency: the whole policy is
// twenty lines and it is worth being able to see exactly what is allowed.
//
// Origins are matched against an explicit list instead of reflecting whatever
// the browser sent, and "*" is avoided, so this stays safe to deploy as-is.
func corsMiddleware(allowedOrigins []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")

		if origin != "" && slices.Contains(allowedOrigins, origin) {
			h := c.Writer.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Content-Type, Accept, X-Admin-Key, X-Requested-With")
			// The session cookie travels with requests from the listed
			// origins only; "*" is never combined with credentials.
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Set("Access-Control-Max-Age", "300") // cache the preflight for 5 min

			// Responses vary by Origin, so any cache in between must key on it.
			h.Add("Vary", "Origin")
		}

		// Browsers send an OPTIONS "preflight" before any non-simple request.
		// It needs the headers above and an empty success response -- it must
		// not fall through to the route handlers.
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		// Hand control to the next middleware / the route handler.
		c.Next()
	}
}

// requestLogger logs one structured line per request, after it completes.
//
// Logging afterwards is what allows the status code and duration to be included:
// neither is known when the request arrives.
func requestLogger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		c.Next() // everything downstream runs here

		attrs := []any{
			slog.String("method", c.Request.Method),
			slog.String("path", c.Request.URL.Path),
			slog.Int("status", c.Writer.Status()),
			slog.Duration("duration", time.Since(start).Round(time.Microsecond)),
		}
		if raw := c.Request.URL.RawQuery; raw != "" {
			attrs = append(attrs, slog.String("query", redactQuery(raw)))
		}

		// Server faults are the ones worth waking someone up for; client
		// mistakes are routine. Splitting the level makes 5xx greppable.
		if c.Writer.Status() >= http.StatusInternalServerError {
			logger.Error("request failed", attrs...)
		} else {
			logger.Info("request", attrs...)
		}
	}
}

// recoveryMiddleware turns a panic into a logged 500 instead of a dead process.
//
// Go's default behaviour on an unhandled panic is to crash the whole program. In
// a server that means one bad request -- a nil map write, an out-of-range index
// -- takes down every other user's connection too. recover() inside a deferred
// function stops the unwinding at the request boundary.
//
// This is a safety net for bugs, not an error-handling strategy: expected
// failures are returned as errors, never panicked.
func recoveryMiddleware(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.Error("panic recovered",
					slog.String("path", c.Request.URL.Path),
					slog.Any("panic", recovered),
					slog.String("stack", string(debug.Stack())),
				)

				// Only write a response if nothing has been written yet;
				// appending JSON to a half-sent body would corrupt it.
				if !c.Writer.Written() {
					respondError(c, http.StatusInternalServerError, codeInternalError,
						"Something went wrong on our side. Please try again.")
					return
				}
				c.Abort()
			}
		}()

		c.Next()
	}
}

// sensitiveParams are query parameters whose values are secrets: Meta's webhook
// check sends the verify token in the URL, and Graph-style callers may send an
// access token. Their values never reach the log.
var sensitiveParams = []string{"hub.verify_token", "access_token", "appsecret_proof", "token", "key"}

// redactQuery is the query string with every sensitive value replaced, for
// logging. A query that does not parse is dropped whole rather than risk it.
func redactQuery(raw string) string {
	values, err := url.ParseQuery(raw)
	if err != nil {
		return "[unparseable query omitted]"
	}
	changed := false
	for name := range values {
		if slices.Contains(sensitiveParams, strings.ToLower(name)) {
			values[name] = []string{"[redacted]"}
			changed = true
		}
	}
	if !changed {
		return raw
	}
	return values.Encode()
}

// adminKeyHeader carries ADMIN_API_KEY, for scripts: a request with the
// right key acts as an admin (see authenticate).
const adminKeyHeader = "X-Admin-Key"

// rateLimit allows each caller (by address) at most perMinute requests a
// minute through the routes it guards, and answers 429 past that. It slows
// password and key guessing, and keeps Meta from being called in a loop.
// Kept in memory: enough for one server, and it forgets on restart.
func rateLimit(perMinute int) gin.HandlerFunc {
	type window struct {
		start time.Time
		count int
	}
	var (
		mu      sync.Mutex
		callers = map[string]*window{}
	)
	return func(c *gin.Context) {
		now := time.Now()
		key := c.ClientIP()
		mu.Lock()
		w, ok := callers[key]
		if !ok || now.Sub(w.start) >= time.Minute {
			w = &window{start: now}
			callers[key] = w
			// Forget callers whose window ended long ago, so the map stays small.
			if len(callers) > 10_000 {
				for k, old := range callers {
					if now.Sub(old.start) >= time.Minute {
						delete(callers, k)
					}
				}
			}
		}
		w.count++
		over, retry := w.count > perMinute, time.Minute-now.Sub(w.start)
		mu.Unlock()
		if over {
			c.Header("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
			respondError(c, http.StatusTooManyRequests, codeRateLimited, "Too many requests; please wait a minute and try again.")
			return
		}
		c.Next()
	}
}
