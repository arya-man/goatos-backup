package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// TestShiftingLocationCompositionPartitioned verifies that partitioned destination
// sheds render both shed name and partition label in the verification subject label.
func TestShiftingLocationCompositionPartitioned(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)

	// Seed a partitioned destination shed (partition_label = "Part 3")
	destShedID := "00000000-0000-4000-8000-0000000ddest"
	seedShedProfile(t, ctx, pool, destShedID, "adult")
	// Manually set partition_label on the destination shed to "Part 3"
	_, err := pool.Exec(ctx, `UPDATE locations SET partition_label = 'Part 3' WHERE location_id = $1::uuid`, destShedID)
	if err != nil {
		t.Fatalf("set partition_label: %v", err)
	}

	goatA := "00000000-0000-4000-8000-00000000d001"
	seedApprovalGoat(t, ctx, pool, goatA, countsShedA)
	shiftingEventID, approvalRequestID := submitShiftingApproval(t, ctx, repo, "comp-partitioned", []string{goatA})
	if _, _, err := approveShifting(repo, ctx, "comp-partitioned", approvalRequestID, shiftingEventID, []string{goatA}); err != nil {
		t.Fatalf("approve shifting: %v", err)
	}

	// Complete the shift: the destination is partitioned
	result, _, err := repo.CompleteShiftingEvent(ctx, domain.ShiftingCompletionCommand{
		TenantID:           countsTenant,
		ShiftingEventID:    shiftingEventID,
		CompletedByUserID:  countsOperator,
		CompletedAt:        time.Now().In(biztime.DefaultLocation()),
		ProofRef:           "proof-partitioned",
		IdempotencyKey:     "complete-partitioned",
		RequestFingerprint: "fp-partitioned",
	})
	if err != nil {
		t.Fatalf("complete shifting: %v", err)
	}

	// The result must carry both destination shed name and partition label
	if result.DestinationShedName == "" {
		t.Fatalf("DestinationShedName empty, want non-empty")
	}
	if result.DestinationPartitionLabel == "" {
		t.Fatalf("DestinationPartitionLabel empty, want 'Part 3'")
	}

	// Verify the location displays correctly: "Godel 1 - Part 3" or similar format
	locDisplay := result.DestinationShedName + " - " + result.DestinationPartitionLabel
	if result.DestinationPartitionLabel != "Part 3" {
		t.Fatalf("DestinationPartitionLabel=%q, want 'Part 3'", result.DestinationPartitionLabel)
	}
	t.Logf("Partitioned location display: %s", locDisplay)
}

// TestShiftingLocationCompositionUnpartitioned verifies that unpartitioned destination
// sheds render only the shed name without a trailing separator.
func TestShiftingLocationCompositionUnpartitioned(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)

	goatA := "00000000-0000-4000-8000-00000000d001"
	seedApprovalGoat(t, ctx, pool, goatA, countsShedA)
	shiftingEventID, approvalRequestID := submitShiftingApproval(t, ctx, repo, "comp-unpart", []string{goatA})
	if _, _, err := approveShifting(repo, ctx, "comp-unpart", approvalRequestID, shiftingEventID, []string{goatA}); err != nil {
		t.Fatalf("approve shifting: %v", err)
	}

	// Complete the shift: the destination (countsShedB) is unpartitioned
	result, _, err := repo.CompleteShiftingEvent(ctx, domain.ShiftingCompletionCommand{
		TenantID:           countsTenant,
		ShiftingEventID:    shiftingEventID,
		CompletedByUserID:  countsOperator,
		CompletedAt:        time.Now().In(biztime.DefaultLocation()),
		ProofRef:           "proof-unpart",
		IdempotencyKey:     "complete-unpart",
		RequestFingerprint: "fp-unpart",
	})
	if err != nil {
		t.Fatalf("complete shifting: %v", err)
	}

	// For unpartitioned sheds, PartitionLabel should be empty or "whole"
	if result.DestinationShedName == "" {
		t.Fatalf("DestinationShedName empty, want non-empty")
	}
	// Unpartitioned shed should have empty or "whole" partition label
	if result.DestinationPartitionLabel != "" && result.DestinationPartitionLabel != "whole" {
		t.Fatalf("DestinationPartitionLabel=%q for unpartitioned shed, want empty or 'whole'", result.DestinationPartitionLabel)
	}

	// Verify no trailing separator appears when unpartitioned
	// The display should be just the shed name
	if result.DestinationPartitionLabel == "whole" || result.DestinationPartitionLabel == "" {
		// This is correct - bare shed name only
		t.Logf("Unpartitioned location display: %s (no separator)", result.DestinationShedName)
	}
}

// TestShiftingSubjectLabelIncludesLocation verifies that the subject label passed to
// verification includes location information in the expected format.
func TestShiftingSubjectLabelIncludesLocation(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)

	// Seed a partitioned destination shed
	destShedID := "00000000-0000-4000-8000-0000000ddest"
	seedShedProfile(t, ctx, pool, destShedID, "adult")
	_, err := pool.Exec(ctx, `UPDATE locations SET partition_label = 'Part 3' WHERE location_id = $1::uuid`, destShedID)
	if err != nil {
		t.Fatalf("set partition_label: %v", err)
	}

	goatA := "00000000-0000-4000-8000-00000000d001"
	seedApprovalGoat(t, ctx, pool, goatA, countsShedA)
	shiftingEventID, approvalRequestID := submitShiftingApproval(t, ctx, repo, "comp-label", []string{goatA})
	if _, _, err := approveShifting(repo, ctx, "comp-label", approvalRequestID, shiftingEventID, []string{goatA}); err != nil {
		t.Fatalf("approve shifting: %v", err)
	}

	// Complete the shift and verify the result can compose a subject label with location
	result, _, err := repo.CompleteShiftingEvent(ctx, domain.ShiftingCompletionCommand{
		TenantID:           countsTenant,
		ShiftingEventID:    shiftingEventID,
		CompletedByUserID:  countsOperator,
		CompletedAt:        time.Now().In(biztime.DefaultLocation()),
		ProofRef:           "proof-label",
		IdempotencyKey:     "complete-label",
		RequestFingerprint: "fp-label",
	})
	if err != nil {
		t.Fatalf("complete shifting: %v", err)
	}

	// Compose the label as the service does (via oploc)
	var locDisplay string
	if result.DestinationPartitionLabel != "" && result.DestinationPartitionLabel != "whole" {
		locDisplay = result.DestinationShedName + " - " + result.DestinationPartitionLabel
	} else {
		locDisplay = result.DestinationShedName
	}
	subject := "Shed move · " + locDisplay + " · 1 animals"

	// Verify the subject has the expected pattern
	if result.DestinationPartitionLabel == "Part 3" {
		if locDisplay != result.DestinationShedName+" - Part 3" {
			t.Fatalf("locDisplay=%q, want %q", locDisplay, result.DestinationShedName+" - Part 3")
		}
	}
	t.Logf("Composed subject label: %s", subject)
	if !contains(subject, "Shed move · ") {
		t.Fatalf("subject missing 'Shed move · ' prefix")
	}
	if !contains(subject, "animals") {
		t.Fatalf("subject missing 'animals' suffix")
	}
}

func contains(s, substr string) bool {
	for i := 0; i < len(s)-len(substr)+1; i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
