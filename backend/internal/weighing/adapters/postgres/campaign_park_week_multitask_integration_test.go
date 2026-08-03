package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// ONE PARK-WEEK MAY HOLD MORE THAN ONE WEIGHING TASK.
//
// The real scheduling invariant is shed-grain, not campaign-grain: a shed must
// not be somebody's open work twice on the same date. That is enforced by
// uq_weighing_open_shed_per_park_date (migration 000062) on
// weighing_campaign_sheds.
//
// The campaign-grain index weighing_campaigns_one_active_week_per_park_idx
// (tenant_id, park_id, period_type, cadence_type, period_start_date) could not
// express that rule and blocked legitimate work instead:
//   - weighing_category lives on weighing_campaign_sheds, NOT on
//     weighing_campaigns, so the campaign-grain index cannot tell a lump-sum
//     task from an individual one -- and a campaign may legitimately MIX both,
//     so the category can never be denormalized up to the campaign to fix it.
//   - period_type and cadence_type are pinned to single values by CHECK
//     constraints, so neither discriminates either.
// Net effect: exactly one campaign per park per week, full stop. Planning the
// LEFTOVER sheds of a park as a second task in the same week -- the maintainer's
// real flow -- was refused with a 409.
//
// Postgres integration tests are opt-in: GOATOS_RUN_POSTGRES_TESTS=1.

// A CEO plans two sheds as one lump-sum task, then plans the park's REMAINING
// sheds as a second, individual-capture task in the SAME week and on the SAME
// weigh date. Both tasks must exist, and each must keep its own bucket set.
func TestCreateCampaignAllowsSecondTaskForLeftoverShedsInSameParkWeek(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	const week = "2026-10-05"
	const weekEnd = "2026-10-11"
	const weighDate = "2026-10-07"

	lumpSum, err := repo.CreateCampaign(ctx, domain.CreateCampaign{
		TenantID:          repoTenant,
		ParkID:            repoPark,
		PeriodStartDate:   week,
		PeriodEndDate:     weekEnd,
		StartBusinessDate: weighDate,
		PlannedCapPerDay:  100,
		OperatorUserID:    repoOperator,
		CreatedBy:         repoOperator,
		IdempotencyKey:    "park-week:leftover:lumpsum",
		Sheds: []domain.CreateCampaignShed{
			{LocationID: lcpShedOne, LocationType: "shed", DisplayName: "CBE Godel 1 - Part 8", WeighingCategory: domain.CategoryPerShedPartition},
			{LocationID: lcpShedTwo, LocationType: "shed", DisplayName: "CBE Yashoda 4", WeighingCategory: domain.CategoryPerShedPartition},
		},
	})
	if err != nil {
		t.Fatalf("first (lump-sum) task create: %v", err)
	}

	// THE MAINTAINER'S CASE: same park, same week, same weigh date, DIFFERENT
	// sheds, different category. Nothing about this is duplicate work.
	individual, err := repo.CreateCampaign(ctx, domain.CreateCampaign{
		TenantID:          repoTenant,
		ParkID:            repoPark,
		PeriodStartDate:   week,
		PeriodEndDate:     weekEnd,
		StartBusinessDate: weighDate,
		PlannedCapPerDay:  100,
		OperatorUserID:    repoOperator,
		CreatedBy:         repoOperator,
		IdempotencyKey:    "park-week:leftover:individual",
		Sheds: []domain.CreateCampaignShed{
			{LocationID: lcpShedThree, LocationType: "shed", DisplayName: "CBE Sumathi 1 - Part 7", WeighingCategory: domain.CategoryIndividualAnimal},
		},
	})
	if err != nil {
		t.Fatalf("second (leftover, individual) task create in the same park-week: %v", err)
	}

	if lumpSum.CampaignID == individual.CampaignID {
		t.Fatalf("both creates returned the same campaign %s; the second must be its own task", lumpSum.CampaignID)
	}

	// BOTH campaign rows survive, and each keeps exactly its own buckets. This is
	// the assertion that would still fail if the second create were silently
	// folded into the first by idempotency rather than genuinely created.
	assertCampaignShedLocations(t, ctx, pool, lumpSum.CampaignID, lcpShedOne, lcpShedTwo)
	assertCampaignShedLocations(t, ctx, pool, individual.CampaignID, lcpShedThree)
	assertCampaignCategories(t, ctx, pool, lumpSum.CampaignID, domain.CategoryPerShedPartition)
	assertCampaignCategories(t, ctx, pool, individual.CampaignID, domain.CategoryIndividualAnimal)
}

// The rule that actually matters must still hold: the SAME shed cannot be open
// work twice on the SAME date, no matter which campaign asks for it. Dropping
// the campaign-grain index must not weaken this.
func TestCreateCampaignStillRejectsShedAlreadyScheduledOnThatDate(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	const week = "2026-10-12"
	const weekEnd = "2026-10-18"
	const weighDate = "2026-10-14"

	if _, err := repo.CreateCampaign(ctx, domain.CreateCampaign{
		TenantID:          repoTenant,
		ParkID:            repoPark,
		PeriodStartDate:   week,
		PeriodEndDate:     weekEnd,
		StartBusinessDate: weighDate,
		PlannedCapPerDay:  100,
		OperatorUserID:    repoOperator,
		CreatedBy:         repoOperator,
		IdempotencyKey:    "park-week:double-book:first",
		Sheds: []domain.CreateCampaignShed{
			{LocationID: lcpShedFour, LocationType: "shed", DisplayName: "CBE Mandela 2 - Part 2", WeighingCategory: domain.CategoryIndividualAnimal},
		},
	}); err != nil {
		t.Fatalf("first task create: %v", err)
	}

	// Same date, same shed, different campaign and different category. The
	// category difference must NOT buy a second claim on the same bucket.
	_, err := repo.CreateCampaign(ctx, domain.CreateCampaign{
		TenantID:          repoTenant,
		ParkID:            repoPark,
		PeriodStartDate:   week,
		PeriodEndDate:     weekEnd,
		StartBusinessDate: weighDate,
		PlannedCapPerDay:  100,
		OperatorUserID:    repoOperator,
		CreatedBy:         repoOperator,
		IdempotencyKey:    "park-week:double-book:second",
		Sheds: []domain.CreateCampaignShed{
			{LocationID: lcpShedFour, LocationType: "shed", DisplayName: "CBE Mandela 2 - Part 2", WeighingCategory: domain.CategoryPerShedPartition},
		},
	})
	var conflict *ports.ShedScheduleConflict
	if !errors.As(err, &conflict) {
		t.Fatalf("re-booking an already-scheduled shed err=%v, want *ports.ShedScheduleConflict", err)
	}
	if conflict.WeighDate != weighDate {
		t.Fatalf("conflict weigh date=%s, want %s", conflict.WeighDate, weighDate)
	}
	if len(conflict.Sheds) != 1 {
		t.Fatalf("conflict names %d sheds, want exactly the 1 blocked bucket: %v", len(conflict.Sheds), conflict.Sheds)
	}

	// The rejected create must leave NOTHING behind: no orphan campaign row and
	// no second bucket for that shed.
	assertOpenBucketCountForShed(t, ctx, pool, weighDate, lcpShedFour, 1)
	assertParkWeekCampaignCount(t, ctx, pool, week, 1)
}

// READ MODELS with two campaigns in one park-week. The campaign list is the
// surface that would break silently if any reader still assumed a park-week
// held at most one task: both tasks must appear as SEPARATE rows carrying
// their OWN bucket counts, never merged and never fanned out.
func TestReadModelsReportBothCampaignsInOneParkWeek(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	const week = "2026-10-19"
	const weekEnd = "2026-10-25"
	const weighDate = "2026-10-21"

	first, err := repo.CreateCampaign(ctx, domain.CreateCampaign{
		TenantID: repoTenant, ParkID: repoPark,
		PeriodStartDate: week, PeriodEndDate: weekEnd, StartBusinessDate: weighDate,
		PlannedCapPerDay: 100, OperatorUserID: repoOperator, CreatedBy: repoOperator,
		IdempotencyKey: "park-week:readmodel:first",
		Sheds: []domain.CreateCampaignShed{
			{LocationID: lcpShedOne, LocationType: "shed", DisplayName: "CBE Godel 1 - Part 8", WeighingCategory: domain.CategoryPerShedPartition},
			{LocationID: lcpShedTwo, LocationType: "shed", DisplayName: "CBE Yashoda 4", WeighingCategory: domain.CategoryPerShedPartition},
		},
	})
	if err != nil {
		t.Fatalf("first task create: %v", err)
	}
	second, err := repo.CreateCampaign(ctx, domain.CreateCampaign{
		TenantID: repoTenant, ParkID: repoPark,
		PeriodStartDate: week, PeriodEndDate: weekEnd, StartBusinessDate: weighDate,
		PlannedCapPerDay: 100, OperatorUserID: repoOperator, CreatedBy: repoOperator,
		IdempotencyKey: "park-week:readmodel:second",
		Sheds: []domain.CreateCampaignShed{
			{LocationID: lcpShedThree, LocationType: "shed", DisplayName: "CBE Sumathi 1 - Part 7", WeighingCategory: domain.CategoryIndividualAnimal},
		},
	})
	if err != nil {
		t.Fatalf("second task create: %v", err)
	}

	page, err := repo.ListCampaigns(ctx, repoTenant, repoPark, "", 100)
	if err != nil {
		t.Fatalf("list campaigns: %v", err)
	}
	shedCountByCampaign := map[string]int{}
	occurrences := map[string]int{}
	for _, item := range page.Items {
		occurrences[item.CampaignID]++
		shedCountByCampaign[item.CampaignID] = len(item.Sheds)
	}
	// NO FAN-OUT: the 2-bucket campaign must be ONE row, not two.
	if occurrences[first.CampaignID] != 1 {
		t.Fatalf("first campaign appears %d times in the list, want exactly 1 row", occurrences[first.CampaignID])
	}
	if occurrences[second.CampaignID] != 1 {
		t.Fatalf("second campaign appears %d times in the list, want exactly 1 row", occurrences[second.CampaignID])
	}
	// NO MERGE: each row carries its OWN bucket count, not the park-week total.
	if shedCountByCampaign[first.CampaignID] != 2 {
		t.Fatalf("first campaign shed_count=%d, want 2 (its own buckets only)", shedCountByCampaign[first.CampaignID])
	}
	if shedCountByCampaign[second.CampaignID] != 1 {
		t.Fatalf("second campaign shed_count=%d, want 1 (its own buckets only)", shedCountByCampaign[second.CampaignID])
	}

	// The PLANNER CATALOG is park-grain. Two campaigns in the park-week must not
	// multiply the park row, and the park must report that it holds 2 tasks --
	// under-reporting 1 is what let the UI claim "already scheduled, create is
	// blocked" and hide the leftover-shed path.
	catalog, err := repo.PlannerCatalog(ctx, repoTenant, week)
	if err != nil {
		t.Fatalf("planner catalog: %v", err)
	}
	parkRows := 0
	var park domain.PlannerPark
	for _, p := range catalog.Parks {
		if p.ParkID == repoPark {
			parkRows++
			park = p
		}
	}
	if parkRows != 1 {
		t.Fatalf("planner catalog returned the park %d times, want exactly 1 row", parkRows)
	}
	if park.ExistingCampaignCount != 2 {
		t.Fatalf("planner park existing_campaign_count=%d, want 2", park.ExistingCampaignCount)
	}
	if park.ExistingCampaign == nil {
		t.Fatalf("planner park existing_campaign is nil, want the most recent task summarized")
	}
	// Whichever task the summary picks, its shed count must be that task's OWN
	// bucket count -- never the park-week sum (3), which is the classic merge bug.
	if park.ExistingCampaign.ShedCount != shedCountByCampaign[park.ExistingCampaign.CampaignID] {
		t.Fatalf("planner existing_campaign shed_count=%d for campaign %s, want %d",
			park.ExistingCampaign.ShedCount, park.ExistingCampaign.CampaignID,
			shedCountByCampaign[park.ExistingCampaign.CampaignID])
	}
}

// ---------------------------------------------------------------------------
// assertions
// ---------------------------------------------------------------------------

func assertCampaignShedLocations(t *testing.T, ctx context.Context, pool *pgxpool.Pool, campaignID string, want ...string) {
	t.Helper()
	rows, err := pool.Query(ctx, `
SELECT location_id::text
FROM weighing_campaign_sheds
WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid
ORDER BY location_id`, repoTenant, campaignID)
	if err != nil {
		t.Fatalf("read campaign buckets: %v", err)
	}
	defer rows.Close()
	got := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan bucket: %v", err)
		}
		got[id] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("bucket rows: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("campaign %s holds %d buckets, want %d", campaignID, len(got), len(want))
	}
	for _, id := range want {
		if !got[id] {
			t.Fatalf("campaign %s is missing its bucket for shed %s", campaignID, id)
		}
	}
}

func assertCampaignCategories(t *testing.T, ctx context.Context, pool *pgxpool.Pool, campaignID string, want string) {
	t.Helper()
	var mismatched int
	if err := pool.QueryRow(ctx, `
SELECT count(*)::int
FROM weighing_campaign_sheds
WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid AND weighing_category <> $3`,
		repoTenant, campaignID, want).Scan(&mismatched); err != nil {
		t.Fatalf("read campaign categories: %v", err)
	}
	if mismatched != 0 {
		t.Fatalf("campaign %s has %d buckets whose category is not %s", campaignID, mismatched, want)
	}
}

func assertOpenBucketCountForShed(t *testing.T, ctx context.Context, pool *pgxpool.Pool, weighDate, locationID string, want int) {
	t.Helper()
	var got int
	if err := pool.QueryRow(ctx, `
SELECT count(*)::int
FROM weighing_campaign_sheds
WHERE tenant_id=$1::uuid AND start_business_date=$2::date AND location_id=$3::uuid
  AND status NOT IN ('canceled','closed')`, repoTenant, weighDate, locationID).Scan(&got); err != nil {
		t.Fatalf("count open buckets: %v", err)
	}
	if got != want {
		t.Fatalf("shed %s has %d open buckets on %s, want %d", locationID, got, weighDate, want)
	}
}

func assertParkWeekCampaignCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, week string, want int) {
	t.Helper()
	var got int
	if err := pool.QueryRow(ctx, `
SELECT count(*)::int
FROM weighing_campaigns
WHERE tenant_id=$1::uuid AND park_id=$2::uuid AND period_start_date=$3::date
  AND status <> 'canceled'`, repoTenant, repoPark, week).Scan(&got); err != nil {
		t.Fatalf("count park-week campaigns: %v", err)
	}
	if got != want {
		t.Fatalf("park-week %s holds %d campaigns, want %d", week, got, want)
	}
}
