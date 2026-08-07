package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// Adversarial coverage for the shed-weights read. Every test seeds real rows and asserts the
// number the screen renders, because each of these is a way the query can be quietly wrong while
// still returning plausible-looking data.

func shedWeightsWindow() (time.Time, time.Time) {
	// Wide enough that membership is never the thing under test, except where it is.
	return time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
}

func seedShedWeightScan(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tag string, weightKg float64, at time.Time) {
	t.Helper()
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_observations (tenant_id, campaign_id, campaign_shed_id, scanned_identifier, weight_kg, proof_artifact_id, recorded_by, idempotency_key, accepted_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6::uuid, $7::uuid, $8, $9::timestamptz)`,
		repoTenant, repoCampaign, repoAnimalScope, tag, weightKg, repoAnimalProof, repoOperator,
		fmt.Sprintf("shedweights:%s:%d", tag, at.UnixNano()), at)
}

// ONE-TO-MANY FAN-OUT. weighing_observations keeps superseded rows: a reopened bucket re-scans a
// tag that already has a row, and 000073's duplicate collapse leaves losers in place. A bare
// count(*) therefore reports MORE animals than the shed holds, and the average is computed over
// captures rather than animals.
//
// Two tags, one of them scanned three times. The honest answer is 2 animals, averaged over each
// tag's LATEST weight (20 and 30 -> 25.0), never 4 captures averaged to something else.
func TestShedWeightsOneToManyDeduplicatesRepeatScansPerTag(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	day := time.Date(2026, 7, 10, 6, 0, 0, 0, time.UTC)
	seedShedWeightScan(t, ctx, pool, "TAG-A", 11.0, day)
	seedShedWeightScan(t, ctx, pool, "TAG-A", 15.0, day.Add(2*time.Hour))
	seedShedWeightScan(t, ctx, pool, "TAG-A", 20.0, day.Add(4*time.Hour)) // latest wins
	seedShedWeightScan(t, ctx, pool, "TAG-B", 30.0, day)

	from, to := shedWeightsWindow()
	out, err := repo.GetShedWeights(ctx, repoTenant, []string{repoPark}, from, to)
	if err != nil {
		t.Fatalf("GetShedWeights: %v", err)
	}

	var found bool
	for _, row := range out.Rows {
		if row.WeighingCategory != "individual_animal" || row.AnimalsWeighed == 0 {
			continue
		}
		found = true
		if row.AnimalsWeighed != 2 {
			t.Fatalf("animals must count DISTINCT tags, not captures: want 2, got %d", row.AnimalsWeighed)
		}
		if got := fmt.Sprintf("%.1f", row.AverageWeightKg); got != "25.0" {
			t.Fatalf("average must use each tag's latest weight (20+30)/2: want 25.0, got %s", got)
		}
		if got := fmt.Sprintf("%.1f", row.TotalWeightKg); got != "50.0" {
			t.Fatalf("total must sum latest-per-tag: want 50.0, got %s", got)
		}
	}
	if !found {
		t.Fatal("expected an individual_animal row carrying the seeded scans")
	}
	if out.Summary.AnimalsWeighed != 2 {
		t.Fatalf("summary animals must match the row grain: want 2, got %d", out.Summary.AnimalsWeighed)
	}
}

// STATUS MATRIX. weighing_campaign_sheds.status spans pending / in_progress / completed /
// canceled. Canceled is work that was called off, so it must leave sheds_in_scope entirely --
// counting it inflates the "N of M sheds weighed" denominator with sheds nobody intended to
// weigh. Every other status stays in scope whether or not it has weighs yet.
func TestShedWeightsStatusMatrixExcludesOnlyCanceledFromScope(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	from, to := shedWeightsWindow()

	for _, status := range []string{"pending", "in_progress", "completed", "canceled"} {
		execWeighingTestSQL(t, ctx, pool,
			`UPDATE weighing_campaign_sheds SET status = $1 WHERE campaign_shed_id = $2::uuid`,
			status, repoAnimalScope)

		out, err := repo.GetShedWeights(ctx, repoTenant, []string{repoPark}, from, to)
		if err != nil {
			t.Fatalf("GetShedWeights(%s): %v", status, err)
		}
		present := false
		for _, row := range out.Rows {
			if row.BucketStatus == status {
				present = true
			}
		}
		if status == "canceled" && present {
			t.Fatal("canceled buckets must be out of scope entirely")
		}
		if status != "canceled" && !present {
			t.Fatalf("status %q must stay in scope", status)
		}
	}
}

// PAGE BOUNDARY. The summary is a WHOLE-FILTER aggregate: it is computed over every shed in
// scope and must not change with the row cap. This asserts the invariant the contract promises --
// summing the returned rows reproduces the summary exactly -- so a future paging change that
// starts computing the cards from the visible slice fails here.
func TestShedWeightsPaginationSummaryMatchesAllReturnedRows(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	day := time.Date(2026, 7, 12, 6, 0, 0, 0, time.UTC)
	seedShedWeightScan(t, ctx, pool, "PAGE-1", 18.0, day)
	seedShedWeightScan(t, ctx, pool, "PAGE-2", 22.0, day)

	from, to := shedWeightsWindow()
	out, err := repo.GetShedWeights(ctx, repoTenant, []string{repoPark}, from, to)
	if err != nil {
		t.Fatalf("GetShedWeights: %v", err)
	}

	var animals int
	var total float64
	var inScope int
	for _, row := range out.Rows {
		inScope++
		animals += row.AnimalsWeighed
		total += row.TotalWeightKg
	}
	if animals != out.Summary.AnimalsWeighed {
		t.Fatalf("summary animals %d must equal the sum over rows %d", out.Summary.AnimalsWeighed, animals)
	}
	if inScope != out.Summary.ShedsInScope {
		t.Fatalf("summary sheds_in_scope %d must equal the row count %d", out.Summary.ShedsInScope, inScope)
	}
	if fmt.Sprintf("%.1f", total) != fmt.Sprintf("%.1f", out.Summary.TotalWeightKg) {
		t.Fatalf("summary total %.1f must equal the sum over rows %.1f", out.Summary.TotalWeightKg, total)
	}
	if out.Summary.AverageWeightKg == nil {
		t.Fatal("average must be populated when animals were weighed")
	}
	// Weighted mean over ANIMALS, not the mean of per-shed averages.
	want := total / float64(animals)
	if fmt.Sprintf("%.3f", *out.Summary.AverageWeightKg) != fmt.Sprintf("%.3f", want) {
		t.Fatalf("average must be total/animals (%.3f), got %.3f", want, *out.Summary.AverageWeightKg)
	}
}

// PARK SCOPE. The repository does no authorization of its own -- the service resolves the park
// list -- so it must honour exactly the parks it is handed. An unrelated park id returns nothing
// rather than falling back to the tenant's whole estate.
func TestShedWeightsParkScopeReturnsNothingOutsideTheRequestedParks(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	from, to := shedWeightsWindow()
	other := "00000000-0000-4000-8000-0000000030ff"
	out, err := repo.GetShedWeights(ctx, repoTenant, []string{other}, from, to)
	if err != nil {
		t.Fatalf("GetShedWeights: %v", err)
	}
	if len(out.Rows) != 0 || out.Summary.ShedsInScope != 0 {
		t.Fatalf("a foreign park must return no rows, got %d rows / %d in scope", len(out.Rows), out.Summary.ShedsInScope)
	}
}

// DATE SHIFT. The window is half-open on accepted_at: a weigh on the exclusive boundary day
// belongs to the NEXT period and must not be counted twice. Anchored on fixed dates rather than
// now±N, because a vaccination-style hour offset makes the result depend on the clock.
func TestShedWeightsDateShiftHonoursHalfOpenWindow(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	inside := time.Date(2026, 7, 20, 6, 0, 0, 0, time.UTC)
	boundary := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC) // exclusive end
	seedShedWeightScan(t, ctx, pool, "WINDOW-IN", 19.0, inside)
	seedShedWeightScan(t, ctx, pool, "WINDOW-OUT", 40.0, boundary)

	out, err := repo.GetShedWeights(ctx, repoTenant, []string{repoPark},
		time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), boundary)
	if err != nil {
		t.Fatalf("GetShedWeights: %v", err)
	}
	for _, row := range out.Rows {
		if row.WeighingCategory != "individual_animal" || row.AnimalsWeighed == 0 {
			continue
		}
		if row.AnimalsWeighed != 1 {
			t.Fatalf("only the weigh inside the half-open window counts: want 1 animal, got %d", row.AnimalsWeighed)
		}
		if got := fmt.Sprintf("%.1f", row.AverageWeightKg); got != "19.0" {
			t.Fatalf("boundary weigh must be excluded: want 19.0, got %s", got)
		}
	}
}
