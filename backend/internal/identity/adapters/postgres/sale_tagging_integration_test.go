package postgres

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/identity/adapters/salesbridge"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// The tag-only Sales surface (maintainer decision 2026-09-11), proved on the real SQL: the rate
// weight typed per animal reads back one row per animal, and the park
// head's queue lists exactly the live animal sales still owed animals for the farms asked.

const (
	tagDealCBE      = "77777777-7777-4777-8777-777777777781"
	tagDealCPT      = "77777777-7777-4777-8777-777777777782"
	tagDealManure   = "77777777-7777-4777-8777-777777777783"
	tagDealFailed   = "77777777-7777-4777-8777-777777777784"
	tagDealComplete = "77777777-7777-4777-8777-777777777785"
)

func seedTaggingDeal(t *testing.T, ctx context.Context, pool *pgxpool.Pool, dealID, date, farm, product, status string, animals int) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO sales_deals (id, tenant_id, sale_date, farm, buyer_name, product_type, breed, animal_count, sales_value, status)
VALUES ($1::uuid, $2::uuid, $3::date, $4, 'Synthetic buyer', $5, 'Boer', $6, 1000, $7)
ON CONFLICT (id) DO UPDATE SET animal_count = EXCLUDED.animal_count, status = EXCLUDED.status`,
		dealID, ssTenant, date, farm, product, animals, status); err != nil {
		t.Fatalf("seed tagging deal: %v", err)
	}
}

// WEIGHT PER ANIMAL, READ BACK ONE ROW PER ANIMAL. The park head resuming a half-tagged sale reads
// these; each weight comes back as the decimal string stored, beside the snapshotted pen.
func TestTheWeightPerAnimalReadsBackOneRowPerAnimal(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool, repo := startCorrectionWriteDB(t, ctx)
	defer pool.Close()
	f := seedShedStageFixture(t, ctx, pool)

	one := seedStageGoat(t, ctx, pool, f.castroShed, "1", "F2", "adult")
	two := seedStageGoat(t, ctx, pool, f.castroShed, "1", "F2", "adult")
	seedSaleAllocationDeal(t, ctx, pool, saleDealA, 2)

	if _, err := repo.RecordSaleAllocations(ctx, saleAllocCmd(saleDealA, []ports.SaleAllocationRow{
		{GoatID: one, RowVersion: goatRowVersion(t, pool, one), WeightKg: "32.5"},
		{GoatID: two, RowVersion: goatRowVersion(t, pool, two), WeightKg: "41"},
	}, "confirm-weight")); err != nil {
		t.Fatalf("RecordSaleAllocations: %v", err)
	}

	animals, err := repo.ListSaleAllocationAnimals(ctx, ssTenant, saleDealA)
	if err != nil {
		t.Fatalf("ListSaleAllocationAnimals: %v", err)
	}
	if len(animals) != 2 {
		t.Fatalf("read back %d animals, want 2: %+v", len(animals), animals)
	}
	byID := map[string]ports.SaleAllocationAnimal{}
	for _, a := range animals {
		byID[a.GoatID] = a
	}
	if got := byID[one]; got.WeightKg != "32.50" {
		t.Fatalf("animal one weight = %q, want 32.50", got.WeightKg)
	}
	if got := byID[two]; got.WeightKg != "41.00" {
		t.Fatalf("animal two weight = %q, want 41.00", got.WeightKg)
	}
	// The display is the canonical composition, never a hand-rolled join.
	if byID[one].OperationalLocationDisplay != "Castro 1" {
		t.Fatalf("display = %q, want Castro 1", byID[one].OperationalLocationDisplay)
	}
}

// THE QUEUE, EVERY STATUS. Live animal sales still owed animals, for the farms asked, newest
// first, keyset-paged. Manure, a failed deal and a fully tagged sale are OUT; another farm's sale
// is out when a farm filter is given and in when none is.
func TestTaggingQueueListsOnlyLiveSalesStillOwedAnimalsForTheFarmsAsked(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool, repo := startCorrectionWriteDB(t, ctx)
	defer pool.Close()
	f := seedShedStageFixture(t, ctx, pool)

	seedTaggingDeal(t, ctx, pool, tagDealCBE, "2026-09-10", "CBE", "Goat", "Deal Closed", 2)
	seedTaggingDeal(t, ctx, pool, tagDealCPT, "2026-09-09", "CPT", "Sheep", "Advance Paid", 3)
	seedTaggingDeal(t, ctx, pool, tagDealManure, "2026-09-11", "CBE", "Manure", "Deal Closed", 5)
	seedTaggingDeal(t, ctx, pool, tagDealFailed, "2026-09-11", "CBE", "Goat", "Deal Failed", 4)
	seedTaggingDeal(t, ctx, pool, tagDealComplete, "2026-09-11", "CBE", "Goat", "Deal Closed", 1)
	done := seedStageGoat(t, ctx, pool, f.castroShed, "1", "F2", "adult")
	if _, err := repo.RecordSaleAllocations(ctx, saleAllocCmd(tagDealComplete, []ports.SaleAllocationRow{
		{GoatID: done, RowVersion: goatRowVersion(t, pool, done), WeightKg: "30"},
	}, "confirm-complete")); err != nil {
		t.Fatalf("complete the one-animal sale: %v", err)
	}

	bridge := salesbridge.New(pool)

	cbe, next, err := bridge.ListSaleTaggingDeals(ctx, ssTenant, []string{"CBE"}, 20, "")
	if err != nil {
		t.Fatalf("CBE queue: %v", err)
	}
	if len(cbe) != 1 || cbe[0].SalesDealID != tagDealCBE || cbe[0].Remaining() != 2 || next != nil {
		t.Fatalf("CBE queue = %+v next=%v, want only the open CBE goat sale with 2 remaining", cbe, next)
	}

	all, _, err := bridge.ListSaleTaggingDeals(ctx, ssTenant, nil, 20, "")
	if err != nil {
		t.Fatalf("tenant-wide queue: %v", err)
	}
	if len(all) != 2 || all[0].SalesDealID != tagDealCBE || all[1].SalesDealID != tagDealCPT {
		t.Fatalf("tenant-wide queue = %+v, want CBE (newer) then CPT", all)
	}

	// Pagination: page size 1 walks the same two rows through the cursor, none repeated.
	first, cursor, err := bridge.ListSaleTaggingDeals(ctx, ssTenant, nil, 1, "")
	if err != nil || cursor == nil || len(first) != 1 || first[0].SalesDealID != tagDealCBE {
		t.Fatalf("page 1 = %+v cursor=%v err=%v", first, cursor, err)
	}
	second, last, err := bridge.ListSaleTaggingDeals(ctx, ssTenant, nil, 1, *cursor)
	if err != nil || last != nil || len(second) != 1 || second[0].SalesDealID != tagDealCPT {
		t.Fatalf("page 2 = %+v cursor=%v err=%v", second, last, err)
	}
	// The farm read that clamps a park-scoped caller to their own park's sales.
	if farm, err := bridge.ReadSaleDealFarm(ctx, ssTenant, tagDealCPT); err != nil || farm != "CPT" {
		t.Fatalf("ReadSaleDealFarm(CPT deal) = %q, %v; want CPT", farm, err)
	}
	if _, err := bridge.ReadSaleDealFarm(ctx, ssTenant, "77777777-7777-4777-8777-777777777799"); err != ports.ErrSaleDealNotFound {
		t.Fatalf("ReadSaleDealFarm(unknown deal) err = %v, want ErrSaleDealNotFound", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM sales_deals WHERE tenant_id=$1::uuid AND id = ANY($2::uuid[])`,
		ssTenant, []string{tagDealCBE, tagDealCPT, tagDealManure, tagDealFailed}); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
}
