package main

import (
	"strings"
	"testing"
	"time"
)

func TestAnalyzePlanPassesWhenExpectedIndexAppears(t *testing.T) {
	raw := []byte(`[{"Plan":{"Node Type":"Nested Loop","Plans":[{"Node Type":"Index Scan","Relation Name":"count_base_anchors","Index Name":"count_base_anchors_hot_idx"},{"Node Type":"Index Only Scan","Relation Name":"shifting_events","Index Name":"shifting_events_projection_window_idx"}]}}]`)
	result := analyzePlan(planCheck{
		Name:            "projection_anchors_latest",
		ExpectedIndexes: []string{"count_base_anchors_hot_idx"},
		ProtectedTables: []string{"count_base_anchors"},
	}, raw)
	if !result.Passed {
		t.Fatalf("result=%+v, want pass", result)
	}
	if len(result.Indexes) != 2 || result.Indexes[0] != "count_base_anchors_hot_idx" {
		t.Fatalf("indexes=%v", result.Indexes)
	}
}

func TestAnalyzePlanBlocksProtectedSeqScan(t *testing.T) {
	raw := []byte(`[{"Plan":{"Node Type":"Seq Scan","Relation Name":"count_base_anchors"}}]`)
	result := analyzePlan(planCheck{
		Name:            "projection_anchors_latest",
		ExpectedIndexes: []string{"count_base_anchors_hot_idx"},
		ProtectedTables: []string{"count_base_anchors"},
	}, raw)
	if result.Passed || !strings.Contains(result.Failure, "sequential scan") {
		t.Fatalf("result=%+v, want protected seq-scan failure", result)
	}
}

func TestAnalyzePlanBlocksWhenNoExpectedIndexAppears(t *testing.T) {
	raw := []byte(`[{"Plan":{"Node Type":"Index Scan","Relation Name":"count_base_anchors","Index Name":"other_idx"}}]`)
	result := analyzePlan(planCheck{
		Name:            "projection_anchors_latest",
		ExpectedIndexes: []string{"count_base_anchors_hot_idx"},
		ProtectedTables: []string{"count_base_anchors"},
	}, raw)
	if result.Passed || !strings.Contains(result.Failure, "none of the expected indexes") {
		t.Fatalf("result=%+v, want missing expected index failure", result)
	}
}

func TestReadinessStatusKeepsCSG10PendingWhenChecksPass(t *testing.T) {
	status, blocker := readinessStatus([]checkResult{{Name: "projection_rows_feed_hot", Passed: true}})
	if status != "pending" {
		t.Fatalf("status=%s, want pending", status)
	}
	if !strings.Contains(blocker, "seeded local E2E") {
		t.Fatalf("blocker=%q", blocker)
	}
}

func TestReadinessStatusBlocksOnFailedCheck(t *testing.T) {
	status, blocker := readinessStatus([]checkResult{{Name: "projection_rows_feed_hot", Failure: "seq scan"}})
	if status != "blocked" {
		t.Fatalf("status=%s, want blocked", status)
	}
	if !strings.Contains(blocker, "projection_rows_feed_hot") {
		t.Fatalf("blocker=%q", blocker)
	}
}

func TestParseFlagsDefaultsTargetDateToTomorrow(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	cfg, err := parseFlags([]string{
		"-tenant-id", "00000000-0000-4000-8000-000000000001",
		"-park-id", "00000000-0000-4000-8000-000000000010",
	}, func() time.Time { return now })
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if got := cfg.TargetDate.Format(time.RFC3339); got != "2026-07-01T00:00:00Z" {
		t.Fatalf("target_date=%s", got)
	}
}
