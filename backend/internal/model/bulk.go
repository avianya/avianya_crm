package model

import (
	"fmt"
	"strings"
)

// This file is bulk actions on leads: one change applied to many leads ticked
// on the Leads list. Each lead goes through the same store method as a change
// made on its own, so the timeline, stage history and close-reason rules are
// the same either way.

// BulkAction is what to do to the leads.
type BulkAction string

const (
	BulkOwner  BulkAction = "owner"
	BulkStage  BulkAction = "stage"
	BulkSource BulkAction = "source"
	BulkDelete BulkAction = "delete"
)

var bulkActions = []BulkAction{BulkOwner, BulkStage, BulkSource, BulkDelete}

// MaxBulkLeads is the most leads one request may change: a full page of the
// list, which is all the list lets a person tick.
const MaxBulkLeads = MaxPageSize

// BulkInput is the body of POST /api/leads/bulk. Only the fields for the
// action are read: OwnerID (null for nobody) for "owner", Stage and the close
// reason for "stage", Source for "source".
type BulkInput struct {
	IDs     []int64    `json:"ids"`
	Action  BulkAction `json:"action"`
	OwnerID *int64     `json:"owner_id"`
	Stage   Stage      `json:"stage"`
	Source  Source     `json:"source"`
	CloseInput
}

// Normalize drops repeated ids, keeping the first of each, and tidies the
// text fields.
func (in *BulkInput) Normalize() {
	seen := make(map[int64]bool, len(in.IDs))
	ids := make([]int64, 0, len(in.IDs))
	for _, id := range in.IDs {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	in.IDs = ids
	in.Action = BulkAction(strings.ToLower(strings.TrimSpace(string(in.Action))))
	in.Stage = Stage(strings.TrimSpace(string(in.Stage)))
	in.Source = Source(strings.TrimSpace(string(in.Source)))
	in.CloseInput.Normalize()
}

// Validate checks the ids and the fields the action needs, with the same
// rules as the single-lead endpoints.
func (in BulkInput) Validate() ValidationErrors {
	errs := ValidationErrors{}

	switch {
	case len(in.IDs) == 0:
		errs["ids"] = "pick at least one lead"
	case len(in.IDs) > MaxBulkLeads:
		errs["ids"] = fmt.Sprintf("at most %d leads at a time", MaxBulkLeads)
	default:
		for _, id := range in.IDs {
			if id <= 0 {
				errs["ids"] = "must be lead ids"
				break
			}
		}
	}

	switch in.Action {
	case BulkOwner:
		if in.OwnerID != nil && *in.OwnerID <= 0 {
			errs["owner_id"] = "must be a team member's id, or null for nobody"
		}
	case BulkStage:
		for field, msg := range (StageInput{Stage: in.Stage, CloseInput: in.CloseInput}).Validate() {
			errs[field] = msg
		}
	case BulkSource:
		if !in.Source.Valid() {
			errs["source"] = "must be one of: " + joinSources(Sources)
		}
	case BulkDelete:
	default:
		names := make([]string, len(bulkActions))
		for i, a := range bulkActions {
			names[i] = string(a)
		}
		errs["action"] = "must be one of: " + strings.Join(names, ", ")
	}
	return errs
}

// BulkFailure is a lead a bulk action could not change, and why.
type BulkFailure struct {
	ID      int64  `json:"id"`
	Message string `json:"message"`
}

// BulkResult is the response of POST /api/leads/bulk.
type BulkResult struct {
	Action  BulkAction    `json:"action"`
	Changed int           `json:"changed"`
	Failed  []BulkFailure `json:"failed"`
}
