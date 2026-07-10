package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"google.golang.org/api/googleapi"
	"google.golang.org/api/sheets/v4"
)

func TestWorkbookTabsIncludeImportGateSheets(t *testing.T) {
	cfg := config{
		SpreadsheetID: defaultSpreadsheetID,
		RunType:       defaultRunType,
		BusinessDate:  "2026-07-09",
		Actor:         "test",
	}
	tabs := workbookTabs(cfg, "run-test", sourceCatalogRows(), validationRuleRows(), issueSeedRows())
	names := map[string]tabSpec{}
	for _, tab := range tabs {
		names[tab.Name] = tab
	}
	for _, name := range []string{
		"README_Problem_Statement",
		"Source_Catalog",
		"Sync_Runs",
		"Raw_Source_Snapshots",
		"Mapping_Crosswalks",
		"Animal_Master",
		"Animal_Identifier_History",
		"Validation_Rules",
		"Issue_Queue",
		"Vaccination_Due_View",
		"Import_Batches",
	} {
		if _, ok := names[name]; !ok {
			t.Fatalf("missing tab %s", name)
		}
	}
	master := names["Animal_Master"]
	for _, col := range []string{"animal_identifier_1", "species", "sex", "dob", "current_shed", "ready_for_goat_os_import"} {
		if !contains(master.Cols, col) {
			t.Fatalf("Animal_Master missing column %s", col)
		}
	}
}

func TestRFIDGateRulesStayPinned(t *testing.T) {
	rules := validationRuleRows()
	byID := map[string]validationRuleRow{}
	for _, rule := range rules {
		byID[rule.RuleID] = rule
	}
	rule, ok := byID["animal_identifier_1_present"]
	if !ok {
		t.Fatal("animal_identifier_1_present missing")
	}
	text := strings.ToLower(rule.Description + " " + rule.FailureMessage)
	for _, want := range []string{"rfid", "old-tag", "new-tag", "dst_tag"} {
		if !strings.Contains(text, want) {
			t.Fatalf("animal_identifier_1_present should mention %s; got %q", want, text)
		}
	}
	if strings.Contains(text, "goat_id satisfies") || strings.Contains(text, "farm_goat_id satisfies") {
		t.Fatalf("legacy source ids must not satisfy animal_identifier_1: %q", text)
	}

	issues := issueSeedRows()
	var found bool
	for _, issue := range issues {
		if issue.IssueID == "seed-drive-rfid-access" {
			found = true
			if !strings.Contains(strings.ToLower(issue.Evidence), "drive/sheets") {
				t.Fatalf("drive RFID issue should state Drive/Sheets blocker: %+v", issue)
			}
		}
	}
	if !found {
		t.Fatal("seed-drive-rfid-access issue missing")
	}
}

func TestSourceCatalogHasDriveGatedRFIDSources(t *testing.T) {
	rows := sourceCatalogRows()
	byID := map[string]sourceCatalogRow{}
	for _, row := range rows {
		byID[row.SourceID] = row
	}
	for _, id := range []string{"goats_db_rfid_mapping", "cpt_rfid_beetal"} {
		row, ok := byID[id]
		if !ok {
			t.Fatalf("missing source %s", id)
		}
		if row.Extractor != "drive+sheets" {
			t.Fatalf("%s extractor=%q, want drive+sheets", id, row.Extractor)
		}
		if !strings.Contains(strings.ToLower(row.Notes), "drive") {
			t.Fatalf("%s notes should mention Drive gate: %q", id, row.Notes)
		}
	}
	if row, ok := byID["shiftings_reports_clean"]; !ok {
		t.Fatal("missing shiftings_reports_clean source")
	} else if strings.Contains(strings.ToLower(row.Notes), "rfid") {
		t.Fatalf("shiftings dst_tag source should not be described as RFID: %q", row.Notes)
	}
}

func TestDryRunSummaryJSON(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"--json", "--business-date=2026-07-09"}, &out); err != nil {
		t.Fatal(err)
	}
	var summary syncSummary
	if err := json.Unmarshal(out.Bytes(), &summary); err != nil {
		t.Fatalf("decode summary: %v\n%s", err, out.String())
	}
	if summary.Apply {
		t.Fatal("dry-run should not apply")
	}
	if summary.SpreadsheetID != defaultSpreadsheetID {
		t.Fatalf("spreadsheet=%q", summary.SpreadsheetID)
	}
	if summary.Tabs < 29 {
		t.Fatalf("tabs=%d, want all workbook tabs", summary.Tabs)
	}
	if summary.SourceCatalogRows < 40 {
		t.Fatalf("source catalog rows=%d, want broad source coverage", summary.SourceCatalogRows)
	}
	if summary.ValidationRules < 17 {
		t.Fatalf("validation rules=%d, want seeded rules", summary.ValidationRules)
	}
	if summary.IssueSeeds < 10 {
		t.Fatalf("issue seeds=%d, want open finding seeds", summary.IssueSeeds)
	}
}

func TestDefaultBusinessDateUsesISTYesterday(t *testing.T) {
	now := time.Date(2026, 7, 10, 2, 0, 0, 0, time.UTC)
	if got := defaultBusinessDate(now); got != "2026-07-09" {
		t.Fatalf("defaultBusinessDate()=%s, want 2026-07-09", got)
	}
}

func TestApplyWorkbookDeletesEmptySheet1AndOrdersManagedTabs(t *testing.T) {
	ctx := context.Background()
	specs := []tabSpec{
		tableSpec("README_Problem_Statement", []string{"section", "content"}, nil),
		tableSpec("Animal_Master", []string{"animal_identifier_1", "verification_status"}, nil),
		tableSpec("Source_Catalog", sourceCatalogColumns(), [][]string{{"src-1"}}),
	}
	fake := newFakeSheets(nil)
	fake.addTab("Sheet1", nil)
	fake.addTab("Source_Catalog", [][]any{stringRow(sourceCatalogColumns())})
	fake.addTab("README_Problem_Statement", [][]any{{"section", "content"}})
	fake.addTab("Animal_Master", [][]any{{"animal_identifier_1", "verification_status"}})

	summary, err := applyWorkbook(ctx, fake, config{SpreadsheetID: "sheet"}, specs)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(summary.DeletedTabs, "Sheet1") {
		t.Fatalf("Sheet1 should be deleted when empty, got %+v", summary)
	}
	if fake.tabs["Sheet1"] {
		t.Fatalf("Sheet1 still present; order=%v", fake.order)
	}
	if got, want := fake.order[:3], []string{"README_Problem_Statement", "Animal_Master", "Source_Catalog"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("managed tabs not ordered: got %v want %v", got, want)
	}
}

func TestApplyWorkbookPreservesHumanTabsAndAppendsSyncRuns(t *testing.T) {
	ctx := context.Background()
	specs := []tabSpec{
		tableSpec("Sync_Runs", syncRunColumns(), [][]string{{"run-2", "manual"}}),
		tableSpec("Animal_Master", []string{"animal_identifier_1", "verification_status"}, nil),
		tableSpec("Source_Catalog", sourceCatalogColumns(), [][]string{{"src-2"}}),
	}
	fake := newFakeSheets(specs)
	fake.values["Sync_Runs"] = [][]any{stringRow(syncRunColumns()), {"run-1", "manual"}}
	fake.values["Animal_Master"] = [][]any{{"animal_identifier_1", "verification_status"}, {"RFID-1", "GREEN"}}
	fake.values["Source_Catalog"] = [][]any{stringRow(sourceCatalogColumns()), {"src-1"}}

	summary, err := applyWorkbook(ctx, fake, config{SpreadsheetID: "sheet"}, specs)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(summary.AppendedRows, "Sync_Runs") {
		t.Fatalf("Sync_Runs should append, got %+v", summary)
	}
	for _, name := range []string{"Animal_Master", "Source_Catalog"} {
		if !contains(summary.SkippedTabs, name) {
			t.Fatalf("%s should be skipped, got %+v", name, summary)
		}
	}
	if fake.countCall("clear:Animal_Master") != 0 || fake.countCall("update:Animal_Master") != 0 {
		t.Fatalf("human tab was modified: %v", fake.calls)
	}
	if fake.countCall("append:Sync_Runs") != 1 {
		t.Fatalf("Sync_Runs append calls=%d calls=%v", fake.countCall("append:Sync_Runs"), fake.calls)
	}
}

func TestReplaceManagedTabsOnlyRefreshesSeedTabs(t *testing.T) {
	ctx := context.Background()
	specs := []tabSpec{
		tableSpec("Sync_Runs", syncRunColumns(), [][]string{{"run-2", "manual"}}),
		tableSpec("Animal_Master", []string{"animal_identifier_1", "verification_status"}, nil),
		tableSpec("Source_Catalog", sourceCatalogColumns(), [][]string{{"src-2"}}),
	}
	fake := newFakeSheets(specs)
	fake.values["Sync_Runs"] = [][]any{stringRow(syncRunColumns()), {"run-1", "manual"}}
	fake.values["Animal_Master"] = [][]any{{"animal_identifier_1", "verification_status"}, {"RFID-1", "GREEN"}}
	fake.values["Source_Catalog"] = [][]any{stringRow(sourceCatalogColumns()), {"src-1"}}

	summary, err := applyWorkbook(ctx, fake, config{SpreadsheetID: "sheet", ReplaceManagedTabs: true}, specs)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(summary.WrittenTabs, "Source_Catalog") {
		t.Fatalf("Source_Catalog should be refreshed, got %+v", summary)
	}
	if !contains(summary.SkippedTabs, "Animal_Master") {
		t.Fatalf("Animal_Master should be preserved, got %+v", summary)
	}
	if !contains(summary.AppendedRows, "Sync_Runs") {
		t.Fatalf("Sync_Runs should append even during seed refresh, got %+v", summary)
	}
	if fake.countCall("clear:Source_Catalog") != 1 || fake.countCall("update:Source_Catalog") != 1 {
		t.Fatalf("seed tab refresh calls wrong: %v", fake.calls)
	}
	if fake.countCall("clear:Animal_Master") != 0 || fake.countCall("update:Animal_Master") != 0 {
		t.Fatalf("human tab was modified: %v", fake.calls)
	}
}

func TestApplyWorkbookRejectsMalformedHumanTab(t *testing.T) {
	ctx := context.Background()
	specs := []tabSpec{tableSpec("Animal_Master", []string{"animal_identifier_1", "verification_status"}, nil)}
	fake := newFakeSheets(specs)
	fake.values["Animal_Master"] = [][]any{{"wrong_header"}, {"RFID-1"}}

	_, err := applyWorkbook(ctx, fake, config{SpreadsheetID: "sheet"}, specs)
	if err == nil || !strings.Contains(err.Error(), "header does not match") {
		t.Fatalf("expected header mismatch error, got %v", err)
	}
	if fake.countCall("clear:Animal_Master") != 0 || fake.countCall("update:Animal_Master") != 0 {
		t.Fatalf("malformed human tab should not be modified: %v", fake.calls)
	}
}

func TestApplyWorkbookCanRefreshMalformedSeedTab(t *testing.T) {
	ctx := context.Background()
	specs := []tabSpec{tableSpec("Source_Catalog", sourceCatalogColumns(), [][]string{{"src-2"}})}
	fake := newFakeSheets(specs)
	fake.values["Source_Catalog"] = [][]any{{"wrong_header"}, {"src-1"}}

	summary, err := applyWorkbook(ctx, fake, config{SpreadsheetID: "sheet", ReplaceManagedTabs: true}, specs)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(summary.WrittenTabs, "Source_Catalog") {
		t.Fatalf("Source_Catalog should be refreshed, got %+v", summary)
	}
	if fake.countCall("clear:Source_Catalog") != 1 || fake.countCall("update:Source_Catalog") != 1 {
		t.Fatalf("seed tab refresh calls wrong: %v", fake.calls)
	}
}

func TestSyncRunSchemaHashUsesWorkbookColumns(t *testing.T) {
	cfg := config{SpreadsheetID: defaultSpreadsheetID, RunType: defaultRunType, BusinessDate: "2026-07-09"}
	specs := workbookTabs(cfg, "run-test", sourceCatalogRows(), validationRuleRows(), issueSeedRows())
	syncRun := findTab(t, specs, "Sync_Runs")
	if len(syncRun.Rows) != 1 || len(syncRun.Rows[0]) < 13 {
		t.Fatalf("bad Sync_Runs rows: %+v", syncRun.Rows)
	}
	gotHash := syncRun.Rows[0][12]
	if want := workbookSchemaHash(specs); gotHash != want {
		t.Fatalf("schema hash=%s, want %s", gotHash, want)
	}
	mutated := append([]tabSpec(nil), specs...)
	mutated[0].Cols = append(append([]string(nil), mutated[0].Cols...), "new_column")
	if gotHash == workbookSchemaHash(mutated) {
		t.Fatal("schema hash should change when workbook columns change")
	}
}

func TestValidateGoogleIdentity(t *testing.T) {
	if err := validateGoogleIdentity(googleIdentity{Principal: "ravi@mesha.sg"}, "", "@mesha.sg"); err != nil {
		t.Fatal(err)
	}
	if err := validateGoogleIdentity(googleIdentity{Principal: "writer@goatos-dev.iam.gserviceaccount.com"}, "", "@mesha.sg,@goatos-dev.iam.gserviceaccount.com"); err != nil {
		t.Fatal(err)
	}
	if err := validateGoogleIdentity(googleIdentity{Principal: "ravi@mesha.sg"}, "ravi@mesha.sg", "@wrong"); err != nil {
		t.Fatal(err)
	}
	if err := validateGoogleIdentity(googleIdentity{Principal: "bot@heva.sg"}, "", "@mesha.sg"); err == nil {
		t.Fatal("expected wrong principal suffix to fail")
	}
}

func TestNewRunIDIncludesUniqueSuffix(t *testing.T) {
	first := newRunID()
	second := newRunID()
	if first == second {
		t.Fatalf("run ids collided: %s", first)
	}
	if !strings.HasPrefix(first, "legacy-god-sheet-") || len(strings.Split(first, "-")) < 4 {
		t.Fatalf("unexpected run id format: %s", first)
	}
}

func TestWithSheetsRetryRetriesTransientErrors(t *testing.T) {
	attempts := 0
	got, err := withSheetsRetry(context.Background(), func() (string, error) {
		attempts++
		if attempts < 2 {
			return "", &googleapi.Error{Code: 429, Message: "quota"}
		}
		return "ok", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "ok" || attempts != 2 {
		t.Fatalf("got=%s attempts=%d", got, attempts)
	}

	attempts = 0
	_, err = withSheetsRetry(context.Background(), func() (string, error) {
		attempts++
		return "", errors.New("not retryable")
	})
	if err == nil || attempts != 1 {
		t.Fatalf("expected one non-retryable attempt, err=%v attempts=%d", err, attempts)
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

type fakeSheets struct {
	tabs   map[string]bool
	values map[string][][]any
	ids    map[string]int64
	order  []string
	nextID int64
	calls  []string
}

func newFakeSheets(specs []tabSpec) *fakeSheets {
	fake := &fakeSheets{tabs: map[string]bool{}, values: map[string][][]any{}, ids: map[string]int64{}, nextID: 1}
	for _, spec := range specs {
		fake.addTab(spec.Name, [][]any{})
	}
	return fake
}

func (f *fakeSheets) addTab(name string, values [][]any) {
	if f.tabs[name] {
		f.values[name] = values
		return
	}
	f.tabs[name] = true
	f.values[name] = values
	f.ids[name] = f.nextID
	f.nextID++
	f.order = append(f.order, name)
}

func (f *fakeSheets) GetSpreadsheet(ctx context.Context, spreadsheetID string) (*sheets.Spreadsheet, error) {
	_ = ctx
	_ = spreadsheetID
	f.calls = append(f.calls, "get-spreadsheet")
	resp := &sheets.Spreadsheet{}
	for index, name := range f.order {
		if !f.tabs[name] {
			continue
		}
		resp.Sheets = append(resp.Sheets, &sheets.Sheet{Properties: &sheets.SheetProperties{Title: name, SheetId: f.ids[name], Index: int64(index)}})
	}
	return resp, nil
}

func (f *fakeSheets) BatchUpdateSpreadsheet(ctx context.Context, spreadsheetID string, request *sheets.BatchUpdateSpreadsheetRequest) (*sheets.BatchUpdateSpreadsheetResponse, error) {
	_ = ctx
	_ = spreadsheetID
	f.calls = append(f.calls, "batch-update")
	resp := &sheets.BatchUpdateSpreadsheetResponse{}
	for _, req := range request.Requests {
		switch {
		case req.AddSheet != nil && req.AddSheet.Properties != nil:
			name := req.AddSheet.Properties.Title
			f.addTab(name, [][]any{})
			resp.Replies = append(resp.Replies, &sheets.Response{AddSheet: &sheets.AddSheetResponse{Properties: &sheets.SheetProperties{Title: name, SheetId: f.ids[name], Index: int64(len(f.order) - 1)}}})
		case req.DeleteSheet != nil:
			name := f.nameByID(req.DeleteSheet.SheetId)
			if name == "" {
				continue
			}
			delete(f.tabs, name)
			delete(f.values, name)
			delete(f.ids, name)
			f.removeFromOrder(name)
		case req.UpdateSheetProperties != nil && req.UpdateSheetProperties.Properties != nil:
			name := f.nameByID(req.UpdateSheetProperties.Properties.SheetId)
			if name == "" {
				continue
			}
			f.moveTab(name, int(req.UpdateSheetProperties.Properties.Index))
		}
	}
	return resp, nil
}

func (f *fakeSheets) nameByID(id int64) string {
	for name, got := range f.ids {
		if got == id {
			return name
		}
	}
	return ""
}

func (f *fakeSheets) removeFromOrder(name string) {
	out := f.order[:0]
	for _, got := range f.order {
		if got != name {
			out = append(out, got)
		}
	}
	f.order = out
}

func (f *fakeSheets) moveTab(name string, index int) {
	f.removeFromOrder(name)
	if index < 0 {
		index = 0
	}
	if index > len(f.order) {
		index = len(f.order)
	}
	f.order = append(f.order, "")
	copy(f.order[index+1:], f.order[index:])
	f.order[index] = name
}

func (f *fakeSheets) GetValues(ctx context.Context, spreadsheetID string, readRange string) (*sheets.ValueRange, error) {
	_ = ctx
	_ = spreadsheetID
	tab, err := tabFromRange(readRange)
	if err != nil {
		return nil, err
	}
	f.calls = append(f.calls, "get:"+tab)
	return &sheets.ValueRange{Values: f.values[tab]}, nil
}

func (f *fakeSheets) ClearValues(ctx context.Context, spreadsheetID string, writeRange string) (*sheets.ClearValuesResponse, error) {
	_ = ctx
	_ = spreadsheetID
	tab, err := tabFromRange(writeRange)
	if err != nil {
		return nil, err
	}
	f.calls = append(f.calls, "clear:"+tab)
	f.values[tab] = nil
	return &sheets.ClearValuesResponse{}, nil
}

func (f *fakeSheets) UpdateValues(ctx context.Context, spreadsheetID string, writeRange string, values *sheets.ValueRange) (*sheets.UpdateValuesResponse, error) {
	_ = ctx
	_ = spreadsheetID
	tab, err := tabFromRange(writeRange)
	if err != nil {
		return nil, err
	}
	f.calls = append(f.calls, "update:"+tab)
	f.values[tab] = values.Values
	return &sheets.UpdateValuesResponse{}, nil
}

func (f *fakeSheets) AppendValues(ctx context.Context, spreadsheetID string, writeRange string, values *sheets.ValueRange) (*sheets.AppendValuesResponse, error) {
	_ = ctx
	_ = spreadsheetID
	tab, err := tabFromRange(writeRange)
	if err != nil {
		return nil, err
	}
	f.calls = append(f.calls, "append:"+tab)
	f.values[tab] = append(f.values[tab], values.Values...)
	return &sheets.AppendValuesResponse{}, nil
}

func (f *fakeSheets) countCall(call string) int {
	count := 0
	for _, got := range f.calls {
		if got == call {
			count++
		}
	}
	return count
}

func tabFromRange(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "'") {
		if idx := strings.Index(value, "!"); idx >= 0 {
			return value[:idx], nil
		}
		return value, nil
	}
	var b strings.Builder
	for i := 1; i < len(value); i++ {
		if value[i] == '\'' {
			if i+1 < len(value) && value[i+1] == '\'' {
				b.WriteByte('\'')
				i++
				continue
			}
			if i+1 < len(value) && value[i+1] == '!' {
				return b.String(), nil
			}
			if i+1 == len(value) {
				return b.String(), nil
			}
			return "", fmt.Errorf("bad range %q", value)
		}
		b.WriteByte(value[i])
	}
	return "", fmt.Errorf("bad range %q", value)
}

func stringRow(values []string) []any {
	out := make([]any, len(values))
	for i, value := range values {
		out[i] = value
	}
	return out
}

func findTab(t *testing.T, specs []tabSpec, name string) tabSpec {
	t.Helper()
	for _, spec := range specs {
		if spec.Name == name {
			return spec
		}
	}
	t.Fatalf("missing tab %s", name)
	return tabSpec{}
}
