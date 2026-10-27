package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"crm-lead-management/backend/internal/model"
)

// importDealValue is a deal value no other test uses, so the rule below only
// ever matches the leads this test imports.
const importDealValue = 987_654_322

func postCSV(t *testing.T, router *gin.Engine, query, body string) (int, model.ImportResult, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/leads/import?"+query, strings.NewReader(body))
	req.Header.Set(adminKeyHeader, testAdminKey)
	req.Header.Set("Content-Type", "text/csv")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	var envelope struct {
		Data model.ImportResult `json:"data"`
	}
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("decode %q: %v", rec.Body.String(), err)
		}
	}
	return rec.Code, envelope.Data, rec.Body.String()
}

func TestImportAndExportLeads(t *testing.T) {
	router, st := newTestAPI(t)
	ctx := context.Background()
	stamp := time.Now().UnixNano()
	email := func(n int) string { return fmt.Sprintf("import-%d-%d@example.test", stamp, n) }
	company := fmt.Sprintf("Import Test Co %d", stamp)

	enabled := true
	min := int64(importDealValue)
	ruleIn := model.AutomationRuleInput{
		Name: fmt.Sprintf("Import test rule %d", stamp), Enabled: &enabled,
		MinDealValue: &min, TaskTitle: "Call {{first_name}}",
	}
	ruleIn.Normalize()
	rule, err := st.CreateAutomationRule(ctx, ruleIn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.DeleteAutomationRule(ctx, rule.ID) })

	// A lead that is already there, to be reported as a duplicate.
	existingIn := model.LeadInput{Name: "Already Here", Company: company, Email: email(0)}
	existingIn.Normalize()
	existing, err := st.CreateLead(ctx, existingIn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.DeleteLead(ctx, existing.ID) })

	csv := "Name,Company,Email,Deal value\n" +
		fmt.Sprintf("Asha Rao,%s,%s,%d\n", company, email(1), importDealValue) +
		fmt.Sprintf("Vik Iyer,%s,%s,\n", company, strings.ToUpper(email(2))) +
		fmt.Sprintf("Dup Here,%s,%s,\n", company, email(0)) +
		fmt.Sprintf("Twice,%s,%s,\n", company, email(2)) +
		fmt.Sprintf(",%s,broken,\n", company)

	// The dry run saves nothing.
	code, preview, body := postCSV(t, router, "dry_run=true", csv)
	if code != http.StatusOK {
		t.Fatalf("dry run: %d %s", code, body)
	}
	if !preview.DryRun || preview.TotalRows != 5 || preview.Ready != 2 || preview.Imported != 0 ||
		len(preview.Duplicates) != 2 || len(preview.Errors) != 1 {
		t.Fatalf("dry run result = %+v", preview)
	}
	if got := preview.Duplicates[1].Messages[0]; got != "Same email as row 3 of this file" {
		t.Errorf("in-file duplicate message = %q", got)
	}
	if preview.Errors[0].Line != 6 {
		t.Errorf("error line = %d, want 6", preview.Errors[0].Line)
	}
	if found, _ := st.ExistingLeadEmails(ctx, []string{email(1)}); found[email(1)] {
		t.Fatal("the dry run created a lead")
	}

	// The real import, without automations.
	code, result, body := postCSV(t, router, "dry_run=false", csv)
	if code != http.StatusOK {
		t.Fatalf("import: %d %s", code, body)
	}
	leads, _, err := st.ListLeads(ctx, model.LeadQuery{Search: company, Page: 1, PageSize: 10, SortBy: "created_at", SortDir: "asc"})
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range leads {
		id := l.ID
		t.Cleanup(func() { _ = st.DeleteLead(ctx, id) })
	}
	if result.Imported != 2 || len(leads) != 3 {
		t.Fatalf("imported %d, %d leads in the company; result %+v", result.Imported, len(leads), result)
	}
	var asha model.Lead
	for _, l := range leads {
		if l.Email == email(1) {
			asha = l
		}
	}
	if tasks, _ := st.ListTasks(ctx, asha.ID); len(tasks) != 0 {
		t.Errorf("automations ran although not asked to: %+v", tasks)
	}

	// Importing again finds them all as duplicates; with automations on, a
	// new matching lead gets its task.
	again := csv + fmt.Sprintf("Lal,%s,%s,%d\n", company, email(3), importDealValue)
	code, result, body = postCSV(t, router, "run_automations=true", again)
	if code != http.StatusOK || result.Imported != 1 || len(result.Duplicates) != 4 {
		t.Fatalf("second import: %d %+v %s", code, result, body)
	}
	lal, _, _ := st.ListLeads(ctx, model.LeadQuery{Search: company, Page: 1, PageSize: 10, SortBy: "created_at", SortDir: "desc"})
	t.Cleanup(func() { _ = st.DeleteLead(ctx, lal[0].ID) })
	if tasks, _ := st.ListTasks(ctx, lal[0].ID); len(tasks) != 1 || tasks[0].Title != "Call Lal" {
		t.Errorf("automation task on imported lead = %+v", tasks)
	}

	// The failed row keeps its cells, for the file of rows to fix.
	if got := preview.Errors[0].Cells; len(got) != 4 || got[2] != "broken" {
		t.Errorf("error row cells = %q", got)
	}

	// No column for the company: the preview asks for one, the import refuses.
	unmapped := fmt.Sprintf("Client,Firm,Mail\nPia,%s,%s\n", company, email(4))
	code, preview, body = postCSV(t, router, "dry_run=true", unmapped)
	if code != http.StatusOK || preview.Ready != 0 || len(preview.Missing) != 2 || len(preview.Fields) == 0 ||
		preview.Headers[0].Name != "Client" || preview.Headers[0].Field != "" {
		t.Errorf("unmapped dry run: %d %+v %s", code, preview, body)
	}
	if code, _, body := postCSV(t, router, "dry_run=false", unmapped); code != http.StatusUnprocessableEntity ||
		!strings.Contains(body, "Company") {
		t.Errorf("unmapped import: %d %s", code, body)
	}

	// With the columns chosen, it imports.
	columns := url.QueryEscape(`["name","company","email"]`)
	code, result, body = postCSV(t, router, "columns="+columns, unmapped)
	if code != http.StatusOK || result.Imported != 1 {
		t.Fatalf("mapped import: %d %+v %s", code, result, body)
	}
	pia, _, _ := st.ListLeads(ctx, model.LeadQuery{Search: company, Page: 1, PageSize: 10, SortBy: "created_at", SortDir: "desc"})
	t.Cleanup(func() { _ = st.DeleteLead(ctx, pia[0].ID) })
	if pia[0].Name != "Pia" {
		t.Errorf("newest lead = %q, want Pia", pia[0].Name)
	}
	if code, _, body := postCSV(t, router, "columns=nope", unmapped); code != http.StatusUnprocessableEntity {
		t.Errorf("bad columns param: %d %s", code, body)
	}

	// Export honours the list filters.
	req := httptest.NewRequest(http.MethodGet, "/api/leads/export?search="+url.QueryEscape(company), nil)
	req.Header.Set(adminKeyHeader, testAdminKey)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/csv") ||
		!strings.Contains(rec.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("export: %d %v", rec.Code, rec.Header())
	}
	lines := strings.Split(strings.TrimSpace(rec.Body.String()), "\n")
	if len(lines) != 6 { // header + 5 leads
		t.Errorf("export has %d lines, want 6:\n%s", len(lines), rec.Body.String())
	}
}
