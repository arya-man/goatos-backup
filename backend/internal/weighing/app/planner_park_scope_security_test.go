package app

import (
	"context"
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// These cover the SECOND half of the cross-park weighing hole. The first half (close/reopen/
// abandon and the leadership gallery) is covered by park_capability_security_test.go; this
// file covers the planner and campaign surfaces, which ran a park-BLIND role check only:
// RolesAuthorize answers "do I hold WeighingPlan somewhere", which a park-scoped planner
// passes for every park in the tenant.
//
// The grants below are deliberately the mildest possible case -- ONE park-scoped grant that
// genuinely carries the capability. No unrelated second grant is needed: naming another park's
// id was enough on its own.

const (
	plannerScopeParkMine   = securityParkA
	plannerScopeParkOthers = securityParkB
)

func plannerScopedContext() context.Context {
	return httpmiddleware.WithAuthGrants(context.Background(), []permissions.ActiveGrant{
		{Role: permissions.RoleGrowthDirector, ScopeType: "park", ScopeID: plannerScopeParkMine},
	})
}

func plannerScopedActor() domain.Actor {
	return domain.Actor{
		TenantID: securityTenant,
		UserID:   securityActorID,
		Roles:    []string{permissions.RoleGrowthDirector},
	}
}

// plannerCatalogRepo answers with BOTH parks, exactly like the real query does -- the
// repository has no park filter, so the whole tenant comes back and the service must narrow it.
type plannerCatalogRepo struct {
	fakeRepo
	bucketsCalledWithPark string
}

func (r *plannerCatalogRepo) PlannerCatalog(context.Context, string, string) (domain.PlannerCatalog, error) {
	return domain.PlannerCatalog{Parks: []domain.PlannerPark{
		{ParkID: plannerScopeParkMine, Name: "mine"},
		{ParkID: plannerScopeParkOthers, Name: "not mine"},
	}}, nil
}

func (r *plannerCatalogRepo) PlannerParkBuckets(_ context.Context, _, parkID, _, _, _ string, _ int) (domain.PlannerParkBuckets, error) {
	r.bucketsCalledWithPark = parkID
	return domain.PlannerParkBuckets{}, nil
}

func TestPlannerCatalogHidesParksTheActorHasNoAuthorityIn(t *testing.T) {
	repo := &plannerCatalogRepo{}
	svc := NewService(repo)

	catalog, err := svc.PlannerCatalog(plannerScopedContext(), plannerScopedActor(), "2026-08-10")
	if err != nil {
		t.Fatalf("planner catalog: %v", err)
	}
	if len(catalog.Parks) != 1 || catalog.Parks[0].ParkID != plannerScopeParkMine {
		// A leaked park is not cosmetic: each option carries that park's kid/shed counts and
		// its existing campaign, so the numbers leak before anything is selected.
		t.Fatalf("catalog parks = %+v, want only %s", catalog.Parks, plannerScopeParkMine)
	}
}

func TestPlannerParkBucketsRefusesAnotherParksSheds(t *testing.T) {
	repo := &plannerCatalogRepo{}
	svc := NewService(repo)

	_, err := svc.PlannerParkBuckets(plannerScopedContext(), plannerScopedActor(),
		plannerScopeParkOthers, "2026-08-10", "", "", 20)
	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("buckets for another park err = %v, want ErrNotFound", err)
	}
	if repo.bucketsCalledWithPark != "" {
		t.Fatalf("repository was queried for park %s despite the refusal", repo.bucketsCalledWithPark)
	}

	if _, err := svc.PlannerParkBuckets(plannerScopedContext(), plannerScopedActor(),
		plannerScopeParkMine, "2026-08-10", "", "", 20); err != nil {
		t.Fatalf("buckets for the actor's OWN park must still work, got %v", err)
	}
}

func TestCreateCampaignRefusesAnotherParksCampaign(t *testing.T) {
	svc := NewService(&plannerCatalogRepo{})

	// WeighingPlan is CEO-only (see permissions.rolePermissions), so the create/update/publish
	// surfaces need a CEO actor to get PAST the flat role gate and reach the park check that is
	// actually under test. Growth Director, used by the read tests above, carries
	// WeighingMonitor but not WeighingPlan.
	ctx := httpmiddleware.WithAuthGrants(context.Background(), []permissions.ActiveGrant{
		{Role: permissions.RoleCEOInternal, ScopeType: "park", ScopeID: plannerScopeParkMine},
	})
	actor := domain.Actor{
		TenantID: securityTenant,
		UserID:   securityActorID,
		Roles:    []string{permissions.RoleCEOInternal},
	}

	_, err := svc.CreateCampaign(ctx, actor, domain.CreateCampaign{
		ParkID:            plannerScopeParkOthers,
		PeriodStartDate:   "2026-08-10",
		PeriodEndDate:     "2026-08-10",
		StartBusinessDate: "2026-08-10",
		OperatorUserID:    securityActorID,
		IdempotencyKey:    "planner-park-scope-test",
		PlannedCapPerDay:  100,
		Sheds: []domain.CreateCampaignShed{{
			LocationID:       securityShed,
			LocationType:     "shed",
			DisplayName:      "Shed 1",
			WeighingCategory: "individual_animal",
		}},
	})
	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("create in another park err = %v, want ErrNotFound", err)
	}
}

// listScopeRepo records the park the repository was asked for, so the test can prove the
// service clamped it rather than merely returning an error.
type listScopeRepo struct {
	fakeRepo
	listedPark string
	listed     bool
}

func (r *listScopeRepo) ListCampaigns(_ context.Context, _, parkID, _ string, _ int) (domain.CampaignPage, error) {
	r.listedPark = parkID
	r.listed = true
	return domain.CampaignPage{}, nil
}

func TestListCampaignsClampsTheParkFilterToTheActorsAuthority(t *testing.T) {
	t.Run("another park is refused", func(t *testing.T) {
		repo := &listScopeRepo{}
		svc := NewService(repo)
		_, err := svc.ListCampaigns(plannerScopedContext(), plannerScopedActor(),
			domain.CampaignListScopeAll, plannerScopeParkOthers, "", 20)
		if !errors.Is(err, ports.ErrForbidden) {
			t.Fatalf("list for another park err = %v, want ErrForbidden", err)
		}
		if repo.listed {
			t.Fatal("repository was queried despite the refusal")
		}
	})

	t.Run("an omitted park defaults to the actor's own park, not all parks", func(t *testing.T) {
		repo := &listScopeRepo{}
		svc := NewService(repo)
		if _, err := svc.ListCampaigns(plannerScopedContext(), plannerScopedActor(),
			domain.CampaignListScopeAll, "", "", 20); err != nil {
			t.Fatalf("list with no park: %v", err)
		}
		// An empty park filter reaches the repository as "every campaign in the tenant".
		if repo.listedPark != plannerScopeParkMine {
			t.Fatalf("repository park filter = %q, want %q", repo.listedPark, plannerScopeParkMine)
		}
	})
}
