package model

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// This file is why a deal closed. A lost deal must say why, because "why do we
// lose?" is the question the loss report answers; a won deal may say why, but
// marking a win stays one click. The reason lives on the lead while it is
// closed and is cleared when it reopens. The timeline keeps every one.

// CloseReason is one of LostReasons or WonReasons. Codes may repeat across the
// two lists ("price" is a reason to lose and to win); the stage says which.
type CloseReason string

// ReasonOption is a reason with its label, for the dropdowns and reports.
type ReasonOption struct {
	Value CloseReason `json:"value"`
	Label string      `json:"label"`
}

// ReasonOther is the catch-all, which needs a note to mean anything.
const ReasonOther CloseReason = "other"

// LostReasons are why a deal is lost, most common first.
var LostReasons = []ReasonOption{
	{"price", "Price too high"},
	{"competitor", "Went with a competitor"},
	{"no_budget", "No budget"},
	{"timing", "Bad timing"},
	{"no_decision", "No decision / went quiet"},
	{"product_fit", "Not the right fit"},
	{ReasonOther, "Other"},
}

// WonReasons are why a deal is won.
var WonReasons = []ReasonOption{
	{"product_fit", "Best fit for their needs"},
	{"price", "Price / value"},
	{"relationship", "Relationship / trust"},
	{"service", "Fast, responsive service"},
	{"referral", "Referral / reputation"},
	{ReasonOther, "Other"},
}

// MaxCloseNoteLen caps the note on a close reason.
const MaxCloseNoteLen = 500

// ReasonsFor returns the reasons for a closed stage, or nil for an open one.
func ReasonsFor(stage Stage) []ReasonOption {
	switch stage {
	case StageLost:
		return LostReasons
	case StageWon:
		return WonReasons
	default:
		return nil
	}
}

// ReasonLabel is the display text for a reason on a closed stage, or the code
// itself if it is not one of that stage's reasons.
func ReasonLabel(stage Stage, reason CloseReason) string {
	for _, r := range ReasonsFor(stage) {
		if r.Value == reason {
			return r.Label
		}
	}
	return string(reason)
}

// CloseInput is a reason for closing a deal, as sent with a stage change, a
// lead edit or a new lead. Both fields are optional on the wire; Validate
// says when they are needed.
type CloseInput struct {
	Reason *CloseReason `json:"close_reason"`
	Note   string       `json:"close_note"`
}

// Normalize trims the note and treats an empty reason as none.
func (in *CloseInput) Normalize() {
	in.Note = strings.TrimSpace(in.Note)
	if in.Reason != nil && strings.TrimSpace(string(*in.Reason)) == "" {
		in.Reason = nil
	}
}

// Given reports whether a reason or a note was sent at all.
func (in CloseInput) Given() bool { return in.Reason != nil || in.Note != "" }

// Validate checks a reason for moving a lead to stage. entering is whether
// the lead is moving into that stage (rather than already being in it): only
// entering Lost requires a reason, so editing an old lost lead's phone number
// does not demand one.
func (in CloseInput) Validate(stage Stage, entering bool) ValidationErrors {
	errs := ValidationErrors{}

	if !stage.IsClosed() {
		if in.Given() {
			errs["close_reason"] = "can only be given when the stage is Won or Lost"
		}
		return errs
	}

	if in.Reason == nil {
		if stage == StageLost && entering {
			errs["close_reason"] = "is required when a deal is lost: say why, so the loss report can learn from it"
		}
		if in.Note != "" {
			errs["close_reason"] = "is required with a note"
		}
		return errs
	}

	if !reasonValid(stage, *in.Reason) {
		errs["close_reason"] = "must be one of: " + joinReasons(ReasonsFor(stage))
	}
	if *in.Reason == ReasonOther && in.Note == "" {
		errs["close_note"] = `is required when the reason is "Other"`
	}
	if utf8.RuneCountInString(in.Note) > MaxCloseNoteLen {
		errs["close_note"] = fmt.Sprintf("must be %d characters or fewer", MaxCloseNoteLen)
	}
	return errs
}

func reasonValid(stage Stage, reason CloseReason) bool {
	for _, r := range ReasonsFor(stage) {
		if r.Value == reason {
			return true
		}
	}
	return false
}

func joinReasons(reasons []ReasonOption) string {
	out := make([]string, len(reasons))
	for i, r := range reasons {
		out[i] = string(r.Value)
	}
	return strings.Join(out, ", ")
}

// NotePtr is the note for the database: nil when empty.
func (in CloseInput) NotePtr() *string {
	if in.Note == "" {
		return nil
	}
	n := in.Note
	return &n
}

// ReasonText is a stored reason as the timeline writes it: the label, with
// the note in brackets. "" when there is no reason.
func ReasonText(stage Stage, reason *CloseReason, note *string) string {
	if reason == nil {
		return ""
	}
	text := ReasonLabel(stage, *reason)
	if note != nil && *note != "" {
		text += " (" + *note + ")"
	}
	return text
}
