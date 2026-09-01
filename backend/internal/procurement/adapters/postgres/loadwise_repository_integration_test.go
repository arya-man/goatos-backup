package postgres

import (
	"context"
	"errors"
	"math"
	"strings"
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
	loadA, loadB, loadC string
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
INSERT INTO procurement_loads (tenant_id, source_party_id, purchase_date, status, expected_count, idempotency_key)
VALUES ($1, $2, '2026-08-05', 'accepted_intake', 9, 'lw-load-b')
RETURNING load_id::text`, testTenant, vendorParty).Scan(&fx.loadB); err != nil {
		t.Fatalf("seed load B: %v", err)
	}

	// Load C exists ONLY for the agree-or-go-bare edge: one animal in a known park and one whose
	// park is unknown. count(DISTINCT) skips NULLs, so this load would otherwise claim the known
	// park and assert an agreement that was never established.
	if err := pool.QueryRow(ctx, `
INSERT INTO procurement_loads (tenant_id, source_party_id, purchase_date, status, expected_count, idempotency_key)
VALUES ($1, $2, '2026-08-09', 'accepted_intake', 2, 'lw-load-c')
RETURNING load_id::text`, testTenant, vendorParty).Scan(&fx.loadC); err != nil {
		t.Fatalf("seed load C: %v", err)
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

	// Load C: one animal in CPT, one with NO park at all (goats.park_id is nullable).
	knownParkC := goat("alive", "", cptPark)
	var unknownParkC string
	if err := pool.QueryRow(ctx, `
INSERT INTO goats (tenant_id, sex, lifecycle_status, custodian_party_id, park_id)
VALUES ($1, 'female', 'alive', $2, NULL)
RETURNING goat_id::text`, testTenant, vendorParty).Scan(&unknownParkC); err != nil {
		t.Fatalf("seed unknown-park goat: %v", err)
	}
	accept(fx.loadC, knownParkC, "2026-08-09T10:00:00Z")
	accept(fx.loadC, unknownParkC, "2026-08-09T10:00:00Z")
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

	// Load B's PRE-GOATOS history: 3 already sold for 30000 and 2 already dead before its
	// remaining animals were tracked here. The read must fold both into the reconciliation.
	if _, err := pool.Exec(ctx, `
INSERT INTO procurement_load_prior_outcomes (tenant_id, load_id, outcome, animal_count, sales_value, first_on, last_on, source_ref)
VALUES ($1, $2::uuid, 'sold', 3, 30000, '2026-04-30', '2026-05-20', 'test fixture'),
       ($1, $2::uuid, 'died', 2, NULL, '2025-11-24', '2025-11-24', 'test fixture')`,
		testTenant, fx.loadB); err != nil {
		t.Fatalf("seed prior outcomes: %v", err)
	}

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

	loadByID := func(out domain.LoadwiseSales, id string) domain.LoadwiseLoad {
		t.Helper()
		for _, l := range out.Loads {
			if l.LoadID == id {
				return l
			}
		}
		t.Fatalf("load %s missing from the read", id)
		return domain.LoadwiseLoad{}
	}

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

	// Newest purchase date first: C (2026-08-09), then B (08-05), then A (08-01).
	if len(out.Loads) < 3 || out.Loads[0].LoadID != fx.loadC ||
		out.Loads[1].LoadID != fx.loadB || out.Loads[2].LoadID != fx.loadA {
		t.Fatalf("order: newest purchase first, got %v",
			[]string{out.Loads[0].LoadID, out.Loads[1].LoadID, out.Loads[2].LoadID})
	}

	t.Run("StatusBucketsPartitionPurchasedDisjointly", func(t *testing.T) {
		// Load A: 5 accepted (the rejected row counts nowhere, the re-accepted-elsewhere animal
		// counts once, on the other load). sold + mortality + other + remaining + unaccounted
		// must equal purchased exactly — the merged animal is the unaccounted case.
		if loadA.Purchased != 5 || loadA.Sold != 2 || loadA.Mortality != 1 || loadA.OtherExits != 0 ||
			loadA.Remaining != 1 || loadA.Unaccounted != 1 {
			t.Fatalf("load A counts = %+v", loadA)
		}
		// The dedupe (one goat, two accepted rows): the animal counts on load B, not load A —
		// plus load B's pre-GoatOS history folded in: 2 tracked + 3 already sold + 2 already dead.
		// Load B DECLARES 9 animals, so the 2 it cannot account for surface as Unaccounted rather
		// than being absorbed into the denominator.
		if loadB.DeclaredCount != 9 || loadB.Purchased != 9 || loadB.Sold != 4 || loadB.Mortality != 2 ||
			loadB.Remaining != 1 || loadB.Unaccounted != 2 {
			t.Fatalf("load B counts = %+v", loadB)
		}
		if loadB.PriorSold.Count != 3 || loadB.PriorSold.FirstOn != "2026-04-30" || loadB.PriorSold.LastOn != "2026-05-20" {
			t.Fatalf("load B prior sold = %+v, want the seeded dated history", loadB.PriorSold)
		}
		if loadB.PriorDead.Count != 2 || loadB.PriorDead.FirstOn != "2025-11-24" {
			t.Fatalf("load B prior dead = %+v", loadB.PriorDead)
		}
	})

	t.Run("OneToManyDealSharesStayPerAnimal", func(t *testing.T) {
		// One deal, three tagged animals across two loads and a farm-born sale: the value divides
		// per animal (10000), each load receives exactly its own animals' shares, the released
		// allocation dilutes nothing, and the sold-without-deal animal stays unpriced.
		if math.Abs(loadA.SoldValue-10000) > 0.01 || loadA.SoldPriced != 1 {
			t.Fatalf("load A sold value = %v priced %d, want 10000 over 1", loadA.SoldValue, loadA.SoldPriced)
		}
		// Load B: the live allocation share (10000) PLUS the prior revenue (30000).
		if math.Abs(loadB.SoldValue-40000) > 0.01 || loadB.SoldPriced != 4 {
			t.Fatalf("load B sold value = %v priced %d, want 40000 over 4", loadB.SoldValue, loadB.SoldPriced)
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

	t.Run("CostLinesRollUpOverStoredColumns", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `
UPDATE procurement_loads
SET animal_cost = 1000, transport_cost = NULL, other_cost = NULL
WHERE tenant_id = $1 AND load_id = $2::uuid;

INSERT INTO procurement_load_cost_lines (tenant_id, load_id, kind, amount, source)
VALUES ($1, $2::uuid, 'animal', 2000, 'sheet_import'),
       ($1, $2::uuid, 'transport', 300, 'sheet_import'),
       ($1, $2::uuid, 'booking', 25, 'sheet_import'),
       ($1, $2::uuid, 'labour', 75, 'sheet_import')`, testTenant, fx.loadA); err != nil {
			t.Fatalf("seed cost lines: %v", err)
		}
		nextA, _, _ := read()
		if nextA.AnimalCost == nil || *nextA.AnimalCost != 2000 {
			t.Fatalf("animal cost = %v, want line roll-up 2000", nextA.AnimalCost)
		}
		if nextA.TransportCost == nil || *nextA.TransportCost != 300 {
			t.Fatalf("transport cost = %v, want line roll-up 300", nextA.TransportCost)
		}
		if nextA.OtherCost == nil || *nextA.OtherCost != 100 {
			t.Fatalf("other cost = %v, want booking+labour roll-up 100", nextA.OtherCost)
		}
		if nextA.PurchaseValue == nil || *nextA.PurchaseValue != 2400 {
			t.Fatalf("purchase value = %v, want landed cost from lines 2400", nextA.PurchaseValue)
		}
	})

	t.Run("ParkScopeAgreeOrGoBareFarmLabel", func(t *testing.T) {
		if loadA.Farm != "CPT" {
			t.Fatalf("load A farm = %q, want CPT (every accepted animal agrees)", loadA.Farm)
		}
		if loadB.Farm != "" {
			t.Fatalf("load B farm = %q, want bare for a park mix", loadB.Farm)
		}
		// An UNKNOWN park is a disagreement, not an abstention. count(DISTINCT) skips NULLs, so
		// this load reported its one known park as unanimous until the read counted the animals
		// that actually named one.
		loadC := loadByID(out, fx.loadC)
		if loadC.Purchased != 2 {
			t.Fatalf("load C purchased = %d, want both animals attributed", loadC.Purchased)
		}
		if loadC.Farm != "" {
			t.Fatalf("load C farm = %q, want BARE: one animal names CPT and the other names no park at all", loadC.Farm)
		}
	})

	t.Run("PageBoundaryWindowKeepsWholeTenantTotals", func(t *testing.T) {
		if out.TotalLoads != 3 {
			t.Fatalf("total loads = %d", out.TotalLoads)
		}
		// The summary sums exactly the served rows, prior history included.
		if out.Summary.Purchased != 16 || out.Summary.Sold != 6 || out.Summary.Mortality != 3 ||
			out.Summary.Remaining != 4 || out.Summary.Unaccounted != 3 {
			t.Fatalf("summary = %+v", out.Summary)
		}
		// A one-load window serves only the newest load while total_loads still reports both, so
		// the screen can say older loads are not shown.
		windowed, err := repo.LoadwiseSales(ctx, testTenant, 1)
		if err != nil {
			t.Fatalf("windowed read: %v", err)
		}
		if len(windowed.Loads) != 1 || windowed.Loads[0].LoadID != fx.loadC || windowed.TotalLoads != 3 {
			t.Fatalf("windowed = %d loads first %s total %d", len(windowed.Loads), windowed.Loads[0].LoadID, windowed.TotalLoads)
		}
	})

	t.Run("OverdueCandidatesAreNotClippedByNewestPageWindow", func(t *testing.T) {
		candidates, err := repo.OverdueLoadCandidates(ctx, testTenant, "2026-11-05")
		if err != nil {
			t.Fatalf("overdue candidates: %v", err)
		}
		if len(candidates) != 1 || candidates[0].LoadID != fx.loadA {
			t.Fatalf("overdue candidates = %+v, want only older open load A", candidates)
		}

		windowed, err := repo.LoadwiseSales(ctx, testTenant, 1)
		if err != nil {
			t.Fatalf("windowed read: %v", err)
		}
		if len(windowed.Loads) != 1 || windowed.Loads[0].LoadID != fx.loadC {
			t.Fatalf("windowed read = %+v, want newest load C", windowed.Loads)
		}
		if candidates[0].LoadID == windowed.Loads[0].LoadID {
			t.Fatalf("overdue candidate unexpectedly came from the one-row newest page")
		}
	})

	t.Run("DuplicateAcceptedRowsResolveToOneStableLoad", func(t *testing.T) {
		// The dedupe must be a TOTAL order, not merely a business-preferred one. intake_accepted_at
		// is nullable and AcceptIntake stamps one timestamp per BATCH, so two accepted rows for the
		// same goat can tie or both be null -- and ordering on that column alone lets Postgres
		// return either load per read, moving the animal's outcome between loads.
		var loadD, loadE string
		for _, seed := range []struct {
			target *string
			key    string
		}{{&loadD, "lw-load-d"}, {&loadE, "lw-load-e"}} {
			if err := pool.QueryRow(ctx, `
INSERT INTO procurement_loads (tenant_id, source_party_id, purchase_date, status, expected_count, idempotency_key)
VALUES ($1, (SELECT party_id FROM parties WHERE display_name = 'Sardar Traders' LIMIT 1),
        '2026-08-11', 'accepted_intake', 1, $2)
RETURNING load_id::text`, testTenant, seed.key).Scan(seed.target); err != nil {
				t.Fatalf("seed %s: %v", seed.key, err)
			}
		}

		// ONE goat, accepted on BOTH loads, both rows carrying a NULL acceptance instant.
		var twoLoadGoat string
		if err := pool.QueryRow(ctx, `
INSERT INTO goats (tenant_id, sex, lifecycle_status, custodian_party_id)
VALUES ($1, 'female', 'alive', (SELECT party_id FROM parties WHERE display_name = 'Sardar Traders' LIMIT 1))
RETURNING goat_id::text`, testTenant).Scan(&twoLoadGoat); err != nil {
			t.Fatalf("seed two-load goat: %v", err)
		}
		for _, load := range []string{loadD, loadE} {
			if _, err := pool.Exec(ctx, `
INSERT INTO procurement_load_goats (tenant_id, load_id, goat_id, selection_state, current_state, intake_accepted_at)
VALUES ($1, $2::uuid, $3::uuid, 'accepted_herd_intake', 'accepted_herd_intake', NULL)`,
				testTenant, load, twoLoadGoat); err != nil {
				t.Fatalf("seed duplicate accepted row: %v", err)
			}
		}

		// Repeated identical reads are NOT enough to expose this: on a small table Postgres
		// returns the same physical order each time, so a partial ORDER BY looks stable. What
		// actually moves the winner is a tuple being REWRITTEN -- an UPDATE relocates it in the
		// heap and changes the input order the sort sees. That is not hypothetical here: this
		// seed's own ON CONFLICT DO UPDATE refreshes animal_identifier_1 on every re-run.
		//
		// So the read is taken, one membership row is rewritten by an edit that changes nothing
		// about which load owns the animal, and the read is taken again. Under a partial order the
		// winner flips; under a total one it cannot.
		var winner string
		for attempt := 0; attempt < 5; attempt++ {
			if attempt == 2 {
				if _, err := pool.Exec(ctx, `
UPDATE procurement_load_goats SET animal_identifier_1 = 'REFRESHED-TAG'
WHERE goat_id = $1::uuid AND load_id = $2::uuid`, twoLoadGoat, loadD); err != nil {
					t.Fatalf("rewrite membership row: %v", err)
				}
			}
			out, err := repo.LoadwiseSales(ctx, testTenant, 60)
			if err != nil {
				t.Fatalf("read %d: %v", attempt, err)
			}
			// Remaining, NOT Purchased: Purchased reports the load's DECLARED size, which both of
			// these loads state as 1 whatever the dedupe does. Remaining counts the animals
			// actually attributed, so it is what moves if the goat lands on both or on neither.
			d, e := loadByID(out, loadD), loadByID(out, loadE)
			if d.Remaining+e.Remaining != 1 {
				t.Fatalf("read %d: goat attributed to %d of the two loads, want exactly one",
					attempt, d.Remaining+e.Remaining)
			}
			got := loadD
			if e.Remaining == 1 {
				got = loadE
			}
			if attempt == 0 {
				winner = got
				continue
			}
			if got != winner {
				t.Fatalf("read %d put the goat on load %s but read 0 put it on %s: the dedupe is not deterministic",
					attempt, got, winner)
			}
		}

		// STRUCTURAL guard, and the honest one of the two.
		//
		// The behavioural loop above does NOT fail against the old partial ORDER BY: with this
		// fixture's size the planner reaches the rows through
		// procurement_load_goats_goat_state_idx, which hands the sort an already-determined order,
		// so the old code looks stable here. The nondeterminism is real by SQL semantics -- ties
		// under DISTINCT ON have no defined winner -- but it is not reproducible at this scale, and
		// a test that cannot fail is not proof.
		//
		// What IS checkable is the property itself: the dedupe must end on a column that is unique
		// and immutable, so the order is total whatever the planner does. This fails the moment
		// someone trims the tie-breaker back.
		memberOrder := loadwiseSalesSQL[strings.Index(loadwiseSalesSQL, "ORDER BY plg.goat_id"):]
		memberOrder = memberOrder[:strings.Index(memberOrder, "\n")]
		if !strings.Contains(memberOrder, "plg.load_goat_id") {
			t.Fatalf("the member dedupe must end on the immutable unique key or ties have no defined winner; ORDER BY is: %s", memberOrder)
		}

		// Leave the fixture as it was so later subtests are unaffected.
		if _, err := pool.Exec(ctx, `DELETE FROM procurement_load_goats WHERE goat_id = $1::uuid`, twoLoadGoat); err != nil {
			t.Fatalf("cleanup: %v", err)
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
