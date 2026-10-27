package gsheets

import (
	"errors"
	"reflect"
	"testing"
)

var testColumns = []string{"lead_id", "name", "email"}

func row(id int64, values ...string) Row {
	return Row{LeadID: id, Values: values}
}

func TestMakePlanEmptyTabAppendsEverything(t *testing.T) {
	plan, err := MakePlan(nil, nil, true, testColumns, []Row{row(1, "1", "Ada", "a@x.io"), row(2, "2", "Bo", "b@x.io")})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"CRM Lead ID", "Name", "Email"}; !reflect.DeepEqual(plan.Header, want) {
		t.Errorf("header = %v, want %v", plan.Header, want)
	}
	if len(plan.Updates) != 0 || len(plan.Appends) != 2 {
		t.Errorf("updates %d, appends %d; want 0, 2", len(plan.Updates), len(plan.Appends))
	}
}

func TestMakePlanUpdatesExistingRowsInsteadOfDuplicating(t *testing.T) {
	header := []string{"CRM Lead ID", "Name", "Email"}
	colA := []string{"CRM Lead ID", "7", "", "my own note", "3"}
	plan, err := MakePlan(header, colA, false, testColumns, []Row{
		row(3, "3", "Cy", "c@x.io"), row(7, "7", "Di", "d@x.io"), row(9, "9", "Ed", "e@x.io"),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []RowWrite{{Row: 5, Values: []string{"3", "Cy", "c@x.io"}}, {Row: 2, Values: []string{"7", "Di", "d@x.io"}}}
	if !reflect.DeepEqual(plan.Updates, want) {
		t.Errorf("updates = %v, want %v", plan.Updates, want)
	}
	if len(plan.Appends) != 1 || plan.Appends[0][0] != "9" {
		t.Errorf("appends = %v, want only lead 9", plan.Appends)
	}
}

func TestMakePlanRefusesATabWithOtherData(t *testing.T) {
	cases := map[string]struct {
		header, colA []string
		empty        bool
	}{
		"someone else's header": {[]string{"Customer", "Phone"}, []string{"Customer", "Asha"}, false},
		"data lower down":       {nil, nil, false},
		"column A only":         {nil, []string{"", "x"}, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := MakePlan(tc.header, tc.colA, tc.empty, testColumns, []Row{row(1, "1", "A", "a")}); !errors.Is(err, ErrTabInUse) {
				t.Errorf("err = %v, want ErrTabInUse", err)
			}
		})
	}
}

func TestMakePlanBlanksColumnsNoLongerChosen(t *testing.T) {
	// Last time: id, name, email, phone. Now: id, name. The two CRM columns
	// dropped are blanked; the admin's own "Comments" column is left alone.
	header := []string{"CRM Lead ID", "Name", "Email", "Phone", "Comments"}
	colA := []string{"CRM Lead ID", "4"}
	plan, err := MakePlan(header, colA, false, []string{"lead_id", "name"}, []Row{row(4, "4", "Fay")})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"CRM Lead ID", "Name", "", ""}; !reflect.DeepEqual(plan.Header, want) {
		t.Errorf("header = %q, want %q", plan.Header, want)
	}
	if want := []string{"4", "Fay", "", ""}; !reflect.DeepEqual(plan.Updates[0].Values, want) {
		t.Errorf("row = %q, want %q", plan.Updates[0].Values, want)
	}
}

func TestMakePlanReportsDuplicateIDs(t *testing.T) {
	colA := []string{"CRM Lead ID", "5", "5"}
	plan, err := MakePlan([]string{"CRM Lead ID"}, colA, false, testColumns, []Row{row(5, "5", "G", "g")})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Updates) != 1 || plan.Updates[0].Row != 2 || !reflect.DeepEqual(plan.DuplicateIDs, []int64{5}) {
		t.Errorf("plan = %+v; want row 2 updated and 5 reported", plan)
	}
}

func TestColumnName(t *testing.T) {
	for n, want := range map[int]string{1: "A", 13: "M", 26: "Z", 27: "AA", 52: "AZ", 53: "BA"} {
		if got := ColumnName(n); got != want {
			t.Errorf("ColumnName(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestA1QuotesTabNames(t *testing.T) {
	if got := A1("Bob's leads", "A1"); got != "'Bob''s leads'!A1" {
		t.Errorf("A1 = %q", got)
	}
}
