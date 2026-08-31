package postgres

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
)

// seedLoadwiseFixture builds the ADVERSARIAL load-wise world the grain proof in
// loadwise_repository.go claims to survive:
//
//   - Load A (CPT, 2026-08-01, vendor "Sardar Traders"): five ACCEPTED animals — one sold WITH a
//     tagged deal share, one sold WITHOUT any allocation (direct exit), one dead, one alive, one
//     merged (the unaccounted case). Plus one load-goat row that never reached intake
//     (source_rejected) and must not count at all.
//   - Load B (mixed parks, 2026-08-05): two accepted animals — one sold with a share, one alive in
//     the OTHER park, so the load's farm label must go BARE (agree-or-go-bare).
//   - The one-goat-two-accepted-rows case: load B's sold animal also carries an ACCEPTED row on
//     load A with an OLDER intake timestamp; DISTINCT ON must count it once, on load B only.
//   - One deal worth 30000 with THREE tagged animals (one from each load plus one FARM-BORN goat
//     with no load at all), so the per-animal share is 10000 and the farm-born share reaches the
//     overall average but no load row.
//   - A RELEASED allocation on the same deal that must not dilute the share.
type loadwiseFixture struct {
	loadA, loadB string
}

func seedLoadwiseFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) loadwiseFixture {
	t.Helper()

	cptPark := parkIDByCode(t, ctx, pool, "CPT")
	cbePark := parkIDByCode(t, ctx, pool, "CBE")

	var vendorParty string
	if err := pool.QueryRow(ctx, `
INSERT INTO parties (party_type, display_name, status)
VALUES ('org', 'Sardar Traders', 'active')
RETURNING party_id::text`).Scan(&vendorParty); err != nil {
		t.Fatalf("seed vendor party: %v", err)
	}

	var fx loadwiseFixture
	if err := pool.QueryRow(ctx, `
INSERT INTO procurement_loads (tenant_id, source_party_id, purchase_date, status, idempotency_key)
VALUES ($1, $2, '2026-08-01', 'accepted_intake', 'lw-load-a')
RETURNING load_id::text`, testTenant, vendorParty).Scan(&fx.loadA); err != nil {
		t.Fatalf("seed load A: %v", err)
	}
	if err := pool.QueryRow(ctx, `
INSERT INTO procurement_loads (tenant_id, source_party_id, purchase_date, status, idempotency_key)
VALUES ($1, $2, '2026-08-05', 'accepted_intake', 'lw-load-b')
RETURNING load_id::text`, testTenant, vendorParty).Scan(&fx.loadB); err != nil {
		t.Fatalf("seed load B: %v", err)
	}

	// The animals. sold-with-share / sold-no-deal / dead / alive / merged on A; sold-with-share /
	// alive on B; plus a farm-born sold animal with no load row.
	goat := func(lifecycle, exitReason string, park string) string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx, `
INSERT INTO goats (tenant_id, sex, lifecycle_status, exit_reason, custodian_party_id, park_id)
VALUES ($1, 'female', $2, nullif($3, ''), $4, $5::uuid)
RETURNING goat_id::text`, testTenant, lifecycle, exitReason, vendorParty, park).Scan(&id); err != nil {
			t.Fatalf("seed goat: %v", err)
		}
		return id
	}
	soldA := goat("sold", "sold", cptPark)
	soldNoDealA := goat("sold", "sold", cptPark)
	deadA := goat("dead", "died", cptPark)
	aliveA := goat("alive", "", cptPark)
	mergedA := goat("merged", "", cptPark)
	rejectedNeverAccepted := goat("alive", "", cptPark)
	soldB := goat("sold", "sold", cbePark)
	aliveB := goat("alive", "", cptPark)
	farmBornSold := goat("sold", "sold", cbePark)

	accept := func(load, goatID, acceptedAt string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `
INSERT INTO procurement_load_goats (tenant_id, load_id, goat_id, selection_state, current_state, intake_accepted_at)
VALUES ($1, $2::uuid, $3::uuid, 'accepted_herd_intake', 'accepted_herd_intake', $4::timestamptz)`,
			testTenant, load, goatID, acceptedAt); err != nil {
			t.Fatalf("seed load goat: %v", err)
		}
	}
	accept(fx.loadA, soldA, "2026-08-01T10:00:00Z")
	accept(fx.loadA, soldNoDealA, "2026-08-01T10:00:00Z")
	accept(fx.loadA, deadA, "2026-08-01T10:00:00Z")
	accept(fx.loadA, aliveA, "2026-08-01T10:00:00Z")
	accept(fx.loadA, mergedA, "2026-08-01T10:00:00Z")
	// The dedupe case: soldB accepted on BOTH loads; the newer acceptance (load B) must win.
	accept(fx.loadA, soldB, "2026-08-01T10:00:00Z")
	accept(fx.loadB, soldB, "2026-08-05T10:00:00Z")
	accept(fx.loadB, aliveB, "2026-08-05T10:00:00Z")
	// A pipeline reject that never reached intake: present on the load, counts nowhere.
	if _, err := pool.Exec(ctx, `
INSERT INTO procurement_load_goats (tenant_id, load_id, goat_id, selection_state, current_state, exit_reason)
VALUES ($1, $2::uuid, $3::uuid, 'rejected', 'source_rejected', 'canceled')`,
		testTenant, fx.loadA, rejectedNeverAccepted); err != nil {
		t.Fatalf("seed rejected load goat: %v", err)
	}

	// One deal, 30000, three TAGGED animals (share 10000 each) + one RELEASED allocation that
	// must not dilute the share.
	var dealID string
	if err := pool.QueryRow(ctx, `
INSERT INTO sales_deals (tenant_id, sale_date, farm, buyer_name, product_type, breed, animal_count, sales_value)
VALUES ($1, '2026-08-20', 'CPT', 'Loadwise Buyer', 'Sheep', 'Nari Suvarna', 3, 30000)
RETURNING id::text`, testTenant).Scan(&dealID); err != nil {
		t.Fatalf("seed deal: %v", err)
	}
	tag := func(goatID, status, key string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `
INSERT INTO goat_sale_allocations (tenant_id, goat_id, sales_deal_id, status, idempotency_key, released_at, release_reason)
VALUES ($1, $2::uuid, $3::uuid, $4, $5,
        CASE WHEN $4 = 'released' THEN now() END,
        CASE WHEN $4 = 'released' THEN 'test release' END)`,
			testTenant, goatID, dealID, status, key); err != nil {
			t.Fatalf("seed allocation: %v", err)
		}
	}
	tag(soldA, "tagged", "lw-alloc-a")
	tag(soldB, "tagged", "lw-alloc-b")
	tag(farmBornSold, "tagged", "lw-alloc-farm-born")
	tag(soldNoDealA, "released", "lw-alloc-released")

	return fx
}

// TestLoadwiseSalesPostgresRead exercises the load-wise reconciliation against a real Postgres,
// because every rule it pins — the accepted-only membership, the DISTINCT ON dedupe, the disjoint
// outcome buckets, the per-deal share pre-aggregation, agree-or-go-bare farm labeling — lives in
// the SQL, where a fake repository cannot see it.
func TestLoadwiseSalesPostgresRead(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	fx := seedLoadwiseFixture(t, ctx, pool)

	repo := NewRepository(pool, 10*time.Second)

	read := func() (loadA, loadB domain.LoadwiseLoad, out domain.LoadwiseSales) {
		t.Helper()
		out, err := repo.LoadwiseSales(ctx, testTenant, 60)
		if err != nil {
			t.Fatalf("loadwise sales: %v", err)
		}
		var foundA, foundB bool
		for _, l := range out.Loads {
			switch l.LoadID {
			case fx.loadA:
				loadA, foundA = l, true
			case fx.loadB:
				loadB, foundB = l, true
			}
		}
		if !foundA || !foundB {
			t.Fatalf("loads missing from read: %+v", out.Loads)
		}
		return loadA, loadB, out
	}

	loadA, loadB, out := read()

	// Load B first (newest purchase date first).
	if out.Loads[0].LoadID != fx.loadB {
		t.Fatalf("order: newest purchase first, got %+v", out.Loads[0])
	}

	t.Run("StatusBucketsPartitionPurchasedDisjointly", func(t *testing.T) {
		// Load A: 5 accepted (the rejected row counts nowhere, the re-accepted-elsewhere animal
		// counts once, on the other load). sold + mortality + other + remaining + unaccounted
		// must equal purchased exactly — the merged animal is the unaccounted case.
		if loadA.Purchased != 5 || loadA.Sold != 2 || loadA.Mortality != 1 || loadA.OtherExits != 0 ||
			loadA.Remaining != 1 || loadA.Unaccounted != 1 {
			t.Fatalf("load A counts = %+v", loadA)
		}
		// The dedupe (one goat, two accepted rows): the animal counts on load B, not load A.
		if loadB.Purchased != 2 || loadB.Sold != 1 || loadB.Remaining != 1 || loadB.Unaccounted != 0 {
			t.Fatalf("load B counts = %+v", loadB)
		}
	})

	t.Run("OneToManyDealSharesStayPerAnimal", func(t *testing.T) {
		// One deal, three tagged animals across two loads and a farm-born sale: the value divides
		// per animal (10000), each load receives exactly its own animals' shares, the released
		// allocation dilutes nothing, and the sold-without-deal animal stays unpriced.
		if math.Abs(loadA.SoldValue-10000) > 0.01 || loadA.SoldPriced != 1 {
			t.Fatalf("load A sold value = %v priced %d, want 10000 over 1", loadA.SoldValue, loadA.SoldPriced)
		}
		if math.Abs(loadB.SoldValue-10000) > 0.01 || loadB.SoldPriced != 1 {
			t.Fatalf("load B sold value = %v priced %d", loadB.SoldValue, loadB.SoldPriced)
		}
		if loadA.PriceBasis != domain.LoadwisePriceBasisLoad || loadA.AvgSoldPrice == nil || math.Abs(*loadA.AvgSoldPrice-10000) > 0.01 {
			t.Fatalf("load A price basis = %s avg %v, want its own 10000", loadA.PriceBasis, loadA.AvgSoldPrice)
		}
		if loadA.RemainingValue == nil || math.Abs(*loadA.RemainingValue-10000) > 0.01 {
			t.Fatalf("load A remaining value = %v, want 1 x 10000", loadA.RemainingValue)
		}
		if loadA.PurchaseValue != nil {
			t.Fatalf("load A purchase value = %v, want ABSENT while no cost is recorded", loadA.PurchaseValue)
		}
		// Overall average: three tagged shares of 10000 (the farm-born sale included; the
		// released allocation excluded from both the share and the average).
		if out.OverallAvgSoldPrice == nil || math.Abs(*out.OverallAvgSoldPrice-10000) > 0.01 {
			t.Fatalf("overall avg = %v, want 10000", out.OverallAvgSoldPrice)
		}
	})

	t.Run("ParkScopeAgreeOrGoBareFarmLabel", func(t *testing.T) {
		if loadA.Farm != "CPT" {
			t.Fatalf("load A farm = %q, want CPT (every accepted animal agrees)", loadA.Farm)
		}
		if loadB.Farm != "" {
			t.Fatalf("load B farm = %q, want bare for a park mix", loadB.Farm)
		}
	})

	t.Run("PageBoundaryWindowKeepsWholeTenantTotals", func(t *testing.T) {
		if out.TotalLoads != 2 {
			t.Fatalf("total loads = %d", out.TotalLoads)
		}
		// The summary sums exactly the served rows.
		if out.Summary.Purchased != 7 || out.Summary.Sold != 3 || out.Summary.Mortality != 1 ||
			out.Summary.Remaining != 2 || out.Summary.Unaccounted != 1 {
			t.Fatalf("summary = %+v", out.Summary)
		}
		// A one-load window serves only the newest load while total_loads still reports both, so
		// the screen can say older loads are not shown.
		windowed, err := repo.LoadwiseSales(ctx, testTenant, 1)
		if err != nil {
			t.Fatalf("windowed read: %v", err)
		}
		if len(windowed.Loads) != 1 || windowed.Loads[0].LoadID != fx.loadB || windowed.TotalLoads != 2 {
			t.Fatalf("windowed = %d loads first %s total %d", len(windowed.Loads), windowed.Loads[0].LoadID, windowed.TotalLoads)
		}
	})

	t.Run("records a landed cost and reads it back as the purchase value", func(t *testing.T) {
		edit := domain.LoadCostEdit{AnimalCost: lwf(50000), TransportCost: lwf(2000)}
		if err := repo.SetLoadCost(ctx, testTenant, fx.loadB, edit, ""); err != nil {
			t.Fatalf("set load cost: %v", err)
		}
		_, loadB, _ := read()
		if loadB.PurchaseValue == nil || math.Abs(*loadB.PurchaseValue-52000) > 0.01 {
			t.Fatalf("load B purchase value = %v, want 52000", loadB.PurchaseValue)
		}
		version := loadB.RowVersion

		// Writing the SAME values changes nothing: naturally idempotent, no version bump.
		if err := repo.SetLoadCost(ctx, testTenant, fx.loadB, edit, ""); err != nil {
			t.Fatalf("replay set load cost: %v", err)
		}
		_, loadB, _ = read()
		if loadB.RowVersion != version {
			t.Fatalf("row version moved on an unchanged write: %d -> %d", version, loadB.RowVersion)
		}

		// Clearing returns the load to cost-not-recorded, never zero.
		if err := repo.SetLoadCost(ctx, testTenant, fx.loadB, domain.LoadCostEdit{}, ""); err != nil {
			t.Fatalf("clear load cost: %v", err)
		}
		_, loadB, _ = read()
		if loadB.PurchaseValue != nil || loadB.AnimalCost != nil {
			t.Fatalf("cleared cost must read as absent, got %+v", loadB)
		}
	})

	t.Run("an unknown load reads as not found", func(t *testing.T) {
		err := repo.SetLoadCost(ctx, testTenant, "00000000-0000-4000-8000-00000000dead", domain.LoadCostEdit{AnimalCost: lwf(1)}, "")
		if !errors.Is(err, ports.ErrLoadNotFound) {
			t.Fatalf("err = %v, want ErrLoadNotFound", err)
		}
	})
}

// lwf is this file's optional-money literal helper (feed purchase tests own f64).
func lwf(v float64) *float64 { return &v }
