package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestParseRequiredAliasesNormalizesAndSorts(t *testing.T) {
	got, err := parseRequiredAliases(strings.NewReader(`{
		"source_ref": "context/source-findings/sheds-db-source-findings.md",
		"required_aliases": [
			{"dimension": "stage_tag", "source_system": "sheds_db", "source_value": "Warmup"},
			{"dimension": "stage_tag", "source_value": "Non-Pregnant"}
		]
	}`))
	if err != nil {
		t.Fatalf("parseRequiredAliases: %v", err)
	}
	if got.SourceRef != "context/source-findings/sheds-db-source-findings.md" {
		t.Fatalf("source_ref=%q", got.SourceRef)
	}
	if got.RequiredAliases[0].SourceSystem != "*" || got.RequiredAliases[0].SourceValue != "Non-Pregnant" {
		t.Fatalf("first alias=%+v, want default wildcard source system and sorted value", got.RequiredAliases[0])
	}
	if got.RequiredAliases[1].SourceSystem != "sheds_db" || got.RequiredAliases[1].SourceValue != "Warmup" {
		t.Fatalf("second alias=%+v", got.RequiredAliases[1])
	}
}

func TestParseSourceWorkbookRequiredAliasesFixture(t *testing.T) {
	f, err := os.Open("../../testdata/counts/source-workbook-required-aliases.json")
	if err != nil {
		t.Fatalf("open source workbook aliases fixture: %v", err)
	}
	defer f.Close()
	got, err := parseRequiredAliases(f)
	if err != nil {
		t.Fatalf("parseRequiredAliases fixture: %v", err)
	}
	if !strings.Contains(got.SourceRef, "feed-direction-workbook-automation-findings.md") {
		t.Fatalf("source_ref=%q, want workbook source finding", got.SourceRef)
	}
	if len(got.RequiredAliases) != 121 {
		t.Fatalf("required aliases=%d, want 121", len(got.RequiredAliases))
	}
	for _, want := range []requiredAlias{
		{Dimension: "stage_tag", SourceSystem: "counting_db", SourceValue: "Pregant"},
		{Dimension: "breed", SourceSystem: "feed_automation_workbook", SourceValue: "Anathapur Sheep"},
		{Dimension: "stage_tag", SourceSystem: "feed_automation_workbook", SourceValue: "F2(30kg +)"},
		{Dimension: "sex", SourceSystem: "counting_db", SourceValue: "Female"},
		{Dimension: "sex", SourceSystem: "counting_db", SourceValue: "Male"},
	} {
		if !hasRequiredAlias(got.RequiredAliases, want) {
			t.Fatalf("fixture missing %+v", want)
		}
	}
}

func TestParseRequiredAliasesRejectsDuplicateNormalizedValues(t *testing.T) {
	_, err := parseRequiredAliases(strings.NewReader(`{
		"required_aliases": [
			{"dimension": "stage_tag", "source_system": "sheds_db", "source_value": "Non Pregnant"},
			{"dimension": "stage_tag", "source_system": "sheds_db", "source_value": "Non   Pregnant"}
		]
	}`))
	if err == nil || !strings.Contains(err.Error(), "duplicate required alias") {
		t.Fatalf("err=%v, want duplicate required alias", err)
	}
}

func TestCoverageStatusNeverMarksCSG7Ready(t *testing.T) {
	status, blocker := coverageStatus("source.md", aliasCoverageResult{Total: 2})
	if status != "pending" {
		t.Fatalf("status=%s, want pending", status)
	}
	if !strings.Contains(blocker, "full source workbook parity") || !strings.Contains(blocker, "owner-approved review remain") {
		t.Fatalf("blocker=%q, want owner review caveat", blocker)
	}
}

func TestCoverageStatusBlocksMissingAliases(t *testing.T) {
	status, blocker := coverageStatus("source.md", aliasCoverageResult{
		Total: 2,
		Missing: []requiredAlias{{
			Dimension: "stage_tag", SourceSystem: "sheds_db", SourceValue: "Warmup",
		}},
	})
	if status != "blocked" {
		t.Fatalf("status=%s, want blocked", status)
	}
	if !strings.Contains(blocker, "stage_tag/sheds_db/Warmup") {
		t.Fatalf("blocker=%q, want missing alias details", blocker)
	}
}

func TestCheckAliasCoverageUsesApprovedSpecificOrWildcardAliases(t *testing.T) {
	db := fakeAliasDB{approved: map[string]bool{
		"stage_tag\x00sheds_db\x00warmup":        true,
		"stage_tag\x00*\x00non-pregnant":         true,
		"stage_tag\x00sheds_db\x00f2-female":     true,
		"stage_tag\x00sheds_db\x00f2-male":       true,
		"stage_tag\x00sheds_db\x00icu-kid":       true,
		"stage_tag\x00sheds_db\x00k1/k2":         true,
		"shed_tag\x00sheds_db\x00gandhi_1":       true,
		"breed\x00feed_shiftings_docx\x00sirohi": true,
	}}
	required := []requiredAlias{
		{Dimension: "stage_tag", SourceSystem: "sheds_db", SourceValue: "Warmup"},
		{Dimension: "stage_tag", SourceSystem: "sheds_db", SourceValue: "Non-Pregnant"},
		{Dimension: "stage_tag", SourceSystem: "sheds_db", SourceValue: "F2-Female"},
		{Dimension: "stage_tag", SourceSystem: "sheds_db", SourceValue: "F2-Male"},
		{Dimension: "stage_tag", SourceSystem: "sheds_db", SourceValue: "ICU-Kid"},
		{Dimension: "stage_tag", SourceSystem: "sheds_db", SourceValue: "K1/K2"},
		{Dimension: "shed_tag", SourceSystem: "sheds_db", SourceValue: "Gandhi 1"},
		{Dimension: "breed", SourceSystem: "feed_shiftings_docx", SourceValue: "SIROHI"},
	}

	result, err := checkAliasCoverage(context.Background(), &db, "tenant-1", required)
	if err != nil {
		t.Fatalf("checkAliasCoverage: %v", err)
	}
	if result.Total != len(required) || len(result.Missing) != 0 {
		t.Fatalf("result=%+v, want all aliases covered", result)
	}
}

func TestCheckAliasCoverageReportsMissingAliases(t *testing.T) {
	db := fakeAliasDB{approved: map[string]bool{
		"stage_tag\x00sheds_db\x00warmup": true,
	}}
	required := []requiredAlias{
		{Dimension: "stage_tag", SourceSystem: "sheds_db", SourceValue: "Warmup"},
		{Dimension: "stage_tag", SourceSystem: "sheds_db", SourceValue: "Mother"},
	}

	result, err := checkAliasCoverage(context.Background(), &db, "tenant-1", required)
	if err != nil {
		t.Fatalf("checkAliasCoverage: %v", err)
	}
	if result.Total != 2 || len(result.Missing) != 1 || result.Missing[0].SourceValue != "Mother" {
		t.Fatalf("result=%+v, want Mother missing only", result)
	}
}

func TestCheckAliasCoverageSurfacesLookupErrors(t *testing.T) {
	db := fakeAliasDB{err: errors.New("db down")}
	_, err := checkAliasCoverage(context.Background(), &db, "tenant-1", []requiredAlias{{
		Dimension: "stage_tag", SourceSystem: "sheds_db", SourceValue: "Warmup",
	}})
	if err == nil || !strings.Contains(err.Error(), "stage_tag/sheds_db/Warmup") {
		t.Fatalf("err=%v, want contextual lookup error", err)
	}
}

func TestUpsertCSG7ReadinessWritesCommandEvidence(t *testing.T) {
	db := &fakeReadinessDB{}
	err := upsertCSG7Readiness(context.Background(), db, "tenant-1", "source.md", "pending", "still pending")
	if err != nil {
		t.Fatalf("upsertCSG7Readiness: %v", err)
	}
	if !strings.Contains(db.query, "counts-alias-coverage-check") {
		t.Fatalf("query=%q, want implementation reference", db.query)
	}
	if len(db.args) != 4 {
		t.Fatalf("args=%+v, want tenant/status/evidence/blocker", db.args)
	}
	if db.args[1] != "pending" {
		t.Fatalf("status arg=%v", db.args[1])
	}
	evidence, _ := db.args[2].(string)
	if !strings.HasPrefix(evidence, "counts-alias-coverage-check:source.md:") {
		t.Fatalf("evidence=%q, want command evidence ref", evidence)
	}
	if db.args[3] != "still pending" {
		t.Fatalf("blocker arg=%v", db.args[3])
	}
}

func hasRequiredAlias(required []requiredAlias, want requiredAlias) bool {
	for _, got := range required {
		if got.Dimension == want.Dimension &&
			got.SourceSystem == want.SourceSystem &&
			got.SourceValue == want.SourceValue {
			return true
		}
	}
	return false
}

type fakeAliasDB struct {
	approved map[string]bool
	err      error
}

func (db *fakeAliasDB) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	dimension, _ := args[1].(string)
	norm, _ := args[2].(string)
	sourceSystem, _ := args[3].(string)
	covered := db.approved[dimension+"\x00"+sourceSystem+"\x00"+norm] ||
		db.approved[dimension+"\x00*\x00"+norm]
	return fakeBoolRow{covered: covered, err: db.err}
}

type fakeBoolRow struct {
	covered bool
	err     error
}

func (row fakeBoolRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	ok, _ := dest[0].(*bool)
	*ok = row.covered
	return nil
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
