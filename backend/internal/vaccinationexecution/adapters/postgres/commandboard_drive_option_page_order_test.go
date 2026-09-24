package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// TestDriveOptionsDateShiftPagesWalkTheWholeSortInOrder pins the drive picker's batch-first rewrite.
//
// The picker now walks obligation_batches in page order and expands each batch to its (park) pairs
// lazily, so the LIMIT stops after the page instead of folding every obligation in the tenant into
// pairs first. That is only correct if the lazily expanded rows come out in EXACTLY the tenant-wide
// order: status rank, planned date DESC (undated last), window DESC, batch, then park. The fixture
// shifts planned dates across batches of every status, gives one batch no date, one batch two
// parks, and one batch no obligations (never offered), then pages with limit 2 and asserts the
// concatenated pages equal the full expected order with no row dropped or repeated.
func TestDriveOptionsDateShiftPagesWalkTheWholeSortInOrder(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	tenantID := "00000000-0000-4000-8000-0000000000d7"
	parkA := "70000000-0000-4000-8000-0000010000d7"
	shedA := "70000000-0000-4000-8000-0000020000d7"
	protocolID := "70000000-0000-4000-8000-0000060000d0"
	protocolVersionID := "70000000-0000-4000-8000-0000060000d7"
	ruleID := "70000000-0000-4000-8000-0000070000d7"
	partyID := "70000000-0000-4000-8000-00000a0000d7"
	seedKPIFixtureBase(t, ctx, pool, tenantID, parkA, shedA, protocolID, protocolVersionID, ruleID, partyID, "d7")
	parkB := "70000000-0000-4000-8000-0000010000d8"
	shedB := "70000000-0000-4000-8000-0000020000d8"
	execProjectionSQL(t, ctx, pool, "park B",
		`INSERT INTO locations (location_id, tenant_id, name, location_type, parent_location_id, status) VALUES ($1, $2, 'Park d8', 'park', NULL, 'active')`, parkB, tenantID)
	execProjectionSQL(t, ctx, pool, "shed B",
		`INSERT INTO locations (location_id, tenant_id, name, location_type, parent_location_id, status) VALUES ($1, $2, 'Shed d8', 'shed', $3, 'active')`, shedB, tenantID, parkB)

	type batch struct {
		id, status string
		planned    *string
		sheds      []string
	}
	date := func(s string) *string { return &s }
	batches := []batch{
		{"70000000-0000-4000-8000-0000b00000d1", "planned", date("2026-07-10"), []string{shedA}},
		{"70000000-0000-4000-8000-0000b00000d2", "in_progress", date("2026-07-01"), []string{shedA, shedB}},
		{"70000000-0000-4000-8000-0000b00000d3", "completed", date("2026-06-20"), []string{shedA}},
		{"70000000-0000-4000-8000-0000b00000d4", "planned", date("2026-07-20"), []string{shedB}},
		{"70000000-0000-4000-8000-0000b00000d5", "planned", nil, []string{shedA}},
		{"70000000-0000-4000-8000-0000b00000d6", "in_progress", date("2026-07-05"), []string{shedB}},
		{"70000000-0000-4000-8000-0000b00000d7", "completed", date("2026-06-25"), []string{shedA}},
		{"70000000-0000-4000-8000-0000b00000d8", "planned", date("2026-07-15"), nil}, // no obligations: never offered
	}
	goatN := 0
	for _, b := range batches {
		execProjectionSQL(t, ctx, pool, "batch",
			`INSERT INTO obligation_batches (batch_id, tenant_id, protocol_version_id, scope_type, scope_id, planned_date, status)
			 VALUES ($1, $2, $3, 'shed', $4, $5::date, $6)`, b.id, tenantID, protocolVersionID, shedA, b.planned, b.status)
		for _, shed := range b.sheds {
			goatN++
			goatID := fmt.Sprintf("70000000-0000-4000-8000-0000030%05d", goatN)
			seedKPIGoat(t, ctx, pool, tenantID, shed, partyID, goatID)
			execProjectionSQL(t, ctx, pool, "obligation",
				`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, target_id, target_type, scope_type, scope_id, rule_id, status, due_at, idempotency_key, batch_id)
				 VALUES ($1, $2, $3, $4, 'goat', 'shed', $5, $6, 'scheduled', '2026-07-01T06:30:00Z', $7, $8)`,
				fmt.Sprintf("70000000-0000-4000-8000-0000080%05d", goatN), tenantID, protocolVersionID, goatID, shed, ruleID,
				fmt.Sprintf("page-order-%d", goatN), b.id)
		}
	}

	// in_progress (rank 0) by date DESC, then completed (1), then planned (2) with the undated one
	// last; the two-park batch is two rows, ordered by park name.
	want := []string{
		"d6|Park d8", "d2|Park d7", "d2|Park d8",
		"d7|Park d7", "d3|Park d7",
		"d4|Park d8", "d1|Park d7", "d5|Park d7",
	}
	repo := NewRepository(pool, 5*time.Second)
	var got []string
	cursor := ""
	for page := 0; page < 10; page++ {
		resp, err := repo.CommandBoardDriveOptions(ctx, domain.CommandBoardDriveOptionsQuery{TenantID: tenantID, Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		for _, option := range resp.Options {
			got = append(got, option.DriveBatchID[len(option.DriveBatchID)-2:]+"|"+option.ParkName)
		}
		if resp.NextCursor == "" {
			break
		}
		cursor = resp.NextCursor
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("paged drive options out of order or incomplete:\n got %v\nwant %v", got, want)
	}
}
