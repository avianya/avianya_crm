package api

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"crm-lead-management/backend/internal/config"
	"crm-lead-management/backend/internal/model"
	"crm-lead-management/backend/internal/store"
)

// Handler carries the dependencies every route needs.
//
// Passing them in explicitly -- rather than reaching for package-level globals --
// is what makes the API testable: a test can construct a Handler pointing at a
// throwaway database and a discard logger.
type Handler struct {
	store  *store.Store
	logger *slog.Logger
	cfg    config.Config
}

// NewRouter builds the fully wired HTTP router.
//
// Reading it top to bottom gives you the whole surface area of the API, which is
// exactly why routing lives in one file instead of being registered from a dozen
// scattered init functions.
func NewRouter(cfg config.Config, st *store.Store, logger *slog.Logger) *gin.Engine {
	// Release mode silences Gin's startup banner and debug warnings.
	if !cfg.IsDevelopment() {
		gin.SetMode(gin.ReleaseMode)
	}

	h := &Handler{store: st, logger: logger, cfg: cfg}

	// gin.New (not gin.Default) so the middleware stack is ours: Default would
	// install its own logger and recovery, both of which write plain text
	// instead of the structured JSON logs configured in main.
	r := gin.New()

	// Order matters. Recovery is outermost so it can catch a panic from
	// anything below it, including the logger.
	r.Use(
		recoveryMiddleware(h.logger),
		requestLogger(h.logger),
		corsMiddleware(cfg.AllowedOrigins),
	)

	// Off by default: without it, a POST to a GET-only route returns 404 rather
	// than the more accurate 405 Method Not Allowed.
	r.HandleMethodNotAllowed = true

	// Unknown paths and wrong methods should return the same JSON error shape as
	// everything else, not Gin's default plain-text 404.
	r.NoRoute(func(c *gin.Context) {
		respondError(c, http.StatusNotFound, codeNotFound, "No such endpoint.")
	})
	r.NoMethod(func(c *gin.Context) {
		respondError(c, http.StatusMethodNotAllowed, codeBadRequest,
			"That HTTP method is not supported on this endpoint.")
	})

	api := r.Group("/api")
	{
		api.GET("/health", h.health)
		api.GET("/meta", h.meta)
		api.GET("/dashboard", h.dashboard)

		leads := api.Group("/leads")
		{
			leads.GET("", h.listLeads)      // ?search=&stage=&source=&sort_by=&sort_dir=&page=&page_size=
			leads.POST("", h.createLead)    // 201 + Location
			leads.GET("/:id", h.getLead)    // lead + full activity timeline
			leads.PUT("/:id", h.updateLead) // full replace, incl. stage
			leads.DELETE("/:id", h.deleteLead)

			// A dedicated stage route so the list view's inline dropdown can
			// move a lead without resending every other field.
			leads.PATCH("/:id/stage", h.updateLeadStage)

			leads.GET("/:id/activities", h.listActivities)
			leads.POST("/:id/activities", h.createActivity)
		}
	}

	return r
}

// health reports whether the service and its database are up.
//
// It deliberately pings Postgres rather than just returning 200: a server that
// cannot reach its database is not healthy, however happily it answers HTTP.
func (h *Handler) health(c *gin.Context) {
	if err := h.store.Ping(c.Request.Context()); err != nil {
		h.logger.Error("health check failed", slog.String("error", err.Error()))
		respondError(c, http.StatusServiceUnavailable, codeInternalError, "Database is unreachable.")
		return
	}

	respondData(c, http.StatusOK, map[string]string{
		"status":   "ok",
		"database": "ok",
	})
}

// meta returns the enum vocabularies -- stages, sources, activity types -- with
// their display labels.
//
// The frontend fetches this once and builds its dropdowns, filter chips and step
// indicator from the result, so the list of stages is defined in exactly one
// place (model.Stages) instead of being duplicated in the React code and drifting
// from the backend.
func (h *Handler) meta(c *gin.Context) {
	type option struct {
		Value string `json:"value"`
		Label string `json:"label"`
	}

	stages := make([]option, 0, len(model.Stages))
	for _, s := range model.Stages {
		stages = append(stages, option{Value: string(s), Label: s.Label()})
	}

	sources := make([]option, 0, len(model.Sources))
	for _, s := range model.Sources {
		sources = append(sources, option{Value: string(s), Label: s.Label()})
	}

	activityTypes := make([]option, 0, len(model.UserActivityTypes))
	for _, t := range model.UserActivityTypes {
		activityTypes = append(activityTypes, option{Value: string(t), Label: activityTypeLabel(t)})
	}

	respondData(c, http.StatusOK, map[string]any{
		"stages":         stages,
		"sources":        sources,
		"activity_types": activityTypes,
	})
}

// activityTypeLabel is the display text for a user-selectable activity type.
func activityTypeLabel(t model.ActivityType) string {
	switch t {
	case model.ActivityNote:
		return "Note"
	case model.ActivityCall:
		return "Call"
	case model.ActivityEmail:
		return "Email"
	case model.ActivityMeeting:
		return "Meeting"
	default:
		return string(t)
	}
}

// dashboard returns the aggregate summary for the dashboard screen.
func (h *Handler) dashboard(c *gin.Context) {
	summary, err := h.store.DashboardSummary(c.Request.Context())
	if err != nil {
		h.respondStoreError(c, err, "dashboard summary")
		return
	}
	respondData(c, http.StatusOK, summary)
}
