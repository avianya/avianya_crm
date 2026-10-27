package api

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"crm-lead-management/backend/internal/config"
	"crm-lead-management/backend/internal/gsheets"
	"crm-lead-management/backend/internal/mail"
	"crm-lead-management/backend/internal/metaads"
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

	// mailer sends test emails; location is the time zone the database runs
	// in, for anything worked out against the clock here (next steps).
	mailer   *mail.Sender
	location *time.Location

	// meta is the Meta Ads integration (settings, leads, conversion events).
	metaAds *metaads.Service

	// sheets is the Google Sheets export.
	sheets *gsheets.Service
}

// NewRouter builds the fully wired HTTP router.
//
// Reading it top to bottom gives you the whole surface area of the API, which is
// exactly why routing lives in one file instead of being registered from a dozen
// scattered init functions.
func NewRouter(cfg config.Config, st *store.Store, logger *slog.Logger, mailer *mail.Sender, location *time.Location) *gin.Engine {
	// Release mode silences Gin's startup banner and debug warnings.
	if !cfg.IsDevelopment() {
		gin.SetMode(gin.ReleaseMode)
	}

	h := &Handler{store: st, logger: logger, cfg: cfg, mailer: mailer, location: location,
		metaAds: metaads.New(st, cfg, logger),
		sheets:  gsheets.New(st, cfg.GoogleSheets, logger, location)}

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

	// Every /api route needs a logged-in member (see auth.go), except the
	// health check, the webhooks and login itself. Roles narrow it further:
	// admin for users and integrations, admin or manager for assigning and
	// deleting leads, and every lead route checks the lead is the viewer's
	// to see.
	admin := requireRole(model.RoleAdmin)
	managers := requireRole(model.RoleAdmin, model.RoleManager)
	limited := rateLimit(60)

	api := r.Group("/api", h.authenticate)
	{
		api.GET("/health", h.health)

		authRoutes := api.Group("/auth")
		{
			authRoutes.POST("/login", rateLimit(10), h.login)          // per address: slows password guessing
			authRoutes.POST("/signup", rateLimit(5), h.signup)         // waits for an admin's approval
			authRoutes.POST("/forgot", rateLimit(5), h.forgotPassword) // emails a one-hour reset link
			authRoutes.POST("/reset", rateLimit(10), h.resetPassword)  // uses that link
			authRoutes.POST("/logout", h.logout)
			authRoutes.GET("/me", h.me)
			authRoutes.PUT("/password", rateLimit(10), h.changeOwnPassword)
		}

		// Pages a lead opens from a link, without logging in.
		public := api.Group("/public")
		{
			public.GET("/booking/:token", limited, h.bookingPage)
			public.POST("/booking/:token", rateLimit(10), h.bookMeeting) // {"date", "time", "note"}
			public.GET("/feedback/:token", limited, h.feedbackPage)
			public.POST("/feedback/:token", rateLimit(10), h.giveFeedback) // {"rating", "comment", "referral"}
		}

		api.GET("/meta", h.meta)
		api.GET("/dashboard", h.dashboard)
		api.GET("/pipeline", h.pipeline)    // the board: one column per stage
		api.GET("/reports", h.reports)      // trends: weekly intake, sources, stage times
		api.GET("/follow-ups", h.followUps) // ?owner=id|none: who to contact, and what is due
		api.GET("/sales-intelligence", h.salesIntelligence)
		api.POST("/sales-intelligence/seed", admin, h.seedIntelligence) // replaces demo data

		// Ad lead forms. The webhooks are called by Meta's and Google's
		// servers and check their own secret; see webhooks.go.
		api.GET("/webhooks/meta", h.verifyMetaWebhook)
		api.POST("/webhooks/meta", h.receiveMetaWebhook)
		api.POST("/webhooks/google", h.receiveGoogleWebhook)
		api.GET("/integrations", admin, h.integrations)         // set-up status + latest ad leads
		api.POST("/integrations/test", admin, h.sendTestAdLead) // {"provider": "meta"|"google"}
		api.POST("/ad-leads/:id/retry", admin, h.retryAdLead)

		// Meta Ads: settings, sync and conversion feedback. Whatever touches
		// credentials or calls Meta for the CRM is for admins; the dashboard
		// is for everyone, scoped to the leads they may see.
		metaAds := api.Group("/meta", limited)
		{
			metaAds.GET("/admin-check", admin, h.adminCheck)
			metaAds.GET("/settings", admin, h.metaSettings)
			metaAds.PUT("/settings", admin, h.saveMetaSettings)
			metaAds.DELETE("/settings/credentials", admin, h.disconnectMeta) // Disconnect
			metaAds.POST("/settings/test", admin, h.testMetaConnection)      // real calls to Meta
			metaAds.POST("/sync", admin, h.syncMetaLeads)                    // read the forms' leads now

			metaAds.GET("/quality-rules", h.qualityRules)
			metaAds.PUT("/quality-rules", admin, h.saveQualityRules) // rescoring every imported lead

			metaAds.GET("/leads", h.metaLeads) // the Meta Leads dashboard

			metaAds.GET("/capi/events", admin, h.capiEvents) // the delivery log: ?status=&lead_id=&from=&to=
			metaAds.POST("/capi/events/retry-failed", admin, h.retryFailedCAPIEvents)
			metaAds.POST("/capi/events/:id/retry", admin, h.retryCAPIEvent)
		}

		// Settings > Marketing Integrations > Meta CAPI: the Conversions API
		// half of the same settings. Admin only throughout, and rate limited;
		// the calls that reach Meta more tightly still.
		// Google Sheets: connect a Google account, pick a sheet, export leads.
		sheets := api.Group("/integrations/google-sheets", limited, admin)
		{
			sheets.GET("", h.sheetsSettings)
			sheets.PUT("", h.sheetsSave)
			sheets.POST("/connect", rateLimit(10), h.sheetsConnect) // {url} of Google's consent page
			sheets.POST("/disconnect", h.sheetsDisconnect)
			sheets.POST("/test", rateLimit(20), h.sheetsTest)
			sheets.GET("/spreadsheets", h.sheetsList)
			sheets.GET("/spreadsheets/:id", h.sheetsGet)
			sheets.POST("/spreadsheets", rateLimit(10), h.sheetsCreate)
			sheets.GET("/picker-token", h.sheetsPickerToken)
			sheets.POST("/sync", rateLimit(10), h.sheetsSync)
			sheets.POST("/syncs/:id/retry", rateLimit(10), h.sheetsRetry)
		}
		// Google sends the admin back here; it checks the session itself.
		api.GET(strings.TrimPrefix(sheetsCallbackPath, "/api"), rateLimit(20), h.sheetsCallback)

		capi := api.Group("/integrations/meta-capi", limited, admin)
		{
			capi.GET("", h.getMetaCAPI)
			capi.PUT("", h.saveMetaCAPI)
			capi.PUT("/token", h.rotateMetaCAPIToken) // Update Access Token
			capi.POST("/disable", h.disableMetaCAPI)
			capi.POST("/test", rateLimit(10), h.testMetaCAPI)                // real call, no event
			capi.POST("/test-event", rateLimit(10), h.sendMetaCAPITestEvent) // one event, test code only
			capi.GET("/events", h.capiEvents)
			capi.POST("/events/retry-failed", h.retryFailedCAPIEvents)
			capi.POST("/events/:id/retry", h.retryCAPIEvent)
			capi.GET("/audit", h.metaCAPIAudit)
		}

		leads := api.Group("/leads")
		{
			// Lists, search and export are scoped in the store: a salesperson
			// only ever gets their own leads back.
			leads.GET("", h.listLeads)                     // ?search=&stage=&source=&status=&owner=&sort_by=&sort_dir=&page=&page_size=
			leads.POST("", h.createLead)                   // 201 + Location; a salesperson's own
			leads.GET("/export", h.exportLeads)            // same filters as the list, as a CSV download
			leads.POST("/import", managers, h.importLeads) // body: the CSV; ?dry_run=true previews, ?run_automations=true
			leads.POST("/bulk", h.bulkUpdateLeads)         // {"ids": [...], "action": "owner"|"stage"|"source"|"delete", ...}
		}

		// Everything about one lead: only for someone who may see it.
		lead := api.Group("/leads/:id", h.leadAccess)
		{
			leads := lead
			leads.GET("", h.getLead)    // lead + full activity timeline
			leads.PUT("", h.updateLead) // full replace, incl. stage
			leads.DELETE("", managers, h.deleteLead)

			// A dedicated stage route so the list view's inline dropdown can
			// move a lead without resending every other field.
			leads.PATCH("/stage", h.updateLeadStage)
			leads.PATCH("/owner", managers, h.updateLeadOwner) // assign: {"owner_id": id | null}, logged on the timeline
			leads.PATCH("/notes", h.updateLeadNotes)           // {"notes": "..."}

			// Links the lead opens without logging in: booking and thank-you.
			leads.GET("/links", h.customerLinks)
			leads.POST("/links/:kind", h.createCustomerLink) // booking | feedback

			// Lead quality: the salesperson's reason, and the admin's override.
			leads.PATCH("/qualification", h.updateQualification) // {"reason": "..."}
			leads.PATCH("/flags", h.updateLeadFlags)             // spam_confirmed, meta_share_opt_out
			leads.PUT("/quality-override", managers, h.overrideQuality)
			leads.DELETE("/quality-override", managers, h.clearQualityOverride)

			leads.GET("/activities", h.listActivities)
			leads.POST("/activities", h.createActivity)

			leads.GET("/tasks", h.listTasks)
			leads.POST("/tasks", h.createTask)

			leads.GET("/next-steps", h.nextSteps) // suggested follow-ups

			leads.POST("/email/preview", h.previewLeadEmail) // fill in {{fields}}; sends nothing
			leads.POST("/emails", h.sendLeadEmail)           // {"send": true} sends; false only logs
		}

		// Reusable emails with {{fields}}, filled in per lead.
		templates := api.Group("/email-templates")
		{
			templates.GET("", h.listEmailTemplates)
			templates.POST("", managers, h.createEmailTemplate)
			templates.PUT("/:id", managers, h.updateEmailTemplate)
			templates.DELETE("/:id", managers, h.deleteEmailTemplate)
		}

		// "When a new lead comes in, plan this task." Run by CreateLead.
		automations := api.Group("/automations", admin)
		{
			automations.GET("", h.listAutomationRules)
			automations.POST("", h.createAutomationRule)
			automations.PUT("/:id", h.updateAutomationRule) // the whole rule, incl. enabled
			automations.DELETE("/:id", h.deleteAutomationRule)
		}

		// "Give new leads to these people, in turns." Run by CreateLead.
		assignment := api.Group("/lead-assignment", admin)
		{
			assignment.GET("", h.leadAssignment)
			assignment.PUT("", h.saveLeadAssignment) // {"enabled", "member_ids"}
		}

		// Timeline entries, by their own id: fix a typo in a note, or remove one
		// logged on the wrong lead. Only entries a user logged can change.
		activities := api.Group("/activities")
		{
			viaLead := h.accessVia(h.activityLead)
			activities.PUT("/:id", viaLead, h.updateActivity)
			activities.DELETE("/:id", viaLead, h.deleteActivity)
		}

		// A task is addressed by its own id once it exists, so ticking one off
		// from the dashboard does not need to know which lead it belongs to.
		tasks := api.Group("/tasks")
		{
			viaLead := h.accessVia(h.taskLead)
			tasks.GET("", h.listAllTasks)                    // ?status=open|completed, or ?from=&to=; scoped
			tasks.PATCH("/:id", viaLead, h.updateTaskStatus) // {"completed": bool} or {"cancelled": bool}
			tasks.PUT("/:id", viaLead, h.updateTask)         // the whole edit form
			tasks.PUT("/:id/schedule", viaLead, h.rescheduleTask)
			tasks.DELETE("/:id", viaLead, h.deleteTask)
		}

		// People leads are assigned to and events assigned to, and their
		// accounts. Everyone may list the team (for names); only admins
		// change it.
		team := api.Group("/team")
		{
			team.GET("", h.listTeam)
			team.POST("", admin, h.createTeamMember)
			team.PUT("/:id", admin, h.updateTeamMember)
			team.DELETE("/:id", admin, h.deleteTeamMember)
			team.PUT("/:id/password", admin, h.setMemberPassword)  // give a login, or reset one
			team.PUT("/:id/account", admin, h.updateMemberAccount) // {"role", "active"}
			team.POST("/:id/test-email", admin, h.sendTestEmail)   // a real email, to check SMTP
			team.POST("/:id/digest", admin, h.sendDigestNow)       // their daily summary, now
		}

		// Reminders that have come due: the bell, and their email delivery.
		notifications := api.Group("/notifications")
		{
			notifications.GET("", h.listNotifications)
			notifications.GET("/settings", h.reminderSettings)
			notifications.POST("/read-all", h.markAllNotificationsRead)
			viaLead := h.accessVia(h.notificationLead)
			notifications.POST("/:id/read", viaLead, h.markNotificationRead)
			notifications.POST("/:id/retry", viaLead, h.retryNotificationEmail)
		}

		// Stuck-deal alerts arrive with the notifications; this marks one read.
		api.POST("/deal-alerts/:id/read", h.accessVia(h.dealAlertLead), h.markDealAlertRead)
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

	// Stages also carry their win probability, so the UI can show how the
	// forecast is worked out rather than presenting it as a black box.
	type stageOption struct {
		option
		Probability int `json:"probability"`
	}

	stages := make([]stageOption, 0, len(model.Stages))
	for _, s := range model.Stages {
		stages = append(stages, stageOption{
			option:      option{Value: string(s), Label: s.Label()},
			Probability: s.WinProbability(),
		})
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
		"stages":          stages,
		"sources":         sources,
		"activity_types":  activityTypes,
		"template_fields": model.TemplateFields,
		"lost_reasons":    model.LostReasons,
		"won_reasons":     model.WonReasons,
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

// pipeline returns the board: every stage as a column with its totals and cards.
func (h *Handler) pipeline(c *gin.Context) {
	columns, err := h.store.Pipeline(c.Request.Context())
	if err != nil {
		h.respondStoreError(c, err, "pipeline")
		return
	}
	respondData(c, http.StatusOK, columns)
}

// reports returns the trends for the Reports screen.
func (h *Handler) reports(c *gin.Context) {
	report, err := h.store.Reports(c.Request.Context())
	if err != nil {
		h.respondStoreError(c, err, "reports")
		return
	}
	respondData(c, http.StatusOK, report)
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
