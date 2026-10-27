package api

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"

	"crm-lead-management/backend/internal/auth"
	"crm-lead-management/backend/internal/model"
	"crm-lead-management/backend/internal/store"
)

// Login. Every /api route needs a logged-in member except the health check,
// the platform webhooks (which prove themselves with signatures) and login
// itself. Who is asking always comes from the session cookie -- never from an
// id in the request -- and is put on the request's context, where the store
// reads it to scope leads (store/scope.go).

const (
	sessionCookie = "crm_session"
	viewerKeyGin  = "viewer"
	// csrfHeader must be on every request that changes something. A form on
	// another site cannot set a custom header, and a script on another site
	// cannot send one without a CORS preflight, which this API refuses for
	// origins it does not know. Together with SameSite=Lax that stops
	// cross-site request forgery.
	csrfHeader = "X-Requested-With"
)

// publicPaths need no login.
var publicPaths = []string{
	"/api/health", "/api/auth/login", "/api/auth/logout",
	"/api/auth/signup", "/api/auth/forgot", "/api/auth/reset",
}

func isPublic(path string) bool {
	return slices.Contains(publicPaths, path) || strings.HasPrefix(path, "/api/webhooks/") ||
		strings.HasPrefix(path, "/api/public/") || // customer links: the token is the credential
		path == sheetsCallbackPath // answers with a redirect; checks the session itself
}

// authenticate finds who is asking: a session cookie, or the admin API key
// (for scripts; it acts as an admin). No one, and the request is refused.
func (h *Handler) authenticate(c *gin.Context) {
	if isPublic(c.Request.URL.Path) || c.Request.Method == http.MethodOptions {
		c.Next()
		return
	}

	var viewer model.Viewer
	if key := c.GetHeader(adminKeyHeader); key != "" {
		if h.cfg.AdminAPIKey == "" || !equalSecret(key, h.cfg.AdminAPIKey) {
			h.logger.Warn("wrong admin key", slog.String("path", c.Request.URL.Path), slog.String("ip", c.ClientIP()))
			respondError(c, http.StatusForbidden, codeAdminRequired, "The admin key is not right.")
			return
		}
		// A header cannot be sent by another site, so no CSRF check is needed.
		viewer = model.Viewer{Name: "Admin API key", Role: model.RoleAdmin}
	} else {
		token, err := c.Cookie(sessionCookie)
		if err != nil || token == "" {
			respondError(c, http.StatusUnauthorized, codeUnauthenticated, "Please log in.")
			return
		}
		viewer, err = h.store.SessionViewer(c.Request.Context(), auth.HashToken(token))
		if errors.Is(err, store.ErrNotFound) {
			h.clearSessionCookie(c)
			respondError(c, http.StatusUnauthorized, codeUnauthenticated, "Your session has ended. Please log in again.")
			return
		}
		if err != nil {
			h.respondStoreError(c, err, "read session")
			return
		}
		if !safeMethod(c.Request.Method) && c.GetHeader(csrfHeader) == "" {
			respondError(c, http.StatusForbidden, codeForbidden, "The request is missing the "+csrfHeader+" header.")
			return
		}
	}

	c.Set(viewerKeyGin, viewer)
	c.Request = c.Request.WithContext(store.WithViewer(c.Request.Context(), viewer))
	c.Next()
}

func safeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

// viewerOf returns who is asking; authenticate has always set it by the time a
// handler behind it runs.
func viewerOf(c *gin.Context) model.Viewer {
	v, _ := c.Get(viewerKeyGin)
	viewer, _ := v.(model.Viewer)
	return viewer
}

// requireRole lets a request through only for these roles.
func requireRole(roles ...model.Role) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !viewerOf(c).Has(roles...) {
			respondError(c, http.StatusForbidden, codeForbidden, "Your role does not allow this.")
			return
		}
		c.Next()
	}
}

// --- lead access ------------------------------------------------------------

// leadAccess guards routes under /leads/:id: the lead must be one the viewer
// may see. A lead they may not see answers 404, exactly as one that does not
// exist, so ids cannot be probed.
func (h *Handler) leadAccess(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}
	h.requireVisible(c, id)
}

func (h *Handler) requireVisible(c *gin.Context, leadID int64) {
	visible, err := h.store.LeadVisible(c.Request.Context(), leadID)
	if err != nil {
		h.respondStoreError(c, err, "lead access")
		return
	}
	if !visible {
		respondError(c, http.StatusNotFound, codeNotFound, "The requested resource was not found.")
		return
	}
	c.Next()
}

// accessVia guards routes addressed by another id (a task, a timeline entry,
// a reminder, an alert): the lead it belongs to must be visible.
func (h *Handler) accessVia(leadOf func(*gin.Context, int64) (int64, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseIDParam(c)
		if !ok {
			return
		}
		leadID, err := leadOf(c, id)
		if err != nil {
			h.respondStoreError(c, err, "access")
			return
		}
		h.requireVisible(c, leadID)
	}
}

func (h *Handler) taskLead(c *gin.Context, id int64) (int64, error) {
	return h.store.LeadOfTask(c.Request.Context(), id)
}
func (h *Handler) activityLead(c *gin.Context, id int64) (int64, error) {
	return h.store.LeadOfActivity(c.Request.Context(), id)
}
func (h *Handler) notificationLead(c *gin.Context, id int64) (int64, error) {
	return h.store.LeadOfNotification(c.Request.Context(), id)
}
func (h *Handler) dealAlertLead(c *gin.Context, id int64) (int64, error) {
	return h.store.LeadOfDealAlert(c.Request.Context(), id)
}

// --- login and logout -------------------------------------------------------

// POST /api/auth/login {"email", "password"}
//
// The same answer, after the same wait, for an unknown email, a wrong password,
// a member with no password and a switched-off member: nothing tells which
// emails have accounts.
func (h *Handler) login(c *gin.Context) {
	var in model.LoginInput
	if !bindJSON(c, &in) {
		return
	}
	ctx := c.Request.Context()
	rec, err := h.store.LoginRecordByEmail(ctx, strings.TrimSpace(in.Email))
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		h.respondStoreError(c, err, "login")
		return
	}
	ok := auth.CheckPassword(rec.PasswordHash, in.Password) // dummy check when not found
	if ok && rec.SignupPending {
		// Only said to someone who knows the password, so it gives nothing away.
		respondError(c, http.StatusForbidden, codeForbidden,
			"Your account is waiting for an admin to approve it. Ask your admin to switch it on.")
		return
	}
	if !ok || !rec.Active {
		h.logger.Info("failed login", slog.String("ip", c.ClientIP()))
		respondError(c, http.StatusUnauthorized, codeUnauthenticated, "The email or password is not right.")
		return
	}

	token, hash, err := auth.NewToken()
	if err != nil {
		h.respondStoreError(c, err, "new session")
		return
	}
	if err := h.store.CreateSession(ctx, rec.Viewer.MemberID, hash, c.ClientIP(), c.Request.UserAgent()); err != nil {
		h.respondStoreError(c, err, "new session")
		return
	}
	h.setSessionCookie(c, token, int(model.SessionMaxAge.Seconds()))
	h.logger.Info("login", slog.Int64("member_id", rec.Viewer.MemberID))
	respondData(c, http.StatusOK, rec.Viewer)
}

// POST /api/auth/logout: ends this browser's session.
func (h *Handler) logout(c *gin.Context) {
	if token, err := c.Cookie(sessionCookie); err == nil && token != "" {
		if err := h.store.DeleteSession(c.Request.Context(), auth.HashToken(token)); err != nil {
			h.respondStoreError(c, err, "logout")
			return
		}
	}
	h.clearSessionCookie(c)
	c.Status(http.StatusNoContent)
}

// GET /api/auth/me: who is logged in.
func (h *Handler) me(c *gin.Context) { respondData(c, http.StatusOK, viewerOf(c)) }

// PUT /api/auth/password: change your own password. Needs the current one;
// every other session of yours ends.
func (h *Handler) changeOwnPassword(c *gin.Context) {
	viewer := viewerOf(c)
	if viewer.MemberID == 0 {
		respondError(c, http.StatusConflict, codeConflict, "The admin API key has no password to change.")
		return
	}
	var in model.PasswordChangeInput
	if !bindJSON(c, &in) {
		return
	}
	if msg := model.ValidatePassword(in.NewPassword); msg != "" {
		respondValidationError(c, model.ValidationErrors{"new_password": msg})
		return
	}
	ctx := c.Request.Context()
	current, err := h.store.PasswordHash(ctx, viewer.MemberID)
	if err != nil {
		h.respondStoreError(c, err, "change password")
		return
	}
	if !auth.CheckPassword(current, in.CurrentPassword) {
		respondValidationError(c, model.ValidationErrors{"current_password": "is not right"})
		return
	}
	hash, err := auth.HashPassword(in.NewPassword)
	if err != nil {
		respondValidationError(c, model.ValidationErrors{"new_password": "is too long"})
		return
	}
	token, _ := c.Cookie(sessionCookie)
	if err := h.store.SetPasswordHash(ctx, viewer.MemberID, hash, auth.HashToken(token)); err != nil {
		h.respondStoreError(c, err, "change password")
		return
	}
	h.audit(c, "account.password_changed", map[string]any{"member_id": viewer.MemberID})
	c.Status(http.StatusNoContent)
}

// --- admin: accounts ---------------------------------------------------------

// PUT /api/team/:id/password {"password"} (admin): set someone's password, to
// give them a login or after they forgot it. Their sessions end.
func (h *Handler) setMemberPassword(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}
	var in model.SetPasswordInput
	if !bindJSON(c, &in) {
		return
	}
	if msg := model.ValidatePassword(in.Password); msg != "" {
		respondValidationError(c, model.ValidationErrors{"password": msg})
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		respondValidationError(c, model.ValidationErrors{"password": "is too long"})
		return
	}
	if err := h.store.SetPasswordHash(c.Request.Context(), id, hash, nil); err != nil {
		h.respondStoreError(c, err, "set password")
		return
	}
	h.audit(c, "account.password_set", map[string]any{"member_id": id})
	member, err := h.store.GetTeamMember(c.Request.Context(), id)
	if err != nil {
		h.respondStoreError(c, err, "set password")
		return
	}
	respondData(c, http.StatusOK, member)
}

// PUT /api/team/:id/account {"role", "active"} (admin)
func (h *Handler) updateMemberAccount(c *gin.Context) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}
	var in model.AccountInput
	if !bindJSON(c, &in) {
		return
	}
	if errs := in.Validate(); errs.Any() {
		respondValidationError(c, errs)
		return
	}
	member, err := h.store.UpdateAccount(c.Request.Context(), id, in)
	if err != nil {
		h.respondStoreError(c, err, "update account")
		return
	}
	h.audit(c, "account.updated", map[string]any{"member_id": id, "role": member.Role, "active": member.Active})
	respondData(c, http.StatusOK, member)
}

// --- cookies -------------------------------------------------------------------

// The cookie is HttpOnly (scripts cannot read it), SameSite=Lax (not sent on
// other sites' requests), scoped to /api, and Secure outside development so
// it only travels over HTTPS (browsers treat http://localhost as secure).
func (h *Handler) setSessionCookie(c *gin.Context, token string, maxAge int) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/api",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   !h.cfg.IsDevelopment(),
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *Handler) clearSessionCookie(c *gin.Context) { h.setSessionCookie(c, "", -1) }

// actorOf names the viewer for the audit log.
func actorOf(c *gin.Context) string {
	v := viewerOf(c)
	if v.MemberID == 0 {
		return fmt.Sprintf("admin API key from %s", c.ClientIP())
	}
	return fmt.Sprintf("%s <%s> (member %d) from %s", v.Name, v.Email, v.MemberID, c.ClientIP())
}

// --- sign up and forgotten passwords ---------------------------------------------

// POST /api/auth/signup {"name", "email", "password"}
//
// Creates an account and logs it in at once. It is a Sales account, so it sees
// only leads assigned to it -- none, until an admin or manager assigns some.
// An email already in the CRM is refused and that member never changed.
func (h *Handler) signup(c *gin.Context) {
	var in model.SignupInput
	if !bindJSON(c, &in) {
		return
	}
	in.Normalize()
	if errs := in.Validate(); errs.Any() {
		respondValidationError(c, errs)
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		respondValidationError(c, model.ValidationErrors{"password": "is too long"})
		return
	}
	ctx := c.Request.Context()
	viewer, err := h.store.CreateSignup(ctx, in.Name, in.Email, hash)
	if errors.Is(err, store.ErrDuplicateMemberEmail) {
		respondError(c, http.StatusConflict, codeConflict, "This email already has an account. Log in, or use Forgot password.")
		return
	}
	if err != nil {
		h.respondStoreError(c, err, "sign up")
		return
	}
	token, tokenHash, err := auth.NewToken()
	if err != nil {
		h.respondStoreError(c, err, "sign up")
		return
	}
	if err := h.store.CreateSession(ctx, viewer.MemberID, tokenHash, c.ClientIP(), c.Request.UserAgent()); err != nil {
		h.respondStoreError(c, err, "sign up")
		return
	}
	h.setSessionCookie(c, token, int(model.SessionMaxAge.Seconds()))
	h.logger.Info("sign-up", slog.Int64("member_id", viewer.MemberID))
	respondData(c, http.StatusCreated, viewer)
}

// POST /api/auth/forgot {"email"}
//
// Emails a one-time reset link, good for an hour, to a member who can log in.
// The answer is the same for any email, so it cannot be used to find accounts.
func (h *Handler) forgotPassword(c *gin.Context) {
	var in model.ForgotPasswordInput
	if !bindJSON(c, &in) {
		return
	}
	answer := gin.H{"message": "If that email has an account, a link to set a new password is on its way. It works for one hour."}
	ctx := c.Request.Context()
	rec, err := h.store.LoginRecordByEmail(ctx, strings.TrimSpace(in.Email))
	if errors.Is(err, store.ErrNotFound) || (err == nil && (!rec.Active || rec.PasswordHash == "")) {
		respondData(c, http.StatusAccepted, answer)
		return
	}
	if err != nil {
		h.respondStoreError(c, err, "forgot password")
		return
	}
	if !h.mailer.Configured() {
		h.logger.Warn("password reset asked for, but email is not set up (SMTP_HOST); an admin must reset it")
		respondData(c, http.StatusAccepted, answer)
		return
	}
	token, hash, err := auth.NewToken()
	if err != nil {
		h.respondStoreError(c, err, "forgot password")
		return
	}
	if err := h.store.CreatePasswordReset(ctx, rec.Viewer.MemberID, hash); err != nil {
		h.respondStoreError(c, err, "forgot password")
		return
	}
	link := h.cfg.BaseURL + "/reset-password?token=" + token
	body := fmt.Sprintf("Hi %s,\n\nSomeone asked to set a new password for your Pipeline CRM login.\n"+
		"If that was you, open this link within an hour:\n\n%s\n\n"+
		"If it was not you, ignore this email; your password stays as it is.\n", rec.Viewer.Name, link)
	if err := h.mailer.Send(ctx, rec.Viewer.Email, "Set a new Pipeline CRM password", body); err != nil {
		// Logged without the link: it is as good as a password.
		h.logger.Error("send password reset email", slog.Int64("member_id", rec.Viewer.MemberID), slog.String("error", err.Error()))
	}
	respondData(c, http.StatusAccepted, answer)
}

// POST /api/auth/reset {"token", "password"}: set a new password with the
// emailed link. Every session of theirs ends; they log in with the new one.
func (h *Handler) resetPassword(c *gin.Context) {
	var in model.ResetPasswordInput
	if !bindJSON(c, &in) {
		return
	}
	if msg := model.ValidatePassword(in.Password); msg != "" {
		respondValidationError(c, model.ValidationErrors{"password": msg})
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		respondValidationError(c, model.ValidationErrors{"password": "is too long"})
		return
	}
	memberID, err := h.store.UsePasswordReset(c.Request.Context(), auth.HashToken(strings.TrimSpace(in.Token)), hash)
	if errors.Is(err, store.ErrNotFound) {
		respondError(c, http.StatusBadRequest, codeBadRequest,
			"This link is not valid any more: it was used, or more than an hour has passed. Ask for a new one.")
		return
	}
	if err != nil {
		h.respondStoreError(c, err, "reset password")
		return
	}
	h.logger.Info("password reset with emailed link", slog.Int64("member_id", memberID))
	c.Status(http.StatusNoContent)
}
