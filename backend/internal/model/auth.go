package model

import (
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Accounts. A team member (team.go) is the person leads are assigned to; with
// a role and a password they can also log in. There are no separate user
// records: leads.owner_id is the "assigned to" user.

// Role decides what a logged-in member may see and do.
type Role string

const (
	// RoleAdmin can do everything, including managing users and integrations.
	RoleAdmin Role = "admin"
	// RoleManager sees every lead and assigns them, but does not manage
	// users or integration credentials.
	RoleManager Role = "manager"
	// RoleSales sees and works only the leads assigned to them.
	RoleSales Role = "sales"
)

// Roles lists every role, most powerful first.
var Roles = []Role{RoleAdmin, RoleManager, RoleSales}

// Valid reports whether r is a known role.
func (r Role) Valid() bool { return slices.Contains(Roles, r) }

// Viewer is who is making a request, as the session (never the request body)
// says. MemberID is 0 for the admin API key, which acts as an admin with no
// member behind it.
type Viewer struct {
	MemberID int64  `json:"id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Role     Role   `json:"role"`
}

// SeesAllLeads reports whether the viewer sees every lead, rather than only
// those assigned to them.
func (v Viewer) SeesAllLeads() bool { return v.Role == RoleAdmin || v.Role == RoleManager }

// CanAssign reports whether the viewer may assign leads to other people.
func (v Viewer) CanAssign() bool { return v.SeesAllLeads() }

// IsAdmin reports whether the viewer is an admin.
func (v Viewer) IsAdmin() bool { return v.Role == RoleAdmin }

// Has reports whether the viewer has one of the roles.
func (v Viewer) Has(roles ...Role) bool { return slices.Contains(roles, v.Role) }

// Session lengths. A session ends SessionMaxAge after login whatever happens,
// and earlier after SessionIdle without a request.
const (
	SessionMaxAge = 7 * 24 * time.Hour
	SessionIdle   = 12 * time.Hour
)

// MinPasswordLength is the shortest password accepted. Length matters more
// than character rules, so that is the only rule.
const MinPasswordLength = 10

// ValidatePassword checks a new password; "" means it is acceptable.
func ValidatePassword(password string) string {
	n := utf8.RuneCountInString(password)
	switch {
	case n < MinPasswordLength:
		return "must be at least 10 characters"
	case n > 128:
		return "must be at most 128 characters"
	case strings.TrimSpace(password) == "":
		return "cannot be only spaces"
	}
	return ""
}

// SignupInput is POST /api/auth/signup: someone asking for an account. It
// starts switched off, as Sales, until an admin approves it.
type SignupInput struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (in *SignupInput) Normalize() {
	in.Name = strings.TrimSpace(in.Name)
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
}

func (in SignupInput) Validate() ValidationErrors {
	errs := ValidationErrors{}
	if in.Name == "" || utf8.RuneCountInString(in.Name) > 120 {
		errs["name"] = "is required, at most 120 characters"
	}
	if !emailPattern.MatchString(in.Email) || len(in.Email) > 254 {
		errs["email"] = "must be a valid email address"
	}
	if msg := ValidatePassword(in.Password); msg != "" {
		errs["password"] = msg
	}
	return errs
}

// ForgotPasswordInput is POST /api/auth/forgot.
type ForgotPasswordInput struct {
	Email string `json:"email"`
}

// ResetPasswordInput is POST /api/auth/reset: the token from the emailed
// link, and the new password.
type ResetPasswordInput struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

// PasswordResetTTL is how long an emailed reset link works.
const PasswordResetTTL = time.Hour

// LoginInput is POST /api/auth/login.
type LoginInput struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// PasswordChangeInput is PUT /api/auth/password: a member changing their own.
type PasswordChangeInput struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// SetPasswordInput is PUT /api/team/:id/password: an admin setting a
// member's password (for a new account, or one that was forgotten).
type SetPasswordInput struct {
	Password string `json:"password"`
}

// AccountInput is PUT /api/team/:id/account: an admin changing a member's
// role or switching their login off. A field left out is left as it is.
type AccountInput struct {
	Role   *Role `json:"role"`
	Active *bool `json:"active"`
}

func (in AccountInput) Validate() ValidationErrors {
	errs := ValidationErrors{}
	if in.Role != nil && !in.Role.Valid() {
		errs["role"] = "must be admin, manager or sales"
	}
	if in.Role == nil && in.Active == nil {
		errs["role"] = "send role or active"
	}
	return errs
}
