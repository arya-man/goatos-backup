package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// Adversarial coverage for the load-wise growth rollup. Each test seeds real rows and asserts the
// number the chart renders, because each is a way the blend can be quietly wrong while still
// returning a plausible-looking bar.

const (
	loadCampaignTwo  = "00000000-0000-4000-8000-00000000a001"
	loadShedScopeTwo = "00000000-0000-4000-8000-00000000a101"
)

// A second campaign, so one LOCATION can hold two weighs. weighing_shed_observations is UNIQUE on
// (tenant_id, campaign_shed_id) WHERE withdrawn_at IS NULL, so a shed's history necessarily lives
// across buckets — which is exactly why the gain CTEs key on location_id rather than bucket.
func seedLoadSecondCampaign(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaigns (campaign_id, tenant_id, park_id, period_start_date, period_end_date, start_business_date, status, planned_cap_per_day, operator_user_id, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, '2026-07-06', '2026-07-12', '2026-07-08', 'published', 100, $4::uuid, $4::uuid)
ON CONFLICT (campaign_id) DO NOTHING`, loadCampaignTwo, repoTenant, repoPark, repoOperator)
}

func seedLoadBucket(t *testing.T, ctx context.Context, pool *pgxpool.Pool, bucketID, campaignID, locationID, category string) {
	t.Helper()
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, weighing_category, operator_user_id, expected_animal_count)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'shed', 'load-test', $5, $6::uuid, 1)
ON CONFLICT (campaign_shed_id) DO UPDATE SET weighing_category = EXCLUDED.weighing_category`,
		bucketID, campaignID, repoTenant, locationID, category, repoOperator)
}

func seedLoadLumpWeigh(t *testing.T, ctx context.Context, pool *pgxpool.Pool, bucketID, campaignID, proofID string, avgKg float64, animals int, at time.Time) {
	t.Helper()
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_shed_observations (tenant_id, campaign_id, campaign_shed_id, weight_kg, average_weight_kg, animal_count, proof_artifact_id, recorded_by, idempotency_key, accepted_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7::uuid, $8::uuid, $9, $10::timestamptz)
ON CONFLICT (tenant_id, idempotency_key) DO NOTHING`,
		repoTenant, campaignID, bucketID, avgKg*float64(animals), avgKg, animals, proofID, repoOperator,
		fmt.Sprintf("loadweights:%s:%d", bucketID, at.UnixNano()), at)
}

func seedLoadTag(t *testing.T, ctx context.Context, pool *pgxpool.Pool, locationID, loadRef, owner string) {
	t.Helper()
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_shed_load_tags (tenant_id, location_id, load_ref, owner_name)
VALUES ($1::uuid, $2::uuid, $3, $4)
ON CONFLICT (tenant_id, location_id, load_ref) DO UPDATE SET owner_name = EXCLUDED.owner_name`,
		repoTenant, locationID, loadRef, owner)
}

// ONE-TO-MANY FAN-OUT, the defect this rollup is most exposed to. A shed genuinely can hold
// animals from TWO loads — CPT Mandela 1 Part 1 holds loads 100 and 101 today — and the mapping's
// primary key admits that on purpose. The shed then has ONE average and TWO claimants.
//
// The wrong answers are both plausible: join the tag table naively and the shed's animals are
// counted once per load (63 head reported as 126 across the chart); apportion the average by head
// count and the chart invents a per-supplier distribution nobody measured. The honest answer is to
// attribute it to NEITHER and say so, which is what `tag`'s HAVING count(*) = 1 does.
func TestLoadWeightsOneToManyExcludesAShedCarryingTwoLoads(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	seedLoadLumpWeigh(t, ctx, pool, repoShedScope, repoCampaign, repoShedProof, 22.0, 40,
		time.Date(2026, 7, 20, 6, 0, 0, 0, time.UTC))
	// The same shed claimed by two loads.
	seedLoadTag(t, ctx, pool, repoPerShed, "L-100", "Supplier A")
	seedLoadTag(t, ctx, pool, repoPerShed, "L-101", "Supplier B")

	from, to := shedWeightsWindow()
	out, err := repo.GetShedWeights(ctx, repoTenant, []string{repoPark}, from, to)
	if err != nil {
		t.Fatalf("GetShedWeights: %v", err)
	}

	for _, load := range out.ByLoad {
		if load.LoadRef == "L-100" || load.LoadRef == "L-101" {
			t.Fatalf("a shed carrying two loads must be attributed to NEITHER, got load %q with %d shed(s) / %d animals",
				load.LoadRef, load.Sheds, load.Animals)
		}
	}
	if out.LoadUnattributedSheds < 1 {
		t.Fatalf("the ambiguous shed must be reported as unattributed, got %d", out.LoadUnattributedSheds)
	}
}

// PAGE BOUNDARY. The load blend is a WHOLE-FILTER aggregate over every tagged shed in scope, not a
// rollup of the shed rows this response happened to return. It must also span BOTH capture modes:
// a load placed into one lump-sum shed and one per-animal shed is still one load, and reading only
// weighing_shed_observations (the shape the shed chart's own gain CTE uses) would silently halve it.
func TestLoadWeightsPageBoundaryBlendsEveryTaggedShedAcrossBothCaptureModes(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	day := time.Date(2026, 7, 20, 6, 0, 0, 0, time.UTC)
	// Lump-sum shed: 40 animals averaging 22.0.
	seedLoadLumpWeigh(t, ctx, pool, repoShedScope, repoCampaign, repoShedProof, 22.0, 40, day)
	// Per-animal shed in the SAME load: two scanned tags.
	seedShedWeightScan(t, ctx, pool, "LOAD-A", 18.0, day)
	seedShedWeightScan(t, ctx, pool, "LOAD-B", 20.0, day)

	seedLoadTag(t, ctx, pool, repoPerShed, "L-131", "Shared Supplier")
	seedLoadTag(t, ctx, pool, repoExpectedShed, "L-131", "Shared Supplier")

	from, to := shedWeightsWindow()
	out, err := repo.GetShedWeights(ctx, repoTenant, []string{repoPark}, from, to)
	if err != nil {
		t.Fatalf("GetShedWeights: %v", err)
	}

	var found bool
	for _, load := range out.ByLoad {
		if load.LoadRef != "L-131" {
			continue
		}
		found = true
		if load.Sheds != 2 {
			t.Fatalf("both capture modes must contribute: want 2 sheds, got %d", load.Sheds)
		}
		if load.Animals != 42 {
			t.Fatalf("animals must sum both sheds (40 lump + 2 scanned): want 42, got %d", load.Animals)
		}
		// Weighted mean over ANIMALS, not the mean of the two shed averages (which would
		// be (22.0 + 19.0)/2 = 20.5 and let a 2-animal shed pull as hard as a 40-animal one).
		want := (22.0*40 + 19.0*2) / 42
		if fmt.Sprintf("%.3f", load.AverageWeightKg) != fmt.Sprintf("%.3f", want) {
			t.Fatalf("average must be head-weighted (%.3f), got %.3f", want, load.AverageWeightKg)
		}
	}
	if !found {
		t.Fatal("expected the tagged load to appear in by_load")
	}
}

// PARK SCOPE. The repository authorizes nothing itself — the service resolves the park list — so it
// must honour exactly the parks it is handed. A load whose sheds sit in another park returns
// nothing rather than leaking across the tenant's estate.
func TestLoadWeightsParkScopeReturnsNothingOutsideTheRequestedParks(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	seedLoadLumpWeigh(t, ctx, pool, repoShedScope, repoCampaign, repoShedProof, 22.0, 40,
		time.Date(2026, 7, 20, 6, 0, 0, 0, time.UTC))
	seedLoadTag(t, ctx, pool, repoPerShed, "L-126", "Other Park Supplier")

	from, to := shedWeightsWindow()
	other := "00000000-0000-4000-8000-0000000030ff"
	out, err := repo.GetShedWeights(ctx, repoTenant, []string{other}, from, to)
	if err != nil {
		t.Fatalf("GetShedWeights: %v", err)
	}
	if len(out.ByLoad) != 0 {
		t.Fatalf("a foreign park must yield no loads, got %d", len(out.ByLoad))
	}
	if out.LoadUnattributedSheds != 0 {
		t.Fatalf("a foreign park must yield no unattributed sheds either, got %d", out.LoadUnattributedSheds)
	}
}

// STATUS MATRIX. weighing_campaign_sheds.status spans pending / in_progress / completed / canceled.
// Canceled is work that was called off, so it must leave the load rollup entirely — otherwise a
// supplier is judged on a weigh nobody intended to take. Every other status contributes.
func TestLoadWeightsStatusMatrixExcludesOnlyCanceledBuckets(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	seedLoadLumpWeigh(t, ctx, pool, repoShedScope, repoCampaign, repoShedProof, 22.0, 40,
		time.Date(2026, 7, 20, 6, 0, 0, 0, time.UTC))
	seedLoadTag(t, ctx, pool, repoPerShed, "L-128", "Status Supplier")
	from, to := shedWeightsWindow()

	for _, status := range []string{"pending", "in_progress", "completed", "canceled"} {
		execWeighingTestSQL(t, ctx, pool,
			`UPDATE weighing_campaign_sheds SET status = $1 WHERE campaign_shed_id = $2::uuid`,
			status, repoShedScope)

		out, err := repo.GetShedWeights(ctx, repoTenant, []string{repoPark}, from, to)
		if err != nil {
			t.Fatalf("GetShedWeights(%s): %v", status, err)
		}
		present := false
		for _, load := range out.ByLoad {
			if load.LoadRef == "L-128" {
				present = true
			}
		}
		if status == "canceled" && present {
			t.Fatal("a canceled bucket must leave the load rollup entirely")
		}
		if status != "canceled" && !present {
			t.Fatalf("status %q must still contribute to its load", status)
		}
	}
}

// GAIN GRAIN. The blend must use each shed's LAST TWO weighs and the days between them — the same
// method the shed chart and the legacy dashboard's adg_goat_last2 use — and must weight by head
// count. A load whose sheds were weighed only once has a weight but NO gain, and must report nil
// rather than 0, which would read as "this supplier's kids are flat".
func TestLoadWeightsGainUsesLastTwoWeighsAndIsNilWithoutASecond(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	seedLoadSecondCampaign(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	from, to := shedWeightsWindow()

	// One weigh only: a weight, but no growth.
	seedLoadLumpWeigh(t, ctx, pool, repoShedScope, repoCampaign, repoShedProof, 22.0, 40,
		time.Date(2026, 7, 20, 6, 0, 0, 0, time.UTC))
	seedLoadTag(t, ctx, pool, repoPerShed, "L-129", "Single Weigh")

	out, err := repo.GetShedWeights(ctx, repoTenant, []string{repoPark}, from, to)
	if err != nil {
		t.Fatalf("GetShedWeights: %v", err)
	}
	for _, load := range out.ByLoad {
		if load.LoadRef == "L-129" && load.GainGPerDay != nil {
			t.Fatalf("a single weigh cannot produce a gain, got %.1f g/day", *load.GainGPerDay)
		}
	}

	// Add an EARLIER weigh in its own bucket: 20.0 on 10 Jul -> 22.0 on 20 Jul is
	// 2.0 kg over 10 days = 200 g/day.
	seedLoadBucket(t, ctx, pool, loadShedScopeTwo, loadCampaignTwo, repoPerShed, "per_shed_partition")
	seedLoadLumpWeigh(t, ctx, pool, loadShedScopeTwo, loadCampaignTwo, repoShedProofTwo, 20.0, 40,
		time.Date(2026, 7, 10, 6, 0, 0, 0, time.UTC))

	out, err = repo.GetShedWeights(ctx, repoTenant, []string{repoPark}, from, to)
	if err != nil {
		t.Fatalf("GetShedWeights after second weigh: %v", err)
	}
	var found bool
	for _, load := range out.ByLoad {
		if load.LoadRef != "L-129" {
			continue
		}
		found = true
		if load.GainGPerDay == nil {
			t.Fatal("two weighs must produce a gain")
		}
		if got := fmt.Sprintf("%.1f", *load.GainGPerDay); got != "200.0" {
			t.Fatalf("gain must be (22.0-20.0)kg over 10 days = 200 g/day, got %s", got)
		}
		if load.GainSpanDays != 10 {
			t.Fatalf("the span must travel with the number: want 10, got %d", load.GainSpanDays)
		}
	}
	if !found {
		t.Fatal("expected the load to appear once it has two weighs")
	}
}
