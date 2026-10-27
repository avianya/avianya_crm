package model

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Lead quality is a judgement about the lead itself -- is this a real,
// reachable buyer who fits? -- and is kept apart from the stage (how far the
// deal has got), from the pipeline score in insights.go (how actively it is
// being worked) and from what is reported to Meta. A Good lead can sit in New;
// a Poor one can be mid-negotiation.
//
// The score comes from rules an admin configures. Each rule looks at one
// thing and comes out matched, failed or unknown. Unknown -- the data is not
// there yet -- is never held against a lead: a lead the CRM knows little about
// is Not Evaluated, not Poor. Only evidence makes a lead Poor: answers that do
// not fit, contact details that are invalid, or a person confirming it is
// spam. Every score keeps a sentence saying why.

// Quality is the band a lead's quality score falls in.
type Quality string

const (
	QualityGood         Quality = "good"
	QualityAverage      Quality = "average"
	QualityPoor         Quality = "poor"
	QualityNotEvaluated Quality = "not_evaluated"
)

// Qualities lists every value, best first.
var Qualities = []Quality{QualityGood, QualityAverage, QualityPoor, QualityNotEvaluated}

var qualityLabels = map[Quality]string{
	QualityGood:         "Good",
	QualityAverage:      "Average",
	QualityPoor:         "Poor",
	QualityNotEvaluated: "Not Evaluated",
}

// Valid reports whether q is a known quality.
func (q Quality) Valid() bool { _, ok := qualityLabels[q]; return ok }

// Label is the display form, e.g. "Not Evaluated".
func (q Quality) Label() string {
	if l, ok := qualityLabels[q]; ok {
		return l
	}
	return string(q)
}

// ---------------------------------------------------------------------------
// Qualification status
// ---------------------------------------------------------------------------

// QualificationStatus is whether the lead has been qualified, read from its
// stage so the two can never disagree: Qualified or any later stage (Won
// included) is qualified; Lost is not qualified; anything earlier is pending.
type QualificationStatus string

const (
	QualificationPending      QualificationStatus = "pending"
	QualificationQualified    QualificationStatus = "qualified"
	QualificationNotQualified QualificationStatus = "not_qualified"
)

// QualificationStatuses lists every value, for filters.
var QualificationStatuses = []QualificationStatus{QualificationPending, QualificationQualified, QualificationNotQualified}

// Valid reports whether s is a known status.
func (s QualificationStatus) Valid() bool { return slices.Contains(QualificationStatuses, s) }

// QualificationFor reads the status from a stage.
func QualificationFor(stage Stage) QualificationStatus {
	switch stage {
	case StageQualified, StageProposalSent, StageNegotiation, StageWon:
		return QualificationQualified
	case StageLost:
		return QualificationNotQualified
	default:
		return QualificationPending
	}
}

// QualifiedStages are the stages QualificationQualified covers, for SQL.
var QualifiedStages = []Stage{StageQualified, StageProposalSent, StageNegotiation, StageWon}

// ---------------------------------------------------------------------------
// Rules
// ---------------------------------------------------------------------------

// QualityCategory groups rules by what they are evidence of.
type QualityCategory string

const (
	CategoryContact   QualityCategory = "contact_validity"
	CategoryService   QualityCategory = "service_fit"
	CategoryBudget    QualityCategory = "budget_fit"
	CategoryFollowUp  QualityCategory = "follow_up_response"
	CategoryMeeting   QualityCategory = "meeting_attendance"
	CategoryOtherRule QualityCategory = "other"
)

var qualityCategories = []QualityCategory{CategoryContact, CategoryService, CategoryBudget, CategoryFollowUp, CategoryMeeting, CategoryOtherRule}

// QualityField is what a rule looks at.
type QualityField string

const (
	FieldEmail   QualityField = "email"
	FieldPhone   QualityField = "phone"
	FieldCompany QualityField = "company"
	FieldName    QualityField = "name"
	// FieldAnswer is one form question, named by AnswerKey (the question's
	// key or label on the form, compared ignoring case, spaces and
	// punctuation).
	FieldAnswer QualityField = "answer"
	// FieldConversations counts the calls, emails and meetings logged with
	// the lead: did it respond to follow-ups?
	FieldConversations QualityField = "conversations"
	// FieldMeetings counts meetings logged, and meeting or demo events
	// marked done: did it turn up?
	FieldMeetings QualityField = "meetings"
)

var qualityFields = []QualityField{FieldEmail, FieldPhone, FieldCompany, FieldName, FieldAnswer, FieldConversations, FieldMeetings}

func (f QualityField) numeric() bool { return f == FieldConversations || f == FieldMeetings }

// QualityOp is the test a rule applies to its field.
type QualityOp string

const (
	OpPresent  QualityOp = "present"  // not blank
	OpEquals   QualityOp = "equals"   // same text, ignoring case
	OpContains QualityOp = "contains" // has the text in it, ignoring case
	OpOneOf    QualityOp = "one_of"   // equals any of Value's comma-separated items
	// OpBusinessEmail: an address not at a free mail provider. Email only.
	OpBusinessEmail QualityOp = "business_email"
	// OpValidEmail: an address shaped like one, at a domain with a dot.
	OpValidEmail QualityOp = "valid_email"
	// OpValidPhone: at least 8 digits.
	OpValidPhone QualityOp = "valid_phone"
	// OpFullName: at least two words. Name only.
	OpFullName QualityOp = "full_name"
	// OpAtLeast: a count of at least Value. Conversations and meetings only.
	OpAtLeast QualityOp = "at_least"
)

var qualityOps = []QualityOp{OpPresent, OpEquals, OpContains, OpOneOf, OpBusinessEmail, OpValidEmail, OpValidPhone, OpFullName, OpAtLeast}

// QualityRule adds Points (negative to take them away) when its test passes.
type QualityRule struct {
	ID        string          `json:"id"`
	Label     string          `json:"label"`
	Category  QualityCategory `json:"category"`
	Field     QualityField    `json:"field"`
	AnswerKey string          `json:"answer_key,omitempty"`
	Op        QualityOp       `json:"op"`
	Value     string          `json:"value,omitempty"`
	Points    int             `json:"points"`
	// RequiredForGood: a lead is Good only once this rule has matched, however
	// high its score.
	RequiredForGood bool `json:"required_for_good"`
}

// QualityRules is the whole configuration: the rules and the cut-offs.
type QualityRules struct {
	Rules []QualityRule `json:"rules"`
	// GoodFrom and AverageFrom are the lowest scores that count as Good and
	// Average; anything lower is Poor.
	GoodFrom    int `json:"good_from"`
	AverageFrom int `json:"average_from"`
	// MinEvidence is how many points' worth of rules must be decidable
	// (matched or failed, not unknown) before a lead is judged at all.
	MinEvidence int `json:"min_evidence"`
}

// DefaultQualityRules are used until an admin saves their own. They add up to
// 100 across the categories. The service and budget rules only check that the
// form asked and was answered; an admin should change them to the answers
// that fit (one_of), and add penalties for ones that do not.
func DefaultQualityRules() QualityRules {
	return QualityRules{
		GoodFrom:    70,
		AverageFrom: 40,
		MinEvidence: 40,
		Rules: []QualityRule{
			{ID: "valid_email", Label: "Valid email address", Category: CategoryContact, Field: FieldEmail, Op: OpValidEmail, Points: 20},
			{ID: "valid_phone", Label: "Valid phone number", Category: CategoryContact, Field: FieldPhone, Op: OpValidPhone, Points: 20, RequiredForGood: true},
			{ID: "service", Label: "Said what service they need", Category: CategoryService, Field: FieldAnswer, AnswerKey: "service", Op: OpPresent, Points: 15},
			{ID: "budget", Label: "Gave a budget", Category: CategoryBudget, Field: FieldAnswer, AnswerKey: "budget", Op: OpPresent, Points: 15},
			{ID: "responded", Label: "Responded to a follow-up", Category: CategoryFollowUp, Field: FieldConversations, Op: OpAtLeast, Value: "1", Points: 15},
			{ID: "met", Label: "Attended a meeting", Category: CategoryMeeting, Field: FieldMeetings, Op: OpAtLeast, Value: "1", Points: 15},
		},
	}
}

var ruleIDPattern = regexp.MustCompile(`^[a-z0-9_]{1,40}$`)

// MaxQualityRules bounds the rule list.
const MaxQualityRules = 30

// Normalize tidies an admin's input before validation.
func (r *QualityRules) Normalize() {
	if r.Rules == nil {
		r.Rules = []QualityRule{}
	}
	for i := range r.Rules {
		rule := &r.Rules[i]
		rule.ID = strings.ToLower(strings.TrimSpace(rule.ID))
		rule.Label = strings.TrimSpace(rule.Label)
		rule.AnswerKey = strings.TrimSpace(rule.AnswerKey)
		rule.Value = strings.TrimSpace(rule.Value)
		if rule.Field != FieldAnswer {
			rule.AnswerKey = ""
		}
		if rule.Category == "" {
			rule.Category = CategoryOtherRule
		}
	}
}

// Validate checks the configuration, keyed "rules[2].points" and so on.
func (r QualityRules) Validate() ValidationErrors {
	errs := ValidationErrors{}
	if len(r.Rules) > MaxQualityRules {
		errs["rules"] = fmt.Sprintf("can have at most %d rules", MaxQualityRules)
	}
	if r.AverageFrom < 1 || r.AverageFrom > 100 {
		errs["average_from"] = "must be from 1 to 100"
	}
	if r.GoodFrom <= r.AverageFrom || r.GoodFrom > 100 {
		errs["good_from"] = "must be above average_from and at most 100"
	}
	if r.MinEvidence < 0 || r.MinEvidence > 100 {
		errs["min_evidence"] = "must be from 0 to 100"
	}
	seen := map[string]bool{}
	for i, rule := range r.Rules {
		at := func(field string) string { return fmt.Sprintf("rules[%d].%s", i, field) }
		switch {
		case !ruleIDPattern.MatchString(rule.ID):
			errs[at("id")] = "must be 1-40 lowercase letters, digits or _"
		case seen[rule.ID]:
			errs[at("id")] = "is used by another rule"
		}
		seen[rule.ID] = true
		if rule.Label == "" || len(rule.Label) > 80 {
			errs[at("label")] = "is required, at most 80 characters"
		}
		if !slices.Contains(qualityCategories, rule.Category) {
			errs[at("category")] = "is not a known category"
		}
		if !slices.Contains(qualityFields, rule.Field) {
			errs[at("field")] = "must be email, phone, company, name, answer, conversations or meetings"
		}
		if rule.Field == FieldAnswer && rule.AnswerKey == "" {
			errs[at("answer_key")] = "is required for a form answer rule"
		}
		if !slices.Contains(qualityOps, rule.Op) {
			errs[at("op")] = "is not a known test"
		}
		switch rule.Op {
		case OpBusinessEmail, OpValidEmail:
			if rule.Field != FieldEmail {
				errs[at("op")] = "only applies to the email field"
			}
		case OpValidPhone:
			if rule.Field != FieldPhone {
				errs[at("op")] = "only applies to the phone field"
			}
		case OpFullName:
			if rule.Field != FieldName {
				errs[at("op")] = "only applies to the name field"
			}
		case OpAtLeast:
			if !rule.Field.numeric() {
				errs[at("op")] = "only applies to conversations and meetings"
			} else if n, err := strconv.Atoi(rule.Value); err != nil || n < 1 || n > 100 {
				errs[at("value")] = "must be a whole number from 1 to 100"
			}
		case OpEquals, OpContains, OpOneOf:
			if rule.Value == "" {
				errs[at("value")] = "is required for this test"
			}
		}
		if rule.Field.numeric() && rule.Op != OpAtLeast {
			errs[at("op")] = "conversations and meetings use at_least"
		}
		if len(rule.Value) > 200 {
			errs[at("value")] = "must be at most 200 characters"
		}
		if rule.Points < -100 || rule.Points > 100 || rule.Points == 0 {
			errs[at("points")] = "must be from -100 to 100, and not 0"
		}
		if rule.RequiredForGood && rule.Points < 0 {
			errs[at("required_for_good")] = "a penalty cannot be required"
		}
	}
	return errs
}

// freeMailDomains are providers anyone can sign up to, so an address there
// says nothing about an employer.
var freeMailDomains = []string{
	"gmail.com", "googlemail.com", "yahoo.com", "yahoo.co.in", "ymail.com", "rediffmail.com",
	"hotmail.com", "outlook.com", "live.com", "msn.com", "icloud.com", "me.com", "aol.com",
	"proton.me", "protonmail.com", "gmx.com", "zoho.com", "yandex.com", "mail.com",
}

var emailShape = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]{2,}$`)

// QualityInput is what the rules can look at.
type QualityInput struct {
	Name, Email, Phone, Company string
	// Answers are the lead form's answers; nil for a lead with none.
	Answers []AdAnswer
	// Conversations and Meetings are counts from the lead's timeline and
	// events (see FieldConversations, FieldMeetings).
	Conversations, Meetings int
	// Spam: a person confirmed the lead's data is invalid or spam.
	Spam bool
}

// Outcome is what one rule found.
type Outcome string

const (
	OutcomeMatched Outcome = "matched"
	OutcomeFailed  Outcome = "failed"
	// OutcomeUnknown: the data the rule needs is not there yet. It neither
	// adds points nor counts against the lead.
	OutcomeUnknown Outcome = "unknown"
)

// QualityHit is one rule's outcome, kept with the lead so the score always
// shows its working, even after the rules change.
type QualityHit struct {
	RuleID   string          `json:"rule_id"`
	Label    string          `json:"label"`
	Category QualityCategory `json:"category"`
	Outcome  Outcome         `json:"outcome"`
	// Matched is Outcome == matched, kept for older stored breakdowns.
	Matched bool `json:"matched"`
	Points  int  `json:"points"` // what it added: the rule's points if matched, else 0
	// Max is the rule's own points: what it could add (or take away).
	Max      int  `json:"max"`
	Required bool `json:"required"`
}

// Assessment is the outcome of scoring one lead.
type Assessment struct {
	// Score is nil when the lead could not be judged (Not Evaluated).
	Score   *int
	Quality Quality
	Hits    []QualityHit
	// Reason says why, in a sentence or two.
	Reason string
}

// Assess scores a lead against the rules.
func (r QualityRules) Assess(in QualityInput) Assessment {
	if in.Spam {
		zero := 0
		return Assessment{Score: &zero, Quality: QualityPoor, Hits: []QualityHit{},
			Reason: "Poor: a person confirmed this lead's details are invalid or spam."}
	}
	if len(r.Rules) == 0 {
		return Assessment{Quality: QualityNotEvaluated, Hits: []QualityHit{}, Reason: "Not evaluated: no quality rules are set up."}
	}

	var (
		hits                      = make([]QualityHit, 0, len(r.Rules))
		score, decided, available int
		matched, failed, unknown  []string
		missingRequired           []string
	)
	for _, rule := range r.Rules {
		outcome := rule.outcome(in)
		hit := QualityHit{RuleID: rule.ID, Label: rule.Label, Category: rule.Category, Outcome: outcome,
			Matched: outcome == OutcomeMatched, Max: rule.Points, Required: rule.RequiredForGood}
		if rule.Points > 0 {
			available += rule.Points
		}
		switch outcome {
		case OutcomeMatched:
			hit.Points = rule.Points
			score += rule.Points
			if rule.Points > 0 {
				decided += rule.Points
				matched = append(matched, rule.Label)
			} else {
				failed = append(failed, rule.Label) // a matched penalty is evidence against
			}
		case OutcomeFailed:
			if rule.Points > 0 {
				decided += rule.Points
				failed = append(failed, "not "+lowerFirst(rule.Label))
			}
		default:
			if rule.Points > 0 {
				unknown = append(unknown, lowerFirst(rule.Label))
			}
		}
		if rule.RequiredForGood && outcome != OutcomeMatched {
			missingRequired = append(missingRequired, lowerFirst(rule.Label))
		}
		hits = append(hits, hit)
	}
	score = min(max(score, 0), 100)

	details := func() string {
		var parts []string
		if len(matched) > 0 {
			parts = append(parts, "has "+lowerFirst(strings.Join(matched, ", ")))
		}
		if len(failed) > 0 {
			parts = append(parts, strings.Join(failed, ", "))
		}
		if len(unknown) > 0 {
			parts = append(parts, "not known yet: "+strings.Join(unknown, ", "))
		}
		return strings.Join(parts, "; ")
	}

	if decided < r.MinEvidence {
		return Assessment{Quality: QualityNotEvaluated, Hits: hits,
			Reason: fmt.Sprintf("Not evaluated: not enough known yet to judge (%d of %d points can be decided, %d needed). %s.",
				decided, available, r.MinEvidence, capitalise(details()))}
	}

	quality := r.Band(score)
	reason := fmt.Sprintf("%s (%d/100): %s.", quality.Label(), score, details())
	if quality == QualityGood && len(missingRequired) > 0 {
		quality = QualityAverage
		reason = fmt.Sprintf("Average (%d/100, Good once confirmed: %s): %s.", score, strings.Join(missingRequired, ", "), details())
	}
	return Assessment{Score: &score, Quality: quality, Hits: hits, Reason: reason}
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	// Keep acronyms ("CRM ...") as they are.
	if len(r) > 1 && unicode.IsUpper(r[1]) {
		return s
	}
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

func capitalise(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// outcome applies one rule. Missing data is unknown, never failed.
func (rule QualityRule) outcome(in QualityInput) Outcome {
	if rule.Field.numeric() {
		count := in.Conversations
		if rule.Field == FieldMeetings {
			count = in.Meetings
		}
		want, _ := strconv.Atoi(rule.Value)
		switch {
		case count >= max(want, 1):
			return OutcomeMatched
		case count == 0:
			return OutcomeUnknown // nothing has happened yet, which proves nothing
		default:
			return OutcomeFailed
		}
	}

	var value string
	switch rule.Field {
	case FieldEmail:
		value = in.Email
	case FieldPhone:
		value = in.Phone
	case FieldCompany:
		// The placeholder for "the form did not ask" is not a company.
		if in.Company != NotProvided {
			value = in.Company
		}
	case FieldName:
		value = in.Name
	case FieldAnswer:
		want := csvKey(rule.AnswerKey)
		for _, a := range in.Answers {
			if csvKey(a.Key) == want || csvKey(a.Label) == want {
				value = a.Value
				break
			}
		}
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return OutcomeUnknown
	}
	lower := strings.ToLower(value)

	ok := false
	switch rule.Op {
	case OpPresent:
		ok = true
	case OpEquals:
		ok = strings.EqualFold(value, rule.Value)
	case OpContains:
		ok = strings.Contains(lower, strings.ToLower(rule.Value))
	case OpOneOf:
		for _, item := range strings.Split(rule.Value, ",") {
			if item = strings.TrimSpace(item); item != "" && strings.EqualFold(value, item) {
				ok = true
			}
		}
	case OpBusinessEmail:
		at := strings.LastIndex(lower, "@")
		ok = at > 0 && at < len(lower)-1 && !slices.Contains(freeMailDomains, lower[at+1:])
	case OpValidEmail:
		ok = emailShape.MatchString(value)
	case OpValidPhone:
		digits := 0
		for _, r := range value {
			if r >= '0' && r <= '9' {
				digits++
			}
		}
		ok = digits >= 8 && digits <= 15
	case OpFullName:
		ok = len(strings.Fields(value)) >= 2
	}
	if ok {
		return OutcomeMatched
	}
	return OutcomeFailed
}

// Band is the quality for a score under these cut-offs.
func (r QualityRules) Band(score int) Quality {
	switch {
	case score >= r.GoodFrom:
		return QualityGood
	case score >= r.AverageFrom:
		return QualityAverage
	default:
		return QualityPoor
	}
}

// Final is a lead's quality and reason once a hand override (if any) and
// confirmed spam are taken into account. Spam beats an override: a lead a
// person confirmed is spam stays Poor.
func (r QualityRules) Final(a Assessment, override *int, overrideReason *string, spam bool) (Quality, string) {
	switch {
	case spam:
		return QualityPoor, a.Reason
	case override != nil:
		reason := fmt.Sprintf("Set by hand to %d", *override)
		if overrideReason != nil {
			reason += ": " + *overrideReason
		}
		return r.Band(*override), reason + "."
	default:
		return a.Quality, a.Reason
	}
}

// ---------------------------------------------------------------------------
// Inputs
// ---------------------------------------------------------------------------

// QualityOverrideInput is PUT /api/leads/:id/quality-override.
type QualityOverrideInput struct {
	Score  *int   `json:"score"`
	Reason string `json:"reason"`
}

func (in *QualityOverrideInput) Normalize() { in.Reason = strings.TrimSpace(in.Reason) }

func (in QualityOverrideInput) Validate() ValidationErrors {
	errs := ValidationErrors{}
	if in.Score == nil || *in.Score < 0 || *in.Score > 100 {
		errs["score"] = "must be a whole number from 0 to 100"
	}
	if in.Reason == "" {
		errs["reason"] = "is required: say why the score is being overridden"
	} else if len([]rune(in.Reason)) > 500 {
		errs["reason"] = "must be at most 500 characters"
	}
	return errs
}

// QualificationInput is PATCH /api/leads/:id/qualification: the salesperson's
// note on why the lead is, or is not, qualified.
type QualificationInput struct {
	Reason string `json:"reason"`
}

func (in *QualificationInput) Normalize() { in.Reason = strings.TrimSpace(in.Reason) }

func (in QualificationInput) Validate() ValidationErrors {
	if len([]rune(in.Reason)) > 1000 {
		return ValidationErrors{"reason": "must be at most 1000 characters"}
	}
	return ValidationErrors{}
}

// LeadFlagsInput is PATCH /api/leads/:id/flags: confirm spam, or opt the lead
// out of sharing with Meta. A field left out is left as it is.
type LeadFlagsInput struct {
	SpamConfirmed   *bool `json:"spam_confirmed"`
	MetaShareOptOut *bool `json:"meta_share_opt_out"`
}

// LeadQuality is the quality part of a lead, as stored.
type LeadQuality struct {
	Quality               Quality      `json:"quality"`
	QualityScore          *int         `json:"quality_score"`
	QualityBreakdown      []QualityHit `json:"quality_breakdown"`
	QualityReason         string       `json:"quality_reason"`
	QualityUpdatedAt      *time.Time   `json:"quality_updated_at"`
	QualityOverride       *int         `json:"quality_override"`
	QualityOverrideReason *string      `json:"quality_override_reason"`
	QualityOverrideAt     *time.Time   `json:"quality_override_at"`
	QualificationReason   string       `json:"qualification_reason"`
	// QualificationStatus is read from the stage (see QualificationFor).
	QualificationStatus QualificationStatus `json:"qualification_status"`
	SpamConfirmed       bool                `json:"spam_confirmed"`
	// MetaShareOptOut: this lead's events are never sent to Meta.
	MetaShareOptOut bool `json:"meta_share_opt_out"`
}

// LeadMetaSummary is a lead's latest conversion event, for the lead table and
// detail. Empty when the lead has sent nothing.
type LeadMetaSummary struct {
	MetaCAPIStatus    CAPIStatus `json:"meta_capi_status"`
	MetaLastEventName string     `json:"meta_last_event_name"`
	MetaLastEventAt   *time.Time `json:"meta_last_event_at"`
	MetaLastErrorCode *int       `json:"meta_last_error_code"`
}
