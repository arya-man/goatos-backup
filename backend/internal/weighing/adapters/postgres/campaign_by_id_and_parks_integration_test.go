package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// TestCampaignByIDResolvesATaskTheKeysetPageCannotReach is the defect this read exists for. The
// task list is keyset-paged with no id filter, so a task that sits past the first pages could
// only be reached by walking the keyset -- which a cold notification tap cannot do. The proof
// here is deliberately positional: the target task is seeded so it is NOT on the first page, the
// test asserts the page really does not carry it, and then resolves it by id in one read.
func TestCampaignByIDResolvesATaskTheKeysetPageCannotReach(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	grantOperatorParkScope(t, ctx, pool)
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	// The list orders by period_start_date DESC, so the OLDEST task sorts last. Five newer tasks
	// then guarantee it is off a two-row page.
	target := lcpUUID(15001)
	lcpInsertCampaign(t, ctx, pool, target, lcpParkCBE, "2026-03-02", domain.StatusPublished, repoOperator)
	lcpInsertBucket(t, ctx, pool, lcpUUID(15011), target, lcpShedOne, domain.CategoryIndividualAnimal, repoOperator, 3, "pending")
	for n := 0; n < 5; n++ {
		newer := lcpUUID(15100 + n)
		lcpInsertCampaign(t, ctx, pool, newer, lcpParkCBE, weighDateAfter("2026-04-06", n), domain.StatusPublished, repoOperator)
		lcpInsertBucket(t, ctx, pool, lcpUUID(15200+n), newer, lcpShedTwo, domain.CategoryIndividualAnimal, repoOperator, 1, "pending")
	}

	page, err := repo.ListCampaigns(ctx, repoTenant, "", "", 2)
	if err != nil {
		t.Fatalf("list first page: %v", err)
	}
	for _, item := range page.Items {
		if item.CampaignID == target {
			t.Fatalf("fixture drift: the target task is on the first page, so this test no longer proves the id read reaches past it")
		}
	}

	campaign, err := repo.CampaignByID(ctx, repoTenant, target, "")
	if err != nil {
		t.Fatalf("CampaignByID: %v", err)
	}
	if campaign.CampaignID != target {
		t.Fatalf("CampaignByID returned %q, want %q", campaign.CampaignID, target)
	}
	// The id read must carry the SAME hydration the list row does -- park name, buckets and
	// progress. A deep link that resolved a bare header would render a task detail with no park
	// label and no buckets.
	if campaign.ParkName == "" {
		t.Fatalf("CampaignByID returned no park name; the deep-linked task detail has no park label")
	}
	if len(campaign.Sheds) != 1 {
		t.Fatalf("CampaignByID returned %d buckets, want 1", len(campaign.Sheds))
	}
	if campaign.Progress.IndividualExpectedCount != campaign.Sheds[0].ExpectedAnimalCount {
		t.Fatalf("CampaignByID progress=%+v is not hydrated against its own bucket", campaign.Progress)
	}
}

// TestCampaignByIDAppliesTheOperatorPredicate pins the assignee arm. An operator resolving a task
// by id must go through the SAME predicate the operator list uses -- a live bucket assigned to
// them, in a park they are granted -- and must not be able to turn a deep link into a read of
// somebody else's task. A miss is ErrNotFound, indistinguishable from a task that does not exist.
func TestCampaignByIDAppliesTheOperatorPredicate(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	grantOperatorParkScope(t, ctx, pool)
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	shared := lcpUUID(16001)
	lcpInsertCampaign(t, ctx, pool, shared, lcpParkCBE, "2026-05-04", domain.StatusPublished, repoOperator)
	mine := lcpUUID(16011)
	theirs := lcpUUID(16012)
	lcpInsertBucket(t, ctx, pool, mine, shared, lcpShedOne, domain.CategoryIndividualAnimal, repoOperator, 2, "pending")
	lcpInsertBucket(t, ctx, pool, theirs, shared, lcpShedTwo, domain.CategoryIndividualAnimal, repoOtherOp, 5, "pending")

	// Somebody else's task: no bucket of it belongs to repoOperator at all.
	foreign := lcpUUID(16002)
	lcpInsertCampaign(t, ctx, pool, foreign, lcpParkCBE, "2026-05-11", domain.StatusPublished, repoOtherOp)
	lcpInsertBucket(t, ctx, pool, lcpUUID(16021), foreign, lcpShedThree, domain.CategoryIndividualAnimal, repoOtherOp, 4, "pending")

	campaign, err := repo.CampaignByID(ctx, repoTenant, shared, repoOperator)
	if err != nil {
		t.Fatalf("CampaignByID for the assignee: %v", err)
	}
	if len(campaign.Sheds) != 1 || campaign.Sheds[0].CampaignShedID != mine {
		t.Fatalf("CampaignByID buckets=%+v, want only the caller's own bucket %s", campaign.Sheds, mine)
	}

	if _, err := repo.CampaignByID(ctx, repoTenant, foreign, repoOperator); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("CampaignByID for a task the operator owns no bucket on err = %v, want ErrNotFound", err)
	}
	// The same task IS resolvable unfiltered, which is what proves the refusal above came from
	// the operator predicate and not from the task being absent.
	if _, err := repo.CampaignByID(ctx, repoTenant, foreign, ""); err != nil {
		t.Fatalf("CampaignByID unfiltered: %v", err)
	}
}

// TestWeighingParksRestrictsToTheRequestedParksInsideTheQuery pins the park-vocabulary read's two
// arms. A named set restricts; nil is UNRESTRICTED and not "authorized for nothing" -- reading an
// empty set as a filter would answer zero chips to a tenant-wide monitor, which is the failure
// mode that made the client fall back to deriving chips from its loaded rows in the first place.
func TestWeighingParksRestrictsToTheRequestedParksInsideTheQuery(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	all, err := repo.WeighingParks(ctx, repoTenant, nil)
	if err != nil {
		t.Fatalf("WeighingParks unrestricted: %v", err)
	}
	if len(all) < 2 {
		t.Fatalf("WeighingParks unrestricted returned %d parks, want the fixture's parks", len(all))
	}
	for _, park := range all {
		if park.Name == "" {
			t.Fatalf("WeighingParks returned park %s with no name; a chip with no label is not a vocabulary", park.ParkID)
		}
	}

	restricted, err := repo.WeighingParks(ctx, repoTenant, []string{lcpParkCPT})
	if err != nil {
		t.Fatalf("WeighingParks restricted: %v", err)
	}
	if len(restricted) != 1 || restricted[0].ParkID != lcpParkCPT {
		t.Fatalf("WeighingParks restricted = %+v, want only %s", restricted, lcpParkCPT)
	}
}

// TestWeighingParksAsksForOneMoreThanItsCapSoTruncationCannotBeSilent pins the overflow probe.
// The chip row is unpaged, so a plain LIMIT would drop parks with no signal at all -- a filter
// vocabulary that quietly omits options is the exact defect this endpoint was added to remove.
// Proven by DATA, not by reading the SQL: seed past the cap and require a loud failure.
func TestWeighingParksAsksForOneMoreThanItsCapSoTruncationCannotBeSilent(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	existing, err := repo.WeighingParks(ctx, repoTenant, nil)
	if err != nil {
		t.Fatalf("WeighingParks baseline: %v", err)
	}
	for n := len(existing); n <= domain.MaxPlannerParks; n++ {
		execWeighingTestSQL(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, name, status, display_order)
VALUES ($1::uuid, $2::uuid, 'park', $3, 'active', $4)`,
			lcpUUID(17000+n), repoTenant, fmt.Sprintf("Overflow Park %03d", n), 900+n)
	}

	if _, err := repo.WeighingParks(ctx, repoTenant, nil); err == nil {
		t.Fatalf("WeighingParks silently truncated past its %d-park cap; an omitted chip is unrecoverable for the client", domain.MaxPlannerParks)
	}
}

// weighDateAfter shifts a business date by whole days, so the fixture can lay tasks out along the
// list's keyset order without hand-writing each date.
func weighDateAfter(base string, days int) string {
	parsed, err := time.Parse("2006-01-02", base)
	if err != nil {
		panic(err)
	}
	return parsed.AddDate(0, 0, days).Format("2006-01-02")
}
