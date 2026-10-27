package model

// This file is round-robin lead assignment: when it is on, a new lead that
// nobody was chosen for goes to the next person in the rotation, in turn.
// The store applies it inside the transaction that creates the lead.

// LeadAssignment is the saved setting, kept in app_settings.
type LeadAssignment struct {
	Enabled bool `json:"enabled"`
	// MemberIDs are the people in the rotation. They take turns in id order;
	// anyone switched off is skipped until they are switched on again.
	MemberIDs []int64 `json:"member_ids"`
	// LastMemberID got the last lead, so the next one goes to whoever follows.
	LastMemberID *int64 `json:"last_member_id"`
}

// LeadAssignmentInput is PUT /api/lead-assignment.
type LeadAssignmentInput struct {
	Enabled   bool    `json:"enabled"`
	MemberIDs []int64 `json:"member_ids"`
}

// Normalize drops repeated ids, keeping the first of each.
func (in *LeadAssignmentInput) Normalize() {
	ids := make([]int64, 0, len(in.MemberIDs))
	seen := make(map[int64]bool, len(in.MemberIDs))
	for _, id := range in.MemberIDs {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	in.MemberIDs = ids
}

// Validate checks the ids look like ids, and that a rotation switched on has
// someone in it.
func (in LeadAssignmentInput) Validate() ValidationErrors {
	errs := ValidationErrors{}
	for _, id := range in.MemberIDs {
		if id <= 0 {
			errs["member_ids"] = "must be team members' ids"
			return errs
		}
	}
	if in.Enabled && len(in.MemberIDs) == 0 {
		errs["member_ids"] = "choose at least one person to take turns"
	}
	return errs
}
