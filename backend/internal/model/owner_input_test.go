package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOwnerInputNeedsTheKey(t *testing.T) {
	cases := []struct {
		body    string
		wantErr bool
		wantID  *int64
	}{
		{`{"owner_id": 3}`, false, ptr(int64(3))},
		{`{"owner_id": null}`, false, nil}, // explicit null: unassign
		{`{}`, true, nil},                  // missing: rejected, not read as unassign
		{`{"owner_id": 0}`, true, nil},
		{`{"owner_id": -2}`, true, nil},
	}
	for _, tc := range cases {
		var in OwnerInput
		if err := json.Unmarshal([]byte(tc.body), &in); err != nil {
			t.Fatalf("%s: unmarshal: %v", tc.body, err)
		}
		errs := in.Validate()
		if errs.Any() != tc.wantErr {
			t.Errorf("%s: errors = %v, want error %v", tc.body, errs, tc.wantErr)
		}
		if !tc.wantErr && (tc.wantID == nil) != (in.OwnerID == nil) {
			t.Errorf("%s: owner = %v, want %v", tc.body, in.OwnerID, tc.wantID)
		}
		if !tc.wantErr && tc.wantID != nil && *in.OwnerID != *tc.wantID {
			t.Errorf("%s: owner = %d, want %d", tc.body, *in.OwnerID, *tc.wantID)
		}
	}

	var in OwnerInput
	if err := json.Unmarshal([]byte(`{"owner_id": "three"}`), &in); err == nil {
		t.Errorf(`{"owner_id": "three"}: want an unmarshal error`)
	}
}

func TestNotesAndActivityUpdateInputs(t *testing.T) {
	notes := NotesInput{Notes: "  \n "}
	notes.Normalize()
	if notes.Notes != "" || notes.Validate().Any() {
		t.Errorf("blank notes should clear without error, got %q %v", notes.Notes, notes.Validate())
	}
	if (NotesInput{Notes: strings.Repeat("न", 5001)}).Validate()["notes"] == "" {
		t.Errorf("5001-character notes accepted")
	}

	edit := ActivityUpdateInput{Note: "   "}
	edit.Normalize()
	if edit.Validate()["note"] != "is required" {
		t.Errorf("blank edit accepted")
	}
	if (ActivityUpdateInput{Note: strings.Repeat("न", 2000)}).Validate().Any() {
		t.Errorf("2000-character Hindi note rejected")
	}
}
