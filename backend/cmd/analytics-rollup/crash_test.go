package main

import (
	"testing"
	"time"

	"cloud.google.com/go/bigquery"
)

func TestBuildCrashDailyRowsComputesCrashFreePercentagesAndDefaultsTopIssues(t *testing.T) {
	tenantID := "tenant-1"
	eventDate := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 7, 2, 3, 15, 0, 0, time.UTC)

	rows := []crashRow{
		{
			AppVersion:    "1.2.0",
			FatalCount:    5,
			NonfatalCount: 10,
			TopIssuesJSON: bigquery.NullString{StringVal: `[{"issue_id":"X"}]`, Valid: true},
		},
		{
			AppVersion:    "1.1.0",
			FatalCount:    0,
			NonfatalCount: 2,
			TopIssuesJSON: bigquery.NullString{Valid: false},
		},
		{
			AppVersion:    "1.0.0",
			FatalCount:    1,
			NonfatalCount: 0,
			TopIssuesJSON: bigquery.NullString{StringVal: "", Valid: true},
		},
	}

	got := buildCrashDailyRows(tenantID, eventDate, rows, 100, 200, now)

	if len(got) != 3 {
		t.Fatalf("len(got) = %d, want 3", len(got))
	}

	first := got[0]
	if first.TenantID != tenantID || !first.EventDate.Equal(eventDate) || !first.UpdatedAt.Equal(now) {
		t.Fatalf("row 0 stamping wrong: %+v", first)
	}
	if first.AppVersion != "1.2.0" || first.FatalCount != 5 || first.NonfatalCount != 10 {
		t.Fatalf("row 0 fields wrong: %+v", first)
	}
	if first.CrashFreeUsersPct != crashFreeRatioPct(5, 100) {
		t.Fatalf("row 0 CrashFreeUsersPct = %v, want %v", first.CrashFreeUsersPct, crashFreeRatioPct(5, 100))
	}
	if first.CrashFreeSessionsPct != crashFreeRatioPct(5, 200) {
		t.Fatalf("row 0 CrashFreeSessionsPct = %v, want %v", first.CrashFreeSessionsPct, crashFreeRatioPct(5, 200))
	}
	if first.TopIssues != `[{"issue_id":"X"}]` {
		t.Fatalf("row 0 TopIssues = %q, want valid JSON passthrough", first.TopIssues)
	}

	if got[1].TopIssues != "[]" {
		t.Fatalf("row 1 (NULL top_issues_json) TopIssues = %q, want default \"[]\"", got[1].TopIssues)
	}
	if got[2].TopIssues != "[]" {
		t.Fatalf("row 2 (valid but empty top_issues_json) TopIssues = %q, want default \"[]\"", got[2].TopIssues)
	}
}

func TestBuildCrashDailyRowsEmptyInputProducesEmptyOutput(t *testing.T) {
	got := buildCrashDailyRows("tenant-1", time.Now(), nil, 100, 200, time.Now())
	if len(got) != 0 {
		t.Fatalf("len(got) = %d, want 0 for no input rows", len(got))
	}
}

func TestCrashDailyRowsToColumnsPreservesOrderAndValues(t *testing.T) {
	eventDate := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	rows := []crashDailyRow{
		{
			TenantID: "t1", EventDate: eventDate, AppVersion: "1.2.0",
			CrashFreeUsersPct: 95, CrashFreeSessionsPct: 90,
			FatalCount: 5, NonfatalCount: 10, TopIssues: "[]", UpdatedAt: updatedAt,
		},
		{
			TenantID: "t1", EventDate: eventDate, AppVersion: "1.1.0",
			CrashFreeUsersPct: 100, CrashFreeSessionsPct: 100,
			FatalCount: 0, NonfatalCount: 2, TopIssues: `[{"a":1}]`, UpdatedAt: updatedAt,
		},
	}

	tenantIDs, eventDates, appVersions, crashFreeUsersPct, crashFreeSessionsPct, fatalCounts, nonfatalCounts, topIssues, updatedAts := crashDailyRowsToColumns(rows)

	wantLen := 2
	for name, got := range map[string]int{
		"tenantIDs": len(tenantIDs), "eventDates": len(eventDates), "appVersions": len(appVersions),
		"crashFreeUsersPct": len(crashFreeUsersPct), "crashFreeSessionsPct": len(crashFreeSessionsPct),
		"fatalCounts": len(fatalCounts), "nonfatalCounts": len(nonfatalCounts),
		"topIssues": len(topIssues), "updatedAts": len(updatedAts),
	} {
		if got != wantLen {
			t.Fatalf("len(%s) = %d, want %d (one UNNEST column slice per row)", name, got, wantLen)
		}
	}

	if tenantIDs[0] != "t1" || appVersions[0] != "1.2.0" || fatalCounts[0] != 5 || nonfatalCounts[0] != 10 {
		t.Fatalf("column 0 mismatched row 0: tenantIDs=%v appVersions=%v fatalCounts=%v nonfatalCounts=%v", tenantIDs, appVersions, fatalCounts, nonfatalCounts)
	}
	if appVersions[1] != "1.1.0" || topIssues[1] != `[{"a":1}]` {
		t.Fatalf("column 1 mismatched row 1: appVersions=%v topIssues=%v", appVersions, topIssues)
	}
	if !eventDates[0].Equal(eventDate) || !updatedAts[0].Equal(updatedAt) {
		t.Fatalf("timestamp columns not preserved: eventDates=%v updatedAts=%v", eventDates, updatedAts)
	}
}

func TestCrashDailyRowsToColumnsEmptyInput(t *testing.T) {
	tenantIDs, eventDates, appVersions, crashFreeUsersPct, crashFreeSessionsPct, fatalCounts, nonfatalCounts, topIssues, updatedAts := crashDailyRowsToColumns(nil)
	for name, got := range map[string]int{
		"tenantIDs": len(tenantIDs), "eventDates": len(eventDates), "appVersions": len(appVersions),
		"crashFreeUsersPct": len(crashFreeUsersPct), "crashFreeSessionsPct": len(crashFreeSessionsPct),
		"fatalCounts": len(fatalCounts), "nonfatalCounts": len(nonfatalCounts),
		"topIssues": len(topIssues), "updatedAts": len(updatedAts),
	} {
		if got != 0 {
			t.Fatalf("len(%s) = %d, want 0 for nil input", name, got)
		}
	}
}
