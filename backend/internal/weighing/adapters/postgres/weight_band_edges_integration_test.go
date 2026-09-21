package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// The Weight-wise bands follow the caller's edges (the tenant's weight_band_edges_kg assumption,
// maintainer decision 2026-09-19). Adversarial on the four axes the aggregate guard names:
//
//   - ONE-TO-MANY: a pen weighed twice in the window still counts its animals ONCE (latest weigh),
//     whatever edges are asked for -- regrouping never fans a pen out into two brackets;
//   - PAGE BOUNDARY: the bands are a whole-scope partition, not a page -- the animal total is the
//     same under the default edges and under different ones, and equals the weighed population;
//   - PARK SCOPE: the bands respect the park list exactly like every other figure on the read;
//   - STATUS BUCKETS: NO edges resolves to the default edges and reproduces the keys the tab has
//     always carried, byte for byte, so a deployment with the seeded rows changes nothing.
func TestWeightBandEdgesOneToManyPageBoundaryParkScopeStatusBuckets(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	// One whole-shed pen weighed twice: 4 animals at 20.0 then 22.8 kg. The LATEST weigh bands it.
	seedLumpSumObservation(t, ctx, pool, repoShedScope, 4, 20.0, time.Date(2026, 8, 1, 6, 0, 0, 0, time.UTC))
	seedLumpSumObservation(t, ctx, pool, repoShedScope, 4, 22.8, time.Date(2026, 8, 8, 6, 0, 0, 0, time.UTC))
	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)

	sum := func(bands []domain.WeightBandBucket) int {
		n := 0
		for _, b := range bands {
			n += b.Animals
		}
		return n
	}
	find := func(bands []domain.WeightBandBucket, key string) (domain.WeightBandBucket, bool) {
		for _, b := range bands {
			if b.Band == key {
				return b, true
			}
		}
		return domain.WeightBandBucket{}, false
	}

	// STATUS BUCKETS: no edges == the default edges, and the default keys/labels are the old ones.
	byDefault, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark}, from, to, "", "", "", "", nil, domain.TimeScope{})
	if err != nil {
		t.Fatalf("GetWeightDemographics(default): %v", err)
	}
	for _, b := range byDefault.ByWeightBand {
		switch b.Band {
		case "under_15", "15_20", "20_25", "25_30", "30_35", "35_plus":
		default:
			t.Fatalf("default edges must reproduce the tab's own keys, got %q", b.Band)
		}
		if b.Label == "" {
			t.Fatalf("band %q served without a label", b.Band)
		}
	}
	pen, ok := find(byDefault.ByWeightBand, "20_25")
	if !ok || pen.Animals < 4 {
		t.Fatalf("the pen's 4 animals must sit in 20_25 at their LATEST average (22.8 kg): %+v", byDefault.ByWeightBand)
	}

	// ONE-TO-MANY + PAGE BOUNDARY: a different partition moves the pen whole and keeps the total.
	byTens, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark}, from, to, "", "", "", "", []float64{10, 20, 30, 40}, domain.TimeScope{})
	if err != nil {
		t.Fatalf("GetWeightDemographics(10/20/30/40): %v", err)
	}
	if got, want := sum(byTens.ByWeightBand), sum(byDefault.ByWeightBand); got != want {
		t.Fatalf("regrouping must keep the weighed population: %d vs %d", got, want)
	}
	moved, ok := find(byTens.ByWeightBand, "20_30")
	if !ok || moved.Animals < 4 || moved.Label != "20 – 30 kg" {
		t.Fatalf("the pen must land whole in 20_30 with its farm label: %+v", byTens.ByWeightBand)
	}
	if _, leaked := find(byTens.ByWeightBand, "20_25"); leaked {
		t.Fatalf("a default key survived a regroup: %+v", byTens.ByWeightBand)
	}

	// PARK SCOPE: another park's list holds none of this pen's animals, whatever the edges.
	other, err := repo.GetWeightDemographics(ctx, repoTenant, []string{"00000000-0000-4000-8000-00000000dead"}, from, to, "", "", "", "", []float64{10, 20, 30, 40}, domain.TimeScope{})
	if err != nil {
		t.Fatalf("GetWeightDemographics(other park): %v", err)
	}
	if sum(other.ByWeightBand) != 0 {
		t.Fatalf("another park's scope must not see this pen: %+v", other.ByWeightBand)
	}
}
