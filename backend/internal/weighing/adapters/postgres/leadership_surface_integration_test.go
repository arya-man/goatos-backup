package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// Tests for the three leadership-surface guarantees:
//
//  1. Bucket AVAILABILITY on one weigh date (planner catalog).
//  2. The Active/Completed task counts are WHOLE-FILTER, not page-derived.
//  3. One OPEN weighing row per (park, weigh date, shed) — duplicate work blocked.
//
// Postgres integration tests are opt-in: GOATOS_RUN_POSTGRES_TESTS=1.

const (
	lsParkCPT    = "00000000-0000-4000-8000-000000003002"
	lsShedCPT    = "6e769414-4dde-43a5-b30f-fb857392b638"
	lsFreeShed   = "654260da-956e-4015-bc95-edf3421cae3c" // repoActualShed, unused by the fixture campaign
	lsFixtureDay = "2026-07-29"                           // the fixture campaign's weigh date
)

// -----------------------------------------------------------------------------
// 1. AVAILABILITY
// -----------------------------------------------------------------------------

// The catalog must report a shed as taken ONLY on the date it is actually claimed,
// must name who claimed it, and must not multiply shed rows while doing so (the
// taken side is pre-aggregated to one row per shed before the join).
func TestPlannerCatalogReportsShedAvailabilityForTheRequestedDateOnly(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	// Bucket availability is a keyset feed per park, so it is asserted over the
	// DRAINED buckets of every park rather than whichever sheds land on page one.
	onDay := drainAllParkBuckets(t, ctx, repo, lsFixtureDay, "")
	taken := lsShed(t, onDay, repoExpectedShed)
	if !taken.Scheduled {
		t.Fatalf("shed %s scheduled=false on %s, want taken by the fixture campaign", repoExpectedShed, lsFixtureDay)
	}
	if taken.ScheduledCampaignID != repoCampaign {
		t.Fatalf("scheduled_campaign_id=%q, want %q", taken.ScheduledCampaignID, repoCampaign)
	}
	if taken.ScheduledOperatorUserID != repoOperator {
		t.Fatalf("scheduled_operator_user_id=%q, want %q", taken.ScheduledOperatorUserID, repoOperator)
	}
	if taken.ScheduledWeighingCategory != domain.CategoryIndividualAnimal {
		t.Fatalf("scheduled_weighing_category=%q, want %q", taken.ScheduledWeighingCategory, domain.CategoryIndividualAnimal)
	}
	if free := lsShed(t, onDay, lsFreeShed); free.Scheduled {
		t.Fatalf("shed %s reads as taken but no task claims it", lsFreeShed)
	}

	// The fixture buckets were inserted WITHOUT park_id/start_business_date. They are
	// only visible to this date-scoped read because the identity columns were derived
	// on write, so this also proves the denormalized copy cannot be forgotten.
	otherDay := drainAllParkBuckets(t, ctx, repo, "2026-07-30", "")
	if shed := lsShed(t, otherDay, repoExpectedShed); shed.Scheduled {
		t.Fatalf("shed %s reads as taken on 2026-07-30; availability must be scoped to the requested weigh date", repoExpectedShed)
	}

	// Editing the task that owns the bucket must not see its own bucket as taken,
	// or an edit could never re-save the sheds it already owns.
	editing := drainAllParkBuckets(t, ctx, repo, lsFixtureDay, repoCampaign)
	if shed := lsShed(t, editing, repoExpectedShed); shed.Scheduled {
		t.Fatalf("shed %s reads as taken while editing the very task that owns it", repoExpectedShed)
	}
}

// THE PARK-PICKER REGRESSION. The catalog and the bucket page were once ONE
// flattened keyset page over (park, shed) pairs with a ~20-row limit. A real park
// holds 76+ sheds, so page one was entirely that park and the wizard's "Select
// park" step offered a SINGLE park -- the others were unreachable without paging
// through dozens of shed rows.
//
// The park read must therefore return EVERY park no matter how many sheds the
// biggest park holds, and it must report each park's shed count at PARK grain.
func TestPlannerCatalogReturnsEveryParkRegardlessOfTheBiggestParksShedCount(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	// Give CPT far more sheds than any page could hold -- the real Channapatna
	// shape. CBE and a third park must still be offered.
	const bulkSheds = 76
	for i := 0; i < bulkSheds; i++ {
		lsInsertShed(t, ctx, pool, lcpUUID(32000+i), lsParkCPT, fmt.Sprintf("Bulk %03d", i), 500+i)
	}
	third := lcpUUID(33001)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, name, status, display_order)
VALUES ($1::uuid, $2::uuid, 'park', 'KLR', 'active', 90)
ON CONFLICT (location_id) DO NOTHING`, third, repoTenant)
	lsInsertShed(t, ctx, pool, lcpUUID(33002), third, "KLR Shed 1", 1)
	// One active field operator, so the picker the wizard holds has something in it.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO workforce_members (tenant_id, user_id, display_code, display_name, status, primary_role_hint)
VALUES ($1::uuid, $2::uuid, 'OP-1', 'Planner Operator', 'active', 'operator')
ON CONFLICT DO NOTHING`, repoTenant, repoOperator)

	catalog, err := repo.PlannerCatalog(ctx, repoTenant, lsFixtureDay)
	if err != nil {
		t.Fatalf("planner catalog: %v", err)
	}
	byID := map[string]domain.PlannerPark{}
	for _, park := range catalog.Parks {
		byID[park.ParkID] = park
	}
	for _, want := range []string{repoPark, lsParkCPT, third} {
		if _, ok := byID[want]; !ok {
			t.Fatalf("park %s missing from the planner catalog (%d parks returned); the park picker must offer every park", want, len(catalog.Parks))
		}
	}
	// PARK-grain shed count: the whole park's active sheds, not a page-local sum.
	// A page-derived count could never exceed the ~20-row bucket page size.
	cpt := byID[lsParkCPT]
	if cpt.ShedCount < bulkSheds {
		t.Fatalf("park %s shed_count=%d, want at least the %d sheds it holds -- the count must be park-grain, not page-local", lsParkCPT, cpt.ShedCount, bulkSheds)
	}
	var wantShedCount int
	if err := pool.QueryRow(ctx, `
SELECT count(*)::int FROM locations
WHERE tenant_id=$1::uuid AND parent_location_id=$2::uuid
  AND location_type='shed' AND status='active' AND retired_at IS NULL`, repoTenant, lsParkCPT).Scan(&wantShedCount); err != nil {
		t.Fatalf("count CPT sheds: %v", err)
	}
	if cpt.ShedCount != wantShedCount {
		t.Fatalf("park %s shed_count=%d, want the park's own active shed total %d", lsParkCPT, cpt.ShedCount, wantShedCount)
	}
	// The operator picker rides the park read, so the wizard holds it while it
	// pages buckets and never re-fetches the roster per shed page.
	if len(catalog.Operators) == 0 {
		t.Fatalf("planner catalog returned no operators; the wizard has no operator picker")
	}

	// The duplicate-block signal survives the split: the park read still names the
	// park's existing task on that date, which is what the wizard blocks on. It is
	// keyed on the task's PERIOD start date, so it is read for that date.
	dupCatalog, err := repo.PlannerCatalog(ctx, repoTenant, "2026-07-27")
	if err != nil {
		t.Fatalf("planner catalog on the fixture task's period: %v", err)
	}
	var existing *domain.CampaignSummary
	for _, park := range dupCatalog.Parks {
		if park.ParkID == repoPark {
			existing = park.ExistingCampaign
		}
	}
	if existing == nil {
		t.Fatalf("park %s reports no existing task on 2026-07-27; the wizard cannot block a duplicate", repoPark)
	}
	if existing.CampaignID != repoCampaign {
		t.Fatalf("existing_campaign=%s, want the fixture task %s", existing.CampaignID, repoCampaign)
	}
	if existing.ShedCount < 1 {
		t.Fatalf("existing task shed_count=%d, want its own bucket count at campaign grain", existing.ShedCount)
	}

	// And the many-side is still bounded: one park's buckets come back ~20 at a time.
	page, err := repo.PlannerParkBuckets(ctx, repoTenant, lsParkCPT, lsFixtureDay, "", "", 0)
	if err != nil {
		t.Fatalf("bucket page: %v", err)
	}
	if len(page.Sheds) != domain.PlannerBucketPageSize {
		t.Fatalf("bucket page size=%d, want the default page of %d", len(page.Sheds), domain.PlannerBucketPageSize)
	}
	if page.NextCursor == "" {
		t.Fatalf("park holding %d sheds reported no bucket cursor", wantShedCount)
	}
}

// A shed appears EXACTLY ONCE per catalog no matter how many weighing rows exist
// for it. If the taken side were joined raw instead of pre-aggregated, a shed with
// a canceled row plus an open row would come back twice and the planner would show
// duplicate buckets.
func TestPlannerCatalogTakenJoinOneToManyDoesNotMultiplyShedRows(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	// A second, CANCELED claim on the same shed and the same date, under another task.
	other := lcpUUID(21001)
	lcpInsertCampaign(t, ctx, pool, other, repoPark, "2026-08-24", domain.StatusDraft, repoOperator)
	lsSetCampaignWeighDate(t, ctx, pool, other, lsFixtureDay)
	lcpInsertBucket(t, ctx, pool, lcpUUID(21011), other, repoExpectedShed, domain.CategoryIndividualAnimal, repoOperator, 1, "canceled")

	buckets := drainAllParkBuckets(t, ctx, repo, lsFixtureDay, "")
	seen := len(buckets[repoExpectedShed])
	if seen != 1 {
		t.Fatalf("shed %s appears %d times across the bucket pages, want exactly 1", repoExpectedShed, seen)
	}
}

func TestPlannerParkBucketsExposeExactPartitionShedsOnly(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	groupShed := lcpUUID(21100)
	exactOne := lcpUUID(21101)
	exactTwo := lcpUUID(21102)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, parent_location_id, location_type, name, status, display_order)
VALUES
  ($1::uuid, $4::uuid, $5::uuid, 'shed', 'Gandhi', 'active', 10),
  ($2::uuid, $4::uuid, $5::uuid, 'shed', 'Gandhi 1', 'active', 11),
  ($3::uuid, $4::uuid, $5::uuid, 'shed', 'Gandhi 2', 'active', 12)
ON CONFLICT (location_id) DO NOTHING`, groupShed, exactOne, exactTwo, repoTenant, repoPark)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, source, operational_location_id)
VALUES
  ($1::uuid, $2::uuid, '1', '1', 'active', 'location_alias', $3::uuid),
  ($1::uuid, $2::uuid, '2', '2', 'active', 'location_alias', $4::uuid)
ON CONFLICT (tenant_id, shed_id, normalized_label) DO UPDATE
SET status='active',
    operational_location_id=EXCLUDED.operational_location_id,
    partition_label=EXCLUDED.partition_label`, repoTenant, groupShed, exactOne, exactTwo)

	buckets := drainParkBuckets(t, ctx, repo, repoPark, lsFixtureDay, "")
	if got := buckets[groupShed]; len(got) != 0 {
		t.Fatalf("group shed Gandhi appeared as %d planner bucket(s): %+v", len(got), got)
	}
	for _, exact := range []struct {
		id   string
		name string
	}{
		{exactOne, "Gandhi 1"},
		{exactTwo, "Gandhi 2"},
	} {
		rows := buckets[exact.id]
		if len(rows) != 1 {
			t.Fatalf("exact shed %s appeared %d times, want exactly once", exact.name, len(rows))
		}
		if rows[0].Name != exact.name || rows[0].OperationalLocationDisplay != exact.name {
			t.Fatalf("exact shed display=%q/%q, want %q", rows[0].Name, rows[0].OperationalLocationDisplay, exact.name)
		}
		if rows[0].PartitionLabel != "" {
			t.Fatalf("exact shed %s partition_label=%q, want blank because the partition is the shed", exact.name, rows[0].PartitionLabel)
		}
	}
}

func TestPlannerParkBucketsCollapsesPartLabelPartitionsWhenPhysicalPartShedsExist(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	parent := lcpUUID(21301)
	partOne := lcpUUID(21302)
	partTwo := lcpUUID(21303)
	lsInsertShed(t, ctx, pool, parent, repoPark, "Alias Parent", 720)
	lsInsertShed(t, ctx, pool, partOne, repoPark, "Alias Parent - Part 1", 721)
	lsInsertShed(t, ctx, pool, partTwo, repoPark, "Alias Parent - Part 2", 722)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, source, display_order)
VALUES
  ($1::uuid, $2::uuid, 'Part 1', '1', 'goat_attested', 1),
  ($1::uuid, $2::uuid, 'Part 2', '2', 'goat_attested', 2)
ON CONFLICT (tenant_id, shed_id, normalized_label) DO UPDATE
SET partition_label=EXCLUDED.partition_label, status='active', display_order=EXCLUDED.display_order`,
		repoTenant, parent)

	buckets := drainAllParkBuckets(t, ctx, repo, "2026-09-03", "")
	names := map[string]int{}
	for _, bucketList := range buckets {
		for _, bucket := range bucketList {
			names[bucket.Name]++
		}
	}
	for _, want := range []string{"Alias Parent - Part 1", "Alias Parent - Part 2"} {
		if names[want] != 1 {
			t.Fatalf("bucket %q appears %d times, want exactly once in %#v", want, names[want], names)
		}
	}
	for _, wrong := range []string{"Alias Parent", "Alias Parent - Part 1 - Part 1", "Alias Parent - Part 2 - Part 2"} {
		if names[wrong] != 0 {
			t.Fatalf("wrong bucket %q leaked into planner buckets: %#v", wrong, names)
		}
	}
}

func TestPlannerParkBucketsMarksOldParentPartitionTaskScheduledOnNumberedShed(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	parent := lcpUUID(21201)
	numbered := lcpUUID(21202)
	campaignID := lcpUUID(21203)
	campaignShedID := lcpUUID(21204)
	lsInsertShed(t, ctx, pool, parent, repoPark, "Alias Parent", 710)
	lsInsertShed(t, ctx, pool, numbered, repoPark, "Alias Parent 1", 711)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, source, display_order, operational_location_id)
VALUES ($1::uuid, $2::uuid, '1', '1', 'active', 'goat_attested', 1, $3::uuid)
ON CONFLICT (tenant_id, shed_id, normalized_label) DO UPDATE
SET partition_label=EXCLUDED.partition_label,
    status='active',
    display_order=EXCLUDED.display_order,
    operational_location_id=EXCLUDED.operational_location_id`,
		repoTenant, parent, numbered)
	lcpInsertCampaign(t, ctx, pool, campaignID, repoPark, "2026-09-02", domain.StatusPublished, repoOperator)
	lsSetCampaignWeighDate(t, ctx, pool, campaignID, "2026-09-02")
	lcpInsertBucket(t, ctx, pool, campaignShedID, campaignID, parent, domain.CategoryIndividualAnimal, repoOperator, 1, "pending")
	execWeighingTestSQL(t, ctx, pool, `
UPDATE weighing_campaign_sheds
SET partition_label='1'
WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid`,
		repoTenant, campaignShedID)

	page, err := repo.PlannerParkBuckets(ctx, repoTenant, repoPark, "2026-09-02", "", "", domain.MaxPlannerBucketPageSize)
	if err != nil {
		t.Fatalf("PlannerParkBuckets: %v", err)
	}
	for _, bucket := range page.Sheds {
		if bucket.LocationID == numbered && bucket.Name == "Alias Parent 1" {
			if !bucket.Scheduled {
				t.Fatalf("Alias Parent 1 rendered free; old parent+partition task must mark it scheduled: %+v", bucket)
			}
			if bucket.ScheduledCampaignID != campaignID {
				t.Fatalf("ScheduledCampaignID=%q, want %q", bucket.ScheduledCampaignID, campaignID)
			}
			return
		}
	}
	t.Fatalf("Alias Parent 1 not found in planner buckets: %#v", page.Sheds)
}

// -----------------------------------------------------------------------------
// 2. COUNTS GRAIN
// -----------------------------------------------------------------------------

// The tab counts are a WHOLE-FILTER aggregate at task grain. They must not change
// with the page size, must not change when the park chip narrows the rows, and must
// not be inflated by a task holding many buckets.
func TestCampaignCountsWholeFilterTaskGrainOneToManyPageBoundaryParkScopeStatusMatrix(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	// The fixture supplies one published (active) task. Add a known spread.
	// Multi-bucket on purpose: counts are per TASK, never per bucket.
	wide := lcpUUID(22001)
	lcpInsertCampaign(t, ctx, pool, wide, repoPark, "2026-09-07", domain.StatusDraft, repoOperator)
	lcpInsertBucket(t, ctx, pool, lcpUUID(22011), wide, repoExpectedShed, domain.CategoryIndividualAnimal, repoOperator, 1, "pending")
	lcpInsertBucket(t, ctx, pool, lcpUUID(22012), wide, lsFreeShed, domain.CategoryIndividualAnimal, repoOperator, 1, "pending")
	lcpInsertBucket(t, ctx, pool, lcpUUID(22013), wide, repoPerShed, domain.CategoryPerShedPartition, repoOperator, 1, "pending")

	lcpInsertCampaign(t, ctx, pool, lcpUUID(22002), repoPark, "2026-09-14", domain.StatusInProgress, repoOperator)
	lcpInsertCampaign(t, ctx, pool, lcpUUID(22003), lsParkCPT, "2026-09-21", domain.StatusDelayed, repoOperator)
	lcpInsertCampaign(t, ctx, pool, lcpUUID(22004), repoPark, "2026-09-28", domain.StatusCompleted, repoOperator)
	lcpInsertCampaign(t, ctx, pool, lcpUUID(22005), lsParkCPT, "2026-10-05", domain.StatusClosed, repoOperator)
	lcpInsertCampaign(t, ctx, pool, lcpUUID(22006), repoPark, "2026-10-12", "canceled", repoOperator)

	// fixture published + draft + in_progress + delayed
	const wantActive = 4
	// completed + closed; canceled is retracted work and counts in neither tab
	const wantCompleted = 2

	full, err := repo.ListCampaigns(ctx, repoTenant, "", "", 100)
	if err != nil {
		t.Fatalf("list campaigns: %v", err)
	}
	if full.Counts.Active != wantActive || full.Counts.Completed != wantCompleted {
		t.Fatalf("counts=%+v, want active=%d completed=%d (task grain, canceled excluded)", full.Counts, wantActive, wantCompleted)
	}

	// One row per page must not shrink the counts.
	page, err := repo.ListCampaigns(ctx, repoTenant, "", "", 1)
	if err != nil {
		t.Fatalf("list campaigns page: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("page items=%d, want 1", len(page.Items))
	}
	if page.Counts != full.Counts {
		t.Fatalf("page counts=%+v, want the whole-filter %+v — counts must never be derived from the page", page.Counts, full.Counts)
	}

	// The park chip filters ROWS only; the tab numbers must stay still.
	parked, err := repo.ListCampaigns(ctx, repoTenant, lsParkCPT, "", 100)
	if err != nil {
		t.Fatalf("list campaigns for park: %v", err)
	}
	if len(parked.Items) != 2 {
		t.Fatalf("park-filtered items=%d, want 2", len(parked.Items))
	}
	for _, item := range parked.Items {
		if item.ParkID != lsParkCPT {
			t.Fatalf("park filter leaked campaign in park %s", item.ParkID)
		}
	}
	if parked.Counts != full.Counts {
		t.Fatalf("park-filtered counts=%+v, want the unnarrowed %+v", parked.Counts, full.Counts)
	}
}

// The operator surface counts only that operator's own tasks — the same scope the
// rows use, so the tabs and the list can never disagree.
func TestCampaignCountsFollowTheOperatorScopeHierarchy(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	someoneElse := lcpUUID(23001)
	lcpInsertCampaign(t, ctx, pool, someoneElse, repoPark, "2026-11-02", domain.StatusPublished, repoOtherOp)
	lcpInsertBucket(t, ctx, pool, lcpUUID(23011), someoneElse, lsFreeShed, domain.CategoryIndividualAnimal, repoOtherOp, 1, "pending")

	all, err := repo.ListCampaigns(ctx, repoTenant, "", "", 100)
	if err != nil {
		t.Fatalf("list campaigns: %v", err)
	}
	mine, err := repo.ListCampaignsForOperator(ctx, repoTenant, repoOperator, "", "", 100)
	if err != nil {
		t.Fatalf("list campaigns for operator: %v", err)
	}
	if mine.Counts.Active >= all.Counts.Active {
		t.Fatalf("operator active=%d, leadership active=%d — the operator's tabs must exclude other people's tasks", mine.Counts.Active, all.Counts.Active)
	}
	if mine.Counts.Active != len(mine.Items) {
		t.Fatalf("operator active=%d but %d rows returned; counts and rows must share one scope", mine.Counts.Active, len(mine.Items))
	}
}

// -----------------------------------------------------------------------------
// 3. ONE OPEN ROW PER (park, weigh date, shed)
// -----------------------------------------------------------------------------

func TestCreateCampaignBlocksASecondOpenRowForTheSameShedAndWeighDateStatusMatrix(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	first := lsCreate(t, ctx, repo, "2026-12-07", "2026-12-07", "dup:first", lsFreeShed)
	if first.CampaignID == "" {
		t.Fatal("first create returned no campaign")
	}

	// Same park, same WEIGH DATE, same shed, different planning week (so the
	// pre-existing park/week guard is not what refuses this).
	_, err := lsCreateErr(ctx, repo, "2026-12-14", "2026-12-07", "dup:second", lsFreeShed)
	if !errors.Is(err, ports.ErrShedAlreadyScheduled) {
		t.Fatalf("duplicate open shed err=%v, want ErrShedAlreadyScheduled", err)
	}
	conflict := &ports.ShedScheduleConflict{}
	if !errors.As(err, &conflict) {
		t.Fatalf("err %v does not carry the conflicting bucket names", err)
	}
	if conflict.WeighDate != "2026-12-07" || len(conflict.Sheds) != 1 {
		t.Fatalf("conflict=%+v, want the weigh date and exactly the one blocked bucket", conflict)
	}

	// A DIFFERENT weigh date for the same shed is ordinary work, not a duplicate.
	if _, err := lsCreateErr(ctx, repo, "2026-12-21", "2026-12-21", "dup:other-day", lsFreeShed); err != nil {
		t.Fatalf("same shed on another weigh date must be allowed, got %v", err)
	}

	// Retiring the first claim frees the slot: a canceled plan is finished history
	// and must never occupy a date forever.
	execWeighingTestSQL(t, ctx, pool, `UPDATE weighing_campaign_sheds SET status='canceled' WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid`, repoTenant, first.CampaignID)
	if _, err := lsCreateErr(ctx, repo, "2027-01-04", "2026-12-07", "dup:after-cancel", lsFreeShed); err != nil {
		t.Fatalf("slot must free up once the earlier claim is canceled, got %v", err)
	}
}

func TestCreateCampaignBlocksNumberedShedWhenLegacyParentPartitionIsOpen(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	parent := lcpUUID(21401)
	numbered := lcpUUID(21402)
	legacyCampaignID := lcpUUID(21403)
	legacyBucketID := lcpUUID(21404)
	lsInsertShed(t, ctx, pool, parent, repoPark, "Alias Parent", 730)
	lsInsertShed(t, ctx, pool, numbered, repoPark, "Alias Parent 1", 731)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, source, display_order)
VALUES ($1::uuid, $2::uuid, '1', '1', 'goat_attested', 1)
ON CONFLICT (tenant_id, shed_id, normalized_label) DO UPDATE
SET partition_label=EXCLUDED.partition_label, status='active', display_order=EXCLUDED.display_order`,
		repoTenant, parent)
	lcpInsertCampaign(t, ctx, pool, legacyCampaignID, repoPark, "2026-12-28", domain.StatusPublished, repoOperator)
	lsSetCampaignWeighDate(t, ctx, pool, legacyCampaignID, "2026-12-28")
	lcpInsertBucket(t, ctx, pool, legacyBucketID, legacyCampaignID, parent, domain.CategoryIndividualAnimal, repoOperator, 1, "pending")
	execWeighingTestSQL(t, ctx, pool, `
UPDATE weighing_campaign_sheds
SET partition_label='1'
WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid`,
		repoTenant, legacyBucketID)

	_, err := repo.CreateCampaign(ctx, domain.CreateCampaign{
		TenantID:          repoTenant,
		ParkID:            repoPark,
		PeriodStartDate:   "2026-12-28",
		PeriodEndDate:     "2026-12-28",
		StartBusinessDate: "2026-12-28",
		PlannedCapPerDay:  100,
		OperatorUserID:    repoOperator,
		CreatedBy:         repoOperator,
		IdempotencyKey:    "legacy-parent-numbered-conflict",
		Sheds: []domain.CreateCampaignShed{{
			LocationID:       numbered,
			LocationType:     "shed",
			DisplayName:      "Alias Parent 1",
			WeighingCategory: domain.CategoryIndividualAnimal,
			OperatorUserID:   repoOperator,
		}},
	})
	if !errors.Is(err, ports.ErrShedAlreadyScheduled) {
		t.Fatalf("numbered shed create err=%v, want ErrShedAlreadyScheduled", err)
	}
	conflict := &ports.ShedScheduleConflict{}
	if !errors.As(err, &conflict) || len(conflict.Sheds) != 1 {
		t.Fatalf("err=%v, want one shed schedule conflict", err)
	}
}

func TestCreateCampaignBlocksPartShedWhenLegacyNormalizedPartitionIsOpen(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	parent := lcpUUID(21501)
	partOne := lcpUUID(21502)
	legacyCampaignID := lcpUUID(21503)
	legacyBucketID := lcpUUID(21504)
	lsInsertShed(t, ctx, pool, parent, repoPark, "Alias Parent", 740)
	lsInsertShed(t, ctx, pool, partOne, repoPark, "Alias Parent - Part 1", 741)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, source, display_order)
VALUES ($1::uuid, $2::uuid, 'Part 1', '1', 'goat_attested', 1)
ON CONFLICT (tenant_id, shed_id, normalized_label) DO UPDATE
SET partition_label=EXCLUDED.partition_label, status='active', display_order=EXCLUDED.display_order`,
		repoTenant, parent)
	lcpInsertCampaign(t, ctx, pool, legacyCampaignID, repoPark, "2027-01-11", domain.StatusPublished, repoOperator)
	lsSetCampaignWeighDate(t, ctx, pool, legacyCampaignID, "2027-01-11")
	lcpInsertBucket(t, ctx, pool, legacyBucketID, legacyCampaignID, parent, domain.CategoryIndividualAnimal, repoOperator, 1, "pending")
	execWeighingTestSQL(t, ctx, pool, `
UPDATE weighing_campaign_sheds
SET partition_label='1'
WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid`,
		repoTenant, legacyBucketID)

	_, err := repo.CreateCampaign(ctx, domain.CreateCampaign{
		TenantID:          repoTenant,
		ParkID:            repoPark,
		PeriodStartDate:   "2027-01-11",
		PeriodEndDate:     "2027-01-11",
		StartBusinessDate: "2027-01-11",
		PlannedCapPerDay:  100,
		OperatorUserID:    repoOperator,
		CreatedBy:         repoOperator,
		IdempotencyKey:    "legacy-parent-part-label-conflict",
		Sheds: []domain.CreateCampaignShed{{
			LocationID:       partOne,
			LocationType:     "shed",
			DisplayName:      "Alias Parent - Part 1",
			WeighingCategory: domain.CategoryIndividualAnimal,
			OperatorUserID:   repoOperator,
		}},
	})
	if !errors.Is(err, ports.ErrShedAlreadyScheduled) {
		t.Fatalf("part shed create err=%v, want ErrShedAlreadyScheduled", err)
	}
	conflict := &ports.ShedScheduleConflict{}
	if !errors.As(err, &conflict) || len(conflict.Sheds) != 1 {
		t.Fatalf("err=%v, want one shed schedule conflict", err)
	}
}

// A shed whose weighing is DONE is finished work, not an occupied slot: the CEO
// may schedule it again on the SAME date, in the same week, exactly as they may
// schedule a shed nobody ever touched. Only work still OWED blocks (maintainer
// decision 2026-08-03, migration 000089 -- this reverses 000062's original rule).
func TestCompletedShedIsSchedulableAgainOnTheSameWeighDate(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	const weighDate = "2027-04-05"
	first := lsCreate(t, ctx, repo, weighDate, weighDate, "done:first", lsFreeShed)

	// While the bucket is still owed, the slot is taken. This half must not regress.
	if _, err := lsCreateErr(ctx, repo, weighDate, weighDate, "done:while-open", lsFreeShed); !errors.Is(err, ports.ErrShedAlreadyScheduled) {
		t.Fatalf("same shed while the first claim is open: err=%v, want ErrShedAlreadyScheduled", err)
	}
	// The planner agrees: the bucket reads as taken.
	if shed := lsShed(t, drainAllParkBuckets(t, ctx, repo, weighDate, ""), lsFreeShed); !shed.Scheduled {
		t.Fatalf("shed %s reads available while its claim is open", lsFreeShed)
	}

	// The operator weighs it. The bucket is DONE.
	execWeighingTestSQL(t, ctx, pool, `UPDATE weighing_campaign_sheds SET status='completed', completed_at=now() WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid`, repoTenant, first.CampaignID)

	// Same shed, same date, same week -- now allowed.
	second, err := lsCreateErr(ctx, repo, weighDate, weighDate, "done:again", lsFreeShed)
	if err != nil {
		t.Fatalf("scheduling a COMPLETED shed again on the same date: %v", err)
	}
	if second.CampaignID == first.CampaignID {
		t.Fatal("second create returned the first task; it must be a new task")
	}
	// And the planner offers it again right up until the new claim is made, then
	// reports the NEW open claim -- never the finished one.
	shed := lsShed(t, drainAllParkBuckets(t, ctx, repo, weighDate, ""), lsFreeShed)
	if !shed.Scheduled || shed.ScheduledCampaignID != second.CampaignID {
		t.Fatalf("planner reports campaign %q for %s, want the new open task %s", shed.ScheduledCampaignID, lsFreeShed, second.CampaignID)
	}

	// A 'closed' bucket frees the slot the same way.
	execWeighingTestSQL(t, ctx, pool, `UPDATE weighing_campaign_sheds SET status='closed', closed_at=now() WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid`, repoTenant, second.CampaignID)
	if _, err := lsCreateErr(ctx, repo, weighDate, weighDate, "done:after-close", lsFreeShed); err != nil {
		t.Fatalf("scheduling a CLOSED shed again on the same date: %v", err)
	}
}

// The database is the authority, not the pre-check: even a write that bypasses the
// service (a script, a fixture, a future code path) cannot create the second open row.
func TestDatabaseRefusesASecondOpenRowForTheSameShedAndWeighDate(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)

	rival := lcpUUID(24001)
	lcpInsertCampaign(t, ctx, pool, rival, repoPark, "2026-08-31", domain.StatusPublished, repoOperator)
	lsSetCampaignWeighDate(t, ctx, pool, rival, lsFixtureDay)

	// Raw insert, no service, no pre-check: the fixture campaign already holds this
	// shed on this weigh date.
	_, err := pool.Exec(ctx, `
INSERT INTO weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, weighing_category, operator_user_id, expected_animal_count)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'shed', 'raw double booking', 'individual_animal', $5::uuid, 1)`,
		lcpUUID(24011), rival, repoTenant, repoExpectedShed, repoOperator)
	if err == nil {
		t.Fatal("a raw insert created a second OPEN row for the same park/weigh date/shed; the unique index is not enforcing")
	}
}

// Publishing re-checks before it hands the work to an operator. The positive case is
// unreachable while the unique index holds — which is the point — so this proves the
// re-check does not refuse an ordinary publish.
func TestPublishStillSucceedsWithTheDuplicateRecheckInPlace(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	draft := lsCreate(t, ctx, repo, "2027-02-01", "2027-02-01", "publish:recheck", lsFreeShed)
	published, err := repo.PublishCampaign(ctx, repoTenant, draft.CampaignID, repoOperator, "publish:recheck:key")
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if published.Status != domain.StatusPublished {
		t.Fatalf("status=%s, want published", published.Status)
	}
}

func TestPublishBlocksPartShedWhenLegacyNormalizedPartitionAppearsAfterCreate(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	parent := lcpUUID(21601)
	partOne := lcpUUID(21602)
	legacyCampaignID := lcpUUID(21603)
	legacyBucketID := lcpUUID(21604)
	lsInsertShed(t, ctx, pool, parent, repoPark, "Alias Parent", 750)
	lsInsertShed(t, ctx, pool, partOne, repoPark, "Alias Parent - Part 1", 751)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, source, display_order)
VALUES ($1::uuid, $2::uuid, 'Part 1', '1', 'goat_attested', 1)
ON CONFLICT (tenant_id, shed_id, normalized_label) DO UPDATE
SET partition_label=EXCLUDED.partition_label, status='active', display_order=EXCLUDED.display_order`,
		repoTenant, parent)

	draft, err := repo.CreateCampaign(ctx, domain.CreateCampaign{
		TenantID:          repoTenant,
		ParkID:            repoPark,
		PeriodStartDate:   "2027-01-18",
		PeriodEndDate:     "2027-01-18",
		StartBusinessDate: "2027-01-18",
		PlannedCapPerDay:  100,
		OperatorUserID:    repoOperator,
		CreatedBy:         repoOperator,
		IdempotencyKey:    "part-label-publish-draft",
		Sheds: []domain.CreateCampaignShed{{
			LocationID:       partOne,
			LocationType:     "shed",
			DisplayName:      "Alias Parent - Part 1",
			WeighingCategory: domain.CategoryIndividualAnimal,
			OperatorUserID:   repoOperator,
		}},
	})
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}

	lcpInsertCampaign(t, ctx, pool, legacyCampaignID, repoPark, "2027-01-18", domain.StatusPublished, repoOperator)
	lsSetCampaignWeighDate(t, ctx, pool, legacyCampaignID, "2027-01-18")
	lcpInsertBucket(t, ctx, pool, legacyBucketID, legacyCampaignID, parent, domain.CategoryIndividualAnimal, repoOperator, 1, "pending")
	execWeighingTestSQL(t, ctx, pool, `
UPDATE weighing_campaign_sheds
SET partition_label='1'
WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid`,
		repoTenant, legacyBucketID)

	_, err = repo.PublishCampaign(ctx, repoTenant, draft.CampaignID, repoOperator, "part-label-publish-conflict")
	if !errors.Is(err, ports.ErrShedAlreadyScheduled) {
		t.Fatalf("publish err=%v, want ErrShedAlreadyScheduled", err)
	}
	conflict := &ports.ShedScheduleConflict{}
	if !errors.As(err, &conflict) || len(conflict.Sheds) != 1 {
		t.Fatalf("err=%v, want one shed schedule conflict", err)
	}
}

// -----------------------------------------------------------------------------
// helpers
// -----------------------------------------------------------------------------

func lsCreate(t *testing.T, ctx context.Context, repo *Repository, periodStart, weighDate, idem, locationID string) domain.Campaign {
	t.Helper()
	campaign, err := lsCreateErr(ctx, repo, periodStart, weighDate, idem, locationID)
	if err != nil {
		t.Fatalf("create campaign %s: %v", idem, err)
	}
	return campaign
}

func lsCreateErr(ctx context.Context, repo *Repository, periodStart, weighDate, idem, locationID string) (domain.Campaign, error) {
	return repo.CreateCampaign(ctx, domain.CreateCampaign{
		TenantID:          repoTenant,
		ParkID:            repoPark,
		PeriodStartDate:   periodStart,
		PeriodEndDate:     periodStart,
		StartBusinessDate: weighDate,
		PlannedCapPerDay:  100,
		OperatorUserID:    repoOperator,
		CreatedBy:         repoOperator,
		IdempotencyKey:    idem,
		Sheds: []domain.CreateCampaignShed{{
			LocationID:       locationID,
			LocationType:     "shed",
			DisplayName:      "weighing bucket",
			WeighingCategory: domain.CategoryIndividualAnimal,
			OperatorUserID:   repoOperator,
		}},
	})
}

func lsSetCampaignWeighDate(t *testing.T, ctx context.Context, pool *pgxpool.Pool, campaignID, weighDate string) {
	t.Helper()
	execWeighingTestSQL(t, ctx, pool, `UPDATE weighing_campaigns SET start_business_date=$3::date WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid`, repoTenant, campaignID, weighDate)
}

func lsShed(t *testing.T, buckets map[string][]domain.PlannerShed, locationID string) domain.PlannerShed {
	t.Helper()
	sheds := buckets[locationID]
	if len(sheds) == 0 {
		t.Fatalf("shed %s missing from the planner bucket pages", locationID)
	}
	return sheds[0]
}
