package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/verification/domain"
)

// TestPartitionLabelWiringEndToEnd verifies the partition_label column is wired through all layers.
// It creates items with and without partitions, reads them back, and asserts the correct display.
func TestPartitionLabelWiringEndToEnd(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, defaultQueryTimeout)
	tenantID := newTenant(t, ctx, pool)

	// Create an item WITH partition_label
	partitionLabel := "Part 3"
	idempotencyKeyA := fmt.Sprintf("partition-test-%d", time.Now().UnixNano())
	itemWithPartition, err := repo.CreateItem(ctx, domain.CreateItem{
		TenantID: tenantID,
		Vertical: "vaccination",
		Module:   "vaccination",
		Category: "vaccination_proof",
		Source: domain.SourceRef{
			Module:  "vaccination",
			RefType: "vaccination_goat",
			RefID:   tenantID,
		},
		MediaRefs:      []string{},
		PartitionLabel: &partitionLabel,
		CapturedAt:     time.Now().UTC(),
		IdempotencyKey: idempotencyKeyA,
	})
	if err != nil {
		t.Fatalf("CreateItem with partition: %v", err)
	}
	if !itemWithPartition.Created {
		t.Fatalf("expected new item, got idempotent replay")
	}
	if itemWithPartition.Item.PartitionLabel == nil || *itemWithPartition.Item.PartitionLabel != partitionLabel {
		t.Errorf("expected partition_label=%q, got %v", partitionLabel, itemWithPartition.Item.PartitionLabel)
	}

	// Read back and verify partition_label is preserved
	readBack, err := repo.GetItem(ctx, tenantID, itemWithPartition.Item.ItemID)
	if err != nil {
		t.Fatalf("GetItem: %v", err)
	}
	if readBack.PartitionLabel == nil || *readBack.PartitionLabel != partitionLabel {
		t.Errorf("round-trip: expected partition_label=%q, got %v", partitionLabel, readBack.PartitionLabel)
	}

	// Create an item WITHOUT partition_label
	idempotencyKeyB := fmt.Sprintf("no-partition-test-%d", time.Now().UnixNano())
	itemWithoutPartition, err := repo.CreateItem(ctx, domain.CreateItem{
		TenantID: tenantID,
		Vertical: "vaccination",
		Module:   "vaccination",
		Category: "vaccination_proof",
		Source: domain.SourceRef{
			Module:  "vaccination",
			RefType: "vaccination_goat",
			RefID:   tenantID,
		},
		MediaRefs:      []string{},
		CapturedAt:     time.Now().UTC(),
		IdempotencyKey: idempotencyKeyB,
	})
	if err != nil {
		t.Fatalf("CreateItem without partition: %v", err)
	}
	if itemWithoutPartition.Item.PartitionLabel != nil {
		t.Errorf("expected partition_label=nil, got %v", itemWithoutPartition.Item.PartitionLabel)
	}

	t.Logf("SUCCESS: partition_label wiring verified")
	t.Logf("  - With partition: partition_label=%v", readBack.PartitionLabel)
	t.Logf("  - Without partition: partition_label=%v", itemWithoutPartition.Item.PartitionLabel)
}

func ptrStr(s string) *string {
	return &s
}
