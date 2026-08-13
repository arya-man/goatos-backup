package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// Growth reads an ADG (Average Daily Gain) pair out of two consecutive weighs of the SAME tag.
// Every test here seeds real observations and asserts the real numbers the leadership screen
// renders, because the bug these exist for shipped a headline of -3,108,762 g/day.

// seedGrowthObservation writes one accepted observation for a tag at an instant.
func seedGrowthObservation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tag string, weightKg float64, at time.Time) {
	t.Helper()
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_observations (tenant_id, campaign_id, campaign_shed_id, scanned_identifier, weight_kg, proof_artifact_id, recorded_by, idempotency_key, accepted_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6::uuid, $7::uuid, $8, $9::timestamptz)`,
		repoTenant, repoCampaign, repoAnimalScope, tag, weightKg, repoAnimalProof, repoOperator,
		fmt.Sprintf("growth:%s:%d", tag, at.UnixNano()), at)
}

func growthWindow() (time.Time, time.Time) {
	// A wide window so membership is never the thing under test.
	return time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)
}

// TestGrowthADGDateShiftIgnoresSameBusinessDayPairs is the regression test for the defect this
// projection actually shipped: ADG divided ELAPSED SECONDS expressed as a fraction of a day, so a
// tag weighed twice 111 seconds apart (15.0 kg then 11.0 kg -- a duplicate scan across two sheds)
// produced -4 kg / 0.001285 days = -3,113,000 g/day and rendered as the herd's growth headline.
// An animal cannot gain or lose meaningfully inside one day, so a same-day pair is a re-weigh, a
// correction, or a double scan -- never growth. Reverting to fractional-day division makes this
// test fail on PairCount, because the same-day pair starts qualifying again.
func TestGrowthADGDateShiftIgnoresSameBusinessDayPairs(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	start, end := growthWindow()

	// 04:04 and 04:06 UTC on the same Kolkata day: exactly the live incident, two minutes apart.
	day := time.Date(2026, 8, 5, 4, 4, 0, 0, time.UTC)
	seedGrowthObservation(t, ctx, pool, "same-day-tag", 15.0, day)
	seedGrowthObservation(t, ctx, pool, "same-day-tag", 11.0, day.Add(111*time.Second))

	adg, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{repoPark}, start, end)
	if err != nil {
		t.Fatalf("GetLeadershipGrowthADG: %v", err)
	}
	if adg.Headline.PairCount != 0 {
		t.Fatalf("two weighs on ONE business day produced %d ADG pair(s); a same-day re-weigh is not growth", adg.Headline.PairCount)
	}
	if adg.Headline.MedianADGGPerDay != nil {
		t.Fatalf("median ADG = %v g/day from a same-day pair; want nil (this is the -3,108,762 g/day defect)", *adg.Headline.MedianADGGPerDay)
	}
	if adg.Headline.Status != "insufficient_data" {
		t.Fatalf("status = %q with no qualifying pair, want insufficient_data", adg.Headline.Status)
	}
}

// TestGrowthADGDateShiftAcrossMidnightCountsWholeDays pins the other half of the same rule: a pair
// that DOES span business days must divide by whole days, and a pair either side of a Kolkata
// midnight is one day apart -- not zero, and not a fraction.
func TestGrowthADGDateShiftAcrossMidnightCountsWholeDays(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	start, end := growthWindow()

	// 18:00 and 19:00 UTC = 23:30 and 00:30 Kolkata: one hour apart, but two business days.
	evening := time.Date(2026, 8, 5, 18, 0, 0, 0, time.UTC)
	seedGrowthObservation(t, ctx, pool, "midnight-tag", 20.0, evening)
	seedGrowthObservation(t, ctx, pool, "midnight-tag", 21.0, evening.Add(time.Hour))

	adg, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{repoPark}, start, end)
	if err != nil {
		t.Fatalf("GetLeadershipGrowthADG: %v", err)
	}
	if adg.Headline.PairCount != 1 {
		t.Fatalf("pair count = %d across a Kolkata midnight, want exactly 1", adg.Headline.PairCount)
	}
	if adg.Headline.MedianADGGPerDay == nil {
		t.Fatalf("median ADG is nil for a genuine cross-day pair")
	}
	// +1.0 kg over 1 whole day = 1000 g/day. An hour-based divisor would report ~24,000.
	if got := *adg.Headline.MedianADGGPerDay; got < 999 || got > 1001 {
		t.Fatalf("median ADG = %v g/day, want ~1000 (1.0 kg over ONE whole day)", got)
	}
}

// TestGrowthADGOneToManyDoesNotMultiplyAnimals is the cardinality guard: one animal weighed many
// times contributes consecutive PAIRS, and must not be multiplied into the herd counts by the
// joins to its shed/park dimensions. Four weighs on four days = three pairs, one animal.
func TestGrowthADGOneToManyDoesNotMultiplyAnimals(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	start, end := growthWindow()

	day := time.Date(2026, 8, 1, 6, 0, 0, 0, time.UTC)
	for i, kg := range []float64{10.0, 11.0, 12.0, 13.0} {
		seedGrowthObservation(t, ctx, pool, "one-to-many-tag", kg, day.AddDate(0, 0, i))
	}

	adg, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{repoPark}, start, end)
	if err != nil {
		t.Fatalf("GetLeadershipGrowthADG: %v", err)
	}
	if adg.Headline.PairCount != 3 {
		t.Fatalf("pair count = %d for four weighs of ONE animal, want 3 consecutive pairs", adg.Headline.PairCount)
	}
	// Each step is +1.0 kg over one day, so the median is 1000 g/day regardless of how many
	// dimension rows the query joins through.
	if adg.Headline.MedianADGGPerDay == nil {
		t.Fatalf("median ADG is nil with three qualifying pairs")
	}
	if got := *adg.Headline.MedianADGGPerDay; got < 999 || got > 1001 {
		t.Fatalf("median ADG = %v g/day, want ~1000; a dimension fan-out would skew this", got)
	}
}

// TestGrowthADGStatusBucketsPlaceEachPairOnce is the status guard: every qualifying pair lands in
// exactly one bucket, including the negative bucket. A losing animal must be counted as losing
// once, not counted twice or silently dropped for being below zero.
func TestGrowthADGStatusBucketsPlaceEachPairOnce(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	start, end := growthWindow()

	day := time.Date(2026, 8, 2, 6, 0, 0, 0, time.UTC)
	// One gaining animal and one losing animal, each a single cross-day pair.
	seedGrowthObservation(t, ctx, pool, "gainer-tag", 10.0, day)
	seedGrowthObservation(t, ctx, pool, "gainer-tag", 11.0, day.AddDate(0, 0, 1))
	seedGrowthObservation(t, ctx, pool, "loser-tag", 20.0, day)
	seedGrowthObservation(t, ctx, pool, "loser-tag", 19.0, day.AddDate(0, 0, 1))

	adg, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{repoPark}, start, end)
	if err != nil {
		t.Fatalf("GetLeadershipGrowthADG: %v", err)
	}
	if adg.Headline.PairCount != 2 {
		t.Fatalf("pair count = %d, want exactly 2 (one gaining, one losing)", adg.Headline.PairCount)
	}
	if adg.Headline.NegativeADGCount != 1 {
		t.Fatalf("negative ADG count = %d, want exactly 1 -- the losing animal is counted once, never dropped", adg.Headline.NegativeADGCount)
	}
}

// TestGrowthADGPaginationTotalsAreNotPageLocal is the pagination guard: the headline totals are
// computed over the whole period, so they must not change when the multi-row reads behind the same
// window page. A page-local COUNT would make the herd headline depend on how many rows fit a page.
func TestGrowthADGPaginationTotalsAreNotPageLocal(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	start, end := growthWindow()

	// 30 animals, each a single cross-day pair: comfortably more than any one page.
	day := time.Date(2026, 8, 3, 6, 0, 0, 0, time.UTC)
	for i := 0; i < 30; i++ {
		tag := fmt.Sprintf("page-growth-%02d", i)
		seedGrowthObservation(t, ctx, pool, tag, 10.0, day)
		seedGrowthObservation(t, ctx, pool, tag, 11.0, day.AddDate(0, 0, 1))
	}

	adg, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{repoPark}, start, end)
	if err != nil {
		t.Fatalf("GetLeadershipGrowthADG: %v", err)
	}
	if adg.Headline.PairCount != 30 {
		t.Fatalf("pair count = %d for 30 animals each weighed twice, want 30 -- totals must span the period, not a page", adg.Headline.PairCount)
	}
}
