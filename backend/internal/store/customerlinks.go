package store

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"crm-lead-management/backend/internal/model"
)

// Customer links (see model/customerlinks.go): the booking page and the
// thank-you page, both opened by a lead without logging in. A link is found by
// its token alone, and only ever shows or changes its own lead.

// ErrAlreadyGiven is returned when a feedback link is used a second time.
var ErrAlreadyGiven = errors.New("store: feedback was already given")

func newLinkToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("make link token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// ensureCustomerLinkTx makes the lead's link of this kind if it has none, and
// leaves an existing one alone, so a link already sent keeps working.
func ensureCustomerLinkTx(ctx context.Context, q dbtx, leadID int64, kind string) error {
	token, err := newLinkToken()
	if err != nil {
		return err
	}
	if _, err := q.Exec(ctx, `
		INSERT INTO customer_links (lead_id, kind, token) VALUES ($1, $2, $3)
		ON CONFLICT (lead_id, kind) DO NOTHING`, leadID, kind, token); err != nil {
		if isForeignKeyViolation(err) {
			return ErrNotFound
		}
		return fmt.Errorf("make %s link for lead %d: %w", kind, leadID, err)
	}
	return nil
}

// CreateCustomerLink makes the lead's link of this kind, or keeps the one it
// has. ErrNotFound for an unknown lead.
func (s *Store) CreateCustomerLink(ctx context.Context, leadID int64, kind string) error {
	return ensureCustomerLinkTx(ctx, s.pool, leadID, kind)
}

// CustomerLinks returns the lead's links and any feedback given, without URLs.
func (s *Store) CustomerLinks(ctx context.Context, leadID int64) (model.CustomerLinks, error) {
	var out model.CustomerLinks
	rows, err := s.query(ctx, `
		SELECT kind, token, created_at, emailed_at FROM customer_links WHERE lead_id = $1`, leadID)
	if err != nil {
		return out, fmt.Errorf("customer links of lead %d: %w", leadID, err)
	}
	defer rows.Close()
	for rows.Next() {
		var link model.CustomerLink
		if err := rows.Scan(&link.Kind, &link.Token, &link.CreatedAt, &link.EmailedAt); err != nil {
			return out, fmt.Errorf("scan customer link: %w", err)
		}
		switch link.Kind {
		case model.LinkBooking:
			out.Booking = &link
		case model.LinkFeedback:
			out.Feedback = &link
		}
	}
	if err := rows.Err(); err != nil {
		return out, fmt.Errorf("iterate customer links: %w", err)
	}

	var fb model.Feedback
	err = s.queryRow(ctx, `
		SELECT rating, comment, referral_lead_id, created_at FROM lead_feedback WHERE lead_id = $1`, leadID,
	).Scan(&fb.Rating, &fb.Comment, &fb.ReferralLeadID, &fb.CreatedAt)
	switch {
	case err == nil:
		out.GivenFeedback = &fb
	case !errors.Is(err, pgx.ErrNoRows):
		return out, fmt.Errorf("feedback of lead %d: %w", leadID, err)
	}
	return out, nil
}

// ThankYou is what the thank-you email needs.
type ThankYou struct {
	Token, LeadName, Email, OwnerName string
}

// ClaimThankYou returns the thank-you email to send for a Won lead whose
// feedback link was never emailed, and marks it emailed in the same statement,
// so two stage changes at once send it once. ok is false when there is none.
// If sending then fails, ReleaseThankYou lets a later win try again.
func (s *Store) ClaimThankYou(ctx context.Context, leadID int64) (t ThankYou, ok bool, err error) {
	err = s.queryRow(ctx, `
		UPDATE customer_links k SET emailed_at = now()
		FROM leads l LEFT JOIN team_members m ON m.id = l.owner_id
		WHERE l.id = k.lead_id AND k.lead_id = $1 AND k.kind = 'feedback'
		  AND k.emailed_at IS NULL AND l.stage = 'won'
		RETURNING k.token, l.name, l.email, COALESCE(m.name, '')`, leadID,
	).Scan(&t.Token, &t.LeadName, &t.Email, &t.OwnerName)
	if errors.Is(err, pgx.ErrNoRows) {
		return t, false, nil
	}
	if err != nil {
		return t, false, fmt.Errorf("claim thank-you for lead %d: %w", leadID, err)
	}
	return t, true, nil
}

// ReleaseThankYou undoes ClaimThankYou after the email could not be sent.
func (s *Store) ReleaseThankYou(ctx context.Context, leadID int64) error {
	if _, err := s.pool.Exec(ctx, `
		UPDATE customer_links SET emailed_at = NULL WHERE lead_id = $1 AND kind = 'feedback'`, leadID); err != nil {
		return fmt.Errorf("release thank-you for lead %d: %w", leadID, err)
	}
	return nil
}

// linkedLead is the lead a token opens, with its salesperson.
type linkedLead struct {
	linkID       int64
	leadID       int64
	name         string
	company      string
	ownerID      *int64
	ownerName    string
	bookedTaskID *int64
}

// leadByToken finds the lead behind a link of this kind; ErrNotFound for a
// token that is not one. lock holds the link row until the transaction ends,
// so one link is used by one request at a time.
func leadByToken(ctx context.Context, q dbtx, token, kind string, lock bool) (linkedLead, error) {
	query := `
		SELECT k.id, l.id, l.name, l.company, l.owner_id, COALESCE(m.name, ''), k.booked_task_id
		FROM customer_links k
		JOIN leads l ON l.id = k.lead_id
		LEFT JOIN team_members m ON m.id = l.owner_id AND m.active
		WHERE k.token = $1 AND k.kind = $2`
	if lock {
		query += ` FOR UPDATE OF k`
	}
	var ll linkedLead
	err := q.QueryRow(ctx, query, token, kind).Scan(
		&ll.linkID, &ll.leadID, &ll.name, &ll.company, &ll.ownerID, &ll.ownerName, &ll.bookedTaskID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ll, ErrNotFound
	}
	if err != nil {
		return ll, fmt.Errorf("find %s link: %w", kind, err)
	}
	// A salesperson who was switched off cannot take the meeting.
	if ll.ownerName == "" {
		ll.ownerID = nil
	}
	return ll, nil
}

// firstName is how the pages greet someone: "Ananya" for "Ananya Sen".
func firstName(name string) string {
	if fields := strings.Fields(name); len(fields) > 0 {
		return fields[0]
	}
	return name
}

// --- booking -----------------------------------------------------------------

// freeSlotsSQL lists the owner's ($1) free slots in the bookable window,
// optionally only the one at $2 (date) and $3 (time). A slot is busy when an
// open timed event of theirs overlaps it.
var freeSlotsSQL = fmt.Sprintf(`
	WITH days AS (
		SELECT d::date AS day
		FROM generate_series(current_date, current_date + %[1]d, interval '1 day') d
		WHERE extract(isodow FROM d) < 7
	), slots AS (
		SELECT day, t::time AS start
		FROM days,
		     generate_series(timestamp '2000-01-01 %[2]s', timestamp '2000-01-01 %[3]s',
		                     interval '%[4]d minutes') t
	)
	SELECT to_char(s.day, 'YYYY-MM-DD'), to_char(s.start, 'HH24:MI')
	FROM slots s
	WHERE s.day + s.start > localtimestamp + interval '%[5]d hours'
	  AND ($2::date IS NULL OR (s.day = $2::date AND s.start = $3::time))
	  AND NOT EXISTS (
		SELECT 1 FROM tasks k
		WHERE k.assignee_id = $1 AND k.due_date = s.day
		  AND k.start_time IS NOT NULL
		  AND k.completed_at IS NULL AND k.cancelled_at IS NULL
		  AND k.start_time < s.start + interval '%[4]d minutes'
		  AND k.start_time + make_interval(mins => k.duration_minutes) > s.start
	  )
	ORDER BY s.day, s.start`,
	model.BookingDaysAhead-1, model.BookingFirstSlot, model.BookingLastSlot,
	model.BookingSlotMinutes, model.BookingLeadHours)

// upcomingBooking is the meeting booked with a link, while it is still ahead
// and neither done nor cancelled.
func upcomingBooking(ctx context.Context, q dbtx, taskID *int64) (*model.BookedMeeting, error) {
	if taskID == nil {
		return nil, nil
	}
	var b model.BookedMeeting
	err := q.QueryRow(ctx, `
		SELECT to_char(k.due_date, 'YYYY-MM-DD'), to_char(k.start_time, 'HH24:MI'), COALESCE(m.name, '')
		FROM tasks k LEFT JOIN team_members m ON m.id = k.assignee_id
		WHERE k.id = $1 AND k.start_time IS NOT NULL
		  AND k.completed_at IS NULL AND k.cancelled_at IS NULL
		  AND k.due_date + k.start_time > localtimestamp`, *taskID,
	).Scan(&b.Date, &b.Time, &b.OwnerName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("booked meeting: %w", err)
	}
	return &b, nil
}

// BookingPage is what the booking link shows. ErrNotFound for a bad token.
func (s *Store) BookingPage(ctx context.Context, token string) (model.BookingPage, error) {
	ll, err := leadByToken(ctx, s.pool, token, model.LinkBooking, false)
	if err != nil {
		return model.BookingPage{}, err
	}
	page := model.BookingPage{
		LeadName:    firstName(ll.name),
		Company:     ll.company,
		OwnerName:   ll.ownerName,
		Days:        []model.BookingDay{},
		SlotMinutes: model.BookingSlotMinutes,
	}
	if page.Booked, err = upcomingBooking(ctx, s.pool, ll.bookedTaskID); err != nil || page.Booked != nil {
		return page, err
	}
	if ll.ownerID == nil {
		return page, nil
	}

	rows, err := s.query(ctx, freeSlotsSQL, *ll.ownerID, nil, nil)
	if err != nil {
		return page, fmt.Errorf("free slots: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var day, start string
		if err := rows.Scan(&day, &start); err != nil {
			return page, fmt.Errorf("scan free slot: %w", err)
		}
		if n := len(page.Days); n == 0 || page.Days[n-1].Date != day {
			page.Days = append(page.Days, model.BookingDay{Date: day, Times: []string{}})
		}
		last := &page.Days[len(page.Days)-1]
		last.Times = append(last.Times, start)
	}
	return page, rows.Err()
}

// BookMeeting books the slot as a meeting for the lead's salesperson, with a
// 15-minute reminder, and notes it on the timeline. The slot is checked again
// with the salesperson's row locked, so two people cannot take the same one.
func (s *Store) BookMeeting(ctx context.Context, token string, in model.BookingInput) (model.BookedMeeting, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.BookedMeeting{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	ll, err := leadByToken(ctx, tx, token, model.LinkBooking, true)
	if err != nil {
		return model.BookedMeeting{}, err
	}
	if ll.ownerID == nil {
		return model.BookedMeeting{}, &FieldError{Field: "time", Message: "Nobody can take a meeting yet. Please contact us instead."}
	}
	if booked, err := upcomingBooking(ctx, tx, ll.bookedTaskID); err != nil {
		return model.BookedMeeting{}, err
	} else if booked != nil {
		return model.BookedMeeting{}, &FieldError{Field: "time", Message: "You already have a meeting booked."}
	}
	if _, err := tx.Exec(ctx, `SELECT 1 FROM team_members WHERE id = $1 FOR UPDATE`, *ll.ownerID); err != nil {
		return model.BookedMeeting{}, fmt.Errorf("lock salesperson: %w", err)
	}
	var day, start string
	err = tx.QueryRow(ctx, freeSlotsSQL, *ll.ownerID, in.Date, in.Time).Scan(&day, &start)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.BookedMeeting{}, &FieldError{Field: "time", Message: "That time is no longer free. Please pick another."}
	}
	if err != nil {
		return model.BookedMeeting{}, fmt.Errorf("check slot: %w", err)
	}

	notes := "Booked by the lead with their booking link."
	if in.Note != "" {
		notes += "\n\nThey would like to talk about: " + in.Note
	}
	reminder := 15
	var taskID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO tasks (lead_id, title, type, due_date, start_time, duration_minutes,
		                   notes, assignee_id, reminder_minutes)
		VALUES ($1, $2, 'meeting', $3::date, $4::time, $5, $6, $7, $8)
		RETURNING id`,
		ll.leadID, "Meeting with "+ll.name, day, start, model.BookingSlotMinutes,
		notes, *ll.ownerID, reminder,
	).Scan(&taskID); err != nil {
		return model.BookedMeeting{}, fmt.Errorf("insert booked meeting: %w", err)
	}

	when, _ := time.Parse(time.DateOnly, day)
	note := fmt.Sprintf("Booked a meeting with %s for %s at %s using the booking link.",
		ll.ownerName, when.Format("Mon 2 Jan"), start)
	if in.Note != "" {
		note += " Topic: " + in.Note
	}
	if err := insertActivityTx(ctx, tx, ll.leadID, model.ActivityBooking, note); err != nil {
		return model.BookedMeeting{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE customer_links SET booked_task_id = $2 WHERE id = $1`, ll.linkID, taskID); err != nil {
		return model.BookedMeeting{}, fmt.Errorf("remember booked meeting: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return model.BookedMeeting{}, fmt.Errorf("commit booking: %w", err)
	}
	return model.BookedMeeting{Date: day, Time: start, OwnerName: ll.ownerName}, nil
}

// --- feedback ----------------------------------------------------------------

// FeedbackPage is what the thank-you link shows. ErrNotFound for a bad token.
func (s *Store) FeedbackPage(ctx context.Context, token string) (model.FeedbackPage, error) {
	ll, err := leadByToken(ctx, s.pool, token, model.LinkFeedback, false)
	if err != nil {
		return model.FeedbackPage{}, err
	}
	page := model.FeedbackPage{LeadName: firstName(ll.name), Company: ll.company, OwnerName: ll.ownerName}
	err = s.queryRow(ctx, `SELECT EXISTS (SELECT 1 FROM lead_feedback WHERE lead_id = $1)`, ll.leadID).Scan(&page.Given)
	if err != nil {
		return page, fmt.Errorf("feedback given: %w", err)
	}
	return page, nil
}

// GiveFeedback saves a won customer's rating and comment, and makes the friend
// they referred into a new lead for the same salesperson. A referral whose
// email is already a lead is not added again; the feedback is still saved.
// ErrAlreadyGiven the second time.
func (s *Store) GiveFeedback(ctx context.Context, token string, in model.FeedbackInput) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	ll, err := leadByToken(ctx, tx, token, model.LinkFeedback, true)
	if err != nil {
		return err
	}

	var feedbackID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO lead_feedback (lead_id, rating, comment) VALUES ($1, $2, $3)
		ON CONFLICT (lead_id) DO NOTHING
		RETURNING id`, ll.leadID, in.Rating, in.Comment).Scan(&feedbackID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAlreadyGiven
	}
	if err != nil {
		return fmt.Errorf("insert feedback: %w", err)
	}

	note := fmt.Sprintf("Gave feedback: %d out of 5 stars.", in.Rating)
	if in.Comment != "" {
		note += fmt.Sprintf(" \"%s\"", in.Comment)
	}

	if in.Referral != nil {
		lead := in.Referral.LeadInput(ll.name+" ("+ll.company+")", ll.ownerID)
		lead.Normalize()
		// A savepoint, so a referral that is already a lead rolls back just
		// that, and the feedback is still saved.
		sp, err := tx.Begin(ctx)
		if err != nil {
			return fmt.Errorf("savepoint: %w", err)
		}
		referral, err := createLeadTx(ctx, sp, lead, true, nil)
		switch {
		case err == nil:
			if err := sp.Commit(ctx); err != nil {
				return fmt.Errorf("release savepoint: %w", err)
			}
			if _, err := tx.Exec(ctx, `UPDATE lead_feedback SET referral_lead_id = $2 WHERE id = $1`,
				feedbackID, referral.ID); err != nil {
				return fmt.Errorf("link referral: %w", err)
			}
			note += fmt.Sprintf(" Referred %s, added as a new lead.", referral.Name)
		case errors.Is(err, ErrDuplicateEmail):
			_ = sp.Rollback(ctx)
			note += fmt.Sprintf(" Referred %s (%s), who is already a lead.", lead.Name, lead.Email)
		default:
			return err
		}
	}

	if err := insertActivityTx(ctx, tx, ll.leadID, model.ActivityFeedback, note); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit feedback: %w", err)
	}
	return nil
}
