package model

import "testing"

// These tests cover the rules that decide whether a request is accepted at all,
// which is the logic most worth pinning down: it has no dependencies, so it runs
// in milliseconds with no database, and a regression here is a data-integrity bug.
//
// Run them with:  go test ./...

func TestStageValid(t *testing.T) {
	// A table-driven test: one slice of cases, one loop. This is the standard Go
	// pattern -- adding a case is one line, and t.Run gives each its own name in
	// the output so a failure says exactly which input broke.
	cases := []struct {
		name  string
		stage Stage
		want  bool
	}{
		{"first lifecycle stage", StageNew, true},
		{"multi-word stage", StageProposalSent, true},
		{"terminal stage", StageLost, true},
		{"empty", Stage(""), false},
		{"unknown value", Stage("bananas"), false},
		{"display label is not a valid value", Stage("Proposal Sent"), false},
		{"wrong case", Stage("NEW"), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.stage.Valid(); got != tc.want {
				t.Errorf("Stage(%q).Valid() = %v, want %v", tc.stage, got, tc.want)
			}
		})
	}
}

func TestStageIsClosed(t *testing.T) {
	closed := []Stage{StageWon, StageLost}
	open := []Stage{StageNew, StageContacted, StageQualified, StageProposalSent, StageNegotiation}

	for _, s := range closed {
		if !s.IsClosed() {
			t.Errorf("Stage(%q).IsClosed() = false, want true", s)
		}
	}
	for _, s := range open {
		if s.IsClosed() {
			t.Errorf("Stage(%q).IsClosed() = true, want false", s)
		}
	}
}

// The Postgres enum in schema.sql is declared in this same order, and the API
// relies on the two agreeing -- ORDER BY stage in SQL must mean the same thing as
// the order of the bars on the dashboard.
func TestStagesAreInLifecycleOrder(t *testing.T) {
	want := []Stage{
		StageNew, StageContacted, StageQualified,
		StageProposalSent, StageNegotiation, StageWon, StageLost,
	}

	if len(Stages) != len(want) {
		t.Fatalf("len(Stages) = %d, want %d", len(Stages), len(want))
	}
	for i := range want {
		if Stages[i] != want[i] {
			t.Errorf("Stages[%d] = %q, want %q", i, Stages[i], want[i])
		}
	}
}

func TestLeadInputNormalize(t *testing.T) {
	in := LeadInput{
		Name:    "  Ada Lovelace  ",
		Company: "  Analytical Engines\t",
		Email:   "  ADA@Example.COM ",
		Phone:   " +91 90000 00000 ",
		Notes:   "  first contact  ",
		// Source and Stage left empty on purpose: both should be defaulted.
	}

	in.Normalize()

	if in.Name != "Ada Lovelace" {
		t.Errorf("Name = %q, want %q", in.Name, "Ada Lovelace")
	}
	if in.Company != "Analytical Engines" {
		t.Errorf("Company = %q, want %q", in.Company, "Analytical Engines")
	}
	// Lower-casing matters: the unique index is on lower(email), so storing
	// mixed case would let "ADA@..." and "ada@..." look like different leads to
	// the app while colliding in the database.
	if in.Email != "ada@example.com" {
		t.Errorf("Email = %q, want %q", in.Email, "ada@example.com")
	}
	if in.Notes != "first contact" {
		t.Errorf("Notes = %q, want %q", in.Notes, "first contact")
	}
	if in.Source != SourceOther {
		t.Errorf("Source = %q, want default %q", in.Source, SourceOther)
	}
	if in.Stage != StageNew {
		t.Errorf("Stage = %q, want default %q", in.Stage, StageNew)
	}
}

func TestLeadInputValidate(t *testing.T) {
	// valid returns a known-good input that each case then breaks in one specific
	// way, so a failure points at exactly one rule.
	valid := func() LeadInput {
		return LeadInput{
			Name:    "Ada Lovelace",
			Company: "Analytical Engines",
			Email:   "ada@example.com",
			Phone:   "+91 90000 00000",
			Source:  SourceReferral,
			Stage:   StageQualified,
		}
	}

	cases := []struct {
		name      string
		mutate    func(*LeadInput)
		wantField string // "" means "expect no errors"
	}{
		{"fully valid", func(*LeadInput) {}, ""},
		{"phone omitted is allowed", func(in *LeadInput) { in.Phone = "" }, ""},
		{"missing name", func(in *LeadInput) { in.Name = "" }, "name"},
		{"over-long name", func(in *LeadInput) { in.Name = repeat("a", 121) }, "name"},
		{"missing company", func(in *LeadInput) { in.Company = "" }, "company"},
		{"missing email", func(in *LeadInput) { in.Email = "" }, "email"},
		{"email without @", func(in *LeadInput) { in.Email = "ada.example.com" }, "email"},
		{"email without domain dot", func(in *LeadInput) { in.Email = "ada@example" }, "email"},
		{"email with space", func(in *LeadInput) { in.Email = "ada lovelace@example.com" }, "email"},
		{"phone with letters", func(in *LeadInput) { in.Phone = "call me" }, "phone"},
		{"phone too short", func(in *LeadInput) { in.Phone = "12" }, "phone"},
		{"unknown source", func(in *LeadInput) { in.Source = Source("carrier-pigeon") }, "source"},
		{"unknown stage", func(in *LeadInput) { in.Stage = Stage("maybe") }, "stage"},
		{"over-long notes", func(in *LeadInput) { in.Notes = repeat("x", 5001) }, "notes"},
		// Postgres counts characters: 120 Devanagari letters (360 bytes) fit.
		{"120-character Hindi name", func(in *LeadInput) { in.Name = repeat("क", 120) }, ""},
		{"121-character Hindi name", func(in *LeadInput) { in.Name = repeat("क", 121) }, "name"},
		{"5000-character Hindi notes", func(in *LeadInput) { in.Notes = repeat("न", 5000) }, ""},
		// Unicode spaces the database's [:space:] rejects must be caught here.
		{"email with no-break space", func(in *LeadInput) { in.Email = "ada lovelace@example.com" }, "email"},
		{"email with narrow no-break space", func(in *LeadInput) { in.Email = "ada@exa mple.com" }, "email"},
		{"email with ideographic space", func(in *LeadInput) { in.Email = "ada@example.　com" }, "email"},
		{"email with next-line char", func(in *LeadInput) { in.Email = "ada\u0085@example.com" }, "email"},
		{"email with vertical tab", func(in *LeadInput) { in.Email = "ada\v@example.com" }, "email"},
		{"email with non-ASCII letters is fine", func(in *LeadInput) { in.Email = "आदा@example.com" }, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := valid()
			tc.mutate(&in)

			errs := in.Validate()

			if tc.wantField == "" {
				if errs.Any() {
					t.Fatalf("expected no validation errors, got %v", errs)
				}
				return
			}

			if _, ok := errs[tc.wantField]; !ok {
				t.Fatalf("expected an error on field %q, got %v", tc.wantField, errs)
			}
		})
	}
}

func TestActivityInputValidate(t *testing.T) {
	cases := []struct {
		name      string
		in        ActivityInput
		wantField string
	}{
		{"plain note", ActivityInput{Type: ActivityNote, Note: "Called, no answer."}, ""},
		{"call", ActivityInput{Type: ActivityCall, Note: "Spoke for 10 minutes."}, ""},
		{"empty note", ActivityInput{Type: ActivityNote, Note: ""}, "note"},
		{"over-long note", ActivityInput{Type: ActivityNote, Note: repeat("x", 2001)}, "note"},
		{"unknown type", ActivityInput{Type: ActivityType("smoke-signal"), Note: "hi"}, "type"},

		// The two system-written types must be rejected from client input,
		// otherwise the timeline could be forged and stops being an audit trail.
		{"created is server-only", ActivityInput{Type: ActivityCreated, Note: "hi"}, "type"},
		{"stage_change is server-only", ActivityInput{Type: ActivityStageChange, Note: "hi"}, "type"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := tc.in.Validate()

			if tc.wantField == "" {
				if errs.Any() {
					t.Fatalf("expected no validation errors, got %v", errs)
				}
				return
			}
			if _, ok := errs[tc.wantField]; !ok {
				t.Fatalf("expected an error on field %q, got %v", tc.wantField, errs)
			}
		})
	}
}

func TestActivityInputNormalizeDefaultsToNote(t *testing.T) {
	in := ActivityInput{Note: "  wrote in  "}
	in.Normalize()

	if in.Type != ActivityNote {
		t.Errorf("Type = %q, want default %q", in.Type, ActivityNote)
	}
	if in.Note != "wrote in" {
		t.Errorf("Note = %q, want %q", in.Note, "wrote in")
	}
}

func TestPhonePtr(t *testing.T) {
	// An omitted phone must become SQL NULL, not an empty string, so "unknown"
	// and "blank" do not become two different states in the database.
	if got := (LeadInput{Phone: ""}).PhonePtr(); got != nil {
		t.Errorf("PhonePtr() on empty phone = %q, want nil", *got)
	}

	got := (LeadInput{Phone: "+91 90000 00000"}).PhonePtr()
	if got == nil {
		t.Fatal("PhonePtr() on a real phone = nil, want a pointer")
	}
	if *got != "+91 90000 00000" {
		t.Errorf("*PhonePtr() = %q, want %q", *got, "+91 90000 00000")
	}
}

func TestNewPageMeta(t *testing.T) {
	cases := []struct {
		name           string
		page, size     int
		total          int
		wantTotalPages int
	}{
		{"exact fit", 1, 10, 20, 2},
		{"partial last page", 1, 10, 14, 2}, // ceiling division, not 1
		{"single item", 1, 10, 1, 1},
		{"empty result", 1, 10, 0, 0},
		{"one big page", 2, 100, 101, 2},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			meta := NewPageMeta(LeadQuery{Page: tc.page, PageSize: tc.size}, tc.total)

			if meta.TotalPages != tc.wantTotalPages {
				t.Errorf("TotalPages = %d, want %d", meta.TotalPages, tc.wantTotalPages)
			}
			if meta.TotalItems != tc.total {
				t.Errorf("TotalItems = %d, want %d", meta.TotalItems, tc.total)
			}
		})
	}
}

func TestLeadQueryOffset(t *testing.T) {
	cases := []struct {
		page, size, want int
	}{
		{1, 10, 0}, // first page starts at the beginning
		{2, 10, 10},
		{3, 25, 50},
	}

	for _, tc := range cases {
		q := LeadQuery{Page: tc.page, PageSize: tc.size}
		if got := q.Offset(); got != tc.want {
			t.Errorf("page %d size %d: Offset() = %d, want %d", tc.page, tc.size, got, tc.want)
		}
	}
}

// repeat builds a string of n copies of s, for length-limit tests.
func repeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for range n {
		out = append(out, s...)
	}
	return string(out)
}
