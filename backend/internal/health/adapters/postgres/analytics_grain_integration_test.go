package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// The three adversarial grain tests every aggregation in this repo owes: fan-out,
// page boundary, and the status matrix. Each one models a shape that is invisible on
// tidy fixture data and wrong on the farm's.

func analyticsWindow(t *testing.T) (string, string) {
	t.Helper()
	today := time.Now().In(biztime.DefaultLocation())
	return today.AddDate(0, 0, -30).Format(domain.HealthAnalyticsDateLayout),
		today.Format(domain.HealthAnalyticsDateLayout)
}

// FAN-OUT. A co-morbid animal really can die with TWO cases open — the farm treats a
// fevered doe that also has mastitis — and the mortality count is over ANIMALS, not
// cases. The attribution test is therefore an EXISTS and never a join: joining
// health_cases to count "died under treatment" would count this animal twice, and the
// two mortality buckets would stop summing to the death total.
//
// The death LIST is the same question one grain over: two open cases must produce ONE
// row naming BOTH diseases, never two rows for one animal.
func TestHealthAnalyticsDeathWithTwoOpenCasesCountsOneToManyOnce(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)
	seedAnalyticsAnimals(t, ctx, pool)
	publishCard(t, ctx, pool, feverCard())

	repo := NewRepository(pool, 30*time.Second)

	// One animal, two courses, both closed by the death.
	diagnoseFever(t, ctx, pool, analyticsDeadGoat, "obs-comorbid-1")
	if _, err := pool.Exec(ctx, `
INSERT INTO health_cases (tenant_id, goat_id, health_protocol_version_id, disease_key, disease_name,
                          age_band, start_date, duration_days, status, park_id, shed_id,
                          register_rule_id, idempotency_key, request_fingerprint)
SELECT c.tenant_id, c.goat_id, c.health_protocol_version_id, 'mastitis', 'Mastitis',
       c.age_band, c.start_date, c.duration_days, 'active', c.park_id, c.shed_id,
       'MASTITIS', 'comorbid-second-case', 'comorbid-fp'
FROM health_cases c
WHERE c.tenant_id = $1::uuid AND c.goat_id = $2::uuid
LIMIT 1`, healthTenant, analyticsDeadGoat); err != nil {
		t.Fatalf("seed the second concurrent case: %v", err)
	}
	if err := repo.CloseForApprovedDeath(ctx, healthTenant, analyticsDeadGoat); err != nil {
		t.Fatalf("close for approved death: %v", err)
	}
	exitAsDied(t, ctx, pool, analyticsDeadGoat)

	from, to := analyticsWindow(t)
	got, err := repo.GetHealthAnalytics(ctx, domain.HealthAnalyticsQuery{TenantID: healthTenant, FromDate: from, ToDate: to})
	if err != nil {
		t.Fatalf("read analytics: %v", err)
	}

	if got.Totals.Deaths != 1 {
		t.Fatalf("deaths = %d, want 1 -- one animal died, however many cases it had open", got.Totals.Deaths)
	}
	if got.Totals.DeathsAttributed != 1 {
		t.Errorf("attributed = %d, want 1 -- two open cases must not count the animal twice", got.Totals.DeathsAttributed)
	}
	if got.Totals.DeathsAttributed+got.Totals.DeathsUnattributed != got.Totals.Deaths {
		t.Errorf("buckets %d + %d != deaths %d",
			got.Totals.DeathsAttributed, got.Totals.DeathsUnattributed, got.Totals.Deaths)
	}
	for _, month := range got.Months {
		if month.DeathsAttributed > month.Deaths {
			t.Errorf("%s: attributed %d exceeds deaths %d -- the fan-out reached the month series",
				month.Month, month.DeathsAttributed, month.Deaths)
		}
	}

	// One ROW for one animal, naming both diseases rather than picking one.
	if len(got.Deaths) != 1 {
		t.Fatalf("death rows = %d, want 1 row for 1 animal: %+v", len(got.Deaths), got.Deaths)
	}
	label := got.Deaths[0].DiseaseLabel
	if label == "" {
		t.Fatal("a death under two open courses names no disease")
	}
	// Both, in one cell. Naming one of two true diseases would be a fabricated pick
	// that silently flips as cases change.
	if label != "Fever · Mastitis" && label != "Mastitis · Fever" {
		t.Errorf("disease label = %q, want both diseases named", label)
	}
}

// PAGE BOUNDARY. The death list is capped; the counts above it are not. A reader must
// never be able to change a headline figure by scrolling, so the cap has to bite the
// LIST and nothing else.
func TestHealthAnalyticsDeathListPaginationCapNeverMovesTheTotals(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)

	// More deaths than the list can hold, none of them ever diagnosed.
	const deaths = domain.HealthAnalyticsDeathListLimit + 7
	for i := 0; i < deaths; i++ {
		if _, err := pool.Exec(ctx, `
INSERT INTO goats (goat_id, tenant_id, display_id, species, sex, lifecycle_status, age_band,
                   custodian_party_id, park_id, shed_id, current_location_id, origin_type,
                   dob, entry_date, exit_reason, exited_at)
VALUES (gen_random_uuid(), $1::uuid, 'G-72' || lpad($2::int::text, 4, '0'), 'goat', 'female', 'dead', 'adult',
        $3::uuid, $4::uuid, $5::uuid, $5::uuid, 'procured', DATE '2024-01-01', DATE '2024-01-01',
        'died', now() - make_interval(hours => $2::int))`,
			healthTenant, i, healthParty, healthPark, healthShed); err != nil {
			t.Fatalf("seed death %d: %v", i, err)
		}
	}

	from, to := analyticsWindow(t)
	got, err := NewRepository(pool, 30*time.Second).GetHealthAnalytics(ctx, domain.HealthAnalyticsQuery{TenantID: healthTenant, FromDate: from, ToDate: to})
	if err != nil {
		t.Fatalf("read analytics: %v", err)
	}

	if got.Totals.Deaths != deaths {
		t.Errorf("deaths total = %d, want %d -- the whole window, not the visible page", got.Totals.Deaths, deaths)
	}
	if got.Totals.DeathsNeverDiagnosed != deaths {
		t.Errorf("never-diagnosed = %d, want %d -- counted by its own query, not off the list",
			got.Totals.DeathsNeverDiagnosed, deaths)
	}
	if len(got.Deaths) != domain.HealthAnalyticsDeathListLimit {
		t.Errorf("death rows = %d, want the cap %d", len(got.Deaths), domain.HealthAnalyticsDeathListLimit)
	}
	var monthly int64
	for _, month := range got.Months {
		monthly += month.Deaths
	}
	if monthly != int64(deaths) {
		t.Errorf("month series sums to %d deaths, want %d -- the cap reached the series", monthly, deaths)
	}
	// Most recent first, so the page the reader sees is the page they expect.
	for i := 1; i < len(got.Deaths); i++ {
		if got.Deaths[i-1].BusinessDate < got.Deaths[i].BusinessDate {
			t.Fatalf("death list is not newest-first at row %d", i)
		}
	}
}

// STATUS MATRIX. The adherence buckets must PARTITION every session status that
// survives the exclusions: no status in two buckets, none in none. A status added to
// the schema and forgotten here would silently vanish from a chart whose four bars are
// presented as the whole of the work.
func TestHealthAnalyticsAdherenceStatusBucketsAreDisjointAndComplete(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)
	publishCard(t, ctx, pool, feverCard())

	opened := diagnoseFever(t, ctx, pool, healthGoat, "obs-status-matrix")

	// Every status the CHECK constraint allows, one session each, all due today.
	statuses := []string{
		"scheduled", "due", "in_progress", "completed", "rework",
		"held_death_review", "canceled_death", "canceled",
	}
	if _, err := pool.Exec(ctx, `DELETE FROM health_treatment_sessions WHERE tenant_id=$1::uuid`, healthTenant); err != nil {
		t.Fatalf("clear generated sessions: %v", err)
	}
	for i, status := range statuses {
		completedAt := "NULL"
		if status == "completed" {
			completedAt = "(now() - interval '2 hours')"
		}
		if _, err := pool.Exec(ctx, `
INSERT INTO health_treatment_sessions (tenant_id, health_case_id, goat_id, day_no, business_date,
                                       session, due_at, status, completed_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, (now() AT TIME ZONE 'Asia/Kolkata')::date, 'morning',
        now(), $5, `+completedAt+`)`,
			healthTenant, opened, healthGoat, i+1, status); err != nil {
			t.Fatalf("seed session %q: %v", status, err)
		}
	}

	from, to := analyticsWindow(t)
	got, err := NewRepository(pool, 30*time.Second).GetHealthAnalytics(ctx, domain.HealthAnalyticsQuery{TenantID: healthTenant, FromDate: from, ToDate: to})
	if err != nil {
		t.Fatalf("read analytics: %v", err)
	}

	a := got.Adherence
	// Three statuses are STOPPED WORK and are excluded from the denominator too:
	// cancelled by a clinical closure, cancelled by the animal's death, and held by
	// the death review. Work the farm was told to stop is not work it failed to do.
	const stopped = 3
	if a.SessionsDue != int64(len(statuses)-stopped) {
		t.Fatalf("sessions due = %d, want %d (every status except the %d stopped ones)",
			a.SessionsDue, len(statuses)-stopped, stopped)
	}
	if sum := a.OnTime + a.Late + a.Rework + a.NotDone; sum != a.SessionsDue {
		t.Errorf("buckets sum to %d, sessions due = %d -- a status fell into none or two", sum, a.SessionsDue)
	}
	if a.OnTime != 1 {
		t.Errorf("on time = %d, want the single completed session", a.OnTime)
	}
	if a.Rework != 1 {
		t.Errorf("rework = %d, want 1", a.Rework)
	}
	// scheduled + due + in_progress.
	if a.NotDone != 3 {
		t.Errorf("not done = %d, want 3", a.NotDone)
	}
	// Verification is a SEPARATE axis and must never be added to the four.
	if a.AwaitingVerification > a.OnTime+a.Late {
		t.Errorf("awaiting verification %d exceeds the completed sessions it is a subset of", a.AwaitingVerification)
	}
}
