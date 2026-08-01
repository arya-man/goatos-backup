package postgres

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// Paging guarantees for the LEADERSHIP weighing reads.
//
// Every one of these reads used to return its whole set: the shed evidence read
// selected every weighing_observations row for the bucket, the planner catalog
// returned every shed of every park (76+ per park in the real data), and the task
// detail had no read of its own at all. These tests pin the four properties a
// keyset page has to have — first page, second page, exhaustion, and stability
// when a new row lands mid-page — plus the shed context that used to travel as
// client route args.
//
// Postgres integration tests are opt-in: GOATOS_RUN_POSTGRES_TESTS=1.

// seedLeadershipObservations inserts n individual observations into the
// individual-animal bucket with STRICTLY INCREASING accepted_at, one second
// apart, starting at base. Distinct timestamps are the point: they make the
// expected page boundaries unambiguous, and the tie-break on observation_id is
// exercised separately by the equal-timestamp case below.
func seedLeadershipObservations(t *testing.T, ctx context.Context, pool *pgxpool.Pool, base time.Time, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_observations (tenant_id, campaign_id, campaign_shed_id, animal_id, scanned_identifier, weight_kg, proof_artifact_id, recorded_by, idempotency_key, accepted_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, NULL, $6, 10.0, $4::uuid, $5::uuid, $7, $8::timestamptz)`,
			repoTenant, repoCampaign, repoAnimalScope, repoAnimalProof, repoOperator,
			fmt.Sprintf("page-tag-%03d", i), fmt.Sprintf("paging:%03d", i),
			base.Add(time.Duration(i)*time.Second))
	}
}

func leadershipTags(page domain.LeadershipShedVideos) []string {
	out := make([]string, 0, len(page.Individual))
	for _, observation := range page.Individual {
		out = append(out, observation.AnimalID)
	}
	return out
}

// A bucket with more evidence than one page must hand back exactly the page size,
// a cursor, then the next page, and finally an empty cursor at exhaustion — and
// the pages together must be the whole set with no row repeated or dropped.
func TestGetLeadershipShedVideosPagesObservationsAndTerminates(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	base := time.Date(2026, 7, 29, 4, 0, 0, 0, time.UTC)
	seedLeadershipObservations(t, ctx, pool, base, 25)
	repo := NewRepository(pool, 5*time.Second)

	first, err := repo.GetLeadershipShedVideos(ctx, repoTenant, repoCampaign, repoAnimalScope, "", 0)
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(first.Individual) != domain.LeadershipShedVideosPageSize {
		t.Fatalf("first page size=%d, want the default page of %d", len(first.Individual), domain.LeadershipShedVideosPageSize)
	}
	if first.NextIndividualCursor == "" {
		t.Fatalf("first page returned no cursor with 25 observations behind a %d-row page", domain.LeadershipShedVideosPageSize)
	}

	second, err := repo.GetLeadershipShedVideos(ctx, repoTenant, repoCampaign, repoAnimalScope, first.NextIndividualCursor, 0)
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(second.Individual) != 5 {
		t.Fatalf("second page size=%d, want the remaining 5", len(second.Individual))
	}
	if second.NextIndividualCursor != "" {
		t.Fatalf("second page cursor=%q, want empty at exhaustion", second.NextIndividualCursor)
	}

	seen := map[string]bool{}
	for _, tag := range append(leadershipTags(first), leadershipTags(second)...) {
		if seen[tag] {
			t.Fatalf("tag %s appeared on both pages — the keyset is not exclusive", tag)
		}
		seen[tag] = true
	}
	if len(seen) != 25 {
		t.Fatalf("pages covered %d distinct observations, want all 25", len(seen))
	}
}

// A row inserted AFTER the first page was served must not shift the second page.
// accepted_at only moves forward, so a new capture sorts after the cursor and
// shows up on a later page — it can never duplicate or push out a row the caller
// already read.
func TestGetLeadershipShedVideosCursorIsStableWhenARowLandsMidPage(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	base := time.Date(2026, 7, 29, 4, 0, 0, 0, time.UTC)
	seedLeadershipObservations(t, ctx, pool, base, 25)
	repo := NewRepository(pool, 5*time.Second)

	first, err := repo.GetLeadershipShedVideos(ctx, repoTenant, repoCampaign, repoAnimalScope, "", 10)
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	firstTags := leadershipTags(first)

	// A fresh capture arrives between the two reads.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_observations (tenant_id, campaign_id, campaign_shed_id, animal_id, scanned_identifier, weight_kg, proof_artifact_id, recorded_by, idempotency_key, accepted_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, NULL, 'page-tag-late', 11.5, $4::uuid, $5::uuid, 'paging:late', $6::timestamptz)`,
		repoTenant, repoCampaign, repoAnimalScope, repoAnimalProof, repoOperator, base.Add(500*time.Second))

	second, err := repo.GetLeadershipShedVideos(ctx, repoTenant, repoCampaign, repoAnimalScope, first.NextIndividualCursor, 10)
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	for _, tag := range leadershipTags(second) {
		for _, already := range firstTags {
			if tag == already {
				t.Fatalf("tag %s was served on both pages after a mid-page insert", tag)
			}
		}
	}
	if second.Individual[0].AnimalID != "page-tag-010" {
		t.Fatalf("second page starts at %s, want page-tag-010 — the insert shifted the window",
			second.Individual[0].AnimalID)
	}
}

// Two observations accepted in the SAME second must still page deterministically:
// the tie-break on observation_id is what stops the pair from looping or being
// skipped at a page boundary.
func TestGetLeadershipShedVideosBreaksTiesOnObservationID(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	same := time.Date(2026, 7, 29, 5, 0, 0, 0, time.UTC)
	for i := 0; i < 4; i++ {
		execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_observations (tenant_id, campaign_id, campaign_shed_id, animal_id, scanned_identifier, weight_kg, proof_artifact_id, recorded_by, idempotency_key, accepted_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, NULL, $6, 10.0, $4::uuid, $5::uuid, $7, $8::timestamptz)`,
			repoTenant, repoCampaign, repoAnimalScope, repoAnimalProof, repoOperator,
			fmt.Sprintf("tie-%d", i), fmt.Sprintf("tie:%d", i), same)
	}
	repo := NewRepository(pool, 5*time.Second)

	seen := map[string]bool{}
	cursor := ""
	for pages := 0; pages < 10; pages++ {
		page, err := repo.GetLeadershipShedVideos(ctx, repoTenant, repoCampaign, repoAnimalScope, cursor, 1)
		if err != nil {
			t.Fatalf("page %d: %v", pages, err)
		}
		for _, observation := range page.Individual {
			if seen[observation.ObservationID] {
				t.Fatalf("observation %s served twice — equal accepted_at is not tie-broken", observation.ObservationID)
			}
			seen[observation.ObservationID] = true
		}
		cursor = page.NextIndividualCursor
		if cursor == "" {
			break
		}
	}
	if len(seen) != 4 {
		t.Fatalf("saw %d of 4 same-second observations across one-row pages", len(seen))
	}
}

// The shed read must carry its OWN context. These three fields used to be handed
// to the screen as route args, so a cold deep link rendered a blank eyebrow and
// "Not assigned yet" for a bucket that was in fact assigned.
func TestGetLeadershipShedVideosCarriesParkWeighDateAndOperatorName(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	shed, err := repo.GetLeadershipShedVideos(ctx, repoTenant, repoCampaign, repoAnimalScope, "", 0)
	if err != nil {
		t.Fatalf("read shed: %v", err)
	}
	if shed.ParkName == "" {
		t.Fatalf("park_name is empty; the client would render a blank eyebrow")
	}
	if shed.WeighDate == "" {
		t.Fatalf("weigh_date is empty; it is the task's Asia/Kolkata business date")
	}
	if len(shed.WeighDate) != len("2026-07-29") {
		t.Fatalf("weigh_date=%q, want a business DATE (YYYY-MM-DD), never a timestamp", shed.WeighDate)
	}
	if shed.OperatorUserID == "" {
		t.Fatalf("operator_user_id is empty on an assigned bucket")
	}
}

// The task DETAIL bucket list is its own keyset page. Before this read existed,
// the detail rendered whatever the task LIST had embedded — every bucket of every
// campaign on the list page.
func TestListCampaignShedsPagesBucketsAndReportsWholeTaskTotal(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	first, err := repo.ListCampaignSheds(ctx, repoTenant, repoCampaign, "", "", 1)
	if err != nil {
		t.Fatalf("first bucket page: %v", err)
	}
	if len(first.Items) != 1 {
		t.Fatalf("first bucket page size=%d, want 1", len(first.Items))
	}
	// The fixture campaign holds two buckets, so a one-row page must report a
	// total of 2 — the header count is whole-task, never the page length.
	if first.TotalCount != 2 {
		t.Fatalf("total_count=%d, want the whole-task count of 2", first.TotalCount)
	}
	if first.NextCursor == "" {
		t.Fatalf("first bucket page returned no cursor with a second bucket outstanding")
	}

	second, err := repo.ListCampaignSheds(ctx, repoTenant, repoCampaign, "", first.NextCursor, 1)
	if err != nil {
		t.Fatalf("second bucket page: %v", err)
	}
	if len(second.Items) != 1 {
		t.Fatalf("second bucket page size=%d, want 1", len(second.Items))
	}
	if second.Items[0].CampaignShedID == first.Items[0].CampaignShedID {
		t.Fatalf("second page repeated bucket %s", second.Items[0].CampaignShedID)
	}
	if second.NextCursor != "" {
		t.Fatalf("second bucket page cursor=%q, want empty at exhaustion", second.NextCursor)
	}
}

// The planner's SHED page is a keyset page over the sheds of ONE park. The park
// picker is a separate, unpaged read -- see
// TestPlannerCatalogReturnsEveryParkRegardlessOfTheBiggestParksShedCount.
func TestPlannerParkBucketsPagesShedsAndTerminates(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	// Walk one park's buckets a row at a time. Every shed must be seen exactly
	// once and the walk must terminate.
	seenSheds := map[string]bool{}
	cursor := ""
	pages := 0
	for ; pages < 500; pages++ {
		page, err := repo.PlannerParkBuckets(ctx, repoTenant, repoPark, lsFixtureDay, "", cursor, 1)
		if err != nil {
			t.Fatalf("bucket page %d: %v", pages, err)
		}
		if len(page.Sheds) > 1 {
			t.Fatalf("bucket page %d returned %d shed rows for a limit of 1", pages, len(page.Sheds))
		}
		if page.ParkID != repoPark {
			t.Fatalf("bucket page %d reports park %s, want the requested park %s", pages, page.ParkID, repoPark)
		}
		for _, shed := range page.Sheds {
			if seenSheds[shed.LocationID] {
				t.Fatalf("shed %s served twice across bucket pages", shed.LocationID)
			}
			seenSheds[shed.LocationID] = true
		}
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
	}
	if cursor != "" {
		t.Fatalf("bucket walk did not terminate within %d pages", pages)
	}
	if len(seenSheds) == 0 {
		t.Fatalf("bucket walk saw no sheds at all")
	}

	// The same sheds must come back when the park is drained at the maximum page
	// size, so paging changed the transport and not the answer.
	drained := drainParkBuckets(t, ctx, repo, repoPark, lsFixtureDay, "")
	if len(drained) != len(seenSheds) {
		t.Fatalf("one-row walk saw %d sheds, max-page drain saw %d", len(seenSheds), len(drained))
	}

	// Every returned shed must belong to the requested park -- the old flattened
	// cursor could carry a page into the next park entirely.
	for shedID := range drained {
		var parent string
		if err := pool.QueryRow(ctx, `SELECT parent_location_id::text FROM locations WHERE location_id=$1::uuid`, shedID).Scan(&parent); err != nil {
			t.Fatalf("read parent of shed %s: %v", shedID, err)
		}
		if parent != repoPark {
			t.Fatalf("shed %s belongs to park %s but was served on park %s's bucket page", shedID, parent, repoPark)
		}
	}
}

// A cursor must stay stable when a row lands MID-PAGE: a shed inserted before the
// boundary after page one was read must not push an already-served shed onto page
// two, and a shed inserted after the boundary must simply appear later.
func TestPlannerParkBucketsCursorIsStableUnderAMidPageInsert(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	first, err := repo.PlannerParkBuckets(ctx, repoTenant, repoPark, lsFixtureDay, "", "", 5)
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(first.Sheds) != 5 || first.NextCursor == "" {
		t.Fatalf("first page size=%d cursor=%q, want a full 5-row page with a cursor", len(first.Sheds), first.NextCursor)
	}
	served := map[string]bool{}
	for _, shed := range first.Sheds {
		served[shed.LocationID] = true
	}

	// A shed whose sort key sorts BEFORE the boundary (display_order 0, name '')
	// -- i.e. squarely inside the page already served.
	behind := lcpUUID(31001)
	lsInsertShed(t, ctx, pool, behind, repoPark, "", 0)
	// A shed whose sort key sorts AFTER every existing row.
	ahead := lcpUUID(31002)
	lsInsertShed(t, ctx, pool, ahead, repoPark, "zzzz-after-everything", 9999)

	second, err := repo.PlannerParkBuckets(ctx, repoTenant, repoPark, lsFixtureDay, "", first.NextCursor, 5)
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	for _, shed := range second.Sheds {
		if served[shed.LocationID] {
			t.Fatalf("shed %s repeated on page two after a mid-page insert", shed.LocationID)
		}
		if shed.LocationID == behind {
			t.Fatalf("shed inserted BEHIND the cursor was served again on page two")
		}
	}

	// Draining from the cursor must still reach the row inserted ahead of it.
	rest := map[string]bool{}
	cursor := first.NextCursor
	for pages := 0; pages < 500 && cursor != ""; pages++ {
		page, err := repo.PlannerParkBuckets(ctx, repoTenant, repoPark, lsFixtureDay, "", cursor, 5)
		if err != nil {
			t.Fatalf("drain page %d: %v", pages, err)
		}
		for _, shed := range page.Sheds {
			rest[shed.LocationID] = true
		}
		cursor = page.NextCursor
	}
	if !rest[ahead] {
		t.Fatalf("shed inserted AHEAD of the cursor never appeared on a later page")
	}
}

// drainParkBuckets walks every bucket page of ONE park at the maximum page size and
// returns the merged shed set keyed by location_id.
func drainParkBuckets(t *testing.T, ctx context.Context, repo *Repository, parkID, weighDate, excludeCampaignID string) map[string][]domain.PlannerShed {
	t.Helper()
	out := map[string][]domain.PlannerShed{}
	cursor := ""
	for pages := 0; pages < 500; pages++ {
		page, err := repo.PlannerParkBuckets(ctx, repoTenant, parkID, weighDate, excludeCampaignID, cursor, domain.MaxPlannerBucketPageSize)
		if err != nil {
			t.Fatalf("drain bucket page %d: %v", pages, err)
		}
		for _, shed := range page.Sheds {
			out[shed.LocationID] = append(out[shed.LocationID], shed)
		}
		cursor = page.NextCursor
		if cursor == "" {
			return out
		}
	}
	t.Fatalf("bucket drain did not terminate")
	return nil
}

// drainAllParkBuckets walks EVERY park in the catalog and merges their bucket pages,
// so a content assertion covers the whole tenant rather than one park.
func drainAllParkBuckets(t *testing.T, ctx context.Context, repo *Repository, weighDate, excludeCampaignID string) map[string][]domain.PlannerShed {
	t.Helper()
	catalog, err := repo.PlannerCatalog(ctx, repoTenant, weighDate)
	if err != nil {
		t.Fatalf("planner catalog: %v", err)
	}
	out := map[string][]domain.PlannerShed{}
	for _, park := range catalog.Parks {
		for shedID, sheds := range drainParkBuckets(t, ctx, repo, park.ParkID, weighDate, excludeCampaignID) {
			out[shedID] = append(out[shedID], sheds...)
		}
	}
	return out
}

// Plan proof for the two leadership keyset reads.
//
// A keyset page whose ORDER BY is not index-backed still reads and sorts the
// whole bucket/task before LIMIT throws most of it away — that moves the
// over-fetch from the wire into the database instead of removing it. These
// assertions pin the actual chosen plan: the expected index is used, and there is
// no Sort node above it.
func TestLeadershipKeysetReadsAreIndexOrdered(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	seedLeadershipObservations(t, ctx, pool, time.Date(2026, 7, 29, 4, 0, 0, 0, time.UTC), 60)
	// The planner's bucket page is only worth an index at a realistic shed count: at
	// the handful of rows the baseline fixture carries, a seq scan genuinely IS the
	// cheaper plan and asserting otherwise would prove nothing. Seed a real park's
	// worth of sheds and refresh planner statistics so the chosen plan is honest --
	// enable_seqscan=off is still deliberately NOT used anywhere in this test.
	for i := 0; i < 400; i++ {
		park := repoPark
		if i%2 == 1 {
			park = lsParkCPT
		}
		lsInsertShed(t, ctx, pool, lcpUUID(34000+i), park, fmt.Sprintf("Plan Shed %04d", i), i)
	}
	execWeighingTestSQL(t, ctx, pool, `ANALYZE locations`)

	// indexes lists the acceptable index choices for the read. It is a SET, not a
	// wildcard: every entry must have the query's equality columns first and then
	// exactly the ORDER BY tuple, so whichever the planner picks, the keyset is
	// walked in index order and never sorted.
	cases := []struct {
		name    string
		indexes []string
		sql     string
		args    []any
	}{
		{
			name:    "shed evidence page",
			indexes: []string{"weighing_observations_shed_keyset_idx"},
			sql: `EXPLAIN SELECT observation_id, accepted_at
FROM weighing_observations
WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid AND campaign_shed_id=$3::uuid
  AND (accepted_at, observation_id) > ($4::timestamptz, $5::uuid)
ORDER BY accepted_at, observation_id
LIMIT 21`,
			args: []any{repoTenant, repoCampaign, repoAnimalScope,
				time.Date(2026, 7, 29, 4, 0, 5, 0, time.UTC), "00000000-0000-0000-0000-000000000000"},
		},
		{
			name:    "task detail bucket page",
			indexes: []string{"weighing_campaign_sheds_detail_keyset_idx"},
			sql: `EXPLAIN SELECT campaign_shed_id, display_name
FROM weighing_campaign_sheds cs
WHERE cs.tenant_id=$1::uuid AND cs.campaign_id=$2::uuid
  AND (cs.display_name, cs.campaign_shed_id) > ($3::text, $4::uuid)
ORDER BY cs.display_name, cs.campaign_shed_id
LIMIT 21`,
			args: []any{repoTenant, repoCampaign, "A", "00000000-0000-0000-0000-000000000000"},
		},
		{
			// The planner's per-park bucket page. The three equality columns and
			// then the exact keyset tuple are the leading columns of this index,
			// so the LIMIT stops the walk with no Sort above it.
			name: "planner park bucket page",
			// Both committed indexes lead with (tenant_id, parent_location_id, ...)
			// and end with (display_order, name, location_id), so either walks this
			// keyset in order. locations_tenant_parent_order_idx also has
			// location_type as an equality column, so the planner usually prefers it.
			indexes: []string{"locations_tenant_parent_order_idx", "locations_tenant_parent_status_idx"},
			sql: `EXPLAIN SELECT shed.location_id, shed.display_order, shed.name
FROM locations shed
WHERE shed.tenant_id=$1::uuid
  AND shed.parent_location_id=$2::uuid
  AND shed.status='active'
  AND shed.location_type='shed'
  AND shed.retired_at IS NULL
  AND (shed.display_order, shed.name, shed.location_id) > ($3::int, $4::text, $5::uuid)
ORDER BY shed.display_order, shed.name, shed.location_id
LIMIT 21`,
			args: []any{repoTenant, repoPark, 0, "A", "00000000-0000-0000-0000-000000000000"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// enable_seqscan=off is deliberately NOT set: a plan that only appears
			// when the planner is forced is not evidence.
			rows, err := pool.Query(ctx, tc.sql, tc.args...)
			if err != nil {
				t.Fatalf("explain: %v", err)
			}
			defer rows.Close()
			plan := ""
			for rows.Next() {
				var line string
				if err := rows.Scan(&line); err != nil {
					t.Fatalf("scan plan: %v", err)
				}
				plan += line + "\n"
			}
			if err := rows.Err(); err != nil {
				t.Fatalf("plan rows: %v", err)
			}
			used := false
			for _, index := range tc.indexes {
				if strings.Contains(plan, index) {
					used = true
					break
				}
			}
			if !used {
				t.Fatalf("plan uses none of %v:\n%s", tc.indexes, plan)
			}
			if strings.Contains(plan, "Sort") {
				t.Fatalf("plan sorts instead of walking one of %v in order:\n%s", tc.indexes, plan)
			}
		})
	}
}

// The GALLERY read must be a page of BUCKETS, bounded on both axes: at most one
// page of buckets, and at most one page of evidence per bucket. Before it existed
// the client built this page by expanding a task page into every embedded bucket
// and calling the single-bucket read once per bucket.
func TestListLeadershipShedsPagesBucketsAndBoundsEvidencePerBucket(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	base := time.Date(2026, 7, 29, 4, 0, 0, 0, time.UTC)
	seedLeadershipObservations(t, ctx, pool, base, 25)
	repo := NewRepository(pool, 5*time.Second)

	page, err := repo.ListLeadershipSheds(ctx, repoTenant, "", 0, 0)
	if err != nil {
		t.Fatalf("gallery page: %v", err)
	}
	if len(page.Items) == 0 {
		t.Fatalf("gallery page returned no buckets")
	}
	if len(page.Items) > domain.LeadershipShedPageSize {
		t.Fatalf("gallery page size=%d, want at most %d", len(page.Items), domain.LeadershipShedPageSize)
	}
	var individual *domain.LeadershipShedVideos
	for i := range page.Items {
		if len(page.Items[i].Individual) > domain.LeadershipShedVideosPageSize {
			t.Fatalf("bucket %s carried %d observations, want at most one %d-row page",
				page.Items[i].CampaignShedID, len(page.Items[i].Individual), domain.LeadershipShedVideosPageSize)
		}
		if page.Items[i].CampaignShedID == repoAnimalScope {
			individual = &page.Items[i]
		}
	}
	if individual == nil {
		t.Fatalf("gallery page did not include the individual-animal bucket")
	}
	if len(individual.Individual) != domain.LeadershipShedVideosPageSize {
		t.Fatalf("individual bucket carried %d observations, want a full %d-row page",
			len(individual.Individual), domain.LeadershipShedVideosPageSize)
	}
	if individual.NextIndividualCursor == "" {
		t.Fatalf("bucket with 25 observations reported no evidence cursor")
	}
	// The bucket states its OWN context, so a gallery row renders without route args.
	if individual.ParkName == "" || individual.WeighDate == "" {
		t.Fatalf("bucket context missing: park=%q weigh_date=%q", individual.ParkName, individual.WeighDate)
	}
	// The per-bucket cursor is the same shape the single-bucket read hands out.
	next, err := repo.GetLeadershipShedVideos(ctx, repoTenant, individual.CampaignID, individual.CampaignShedID, individual.NextIndividualCursor, 0)
	if err != nil {
		t.Fatalf("continuing the gallery bucket cursor: %v", err)
	}
	if len(next.Individual) != 5 {
		t.Fatalf("continuation page size=%d, want the remaining 5", len(next.Individual))
	}
}

// Walking the gallery one bucket at a time must cover every bucket exactly once
// and terminate.
func TestListLeadershipShedsWalksEveryBucketOnceAndTerminates(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	whole, err := repo.ListLeadershipSheds(ctx, repoTenant, "", 0, 0)
	if err != nil {
		t.Fatalf("whole page: %v", err)
	}
	seen := map[string]bool{}
	cursor := ""
	for i := 0; i < len(whole.Items)+2; i++ {
		page, err := repo.ListLeadershipSheds(ctx, repoTenant, cursor, 1, 0)
		if err != nil {
			t.Fatalf("bucket page %d: %v", i, err)
		}
		for _, item := range page.Items {
			if seen[item.CampaignShedID] {
				t.Fatalf("bucket %s repeated across pages — the keyset is not exclusive", item.CampaignShedID)
			}
			seen[item.CampaignShedID] = true
		}
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
	}
	if cursor != "" {
		t.Fatalf("gallery walk did not terminate")
	}
	if len(seen) != len(whole.Items) {
		t.Fatalf("gallery walk covered %d buckets, want %d", len(seen), len(whole.Items))
	}
}

// lsInsertShed adds one active shed to a park with an explicit sort position, so a
// test can place a row before or after a known cursor boundary.
func lsInsertShed(t *testing.T, ctx context.Context, pool *pgxpool.Pool, shedID, parkID, name string, displayOrder int) {
	t.Helper()
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, name, parent_location_id, status, display_order)
VALUES ($1::uuid, $2::uuid, 'shed', $3, $4::uuid, 'active', $5)
ON CONFLICT (location_id) DO UPDATE SET name=EXCLUDED.name, display_order=EXCLUDED.display_order`,
		shedID, repoTenant, name, parkID, displayOrder)
}
