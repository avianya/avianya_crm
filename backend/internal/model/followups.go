package model

import "sort"

// This file shapes the Follow-up Center: one answer to "who do I need to
// contact?", built from the open tasks and the open leads' health. Nothing
// here is stored; it is all derived on each read, like health and score.

// OwnerFilter narrows to one person's work. For leads it is the owner, for
// tasks the assignee (the person who will actually make the call). The zero
// value means everyone; Unowned means nobody's.
type OwnerFilter struct {
	OwnerID *int64
	Unowned bool
}

// FollowUpSummary is the body of GET /api/follow-ups.
type FollowUpSummary struct {
	// Today is the server's date ("2006-01-02") the buckets were cut on, so
	// the page can say "due today" without trusting the browser's clock.
	Today string `json:"today"`

	// Open tasks, each with its lead. Overdue is everything whose time has
	// passed, most overdue first; DueToday is the rest of today; ThisWeek is
	// the next seven days after today. Every list is non-nil.
	Overdue  []TaskWithLead `json:"overdue"`
	DueToday []TaskWithLead `json:"due_today"`
	ThisWeek []TaskWithLead `json:"this_week"`

	// GoingCold is open leads that are Cooling or Cold, furthest gone first.
	// NoNextStep is open leads with no open task, best score first. A lead
	// can be in both.
	GoingCold  []Lead `json:"going_cold"`
	NoNextStep []Lead `json:"no_next_step"`

	// Stuck is open deals past their stage's time limit, furthest past first.
	Stuck []Lead `json:"stuck"`
}

// Caps for one Follow-up Center read. Far above a working day's list, but a
// limit all the same.
const (
	MaxFollowUpTasks = 500
	MaxFollowUpLeads = 1000
)

// SplitFollowUpTasks puts open tasks due up to a week ahead into the three
// task buckets. Overdue comes from the task's status, so a call at 10:00 today
// is overdue at 11:00; anything else dated today is due today; the rest is
// later this week. The input order is kept within each bucket.
func SplitFollowUpTasks(items []TaskWithLead, today string) (overdue, dueToday, thisWeek []TaskWithLead) {
	overdue = make([]TaskWithLead, 0)
	dueToday = make([]TaskWithLead, 0)
	thisWeek = make([]TaskWithLead, 0)
	for _, item := range items {
		switch {
		case item.Task.Status == TaskOverdue:
			overdue = append(overdue, item)
		case item.Task.DueDate <= today:
			dueToday = append(dueToday, item)
		default:
			thisWeek = append(thisWeek, item)
		}
	}
	return overdue, dueToday, thisWeek
}

// SplitAttentionLeads picks the open leads that need attention out of leads
// whose insights and stage age are already applied. Won and Lost leads are
// skipped.
func SplitAttentionLeads(leads []Lead) (goingCold, noNextStep, stuck []Lead) {
	goingCold = make([]Lead, 0)
	noNextStep = make([]Lead, 0)
	stuck = make([]Lead, 0)
	for _, lead := range leads {
		if lead.Stage.IsClosed() {
			continue
		}
		if lead.Health == HealthCooling || lead.Health == HealthCold {
			goingCold = append(goingCold, lead)
		}
		if !lead.HasOpenTask {
			noNextStep = append(noNextStep, lead)
		}
		if lead.Stuck {
			stuck = append(stuck, lead)
		}
	}

	// Furthest past its stage's cold threshold first, the same ranking as the
	// dashboard: 6 silent days in Negotiation (cold at 7) beats 10 in
	// Qualified (cold at 14).
	sort.SliceStable(goingCold, func(i, j int) bool {
		ri, rj := coldRatio(goingCold[i]), coldRatio(goingCold[j])
		if ri != rj {
			return ri > rj
		}
		return goingCold[i].ID < goingCold[j].ID
	})

	// Most promising first: those are the next steps worth planning now.
	sort.SliceStable(noNextStep, func(i, j int) bool {
		si, sj := scoreTotal(noNextStep[i]), scoreTotal(noNextStep[j])
		if si != sj {
			return si > sj
		}
		return noNextStep[i].ID < noNextStep[j].ID
	})

	// Furthest past its stage's limit first.
	sort.SliceStable(stuck, func(i, j int) bool {
		ri, rj := stuckRatio(stuck[i]), stuckRatio(stuck[j])
		if ri != rj {
			return ri > rj
		}
		return stuck[i].ID < stuck[j].ID
	})

	return goingCold, noNextStep, stuck
}

func stuckRatio(lead Lead) float64 {
	if lead.StuckLimitDays == 0 {
		return 0
	}
	return float64(lead.DaysInStage) / float64(lead.StuckLimitDays)
}

func coldRatio(lead Lead) float64 {
	t, ok := lead.Stage.Thresholds()
	if !ok || t.ColdDays == 0 {
		return 0
	}
	return float64(lead.DaysSinceTouch) / float64(t.ColdDays)
}

func scoreTotal(lead Lead) int {
	if lead.Score == nil {
		return -1
	}
	return lead.Score.Total
}
