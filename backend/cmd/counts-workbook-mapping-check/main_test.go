package main

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestParseWorkbookColumnMapFixtureCoversRequiredRoles(t *testing.T) {
	f, err := os.Open("../../testdata/counts/source-workbook-column-map.json")
	if err != nil {
		t.Fatalf("open source workbook column map fixture: %v", err)
	}
	defer f.Close()
	got, err := parseWorkbookMappingFile(f)
	if err != nil {
		t.Fatalf("parseWorkbookMappingFile fixture: %v", err)
	}
	if !strings.Contains(got.SourceRef, "feed-direction-workbook-automation-findings.md") {
		t.Fatalf("source_ref=%q, want workbook source finding", got.SourceRef)
	}
	result := checkWorkbookMapping(got)
	if len(result.Missing) != 0 {
		t.Fatalf("missing=%v, want fixture to cover required fields", result.Missing)
	}
	if result.Columns < 100 || result.Groups < 12 {
		t.Fatalf("columns=%d groups=%d, want broad workbook mapping coverage", result.Columns, result.Groups)
	}
	for _, want := range []mappedColumn{
		{SourceSystem: "counting_db", Workbook: "Counting DB - values only.xlsx", Sheet: "DB", SourceRole: "base_count_anchor", ColumnName: "Shed Tag", CanonicalField: "shed_tag"},
		{SourceSystem: "feed_automation_workbook", Workbook: "Feed Directions Automation DB.xlsx", Sheet: "Feed Direction", SourceRole: "feed_direction_output", ColumnName: "Feed 1 Quantity", CanonicalField: "feed_quantity_as_fed"},
		{SourceSystem: "feed_automation_workbook", Workbook: "Feed Directions Automation DB.xlsx", Sheet: "CBE Validation", SourceRole: "feed_vector_candidate", ColumnName: "Wastage Factor", CanonicalField: "wastage_factor"},
		{SourceSystem: "feed_automation_workbook", Workbook: "Feed Directions Automation DB.xlsx", Sheet: "Feed Consumption & Wastage", SourceRole: "consumption_wastage_proof", ColumnName: "Wastage Qty (kg)", CanonicalField: "wastage_quantity"},
		{SourceSystem: "sheds_db", Workbook: "Sheds DB.xlsx", Sheet: "DB", SourceRole: "shed_profile", ColumnName: "CBE Capacity", CanonicalField: "capacity_value"},
	} {
		if !hasMappedColumn(got.MappedColumns, want) {
			t.Fatalf("fixture missing %+v", want)
		}
	}
}

func TestParseWorkbookMappingRejectsDuplicateColumns(t *testing.T) {
	_, err := parseWorkbookMappingFile(strings.NewReader(`{
		"source_ref": "source.md",
		"mapped_columns": [
			{"source_system":"counting_db","workbook":"book.xlsx","sheet":"DB","source_role":"base_count_anchor","column_name":"Date","canonical_field":"counted_at","value_type":"date"},
			{"source_system":"counting_db","workbook":"book.xlsx","sheet":"DB","source_role":"base_count_anchor","column_name":"Date","canonical_field":"counted_at","value_type":"date"}
		]
	}`))
	if err == nil || !strings.Contains(err.Error(), "duplicate mapped column") {
		t.Fatalf("err=%v, want duplicate mapped column", err)
	}
}

func TestParseWorkbookMappingRejectsInvalidCanonicalField(t *testing.T) {
	_, err := parseWorkbookMappingFile(strings.NewReader(`{
		"source_ref": "source.md",
		"mapped_columns": [
			{"source_system":"counting_db","workbook":"book.xlsx","sheet":"DB","source_role":"base_count_anchor","column_name":"Date","canonical_field":"spreadsheet_truth","value_type":"date"}
		]
	}`))
	if err == nil || !strings.Contains(err.Error(), "canonical_field is invalid") {
		t.Fatalf("err=%v, want invalid canonical field", err)
	}
}

func TestCheckWorkbookMappingReportsMissingRequiredFields(t *testing.T) {
	file := workbookMappingFile{MappedColumns: []mappedColumn{
		{SourceSystem: "counting_db", Workbook: "book.xlsx", Sheet: "DB", SourceRole: "base_count_anchor", ColumnName: "Date", CanonicalField: "counted_at", ValueType: "date"},
		{SourceSystem: "counting_db", Workbook: "book.xlsx", Sheet: "DB", SourceRole: "base_count_anchor", ColumnName: "Farm", CanonicalField: "farm_code", ValueType: "enum"},
	}}
	result := checkWorkbookMapping(file)
	if len(result.Missing) == 0 {
		t.Fatalf("missing=%v, want missing required fields", result.Missing)
	}
	if !strings.Contains(strings.Join(result.Missing, ","), "head_count") {
		t.Fatalf("missing=%v, want head_count missing", result.Missing)
	}
	status, blocker := mappingStatus("source.md", result)
	if status != "blocked" || !strings.Contains(blocker, "missing=") {
		t.Fatalf("status=%s blocker=%q, want blocked missing status", status, blocker)
	}
}

func TestMappingStatusKeepsPassingCheckPending(t *testing.T) {
	status, blocker := mappingStatus("source.md", workbookMappingResult{Columns: 10, Groups: 2})
	if status != "pending" {
		t.Fatalf("status=%s, want pending", status)
	}
	if !strings.Contains(blocker, "seeded local E2E") || !strings.Contains(blocker, "owner-approved") {
		t.Fatalf("blocker=%q, want remaining closure caveats", blocker)
	}
}

func TestUpsertCSG10MappingReadinessWritesCommandEvidence(t *testing.T) {
	db := &fakeReadinessDB{}
	err := upsertCSG10MappingReadiness(context.Background(), db, "tenant-1", "source.md", "pending", "still pending")
	if err != nil {
		t.Fatalf("upsertCSG10MappingReadiness: %v", err)
	}
	if !strings.Contains(db.query, "counts-workbook-mapping-check") {
		t.Fatalf("query=%q, want command implementation ref", db.query)
	}
	if len(db.args) != 4 {
		t.Fatalf("args=%+v, want tenant/status/evidence/blocker", db.args)
	}
	if db.args[1] != "pending" {
		t.Fatalf("status arg=%v", db.args[1])
	}
	evidence, _ := db.args[2].(string)
	if !strings.HasPrefix(evidence, "counts-workbook-mapping-check:source.md:") {
		t.Fatalf("evidence=%q, want command evidence ref", evidence)
	}
}

func hasMappedColumn(columns []mappedColumn, want mappedColumn) bool {
	for _, got := range columns {
		if got.SourceSystem == want.SourceSystem &&
			got.Workbook == want.Workbook &&
			got.Sheet == want.Sheet &&
			got.SourceRole == want.SourceRole &&
			got.ColumnName == want.ColumnName &&
			got.CanonicalField == want.CanonicalField {
			return true
		}
	}
	return false
}

type fakeReadinessDB struct {
	query string
	args  []any
	err   error
}

func (db *fakeReadinessDB) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	db.query = query
	db.args = args
	return pgconn.CommandTag{}, db.err
}
