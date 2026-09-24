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

// A LOAD IS ITS ANIMALS (maintainer decision 2026-09-24): a load's figures follow its own animals
// through every pen move, and a newly bought load appears the moment its animals are weighed --
// nothing is tagged. One fixture:
//
//	Darwin (scanned pen) and Perth (whole-pen weighs on 2, 8 and 15 Aug)
//	Load L-7: A scanned in Darwin 1 and 4 Aug, MOVED to Perth on 5 Aug; B moved Darwin -> Perth on
//	          5 Aug too, never scanned; C stays in Darwin, scanned 1 and 15 Aug.
//	Load L-8: D (male) and E (female), in Perth all along.
//
// Expected gains (total grams / total days per animal, then the mean per load):
//
//	A  scan 20 -> 21 over 3 days, then Perth 8 -> 15 Aug (present at both) 24 -> 25.4 over 7 days
//	   = 2400 g / 10 d = 240
//	B  only Perth 8 -> 15 Aug (NOT there on 2 Aug)                          = 1400 g / 7 d = 200
//	C  scan 22 -> 25 over 14 days                                           = 3000 / 14    = 214.29
//	D, E  Perth 2 -> 8 Aug (22 -> 24) and 8 -> 15 Aug                       = 3400 / 13    = 261.54
const (
	laCampaignA, laCampaignB, laCampaignC = "00000000-0000-4000-8000-00000000e001", "00000000-0000-4000-8000-00000000e002", "00000000-0000-4000-8000-00000000e003"
	laBucketA, laBucketB, laBucketC       = "00000000-0000-4000-8000-00000000e101", "00000000-0000-4000-8000-00000000e102", "00000000-0000-4000-8000-00000000e103"
	laLoad7, laLoad8                      = "00000000-0000-4000-8000-00000000e201", "00000000-0000-4000-8000-00000000e202"
	laGoatA, laGoatB, laGoatC             = "00000000-0000-4000-8000-00000000e301", "00000000-0000-4000-8000-00000000e302", "00000000-0000-4000-8000-00000000e303"
	laGoatD, laGoatE                      = "00000000-0000-4000-8000-00000000e304", "00000000-0000-4000-8000-00000000e305"
)

func laAt(day int) time.Time { return time.Date(2026, 8, day, 6, 0, 0, 0, time.UTC) }

func seedLoadAnimalsFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	seedWeighingObservationFixture(t, ctx, pool)
	// Digit-free names, so the pen bridge reads each as its own undivided pen.
	execWeighingTestSQL(t, ctx, pool, `UPDATE locations SET name = 'Darwin' WHERE location_id = $1::uuid`, repoExpectedShed)
	execWeighingTestSQL(t, ctx, pool, `UPDATE locations SET name = 'Perth' WHERE location_id = $1::uuid`, repoPerShed)

	for _, g := range []struct{ id, display, tag, sex, shed string }{
		{laGoatA, "G-930301", "la-a", "male", repoPerShed}, {laGoatB, "G-930302", "la-b", "male", repoPerShed}, {laGoatC, "G-930303", "la-c", "male", repoExpectedShed},
		{laGoatD, "G-930304", "la-d", "male", repoPerShed}, {laGoatE, "G-930305", "la-e", "female", repoPerShed},
	} {
		execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, breed, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, $3, 'Load Breed', $4, 'kid', 'alive', 'kid', $5::uuid, $6::uuid, $7::uuid, $6::uuid)`,
			g.id, repoTenant, g.display, g.sex, repoParty, g.shed, repoPark)
		execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, normalizer_version)
VALUES ($1::uuid, $2::uuid, 'animal_identifier_1', $3, $3, 'global', true, 'active', now(), 'test')`, repoTenant, g.id, g.tag)
	}
	for _, l := range []struct{ id, ref string }{{laLoad7, "L-7"}, {laLoad8, "L-8"}} {
		execWeighingTestSQL(t, ctx, pool, `
INSERT INTO procurement_loads (load_id, tenant_id, source_party_id, idempotency_key, context, purchase_date)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, jsonb_build_object('load_ref', $5::text), '2026-07-20')`,
			l.id, repoTenant, repoParty, "loadanimals:"+l.ref, l.ref)
	}
	for load, goats := range map[string][]string{laLoad7: {laGoatA, laGoatB, laGoatC}, laLoad8: {laGoatD, laGoatE}} {
		for _, goat := range goats {
			execWeighingTestSQL(t, ctx, pool, `
INSERT INTO procurement_load_goats (load_goat_id, tenant_id, load_id, goat_id) VALUES (gen_random_uuid(), $1::uuid, $2::uuid, $3::uuid)`,
				repoTenant, load, goat)
		}
	}
	// A and B move Darwin -> Perth on 5 Aug.
	for _, goat := range []string{laGoatA, laGoatB} {
		execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goat_location_history (tenant_id, goat_id, from_location_id, to_location_id, occurred_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::timestamptz)`, repoTenant, goat, repoExpectedShed, repoPerShed, laAt(5))
	}
	// Scans in Darwin (the fixture's individual bucket sits on it).
	seedLoadIndividualWeigh(t, ctx, pool, repoAnimalScope, "LA-A", 20.0, laAt(1))
	seedLoadIndividualWeigh(t, ctx, pool, repoAnimalScope, "LA-A", 21.0, laAt(4))
	seedLoadIndividualWeigh(t, ctx, pool, repoAnimalScope, "la-c", 22.0, laAt(1))
	seedLoadIndividualWeigh(t, ctx, pool, repoAnimalScope, "la-c", 25.0, laAt(15))
	// Perth weighed whole three times, one bucket per round.
	for _, w := range []struct {
		campaign, bucket, start string
		avg                     float64
		animals, day            int
	}{
		{laCampaignA, laBucketA, "2026-08-02", 22.0, 2, 2},
		{laCampaignB, laBucketB, "2026-08-08", 24.0, 4, 8},
		{laCampaignC, laBucketC, "2026-08-15", 25.4, 4, 15},
	} {
		seedShedWeightsCampaign(t, ctx, pool, w.campaign, w.start)
		seedLoadBucket(t, ctx, pool, w.bucket, w.campaign, repoPerShed, "per_shed_partition")
		seedLoadLumpWeigh(t, ctx, pool, w.bucket, w.campaign, repoShedProof, w.avg, w.animals, laAt(w.day))
	}
}

func laLoads(t *testing.T, ctx context.Context, repo *Repository, parks []string, sex, origin string) []domain.LoadGainBucket {
	t.Helper()
	repo.cache.EvictAll(ctx)
	out, err := repo.GetShedWeights(domain.WithShedWeightsOptions(ctx, domain.ShedWeightsOptions{IncludeLoads: true}),
		repoTenant, parks, "", laAt(1).Add(-6*time.Hour), laAt(21).Add(-6*time.Hour), sex, origin, "", 0, 0, 0)
	if err != nil {
		t.Fatalf("GetShedWeights: %v", err)
	}
	return out.ByLoad
}

func laNear(t *testing.T, what string, got *float64, want float64) {
	t.Helper()
	if got == nil || math.Abs(*got-want) > 0.01 {
		t.Fatalf("%s: got %v, want %.2f", what, got, want)
	}
}

func TestLoadFollowsItsAnimalsThroughPenMovesOneToManyParkScopeStatusMatrix(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedLoadAnimalsFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	// Nothing tags a pen to a load, anywhere: the loads appear from their animals alone.
	var tags int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM weighing_shed_load_tags WHERE tenant_id = $1::uuid`, repoTenant).Scan(&tags); err != nil {
		t.Fatalf("count tags: %v", err)
	}
	if tags != 0 {
		t.Fatalf("the fixture must carry no load tag, has %d", tags)
	}

	loads := laLoads(t, ctx, repo, []string{repoPark}, "", "")
	l7, l8 := findLoad(t, loads, "L-7"), findLoad(t, loads, "L-8")
	// A (moved pens) 240, B (only the Perth leg it was present for) 200, C 214.29.
	laNear(t, "L-7 gain follows A through its move", l7.GainGPerDay, (240+200+3000.0/14)/3)
	if l7.Animals != 3 || fmt.Sprintf("%.2f", l7.AverageWeightKg) != "25.27" {
		t.Fatalf("L-7 latest: 3 animals averaging (25.4 + 25.4 + 25) / 3, got %d at %.2f", l7.Animals, l7.AverageWeightKg)
	}
	// Where its animals are TODAY: two in Perth, one in Darwin.
	placed := map[string]int{}
	for _, p := range l7.Placements {
		placed[p.OperationalLocationDisplay] = p.Animals
	}
	if placed["Perth"] != 2 || placed["Darwin"] != 1 || len(placed) != 2 {
		t.Fatalf("L-7 pens are where its animals stand now, got %#v", l7.Placements)
	}
	// One-to-many: a pen holding two loads credits each load's own animals, never neither.
	laNear(t, "L-8 shares Perth with L-7 and keeps its own gain", l8.GainGPerDay, 3400.0/13)
	if l8.Animals != 2 {
		t.Fatalf("L-8 holds 2 weighed animals, got %d", l8.Animals)
	}

	// Sex is the animal's own: L-8 on the male page is D alone.
	male := findLoad(t, laLoads(t, ctx, repo, []string{repoPark}, "male", ""), "L-8")
	if male.Animals != 1 {
		t.Fatalf("the male page keeps D only, got %d", male.Animals)
	}
	// Every load animal was bought: Farm born has no loads; Purchased has both.
	if got := laLoads(t, ctx, repo, []string{repoPark}, "", "farm_born"); len(got) != 0 {
		t.Fatalf("farm born holds no load, got %#v", got)
	}
	if got := laLoads(t, ctx, repo, []string{repoPark}, "", "purchased"); len(got) != 2 {
		t.Fatalf("purchased holds both loads, got %d", len(got))
	}
	// Park scope.
	if got := laLoads(t, ctx, repo, []string{"00000000-0000-4000-8000-0000000051ff"}, "", ""); len(got) != 0 {
		t.Fatalf("another park sees no load, got %#v", got)
	}
	// Status: a CANCELED round drops out, so B loses its only leg and L-7 is A and C alone.
	execWeighingTestSQL(t, ctx, pool, `UPDATE weighing_campaign_sheds SET status = 'canceled' WHERE campaign_shed_id = $1::uuid`, laBucketC)
	after := findLoad(t, laLoads(t, ctx, repo, []string{repoPark}, "", ""), "L-7")
	laNear(t, "L-7 without the canceled round", after.GainGPerDay, (1000.0/3+3000.0/14)/2)
}

// The Time-wise per-load series buckets each animal's legs by their LATER weigh: week of 3 Aug holds
// A's scan leg (to 4 Aug); week of 10 Aug holds the Perth 8 -> 15 leg (A, B) and C's leg (to 15 Aug).
// The selected pen narrows the series to legs taken in that pen.
func TestLoadWeeksFollowTheAnimalsAndThePenScopePageBoundary(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedLoadAnimalsFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	read := func(scope domain.TimeScope) map[string]domain.WeightGainLoadWeekBucket {
		repo.cache.EvictAll(ctx)
		demo, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark}, laAt(1).Add(-6*time.Hour), laAt(21).Add(-6*time.Hour),
			"", "", "", "weekly_gain", nil, scope)
		if err != nil {
			t.Fatalf("GetWeightDemographics: %v", err)
		}
		out := map[string]domain.WeightGainLoadWeekBucket{}
		for _, row := range demo.GainByLoadWeek {
			out[row.LoadRef+"@"+row.WeekStart] = row
		}
		return out
	}
	weeks := read(domain.TimeScope{})
	first, second := weeks["L-7@2026-08-03"], weeks["L-7@2026-08-10"]
	if first.Animals != 1 || math.Abs(first.AverageGainGPerDay-1000.0/3) > 0.01 {
		t.Fatalf("week of 3 Aug: A's scan leg alone, got %#v", first)
	}
	if second.Animals != 3 || math.Abs(second.AverageGainGPerDay-(200+200+3000.0/14)/3) > 0.01 {
		t.Fatalf("week of 10 Aug: A and B in Perth plus C, got %#v", second)
	}
	// Narrowed to Perth: C's Darwin leg and A's Darwin scan leg drop out.
	perth := read(domain.TimeScope{PenLocationID: laBucketPenKey(), PenPartitionLabel: ""})
	if _, ok := perth["L-7@2026-08-03"]; ok {
		t.Fatalf("A's Darwin leg is not a Perth leg: %#v", perth)
	}
	if got := perth["L-7@2026-08-10"]; got.Animals != 2 || math.Abs(got.AverageGainGPerDay-200) > 0.01 {
		t.Fatalf("Perth, week of 10 Aug: A and B at 200, got %#v", got)
	}
}

func laBucketPenKey() string { return repoPerShed }
