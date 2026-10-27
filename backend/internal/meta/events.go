package meta

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
	"unicode"
)

// Customer matching data is normalised and SHA-256 hashed before it leaves
// the server, as Meta requires for the Conversions API. Only three things are
// sent: the lead's Meta id (unhashed; Meta issued it), and the hashed email
// and phone it already gave in the form. Nothing else about the person.

// HashEmail normalises an email (trimmed, lower case) and hashes it. "" for no
// email.
func HashEmail(email string) string {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return ""
	}
	return sha256Hex(email)
}

// NormalizePhone keeps only digits, with the country code. A number written
// with + or 00 already has one; otherwise defaultCountryCode (e.g. "91") is
// put in front, after dropping the trunk prefix 0. Without a country code to
// use, a number that did not come with one is left out: guessing would match
// the wrong person. "" for no usable number.
func NormalizePhone(phone, defaultCountryCode string) string {
	trimmed := strings.TrimSpace(phone)
	digits := strings.Map(func(r rune) rune {
		if unicode.IsDigit(r) && r < unicode.MaxASCII {
			return r
		}
		return -1
	}, trimmed)
	switch {
	case digits == "":
		return ""
	case strings.HasPrefix(trimmed, "+"):
		return digits
	case strings.HasPrefix(digits, "00"):
		return strings.TrimLeft(digits, "0")
	}
	cc := strings.TrimLeft(strings.TrimSpace(defaultCountryCode), "+")
	if cc == "" {
		return ""
	}
	return cc + strings.TrimLeft(digits, "0")
}

// HashPhone normalises and hashes a phone number; "" when there is none.
func HashPhone(phone, defaultCountryCode string) string {
	n := NormalizePhone(phone, defaultCountryCode)
	if n == "" {
		return ""
	}
	return sha256Hex(n)
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// LeadEvent is what the CRM knows about one conversion to report.
type LeadEvent struct {
	EventName  string
	EventID    string
	EventTime  time.Time
	MetaLeadID string
	Email      string
	Phone      string
	// CountryCode is the default for phone numbers written without one.
	CountryCode string
	// CRMName names the CRM to Meta (custom_data.lead_event_source).
	CRMName string
}

// ServerEvent builds the Conversions API event for a lead's stage change, as
// Meta's CRM integration describes it.
func (e LeadEvent) ServerEvent() ServerEvent {
	user := map[string]any{}
	if e.MetaLeadID != "" {
		// Meta's lead id is a number; sent as one when it parses, the way the
		// CRM integration examples show it.
		user["lead_id"] = leadIDValue(e.MetaLeadID)
	}
	if h := HashEmail(e.Email); h != "" {
		user["em"] = []string{h}
	}
	if h := HashPhone(e.Phone, e.CountryCode); h != "" {
		user["ph"] = []string{h}
	}
	crm := e.CRMName
	if crm == "" {
		crm = "Pipeline CRM"
	}
	return ServerEvent{
		EventName:    e.EventName,
		EventTime:    e.EventTime.Unix(),
		EventID:      e.EventID,
		ActionSource: "system_generated",
		UserData:     user,
		CustomData:   map[string]any{"event_source": "crm", "lead_event_source": crm},
	}
}

// leadIDValue returns the id as an integer when it is one (Meta's ids fit in
// 64 bits), else the string.
func leadIDValue(id string) any {
	var n int64
	for _, r := range id {
		if r < '0' || r > '9' || n > (1<<62)/10 {
			return id
		}
		n = n*10 + int64(r-'0')
	}
	return n
}

// MaxEventAge is how old an event Meta accepts: event_time may be at most
// seven days in the past. An older one is refused, so it is sent with the
// time it is sent instead (see EventTimeFor).
const MaxEventAge = 7 * 24 * time.Hour

// EventTimeFor is the event_time to send for something that happened at
// happened, sent at now: the real time while Meta still accepts it, else now.
func EventTimeFor(happened, now time.Time) time.Time {
	if now.Sub(happened) > MaxEventAge-time.Hour {
		return now
	}
	return happened
}
