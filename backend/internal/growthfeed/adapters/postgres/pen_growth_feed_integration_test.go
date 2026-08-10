package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/growthfeed/domain"
	"github.com/vgoats/goatos/backend/internal/growthfeed/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// Adversarial coverage for the two pen-comparison queries. Each test seeds real
// rows and asserts the number the screen renders, because every one of these is a
// way the query can be quietly wrong while still returning plausible data.

// asOf is the ration resolution date used throughout; the weighing window brackets it.
const asOf = "2026-08-01"

func penWindow() (time.Time, time.Time) {
	return time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
}

type penFixture struct {
	tenant, park       string
	castro, mandela    string // physical sheds holding the live animals
	castroPen, mandPen string // the weighing buckets' locations
	campaign           string
	operator, party    string
	proof              string
	bucketCastro       string
	bucketMandela      string
}

func seedPenFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) penFixture {
	t.Helper()
	f := penFixture{
		tenant: uuid.NewString(), park: uuid.NewString(),
		castro: uuid.NewString(), mandela: uuid.NewString(),
		campaign: uuid.NewString(), operator: uuid.NewString(), party: uuid.NewString(),
		proof:        uuid.NewString(),
		bucketCastro: uuid.NewString(), bucketMandela: uuid.NewString(),
	}
	// The weighing buckets point at the SHEDS themselves here; the partition fallback
	// gets its own test below.
	f.castroPen, f.mandPen = f.castro, f.mandela

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v\nSQL: %s", err, sql)
		}
	}

	exec(`INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'Pen Test', 'active')
ON CONFLICT DO NOTHING`, f.tenant)
	exec(`INSERT INTO parties (party_id, party_type, display_name, status)
VALUES ($1::uuid, 'org', 'Pen Test Farm', 'active') ON CONFLICT DO NOTHING`, f.party)
	exec(`INSERT INTO locations (tenant_id, location_id, location_type, name, status)
VALUES ($1::uuid, $2::uuid, 'park', 'CBE', 'active')`, f.tenant, f.park)
	for _, shed := range []struct{ id, name string }{{f.castro, "Castro"}, {f.mandela, "Mandela 1"}} {
		exec(`INSERT INTO locations (tenant_id, location_id, parent_location_id, location_type, name, status)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'shed', $4, 'active')`, f.tenant, shed.id, f.park, shed.name)
	}

	// Both sheds hold the SAME kind of animal — one breed, one sex, one stage — which
	// is what makes them peers and what the whole comparison turns on.
	for _, g := range []struct {
		shed  string
		count int
	}{{f.castro, 4}, {f.mandela, 4}} {
		for i := 0; i < g.count; i++ {
			exec(`INSERT INTO goats (goat_id, tenant_id, display_id, breed, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, $3, 'Sirohi', 'female', 'adult', 'alive', 'Grower', $4::uuid, $5::uuid, $6::uuid, $5::uuid)`,
				uuid.NewString(), f.tenant, fmt.Sprintf("G-%06d", nextDisplay()), f.party, g.shed, f.park)
		}
	}

	exec(`INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'operator', 'park', $3::uuid, 'active', now())`, f.tenant, f.operator, f.park)
	exec(`INSERT INTO weighing_campaigns (campaign_id, tenant_id, park_id, period_start_date, period_end_date, start_business_date, status, planned_cap_per_day, operator_user_id, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, '2026-07-01', '2026-08-31', '2026-07-01', 'published', 100, $4::uuid, $4::uuid)`,
		f.campaign, f.tenant, f.park, f.operator)
	exec(`INSERT INTO weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, weighing_category, operator_user_id, expected_animal_count)
VALUES ($1::uuid, $3::uuid, $4::uuid, $5::uuid, 'shed', 'Castro', 'individual_animal', $7::uuid, 4),
       ($2::uuid, $3::uuid, $4::uuid, $6::uuid, 'shed', 'Mandela 1', 'individual_animal', $7::uuid, 4)`,
		f.bucketCastro, f.bucketMandela, f.campaign, f.tenant, f.castroPen, f.mandPen, f.operator)
	exec(`INSERT INTO proof_artifacts (proof_id, tenant_id, storage_provider, object_key, mime_type, upload_state, scope_type, scope_id, subject_type, subject_id, proof_type, uploaded_by, uploaded_at)
VALUES ($1::uuid, $2::uuid, 'local', 'pen-test/' || $1, 'video/mp4', 'completed', 'shed', $3::uuid, 'shed', $3::uuid, 'video', $4::uuid, now())`,
		f.proof, f.tenant, f.castroPen, f.operator)

	// ---- authored feed config ----
	exec(`INSERT INTO feed_ration_groups (tenant_id, breed_label, ration_group_label)
VALUES ($1::uuid, 'Sirohi', 'Sirohi')`, f.tenant)
	exec(`INSERT INTO feed_shed_tags (tenant_id, shed_tag_label, applies_to, status)
VALUES ($1::uuid, 'Grower', 'adult', 'active')`, f.tenant)
	exec(`INSERT INTO feed_item_catalog (tenant_id, feed_item_label, energy_kcal_per_kg, status, display_order)
VALUES ($1::uuid, 'Maize', 3200, 'active', 1),
       ($1::uuid, 'Hay', 1800, 'active', 2)`, f.tenant)
	exec(`INSERT INTO feed_ration_rates (tenant_id, park_id, ration_group_label, shed_tag_label, feed_item_label, grams_per_head, valid_from)
VALUES ($1::uuid, $2::uuid, 'Sirohi', 'Grower', 'Maize', 600, '2026-07-01'),
       ($1::uuid, $2::uuid, 'Sirohi', 'Grower', 'Hay',   400, '2026-07-01')`, f.tenant, f.park)
	return f
}

var displaySeq = 900000

func nextDisplay() int { displaySeq++; return displaySeq }

func seedScan(t *testing.T, ctx context.Context, pool *pgxpool.Pool, f penFixture, bucket, tag string, kg float64, at time.Time) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO weighing_observations (tenant_id, campaign_id, campaign_shed_id, scanned_identifier, weight_kg, proof_artifact_id, recorded_by, idempotency_key, accepted_at, submitted_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6::uuid, $7::uuid, $8, $9::timestamptz, $9::timestamptz)`,
		f.tenant, f.campaign, bucket, tag, kg, f.proof, f.operator,
		fmt.Sprintf("pen:%s:%s:%d", bucket, tag, at.UnixNano()), at); err != nil {
		t.Fatalf("seed scan: %v", err)
	}
}

func penByLocation(rows []ports.PenGrowth, locationID string) (ports.PenGrowth, bool) {
	for _, row := range rows {
		if row.LocationID == locationID {
			return row, true
		}
	}
	return ports.PenGrowth{}, false
}

func rationByLocation(rows []ports.PenCohortRation, locationID string) (ports.PenCohortRation, bool) {
	for _, row := range rows {
		if row.LocationID == locationID {
			return row, true
		}
	}
	return ports.PenCohortRation{}, false
}

// ONE-TO-MANY FAN-OUT, on BOTH halves of the comparison at once.
//
// Growth side: weighing_observations keeps superseded rows, so a bare count(*)
// reports more animals than the pen holds and averages over captures instead of
// animals.
//
// Ration side: two authored rate rows can legitimately be in force for the same
// cell on the same date — the open-row unique index only forbids two rows with a
// NULL valid_to, and a row closed in the FUTURE is still valid today. Without the
// DISTINCT ON, that pen's grams are counted twice and its feed-per-kg-gain doubles.
func TestPenGrowthFeedOneToManyDeduplicatesRepeatScansAndOverlappingRationRows(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	f := seedPenFixture(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)

	day := time.Date(2026, 7, 10, 6, 0, 0, 0, time.UTC)
	// TAG-A scanned three times on one day; only its latest weight is the animal's.
	seedScan(t, ctx, pool, f, f.bucketCastro, "TAG-A", 11.0, day)
	seedScan(t, ctx, pool, f, f.bucketCastro, "TAG-A", 15.0, day.Add(2*time.Hour))
	seedScan(t, ctx, pool, f, f.bucketCastro, "TAG-A", 20.0, day.Add(4*time.Hour))
	seedScan(t, ctx, pool, f, f.bucketCastro, "TAG-B", 30.0, day)

	// A SECOND rate row for Maize, in force on the same date because its valid_to is
	// in the future. Both rows satisfy the as-of predicate.
	if _, err := pool.Exec(ctx, `
INSERT INTO feed_ration_rates (tenant_id, park_id, ration_group_label, shed_tag_label, feed_item_label, grams_per_head, valid_from, valid_to)
VALUES ($1::uuid, $2::uuid, 'Sirohi', 'Grower', 'Maize', 500, '2026-06-01', '2026-12-01')`,
		f.tenant, f.park); err != nil {
		t.Fatalf("seed overlapping rate: %v", err)
	}

	from, to := penWindow()
	growth, err := repo.ListPenGrowth(ctx, f.tenant, []string{f.park}, from, to)
	if err != nil {
		t.Fatalf("ListPenGrowth: %v", err)
	}
	castro, ok := penByLocation(growth, f.castroPen)
	if !ok {
		t.Fatal("Castro pen missing from the growth read")
	}
	if castro.AnimalsWeighed != 2 {
		t.Fatalf("animals must count DISTINCT tags, not captures: want 2, got %d", castro.AnimalsWeighed)
	}
	if castro.AverageWeightKg == nil || fmt.Sprintf("%.1f", *castro.AverageWeightKg) != "25.0" {
		t.Fatalf("average must use each tag's latest weight (20+30)/2 = 25.0, got %v", castro.AverageWeightKg)
	}

	rations, err := repo.ListPenCohortRation(ctx, f.tenant, []string{f.park}, []string{f.castroPen}, asOf)
	if err != nil {
		t.Fatalf("ListPenCohortRation: %v", err)
	}
	ration, ok := rationByLocation(rations, f.castroPen)
	if !ok {
		t.Fatal("Castro pen missing from the ration read")
	}
	// Two active items, each resolving ONCE: 600 Maize (the newer row wins) + 400 Hay.
	if ration.Plan.ItemsConfigured != 2 {
		t.Fatalf("items configured = %d, want 2 — the duplicate rate row fanned the pen out", ration.Plan.ItemsConfigured)
	}
	if ration.Plan.ItemsBlocked != 0 {
		t.Fatalf("items blocked = %d, want 0", ration.Plan.ItemsBlocked)
	}
	if got := ration.Plan.PlannedGramsPerHeadDay; got != 1000 {
		t.Fatalf("planned grams = %v, want 1000 (600 newest Maize + 400 Hay); 1500 means both Maize rows were summed", got)
	}
}

// STATUS BUCKETS. Three different "this row does not count" states live on three
// different tables, and each one silently inflates a different number if missed:
// a canceled bucket adds a pen that nobody intended to weigh, a rejected
// observation counts a weight the verifier threw out, and a withdrawn shed
// observation counts a superseded whole-shed total.
func TestPenGrowthFeedStatusBucketsExcludeCanceledRejectedAndWithdrawn(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	f := seedPenFixture(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)
	from, to := penWindow()

	day := time.Date(2026, 7, 10, 6, 0, 0, 0, time.UTC)
	seedScan(t, ctx, pool, f, f.bucketCastro, "GOOD-1", 20.0, day)

	// THE ACTUAL STATUS DOMAIN. weighing_observations.verification_status is
	// CHECK-constrained to pending / verified / rework (migration 000058) — there is
	// NO 'rejected' value. The `<> 'rejected'` predicate this query carries is
	// inherited verbatim from weighing's own growth and shed-weights reads, and on
	// this schema it excludes nothing at all.
	//
	// Asserted rather than quietly relied on, for two reasons: the shared predicate
	// must keep meaning the same thing on both surfaces (that is the whole parity
	// contract), and if 'rejected' is ever added to the domain, this test is what
	// tells the next author that three queries change together.
	if _, err := pool.Exec(ctx,
		`UPDATE weighing_observations SET verification_status = 'rejected' WHERE scanned_identifier = 'GOOD-1'`); err == nil {
		t.Fatal("verification_status accepted 'rejected'; the status domain changed and every <> 'rejected' filter now needs review")
	}

	// A weight in REWORK is still a real measurement and stays counted, exactly as
	// it does on the Weights screen's own charts. Diverging here would make the two
	// surfaces disagree about the same pen.
	seedScan(t, ctx, pool, f, f.bucketCastro, "REWORK-1", 99.0, day)
	if _, err := pool.Exec(ctx,
		`UPDATE weighing_observations SET verification_status = 'rework' WHERE scanned_identifier = 'REWORK-1'`); err != nil {
		t.Fatalf("set rework: %v", err)
	}
	// A pending (unverified) weight likewise counts — asserting it stops a future
	// "tighten the filter" change from hiding most of the estate.
	seedScan(t, ctx, pool, f, f.bucketCastro, "PENDING-1", 22.0, day.Add(time.Hour))

	growth, err := repo.ListPenGrowth(ctx, f.tenant, []string{f.park}, from, to)
	if err != nil {
		t.Fatalf("ListPenGrowth: %v", err)
	}
	castro, _ := penByLocation(growth, f.castroPen)
	if castro.AnimalsWeighed != 3 {
		t.Fatalf("pending and rework weights must both count: want 3 animals, got %d", castro.AnimalsWeighed)
	}

	// Canceled bucket: the pen leaves the comparison entirely.
	if _, err := pool.Exec(ctx,
		`UPDATE weighing_campaign_sheds SET status = 'canceled' WHERE campaign_shed_id = $1::uuid`, f.bucketMandela); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	growth, _ = repo.ListPenGrowth(ctx, f.tenant, []string{f.park}, from, to)
	if _, present := penByLocation(growth, f.mandPen); present {
		t.Fatal("a canceled bucket must leave the comparison entirely")
	}

	// Withdrawn whole-shed observation: not counted.
	if _, err := pool.Exec(ctx, `
UPDATE weighing_campaign_sheds SET weighing_category = 'per_shed_partition', status = 'pending'
WHERE campaign_shed_id = $1::uuid`, f.bucketMandela); err != nil {
		t.Fatalf("recategorize: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO weighing_shed_observations (tenant_id, campaign_id, campaign_shed_id, weight_kg, average_weight_kg, animal_count, proof_artifact_id, recorded_by, idempotency_key, accepted_at, withdrawn_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, 200, 50, 4, $4::uuid, $5::uuid, 'withdrawn-1', $6::timestamptz, now())`,
		f.tenant, f.campaign, f.bucketMandela, f.proof, f.operator, day); err != nil {
		t.Fatalf("seed withdrawn: %v", err)
	}
	growth, _ = repo.ListPenGrowth(ctx, f.tenant, []string{f.park}, from, to)
	mandela, ok := penByLocation(growth, f.mandPen)
	if !ok {
		t.Fatal("Mandela pen should be back in scope once un-canceled")
	}
	if mandela.AnimalsWeighed != 0 || mandela.AverageWeightKg != nil {
		t.Fatalf("a withdrawn whole-shed weigh must not count: animals=%d avg=%v",
			mandela.AnimalsWeighed, mandela.AverageWeightKg)
	}
}

// MULTI-PAGE / WHOLE-SCOPE TRUTH. The peer median is a WHOLE-FILTER aggregate: it
// must range over every comparable pen in scope, never over the rows a client
// happens to be showing. This test proves the read returns the whole scope in one
// answer and that the benchmark shifts when a pen outside the first ten is added —
// the failure mode being a median computed from a visible page, which would change
// every time somebody paged the table.
func TestPenGrowthFeedMultiPageMedianRangesOverWholeScopeNotTheVisiblePage(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	f := seedPenFixture(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)
	from, to := penWindow()

	// Twelve more pens, all Sirohi growers, so the comparable set is comfortably
	// larger than one screen of ten rows.
	extra := make([]string, 0, 12)
	for i := 0; i < 12; i++ {
		shed, bucket := uuid.NewString(), uuid.NewString()
		if _, err := pool.Exec(ctx, `
INSERT INTO locations (tenant_id, location_id, parent_location_id, location_type, name, status)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'shed', $4, 'active')`,
			f.tenant, shed, f.park, fmt.Sprintf("Pen %02d", i)); err != nil {
			t.Fatalf("seed shed: %v", err)
		}
		if _, err := pool.Exec(ctx, `
INSERT INTO goats (goat_id, tenant_id, display_id, breed, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, $3, 'Sirohi', 'female', 'adult', 'alive', 'Grower', $4::uuid, $5::uuid, $6::uuid, $5::uuid)`,
			uuid.NewString(), f.tenant, fmt.Sprintf("G-%06d", nextDisplay()), f.party, shed, f.park); err != nil {
			t.Fatalf("seed goat: %v", err)
		}
		if _, err := pool.Exec(ctx, `
INSERT INTO weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, weighing_category, operator_user_id, expected_animal_count)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'shed', $5, 'individual_animal', $6::uuid, 1)`,
			bucket, f.campaign, f.tenant, shed, fmt.Sprintf("Pen %02d", i), f.operator); err != nil {
			t.Fatalf("seed bucket: %v", err)
		}
		// Each pen gains a different amount, so the median is a real choice.
		start := time.Date(2026, 7, 1, 6, 0, 0, 0, time.UTC)
		tag := fmt.Sprintf("EXTRA-%02d", i)
		seedScan(t, ctx, pool, f, bucket, tag, 20.0, start)
		seedScan(t, ctx, pool, f, bucket, tag, 20.0+float64(i+1), start.AddDate(0, 0, 10))
		extra = append(extra, shed)
	}

	growth, err := repo.ListPenGrowth(ctx, f.tenant, []string{f.park}, from, to)
	if err != nil {
		t.Fatalf("ListPenGrowth: %v", err)
	}
	if len(growth) < 14 {
		t.Fatalf("whole scope returned %d pens, want every pen in one answer (14+)", len(growth))
	}

	locations := make([]string, 0, len(growth))
	for _, row := range growth {
		locations = append(locations, row.LocationID)
	}
	rations, err := repo.ListPenCohortRation(ctx, f.tenant, []string{f.park}, locations, asOf)
	if err != nil {
		t.Fatalf("ListPenCohortRation: %v", err)
	}

	pens := assemble(growth, rations)
	_, _, _, comparable := domain.Benchmark(pens)
	if comparable < 12 {
		t.Fatalf("comparable pens = %d, want every seeded pen benchmarked (12+)", comparable)
	}

	// The median over the whole scope, versus the median over only the first ten
	// rows a client would show. They must differ, or this test proves nothing about
	// where the aggregate is computed.
	whole := medianOf(pens)
	firstPage := pens[:10]
	pageCopy := append([]domain.Pen(nil), firstPage...)
	domain.Benchmark(pageCopy)
	if page := medianOf(pageCopy); page == whole {
		t.Skip("fixture produced identical medians; the scope assertion above is the binding one")
	}
	for _, pen := range pens {
		if pen.PeerMedianADGGPerDay != nil && *pen.PeerMedianADGGPerDay != whole {
			t.Fatalf("pen %s was benchmarked against %v, not the whole-scope median %v",
				pen.ShedName, *pen.PeerMedianADGGPerDay, whole)
		}
	}
	_ = extra
}

// assemble mirrors the service's join so the adapters can be exercised together
// without pulling the whole app layer into a database test.
func assemble(growth []ports.PenGrowth, rations []ports.PenCohortRation) []domain.Pen {
	byLocation := map[string]ports.PenCohortRation{}
	for _, row := range rations {
		byLocation[row.LocationID] = row
	}
	out := make([]domain.Pen, 0, len(growth))
	for _, g := range growth {
		pen := domain.Pen{
			ParkID: g.ParkID, LocationID: g.LocationID, ShedName: g.ShedName,
			WeighingCategory: g.WeighingCategory, AnimalsWeighed: g.AnimalsWeighed,
			AverageWeightKg: g.AverageWeightKg, ADGGPerDay: g.ADGGPerDay,
			ADGBasis: g.ADGBasis, ADGSampleCount: g.ADGSampleCount,
		}
		if ration, ok := byLocation[g.LocationID]; ok {
			if ration.Breed != "" {
				breed := ration.Breed
				pen.Breed = &breed
			}
			if ration.Stage != "" {
				stage := ration.Stage
				pen.Stage = &stage
			}
			pen.LiveAnimals = ration.LiveAnimals
			status, grams, kcal := domain.ResolveFeedPlan(ration.Plan)
			pen.FeedPlanStatus, pen.PlannedFeedGPerHeadDay, pen.PlannedEnergyKcalPerHeadDay = status, grams, kcal
		}
		out = append(out, pen)
	}
	return out
}

func medianOf(pens []domain.Pen) float64 {
	for _, pen := range pens {
		if pen.PeerMedianADGGPerDay != nil {
			return *pen.PeerMedianADGGPerDay
		}
	}
	return 0
}

// The pen the weighing bucket points at is often a PARTITION with no animals of
// its own, while the herd register puts them on the physical shed. Without the
// name-matched fallback every partitioned pen reports an unknown cohort and drops
// out of the comparison — which is most of the estate.
func TestPenCohortFallsBackToTheParentShedWhenThePartitionHoldsNoAnimals(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	f := seedPenFixture(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)

	// "Castro 1" is a partition alias hanging off the PARK, holding no animals; the
	// four Sirohi growers sit on "Castro".
	partition := uuid.NewString()
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (tenant_id, location_id, parent_location_id, location_type, name, status)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'shed', 'Castro 1', 'inactive')`, f.tenant, partition, f.park); err != nil {
		t.Fatalf("seed partition: %v", err)
	}

	rations, err := repo.ListPenCohortRation(ctx, f.tenant, []string{f.park}, []string{partition}, asOf)
	if err != nil {
		t.Fatalf("ListPenCohortRation: %v", err)
	}
	ration, ok := rationByLocation(rations, partition)
	if !ok {
		t.Fatal("the partition resolved to no cohort at all; the parent-shed fallback did not fire")
	}
	if ration.Breed != "Sirohi" || ration.Stage != "Grower" {
		t.Fatalf("cohort = %s/%s, want Sirohi/Grower from the parent shed", ration.Breed, ration.Stage)
	}
	if ration.LiveAnimals != 4 {
		t.Fatalf("live animals = %d, want the parent shed's 4", ration.LiveAnimals)
	}
}

// AGREE OR GO BARE. A pen holding two breeds must be attributed to NEITHER, and
// must therefore be priced by no ration — the alternative is stamping one breed's
// ration onto a mixed pen because it happened to sort first.
func TestMixedBreedPenIsAttributedToNoCohortAndNoRation(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	f := seedPenFixture(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)

	if _, err := pool.Exec(ctx, `
INSERT INTO goats (goat_id, tenant_id, display_id, breed, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, $3, 'Osmanabadi', 'female', 'adult', 'alive', 'Grower', $4::uuid, $5::uuid, $6::uuid, $5::uuid)`,
		uuid.NewString(), f.tenant, fmt.Sprintf("G-%06d", nextDisplay()), f.party, f.castro, f.park); err != nil {
		t.Fatalf("seed second breed: %v", err)
	}

	rations, err := repo.ListPenCohortRation(ctx, f.tenant, []string{f.park}, []string{f.castroPen}, asOf)
	if err != nil {
		t.Fatalf("ListPenCohortRation: %v", err)
	}
	ration, _ := rationByLocation(rations, f.castroPen)
	if ration.Breed != "" {
		t.Fatalf("mixed pen was attributed to breed %q", ration.Breed)
	}
	if ration.Plan.CohortResolved {
		t.Fatal("a mixed-breed adult pen must not be treated as priceable")
	}
	status, grams, _ := domain.ResolveFeedPlan(ration.Plan)
	if status != domain.FeedPlanUnknownCohort || grams != nil {
		t.Fatalf("status=%q grams=%v, want unknown_cohort with no ration", status, grams)
	}
}

// A missing rate row is "not configured", never zero. The pen must come back
// PARTIAL — and therefore produce no feed-per-kg-gain — rather than silently
// reporting a smaller ration that would rank it as the farm's most efficient pen.
func TestMissingRateRowMakesTheRationPartialRatherThanZero(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	f := seedPenFixture(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)

	if _, err := pool.Exec(ctx,
		`DELETE FROM feed_ration_rates WHERE tenant_id = $1::uuid AND feed_item_label = 'Hay'`, f.tenant); err != nil {
		t.Fatalf("remove rate: %v", err)
	}

	rations, err := repo.ListPenCohortRation(ctx, f.tenant, []string{f.park}, []string{f.castroPen}, asOf)
	if err != nil {
		t.Fatalf("ListPenCohortRation: %v", err)
	}
	ration, _ := rationByLocation(rations, f.castroPen)
	if ration.Plan.ItemsBlocked != 1 || ration.Plan.ItemsConfigured != 1 {
		t.Fatalf("configured=%d blocked=%d, want 1/1", ration.Plan.ItemsConfigured, ration.Plan.ItemsBlocked)
	}
	status, grams, _ := domain.ResolveFeedPlan(ration.Plan)
	if status != domain.FeedPlanPartial {
		t.Fatalf("status = %q, want partial", status)
	}
	if grams == nil || *grams != 600 {
		t.Fatalf("grams = %v, want the 600 that IS authored, reported as partial", grams)
	}

	pens := []domain.Pen{{
		Breed: strPtr("Sirohi"), Stage: strPtr("Grower"),
		ADGGPerDay: floatPtr(300), ADGBasis: domain.ADGBasisPerAnimalMedian,
		FeedPlanStatus: status, PlannedFeedGPerHeadDay: grams,
	}}
	domain.Benchmark(pens)
	if pens[0].FeedPerKgGainKg != nil {
		t.Fatalf("a partial ration produced a conversion ratio of %v", *pens[0].FeedPerKgGainKg)
	}
}

// The per-head ration is grams_per_head x the pen's own shed factor, and the
// energy rollup follows the same weighting. A factor silently ignored would
// under- or over-state every pen the farm has tuned.
func TestShedFactorMultipliesTheAuthoredRationAndItsEnergy(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	f := seedPenFixture(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)

	if _, err := pool.Exec(ctx, `
INSERT INTO feed_shed_factors (tenant_id, park_id, shed_id, feed_item_label, multiplier, valid_from)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'Maize', 1.5, '2026-07-01')`,
		f.tenant, f.park, f.castroPen); err != nil {
		t.Fatalf("seed factor: %v", err)
	}

	rations, err := repo.ListPenCohortRation(ctx, f.tenant, []string{f.park}, []string{f.castroPen, f.mandPen}, asOf)
	if err != nil {
		t.Fatalf("ListPenCohortRation: %v", err)
	}
	castro, _ := rationByLocation(rations, f.castroPen)
	mandela, _ := rationByLocation(rations, f.mandPen)

	// Castro: 600 x 1.5 Maize + 400 Hay. Mandela has no factor and stays at 1000.
	if got := castro.Plan.PlannedGramsPerHeadDay; got != 1300 {
		t.Fatalf("Castro grams = %v, want 1300 (600x1.5 + 400)", got)
	}
	if got := mandela.Plan.PlannedGramsPerHeadDay; got != 1000 {
		t.Fatalf("Mandela grams = %v, want 1000 — a factor leaked across pens", got)
	}
	// Energy follows the same weighting: 0.9 kg Maize x 3200 + 0.4 kg Hay x 1800.
	if got := fmt.Sprintf("%.0f", castro.Plan.EnergyKcalPerHeadDay); got != "3600" {
		t.Fatalf("Castro energy = %s kcal, want 3600", got)
	}
	if !castro.Plan.EnergyComplete {
		t.Fatal("both items carry an authored energy value; the rollup should be complete")
	}
}

func strPtr(s string) *string     { return &s }
func floatPtr(f float64) *float64 { return &f }
