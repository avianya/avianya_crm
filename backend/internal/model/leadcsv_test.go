package model

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

var csvTestTeam = []TeamMember{
	{ID: 1, Name: "Ravi Kumar", Email: "ravi@example.test"},
	{ID: 2, Name: "Meera Shah", Email: "meera@example.test"},
	{ID: 3, Name: "Meera  Shah", Email: "meera.s@example.test"}, // same name, spaced differently
}

func parseCSV(t *testing.T, text string) LeadCSV {
	t.Helper()
	parsed, err := ParseLeadCSV([]byte(text), csvTestTeam, nil)
	if err != nil {
		t.Fatalf("ParseLeadCSV: %v", err)
	}
	return parsed
}

func TestParseLeadCSVReadsLooseHeadersAndValues(t *testing.T) {
	text := "\xef\xbb\xbfFull Name,Company Name,E-mail,Mobile,Lead Source,Status,Deal value (₹),Assigned to,Favourite colour\n" +
		"Asha Rao,Kestrel,ASHA@Kestrel.in,'+91 90000 00000,Cold Call,Proposal Sent,\"₹2,50,000\",ravi@example.test,blue\n" +
		",,,,,,,,\n" + // blank rows are skipped
		"Vik Iyer,Iyer & Co,vik@iyer.test,,linkedin,qualified,Rs. 1000.00,Ravi Kumar,\n"

	parsed := parseCSV(t, text)
	wantFields := []string{"name", "company", "email", "phone", "source", "stage", "deal_value", "owner", ""}
	if got := headerFields(parsed); !slices.Equal(got, wantFields) {
		t.Errorf("fields = %q, want %q", got, wantFields)
	}
	if len(parsed.Rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(parsed.Rows))
	}

	asha := parsed.Rows[0]
	if asha.Errors != nil {
		t.Fatalf("row 2 errors: %v", asha.Errors)
	}
	in := asha.Input
	if asha.Line != 2 || in.Email != "asha@kestrel.in" || in.Phone != "+91 90000 00000" ||
		in.Source != SourceColdCall || in.Stage != StageProposalSent {
		t.Errorf("row 2 = line %d, %+v", asha.Line, in)
	}
	if in.DealValue == nil || *in.DealValue != 250_000 {
		t.Errorf("deal value = %v, want 250000", in.DealValue)
	}
	if in.OwnerID == nil || *in.OwnerID != 1 {
		t.Errorf("owner = %v, want 1 (by email)", in.OwnerID)
	}

	vik := parsed.Rows[1]
	if vik.Errors != nil {
		t.Fatalf("row 4 errors: %v", vik.Errors)
	}
	if vik.Line != 4 || vik.Input.Source != SourceLinkedIn || vik.Input.Stage != StageQualified ||
		*vik.Input.DealValue != 1000 || *vik.Input.OwnerID != 1 {
		t.Errorf("row 4 = line %d, %+v", vik.Line, vik.Input)
	}
}

func TestParseLeadCSVDefaultsAndSemicolons(t *testing.T) {
	parsed := parseCSV(t, "name;company;email\nAsha;Kestrel;asha@kestrel.in\n")
	in := parsed.Rows[0].Input
	if parsed.Rows[0].Errors != nil || in.Source != SourceOther || in.Stage != StageNew || in.DealValue != nil || in.OwnerID != nil {
		t.Errorf("row = %+v, errors %v", in, parsed.Rows[0].Errors)
	}
}

func TestParseLeadCSVRowErrors(t *testing.T) {
	text := "Name,Company,Email,Source,Stage,Deal value,Owner,Close reason\n" +
		",Kestrel,not-an-email,fax,,12.5,Nobody,\n" +
		"Asha,Kestrel,asha@kestrel.in,,Lost,,Meera Shah,\n" +
		"Vik,Iyer,vik@iyer.test,,New,,,price\n" +
		"Lal,Lal Co,lal@lal.test,,Lost,,,went with a competitor\n"

	parsed := parseCSV(t, text)
	wantErrors := [][]string{
		{
			`Name: is required`,
			`Email: must be a valid email address`,
			`Source: "fax" is not a source (use Website, Referral, LinkedIn, Cold Call, Meta Ads, Google Ads, Other)`,
			`Deal value: "12.5" is not an amount in whole rupees`,
			`Owner: "Nobody" is not on the Team page; add them first, or leave it empty`,
		},
		{
			`Owner: "Meera Shah" matches more than one team member; use their email instead`,
			`Close reason: is required for a Lost lead`,
		},
		{`Close reason: only goes with a Won or Lost lead`},
		nil,
	}
	for i, want := range wantErrors {
		if got := parsed.Rows[i].Errors; !slices.Equal(got, want) {
			t.Errorf("row %d errors =\n  %q\nwant\n  %q", parsed.Rows[i].Line, got, want)
		}
	}
	if r := parsed.Rows[3].Input.CloseInput.Reason; r == nil || *r != "competitor" {
		t.Errorf("close reason by label = %v, want competitor", r)
	}
}

func TestParseLeadCSVFileErrors(t *testing.T) {
	cases := map[string]string{
		"":                                "empty",
		"Name,Company,Email\n":            "no leads",
		"Name,Company,Email\n\"a,b,c\n\n": "", // lazy quotes: read, not refused
	}
	for text, want := range cases {
		_, err := ParseLeadCSV([]byte(text), nil, nil)
		if want == "" {
			continue
		}
		if !errors.Is(err, ErrImportFile) || !strings.Contains(err.Error(), want) {
			t.Errorf("ParseLeadCSV(%q) err = %v, want an import file error with %q", text, err, want)
		}
	}

	var big strings.Builder
	big.WriteString("Name,Company,Email\n")
	for range MaxImportRows + 1 {
		big.WriteString("a,b,c@d.test\n")
	}
	if _, err := ParseLeadCSV([]byte(big.String()), nil, nil); err == nil || !strings.Contains(err.Error(), "more than") {
		t.Errorf("too many rows: err = %v", err)
	}
}

func TestParseRupees(t *testing.T) {
	good := map[string]int64{"0": 0, "250000": 250_000, "2,50,000": 250_000, "₹ 2,50,000": 250_000,
		"INR 5000": 5000, "rs.1,000.00": 1000, "1e6": 1_000_000}
	for raw, want := range good {
		if got, ok := parseRupees(raw); !ok || got != want {
			t.Errorf("parseRupees(%q) = %d, %v; want %d", raw, got, ok, want)
		}
	}
	for _, raw := range []string{"", "lots", "12.5", "₹", "1e30"} {
		if _, ok := parseRupees(raw); ok {
			t.Errorf("parseRupees(%q) accepted", raw)
		}
	}
}

func TestLeadsCSVRoundTrip(t *testing.T) {
	phone, note := "+91 90000 00000", "Chose Acme"
	value := int64(250_000)
	ownerID := int64(1)
	lost := CloseReason("competitor")
	leads := []Lead{
		{
			Name: "Asha Rao", Company: "=HYPERLINK(\"x\")", Email: "asha@kestrel.in", Phone: &phone,
			Source: SourceColdCall, Stage: StageLost, DealValue: &value, OwnerID: &ownerID,
			CloseReason: &lost, CloseNote: &note, Notes: "Line one\nline two, with a comma",
			CreatedAt: time.Date(2026, 10, 22, 5, 30, 0, 0, time.UTC),
		},
		{Name: "Vik", Company: "Iyer", Email: "vik@iyer.test", Source: SourceWebsite, Stage: StageNew,
			Score: &LeadScore{Band: BandHot}},
	}

	var buf bytes.Buffer
	owners := map[int64]TeamMember{1: csvTestTeam[0]}
	ist := time.FixedZone("IST", 5*3600+1800)
	if err := WriteLeadsCSV(&buf, leads, owners, ist); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.HasPrefix(out, "\xef\xbb\xbfName,Company,Email,") {
		t.Errorf("export does not start with a BOM and the header: %q", out[:40])
	}
	for _, want := range []string{`"'=HYPERLINK(""x"")"`, "'+91 90000 00000", "Cold Call", "Went with a competitor",
		"ravi@example.test", "2026-10-22 11:00", ",Hot,"} {
		if !strings.Contains(out, want) {
			t.Errorf("export lacks %q:\n%s", want, out)
		}
	}

	parsed := parseCSV(t, out)
	if len(parsed.Rows) != 2 || parsed.Rows[0].Errors != nil || parsed.Rows[1].Errors != nil {
		t.Fatalf("re-import: %+v", parsed.Rows)
	}
	if got := headerFields(parsed); got[12] != "" || got[13] != "" || len(parsed.Missing) != 0 {
		t.Errorf("re-import fields = %q, missing %q; want Score band and Created not imported", got, parsed.Missing)
	}
	in := parsed.Rows[0].Input
	if in.Company != `=HYPERLINK("x")` || in.Phone != phone || in.Stage != StageLost || *in.CloseInput.Reason != lost ||
		in.CloseInput.Note != note || in.Notes != leads[0].Notes || *in.OwnerID != 1 || *in.DealValue != value {
		t.Errorf("re-imported lead = %+v", in)
	}
}

func headerFields(parsed LeadCSV) []string {
	fields := make([]string, len(parsed.Headers))
	for i, h := range parsed.Headers {
		fields[i] = h.Field
	}
	return fields
}

func TestParseLeadCSVMissingAndRepeatedColumns(t *testing.T) {
	// No column is recognisably the company: the rows are kept unchecked and
	// the field is reported missing, for the column picker to fix.
	parsed := parseCSV(t, "Name,Firm,Email,Email\nAsha,Kestrel,asha@kestrel.in,other@x.test\n")
	if !slices.Equal(parsed.Missing, []string{"Company"}) {
		t.Errorf("missing = %q, want [Company]", parsed.Missing)
	}
	if got := headerFields(parsed); !slices.Equal(got, []string{"name", "", "email", ""}) {
		t.Errorf("fields = %q; the second Email column should not be imported", got)
	}
	if len(parsed.Rows) != 1 || parsed.Rows[0].Errors != nil || parsed.Rows[0].Cells[1] != "Kestrel" {
		t.Errorf("rows = %+v, want one unchecked row with its cells", parsed.Rows)
	}
}

func TestParseLeadCSVWithChosenColumns(t *testing.T) {
	text := "Client,Firm,Mail,Email\nAsha Rao,Kestrel,asha@kestrel.in,wrong@x.test\n"
	parsed, err := ParseLeadCSV([]byte(text), nil, []string{"name", "company", "email", ""})
	if err != nil {
		t.Fatal(err)
	}
	row := parsed.Rows[0]
	if len(parsed.Missing) != 0 || row.Errors != nil || row.Input.Company != "Kestrel" || row.Input.Email != "asha@kestrel.in" {
		t.Errorf("parsed = %+v", parsed)
	}
	if !slices.Equal(row.Cells, []string{"Asha Rao", "Kestrel", "asha@kestrel.in", "wrong@x.test"}) {
		t.Errorf("cells = %q", row.Cells)
	}

	for want, mapping := range map[string][]string{
		"do not match": {"name", "company"},
		"not a field":  {"name", "company", "email", "colour"},
		"two columns":  {"name", "company", "email", "email"},
	} {
		if _, err := ParseLeadCSV([]byte(text), nil, mapping); !errors.Is(err, ErrImportFile) || !strings.Contains(err.Error(), want) {
			t.Errorf("mapping %q: err = %v, want an import file error with %q", mapping, err, want)
		}
	}
}
