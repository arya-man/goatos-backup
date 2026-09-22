package postgres

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// The four adversarial grain tests the Health problems aggregation owes: fan-out, the page
// boundary of its one capped arm, scope, and the status matrix. Each models a shape that is
// invisible on tidy fixture data and wrong on the farm's.
//
// The property under all four is the same one the section rests on: the three breakdowns cut
// ONE set of cases, so each must add back up to the headline, in every one of these shapes.

func problemBucket(t *testing.T, buckets []domain.HealthAnalyticsProblemBucket, key string) int64 {
	t.Helper()
	for _, bucket := range buckets {
		if bucket.Key == key {
			return bucket.Cases
		}
	}
	t.Fatalf("bucket %q is absent; the spine should carry it even at zero", key)
	return 0
}

func assertProblemsAddUp(t *testing.T, problems domain.HealthAnalyticsProblems) {
	t.Helper()
	for name, buckets := range map[string][]domain.HealthAnalyticsProblemBucket{
		"by_breed":    problems.ByBreed,
		"by_pen_type": problems.ByPenType,
		"by_age":      problems.ByAge,
	} {
		var sum int64
		for _, bucket := range buckets {
			sum += bucket.Cases
		}
		if sum != problems.Total {
			t.Fatalf("%s sums to %d, headline says %d", name, sum, problems.Total)
		}
	}
}

func setPenType(t *testing.T, ctx context.Context, pool *pgxpool.Pool, shedID, penType string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, source, shed_type)
VALUES ($2::uuid, $1::uuid, 'Part 1', '1', 'active', 'manual', $3)
ON CONFLICT (tenant_id, shed_id, normalized_label) DO UPDATE
SET shed_type = EXCLUDED.shed_type, status = 'active'`, shedID, healthTenant, penType); err != nil {
		t.Fatalf("set pen type: %v", err)
	}
}

// FAN-OUT. The breakdown joins goats for the breed and the date of birth, and shed_profiles for
// the pen type. Either could multiply a case if it were not on a unique key, and the failure is
// silent: the bars grow, the headline does not, and the two stop agreeing.
//
// The case that makes it visible on the farm: ONE animal treated for TWO illnesses inside the
// window. That is genuinely TWO problems -- the grain is the episode -- so both must appear, in
// every arm, and the arms must still add up.
func TestHealthProblemsOneToManyCountsEachEpisodeOnceAcrossEveryBreakdown(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)
	seedAnalyticsAnimals(t, ctx, pool)
	publishCard(t, ctx, pool, feverCard())
	setPenType(t, ctx, pool, healthShed, domain.HealthPenTypeElevated)
	if _, err := pool.Exec(ctx, `UPDATE goats SET breed = 'Beetal' WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`,
		healthTenant, analyticsDeadGoat); err != nil {
		t.Fatalf("set breed: %v", err)
	}

	repo := NewRepository(pool, 30*time.Second)
	diagnoseFever(t, ctx, pool, analyticsDeadGoat, "problems-fanout-1")
	if _, err := pool.Exec(ctx, `
INSERT INTO health_cases (tenant_id, goat_id, health_protocol_version_id, disease_key, disease_name,
                          age_band, start_date, duration_days, status, park_id, shed_id, partition_label,
                          register_rule_id, idempotency_key, request_fingerprint)
SELECT c.tenant_id, c.goat_id, c.health_protocol_version_id, 'mastitis', 'Mastitis',
       c.age_band, c.start_date, c.duration_days, 'active', c.park_id, c.shed_id, c.partition_label,
       'MASTITIS', 'problems-fanout-2', 'problems-fanout-2-fp'
FROM health_cases c
WHERE c.tenant_id = $1::uuid AND c.goat_id = $2::uuid
LIMIT 1`, healthTenant, analyticsDeadGoat); err != nil {
		t.Fatalf("seed the second episode: %v", err)
	}

	from, to := analyticsWindow(t)
	got, err := repo.GetHealthAnalytics(ctx, domain.HealthAnalyticsQuery{TenantID: healthTenant, FromDate: from, ToDate: to})
	if err != nil {
		t.Fatalf("health analytics: %v", err)
	}

	if got.Problems.Total != 2 {
		t.Fatalf("total = %d, want 2: one animal, two episodes, and an episode is the grain", got.Problems.Total)
	}
	// The headline must be the SAME number the KPI strip shows, from its own separate query.
	if got.Problems.Total != got.Totals.NewCases {
		t.Fatalf("problems total %d disagrees with totals.new_cases %d; they count the same rows", got.Problems.Total, got.Totals.NewCases)
	}
	if n := problemBucket(t, got.Problems.ByBreed, "Beetal"); n != 2 {
		t.Fatalf("Beetal = %d, want 2", n)
	}
	if n := problemBucket(t, got.Problems.ByPenType, domain.HealthPenTypeElevated); n != 2 {
		t.Fatalf("elevated = %d, want 2; a second shed_profiles row must never multiply a case", n)
	}
	assertProblemsAddUp(t, got.Problems)
}

// PAGE BOUNDARY. The breed arm is the only capped one. A farm with more breeds than the cap
// must still read the true total above the chart: summing the visible bars for a headline is
// the banned read-time rollup, one card wide.
func TestHealthProblemsBreedPaginationCapNeverMovesTheTotal(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)
	seedAnalyticsAnimals(t, ctx, pool)
	publishCard(t, ctx, pool, feverCard())
	setPenType(t, ctx, pool, healthShed, domain.HealthPenTypeNonElevated)

	repo := NewRepository(pool, 30*time.Second)
	diagnoseFever(t, ctx, pool, analyticsDeadGoat, "problems-cap-seed")

	// One extra animal per breed, past the cap, each with its own case.
	breeds := domain.HealthAnalyticsBreedLimit + 5
	for i := 0; i < breeds; i++ {
		// The index is bound as TEXT: every use of it below is string concatenation, and an
		// int arg has no text encode plan.
		suffix := strconv.Itoa(i)
		if _, err := pool.Exec(ctx, `
WITH new_goat AS (
  INSERT INTO goats (tenant_id, display_id, species, breed, sex, lifecycle_status, age_band,
                     custodian_party_id, park_id, shed_id, current_location_id, origin_type, dob, entry_date)
  VALUES ($1::uuid, 'G-72' || lpad($2::text, 4, '0'), 'goat', 'Breed ' || lpad($2::text, 2, '0'), 'female',
          'alive', 'adult', $3::uuid, $4::uuid, $5::uuid, $5::uuid, 'procured', DATE '2024-01-01', DATE '2024-01-01')
  RETURNING goat_id, tenant_id
)
INSERT INTO health_cases (tenant_id, goat_id, health_protocol_version_id, disease_key, disease_name,
                          age_band, start_date, duration_days, status, park_id, shed_id, partition_label,
                          register_rule_id, idempotency_key, request_fingerprint)
SELECT n.tenant_id, n.goat_id, c.health_protocol_version_id, 'fever', 'Fever', 'adult', c.start_date,
       c.duration_days, 'active', c.park_id, c.shed_id, c.partition_label, 'FEVER',
       'problems-cap-' || $2::text, 'problems-cap-fp-' || $2::text
FROM new_goat n, health_cases c
WHERE c.tenant_id = $1::uuid
LIMIT 1`, healthTenant, suffix, healthParty, healthPark, healthShed); err != nil {
			t.Fatalf("seed breed %d: %v", i, err)
		}
	}

	from, to := analyticsWindow(t)
	got, err := repo.GetHealthAnalytics(ctx, domain.HealthAnalyticsQuery{TenantID: healthTenant, FromDate: from, ToDate: to})
	if err != nil {
		t.Fatalf("health analytics: %v", err)
	}

	if len(got.Problems.ByBreed) != domain.HealthAnalyticsBreedLimit {
		t.Fatalf("breed bars = %d, want the cap of %d", len(got.Problems.ByBreed), domain.HealthAnalyticsBreedLimit)
	}
	if got.Problems.Total != got.Totals.NewCases {
		t.Fatalf("the cap moved the headline: problems %d, new cases %d", got.Problems.Total, got.Totals.NewCases)
	}
	// The uncapped arms still carry every case, which is what makes the headline checkable.
	var penSum int64
	for _, bucket := range got.Problems.ByPenType {
		penSum += bucket.Cases
	}
	if penSum != got.Problems.Total {
		t.Fatalf("pen type sums to %d, headline %d; only the breed arm may be capped", penSum, got.Problems.Total)
	}
}

// PARK SCOPE. The park predicate is applied ONCE, inside the scoped CTE, and inherited by all
// three arms. Applied to one arm and not another, the breakdowns would describe different farms
// while sitting on one screen under one headline.
func TestHealthProblemsParkScopeNarrowsEveryBreakdownTogether(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)
	seedAnalyticsAnimals(t, ctx, pool)
	publishCard(t, ctx, pool, feverCard())
	setPenType(t, ctx, pool, healthShed, domain.HealthPenTypeElevated)

	repo := NewRepository(pool, 30*time.Second)
	diagnoseFever(t, ctx, pool, analyticsDeadGoat, "problems-scope-in")

	// A second park with its own pen, its own animal and its own case.
	otherPark := "71000000-0000-4000-8000-0000000009a1"
	otherShed := "71000000-0000-4000-8000-0000000009a2"
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id,tenant_id,location_type,location_code,name,status)
VALUES ($2::uuid,$1::uuid,'park','CBE','CBE','active') ON CONFLICT DO NOTHING`, healthTenant, otherPark); err != nil {
		t.Fatalf("seed other park: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id,tenant_id,location_type,location_code,name,parent_location_id,status)
VALUES ($3::uuid,$1::uuid,'shed','CBE-H1','Other Pen',$2::uuid,'active') ON CONFLICT DO NOTHING`,
		healthTenant, otherPark, otherShed); err != nil {
		t.Fatalf("seed other pen: %v", err)
	}
	setPenType(t, ctx, pool, otherShed, domain.HealthPenTypeNonElevated)
	if _, err := pool.Exec(ctx, `
WITH new_goat AS (
  INSERT INTO goats (tenant_id, display_id, species, breed, sex, lifecycle_status, age_band,
                     custodian_party_id, park_id, shed_id, current_location_id, origin_type, dob, entry_date)
  VALUES ($1::uuid, 'G-730001', 'goat', 'Sirohi', 'female', 'alive', 'adult', $2::uuid, $3::uuid, $4::uuid,
          $4::uuid, 'procured', DATE '2024-01-01', DATE '2024-01-01')
  RETURNING goat_id, tenant_id
)
INSERT INTO health_cases (tenant_id, goat_id, health_protocol_version_id, disease_key, disease_name,
                          age_band, start_date, duration_days, status, park_id, shed_id, partition_label,
                          register_rule_id, idempotency_key, request_fingerprint)
SELECT n.tenant_id, n.goat_id, c.health_protocol_version_id, 'fever', 'Fever', 'adult', c.start_date,
       c.duration_days, 'active', $3::uuid, $4::uuid, 'Part 1', 'FEVER', 'problems-scope-out', 'problems-scope-out-fp'
FROM new_goat n, health_cases c
WHERE c.tenant_id = $1::uuid
LIMIT 1`, healthTenant, healthParty, otherPark, otherShed); err != nil {
		t.Fatalf("seed the other park's case: %v", err)
	}

	from, to := analyticsWindow(t)
	wide, err := repo.GetHealthAnalytics(ctx, domain.HealthAnalyticsQuery{TenantID: healthTenant, FromDate: from, ToDate: to})
	if err != nil {
		t.Fatalf("health analytics, every park: %v", err)
	}
	if wide.Problems.Total != 2 {
		t.Fatalf("every park total = %d, want 2", wide.Problems.Total)
	}
	assertProblemsAddUp(t, wide.Problems)

	scopedPark := healthPark
	narrow, err := repo.GetHealthAnalytics(ctx, domain.HealthAnalyticsQuery{TenantID: healthTenant, ParkID: &scopedPark, FromDate: from, ToDate: to})
	if err != nil {
		t.Fatalf("health analytics, one park: %v", err)
	}
	if narrow.Problems.Total != 1 {
		t.Fatalf("one park total = %d, want 1", narrow.Problems.Total)
	}
	if n := problemBucket(t, narrow.Problems.ByPenType, domain.HealthPenTypeNonElevated); n != 0 {
		t.Fatalf("the other park's non-elevated pen leaked into the scoped read: %d", n)
	}
	assertProblemsAddUp(t, narrow.Problems)
}

// STATUS MATRIX, and the honesty property in one test. A health problem is a case OPENED in the
// window, whatever became of it -- still running, recovered, or closed by the animal's death.
// Narrowing to open cases would make the chart shrink as the farm cured animals, which reads as
// fewer problems and is the opposite of what happened.
//
// The second half is the unknown buckets: an animal with no breed, no date of birth and a pen
// nobody has typed is still a problem the farm had. Every arm must NAME it rather than drop it,
// or the chart quietly answers a smaller question than its own headline.
func TestHealthProblemsStatusMatrixCountsEveryStatusAndNamesTheUnknowns(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)
	seedAnalyticsAnimals(t, ctx, pool)
	publishCard(t, ctx, pool, feverCard())
	// healthShed is left UNTYPED on purpose: that is the unclassified bucket.

	repo := NewRepository(pool, 30*time.Second)

	// No breed and no date of birth on any of the three, so each arm's unknown bucket is exercised.
	if _, err := pool.Exec(ctx, `
UPDATE goats SET breed = NULL, dob = NULL, approx_dob = NULL
WHERE tenant_id = $1::uuid AND goat_id = ANY($2::uuid[])`,
		healthTenant, []string{analyticsDeadGoat, analyticsUndx, analyticsRecovered}); err != nil {
		t.Fatalf("clear breed and dob: %v", err)
	}

	diagnoseFever(t, ctx, pool, analyticsDeadGoat, "problems-status-dead")
	diagnoseFever(t, ctx, pool, analyticsUndx, "problems-status-open")
	diagnoseFever(t, ctx, pool, analyticsRecovered, "problems-status-recovered")
	if _, err := pool.Exec(ctx, `
UPDATE health_cases SET status = 'recovered', closed_at = now()
WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`, healthTenant, analyticsRecovered); err != nil {
		t.Fatalf("recover a case: %v", err)
	}
	if err := repo.CloseForApprovedDeath(ctx, healthTenant, analyticsDeadGoat, domain.DeathCause{}); err != nil {
		t.Fatalf("close for approved death: %v", err)
	}
	exitAsDied(t, ctx, pool, analyticsDeadGoat)

	from, to := analyticsWindow(t)
	got, err := repo.GetHealthAnalytics(ctx, domain.HealthAnalyticsQuery{TenantID: healthTenant, FromDate: from, ToDate: to})
	if err != nil {
		t.Fatalf("health analytics: %v", err)
	}

	if got.Problems.Total != 3 {
		t.Fatalf("total = %d, want 3: open, recovered and dead are all problems the farm had", got.Problems.Total)
	}
	if n := problemBucket(t, got.Problems.ByBreed, domain.HealthProblemBreedUnknown); n != 3 {
		t.Fatalf("unknown breed = %d, want 3 counted rather than dropped", n)
	}
	if n := problemBucket(t, got.Problems.ByPenType, domain.HealthPenTypeUnclassified); n != 3 {
		t.Fatalf("unclassified pen = %d, want 3; an untyped pen is named, never folded into a side", n)
	}
	if n := problemBucket(t, got.Problems.ByAge, "unknown"); n != 3 {
		t.Fatalf("unknown age = %d, want 3 counted rather than dropped", n)
	}
	assertProblemsAddUp(t, got.Problems)
}
