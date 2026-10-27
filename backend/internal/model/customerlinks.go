package model

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// This file is customer links: pages a lead opens without logging in. A
// booking link offers free slots in their salesperson's calendar; a feedback
// link is the thank-you page a won customer gets, with a rating and a way to
// refer a friend. The store makes the links and does what the pages ask.

// The kinds of link. Mirrors the customer_links_kind_valid CHECK.
const (
	LinkBooking  = "booking"
	LinkFeedback = "feedback"
)

// ValidLinkKind reports whether kind is a kind of link.
func ValidLinkKind(kind string) bool { return kind == LinkBooking || kind == LinkFeedback }

// The bookable week: 30-minute slots from 10:00 to 18:00, Monday to Saturday,
// for the next two weeks, starting at least two hours from now. Times are in
// APP_TIMEZONE, like every task.
const (
	BookingSlotMinutes = 30
	BookingDaysAhead   = 14
	BookingFirstSlot   = "10:00"
	BookingLastSlot    = "17:30"
	BookingLeadHours   = 2
)

// CustomerLink is one link, as the lead page shows it. URL is filled in by the
// API, which knows where the app is served.
type CustomerLink struct {
	Kind      string     `json:"kind"`
	Token     string     `json:"-"`
	URL       string     `json:"url"`
	CreatedAt time.Time  `json:"created_at"`
	EmailedAt *time.Time `json:"emailed_at"`
}

// Feedback is what a won customer said.
type Feedback struct {
	Rating         int       `json:"rating"`
	Comment        string    `json:"comment"`
	ReferralLeadID *int64    `json:"referral_lead_id"`
	CreatedAt      time.Time `json:"created_at"`
}

// CustomerLinks is GET /api/leads/:id/links. A nil link was never made.
type CustomerLinks struct {
	Booking  *CustomerLink `json:"booking"`
	Feedback *CustomerLink `json:"feedback"`
	// GivenFeedback is nil until the customer sends it.
	GivenFeedback *Feedback `json:"given_feedback"`
}

// --- the booking page --------------------------------------------------------

// BookingPage is GET /api/public/booking/:token: who the meeting is with, and
// when they are free.
type BookingPage struct {
	LeadName  string `json:"lead_name"`
	Company   string `json:"company"`
	OwnerName string `json:"owner_name"` // "" when nobody is assigned the lead yet
	// Booked is the meeting already booked, while it is still ahead. Then no
	// days are offered.
	Booked      *BookedMeeting `json:"booked"`
	Days        []BookingDay   `json:"days"`
	SlotMinutes int            `json:"slot_minutes"`
}

// BookingDay is one day's free start times, "15:04".
type BookingDay struct {
	Date  string   `json:"date"` // "2006-01-02"
	Times []string `json:"times"`
}

// BookedMeeting is a booked slot.
type BookedMeeting struct {
	Date      string `json:"date"`
	Time      string `json:"time"`
	OwnerName string `json:"owner_name"`
}

// BookingInput is POST /api/public/booking/:token.
type BookingInput struct {
	Date string `json:"date"`
	Time string `json:"time"`
	Note string `json:"note"` // what they would like to talk about; optional
}

const maxBookingNoteLen = 500

func (in *BookingInput) Normalize() {
	in.Date = strings.TrimSpace(in.Date)
	in.Time = strings.TrimSpace(in.Time)
	in.Note = strings.TrimSpace(in.Note)
}

func (in BookingInput) Validate() ValidationErrors {
	errs := ValidationErrors{}
	if _, err := time.Parse(time.DateOnly, in.Date); err != nil {
		errs["date"] = "must be a date like 2026-10-28"
	}
	if _, err := time.Parse(TimeLayout, in.Time); err != nil {
		errs["time"] = "must be a time like 14:30"
	}
	if utf8.RuneCountInString(in.Note) > maxBookingNoteLen {
		errs["note"] = fmt.Sprintf("must be %d characters or fewer", maxBookingNoteLen)
	}
	return errs
}

// --- the feedback page -------------------------------------------------------

// FeedbackPage is GET /api/public/feedback/:token.
type FeedbackPage struct {
	LeadName  string `json:"lead_name"`
	Company   string `json:"company"`
	OwnerName string `json:"owner_name"`
	// Given is true once they have sent feedback; it is taken once.
	Given bool `json:"given"`
}

// FeedbackInput is POST /api/public/feedback/:token.
type FeedbackInput struct {
	Rating  int    `json:"rating"`
	Comment string `json:"comment"`
	// Referral is a friend to pass on; nil (or a blank name and email) for none.
	Referral *ReferralInput `json:"referral"`
}

// ReferralInput is the friend a customer refers. They become a new lead.
type ReferralInput struct {
	Name    string `json:"name"`
	Email   string `json:"email"`
	Phone   string `json:"phone"`
	Company string `json:"company"`
}

const maxFeedbackCommentLen = 2000

// ReferralCompanyUnknown is the company a referral is saved with when the
// customer did not say, since a lead must have one.
const ReferralCompanyUnknown = "Not given"

func (in *FeedbackInput) Normalize() {
	in.Comment = strings.TrimSpace(in.Comment)
	if r := in.Referral; r != nil {
		r.Name = strings.TrimSpace(r.Name)
		r.Email = strings.TrimSpace(r.Email)
		r.Phone = strings.TrimSpace(r.Phone)
		r.Company = strings.TrimSpace(r.Company)
		if r.Name == "" && r.Email == "" && r.Phone == "" && r.Company == "" {
			in.Referral = nil
		}
	}
}

func (in FeedbackInput) Validate() ValidationErrors {
	errs := ValidationErrors{}
	if in.Rating < 1 || in.Rating > 5 {
		errs["rating"] = "choose 1 to 5 stars"
	}
	if utf8.RuneCountInString(in.Comment) > maxFeedbackCommentLen {
		errs["comment"] = fmt.Sprintf("must be %d characters or fewer", maxFeedbackCommentLen)
	}
	if in.Referral != nil {
		lead := in.Referral.LeadInput("", nil)
		lead.Normalize()
		for field, msg := range lead.Validate() {
			errs["referral."+field] = msg
		}
	}
	return errs
}

// LeadInput is the referral as a new lead, given to ownerID (the referrer's
// salesperson) with a note saying who referred them.
func (r ReferralInput) LeadInput(referredBy string, ownerID *int64) LeadInput {
	company := r.Company
	if company == "" {
		company = ReferralCompanyUnknown
	}
	notes := ""
	if referredBy != "" {
		notes = "Referred by " + referredBy + " on their feedback page."
	}
	return LeadInput{
		Name:    r.Name,
		Company: company,
		Email:   r.Email,
		Phone:   r.Phone,
		Source:  SourceReferral,
		Stage:   StageNew,
		Notes:   notes,
		OwnerID: ownerID,
	}
}
