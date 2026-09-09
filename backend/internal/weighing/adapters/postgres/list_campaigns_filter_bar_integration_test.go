package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// The phone's task-list FILTER BAR (maintainer request 2026-09-10): a Pending / Completed
// tab, an inclusive business-date window on the weigh date, and one pen. These pin the
// three rules a reader relies on without seeing them:
//
//  1. a Pending read carries still-open work planned BEFORE the window (a delayed task keeps
//     its original date and must not vanish behind "today onwards"), and reads soonest-first;
//  2. the pill counts are whole-filter and follow the window + pen, never the page;
//  3. the pen vocabulary is status-blind, so a pick survives switching tabs.
//
// Postgres integration tests are opt-in: GOATOS_RUN_POSTGRES_TESTS=1.
func TestListCampaignsFilterBarStatusMatrixPaginationAndOneToManyPen(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	grantOperatorParkScope(t, ctx, pool)
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	inWindowOpen := lcpUUID(12001)     // Thu 10 Sep, published: Pending, inside the window
	beforeWindowOpen := lcpUUID(12002) // 5 Sep, in_progress: Pending, CARRIED into the window
	inWindowDone := lcpUUID(12003)     // 12 Sep, completed: Completed, inside the window
	beforeWindowDone := lcpUUID(12004) // 3 Sep, completed: outside the window, never shown
	afterWindowOpen := lcpUUID(12005)  // 20 Sep, published: outside the window, never shown
	lcpInsertCampaign(t, ctx, pool, inWindowOpen, lcpParkCBE, "2026-09-10", domain.StatusPublished, repoOperator)
	lcpInsertCampaign(t, ctx, pool, beforeWindowOpen, lcpParkCBE, "2026-09-05", domain.StatusInProgress, repoOperator)
	lcpInsertCampaign(t, ctx, pool, inWindowDone, lcpParkCBE, "2026-09-12", domain.StatusCompleted, repoOperator)
	lcpInsertCampaign(t, ctx, pool, beforeWindowDone, lcpParkCBE, "2026-09-03", domain.StatusCompleted, repoOperator)
	lcpInsertCampaign(t, ctx, pool, afterWindowOpen, lcpParkCBE, "2026-09-20", domain.StatusPublished, repoOperator)
	// One bucket per task (a campaign holds at most one bucket per pen by unique index).
	lcpInsertBucket(t, ctx, pool, lcpUUID(12011), inWindowOpen, lcpShedOne, domain.CategoryIndividualAnimal, repoOperator, 2, "pending")
	lcpInsertBucket(t, ctx, pool, lcpUUID(12012), beforeWindowOpen, lcpShedThree, domain.CategoryIndividualAnimal, repoOperator, 2, "pending")
	lcpInsertBucket(t, ctx, pool, lcpUUID(12013), inWindowDone, lcpShedTwo, domain.CategoryIndividualAnimal, repoOperator, 2, "completed")
	lcpInsertBucket(t, ctx, pool, lcpUUID(12014), beforeWindowDone, lcpShedFour, domain.CategoryIndividualAnimal, repoOperator, 2, "completed")
	lcpInsertBucket(t, ctx, pool, lcpUUID(12015), afterWindowOpen, lcpShedFive, domain.CategoryIndividualAnimal, repoOperator, 2, "pending")

	window := domain.CampaignListFilter{DateFrom: "2026-09-10", DateTo: "2026-09-17", Today: "2026-09-10"}
	// The shared fixture already seeds ONE open campaign (27 Jul, repoPark): it is carried
	// into every Pending window read and counted as active. Assertions below name it.
	fixtureOpen := lcpFixtureOpenCampaign(t, ctx, pool)

	t.Run("pending reads soonest-first and carries open work planned before the window", func(t *testing.T) {
		filter := window
		filter.Status = domain.CampaignListStatusPending
		page, err := repo.ListCampaigns(ctx, repoTenant, "", filter, "", 100)
		if err != nil {
			t.Fatalf("ListCampaigns pending: %v", err)
		}
		got := lcpIDs(page.Items)
		want := []string{fixtureOpen, beforeWindowOpen, inWindowOpen}
		if !lcpSameOrder(got, want) {
			t.Fatalf("pending rows = %v, want %v (carried July fixture, carried 5 Sep, then 10 Sep)", got, want)
		}
		if page.Counts.Active != 3 || page.Counts.Completed != 1 {
			t.Fatalf("counts = %+v, want active 3 / completed 1 over the window", page.Counts)
		}
	})

	t.Run("completed reads the window exactly and keeps newest-first", func(t *testing.T) {
		filter := window
		filter.Status = domain.CampaignListStatusCompleted
		page, err := repo.ListCampaigns(ctx, repoTenant, "", filter, "", 100)
		if err != nil {
			t.Fatalf("ListCampaigns completed: %v", err)
		}
		got := lcpIDs(page.Items)
		if !lcpSameOrder(got, []string{inWindowDone}) {
			t.Fatalf("completed rows = %v, want only the 12 Sep task (3 Sep is outside the window)", got)
		}
		if page.Counts.Active != 3 || page.Counts.Completed != 1 {
			t.Fatalf("counts on the completed tab = %+v, want the SAME active 3 / completed 1", page.Counts)
		}
	})

	t.Run("the pen vocabulary is windowed, status-blind and counts tasks not buckets", func(t *testing.T) {
		filter := window
		filter.Status = domain.CampaignListStatusCompleted
		page, err := repo.ListCampaigns(ctx, repoTenant, "", filter, "", 100)
		if err != nil {
			t.Fatalf("ListCampaigns: %v", err)
		}
		byShed := map[string]domain.CampaignPenOption{}
		for _, pen := range page.Pens {
			byShed[pen.ShedID] = pen
		}
		for shed, wantCount := range map[string]int{lcpShedOne: 1, lcpShedTwo: 1, lcpShedThree: 1} {
			pen, ok := byShed[shed]
			if !ok {
				t.Fatalf("pen %s missing from the vocabulary %+v", shed, page.Pens)
			}
			if pen.TaskCount != wantCount {
				t.Fatalf("pen %s task_count = %d, want %d", shed, pen.TaskCount, wantCount)
			}
			if pen.Label == "" || pen.ParkName == "" {
				t.Fatalf("pen %s has no label/park: %+v", shed, pen)
			}
		}
		for _, outside := range []string{lcpShedFour, lcpShedFive} {
			if _, ok := byShed[outside]; ok {
				t.Fatalf("pen %s is outside the window and must not be offered", outside)
			}
		}
	})

	t.Run("one pen narrows rows and counts together", func(t *testing.T) {
		filter := window
		filter.Status = domain.CampaignListStatusPending
		filter.PenShedID = lcpShedOne
		page, err := repo.ListCampaigns(ctx, repoTenant, "", filter, "", 100)
		if err != nil {
			t.Fatalf("ListCampaigns pen: %v", err)
		}
		if got := lcpIDs(page.Items); !lcpSameOrder(got, []string{inWindowOpen}) {
			t.Fatalf("pen-narrowed rows = %v, want only %s", got, inWindowOpen)
		}
		if page.Counts.Active != 1 || page.Counts.Completed != 0 {
			t.Fatalf("pen-narrowed counts = %+v, want active 1 / completed 0", page.Counts)
		}
	})

	t.Run("the pending cursor walks forward in the ascending order", func(t *testing.T) {
		filter := window
		filter.Status = domain.CampaignListStatusPending
		first, err := repo.ListCampaigns(ctx, repoTenant, "", filter, "", 1)
		if err != nil {
			t.Fatalf("page 1: %v", err)
		}
		if first.NextCursor == "" || len(first.Items) != 1 || first.Items[0].CampaignID != fixtureOpen {
			t.Fatalf("page 1 = %v cursor=%q, want the oldest carried task and a cursor", lcpIDs(first.Items), first.NextCursor)
		}
		second, err := repo.ListCampaigns(ctx, repoTenant, "", filter, first.NextCursor, 1)
		if err != nil {
			t.Fatalf("page 2: %v", err)
		}
		if len(second.Items) != 1 || second.Items[0].CampaignID != beforeWindowOpen || second.NextCursor == "" {
			t.Fatalf("page 2 = %v cursor=%q, want the 5 Sep task and a cursor", lcpIDs(second.Items), second.NextCursor)
		}
		third, err := repo.ListCampaigns(ctx, repoTenant, "", filter, second.NextCursor, 1)
		if err != nil {
			t.Fatalf("page 3: %v", err)
		}
		if len(third.Items) != 1 || third.Items[0].CampaignID != inWindowOpen || third.NextCursor != "" {
			t.Fatalf("page 3 = %v cursor=%q, want the 10 Sep task and no cursor", lcpIDs(third.Items), third.NextCursor)
		}
	})

	t.Run("a window starting after today carries nothing: a future week is asked about alone", func(t *testing.T) {
		filter := domain.CampaignListFilter{DateFrom: "2026-09-13", DateTo: "2026-09-13", Today: "2026-09-10", Status: domain.CampaignListStatusPending}
		page, err := repo.ListCampaigns(ctx, repoTenant, "", filter, "", 100)
		if err != nil {
			t.Fatalf("ListCampaigns future day: %v", err)
		}
		if len(page.Items) != 0 || page.Counts.Active != 0 {
			t.Fatalf("future single day = %v counts=%+v, want no carried work", lcpIDs(page.Items), page.Counts)
		}
	})

	t.Run("an unfiltered read is the legacy list: newest-first, every task, no pens", func(t *testing.T) {
		page, err := repo.ListCampaigns(ctx, repoTenant, "", domain.CampaignListFilter{Today: "2026-09-10"}, "", 100)
		if err != nil {
			t.Fatalf("ListCampaigns legacy: %v", err)
		}
		got := lcpIDs(page.Items)
		want := []string{afterWindowOpen, inWindowDone, inWindowOpen, beforeWindowOpen, beforeWindowDone, fixtureOpen}
		if !lcpSameOrder(got, want) {
			t.Fatalf("legacy rows = %v, want %v", got, want)
		}
		if len(page.Pens) != 0 {
			t.Fatalf("legacy read offered pens %+v, want none", page.Pens)
		}
		if page.Counts.Active != 4 || page.Counts.Completed != 2 {
			t.Fatalf("legacy counts = %+v, want active 4 / completed 2", page.Counts)
		}
	})
}

func lcpIDs(items []domain.Campaign) []string {
	out := make([]string, 0, len(items))
	for _, c := range items {
		out = append(out, c.CampaignID)
	}
	return out
}

func lcpSameOrder(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// lcpFixtureOpenCampaign is the single open campaign the shared observation fixture seeds,
// read back rather than hardcoded so a fixture change cannot silently invalidate this test.
func lcpFixtureOpenCampaign(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx, `
SELECT campaign_id::text FROM weighing_campaigns
WHERE tenant_id=$1::uuid AND period_start_date < '2026-09-01'::date
  AND status IN ('draft','published','in_progress','delayed')`, repoTenant).Scan(&id); err != nil {
		t.Fatalf("fixture open campaign: %v", err)
	}
	return id
}
