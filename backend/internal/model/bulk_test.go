package model

import (
	"slices"
	"testing"
)

func TestBulkInputValidate(t *testing.T) {
	in := BulkInput{IDs: []int64{3, 1, 3}, Action: " Stage ", Stage: "won"}
	in.Normalize()
	if errs := in.Validate(); errs.Any() {
		t.Fatalf("valid input rejected: %v", errs)
	}
	if !slices.Equal(in.IDs, []int64{3, 1}) || in.Action != BulkStage {
		t.Errorf("normalized = %+v", in)
	}

	many := make([]int64, MaxBulkLeads+1)
	for i := range many {
		many[i] = int64(i + 1)
	}
	zero := int64(0)
	cases := []struct {
		in    BulkInput
		field string
	}{
		{BulkInput{Action: BulkDelete}, "ids"},
		{BulkInput{IDs: many, Action: BulkDelete}, "ids"},
		{BulkInput{IDs: []int64{-1}, Action: BulkDelete}, "ids"},
		{BulkInput{IDs: []int64{1}, Action: "archive"}, "action"},
		{BulkInput{IDs: []int64{1}, Action: BulkOwner, OwnerID: &zero}, "owner_id"},
		{BulkInput{IDs: []int64{1}, Action: BulkStage}, "stage"},
		{BulkInput{IDs: []int64{1}, Action: BulkStage, Stage: "won", CloseInput: CloseInput{Reason: ptr(CloseReason("competitor"))}}, "close_reason"},
		{BulkInput{IDs: []int64{1}, Action: BulkSource, Source: "fax"}, "source"},
	}
	for _, tc := range cases {
		tc.in.Normalize()
		if _, ok := tc.in.Validate()[tc.field]; !ok {
			t.Errorf("%+v: want an error on %s, got %v", tc.in, tc.field, tc.in.Validate())
		}
	}

	// Unowned is a valid owner change.
	unown := BulkInput{IDs: []int64{1}, Action: BulkOwner}
	if errs := unown.Validate(); errs.Any() {
		t.Errorf("owner_id null rejected: %v", errs)
	}
}
