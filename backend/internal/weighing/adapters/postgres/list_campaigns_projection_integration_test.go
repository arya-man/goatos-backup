package postgres

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// Adversarial regression tests for the campaign-list projection
// (listCampaigns + hydrateCampaigns). See the projection-review markers in
// repository.go: the campaign row grain is (tenant_id, campaign_id), the bucket
// grain is (tenant_id, campaign_id, campaign_shed_id), and every progress ratio
// must range over the SAME operator-visible bucket key set on both sides.
//
// Postgres integration tests are opt-in: GOATOS_RUN_POSTGRES_TESTS=1.

const (
	lcpParkCBE = "00000000-0000-4000-8000-000000003001"
	lcpParkCPT = "00000000-0000-4000-8000-000000003002"

	lcpShedOne   = "c93c636f-08f5-4ee9-aaca-bf7f1efd4788" // CBE Godel 1 - Part 8
	lcpShedTwo   = "83ced0ed-a9e5-415d-b06c-ad52dabebb7f" // CBE Yashoda 4
	lcpShedThree = "f7e8a700-0fde-48e6-bee0-fadce1f6816d" // CBE Sumathi 1 - Part 7
	lcpShedFour  = "9da6b465-489d-43d5-bda9-2e85d168459d" // CBE Mandela 2 - Part 2
	lcpShedFive  = "75731dd3-d669-4c12-8f6f-13caa501a5c1" // CBE Mandela 1 - Part 1
	lcpShedSix   = "9487caa4-fe05-4a42-ad01-0a6d0565f708" // CBE Godel 1 - Part 6
	lcpShedCPT   = "6e769414-4dde-43a5-b30f-fb857392b638" // CPT Mandela 1 - Part 5
)

// -----------------------------------------------------------------------------
// CARDINALITY / FAN-OUT
// -----------------------------------------------------------------------------

// A campaign that spans MANY buckets across MANY sheds (and a sibling campaign in
// a second park) must still be ONE row with ONE set of totals. Two fan-out paths
// are exercised: the locations join in the row path (if it were not 1:1 on the
// locations primary key the campaign would repeat per matching location row), and
// the operator predicate (if the weighing_campaign_sheds semijoin were written as a
// plain JOIN, a campaign holding 3 buckets for the operator would come back 3
// times and every count would be tripled).
func TestListCampaignsOneToManyShedFanOutDoesNotMultiplyCampaignRows(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	grantOperatorParkScope(t, ctx, pool)
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	wide := lcpUUID(11001)
	lcpInsertCampaign(t, ctx, pool, wide, lcpParkCBE, "2026-09-14", domain.StatusPublished, repoOperator)
	bucketOne := lcpUUID(11011)
	bucketTwo := lcpUUID(11012)
	bucketThree := lcpUUID(11013)
	bucketScope := lcpUUID(11014)
	lcpInsertBucket(t, ctx, pool, bucketOne, wide, lcpShedOne, domain.CategoryIndividualAnimal, repoOperator, 2, "pending")
	lcpInsertBucket(t, ctx, pool, bucketTwo, wide, lcpShedTwo, domain.CategoryIndividualAnimal, repoOperator, 3, "pending")
	lcpInsertBucket(t, ctx, pool, bucketThree, wide, lcpShedThree, domain.CategoryIndividualAnimal, repoOperator, 4, "pending")
	lcpInsertBucket(t, ctx, pool, bucketScope, wide, lcpShedFour, domain.CategoryPerShedPartition, repoOperator, 0, "pending")

	// Three real roster rows spread over two of the three individual buckets. The
	// expected-animal status column is roster/availability bookkeeping only --
	// IndividualCompletedCount is sourced from weighing_observations (see the
	// progressStats projection-review marker in repository.go), so "weighed" here
	// is left at its default and the two real captures below are what the count
	// actually measures. Any fan-out over the 4 buckets or the park row would
	// inflate the capture count.
	lcpInsertExpectedAnimal(t, ctx, pool, wide, lcpUUID(11101), bucketOne, lcpShedOne, "pending")
	lcpInsertExpectedAnimal(t, ctx, pool, wide, lcpUUID(11102), bucketOne, lcpShedOne, "pending")
	lcpInsertExpectedAnimal(t, ctx, pool, wide, lcpUUID(11103), bucketTwo, lcpShedTwo, "pending")
	lcpInsertProof(t, ctx, pool, lcpUUID(11401), lcpShedOne)
	lcpCapture(t, ctx, pool, repo, wide, bucketOne, lcpUUID(11401), "lcp-wide-bucket-one-capture", "lcp:wide:bucket-one:capture")
	lcpInsertProof(t, ctx, pool, lcpUUID(11402), lcpShedTwo)
	lcpCapture(t, ctx, pool, repo, wide, bucketTwo, lcpUUID(11402), "lcp-wide-bucket-two-capture", "lcp:wide:bucket-two:capture")

	// Sibling campaign in a SECOND park, same operator: proves the park label is
	// resolved per campaign and that two parks do not cross-multiply.
	otherPark := lcpUUID(11002)
	lcpInsertCampaign(t, ctx, pool, otherPark, lcpParkCPT, "2026-09-21", domain.StatusPublished, repoOperator)
	lcpInsertBucket(t, ctx, pool, lcpUUID(11021), otherPark, lcpShedCPT, domain.CategoryIndividualAnimal, repoOperator, 1, "pending")

	for _, tc := range []struct {
		name string
		list func() ([]domain.Campaign, error)
	}{
		{"leadership", func() ([]domain.Campaign, error) {
			page, err := repo.ListCampaigns(ctx, repoTenant, "", "", 100)
			return page.Items, err
		}},
		{"operator", func() ([]domain.Campaign, error) {
			page, err := repo.ListCampaignsForOperator(ctx, repoTenant, repoOperator, "", "", 100)
			return page.Items, err
		}},
	} {
		items, err := tc.list()
		if err != nil {
			t.Fatalf("%s list: %v", tc.name, err)
		}
		seen := map[string]int{}
		for _, item := range items {
			seen[item.CampaignID]++
		}
		if seen[wide] != 1 {
			t.Fatalf("%s: wide campaign returned %d times, want exactly 1 (bucket/location fan-out)", tc.name, seen[wide])
		}
		if seen[otherPark] != 1 {
			t.Fatalf("%s: second-park campaign returned %d times, want exactly 1", tc.name, seen[otherPark])
		}
		wideRow := lcpFind(t, items, wide)
		if len(wideRow.Sheds) != 4 {
			t.Fatalf("%s: wide campaign buckets=%d, want 4 distinct campaign_shed rows", tc.name, len(wideRow.Sheds))
		}
		if lcpDistinctBuckets(wideRow) != 4 {
			t.Fatalf("%s: wide campaign carries duplicate campaign_shed_id rows: %+v", tc.name, wideRow.Sheds)
		}
		if wideRow.Progress.IndividualExpectedCount != 9 {
			t.Fatalf("%s: individual expected=%d, want 9 (2+3+4 counted once per bucket)", tc.name, wideRow.Progress.IndividualExpectedCount)
		}
		if wideRow.Progress.PerScopeExpectedCount != 1 {
			t.Fatalf("%s: per-scope expected=%d, want 1", tc.name, wideRow.Progress.PerScopeExpectedCount)
		}
		if wideRow.Progress.IndividualCompletedCount != 2 {
			t.Fatalf("%s: individual completed=%d, want 2 weighed roster rows (fan-out would report 4 or 8)", tc.name, wideRow.Progress.IndividualCompletedCount)
		}
		if wideRow.ParkName != "Coimbatore" {
			t.Fatalf("%s: wide campaign park=%q, want Coimbatore", tc.name, wideRow.ParkName)
		}
		if got := lcpFind(t, items, otherPark).ParkName; got != "Channapatna" {
			t.Fatalf("%s: second campaign park=%q, want Channapatna", tc.name, got)
		}
	}
}

// -----------------------------------------------------------------------------
// PAGINATION / PAGE BOUNDARY
// -----------------------------------------------------------------------------

// Whole-filter truth must not depend on page size. The same five campaigns are
// walked at page sizes 1, 2, 3 and 100: the ordered id sequence and every
// campaign's whole-filter Progress must be byte-identical, including the campaign
// that lands exactly on a cursor boundary.
func TestListCampaignsPageBoundaryTotalsIdenticalAcrossPageSizes(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	grantOperatorParkScope(t, ctx, pool)
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	dates := []string{"2026-10-05", "2026-10-12", "2026-10-19", "2026-10-26", "2026-11-02"}
	ids := make([]string, 0, len(dates))
	for i, date := range dates {
		campaign := lcpUUID(12001 + i)
		bucket := lcpUUID(12101 + i)
		lcpInsertCampaign(t, ctx, pool, campaign, lcpParkCBE, date, domain.StatusPublished, repoOperator)
		lcpInsertBucket(t, ctx, pool, bucket, campaign, lcpShedOne, domain.CategoryIndividualAnimal, repoOperator, i+1, "pending")
		// i+1 roster rows, the first of them weighed, so each campaign has a distinct
		// non-trivial Progress that a page-local recomputation would get wrong.
		for n := 0; n <= i; n++ {
			status := "pending"
			if n == 0 {
				status = "weighed"
			}
			lcpInsertExpectedAnimal(t, ctx, pool, campaign, lcpUUID(12201+i*10+n), bucket, lcpShedOne, status)
		}
		ids = append(ids, campaign)
	}

	type walk struct {
		order    []string
		progress map[string]domain.Progress
	}
	walkAt := func(limit int) walk {
		out := walk{progress: map[string]domain.Progress{}}
		cursor := ""
		for pages := 0; ; pages++ {
			if pages > 50 {
				t.Fatalf("limit=%d: pagination did not terminate", limit)
			}
			page, err := repo.ListCampaignsForOperator(ctx, repoTenant, repoOperator, "", cursor, limit)
			if err != nil {
				t.Fatalf("limit=%d cursor=%q: %v", limit, cursor, err)
			}
			for _, item := range page.Items {
				out.order = append(out.order, item.CampaignID)
				if prev, ok := out.progress[item.CampaignID]; ok {
					t.Fatalf("limit=%d: campaign %s appeared twice across pages (%+v then %+v)", limit, item.CampaignID, prev, item.Progress)
				}
				out.progress[item.CampaignID] = item.Progress
			}
			if page.NextCursor == "" {
				return out
			}
			cursor = page.NextCursor
		}
	}

	baseline := walkAt(100)
	if len(baseline.order) != len(dates)+1 {
		t.Fatalf("baseline walk returned %d campaigns, want %d (5 new + the fixture campaign)", len(baseline.order), len(dates)+1)
	}
	for _, id := range ids {
		if _, ok := baseline.progress[id]; !ok {
			t.Fatalf("baseline walk missed campaign %s", id)
		}
	}
	// Page size 2 cuts the 6-row result exactly on a boundary; 1 and 3 shift it.
	for _, limit := range []int{1, 2, 3} {
		got := walkAt(limit)
		if len(got.order) != len(baseline.order) {
			t.Fatalf("limit=%d returned %d campaigns, want %d", limit, len(got.order), len(baseline.order))
		}
		for i := range got.order {
			if got.order[i] != baseline.order[i] {
				t.Fatalf("limit=%d order[%d]=%s, want %s (keyset boundary lost or duplicated a row)", limit, i, got.order[i], baseline.order[i])
			}
		}
		for id, want := range baseline.progress {
			if got.progress[id] != want {
				t.Fatalf("limit=%d progress for %s = %+v, want whole-filter %+v", limit, id, got.progress[id], want)
			}
		}
	}
}

// -----------------------------------------------------------------------------
// SCOPE HIERARCHY / PARK SCOPE
// -----------------------------------------------------------------------------

// An operator sees ONLY their own buckets, and every number attached to the
// campaign must be computed over that same bucket key set. The shared campaign
// below has 2 expected animals (1 weighed) in this operator's bucket and 5
// expected animals (4 weighed) in a sibling operator's bucket. If the rollups
// ranged over the whole campaign while the expected counts ranged over the
// operator's buckets, this operator would read 5 completed against 2 expected and
// a remaining count of 0 - the BUG-027 collapsed-key-set shape.
func TestListCampaignsParkScopeHierarchyOperatorSeesOnlyOwnBuckets(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	grantOperatorParkScope(t, ctx, pool)
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	shared := lcpUUID(13001)
	lcpInsertCampaign(t, ctx, pool, shared, lcpParkCBE, "2026-09-07", domain.StatusPublished, repoOperator)
	mine := lcpUUID(13011)
	theirs := lcpUUID(13012)
	lcpInsertBucket(t, ctx, pool, mine, shared, lcpShedOne, domain.CategoryIndividualAnimal, repoOperator, 2, "pending")
	lcpInsertBucket(t, ctx, pool, theirs, shared, lcpShedTwo, domain.CategoryIndividualAnimal, repoOtherOp, 5, "pending")
	lcpInsertExpectedAnimal(t, ctx, pool, shared, lcpUUID(13101), mine, lcpShedOne, "pending")
	lcpInsertExpectedAnimal(t, ctx, pool, shared, lcpUUID(13102), mine, lcpShedOne, "pending")
	for n := 0; n < 5; n++ {
		lcpInsertExpectedAnimal(t, ctx, pool, shared, lcpUUID(13201+n), theirs, lcpShedTwo, "pending")
	}
	// IndividualCompletedCount is sourced from weighing_observations, not the
	// expected-animal status column (see progressStats' projection-review marker) --
	// 1 real capture in "mine", 4 real captures in "theirs".
	lcpInsertProof(t, ctx, pool, lcpUUID(13401), lcpShedOne)
	lcpCapture(t, ctx, pool, repo, shared, mine, lcpUUID(13401), "lcp-shared-mine-capture", "lcp:shared:mine:capture")
	lcpInsertProof(t, ctx, pool, lcpUUID(13402), lcpShedTwo)
	for n := 0; n < 4; n++ {
		lcpCaptureAs(t, ctx, pool, repo, shared, theirs, lcpUUID(13402), fmt.Sprintf("lcp-shared-theirs-capture-%d", n), fmt.Sprintf("lcp:shared:theirs:capture:%d", n), repoOtherOp)
	}

	// A campaign whose only bucket for this operator is canceled: park-level work
	// exists, but this operator owns nothing live in it.
	canceledOnly := lcpUUID(13002)
	lcpInsertCampaign(t, ctx, pool, canceledOnly, lcpParkCBE, "2026-09-28", domain.StatusPublished, repoOtherOp)
	lcpInsertBucket(t, ctx, pool, lcpUUID(13021), canceledOnly, lcpShedThree, domain.CategoryIndividualAnimal, repoOperator, 3, "canceled")
	lcpInsertBucket(t, ctx, pool, lcpUUID(13022), canceledOnly, lcpShedFour, domain.CategoryIndividualAnimal, repoOtherOp, 1, "pending")

	// A campaign in the OTHER park with no bucket for this operator at all.
	otherParkCampaign := lcpUUID(13003)
	lcpInsertCampaign(t, ctx, pool, otherParkCampaign, lcpParkCPT, "2026-10-01", domain.StatusPublished, repoOtherOp)
	lcpInsertBucket(t, ctx, pool, lcpUUID(13031), otherParkCampaign, lcpShedCPT, domain.CategoryIndividualAnimal, repoOtherOp, 4, "pending")

	operatorPage, err := repo.ListCampaignsForOperator(ctx, repoTenant, repoOperator, "", "", 100)
	if err != nil {
		t.Fatalf("operator list: %v", err)
	}
	for _, item := range operatorPage.Items {
		if item.CampaignID == canceledOnly {
			t.Fatal("campaign whose only operator bucket is canceled leaked into the operator scope")
		}
		if item.CampaignID == otherParkCampaign {
			t.Fatal("a campaign in another park with no operator bucket leaked into the operator scope")
		}
		for _, shed := range item.Sheds {
			if shed.OperatorUserID != repoOperator {
				t.Fatalf("operator scope returned a foreign bucket %s owned by %s", shed.CampaignShedID, shed.OperatorUserID)
			}
		}
	}
	operatorRow := lcpFind(t, operatorPage.Items, shared)
	if len(operatorRow.Sheds) != 1 || operatorRow.Sheds[0].CampaignShedID != mine {
		t.Fatalf("operator buckets=%+v, want only %s", operatorRow.Sheds, mine)
	}
	if operatorRow.Progress.IndividualExpectedCount != 2 {
		t.Fatalf("operator expected=%d, want 2 (own bucket only)", operatorRow.Progress.IndividualExpectedCount)
	}
	if operatorRow.Progress.IndividualCompletedCount != 1 {
		t.Fatalf("operator completed=%d, want 1; the sibling operator's 4 completions must not be counted against this operator's expected set", operatorRow.Progress.IndividualCompletedCount)
	}
	if operatorRow.Progress.RemainingCount != 1 {
		t.Fatalf("operator remaining=%d, want 1; a collapsed key set clamps this to 0", operatorRow.Progress.RemainingCount)
	}

	leadershipPage, err := repo.ListCampaigns(ctx, repoTenant, "", "", 100)
	if err != nil {
		t.Fatalf("leadership list: %v", err)
	}
	leadershipRow := lcpFind(t, leadershipPage.Items, shared)
	if len(leadershipRow.Sheds) != 2 {
		t.Fatalf("leadership buckets=%d, want both park buckets", len(leadershipRow.Sheds))
	}
	if leadershipRow.Progress.IndividualExpectedCount != 7 {
		t.Fatalf("leadership expected=%d, want 7 (2+5)", leadershipRow.Progress.IndividualExpectedCount)
	}
	if leadershipRow.Progress.IndividualCompletedCount != 5 {
		t.Fatalf("leadership completed=%d, want 5 (1+4)", leadershipRow.Progress.IndividualCompletedCount)
	}
	if leadershipRow.Progress.RemainingCount != 2 {
		t.Fatalf("leadership remaining=%d, want 2", leadershipRow.Progress.RemainingCount)
	}
	lcpFind(t, leadershipPage.Items, canceledOnly)
	lcpFind(t, leadershipPage.Items, otherParkCampaign)
}

// -----------------------------------------------------------------------------
// STATUS MATRIX
// -----------------------------------------------------------------------------

// Every live DB-constrained status must be reachable, appear exactly once, and add
// up: total = sum(disjoint status buckets). The status sets are read from the live
// CHECK constraints (migration 000058 added campaign/bucket status 'closed'), so a
// future status that nobody wired into this projection fails here instead of
// silently disappearing from leadership's list.
func TestListCampaignsStatusMatrixCoversEveryStatusBucket(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	grantOperatorParkScope(t, ctx, pool)
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	campaignStatuses := lcpConstraintStatuses(t, ctx, pool, "weighing_campaigns_status_check")
	if !lcpContains(campaignStatuses, domain.StatusClosed) {
		t.Fatalf("campaign status set %v does not contain the migration 000058 'closed' status", campaignStatuses)
	}
	bucketStatuses := lcpConstraintStatuses(t, ctx, pool, "weighing_campaign_sheds_status_check")
	if !lcpContains(bucketStatuses, domain.StatusClosed) {
		t.Fatalf("bucket status set %v does not contain the migration 000058 'closed' status", bucketStatuses)
	}

	// One campaign per campaign status, each on its own week so the live
	// one-active-week-per-park uniqueness rule is respected.
	wantByStatus := map[string]string{}
	for i, status := range campaignStatuses {
		campaign := lcpUUID(14001 + i)
		date := fmt.Sprintf("2026-12-%02d", 1+i)
		lcpInsertCampaign(t, ctx, pool, campaign, lcpParkCBE, date, status, repoOperator)
		lcpInsertBucket(t, ctx, pool, lcpUUID(14101+i), campaign, lcpShedOne, domain.CategoryIndividualAnimal, repoOperator, 1, "pending")
		wantByStatus[status] = campaign
	}

	// One campaign carrying every BUCKET status at once, on distinct sheds.
	bucketMatrix := lcpUUID(14201)
	lcpInsertCampaign(t, ctx, pool, bucketMatrix, lcpParkCPT, "2026-12-21", domain.StatusInProgress, repoOperator)
	bucketSheds := []string{lcpShedCPT, lcpShedOne, lcpShedTwo, lcpShedThree, lcpShedFour, lcpShedFive, lcpShedSix}
	if len(bucketStatuses) > len(bucketSheds) {
		t.Fatalf("bucket status set %v needs more distinct sheds than the fixture provides", bucketStatuses)
	}
	for i, status := range bucketStatuses {
		lcpInsertBucket(t, ctx, pool, lcpUUID(14301+i), bucketMatrix, bucketSheds[i], domain.CategoryIndividualAnimal, repoOperator, 1, status)
	}

	page, err := repo.ListCampaigns(ctx, repoTenant, "", "", 100)
	if err != nil {
		t.Fatalf("leadership list: %v", err)
	}
	if page.NextCursor != "" {
		t.Fatalf("status matrix did not fit one page; cursor=%q", page.NextCursor)
	}
	buckets := map[string]int{}
	total := 0
	for _, item := range page.Items {
		if !lcpContains(campaignStatuses, item.Status) {
			t.Fatalf("campaign %s has status %q outside the live CHECK constraint %v", item.CampaignID, item.Status, campaignStatuses)
		}
		buckets[item.Status]++
		total++
	}
	sum := 0
	for _, status := range campaignStatuses {
		if buckets[status] == 0 {
			t.Fatalf("status %q produced no rows; every live status must be listable", status)
		}
		sum += buckets[status]
	}
	if sum != total {
		t.Fatalf("sum of disjoint status buckets=%d, total rows=%d", sum, total)
	}
	for status, campaign := range wantByStatus {
		item := lcpFind(t, page.Items, campaign)
		if item.Status != status {
			t.Fatalf("campaign %s status=%q, want %q", campaign, item.Status, status)
		}
	}

	matrixRow := lcpFind(t, page.Items, bucketMatrix)
	if len(matrixRow.Sheds) != len(bucketStatuses) {
		t.Fatalf("bucket matrix returned %d buckets, want %d", len(matrixRow.Sheds), len(bucketStatuses))
	}
	seenBucketStatus := map[string]int{}
	for _, shed := range matrixRow.Sheds {
		seenBucketStatus[shed.Status]++
	}
	for _, status := range bucketStatuses {
		if seenBucketStatus[status] != 1 {
			t.Fatalf("bucket status %q appeared %d times, want exactly 1 (buckets are disjoint by campaign_shed_id)", status, seenBucketStatus[status])
		}
	}
}

// -----------------------------------------------------------------------------
// helpers
// -----------------------------------------------------------------------------

func lcpUUID(n int) string {
	return fmt.Sprintf("00000000-0000-4000-8000-0000000%05d", n)
}

func lcpInsertCampaign(t *testing.T, ctx context.Context, pool *pgxpool.Pool, campaignID, parkID, periodStart, status, operatorID string) {
	t.Helper()
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaigns (campaign_id, tenant_id, park_id, period_start_date, period_end_date, start_business_date, status, planned_cap_per_day, operator_user_id, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::date, $4::date + 6, $4::date, $5, 100, $6::uuid, $6::uuid)`,
		campaignID, repoTenant, parkID, periodStart, status, operatorID)
}

func lcpInsertBucket(t *testing.T, ctx context.Context, pool *pgxpool.Pool, bucketID, campaignID, locationID, category, operatorID string, expected int, status string) {
	t.Helper()
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, weighing_category, operator_user_id, expected_animal_count, status)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'shed', (SELECT name FROM locations WHERE location_id=$4::uuid), $5, $6::uuid, $7, $8)`,
		bucketID, campaignID, repoTenant, locationID, category, operatorID, expected, status)
}

func lcpInsertExpectedAnimal(t *testing.T, ctx context.Context, pool *pgxpool.Pool, campaignID, animalID, bucketID, locationID, status string) {
	t.Helper()
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, 'G-' || right(replace($1::text,'-',''), 8), 'female', 'kid', 'alive', 'kid', $3::uuid,
  $4::uuid, (SELECT parent_location_id FROM locations WHERE location_id=$4::uuid), $4::uuid)
ON CONFLICT (goat_id) DO NOTHING`, animalID, repoTenant, repoParty, locationID)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_expected_animals (campaign_id, tenant_id, animal_id, expected_location_id, expected_location_label, campaign_shed_id, status)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, (SELECT name FROM locations WHERE location_id=$4::uuid), $5::uuid, $6)`,
		campaignID, repoTenant, animalID, locationID, bucketID, status)
}

// lcpInsertProof seeds a completed shed-scoped video proof, the only proof
// shape RecordAnimalObservation's free-flow write path accepts.
func lcpInsertProof(t *testing.T, ctx context.Context, pool *pgxpool.Pool, proofID, locationID string) {
	t.Helper()
	insertProof(t, ctx, pool, proofID, "video", "completed", "shed", locationID, "shed", locationID)
}

// lcpCapture drives a REAL free-flow capture through the repository (not a
// hand-set weighing_expected_animals.status column) so
// Progress.IndividualCompletedCount -- sourced from weighing_observations, see
// progressStats' projection-review marker -- reflects an actual scan.
func lcpCapture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repo *Repository, campaignID, bucketID, proofID, scannedIdentifier, idempotencyKey string) {
	t.Helper()
	lcpCaptureAs(t, ctx, pool, repo, campaignID, bucketID, proofID, scannedIdentifier, idempotencyKey, repoOperator)
}

// lcpCaptureAs is lcpCapture with an explicit RecordedBy, for buckets owned by
// an operator other than repoOperator (RecordAnimalObservation requires the
// caller to be the bucket's assignee).
func lcpCaptureAs(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repo *Repository, campaignID, bucketID, proofID, scannedIdentifier, idempotencyKey, recordedBy string) {
	t.Helper()
	if _, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID:          repoTenant,
		CampaignID:        campaignID,
		CampaignShedID:    bucketID,
		ScannedIdentifier: scannedIdentifier,
		WeightKg:          10,
		ProofArtifactID:   proofID,
		RecordedBy:        recordedBy,
		IdempotencyKey:    idempotencyKey,
	}); err != nil {
		t.Fatalf("lcpCapture %s/%s: %v", campaignID, scannedIdentifier, err)
	}
}

func lcpFind(t *testing.T, items []domain.Campaign, campaignID string) domain.Campaign {
	t.Helper()
	for _, item := range items {
		if item.CampaignID == campaignID {
			return item
		}
	}
	t.Fatalf("campaign %s missing from listing", campaignID)
	return domain.Campaign{}
}

func lcpDistinctBuckets(c domain.Campaign) int {
	seen := map[string]struct{}{}
	for _, shed := range c.Sheds {
		seen[shed.CampaignShedID] = struct{}{}
	}
	return len(seen)
}

func lcpContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

var lcpStatusLiteral = regexp.MustCompile(`'([a-z_]+)'`)

// lcpConstraintStatuses reads the live status vocabulary from the database CHECK
// constraint instead of a remembered list, as required by the aggregate review
// reference.
func lcpConstraintStatuses(t *testing.T, ctx context.Context, pool *pgxpool.Pool, constraintName string) []string {
	t.Helper()
	var definition string
	if err := pool.QueryRow(ctx, `
SELECT pg_get_constraintdef(oid)
FROM pg_constraint
WHERE conname=$1`, constraintName).Scan(&definition); err != nil {
		t.Fatalf("read %s definition: %v", constraintName, err)
	}
	matches := lcpStatusLiteral.FindAllStringSubmatch(definition, -1)
	statuses := make([]string, 0, len(matches))
	for _, match := range matches {
		statuses = append(statuses, match[1])
	}
	sort.Strings(statuses)
	if len(statuses) < 2 {
		t.Fatalf("parsed %d statuses from %s definition %q", len(statuses), constraintName, definition)
	}
	return statuses
}

// TestWeighingObservationsCompletedCountMultipleDimensions exercises the
// weighing_observations aggregate grain for IndividualCompletedCount:
// count(DISTINCT lower(btrim(...scanned_identifier))) across individual_animal
// buckets. Two separate dimension rows (sheds) with captures must not multiply
// the animal count.
func TestWeighingObservationsCompletedCountMultipleDimensions(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	grantOperatorParkScope(t, ctx, pool)
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	campaign := lcpUUID(20001)
	lcpInsertCampaign(t, ctx, pool, campaign, lcpParkCBE, "2026-10-01", domain.StatusPublished, repoOperator)
	bucket1 := lcpUUID(20011)
	bucket2 := lcpUUID(20012)
	lcpInsertBucket(t, ctx, pool, bucket1, campaign, lcpShedOne, domain.CategoryIndividualAnimal, repoOperator, 5, "pending")
	lcpInsertBucket(t, ctx, pool, bucket2, campaign, lcpShedTwo, domain.CategoryIndividualAnimal, repoOperator, 5, "pending")
	// Two captures in bucket1, two in bucket2 - total should be 4, not 8
	lcpInsertProof(t, ctx, pool, lcpUUID(20401), lcpShedOne)
	lcpCapture(t, ctx, pool, repo, campaign, bucket1, lcpUUID(20401), "tag1", "idem1")
	lcpCapture(t, ctx, pool, repo, campaign, bucket1, lcpUUID(20401), "tag2", "idem2")
	lcpInsertProof(t, ctx, pool, lcpUUID(20402), lcpShedTwo)
	lcpCapture(t, ctx, pool, repo, campaign, bucket2, lcpUUID(20402), "tag3", "idem3")
	lcpCapture(t, ctx, pool, repo, campaign, bucket2, lcpUUID(20402), "tag4", "idem4")

	page, err := repo.ListCampaigns(ctx, repoTenant, "", "", 100)
	if err != nil {
		t.Fatalf("ListCampaigns: %v", err)
	}
	campaigns := page.Items
	if len(campaigns) != 1 {
		t.Fatalf("expected 1 campaign, got %d", len(campaigns))
	}
	if campaigns[0].Progress.IndividualCompletedCount != 4 {
		t.Fatalf("IndividualCompletedCount: expected 4, got %d", campaigns[0].Progress.IndividualCompletedCount)
	}
}

// TestWeighingObservationsCompletedCountPaginationBoundary exercises the
// weighing_observations aggregate across a paginated list. The completed count
// must remain the same regardless of page size.
func TestWeighingObservationsCompletedCountPaginationBoundary(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	grantOperatorParkScope(t, ctx, pool)
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	// Create 3 campaigns with captures
	for i := 0; i < 3; i++ {
		campaign := lcpUUID(30000 + i)
		lcpInsertCampaign(t, ctx, pool, campaign, lcpParkCBE, "2026-10-01", domain.StatusPublished, repoOperator)
		bucket := lcpUUID(30100 + i)
		lcpInsertBucket(t, ctx, pool, bucket, campaign, lcpShedOne, domain.CategoryIndividualAnimal, repoOperator, 5, "pending")
		lcpInsertProof(t, ctx, pool, lcpUUID(30500+i), lcpShedOne)
		lcpCapture(t, ctx, pool, repo, campaign, bucket, lcpUUID(30500+i), fmt.Sprintf("tag%d", i), fmt.Sprintf("idem%d", i))
	}

	// List with page size 2
	page1, err := repo.ListCampaigns(ctx, repoTenant, "", "", 2)
	if err != nil {
		t.Fatalf("ListCampaigns page 1: %v", err)
	}
	if len(page1.Items) != 2 {
		t.Fatalf("page 1: expected 2 items, got %d", len(page1.Items))
	}
	if page1.Items[0].Progress.IndividualCompletedCount != 1 || page1.Items[1].Progress.IndividualCompletedCount != 1 {
		t.Fatalf("page 1 counts wrong: %d, %d", page1.Items[0].Progress.IndividualCompletedCount, page1.Items[1].Progress.IndividualCompletedCount)
	}

	// List with page size 3 (everything in one page)
	page2, err := repo.ListCampaigns(ctx, repoTenant, "", "", 3)
	if err != nil {
		t.Fatalf("ListCampaigns page 2: %v", err)
	}
	if len(page2.Items) != 3 {
		t.Fatalf("page 2: expected 3 items, got %d", len(page2.Items))
	}
	for i, item := range page2.Items {
		if item.Progress.IndividualCompletedCount != 1 {
			t.Fatalf("page 2 item %d: expected count 1, got %d", i, item.Progress.IndividualCompletedCount)
		}
	}
}

// TestWeighingObservationsCompletedCountEveryStatus exercises the
// weighing_observations aggregate across all campaign statuses. The completed
// count must be computed regardless of campaign status (published/canceled/etc).
func TestWeighingObservationsCompletedCountEveryStatus(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	grantOperatorParkScope(t, ctx, pool)
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	statuses := []string{domain.StatusPublished, domain.StatusClosed, domain.StatusCompleted}
	for i, status := range statuses {
		campaign := lcpUUID(40000 + i)
		lcpInsertCampaign(t, ctx, pool, campaign, lcpParkCBE, "2026-10-01", status, repoOperator)
		bucket := lcpUUID(40100 + i)
		lcpInsertBucket(t, ctx, pool, bucket, campaign, lcpShedOne, domain.CategoryIndividualAnimal, repoOperator, 5, "pending")
		lcpInsertProof(t, ctx, pool, lcpUUID(40500+i), lcpShedOne)
		lcpCapture(t, ctx, pool, repo, campaign, bucket, lcpUUID(40500+i), fmt.Sprintf("tag%d", i), fmt.Sprintf("idem%d", i))
	}

	page, err := repo.ListCampaigns(ctx, repoTenant, "", "", 100)
	if err != nil {
		t.Fatalf("ListCampaigns: %v", err)
	}
	if len(page.Items) != 3 {
		t.Fatalf("expected 3 campaigns, got %d", len(page.Items))
	}
	for i, campaign := range page.Items {
		if campaign.Progress.IndividualCompletedCount != 1 {
			t.Fatalf("campaign %d: expected count 1, got %d", i, campaign.Progress.IndividualCompletedCount)
		}
	}
}
