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

// submitted_at IS STAMPED, and that is what makes a repeat scan of the same tag legal.
// weighing_observations_one_open_tag_uidx is UNIQUE on
// (tenant_id, campaign_shed_id, lower(btrim(scanned_identifier))) WHERE submitted_at IS NULL, so a
// bucket may hold exactly one OPEN row per tag. Superseded captures are the ones 000061 stamped on
// completion — a finished bucket whose tag was re-scanned after a reopen — which is precisely the
// history the dedup in `ind` exists to collapse, and the terminal state of essentially all real
// weighing work. Seeding rows with a NULL submitted_at modelled a state the database forbids.
func seedShedWeightScan(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tag string, weightKg float64, at time.Time) {
	t.Helper()
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_observations (tenant_id, campaign_id, campaign_shed_id, scanned_identifier, weight_kg, proof_artifact_id, recorded_by, idempotency_key, accepted_at, submitted_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6::uuid, $7::uuid, $8, $9::timestamptz, $9::timestamptz)`,
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
	seedShedWeightScan(t, ctx, pool, "TAG-A", 10.0, day.AddDate(0, 0, -7))
	seedShedWeightScan(t, ctx, pool, "TAG-B", 29.0, day.AddDate(0, 0, -7))
	seedShedWeightScan(t, ctx, pool, "TAG-A", 11.0, day)
	seedShedWeightScan(t, ctx, pool, "TAG-A", 15.0, day.Add(2*time.Hour))
	seedShedWeightScan(t, ctx, pool, "TAG-A", 20.0, day.Add(4*time.Hour)) // latest wins
	seedShedWeightScan(t, ctx, pool, "TAG-B", 30.0, day)

	from, to := shedWeightsWindow()
	out, err := repo.GetShedWeights(ctx, repoTenant, []string{repoPark}, "", from, to, "")
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
	if out.Summary.AnimalsWeighed != 2 || out.Summary.IndividualAnimalsWeighed != 2 {
		t.Fatalf("summary animals must match the ADG basis: want 2, got individual=%d total=%d",
			out.Summary.IndividualAnimalsWeighed, out.Summary.AnimalsWeighed)
	}
}

func TestShedWeightsSummaryUsesSameGainBasisAcrossSameShedBuckets(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	const (
		laterCampaign = "00000000-0000-4000-8000-00000000c001"
		laterBucket   = "00000000-0000-4000-8000-00000000c101"
	)
	seedShedWeightsCampaign(t, ctx, pool, laterCampaign, "2026-08-31")
	seedLoadBucket(t, ctx, pool, laterBucket, laterCampaign, repoExpectedShed, "individual_animal")

	aug31 := time.Date(2026, 8, 31, 6, 0, 0, 0, time.UTC)
	sep1 := time.Date(2026, 9, 1, 6, 0, 0, 0, time.UTC)
	aug24 := aug31.AddDate(0, 0, -7)
	seedShedWeightScan(t, ctx, pool, "WINDOW-OLD-A", 19.0, aug24)
	seedShedWeightScan(t, ctx, pool, "WINDOW-OLD-B", 21.0, aug24.Add(time.Hour))
	seedShedWeightScan(t, ctx, pool, "WINDOW-OLD-A", 20.0, aug31)
	seedShedWeightScan(t, ctx, pool, "WINDOW-OLD-B", 22.0, aug31.Add(time.Hour))
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_observations (tenant_id, campaign_id, campaign_shed_id, scanned_identifier, weight_kg, proof_artifact_id, recorded_by, idempotency_key, accepted_at, submitted_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'WINDOW-NEW-A', 29.0, $4::uuid, $5::uuid, 'shedweights:later:window-new-a-prior', $6::timestamptz, $6::timestamptz),
       ($1::uuid, $2::uuid, $3::uuid, 'WINDOW-NEW-A', 30.0, $4::uuid, $5::uuid, 'shedweights:later:window-new-a', $7::timestamptz, $7::timestamptz)`,
		repoTenant, laterCampaign, laterBucket, repoAnimalProof, repoOperator, aug24, sep1)

	out, err := repo.GetShedWeights(ctx, repoTenant, []string{repoPark}, "",
		time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC), "")
	if err != nil {
		t.Fatalf("GetShedWeights: %v", err)
	}

	var snapshotAnimals int
	for _, row := range out.Rows {
		if row.LocationID == repoExpectedShed {
			snapshotAnimals = row.AnimalsWeighed
		}
	}
	if snapshotAnimals != 1 {
		t.Fatalf("row stays latest-shed snapshot: want latest bucket's 1 animal, got %d", snapshotAnimals)
	}
	if out.Summary.IndividualAnimalsWeighed != 3 || out.Summary.AnimalsWeighed != 3 {
		t.Fatalf("summary must count the same individual denominator as ADG, got individual=%d total=%d",
			out.Summary.IndividualAnimalsWeighed, out.Summary.AnimalsWeighed)
	}
	if got := fmt.Sprintf("%.1f", out.Summary.TotalWeightKg); got != "72.0" {
		t.Fatalf("summary total must use latest selected-window weight per tag: want 72.0, got %s", got)
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

		out, err := repo.GetShedWeights(ctx, repoTenant, []string{repoPark}, "", from, to, "")
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
// scope and must not change with the row cap. In the simple one-bucket shape below, the row
// snapshot and the ADG-basis summary happen to line up; a separate test covers the multi-bucket
// shape where they deliberately differ.
func TestShedWeightsPaginationSummaryMatchesAllReturnedRows(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	day := time.Date(2026, 7, 12, 6, 0, 0, 0, time.UTC)
	seedShedWeightScan(t, ctx, pool, "PAGE-1", 17.0, day.AddDate(0, 0, -7))
	seedShedWeightScan(t, ctx, pool, "PAGE-2", 21.0, day.AddDate(0, 0, -7))
	seedShedWeightScan(t, ctx, pool, "PAGE-1", 18.0, day)
	seedShedWeightScan(t, ctx, pool, "PAGE-2", 22.0, day)

	from, to := shedWeightsWindow()
	out, err := repo.GetShedWeights(ctx, repoTenant, []string{repoPark}, "", from, to, "")
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

// PAGE BOUNDARY, REAL LIMIT. The table is capped at MaxShedWeightsRows, but the
// KPI cards are contractually whole-filter aggregates. More sheds than the row
// cap must therefore report a larger sheds_in_scope than len(rows); otherwise the
// screen silently turns a bounded table slice into the business truth.
func TestShedWeightsSummaryCountsShedsBeyondReturnedRowCap(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	execWeighingTestSQL(t, ctx, pool, `
WITH extra AS (
  -- lpad, not format('%012s', ...): Postgres pads %s to width with SPACES, so the node segment
  -- came out as '           1' and the ::uuid cast raised 22P02 before any row was inserted.
  SELECT gs,
         ('00000000-0000-4000-8000-' || lpad(gs::text, 12, '0'))::uuid AS location_id,
         ('00000000-0000-4000-9000-' || lpad(gs::text, 12, '0'))::uuid AS campaign_shed_id
  FROM generate_series(1, 301) gs
),
new_locations AS (
  INSERT INTO locations (location_id, tenant_id, location_type, name, parent_location_id, status)
  SELECT location_id, $1::uuid, 'shed', format('Limit Shed %03s', gs), $2::uuid, 'active'
  FROM extra
  ON CONFLICT (tenant_id, location_id) DO NOTHING
  RETURNING location_id
)
INSERT INTO weighing_campaign_sheds (
  campaign_shed_id, campaign_id, tenant_id, location_id, location_type,
  display_name, weighing_category, operator_user_id, expected_animal_count
)
SELECT campaign_shed_id, $3::uuid, $1::uuid, location_id, 'shed',
       format('Limit Shed %03s', gs), 'individual_animal', $4::uuid, 0
FROM extra
-- weighing_campaign_sheds is unique on campaign_shed_id ALONE (its primary key); there is no
-- (tenant_id, campaign_shed_id) constraint for ON CONFLICT to infer, so naming the pair made this
-- fixture raise 42P10 and the test could never reach its assertion.
ON CONFLICT (campaign_shed_id) DO NOTHING`,
		repoTenant, repoPark, repoCampaign, repoOperator)

	from, to := shedWeightsWindow()
	out, err := repo.GetShedWeights(ctx, repoTenant, []string{repoPark}, "", from, to, "")
	if err != nil {
		t.Fatalf("GetShedWeights: %v", err)
	}
	if len(out.Rows) != 300 {
		t.Fatalf("row list must stay capped at 300, got %d", len(out.Rows))
	}
	if out.Summary.ShedsInScope <= len(out.Rows) {
		t.Fatalf("summary must count sheds beyond the returned row cap: rows=%d summary=%d", len(out.Rows), out.Summary.ShedsInScope)
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
	out, err := repo.GetShedWeights(ctx, repoTenant, []string{other}, "", from, to, "")
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

	out, err := repo.GetShedWeights(ctx, repoTenant, []string{repoPark}, "",
		time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), boundary, "")
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

const (
	shedWeightsCampaignFourWeek  = "00000000-0000-4000-8000-00000000b001"
	shedWeightsScopeFourWeek     = "00000000-0000-4000-8000-00000000b101"
	shedWeightsCampaignShortSpan = "00000000-0000-4000-8000-00000000b002"
	shedWeightsScopeShortSpan    = "00000000-0000-4000-8000-00000000b102"
)

func seedShedWeightsCampaign(t *testing.T, ctx context.Context, pool *pgxpool.Pool, campaignID, start string) {
	t.Helper()
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaigns (campaign_id, tenant_id, park_id, period_start_date, period_end_date, start_business_date, status, planned_cap_per_day, operator_user_id, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::date, $4::date + 6, $4::date, 'published', 100, $5::uuid, $5::uuid)
ON CONFLICT (campaign_id) DO NOTHING`, campaignID, repoTenant, repoPark, start, repoOperator)
}

// SELECTED-WINDOW GAIN. A whole-shed row must use the first and latest weighed
// dates inside the reader's selected date range. Castro-style data has 3 Aug as
// latest, 29 Jul inside the selected range, and 6 Jul outside it; the dashboard
// must render the 29 Jul -> 3 Aug rate.
func TestShedWeightsSelectedWindowGainUsesFirstAndLatestInWindow(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	seedLoadSecondCampaign(t, ctx, pool)
	seedShedWeightsCampaign(t, ctx, pool, shedWeightsCampaignFourWeek, "2026-07-29")
	repo := NewRepository(pool, 5*time.Second)

	// The base fixture already has an open bucket for repoPerShed on 2026-07-29.
	// Mark it completed before seeding another same-day bucket; the real open
	// uniqueness guard keys by operational shed + partition + start date.
	execWeighingTestSQL(t, ctx, pool,
		`UPDATE weighing_campaign_sheds SET status='completed' WHERE campaign_shed_id=$1::uuid`,
		repoShedScope)
	seedLoadBucket(t, ctx, pool, loadShedScopeTwo, loadCampaignTwo, repoPerShed, "per_shed_partition")
	seedLoadLumpWeigh(t, ctx, pool, loadShedScopeTwo, loadCampaignTwo, repoShedProofTwo, 24.53125, 64,
		time.Date(2026, 7, 6, 6, 0, 0, 0, time.UTC))
	seedLoadBucket(t, ctx, pool, shedWeightsScopeFourWeek, shedWeightsCampaignFourWeek, repoPerShed, "per_shed_partition")
	seedLoadLumpWeigh(t, ctx, pool, shedWeightsScopeFourWeek, shedWeightsCampaignFourWeek, repoShedProof, 29.21875, 64,
		time.Date(2026, 7, 29, 6, 0, 0, 0, time.UTC))
	seedLoadLumpWeigh(t, ctx, pool, repoShedScope, repoCampaign, repoShedProof, 30.793650793650794, 63,
		time.Date(2026, 8, 3, 6, 0, 0, 0, time.UTC))

	from := time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC)
	out, err := repo.GetShedWeights(ctx, repoTenant, []string{repoPark}, "", from, to, "")
	if err != nil {
		t.Fatalf("GetShedWeights: %v", err)
	}
	for _, row := range out.Rows {
		if row.LocationID != repoPerShed {
			continue
		}
		if row.ShedAverageGainGPerDay == nil {
			t.Fatal("two in-window weighs must produce a gain")
		}
		if got := fmt.Sprintf("%.1f", *row.ShedAverageGainGPerDay); got != "315.0" {
			t.Fatalf("gain must use 29 Jul -> 3 Aug, not the older 6 Jul baseline: got %s g/day", got)
		}
		if row.GainSpanDays != 5 {
			t.Fatalf("span must be 5 days, got %d", row.GainSpanDays)
		}
		return
	}
	t.Fatal("expected the per-shed row")
}

// PARTITION GRAIN. Two operational rows can share one physical location_id while
// carrying different partition labels. Each row's selected-window gain must stay
// on its own partition; otherwise Part B can quietly inherit Part A's movement.
func TestShedWeightsSelectedWindowGainPartitionOneToManyPageBoundaryParkScopeStatusMatrix(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	seedShedWeightsCampaign(t, ctx, pool, loadCampaignPartA, "2026-07-10")
	seedShedWeightsCampaign(t, ctx, pool, loadCampaignPartB, "2026-07-17")
	repo := NewRepository(pool, 5*time.Second)

	seedLoadBucketPartition(t, ctx, pool, loadPartAOld, loadCampaignPartA, repoPerShed, "Part A", "per_shed_partition")
	seedLoadBucketPartition(t, ctx, pool, loadPartANew, loadCampaignPartB, repoPerShed, "Part A", "per_shed_partition")
	seedLoadBucketPartition(t, ctx, pool, loadPartBOld, loadCampaignPartA, repoPerShed, "Part B", "per_shed_partition")
	seedLoadBucketPartition(t, ctx, pool, loadPartBNew, loadCampaignPartB, repoPerShed, "Part B", "per_shed_partition")
	seedLoadLumpWeigh(t, ctx, pool, loadPartAOld, loadCampaignPartA, repoShedProof, 20.0, 10,
		time.Date(2026, 7, 10, 6, 0, 0, 0, time.UTC))
	seedLoadLumpWeigh(t, ctx, pool, loadPartANew, loadCampaignPartB, repoShedProofTwo, 27.0, 10,
		time.Date(2026, 7, 17, 6, 0, 0, 0, time.UTC))
	seedLoadLumpWeigh(t, ctx, pool, loadPartBOld, loadCampaignPartA, repoShedProofThree, 30.0, 10,
		time.Date(2026, 7, 10, 6, 0, 0, 0, time.UTC))
	seedLoadLumpWeigh(t, ctx, pool, loadPartBNew, loadCampaignPartB, repoShedProofFour, 31.0, 10,
		time.Date(2026, 7, 17, 6, 0, 0, 0, time.UTC))

	out, err := repo.GetShedWeights(ctx, repoTenant, []string{repoPark}, "",
		time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC), "")
	if err != nil {
		t.Fatalf("GetShedWeights: %v", err)
	}

	got := map[string]string{}
	for _, row := range out.Rows {
		if row.LocationID != repoPerShed || row.PartitionLabel == "" || row.ShedAverageGainGPerDay == nil {
			continue
		}
		got[row.PartitionLabel] = fmt.Sprintf("%.1f", *row.ShedAverageGainGPerDay)
		if row.GainSpanDays != 7 {
			t.Fatalf("%s span must be 7 days, got %d", row.PartitionLabel, row.GainSpanDays)
		}
	}
	if got["Part A"] != "1000.0" {
		t.Fatalf("Part A gain must use Part A rows only: got %q", got["Part A"])
	}
	if got["Part B"] != "142.9" {
		t.Fatalf("Part B gain must use Part B rows only: got %q", got["Part B"])
	}
}

// ONE IN-WINDOW WEIGH. If the selected range only contains one accepted weigh,
// the row has a weight but no selected-window gain.
func TestShedWeightsGainNeedsTwoWeighedDatesInsideSelectedWindow(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	seedShedWeightsCampaign(t, ctx, pool, shedWeightsCampaignShortSpan, "2026-07-29")
	repo := NewRepository(pool, 5*time.Second)

	execWeighingTestSQL(t, ctx, pool,
		`UPDATE weighing_campaign_sheds SET status='completed' WHERE campaign_shed_id=$1::uuid`,
		repoShedScope)
	seedLoadBucket(t, ctx, pool, shedWeightsScopeShortSpan, shedWeightsCampaignShortSpan, repoPerShed, "per_shed_partition")
	seedLoadLumpWeigh(t, ctx, pool, shedWeightsScopeShortSpan, shedWeightsCampaignShortSpan, repoShedProofTwo, 29.21875, 64,
		time.Date(2026, 7, 29, 6, 0, 0, 0, time.UTC))
	seedLoadLumpWeigh(t, ctx, pool, repoShedScope, repoCampaign, repoShedProof, 30.793650793650794, 63,
		time.Date(2026, 8, 3, 6, 0, 0, 0, time.UTC))

	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC)
	out, err := repo.GetShedWeights(ctx, repoTenant, []string{repoPark}, "", from, to, "")
	if err != nil {
		t.Fatalf("GetShedWeights: %v", err)
	}
	for _, row := range out.Rows {
		if row.LocationID != repoPerShed {
			continue
		}
		if row.ShedAverageGainGPerDay != nil {
			t.Fatalf("one in-window weigh must not produce selected-window gain, got %.1f", *row.ShedAverageGainGPerDay)
		}
		return
	}
	t.Fatal("expected the per-shed row")
}

// A DAY WITH ONLY SCANNED KIDS IS STILL A DAY THE FARM WEIGHED.
//
// The defect this pins, reported off real STG data (2026-08-26): the Weights page opens on "the last
// two whole-shed weigh dates" and took BOTH ends of its window from that lump-only list. On 25 Aug
// the farm scanned 199 kids across 17 sheds and weighed no shed whole, so the 25th never entered
// lump_weighing_dates and the window closed on the 24th -- dropping all 199 kids out of the KPIs,
// the gain charts and Fair fight, while the period label read as though nothing were missing.
//
// The START must still come from the lump dates: two whole-shed weighs are what make a shed-average
// movement measurable. Only the END moves, to the last day anything was weighed at all -- which is
// what LatestWeighingDate answers and lump_weighing_dates cannot.
func TestShedWeightsLatestWeighingDateSeesAScanOnlyDay(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	from, to := shedWeightsWindow()

	// Two whole-shed weighs -- the pair the window's START is drawn from.
	//
	// One bucket PER WEIGH DATE, which is how the farm's data really lands:
	// weighing_shed_observations_one_open_scope_uidx admits exactly one live row per bucket, so a
	// shed weighed twice is two buckets, not two rows on one.
	seedShedWeightsCampaign(t, ctx, pool, loadCampaignPartA, "2026-07-10")
	seedShedWeightsCampaign(t, ctx, pool, loadCampaignPartB, "2026-07-17")
	insertProof(t, ctx, pool, repoShedProofTwo, "video", "completed", "shed", repoPerShed, "shed", repoPerShed)
	seedLoadBucketPartition(t, ctx, pool, loadPartAOld, loadCampaignPartA, repoExpectedShed, "", "per_shed_partition")
	seedLoadBucketPartition(t, ctx, pool, loadPartANew, loadCampaignPartB, repoExpectedShed, "", "per_shed_partition")
	seedLoadLumpWeigh(t, ctx, pool, loadPartAOld, loadCampaignPartA, repoShedProof, 20.0, 10,
		time.Date(2026, 7, 10, 6, 0, 0, 0, time.UTC))
	seedLoadLumpWeigh(t, ctx, pool, loadPartANew, loadCampaignPartB, repoShedProofTwo, 27.0, 10,
		time.Date(2026, 7, 17, 6, 0, 0, 0, time.UTC))

	lumpOnly, err := repo.GetShedWeights(ctx, repoTenant, []string{repoPark}, "", from, to, "")
	if err != nil {
		t.Fatalf("GetShedWeights: %v", err)
	}
	if lumpOnly.LatestWeighingDate != "2026-07-17" {
		t.Fatalf("with only whole-shed weighs the latest date is the later of them, got %q", lumpOnly.LatestWeighingDate)
	}

	// Now a SCAN-ONLY day, three days after the last whole-shed weigh. This is the 25 Aug shape.
	seedShedWeightScan(t, ctx, pool, "SCAN-ONLY-DAY-1", 18.0, time.Date(2026, 7, 20, 6, 0, 0, 0, time.UTC))

	after, err := repo.GetShedWeights(ctx, repoTenant, []string{repoPark}, "", from, to, "")
	if err != nil {
		t.Fatalf("GetShedWeights after the scan-only day: %v", err)
	}
	if after.LatestWeighingDate != "2026-07-20" {
		t.Fatalf("a scan-only day is still a day the farm weighed: want 2026-07-20, got %q", after.LatestWeighingDate)
	}
	// And it stays OUT of the lump dates, because the window's START must keep meaning "a day a shed
	// was weighed whole". Folding it in there would move the start onto a day with no shed average.
	for _, day := range after.LumpWeighingDates {
		if day == "2026-07-20" {
			t.Fatalf("a scan-only day must not become a lump-sum weighing date: %v", after.LumpWeighingDates)
		}
	}
}

// THE SEX FILTER REACHES EVERY BLOCK OF THE GROWTH READ, NOT MOST OF THEM.
//
// Review findings, 2026-08-26. One read resolves ONE sex scope and then hands it to a dozen
// helpers, and four of them quietly ignored it -- three by never being passed it, one by taking
// the parameters and never referencing them, which compiles and reads as done:
//
//   - lump_sum.shed_week_trend  accepted sexFiltered/scope and used neither, so a reader on Female
//     saw a female headline above whole-shed pens holding no females.
//   - sale_readiness            was never passed the scope, so "ready to sell" counted the whole
//     herd beside a headline about half of it.
//   - eligibility               likewise: the phone's "70/382" coverage tile ignored the filter.
//   - shed_leaderboard          was HALF filtered -- its daily gain followed the scope while its
//     kid count and median weight did not, so one row carried a male-only
//     gain beside an all-kids count with nothing saying so.
//
// A whole-shed pen is claimed only when its cohort is entirely that sex, so a fixture whose only
// pen is female must vanish from the male read completely -- head count included, which is the
// assertion that catches a filter applied to the rows but not the aggregate.
func TestGrowthReadSexFilterOneToManyPageBoundaryParkScopeStatusBuckets(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	from, to := shedWeightsWindow()

	// Her OWN shed, holding nobody else. A whole-shed pen is claimed only when its cohort is
	// entirely one sex, so putting her on a shed the shared fixture already fills with both sexes
	// would make the pen mixed -- claimed by neither reader -- and the lump assertion below would
	// then pass for the wrong reason.
	const (
		femaleGoat = "00000000-0000-4000-8000-0000000092a1"
		femaleShed = "00000000-0000-4000-8000-0000000092a2"
	)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, name, parent_location_id, status)
VALUES ($1::uuid, $2::uuid, 'shed', 'Sex Filter Demo Shed', $3::uuid, 'active')
ON CONFLICT (tenant_id, location_id) DO NOTHING`, femaleShed, repoTenant, repoPark)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, breed, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, 'G-990931', 'Beetal', 'female', 'kid', 'alive', 'kid', $3::uuid, $4::uuid, $5::uuid, $4::uuid)
ON CONFLICT (goat_id) DO UPDATE SET sex = EXCLUDED.sex, shed_id = EXCLUDED.shed_id`,
		femaleGoat, repoTenant, repoParty, femaleShed, repoPark)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, normalizer_version)
VALUES ($1::uuid, $2::uuid, 'animal_identifier_1', 'FEMALE-TAG-1', 'female-tag-1', 'global', true, 'active', now(), 'test')
ON CONFLICT DO NOTHING`, repoTenant, femaleGoat)

	// Two weighs, two business days apart, so this kid has a real pair to count.
	seedShedWeightScan(t, ctx, pool, "FEMALE-TAG-1", 18.0, time.Date(2026, 7, 10, 6, 0, 0, 0, time.UTC))
	seedShedWeightScan(t, ctx, pool, "FEMALE-TAG-1", 20.0, time.Date(2026, 7, 17, 6, 0, 0, 0, time.UTC))

	// A whole-shed pen whose residents are that same female cohort.
	seedShedWeightsCampaign(t, ctx, pool, loadCampaignPartA, "2026-07-10")
	insertProof(t, ctx, pool, repoShedProofTwo, "video", "completed", "shed", repoPerShed, "shed", repoPerShed)
	seedLoadBucketPartition(t, ctx, pool, loadPartAOld, loadCampaignPartA, femaleShed, "", "per_shed_partition")
	seedLoadLumpWeigh(t, ctx, pool, loadPartAOld, loadCampaignPartA, repoShedProof, 20.0, 10,
		time.Date(2026, 7, 10, 6, 0, 0, 0, time.UTC))

	female, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{repoPark}, from, to, "female")
	if err != nil {
		t.Fatalf("GetLeadershipGrowthADG(female): %v", err)
	}
	male, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{repoPark}, from, to, "male")
	if err != nil {
		t.Fatalf("GetLeadershipGrowthADG(male): %v", err)
	}

	// lump_sum: the pen is a FEMALE cohort, so it is the female reader's and nobody else's.
	if len(female.LumpSum.ShedWeekTrend) == 0 {
		t.Fatal("the female read must keep a female-cohort whole-shed pen in its lump-sum trend")
	}
	if n := len(male.LumpSum.ShedWeekTrend); n != 0 {
		t.Fatalf("a female-cohort pen must not appear in the male lump-sum trend, got %d row(s)", n)
	}
	var maleHeads int
	for _, p := range male.LumpSum.ShedWeekTrend {
		maleHeads += p.HeadCount
	}
	if maleHeads != 0 {
		t.Fatalf("the male lump-sum head count must be 0, got %d", maleHeads)
	}

	// eligibility: the female kid is countable for her, invisible to him.
	if female.Eligibility.TotalAnimalsWeighed == 0 || female.Eligibility.AnimalsWithTwoPlusWeighs == 0 {
		t.Fatalf("the female read must count her own coverage, got %#v", female.Eligibility)
	}
	if male.Eligibility.TotalAnimalsWeighed >= female.Eligibility.TotalAnimalsWeighed &&
		female.Eligibility.TotalAnimalsWeighed > 0 && male.Eligibility.TotalAnimalsWeighed == female.Eligibility.TotalAnimalsWeighed {
		t.Fatalf("eligibility ignored the filter: male=%d female=%d equal",
			male.Eligibility.TotalAnimalsWeighed, female.Eligibility.TotalAnimalsWeighed)
	}

	// sale_readiness: a latest-EVER count, still narrowed to the reader's half of the herd.
	if female.SaleReadiness.AnimalsConsidered == 0 {
		t.Fatal("the female read must consider her own kids for sale readiness")
	}
	if male.SaleReadiness.AnimalsConsidered == female.SaleReadiness.AnimalsConsidered {
		t.Fatalf("sale_readiness ignored the filter: both sexes considered %d animals",
			male.SaleReadiness.AnimalsConsidered)
	}

	// shed_leaderboard: the row's COUNT must follow the same filter its gain already did.
	var femaleLeaderboardN int
	for _, row := range female.ShedLeaderboard {
		femaleLeaderboardN += row.AnimalCount
	}
	var maleLeaderboardN int
	for _, row := range male.ShedLeaderboard {
		maleLeaderboardN += row.AnimalCount
	}
	if femaleLeaderboardN == 0 {
		t.Fatal("the female leaderboard must count her kids")
	}
	if maleLeaderboardN == femaleLeaderboardN {
		t.Fatalf("leaderboard counts ignored the filter: both sexes counted %d kids", maleLeaderboardN)
	}

	// ONE-TO-MANY. She was weighed TWICE. Every one of these blocks counts ANIMALS, not weighs, so
	// two observations of one tag must collapse to one kid -- the fan-out that turns a coverage
	// tile into a lie the moment an operator re-scans.
	if got := female.Eligibility.TotalAnimalsWeighed; got != 1 {
		t.Fatalf("two weighs of one tag are one animal: eligibility counted %d", got)
	}
	if got := female.Eligibility.AnimalsWithTwoPlusWeighs; got != 1 {
		t.Fatalf("one animal has the second weigh, got %d", got)
	}
	if femaleLeaderboardN != 1 {
		t.Fatalf("the leaderboard counts animals, not weighs: got %d", femaleLeaderboardN)
	}

	// PAGE BOUNDARY. LosingAnimalCount is a whole-filter summary and LosingAnimals is the list it
	// drills into; the domain comment says both exist precisely because they answer different
	// questions, and a summary recomputed from a page slice is how "15 losing" came to open a list
	// of 2. They must agree here, where the list is whole.
	if female.Headline.LosingAnimalCount != len(female.LosingAnimals) {
		t.Fatalf("losing summary must match the list it opens: count=%d, list=%d",
			female.Headline.LosingAnimalCount, len(female.LosingAnimals))
	}

	// PARK SCOPE. These kids and this pen hang off repoPark. A caller authorized for a park that
	// owns none of them must get nothing back -- the scope predicate carrying, not the caller's
	// good manners. Every block is checked, because a filter threaded into three of four is exactly
	// the defect this test exists for.
	const otherPark = "00000000-0000-4000-8000-0000000030fe"
	scoped, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{otherPark}, from, to, "female")
	if err != nil {
		t.Fatalf("GetLeadershipGrowthADG(other park): %v", err)
	}
	if len(scoped.LumpSum.ShedWeekTrend) != 0 || len(scoped.ShedLeaderboard) != 0 {
		t.Fatalf("another park's growth read must be empty: %d lump row(s), %d leaderboard row(s)",
			len(scoped.LumpSum.ShedWeekTrend), len(scoped.ShedLeaderboard))
	}
	if scoped.Eligibility.TotalAnimalsWeighed != 0 || scoped.SaleReadiness.AnimalsConsidered != 0 {
		t.Fatalf("another park's coverage must be empty: eligibility=%d sale=%d",
			scoped.Eligibility.TotalAnimalsWeighed, scoped.SaleReadiness.AnimalsConsidered)
	}

	// STATUS BUCKETS. A WITHDRAWN whole-shed weigh is not a measurement. `withdrawn_at IS NULL` is
	// the live rule here -- migration 000058 narrowed verification_status to pending/verified/rework,
	// so the `<> 'rejected'` predicates these queries still carry can no longer exclude anything --
	// and it is why the uniqueness on that table is PARTIAL: a reopened bucket legitimately holds
	// several rows, one of them live.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_shed_observations (
  tenant_id, campaign_id, campaign_shed_id, weight_kg, average_weight_kg, animal_count,
  proof_artifact_id, recorded_by, idempotency_key, accepted_at, verification_status, withdrawn_at
) VALUES ($1::uuid, $2::uuid, $3::uuid, 499950, 999.9, 500,
  $4::uuid, $5::uuid, 'sexfilter-withdrawn', $6::timestamptz, 'rework', $6::timestamptz)`,
		repoTenant, loadCampaignPartA, loadPartAOld, repoShedProofTwo, repoOperator,
		time.Date(2026, 7, 11, 6, 0, 0, 0, time.UTC))
	afterWithdrawn, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{repoPark}, from, to, "female")
	if err != nil {
		t.Fatalf("GetLeadershipGrowthADG after a withdrawn weigh: %v", err)
	}
	var beforeHeads, afterHeads int
	for _, p := range female.LumpSum.ShedWeekTrend {
		beforeHeads += p.HeadCount
	}
	for _, p := range afterWithdrawn.LumpSum.ShedWeekTrend {
		afterHeads += p.HeadCount
	}
	if afterHeads != beforeHeads {
		t.Fatalf("a withdrawn weigh must not enter the lump-sum trend: %d head became %d", beforeHeads, afterHeads)
	}
}

// SALE READINESS IS LATEST-EVER, AND THE SEX FILTER MUST NOT QUIETLY MAKE IT LATEST-RECENT.
//
// The defect this pins (review finding on the sex-filter fix itself, 2026-08-26): sale readiness
// deliberately reads each animal's latest-EVER weigh rather than the selected window, because a kid
// heavy enough to sell but not weighed this fortnight is still heavy enough to sell. Filtering it
// with the ORDINARY sex scope -- which is bounded by the window plus the 90-day gain lookback --
// silently redefined its denominator from "every animal of this sex ever weighed" to "every animal
// of this sex weighed recently", and an animal last weighed a year ago dropped out of the count
// that exists precisely to find her.
//
// THE FIXTURE REACHES OUTSIDE THE LOOKBACK ON PURPOSE. The farm's own data cannot show this defect:
// every weigh in it falls inside the 90 days, so the two tag lists happen to be identical and a
// test built on the shared fixture would pass either way. This kid is weighed 200 days before the
// window, once, at a weight that is unambiguously sale-ready.
func TestSaleReadinessKeepsLatestEverAnimalsUnderTheSexFilter(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	from, to := shedWeightsWindow()

	const oldGoat = "00000000-0000-4000-8000-0000000093a1"
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, breed, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, 'G-990941', 'Beetal', 'female', 'kid', 'alive', 'kid', $3::uuid, $4::uuid, $5::uuid, $4::uuid)
ON CONFLICT (goat_id) DO UPDATE SET sex = EXCLUDED.sex`,
		oldGoat, repoTenant, repoParty, repoExpectedShed, repoPark)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, normalizer_version)
VALUES ($1::uuid, $2::uuid, 'animal_identifier_1', 'LONG-AGO-TAG', 'long-ago-tag', 'global', true, 'active', now(), 'test')
ON CONFLICT DO NOTHING`, repoTenant, oldGoat)

	// 200 days before the window: outside the 90-day lookback the ordinary scope uses, and heavy
	// enough that any honest sale-readiness count must include her.
	seedShedWeightScan(t, ctx, pool, "LONG-AGO-TAG", 41.0, from.AddDate(0, 0, -200))

	female, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{repoPark}, from, to, "female")
	if err != nil {
		t.Fatalf("GetLeadershipGrowthADG(female): %v", err)
	}
	unfiltered, err := repo.GetLeadershipGrowthADG(ctx, repoTenant, []string{repoPark}, from, to, "")
	if err != nil {
		t.Fatalf("GetLeadershipGrowthADG(unfiltered): %v", err)
	}

	if female.SaleReadiness.AnimalsConsidered == 0 {
		t.Fatal("a female weighed only long ago must still be considered for sale readiness")
	}
	if female.SaleReadiness.AtOrAbove35Kg == 0 {
		t.Fatalf("her 41 kg latest-ever weight must count: %#v", female.SaleReadiness)
	}
	// The unfiltered read has always seen her; the filtered one must not see fewer 35 kg females
	// than there are 35 kg animals in total minus the males -- concretely, dropping her would make
	// the filtered count smaller than the unfiltered one by exactly her.
	if female.SaleReadiness.AtOrAbove35Kg > unfiltered.SaleReadiness.AtOrAbove35Kg {
		t.Fatalf("the female count cannot exceed the whole herd's: %d > %d",
			female.SaleReadiness.AtOrAbove35Kg, unfiltered.SaleReadiness.AtOrAbove35Kg)
	}

	// And the windowed reads stay windowed: she has no weigh inside the period, so she must NOT
	// appear in the coverage counters. This is the other half of the rule -- one scope per read,
	// chosen by whether that read spans all time.
	if female.Eligibility.TotalAnimalsWeighed != 0 {
		t.Fatalf("a kid with no weigh in the window must not enter windowed coverage, got %d",
			female.Eligibility.TotalAnimalsWeighed)
	}
}

// THE ALL-HISTORY SCAN IS OPT-IN, AND A CALLER THAT SKIPS IT FAILS LOUDLY.
//
// The defect this pins (review finding, 2026-08-26): AllTimeTags is resolved by an arm with NO date
// bound -- an all-history scan of the tenant's weighs -- and it was computed for EVERY caller of the
// shared resolver. Shed weights and Growth Director never read it, so every sex-filtered page load
// paid for a scan whose result it discarded, while the resolver's own scale note still claimed it
// was bounded by the selected window.
//
// Two halves, and the second is what stops the first from becoming a silent wrong number: the plain
// resolver must NOT resolve the list, and a read that needs it must refuse to run without it rather
// than filter every animal out and report a farm with nothing to sell.
func TestSexScopeAllTimeTagsAreOptIn(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	from, to := shedWeightsWindow()

	const oldGoat = "00000000-0000-4000-8000-0000000094a1"
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, breed, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, 'G-990951', 'Beetal', 'female', 'kid', 'alive', 'kid', $3::uuid, $4::uuid, $5::uuid, $4::uuid)
ON CONFLICT (goat_id) DO UPDATE SET sex = EXCLUDED.sex`,
		oldGoat, repoTenant, repoParty, repoExpectedShed, repoPark)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, normalizer_version)
VALUES ($1::uuid, $2::uuid, 'animal_identifier_1', 'OPTIN-TAG', 'optin-tag', 'global', true, 'active', now(), 'test')
ON CONFLICT DO NOTHING`, repoTenant, oldGoat)
	// Outside the 90-day lookback, so she can ONLY appear via the all-time list.
	seedShedWeightScan(t, ctx, pool, "OPTIN-TAG", 41.0, from.AddDate(0, 0, -200))

	windowOnly, err := repo.resolveSexScope(ctx, repoTenant, []string{repoPark}, "female", from, to)
	if err != nil {
		t.Fatalf("resolveSexScope: %v", err)
	}
	if len(windowOnly.AllTimeTags) != 0 {
		t.Fatalf("the plain resolver must not run the all-history arm, got %d tag(s)", len(windowOnly.AllTimeTags))
	}

	withAllTime, err := repo.resolveSexScopeWithAllTime(ctx, repoTenant, []string{repoPark}, "female", from, to)
	if err != nil {
		t.Fatalf("resolveSexScopeWithAllTime: %v", err)
	}
	var found bool
	for _, tag := range withAllTime.AllTimeTags {
		if tag == "optin-tag" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the opt-in resolver must reach a kid weighed outside the lookback, got %v", withAllTime.AllTimeTags)
	}
	// The WINDOWED list is unchanged by opting in -- one extra arm, not a different scope.
	if len(withAllTime.Tags) != len(windowOnly.Tags) {
		t.Fatalf("opting in must not change the windowed tag list: %d vs %d",
			len(withAllTime.Tags), len(windowOnly.Tags))
	}

	// And the loud failure: sale readiness handed a window-only scope must refuse, not report zero.
	if _, err := repo.growthSaleReadiness(ctx, repoTenant, []string{repoPark}, true, windowOnly); err == nil {
		t.Fatal("sale readiness must refuse a window-only scope rather than silently filtering every animal out")
	}
	// An UNFILTERED read needs no all-time list at all, and must still work.
	if _, err := repo.growthSaleReadiness(ctx, repoTenant, []string{repoPark}, false, SexScope{}); err != nil {
		t.Fatalf("an unfiltered sale-readiness read needs no sex scope: %v", err)
	}
}
