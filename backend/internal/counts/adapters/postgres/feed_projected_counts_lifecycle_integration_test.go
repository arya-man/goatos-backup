package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The animal that dies stops being fed.
//
// This is the coupling the farm states in plain words -- mark an animal dead and the feed must
// recalculate -- and it is NOT a domain event: nothing publishes "feed, recount". The live head
// count the feed sheet is built from is a QUERY over goats pinned to the live herd
// (lifecycle_status, defaulted to 'alive' in ProjectedShedCountsForFeed). A coupling carried by a
// predicate has nowhere to be registered, so the domain-event registry cannot describe it and no
// consumer test can cover it.
//
// Before this test every fixture in the feed-projection suite inserted 'alive' animals only, so
// the predicate could be deleted and the whole suite stayed green: a dead animal would have gone
// on being counted, and its pen would have gone on being fed for it, indefinitely and invisibly.
// Dropping the filter is exactly what the operator would never see -- the number stays plausible.
//
// The fixtures insert only INPUT facts (goats, with the lifecycle the herd register would hold).
// Every number under assertion is produced by the production query.
func TestFeedProjectionFeedsTheLiveHerdOnlyAndDropsAnimalsThatLeftIt(t *testing.T) {
	ctx := context.Background()
	repo, pool := newFeedProjRepo(t, ctx)

	// Six animals are alive in the pen and are fed.
	for i := 0; i < 6; i++ {
		insertFeedProjGoat(t, ctx, pool, 900+i, "Beetal", "female", "K1", feedProjShedB)
	}

	// Four more stood in the same pen and have left the live herd: two died, one was sold, one
	// was culled. Each is a distinct exit the register records, and none of them eats.
	insertFeedProjExitedGoat(t, ctx, pool, 920, "Beetal", "female", "K1", feedProjShedB, "dead", "died")
	insertFeedProjExitedGoat(t, ctx, pool, 921, "Beetal", "female", "K1", feedProjShedB, "dead", "died")
	insertFeedProjExitedGoat(t, ctx, pool, 922, "Beetal", "female", "K1", feedProjShedB, "sold", "sold")
	insertFeedProjExitedGoat(t, ctx, pool, 923, "Beetal", "female", "K1", feedProjShedB, "culled", "culled")

	got, err := repo.ProjectedShedCountsForFeed(ctx, feedProjQuery(feedProjDay(2026, time.July, 10)))
	if err != nil {
		t.Fatalf("ProjectedShedCountsForFeed: %v", err)
	}

	row := findFeedProjRow(t, got, feedProjShedB, "Beetal", "K1", "female")
	if row.CurrentHeadCount != 6 {
		t.Fatalf("current_head_count=%d, want 6 -- the four animals that left the herd (2 dead, "+
			"1 sold, 1 culled) must not be fed", row.CurrentHeadCount)
	}
	if row.ProjectedHeadCount != 6 {
		t.Errorf("projected_head_count=%d, want 6 -- with no pending movement the projection is the "+
			"live herd", row.ProjectedHeadCount)
	}
}

// insertFeedProjExitedGoat seeds one animal that has LEFT the live herd, the way the register
// holds it after the exit is approved: a terminal lifecycle_status, the coded reason, and the exit
// stamp goats_exited_lifecycle_check requires. Its pen is still recorded -- an exited animal keeps
// the last pen it stood in -- which is precisely why the feed read must exclude it by lifecycle
// rather than by location.
func insertFeedProjExitedGoat(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	n int,
	breed, sex, stage string,
	shedID string,
	lifecycleStatus, exitReason string,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO goats (
  goat_id, tenant_id, display_id, species, breed, sex, lifecycle_status,
  age_band, custodian_party_id, park_id, shed_id, management_stage,
  exit_reason, exited_at
) VALUES (
  $1::uuid, $2::uuid, $3, 'goat', $4, $5, $9, 'adult',
  '00000000-0000-4000-8000-000000001001'::uuid, $6::uuid, $7::uuid, $8,
  $10, now()
)`,
		feedProjGoatUUID(n), countsTenant, feedProjDisplayID(n), breed, sex,
		feedProjPark, shedID, stage, lifecycleStatus, exitReason); err != nil {
		t.Fatalf("seed exited feed projection goat %d (%s/%s): %v", n, lifecycleStatus, exitReason, err)
	}
}
