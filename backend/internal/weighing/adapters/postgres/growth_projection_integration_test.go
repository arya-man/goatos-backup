package postgres

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// Growth reads an ADG (Average Daily Gain) pair out of two consecutive weighs of the SAME tag.
// Every test here seeds real observations and asserts the real numbers the leadership screen
// renders, because the bug these exist for shipped a headline of -3,108,762 g/day.

// seedGrowthObservation writes one accepted observation for a tag at an instant.
func seedGrowthObservation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tag string, weightKg float64, at time.Time) {
	t.Helper()
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_observations (tenant_id, campaign_id, campaign_shed_id, scanned_identifier, weight_kg, proof_artifact_id, recorded_by, idempotency_key, accepted_at, submitted_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6::uuid, $7::uuid, $8, $9::timestamptz, $9::timestamptz)`,
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

	adg, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{repoPark}, start, end, "", "", "", "", domain.TimeScope{})
	if err != nil {
		t.Fatalf("GetLeadershipGrowthADG: %v", err)
	}
	if adg.Headline.PairCount != 0 {
		t.Fatalf("two weighs on ONE business day produced %d ADG pair(s); a same-day re-weigh is not growth", adg.Headline.PairCount)
	}
	if adg.Headline.AverageADGGPerDay != nil {
		t.Fatalf("headline ADG = %v g/day from a same-day pair; want nil (this is the -3,108,762 g/day defect)", *adg.Headline.AverageADGGPerDay)
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

	adg, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{repoPark}, start, end, "", "", "", "", domain.TimeScope{})
	if err != nil {
		t.Fatalf("GetLeadershipGrowthADG: %v", err)
	}
	if adg.Headline.PairCount != 1 {
		t.Fatalf("pair count = %d across a Kolkata midnight, want exactly 1", adg.Headline.PairCount)
	}
	if adg.Headline.AverageADGGPerDay == nil {
		t.Fatalf("median ADG is nil for a genuine cross-day pair")
	}
	// +1.0 kg over 1 whole day = 1000 g/day. An hour-based divisor would report ~24,000.
	if got := *adg.Headline.AverageADGGPerDay; got < 999 || got > 1001 {
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

	adg, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{repoPark}, start, end, "", "", "", "", domain.TimeScope{})
	if err != nil {
		t.Fatalf("GetLeadershipGrowthADG: %v", err)
	}
	if adg.Headline.PairCount != 3 {
		t.Fatalf("pair count = %d for four weighs of ONE animal, want 3 consecutive pairs", adg.Headline.PairCount)
	}
	// Each step is +1.0 kg over one day, so the median is 1000 g/day regardless of how many
	// dimension rows the query joins through.
	if adg.Headline.AverageADGGPerDay == nil {
		t.Fatalf("median ADG is nil with three qualifying pairs")
	}
	if got := *adg.Headline.AverageADGGPerDay; got < 999 || got > 1001 {
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

	adg, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{repoPark}, start, end, "", "", "", "", domain.TimeScope{})
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

	adg, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{repoPark}, start, end, "", "", "", "", domain.TimeScope{})
	if err != nil {
		t.Fatalf("GetLeadershipGrowthADG: %v", err)
	}
	if adg.Headline.PairCount != 30 {
		t.Fatalf("pair count = %d for 30 animals each weighed twice, want 30 -- totals must span the period, not a page", adg.Headline.PairCount)
	}
}

// TestGrowthDistributionFillsTheOverflowBinAboveThreeHundred is the regression test for a bar
// chart whose LAST bar could never fill. The bin query wrapped width_bucket in
// LEAST(..., $12) -- $12 being the 12 regular bins -- which clamped the overflow bin 13 back
// down to 12, the 275-300 band. The Go below then read counts[13] for the "300+" bucket, and
// bin 13 no longer existed, so that bucket was ALWAYS zero. On live STG data the chart showed
// 107 pairs in 275-300 and 0 above it, when the truth was 11 and 99, with a fastest pair of
// 1,600 g/day. The comment above the "300+" bucket already promised the opposite of what the
// code did ("rather than silently dropped or mis-binned into the last regular bucket").
//
// Restoring the LEAST() clamp turns this red: the 600 g/day pair lands in 275-300 instead.
func TestGrowthDistributionFillsTheOverflowBinAboveThreeHundred(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	start, end := growthWindow()

	// +6.0 kg across 10 whole days = 600 g/day, which belongs ABOVE the 300 g/day top edge.
	first := time.Date(2026, 7, 10, 4, 0, 0, 0, time.UTC)
	seedGrowthObservation(t, ctx, pool, "fast-grower", 20.0, first)
	seedGrowthObservation(t, ctx, pool, "fast-grower", 26.0, first.AddDate(0, 0, 10))

	adg, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{repoPark}, start, end, "", "", "", "", domain.TimeScope{})
	if err != nil {
		t.Fatalf("GetLeadershipGrowthADG: %v", err)
	}

	counts := map[string]int{}
	total := 0
	for _, b := range adg.Distribution {
		counts[b.Label] = b.Count
		total += b.Count
	}
	if counts["300+"] != 1 {
		t.Fatalf("a 600 g/day pair must land in the 300+ bucket; got 300+=%d, 275-300=%d (the overflow bin is being clamped into the last regular bucket)",
			counts["300+"], counts["275-300"])
	}
	if counts["275-300"] != 0 {
		t.Fatalf("a 600 g/day pair must NOT be filed as 275-300; got 275-300=%d", counts["275-300"])
	}
	// The bars are the pairs, whole: a histogram that does not add up to its own denominator is
	// hiding rows somewhere, which is exactly how the empty top bar went unnoticed.
	if total != adg.Headline.PairCount {
		t.Fatalf("distribution buckets sum to %d but PairCount is %d; every pair must land in exactly one bar",
			total, adg.Headline.PairCount)
	}
}

// TestGrowthPairsRequireBothWeighsInsideTheSelectedPeriodOneToManyPageBoundaryParkScope pins
// the rule that the date filter actually scopes the measurement (maintainer decision
// 2026-09-09). The pair anchor used to reach growthLookbackDays (400) BEFORE periodStart, so an
// animal weighed ONCE inside the period paired against a weigh up to 400 days old and still
// counted. On live STG data that made 250 of 349 animals qualify on an August weigh for a 1-7
// September view, and 62% of the measured growth-days fell outside the window the reader had
// selected -- so narrowing the dates barely moved the number, while the lump-sum half (which
// always required both of its points inside the period) did move. Same headline, two rules.
//
// The fixture is adversarial at the aggregate-projection grain too: multiple candidate animals
// exercise the one-to-many pair source without changing the denominator; the headline and
// distribution totals must agree across the page boundary; and the same facts must not leak when
// the park scope is different.
//
// Restoring `lookbackStart := periodStart.Add(-growthLookbackDays * 24 * time.Hour)` turns this
// red: the straddling animal starts pairing again and PairCount becomes 2.
func TestGrowthPairsRequireBothWeighsInsideTheSelectedPeriodOneToManyPageBoundaryParkScope(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	start, end := growthWindow()

	// Straddles the window: earlier weigh 11 days BEFORE it opens, latest weigh inside.
	seedGrowthObservation(t, ctx, pool, "straddles-window", 18.0, start.AddDate(0, 0, -11))
	seedGrowthObservation(t, ctx, pool, "straddles-window", 20.0, start.AddDate(0, 0, 3))

	// Wholly inside: this one is what the reader asked for and must survive.
	seedGrowthObservation(t, ctx, pool, "inside-window", 18.0, start.AddDate(0, 0, 3))
	seedGrowthObservation(t, ctx, pool, "inside-window", 20.0, start.AddDate(0, 0, 13))

	adg, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{repoPark}, start, end, "", "", "", "", domain.TimeScope{})
	if err != nil {
		t.Fatalf("GetLeadershipGrowthADG: %v", err)
	}
	if adg.Headline.PairCount != 1 {
		t.Fatalf("only the animal weighed TWICE inside the period may pair; PairCount=%d (an animal weighed once in the period is pairing against a weigh from before it)",
			adg.Headline.PairCount)
	}
	// 2.0 kg over 10 whole days = 200 g/day, the inside-window animal and nothing else.
	if adg.Headline.AverageADGGPerDay == nil {
		t.Fatal("AverageADGGPerDay is nil; the wholly-inside pair should carry the headline")
	}
	if got := *adg.Headline.AverageADGGPerDay; got < 199.9 || got > 200.1 {
		t.Fatalf("headline ADG = %.2f g/day, want 200.0 from the wholly-inside pair alone", got)
	}

	totalBars := 0
	for _, bucket := range adg.Distribution {
		totalBars += bucket.Count
	}
	if totalBars != adg.Headline.PairCount {
		t.Fatalf("distribution page-boundary total = %d, PairCount = %d; every qualifying pair must land in one bar",
			totalBars, adg.Headline.PairCount)
	}

	otherPark := "00000000-0000-4000-8000-0000000030ff"
	scoped, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{otherPark}, start, end, "", "", "", "", domain.TimeScope{})
	if err != nil {
		t.Fatalf("GetLeadershipGrowthADG(other park): %v", err)
	}
	if scoped.Headline.PairCount != 0 {
		t.Fatalf("park scope leaked %d pair(s) into another park", scoped.Headline.PairCount)
	}
}

func TestGrowthLosingAnimalsPreservesCanonicalSameDayPairSemantics(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	start, end := growthWindow()
	day := time.Date(2026, 8, 5, 4, 0, 0, 0, time.UTC)
	// Only the first pair spans business days. The final same-day correction
	// must not introduce a losing pair that the canonical growth model excludes.
	seedGrowthObservation(t, ctx, pool, "corrected-tag", 20, day)
	seedGrowthObservation(t, ctx, pool, "corrected-tag", 21, day.Add(24*time.Hour))
	seedGrowthObservation(t, ctx, pool, "corrected-tag", 19, day.Add(25*time.Hour))
	losing, err := repo.growthLosingAnimals(ctx, repoTenant, []string{repoPark}, start.AddDate(0, 0, -growthLookbackDays), start, end, false, SexScope{}, AnimalIdentityMap{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(losing) != 0 {
		t.Fatalf("same-day correction invented a losing pair absent from canonical growth: %+v", losing)
	}
}

// Multiple observations must not multiply animal rows, and pending/rework facts
// remain real weights under the same canonical status rules as verified facts.
func TestGrowthLosingAnimalsOneToManyDateShiftStatusMatrix(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	start, end := growthWindow()
	day := time.Date(2026, 8, 5, 4, 0, 0, 0, time.UTC)
	for _, tag := range []string{"loser-a", "loser-b"} {
		for i, weight := range []float64{22, 21, 20} {
			seedGrowthObservation(t, ctx, pool, tag, weight, day.AddDate(0, 0, i))
		}
	}
	for _, status := range []string{"pending", "verified", "rework"} {
		t.Run(status, func(t *testing.T) {
			execWeighingTestSQL(t, ctx, pool, `UPDATE weighing_observations SET verification_status = $2 WHERE tenant_id = $1::uuid`, repoTenant, status)
			losing, err := repo.growthLosingAnimals(ctx, repoTenant, []string{repoPark}, start.AddDate(0, 0, -growthLookbackDays), start, end, false, SexScope{}, AnimalIdentityMap{}, "")
			if err != nil {
				t.Fatal(err)
			}
			if len(losing) != 2 {
				t.Fatalf("%s: six observations must produce exactly two animals, got %+v", status, losing)
			}
			// Moving the report start past every endpoint must exclude the animals
			// even though all their observations remain in the pairing lookback.
			shifted, err := repo.growthLosingAnimals(ctx, repoTenant, []string{repoPark}, start.AddDate(0, 0, -growthLookbackDays), day.AddDate(0, 0, 3), end, false, SexScope{}, AnimalIdentityMap{}, "")
			if err != nil {
				t.Fatal(err)
			}
			if len(shifted) != 0 {
				t.Fatalf("%s: report boundary retained historical losing rows: %+v", status, shifted)
			}
			seen := map[string]bool{}
			for _, animal := range losing {
				if seen[animal.ScannedIdentifier] || animal.PreviousWeightKg != 21 || animal.LatestWeightKg != 20 || animal.ADGGPerDay != -1000 || animal.DaysBetween != 1 {
					t.Fatalf("%s: duplicated animal or changed latest qualifying pair: %+v", status, losing)
				}
				seen[animal.ScannedIdentifier] = true
			}
		})
	}
}

// TestAnimalGainIsTotalMovementNotAMedianOfLegRates is the regression test for the defect the
// maintainer caught on the Time-wise tab on 2026-09-22: one kid's weekly row read 764 g/day.
//
// The kid was weighed three times in eight days -- 27.5 kg on Mon 24 Aug, 28.4 on Mon 31 Aug,
// 29.8 on Tue 1 Sep. Both 31 Aug and 1 Sep fall in the ISO week beginning 31 Aug, and a pair is
// bucketed by its LATER weigh, so that ONE weekly bucket held TWO legs:
//
//	24 Aug -> 31 Aug   0.9 kg over 7 days =  129 g/day
//	31 Aug ->  1 Sep   1.4 kg over 1 day  = 1400 g/day
//
// animal_gain took the MEDIAN of those two RATES, and the median of two values is their mean:
// 764 g/day, for an animal that actually gained 2.3 kg in 8 days -- 287 g/day. A rate median
// weights a 1-day leg exactly as heavily as a 7-day one, and over a single day ordinary scale
// noise IS the entire rate. The whole-shed arm beside it never had this problem: it has always
// divided total grams by total days.
//
// All three reads are asserted together because the 2026-08-26 lock requires them to be the same
// statistic, and the existing parity test cannot catch this one -- its fixture is whole-shed pens
// only, so both sides of that comparison have an EMPTY animal arm and agree while drifting.
//
// MUTATION TEST when this was written: restoring percentile_cont(0.5) in any one of the four
// animal_gain CTEs turns this red at 764.29 against 287.5.
func TestAnimalGainIsTotalMovementNotAMedianOfLegRates(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	const tag = "three-weigh-kid"
	const goatID = "00000000-0000-4000-8000-000000009401"
	// Resolved to a real animal so the breed reads below have a bucket to report in; the growth
	// reads never look at this row.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, breed, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, 'G-990940', 'Three Weigh Breed', 'female', 'kid', 'alive', 'kid', $3::uuid, $4::uuid, $5::uuid, $4::uuid)
ON CONFLICT (goat_id) DO UPDATE SET breed=EXCLUDED.breed, shed_id=EXCLUDED.shed_id`,
		goatID, repoTenant, repoParty, repoExpectedShed, repoPark)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, normalizer_version)
VALUES ($1::uuid, $2::uuid, 'animal_identifier_1', $3, $3, 'global', true, 'active', now(), 'test')
ON CONFLICT (tenant_id, normalized_value) DO UPDATE SET goat_id=EXCLUDED.goat_id, status='active'`,
		repoTenant, goatID, tag)

	// 06:00 UTC is 11:30 IST, so each instant lands on the intended business date.
	seedGrowthObservation(t, ctx, pool, tag, 27.5, time.Date(2026, 8, 24, 6, 0, 0, 0, time.UTC))
	seedGrowthObservation(t, ctx, pool, tag, 28.4, time.Date(2026, 8, 31, 6, 0, 0, 0, time.UTC))
	seedGrowthObservation(t, ctx, pool, tag, 29.8, time.Date(2026, 9, 1, 6, 0, 0, 0, time.UTC))

	start := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	// 2.3 kg over the 8 days those two legs span. The retired rate median reported 764.29.
	const want = 287.5

	adg, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{repoPark}, start, end, "", "", "", "", domain.TimeScope{})
	if err != nil {
		t.Fatalf("GetLeadershipGrowthADG: %v", err)
	}
	if adg.Headline.AverageADGGPerDay == nil {
		t.Fatalf("headline reported no gain for a kid weighed three times: %#v", adg.Headline)
	}
	if got := *adg.Headline.AverageADGGPerDay; math.Abs(got-want) > 0.01 {
		t.Fatalf("headline = %.2f g/day, want %.2f -- the animal gained 2.3 kg in 8 days", got, want)
	}

	var weekly *domain.GrowthWeeklyGainPoint
	for i := range adg.WeeklyGain {
		if adg.WeeklyGain[i].WeekStart == "2026-08-31" {
			weekly = &adg.WeeklyGain[i]
		}
	}
	if weekly == nil {
		t.Fatalf("no weekly point for the week holding both legs; got %#v", adg.WeeklyGain)
	}
	if math.Abs(weekly.AverageADGGPerDay-want) > 0.01 {
		t.Fatalf("week 2026-08-31 = %.2f g/day, want %.2f -- this is the 764 g/day the maintainer reported",
			weekly.AverageADGGPerDay, want)
	}

	demo, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark}, start, end, "", "", "", "", nil, domain.TimeScope{})
	if err != nil {
		t.Fatalf("GetWeightDemographics: %v", err)
	}
	var breed *domain.WeightGainBucket
	for i := range demo.GainByBreed {
		if demo.GainByBreed[i].Label == "Three Weigh Breed" {
			breed = &demo.GainByBreed[i]
		}
	}
	if breed == nil {
		t.Fatalf("no breed gain bucket for the kid; got %#v", demo.GainByBreed)
	}
	if math.Abs(breed.MedianGainGPerDay-want) > 0.01 {
		t.Fatalf("gain-by-breed = %.2f g/day, want %.2f -- the breed chart must report the same statistic as the headline",
			breed.MedianGainGPerDay, want)
	}

	var breedWeek *domain.WeightGainBreedWeekBucket
	for i := range demo.GainByBreedWeek {
		if demo.GainByBreedWeek[i].Label == "Three Weigh Breed" && demo.GainByBreedWeek[i].WeekStart == "2026-08-31" {
			breedWeek = &demo.GainByBreedWeek[i]
		}
	}
	if breedWeek == nil {
		t.Fatalf("no breed/week gain bucket for the week holding both legs; got %#v", demo.GainByBreedWeek)
	}
	if math.Abs(breedWeek.AverageGainGPerDay-want) > 0.01 {
		t.Fatalf("gain-by-breed week 2026-08-31 = %.2f g/day, want %.2f", breedWeek.AverageGainGPerDay, want)
	}
}
