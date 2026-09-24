package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
)

// THE REMAINING HEAD MIX (maintainer decision 2026-09-24): each load's remaining animals split by
// (species, management stage, sex), so the Load-wise tab can value stock at a stage x sex price.
// The mix is grouped at (load, species, stage, sex) and folded to one array per load; these tests
// pin that the fold never multiplies a load row, counts only the live animals, and travels with
// its own load through the page window and the park filter.
func TestLoadwiseRemainingMixAdversarial(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	fx := seedLoadwiseFixture(t, ctx, pool)

	// Load A already holds one live female goat with no stage beside sold, dead and merged
	// animals. Add three more live animals across two new (species, stage, sex) groups, plus a
	// SOLD K3 male that must not reach the mix.
	var vendorParty, cptPark string
	if err := pool.QueryRow(ctx, `
SELECT g.custodian_party_id::text, g.park_id::text
FROM procurement_load_goats plg JOIN goats g ON g.goat_id = plg.goat_id
WHERE plg.tenant_id = $1 AND plg.load_id = $2::uuid AND g.lifecycle_status = 'alive' LIMIT 1`,
		testTenant, fx.loadA).Scan(&vendorParty, &cptPark); err != nil {
		t.Fatalf("read fixture party/park: %v", err)
	}
	add := func(sex, lifecycle, stage, key string) {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx, `
INSERT INTO goats (tenant_id, sex, species, management_stage, lifecycle_status, exit_reason, custodian_party_id, park_id)
VALUES ($1, $2, 'sheep', $3, $4, CASE WHEN $4 = 'sold' THEN 'sold' END, $5, $6::uuid)
RETURNING goat_id::text`, testTenant, sex, stage, lifecycle, vendorParty, cptPark).Scan(&id); err != nil {
			t.Fatalf("seed goat %s: %v", key, err)
		}
		if _, err := pool.Exec(ctx, `
INSERT INTO procurement_load_goats (tenant_id, load_id, goat_id, selection_state, current_state, intake_accepted_at)
VALUES ($1, $2::uuid, $3::uuid, 'accepted_herd_intake', 'accepted_herd_intake', '2026-08-01T10:00:00Z')`,
			testTenant, fx.loadA, id); err != nil {
			t.Fatalf("accept goat %s: %v", key, err)
		}
	}
	add("male", "alive", "K3", "k3m-1")
	add("male", "alive", "K3", "k3m-2")
	add("female", "alive", "K3", "k3f-1")
	add("male", "sold", "K3", "k3m-sold")

	repo := NewRepository(pool, 10*time.Second)
	find := func(out domain.LoadwiseSales, id string) (domain.LoadwiseLoad, int) {
		var hit domain.LoadwiseLoad
		n := 0
		for _, l := range out.Loads {
			if l.LoadID == id {
				hit, n = l, n+1
			}
		}
		return hit, n
	}
	mixSum := func(l domain.LoadwiseLoad) int {
		s := 0
		for _, m := range l.RemainingMix {
			s += m.Animals
		}
		return s
	}
	all, err := repo.LoadwiseSales(ctx, testTenant, "", 60)
	if err != nil {
		t.Fatalf("loadwise sales: %v", err)
	}
	loadA, rows := find(all, fx.loadA)

	t.Run("OneToManyGroupsKeepOneRowPerLoad", func(t *testing.T) {
		// Three mix groups must not fan the load out into three rows or triple its counts.
		if rows != 1 {
			t.Fatalf("load A served %d times, want exactly once", rows)
		}
		if len(loadA.RemainingMix) != 3 {
			t.Fatalf("load A mix = %+v, want three (species, stage, sex) groups", loadA.RemainingMix)
		}
		if loadA.Remaining != 4 || loadA.Sold != 3 {
			t.Fatalf("load A remaining=%d sold=%d, want 4 live and 3 sold (the mix join must not multiply them)", loadA.Remaining, loadA.Sold)
		}
		want := map[[3]string]int{{"sheep", "K3", "male"}: 2, {"sheep", "K3", "female"}: 1, {"goat", "", "female"}: 1}
		for _, m := range loadA.RemainingMix {
			key := [3]string{m.Species, m.ManagementStage, m.Sex}
			if want[key] != m.Animals {
				t.Fatalf("mix group %v = %d, want %d (mix %+v)", key, m.Animals, want[key], loadA.RemainingMix)
			}
		}
	})

	t.Run("StatusBucketsCountOnlyTheLiveAnimals", func(t *testing.T) {
		// Sold, dead and merged animals are in their own outcome buckets and never in the mix; the
		// SOLD K3 male in particular must not appear as a third K3 male.
		for _, l := range all.Loads {
			if got := mixSum(l); got != l.Remaining {
				t.Fatalf("load %s mix sums to %d, remaining is %d", l.LoadID, got, l.Remaining)
			}
		}
	})

	t.Run("PageBoundaryServesEachLoadItsOwnMix", func(t *testing.T) {
		// The newest-first window cuts loads off; whatever survives the cut must carry ITS OWN
		// mix, never a neighbour's, and a load with no live animals carries an empty mix.
		for limit := 1; limit <= 3; limit++ {
			page, err := repo.LoadwiseSales(ctx, testTenant, "", limit)
			if err != nil {
				t.Fatalf("limit %d: %v", limit, err)
			}
			for _, l := range page.Loads {
				full, _ := find(all, l.LoadID)
				if mixSum(l) != full.Remaining || len(l.RemainingMix) != len(full.RemainingMix) {
					t.Fatalf("limit %d: load %s mix %+v, unpaged %+v", limit, l.LoadID, l.RemainingMix, full.RemainingMix)
				}
				if l.RemainingMix == nil {
					t.Fatalf("limit %d: load %s mix is nil, want an empty list", limit, l.LoadID)
				}
			}
		}
	})

	t.Run("ParkScopeFilterKeepsTheSameMix", func(t *testing.T) {
		// Load A is all-CPT; under the CPT filter it is the same load with the same mix.
		cpt, err := repo.LoadwiseSales(ctx, testTenant, parkIDByCode(t, ctx, pool, "CPT"), 60)
		if err != nil {
			t.Fatalf("CPT: %v", err)
		}
		scoped, n := find(cpt, fx.loadA)
		if n != 1 || mixSum(scoped) != loadA.Remaining || len(scoped.RemainingMix) != len(loadA.RemainingMix) {
			t.Fatalf("CPT load A mix = %+v (served %d), unfiltered %+v", scoped.RemainingMix, n, loadA.RemainingMix)
		}
	})
}
