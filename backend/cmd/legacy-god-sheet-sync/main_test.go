package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
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

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
