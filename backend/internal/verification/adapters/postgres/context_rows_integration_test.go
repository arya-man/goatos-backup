package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/verification/domain"
	"github.com/vgoats/goatos/backend/internal/verification/ports"
)

// context_rows must survive a REAL round trip: insert, then read back through both item read paths.
//
// A pure-Go test on the mapper would pass while the column was missing from a SELECT list or the
// scan destinations were one short -- the latter is a RUNTIME failure ("number of field descriptions
// must equal number of destinations") that only a live query surfaces. This module has already
// shipped a partition column, a decoder, an OpenAPI field and two client DTOs that no query touched;
// asserting on the returned VALUE after a DB round trip is what stops that repeating.
// newPark inserts a real park because the queue read requires park_id IS NOT NULL -- an item with
// no park is invisible to it, which is a property of the queue rather than of this feature.
func newPark(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string) string {
	t.Helper()
	var parkID string
	err := pool.QueryRow(ctx,
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
		 VALUES (gen_random_uuid(), $1::uuid, 'park', 'CTX_TEST_PARK', 'Context Test Park', 'active')
		 RETURNING location_id::text`, tenantID).Scan(&parkID)
	if err != nil {
		t.Fatalf("insert park: %v", err)
	}
	return parkID
}

func TestContextRowsRoundTripThroughBothReadPaths_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)
	parkID := newPark(t, ctx, pool, tenantID)

	want := []domain.ContextRow{
		{Label: "Expected ration", Value: "Maize 12.5 kg · Soya 4 kg"},
		{Label: "Animals in this pen", Value: "38"},
	}
	created, err := repo.CreateItem(ctx, domain.CreateItem{
		TenantID: tenantID,
		Vertical: "feed",
		Module:   "feed",
		Category: "feed_packing",
		Source: domain.SourceRef{
			Module:  "feed",
			RefType: "feed_packing_completion",
			RefID:   tenantID,
		},
		ContextRows:    want,
		ParkID:         &parkID,
		MediaRefs:      []string{"proof-1"},
		CapturedAt:     time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey: "feed-packing-verification:ctx-1",
	})
	if err != nil {
		t.Fatalf("CreateItem: %v", err)
	}

	// Read path 1: the plain item read (itemColumns / scanItem).
	got, err := repo.GetItem(ctx, tenantID, created.Item.ItemID)
	if err != nil {
		t.Fatalf("GetItem: %v", err)
	}
	assertContextRows(t, "GetItem", got.ContextRows, want)

	// Read path 2: the queue read, which uses a DIFFERENT column list and scan function
	// (itemColumnsWithLabels / scanItemWithLabels). The two drifting apart is exactly the defect
	// this asserts against -- one list carrying the column and the other not.
	rows, err := repo.ListQueue(ctx, ports.ListQueueParams{TenantID: tenantID, Status: domain.StatusPending, Limit: 10})
	if err != nil {
		t.Fatalf("ListQueue: %v", err)
	}
	if len(rows) == 0 {
		t.Fatalf("ListQueue returned no rows for the item just created")
	}
	assertContextRows(t, "ListQueue", rows[0].ContextRows, want)
}

// A producer attaching nothing must read back as an EMPTY array, never null: the column's CHECK
// requires a json array, and a nil slice marshals to `null`.
func TestContextRowsDefaultToEmptyArray_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)

	created, err := repo.CreateItem(ctx, domain.CreateItem{
		TenantID: tenantID,
		Vertical: "feed",
		Module:   "feed",
		Category: "feed_packing",
		Source: domain.SourceRef{
			Module:  "feed",
			RefType: "feed_packing_completion",
			RefID:   tenantID,
		},
		MediaRefs:      []string{"proof-1"},
		CapturedAt:     time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey: "feed-packing-verification:ctx-empty",
	})
	if err != nil {
		t.Fatalf("CreateItem with no context rows: %v", err)
	}

	var stored string
	if err := pool.QueryRow(ctx,
		`SELECT context_rows::text FROM verification_items WHERE tenant_id = $1::uuid AND item_id = $2::uuid`,
		tenantID, created.Item.ItemID).Scan(&stored); err != nil {
		t.Fatalf("read stored context_rows: %v", err)
	}
	if stored != "[]" {
		t.Errorf("stored context_rows = %q, want %q -- null fails the array CHECK", stored, "[]")
	}
}

func assertContextRows(t *testing.T, path string, got, want []domain.ContextRow) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: context rows = %+v, want %+v", path, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s: row %d = %+v, want %+v (order is the producer's and must be preserved)", path, i, got[i], want[i])
		}
	}
}
