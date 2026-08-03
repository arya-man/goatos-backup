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
// close and the leadership gallery) is covered by park_capability_security_test.go; this
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

// --- Judge-found regressions in the FIRST version of this fix. Each of these passed the
// original park-scope commit and was caught only by adversarial review afterwards.

// campaignShedsRepo routes each campaign to its own park (parkRoutedRepo) and authorizes the
// bucket page INSIDE the read, so a cross-park drilldown is distinguishable from an authorized
// one.
//
// The refusal deliberately no longer happens before the repository is reached. Authorizing a
// park in one statement and reading the buckets in another is what let a task that moved park
// in between be authorized as its old park and paged as its new one, so the authority is now
// data the read evaluates against the row it returns.
func newCampaignShedsRepo() *campaignShedsRepo {
	return &campaignShedsRepo{parkRoutedRepo: parkRoutedRepo{parkByCampaign: map[string]string{
		securityCampaignA: plannerScopeParkMine,
		securityCampaignB: plannerScopeParkOthers,
	}}}
}

type campaignShedsRepo struct {
	parkRoutedRepo
}

// A monitor reads the campaign UNFILTERED, so the campaign id off the request is the only
// thing naming what they see -- and the role check is park-blind.
func TestListCampaignShedsRefusesAnotherParksCampaign(t *testing.T) {
	repo := newCampaignShedsRepo()
	svc := NewService(repo)

	_, err := svc.ListCampaignSheds(plannerScopedContext(), plannerScopedActor(), securityCampaignB, "", 20)
	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("sheds of another park's campaign err = %v, want ErrNotFound", err)
	}
	// The authority the service pushed down must be the actor's own park set -- not
	// unrestricted, which would make the refusal above an accident of this fixture.
	if repo.campaignShedsAccess.Unrestricted {
		t.Fatal("ListCampaignSheds pushed UNRESTRICTED access for a park-scoped actor")
	}
	if len(repo.campaignShedsAccess.AuthorizedParkIDs) != 1 ||
		repo.campaignShedsAccess.AuthorizedParkIDs[0] != plannerScopeParkMine {
		t.Fatalf("ListCampaignSheds pushed park set %v, want exactly [%s]",
			repo.campaignShedsAccess.AuthorizedParkIDs, plannerScopeParkMine)
	}

	if _, err := svc.ListCampaignSheds(plannerScopedContext(), plannerScopedActor(), securityCampaignA, "", 20); err != nil {
		t.Fatalf("the actor's OWN park must still list, got %v", err)
	}
}

// PlannerCatalog filtered Parks but not Operators, so the planner still received every
// assignable person in the tenant -- and admin-web pre-selects operators[0].
func TestPlannerCatalogFiltersOperatorsNotJustParks(t *testing.T) {
	repo := &plannerOperatorRepo{}
	catalog, err := NewService(repo).PlannerCatalog(plannerScopedContext(), plannerScopedActor(), "2026-08-10")
	if err != nil {
		t.Fatalf("planner catalog: %v", err)
	}
	got := map[string]bool{}
	for _, operator := range catalog.Operators {
		got[operator.UserID] = true
	}
	if got["theirs"] {
		t.Error("an operator scoped only to another park was offered")
	}
	if !got["mine"] {
		t.Error("the actor's own park's operator was dropped")
	}
	// Empty ParkIDs means EVERY park on this type, not "no parks" -- dropping it would hide
	// exactly the cross-park director a short-handed planner reaches for.
	if !got["crosspark"] {
		t.Error("the cross-park director (empty ParkIDs) was dropped")
	}
}

type plannerOperatorRepo struct{ fakeRepo }

func (r *plannerOperatorRepo) PlannerCatalog(context.Context, string, string) (domain.PlannerCatalog, error) {
	return domain.PlannerCatalog{
		Parks: []domain.PlannerPark{{ParkID: plannerScopeParkMine}},
		Operators: []domain.PlannerOperator{
			{UserID: "mine", ParkIDs: []string{plannerScopeParkMine}},
			{UserID: "theirs", ParkIDs: []string{plannerScopeParkOthers}},
			{UserID: "crosspark"},
		},
	}, nil
}

// A multi-park actor got a bare 400 "request is invalid", which Android renders as a
// permanently blank list with no discoverable remedy.
func TestListCampaignsAsksMultiParkActorToChooseWithAnActionableError(t *testing.T) {
	ctx := httpmiddleware.WithAuthGrants(context.Background(), []permissions.ActiveGrant{
		{Role: permissions.RoleGrowthDirector, ScopeType: "park", ScopeID: plannerScopeParkMine},
		{Role: permissions.RoleGrowthDirector, ScopeType: "park", ScopeID: plannerScopeParkOthers},
	})
	_, err := NewService(&listScopeRepo{}).ListCampaigns(ctx, plannerScopedActor(),
		domain.CampaignListScopeAll, "", "", 20)
	if !errors.Is(err, ports.ErrParkSelectionRequired) {
		t.Fatalf("multi-park list err = %v, want ErrParkSelectionRequired", err)
	}
	if errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatal("must not be ErrInvalidArgument -- the request was well formed")
	}
}
