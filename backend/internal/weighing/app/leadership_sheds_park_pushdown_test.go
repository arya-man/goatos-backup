package app

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// Regression test for the leadership gallery's page-shrink defect.
//
// The park filter used to run on the page the repository had already cut. The keyset walks every
// park in the tenant in task order, so a park-scoped monitor whose parks happen to sort later got
// a page made entirely of parks they may not see -- and after filtering, an EMPTY page carrying a
// next cursor. Empty reads as "no weighing evidence", and no cursor value could reach the buckets
// that were in fact theirs. Pagination has to happen over already-authorized rows, which means the
// park set belongs in the query.

// pagingLeadershipRepo is a fakeRepo that walks a fixed, ORDERED bucket list the way the real
// keyset does: it applies the park restriction first and only then cuts the page. A fake that cut
// the page first would encode the very bug under test.
type pagingLeadershipRepo struct {
	fakeRepo
	rows      []domain.LeadershipShedVideos
	parkByID  map[string]string
	sawParkID []string
}

func (r *pagingLeadershipRepo) ListLeadershipSheds(_ context.Context, _ string, parkIDs []string, _ string, limit, _ int) (domain.LeadershipShedPage, error) {
	r.sawParkID = parkIDs
	allowed := map[string]struct{}{}
	for _, parkID := range parkIDs {
		allowed[parkID] = struct{}{}
	}
	items := make([]domain.LeadershipShedVideos, 0, limit)
	for _, row := range r.rows {
		if len(parkIDs) > 0 {
			if _, ok := allowed[r.parkByID[row.CampaignID]]; !ok {
				continue
			}
		}
		if len(items) == limit {
			break
		}
		items = append(items, row)
	}
	return domain.LeadershipShedPage{Items: items}, nil
}

func TestListLeadershipShedsPagesOverAuthorizedParksOnly(t *testing.T) {
	// Park A sorts FIRST and owns enough buckets to fill the requested page on its own; the
	// actor's own park B is entirely behind them. This is the arrangement that produced an
	// empty page for a monitor with real evidence to look at.
	parkByCampaign := map[string]string{
		"30000000-0000-4000-8000-0000000001a1": securityParkA,
		"30000000-0000-4000-8000-0000000001a2": securityParkA,
		"30000000-0000-4000-8000-0000000001b1": securityParkB,
		"30000000-0000-4000-8000-0000000001b2": securityParkB,
	}
	repo := &pagingLeadershipRepo{
		parkByID: parkByCampaign,
		rows: []domain.LeadershipShedVideos{
			{CampaignID: "30000000-0000-4000-8000-0000000001a1", CampaignShedID: securityShed},
			{CampaignID: "30000000-0000-4000-8000-0000000001a2", CampaignShedID: securityShed},
			{CampaignID: "30000000-0000-4000-8000-0000000001b1", CampaignShedID: securityShed},
			{CampaignID: "30000000-0000-4000-8000-0000000001b2", CampaignShedID: securityShed},
		},
	}
	service := NewService(repo)

	page, err := service.ListLeadershipSheds(escalationContext(), escalationActor(), "", 2)
	if err != nil {
		t.Fatalf("ListLeadershipSheds err = %v, want nil", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("page has %d buckets, want a FULL page of 2 -- an authorized monitor's page must not be shrunk by another park's rows (got %#v)", len(page.Items), page.Items)
	}
	for _, item := range page.Items {
		if parkByCampaign[item.CampaignID] != securityParkB {
			t.Fatalf("PRIVILEGE ESCALATION: page carries bucket %#v from a park the actor is not authorized for", item)
		}
	}
	if len(repo.sawParkID) != 1 || repo.sawParkID[0] != securityParkB {
		t.Fatalf("repository was asked for parks %v, want exactly [%s] -- the park set must reach the QUERY, not be applied to its result", repo.sawParkID, securityParkB)
	}
}

// TestListLeadershipShedsUnrestrictedForTenantWideMonitor guards the other arm: an empty park set
// is the repository's "no restriction" input, so a tenant-wide monitor must be handed nil rather
// than an empty slice, which would read as "authorized for no park at all".
func TestListLeadershipShedsUnrestrictedForTenantWideMonitor(t *testing.T) {
	repo := &pagingLeadershipRepo{
		parkByID: map[string]string{"30000000-0000-4000-8000-0000000001a1": securityParkA},
		rows: []domain.LeadershipShedVideos{
			{CampaignID: "30000000-0000-4000-8000-0000000001a1", CampaignShedID: securityShed},
		},
	}
	service := NewService(repo)
	ctx := httpmiddleware.WithAuthGrants(context.Background(), []permissions.ActiveGrant{
		{Role: permissions.RoleGrowthDirector, ScopeType: "tenant", ScopeID: securityTenant},
	})

	page, err := service.ListLeadershipSheds(ctx, escalationActor(), "", 2)
	if err != nil {
		t.Fatalf("ListLeadershipSheds err = %v, want nil", err)
	}
	if len(repo.sawParkID) != 0 {
		t.Fatalf("tenant-wide monitor was scoped to parks %v, want no restriction", repo.sawParkID)
	}
	if len(page.Items) != 1 {
		t.Fatalf("tenant-wide monitor saw %d buckets, want the whole tenant's page", len(page.Items))
	}
}

func (r *pagingLeadershipRepo) ListParks(context.Context, string) ([]domain.WeighingPark, error) {
	return nil, nil
}
