package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	domain "github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// These five tests are the adversarial proof for the REWRITTEN cohort aggregate.
//
// commandBoardCohortSQL now folds TWICE: `narrowed` aggregates obligations to
// (target_id, scope_id, dose_code) on the cheap join, and cell_totals/cell_animals fold that to
// (park, stage, sex, dose). The rewrite is only correct because the first key is strictly FINER
// than the second, so no narrowed group straddles two cells. Each test below attacks one dimension
// of that claim on real rows rather than by reading the SQL.

func cohortMatrixFor(t *testing.T, ctx context.Context, repo *Repository, f commandBoardPlanFixture, parkID *string) domain.CommandBoardCohortMatrixPage {
	t.Helper()
	page, err := repo.CommandBoardCohortMatrix(ctx, domain.CommandBoardDrilldownQuery{
		TenantID: f.tenantID,
		AsOf:     f.asOf,
		ParkID:   parkID,
	})
	if err != nil {
		t.Fatalf("CommandBoardCohortMatrix() error = %v", err)
	}
	return page
}

// TestCohortAggregateOneToManyDoesNotMultiplyAnimalCount is the cardinality attack.
//
// The fixture gives every animal FOUR obligations. animal_count is a DISTINCT-goat figure and must
// stay at the herd size; the three bucket counts are obligation-grain and must sum to the
// obligations. A join that fanned out, or a SUM applied to the wrong grain, shows up here as
// animal_count multiplying by four.
func TestCohortAggregateOneToManyDoesNotMultiplyAnimalCount(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	f := seedCommandBoardPlanFixture(t, ctx, pool)
	repo := NewRepository(pool, 30*time.Second)

	page := cohortMatrixFor(t, ctx, repo, f, nil)
	if len(page.Cells) == 0 {
		t.Fatal("cohort matrix returned no cells; the fixture can no longer exercise this path")
	}

	// Cohort.AnimalCount is the TRUE herd head count for (park, stage, sex) and is repeated on every
	// dose cell of that cohort, so it is compared per cohort -- never summed across cells.
	for _, cell := range page.Cells {
		var live int
		if err := pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM goats g
			LEFT JOIN locations shed ON g.shed_id = shed.location_id AND g.tenant_id = shed.tenant_id
			LEFT JOIN locations park ON shed.parent_location_id = park.location_id AND shed.tenant_id = park.tenant_id
			WHERE g.tenant_id = $1::uuid
			  AND COALESCE(park.location_id::text, '') = $2
			  AND COALESCE(g.management_stage, '') = $3
			  AND g.sex = $4
			  AND g.lifecycle_status IN ('alive','sick','under_treatment','quarantine','icu')
			  AND g.merged_into_goat_id IS NULL`,
			f.tenantID, cell.Cohort.ParkID, cell.Cohort.ManagementStage, cell.Cohort.Sex).Scan(&live); err != nil {
			t.Fatalf("count live cohort animals: %v", err)
		}
		if cell.Cohort.AnimalCount != live {
			t.Fatalf("cohort (%s/%s/%s) head count = %d, want %d; every animal holds FOUR obligations, "+
				"so a one-to-many join fanning out shows up here as a multiplied head count",
				cell.Cohort.ParkID, cell.Cohort.ManagementStage, cell.Cohort.Sex, cell.Cohort.AnimalCount, live)
		}
	}

	// The three buckets are deliberately NOT exhaustive over obligations: a CLOSED-WITHOUT-DOSE
	// obligation (canceled, never completed) belongs to none of them and is reported by its own KPI.
	// The fixture puts every fourth animal in exactly that state, so the expected bucket total is
	// obligations MINUS the residual -- asserting the full 4-per-animal here would be asserting a
	// contract the board does not have.
	buckets := 0
	for _, cell := range page.Cells {
		buckets += cell.PendingCount + cell.SubmittedCount + cell.VerifiedCount
	}
	bucketed := commandBoardBucketedObligations(t, ctx, pool, f)
	if buckets != bucketed {
		t.Fatalf("bucket counts total %d, want %d obligations matching the three bucket predicates: "+
			"the obligation-grain counts did not survive the two-level fold", buckets, bucketed)
	}
	if bucketed >= commandBoardPlanFixtureAnimals*4 {
		t.Fatalf("the fixture no longer holds any closed-without-dose obligations (%d of %d bucketed), "+
			"so this test is no longer proving that the residual stays OUT of the three buckets",
			bucketed, commandBoardPlanFixtureAnimals*4)
	}
}

// commandBoardBucketedObligations counts the obligations that match the union of the cohort
// matrix's three bucket predicates, spelled exactly as commandBoardCohortSQL spells them. It is the
// independent second opinion the aggregate is compared against.
func commandBoardBucketedObligations(t *testing.T, ctx context.Context, pool *pgxpool.Pool, f commandBoardPlanFixture) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `
		WITH comp AS (
		  SELECT obligation_id,
		    bool_or(status = 'recorded' AND verified_at IS NULL) AS has_recorded_unverified,
		    bool_or(status = 'accepted') AS has_accepted
		  FROM vaccination_completions WHERE tenant_id = $1::uuid GROUP BY obligation_id
		)
		SELECT COUNT(*)
		FROM obligation_instances oi
		JOIN goats g ON g.goat_id = oi.target_id AND g.tenant_id = oi.tenant_id
		LEFT JOIN comp ON comp.obligation_id = oi.obligation_id
		WHERE oi.tenant_id = $1::uuid
		  AND g.lifecycle_status IN ('alive','sick','under_treatment','quarantine','icu')
		  AND g.merged_into_goat_id IS NULL
		  AND (
		    COALESCE(comp.has_accepted, false)
		    OR (NOT COALESCE(comp.has_accepted, false) AND COALESCE(comp.has_recorded_unverified, false))
		    OR (NOT COALESCE(comp.has_accepted, false)
		        AND NOT COALESCE(comp.has_recorded_unverified, false)
		        AND oi.status IN ('scheduled','due','in_progress','deferred','missed')
		        AND (oi.due_at AT TIME ZONE 'Asia/Kolkata')::date <= ($2::timestamptz AT TIME ZONE 'Asia/Kolkata')::date)
		  )`, f.tenantID, f.asOf).Scan(&n); err != nil {
		t.Fatalf("count bucketed obligations: %v", err)
	}
	return n
}

// TestCohortAggregateStatusBucketsAreDisjointAndComplete is the status attack. Every obligation
// belongs to exactly one bucket; a predicate that overlaps double-counts, one that has a gap loses
// work the CEO is accountable for.
func TestCohortAggregateStatusBucketsAreDisjointAndComplete(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	f := seedCommandBoardPlanFixture(t, ctx, pool)
	repo := NewRepository(pool, 30*time.Second)

	page := cohortMatrixFor(t, ctx, repo, f, nil)
	var pending, submitted, verified int
	for _, cell := range page.Cells {
		pending += cell.PendingCount
		submitted += cell.SubmittedCount
		verified += cell.VerifiedCount
	}
	// Compared against the union of the three predicates, NOT against every obligation: a
	// closed-without-dose obligation is deliberately in none of them. An overlap makes the sum
	// exceed the union; a gap makes it fall short.
	bucketed := commandBoardBucketedObligations(t, ctx, pool, f)
	if pending+submitted+verified != bucketed {
		t.Fatalf("buckets sum to %d but %d obligations match the union of the three bucket "+
			"predicates; the predicates overlap (double count) or leave a gap (lost work)",
			pending+submitted+verified, bucketed)
	}
}

// TestCohortAggregateParkScopeDoesNotLeakAcrossParks is the scope attack. park_id is derived from
// the obligation's scope_id through its shed's parent, inside `narrowed`'s decoration; a scope
// applied on the wrong side of the fold leaks another park's animals into a park-filtered read.
func TestCohortAggregateParkScopeDoesNotLeakAcrossParks(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	f := seedCommandBoardPlanFixture(t, ctx, pool)
	repo := NewRepository(pool, 30*time.Second)

	all := cohortMatrixFor(t, ctx, repo, f, nil)
	scoped := cohortMatrixFor(t, ctx, repo, f, &f.parkID)

	allAnimals, scopedAnimals := 0, 0
	for _, c := range all.Cells {
		allAnimals += c.PendingCount + c.SubmittedCount + c.VerifiedCount
	}
	for _, c := range scoped.Cells {
		scopedAnimals += c.PendingCount + c.SubmittedCount + c.VerifiedCount
		if c.Cohort.ParkID != "" && c.Cohort.ParkID != f.parkID {
			t.Fatalf("park-scoped read returned a cell for park %q, want only %q", c.Cohort.ParkID, f.parkID)
		}
	}
	if scopedAnimals > allAnimals {
		t.Fatalf("park-scoped read counts %d animals, more than the whole tenant's %d", scopedAnimals, allAnimals)
	}
	if scopedAnimals == 0 {
		t.Fatal("park-scoped read returned nothing; the scope is being applied to the wrong side of the fold")
	}
}

// TestCohortAggregateScheduledDateBoundaryUsesISTBusinessDay is the date attack. is_pending compares
// an IST BUSINESS DATE on both sides. A dose due today at 00:00 IST must not read pending-overdue
// merely because as_of is later the same day, and the two-level fold must not shift that comparison.
func TestCohortAggregateScheduledDateBoundaryUsesISTBusinessDay(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	f := seedCommandBoardPlanFixture(t, ctx, pool)
	repo := NewRepository(pool, 30*time.Second)

	early := f
	early.asOf = time.Date(2026, 8, 31, 0, 30, 0, 0, time.UTC) // 06:00 IST
	late := f
	late.asOf = time.Date(2026, 8, 31, 18, 0, 0, 0, time.UTC) // 23:30 IST, same IST day

	sum := func(p domain.CommandBoardCohortMatrixPage) int {
		total := 0
		for _, c := range p.Cells {
			total += c.PendingCount
		}
		return total
	}
	if a, b := sum(cohortMatrixFor(t, ctx, repo, early, nil)), sum(cohortMatrixFor(t, ctx, repo, late, nil)); a != b {
		t.Fatalf("pending count changed within one IST business day (%d at 06:00 IST vs %d at 23:30 IST); "+
			"the due-date comparison is using an instant, not the business day", a, b)
	}
}

// TestCohortAggregatePageBoundaryDrilldownAgreesWithTheTile is the pagination attack. The tile is a
// whole-scope aggregate; the drawer is keyset-paged. Paging must not drop or duplicate an animal at
// a page boundary, which is what makes the two disagree.
func TestCohortAggregatePageBoundaryDrilldownAgreesWithTheTile(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	f := seedCommandBoardPlanFixture(t, ctx, pool)
	repo := NewRepository(pool, 30*time.Second)

	page := cohortMatrixFor(t, ctx, repo, f, nil)
	if len(page.Cells) == 0 {
		t.Fatal("cohort matrix returned no cells; the fixture can no longer exercise this path")
	}
	cell := page.Cells[0]

	// The cohort-day drilldown is not paged; the EXCEPTIONS drawer is, so the page-boundary attack
	// runs against that. It is the drilldown whose tile/drawer parity this branch already had to fix
	// twice, which makes it the right place to prove the keyset is total.
	seen := map[string]struct{}{}
	cursor := ""
	pages := 0
	for i := 0; i < 50; i++ {
		exceptions, err := repo.CommandBoardCohortExceptions(ctx, domain.CommandBoardCohortCellQuery{
			CommandBoardDrilldownQuery: domain.CommandBoardDrilldownQuery{
				TenantID: f.tenantID, AsOf: f.asOf, Limit: 1, Cursor: cursor,
			},
			CohortParkID:    cell.Cohort.ParkID,
			ManagementStage: cell.Cohort.ManagementStage,
			Sex:             cell.Cohort.Sex,
			DoseCodes:       cell.DoseCodes,
		})
		if err != nil {
			t.Fatalf("CommandBoardCohortExceptions() error = %v", err)
		}
		pages++
		for _, a := range exceptions.Animals {
			if _, dup := seen[a.GoatID]; dup {
				t.Fatalf("animal %s returned twice across page boundaries at page %d; the keyset is "+
					"not total, so a drawer double-counts what the tile counted once", a.GoatID, pages)
			}
			seen[a.GoatID] = struct{}{}
		}
		if exceptions.NextCursor == "" {
			break
		}
		cursor = exceptions.NextCursor
	}

	// The tile's own count for this cell must equal what paging the drawer actually yielded. A page
	// boundary that drops a row shows up here and nowhere else.
	if cell.MissingPriorDoseCount != len(seen) {
		t.Fatalf("tile reports %d dose-sequence exceptions for this cell but paging the drawer at "+
			"limit=1 yielded %d distinct animals over %d pages", cell.MissingPriorDoseCount, len(seen), pages)
	}
}
