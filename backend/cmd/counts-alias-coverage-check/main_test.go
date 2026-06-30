package main

import (
	"strings"
	"testing"
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
	if !strings.Contains(blocker, "owner-approved review remain") {
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
