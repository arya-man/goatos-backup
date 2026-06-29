package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/feed/domain"
)

func TestReadinessFailsClosedAtG2WithCountsShiftingSubgates(t *testing.T) {
	checkedAt := time.Date(2026, 6, 30, 9, 0, 0, 0, time.UTC)
	service := NewService(nil).WithClock(func() time.Time { return checkedAt })

	readiness, err := service.Readiness(context.Background(), "00000000-0000-4000-8000-000000000001")
	if err != nil {
		t.Fatalf("Readiness returned error: %v", err)
	}
	if readiness.Status != domain.ReadinessBlocked {
		t.Fatalf("status=%q, want %q", readiness.Status, domain.ReadinessBlocked)
	}
	if readiness.CurrentGate != "G2" {
		t.Fatalf("current gate=%q, want G2", readiness.CurrentGate)
	}
	if readiness.GenerationAllowed {
		t.Fatal("generation must stay blocked until G2 is closed")
	}
	if readiness.GeneratedDirectionsState != "disabled_until_g2_ready" {
		t.Fatalf("generated state=%q", readiness.GeneratedDirectionsState)
	}
	if len(readiness.Gates) != 17 {
		t.Fatalf("gates=%d, want 17", len(readiness.Gates))
	}
	if readiness.Gates[0].ID != "G1" || readiness.Gates[0].Status != domain.ReadinessReady {
		t.Fatalf("G1 = %+v", readiness.Gates[0])
	}
	if readiness.Gates[1].ID != "G2" || readiness.Gates[1].Status != domain.ReadinessBlocked || readiness.Gates[1].AllowsGenerate {
		t.Fatalf("G2 must be blocked/no-generate, got %+v", readiness.Gates[1])
	}
	if !strings.Contains(readiness.Gates[1].BlockerReason, "ShiftingEvent ledger") {
		t.Fatalf("G2 blocker should name ShiftingEvent ledger, got %q", readiness.Gates[1].BlockerReason)
	}
	if len(readiness.CountsShiftingSubgates) != 10 {
		t.Fatalf("counts/shifting subgates=%d, want 10", len(readiness.CountsShiftingSubgates))
	}
	for _, subgate := range readiness.CountsShiftingSubgates {
		if subgate.Status != domain.ReadinessBlocked {
			t.Fatalf("%s status=%q, want blocked", subgate.ID, subgate.Status)
		}
		if subgate.AllowsGenerate {
			t.Fatalf("%s unexpectedly allows generation", subgate.ID)
		}
		if !subgate.LastCheckedAt.Equal(checkedAt) {
			t.Fatalf("%s last_checked_at=%s, want %s", subgate.ID, subgate.LastCheckedAt, checkedAt)
		}
	}
}

func TestReadinessSurfacesShiftedPregnantAndFeedSafetyInvariants(t *testing.T) {
	service := NewService(nil).WithClock(func() time.Time {
		return time.Date(2026, 6, 30, 9, 0, 0, 0, time.UTC)
	})

	readiness, err := service.Readiness(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("Readiness returned error: %v", err)
	}
	byKey := map[string]domain.SafetyInvariant{}
	for _, invariant := range readiness.SafetyInvariants {
		byKey[invariant.Key] = invariant
	}
	required := []string{
		"shifted_pregnant_destination_recompute",
		"destination_shed_shortage_fail_closed",
		"overfeed_wastage_moist_feed_exception",
		"bounded_projection_no_full_herd_scan",
	}
	for _, key := range required {
		invariant, ok := byKey[key]
		if !ok {
			t.Fatalf("missing safety invariant %s", key)
		}
		if invariant.Status != domain.ReadinessBlocked || invariant.AllowsGenerate {
			t.Fatalf("%s = %+v, want blocked/no-generate", key, invariant)
		}
	}
	if !strings.Contains(byKey["shifted_pregnant_destination_recompute"].BlockerReason, "destination shed") {
		t.Fatalf("pregnant-shift invariant should require destination-shed recompute, got %q", byKey["shifted_pregnant_destination_recompute"].BlockerReason)
	}
	if !strings.Contains(byKey["overfeed_wastage_moist_feed_exception"].BlockerReason, "moist/stale") {
		t.Fatalf("wastage invariant should name moist/stale feed risk, got %q", byKey["overfeed_wastage_moist_feed_exception"].BlockerReason)
	}
}
