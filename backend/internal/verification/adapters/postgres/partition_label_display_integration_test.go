package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/verification/domain"
	"github.com/vgoats/goatos/backend/internal/verification/ports"
)

func TestListQueueEmitsOperationalLocationDisplay_Partitioned_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)

	// Create a park and a partitioned shed (Castro with partition "2")
	var parkID, shedID string
	err := pool.QueryRow(ctx, `
INSERT INTO locations (tenant_id, location_id, location_type, name, status)
VALUES ($1::uuid, gen_random_uuid(), 'park', 'CPT', 'active')
RETURNING location_id::text
`, tenantID).Scan(&parkID)
	if err != nil {
		t.Fatalf("insert park: %v", err)
	}

	err = pool.QueryRow(ctx, `
INSERT INTO locations (tenant_id, location_id, location_type, name, status)
VALUES ($1::uuid, gen_random_uuid(), 'shed', 'Castro', 'active')
RETURNING location_id::text
`, tenantID).Scan(&shedID)
	if err != nil {
		t.Fatalf("insert shed: %v", err)
	}

	// Create a verification item with shed_id and partition_label
	in := domain.CreateItem{
		TenantID: tenantID,
		Vertical: "preventive_care",
		Module:   "vaccination",
		Category: "vaccination_proof",
		Source: domain.SourceRef{
			Module:  "vaccination",
			RefType: "vaccination_goat",
			RefID:   tenantID,
		},
		MediaRefs:      []string{"proof-1"},
		ShedID:         &shedID,
		ParkID:         &parkID,
		PartitionLabel: stringPtr("2"),
		CapturedAt:     time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey: "test:partitioned:item",
	}

	result, err := repo.CreateItem(ctx, in)
	if err != nil {
		t.Fatalf("CreateItem: %v", err)
	}

	// List the queue and verify operational_location_display
	items, err := repo.ListQueue(ctx, listQueueParams(tenantID, ""))
	if err != nil {
		t.Fatalf("ListQueue: %v", err)
	}

	if len(items) == 0 {
		t.Fatal("queue is empty, expected at least 1 item")
	}

	found := false
	for _, item := range items {
		if item.ItemID == result.Item.ItemID {
			found = true
			// Verify operational location display for partitioned shed
			if item.OperationalLocationDisplay == nil {
				t.Fatal("operational_location_display is nil, expected 'Castro 2'")
			}
			if *item.OperationalLocationDisplay != "Castro 2" {
				t.Fatalf("operational_location_display = %q, want 'Castro 2'", *item.OperationalLocationDisplay)
			}
			break
		}
	}

	if !found {
		t.Fatal("created item not found in queue")
	}
}

func TestListQueueEmitsOperationalLocationDisplay_NonPartitioned_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)

	// Create a park and a non-partitioned shed (Yashoda)
	var parkID, shedID string
	err := pool.QueryRow(ctx, `
INSERT INTO locations (tenant_id, location_id, location_type, name, status)
VALUES ($1::uuid, gen_random_uuid(), 'park', 'CBE', 'active')
RETURNING location_id::text
`, tenantID).Scan(&parkID)
	if err != nil {
		t.Fatalf("insert park: %v", err)
	}

	err = pool.QueryRow(ctx, `
INSERT INTO locations (tenant_id, location_id, location_type, name, status)
VALUES ($1::uuid, gen_random_uuid(), 'shed', 'Yashoda', 'active')
RETURNING location_id::text
`, tenantID).Scan(&shedID)
	if err != nil {
		t.Fatalf("insert shed: %v", err)
	}

	// Create a verification item WITHOUT partition_label (non-partitioned)
	in := domain.CreateItem{
		TenantID: tenantID,
		Vertical: "preventive_care",
		Module:   "vaccination",
		Category: "vaccination_proof",
		Source: domain.SourceRef{
			Module:  "vaccination",
			RefType: "vaccination_goat",
			RefID:   tenantID,
		},
		MediaRefs:  []string{"proof-1"},
		ShedID:     &shedID,
		ParkID:     &parkID,
		CapturedAt: time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey: "test:nonpartitioned:item",
	}

	result, err := repo.CreateItem(ctx, in)
	if err != nil {
		t.Fatalf("CreateItem: %v", err)
	}

	// List the queue and verify operational_location_display
	items, err := repo.ListQueue(ctx, listQueueParams(tenantID, ""))
	if err != nil {
		t.Fatalf("ListQueue: %v", err)
	}

	if len(items) == 0 {
		t.Fatal("queue is empty, expected at least 1 item")
	}

	found := false
	for _, item := range items {
		if item.ItemID == result.Item.ItemID {
			found = true
			// Verify operational location display for non-partitioned shed
			if item.OperationalLocationDisplay == nil {
				t.Fatal("operational_location_display is nil, expected 'Yashoda'")
			}
			if *item.OperationalLocationDisplay != "Yashoda" {
				t.Fatalf("operational_location_display = %q, want 'Yashoda'", *item.OperationalLocationDisplay)
			}
			break
		}
	}

	if !found {
		t.Fatal("created item not found in queue")
	}
}

func TestListQueueEmitsOperationalLocationDisplay_PrefixedPartition_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)

	// Create a park and a shed with prefixed partition (Godel 1 - Part 3)
	var parkID, shedID string
	err := pool.QueryRow(ctx, `
INSERT INTO locations (tenant_id, location_id, location_type, name, status)
VALUES ($1::uuid, gen_random_uuid(), 'park', 'CPT', 'active')
RETURNING location_id::text
`, tenantID).Scan(&parkID)
	if err != nil {
		t.Fatalf("insert park: %v", err)
	}

	err = pool.QueryRow(ctx, `
INSERT INTO locations (tenant_id, location_id, location_type, name, status)
VALUES ($1::uuid, gen_random_uuid(), 'shed', 'Godel 1', 'active')
RETURNING location_id::text
`, tenantID).Scan(&shedID)
	if err != nil {
		t.Fatalf("insert shed: %v", err)
	}

	// Create a verification item with prefixed partition label
	in := domain.CreateItem{
		TenantID: tenantID,
		Vertical: "preventive_care",
		Module:   "vaccination",
		Category: "vaccination_proof",
		Source: domain.SourceRef{
			Module:  "vaccination",
			RefType: "vaccination_goat",
			RefID:   tenantID,
		},
		MediaRefs:      []string{"proof-1"},
		ShedID:         &shedID,
		ParkID:         &parkID,
		PartitionLabel: stringPtr("Part 3"),
		CapturedAt:     time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey: "test:prefixed:partition",
	}

	result, err := repo.CreateItem(ctx, in)
	if err != nil {
		t.Fatalf("CreateItem: %v", err)
	}

	// List the queue and verify operational_location_display
	items, err := repo.ListQueue(ctx, listQueueParams(tenantID, ""))
	if err != nil {
		t.Fatalf("ListQueue: %v", err)
	}

	if len(items) == 0 {
		t.Fatal("queue is empty, expected at least 1 item")
	}

	found := false
	for _, item := range items {
		if item.ItemID == result.Item.ItemID {
			found = true
			// Verify operational location display for prefixed partition
			if item.OperationalLocationDisplay == nil {
				t.Fatal("operational_location_display is nil, expected 'Godel 1 - Part 3'")
			}
			if *item.OperationalLocationDisplay != "Godel 1 - Part 3" {
				t.Fatalf("operational_location_display = %q, want 'Godel 1 - Part 3'", *item.OperationalLocationDisplay)
			}
			break
		}
	}

	if !found {
		t.Fatal("created item not found in queue")
	}
}

func stringPtr(s string) *string {
	return &s
}

func listQueueParams(tenantID, category string) ports.ListQueueParams {
	return ports.ListQueueParams{
		TenantID:   tenantID,
		Status:     "",
		Category:   category,
		Limit:      100,
		Vertical:   "",
		Module:     "",
		ScopeRestricted: false,
	}
}
