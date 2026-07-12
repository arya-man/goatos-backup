package main

import (
	"testing"
	"time"
)

func TestBuildFunnelDailyRowsOrdersStepsAndFillsZeroForMissingEvents(t *testing.T) {
	tenantID := "tenant-1"
	eventDate := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)

	steps := []FunnelStep{
		{FunnelKey: "core", StepKey: "login", StepIndex: 0, EventName: "login_success"},
		{FunnelKey: "core", StepKey: "submit", StepIndex: 1, EventName: "vaccination_capture_submitted"},
		// scan_completed is one of the "not yet emitted by Android" steps -
		// BigQuery's GROUP BY simply never returns a row for it, so the map
		// lookup below has no entry (see buildFunnelDailyRows doc comment).
		{FunnelKey: "core", StepKey: "scan", StepIndex: 2, EventName: "scan_completed"},
	}
	usersByEvent := map[string]int64{"login_success": 100, "vaccination_capture_submitted": 40}
	sessionsByEvent := map[string]int64{"login_success": 120, "vaccination_capture_submitted": 45}

	got := buildFunnelDailyRows(tenantID, eventDate, steps, usersByEvent, sessionsByEvent, now)

	if len(got) != 3 {
		t.Fatalf("len(got) = %d, want 3 (one row per configured step, regardless of BQ result)", len(got))
	}

	for i, row := range got {
		if row.TenantID != tenantID || !row.EventDate.Equal(eventDate) || !row.UpdatedAt.Equal(now) {
			t.Fatalf("row %d stamping wrong: %+v", i, row)
		}
		if row.FunnelKey != "core" {
			t.Fatalf("row %d FunnelKey = %q, want core", i, row.FunnelKey)
		}
	}

	if got[0].StepKey != "login" || got[0].StepIndex != 0 || got[0].Users != 100 || got[0].Sessions != 120 || got[0].Conversions != 100 {
		t.Fatalf("row 0 (login) wrong: %+v", got[0])
	}
	if got[1].StepKey != "submit" || got[1].StepIndex != 1 || got[1].Users != 40 || got[1].Sessions != 45 || got[1].Conversions != 40 {
		t.Fatalf("row 1 (submit) wrong: %+v", got[1])
	}
	// The missing-event step must zero-value, not panic or skip a row - the
	// funnel_daily table must still carry a step_index=2 row so Grafana's
	// step ordering isn't silently missing an entry.
	if got[2].StepKey != "scan" || got[2].StepIndex != 2 || got[2].Users != 0 || got[2].Sessions != 0 || got[2].Conversions != 0 {
		t.Fatalf("row 2 (scan, no BQ data) wrong: %+v", got[2])
	}
}

func TestBuildFunnelDailyRowsEmptyStepsProducesEmptyOutput(t *testing.T) {
	got := buildFunnelDailyRows("tenant-1", time.Now(), nil, nil, nil, time.Now())
	if len(got) != 0 {
		t.Fatalf("len(got) = %d, want 0 for no configured steps", len(got))
	}
}

func TestFunnelDailyRowsToColumnsPreservesOrderAndValues(t *testing.T) {
	eventDate := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	rows := []funnelDailyRow{
		{TenantID: "t1", EventDate: eventDate, FunnelKey: "core", StepKey: "login", StepIndex: 0, Users: 100, Sessions: 120, Conversions: 100, UpdatedAt: updatedAt},
		{TenantID: "t1", EventDate: eventDate, FunnelKey: "core", StepKey: "submit", StepIndex: 1, Users: 40, Sessions: 45, Conversions: 40, UpdatedAt: updatedAt},
	}

	tenantIDs, eventDates, funnelKeys, stepKeys, stepIndexes, users, sessions, conversions, updatedAts := funnelDailyRowsToColumns(rows)

	wantLen := 2
	for name, got := range map[string]int{
		"tenantIDs": len(tenantIDs), "eventDates": len(eventDates), "funnelKeys": len(funnelKeys),
		"stepKeys": len(stepKeys), "stepIndexes": len(stepIndexes), "users": len(users),
		"sessions": len(sessions), "conversions": len(conversions), "updatedAts": len(updatedAts),
	} {
		if got != wantLen {
			t.Fatalf("len(%s) = %d, want %d (one UNNEST column slice per row)", name, got, wantLen)
		}
	}

	if stepKeys[0] != "login" || stepIndexes[0] != 0 || users[0] != 100 || sessions[0] != 120 || conversions[0] != 100 {
		t.Fatalf("column 0 mismatched row 0: stepKeys=%v stepIndexes=%v users=%v sessions=%v conversions=%v", stepKeys, stepIndexes, users, sessions, conversions)
	}
	if stepKeys[1] != "submit" || stepIndexes[1] != 1 || users[1] != 40 {
		t.Fatalf("column 1 mismatched row 1: stepKeys=%v stepIndexes=%v users=%v", stepKeys, stepIndexes, users)
	}
	if !eventDates[0].Equal(eventDate) || !updatedAts[0].Equal(updatedAt) {
		t.Fatalf("timestamp columns not preserved: eventDates=%v updatedAts=%v", eventDates, updatedAts)
	}
}

func TestFunnelDailyRowsToColumnsEmptyInput(t *testing.T) {
	tenantIDs, eventDates, funnelKeys, stepKeys, stepIndexes, users, sessions, conversions, updatedAts := funnelDailyRowsToColumns(nil)
	for name, got := range map[string]int{
		"tenantIDs": len(tenantIDs), "eventDates": len(eventDates), "funnelKeys": len(funnelKeys),
		"stepKeys": len(stepKeys), "stepIndexes": len(stepIndexes), "users": len(users),
		"sessions": len(sessions), "conversions": len(conversions), "updatedAts": len(updatedAts),
	} {
		if got != 0 {
			t.Fatalf("len(%s) = %d, want 0 for nil input", name, got)
		}
	}
}
