package model

import (
	"strings"
	"time"
	"unicode/utf8"
)

// TeamMember is one row of team_members: someone leads can be assigned to
// (leads.owner_id) and events assigned to, and who reminders are sent to.
// With a role and a password set, also an account that can log in (auth.go).
type TeamMember struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`

	// EmailReminders says whether this person's reminders are emailed. The
	// in-app reminder appears either way.
	EmailReminders bool `json:"email_reminders"`

	// DefaultReminderMinutes is the reminder new events assigned to this person
	// start with; nil means none.
	DefaultReminderMinutes *int `json:"default_reminder_minutes"`

	// DailyDigest says whether this person gets the morning summary email.
	DailyDigest bool `json:"daily_digest"`

	// The account. The password hash itself is never read into this struct.
	Role     Role `json:"role"`
	Active   bool `json:"active"`
	HasLogin bool `json:"has_login"` // a password is set
	// SignupPending: they signed themselves up and wait for an admin to
	// approve them (switch them on).
	SignupPending bool       `json:"signup_pending"`
	LastLoginAt   *time.Time `json:"last_login_at"`

	CreatedAt time.Time `json:"created_at"`

	// LastDigest is their most recent daily summary, or nil if none was ever
	// attempted. Filled in on the team list only.
	LastDigest *DigestRecord `json:"last_digest,omitempty"`
}

// TeamMemberInput is the body of POST /api/team and PUT /api/team/:id.
type TeamMemberInput struct {
	Name                   string `json:"name"`
	Email                  string `json:"email"`
	EmailReminders         *bool  `json:"email_reminders"` // omitted means true
	DefaultReminderMinutes *int   `json:"default_reminder_minutes"`
	DailyDigest            *bool  `json:"daily_digest"` // omitted means true
}

// Normalize trims the fields, lower-cases the email and turns email reminders
// and the daily summary on when left out.
func (in *TeamMemberInput) Normalize() {
	in.Name = strings.TrimSpace(in.Name)
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	if in.EmailReminders == nil {
		on := true
		in.EmailReminders = &on
	}
	if in.DailyDigest == nil {
		on := true
		in.DailyDigest = &on
	}
}

// Validate mirrors the team_members CHECK constraints.
func (in TeamMemberInput) Validate() ValidationErrors {
	errs := ValidationErrors{}

	switch {
	case in.Name == "":
		errs["name"] = "is required"
	case utf8.RuneCountInString(in.Name) > maxNameLen:
		errs["name"] = "must be 120 characters or fewer"
	}

	switch {
	case in.Email == "":
		errs["email"] = "is required"
	case utf8.RuneCountInString(in.Email) > maxEmailLen:
		errs["email"] = "must be 254 characters or fewer"
	case !emailPattern.MatchString(in.Email):
		errs["email"] = "must be a valid email address"
	}

	if in.DefaultReminderMinutes != nil && !ValidReminder(*in.DefaultReminderMinutes) {
		errs["default_reminder_minutes"] = "must be 5, 15, 30 or 1440 minutes, or null for none"
	}

	return errs
}
