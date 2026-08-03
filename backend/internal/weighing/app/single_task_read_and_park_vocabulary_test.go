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

// Regression tests for the two reads the phone could not make.
//
// GetCampaign resolves ONE task by id, because the task list is keyset-paged with no id filter
// and a cold notification deep link to a task further down the keyset was unanswerable. ListParks
// gives an oversight actor a park vocabulary they may legitimately read, because the only other
// park list (the planner catalog) is gated on the CEO-only WeighingPlan.
//
// Both take an id or a scope off the request while their role gates are park-BLIND, which is the
// exact shape of the cross-park holes this branch closed elsewhere -- so both are pinned here
// against the escalation fixture the rest of this package already uses (an unrelated grant in
// park A plus a capability-carrying grant in park B).

// singleTaskRepo answers the two new reads. CampaignByID mirrors the real repository contract:
// authorization happens INSIDE the read, as a disjunction over the access arms evaluated against
// the task's own park, and an excluded task is ErrNotFound. A fake that authorized nothing would
// let a service that forgot to push its authority down pass by accident.
type singleTaskRepo struct {
	parkRoutedRepo
	// campaignByIDAccess records the authority the service pushed into the query.
	campaignByIDAccess ports.CampaignAccess
	// campaignByIDOperator is the EFFECTIVE bucket-narrowing the access implies -- empty when the
	// caller is admitted by park authority (unfiltered oversight read), their own id when they
	// are admitted only as the assignee. It mirrors the hydration split the real repository
	// derives from the returned row, which is the only observable difference between the two.
	campaignByIDOperator string
	campaignByIDCalls    int
	// parksArg records the capability-scoped park set the service pushed into the query. nil is
	// the repository's UNRESTRICTED arm, so "was it nil" and "was it empty" must stay separable.
	parksArg    []string
	parksCalled bool
	parks       map[string]domain.WeighingPark
	// assignedByCampaign lives on the embedded parkRoutedRepo: the bucket page authorizes
	// against the same per-campaign assignment map this read does, and two copies of it could
	// disagree about who is assigned where.
}

func (r *singleTaskRepo) CampaignByID(_ context.Context, _, campaignID string, access ports.CampaignAccess) (domain.Campaign, error) {
	r.campaignByIDCalls++
	r.campaignByIDAccess = access
	r.campaignByIDOperator = ""
	parkID, ok := r.parkByCampaign[campaignID]
	if !ok {
		return domain.Campaign{}, ports.ErrNotFound
	}
	// The disjunction the production query evaluates: park authority over THIS row's park, or a
	// live assignment on it. Reading the park here, at answer time, is what makes this fake able
	// to expose an implementation that authorized some earlier value of it.
	switch {
	case access.AdmitsPark(parkID):
	case access.AssigneeUserID != "" && access.AssigneeUserID == r.assignedByCampaign[campaignID]:
		r.campaignByIDOperator = access.AssigneeUserID
	default:
		return domain.Campaign{}, ports.ErrNotFound
	}
	return domain.Campaign{CampaignID: campaignID, ParkID: parkID}, nil
}

func (r *singleTaskRepo) WeighingParks(_ context.Context, _ string, parkIDs []string) ([]domain.WeighingPark, error) {
	r.parksCalled = true
	r.parksArg = parkIDs
	allowed := map[string]struct{}{}
	for _, parkID := range parkIDs {
		allowed[parkID] = struct{}{}
	}
	out := []domain.WeighingPark{}
	for parkID, park := range r.parks {
		if len(parkIDs) > 0 {
			if _, ok := allowed[parkID]; !ok {
				continue
			}
		}
		out = append(out, park)
	}
	return out, nil
}

func newSingleTaskRepo() *singleTaskRepo {
	return &singleTaskRepo{
		parkRoutedRepo: parkRoutedRepo{
			parkByCampaign: map[string]string{
				securityCampaignA: securityParkA,
				securityCampaignB: securityParkB,
			},
			// The actor is the assignee in park B, where they hold the capability -- and NOT in
			// park A, matching the park-bound-operator invariant the database enforces.
			assignedByCampaign: map[string]string{
				securityCampaignA: "00000000-0000-4000-8000-0000000000ff",
				securityCampaignB: securityActorID,
			},
		},
		parks: map[string]domain.WeighingPark{
			securityParkA: {ParkID: securityParkA, Name: "Park A"},
			securityParkB: {ParkID: securityParkB, Name: "Park B"},
		},
	}
}

// TestGetCampaignRefusesCrossParkTaskById is the cross-park refusal for the new single-task read.
// The actor monitors park B and holds only an unrelated grant in park A, so naming park A's task
// id must answer ErrNotFound -- existence included, since a distinguishable 403 would confirm the
// task is real. Without the park check the flat "I monitor SOMEWHERE" role gate admits both.
func TestGetCampaignRefusesCrossParkTaskById(t *testing.T) {
	repo := newSingleTaskRepo()
	service := NewService(repo)
	ctx := escalationContext()
	actor := escalationActor()

	if _, err := service.GetCampaign(ctx, actor, securityCampaignA); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("GetCampaign in unrelated-grant park err = %v, want ErrNotFound (PRIVILEGE ESCALATION if nil)", err)
	}
	campaign, err := service.GetCampaign(ctx, actor, securityCampaignB)
	if err != nil {
		t.Fatalf("GetCampaign in capability-carrying park err = %v, want nil", err)
	}
	if campaign.CampaignID != securityCampaignB {
		t.Fatalf("GetCampaign returned campaign %q, want %q", campaign.CampaignID, securityCampaignB)
	}
	// A monitor reads the task UNFILTERED -- narrowing them to their own assignments would hide
	// the operator work they are there to oversee.
	if repo.campaignByIDOperator != "" {
		t.Fatalf("GetCampaign passed operator filter %q for a monitor, want unfiltered", repo.campaignByIDOperator)
	}
}

// TestGetCampaignNarrowsAnExecuteOnlyActorToTheirOwnAssignment pins the other half of the split
// GetCampaign shares with the bucket page it drills into: an assignee resolves the task only
// through their own assignment, which is what makes their branch safe WITHOUT a park check
// (nobody is assigned work in a park they do not work in).
func TestGetCampaignNarrowsAnExecuteOnlyActorToTheirOwnAssignment(t *testing.T) {
	repo := newSingleTaskRepo()
	service := NewService(repo)
	// An execute-only actor with NO grants at all in context: the park-check escape hatch would
	// otherwise mask a missing operator filter.
	ctx := httpmiddleware.WithAuthGrants(context.Background(), []permissions.ActiveGrant{
		{Role: permissions.RoleOperator, ScopeType: "park", ScopeID: securityParkB},
	})
	actor := domain.Actor{TenantID: securityTenant, UserID: securityActorID, Roles: []string{permissions.RoleOperator}}

	if _, err := service.GetCampaign(ctx, actor, securityCampaignB); err != nil {
		t.Fatalf("GetCampaign for the assignee err = %v, want nil", err)
	}
	if repo.campaignByIDOperator != securityActorID {
		t.Fatalf("GetCampaign passed operator filter %q, want %q -- an unfiltered read lets an operator resolve anyone's task by id", repo.campaignByIDOperator, securityActorID)
	}

	// Somebody else's task: the repository's own operator predicate refuses it, and the service
	// must surface that refusal rather than falling back to an unfiltered read.
	repo.assignedByCampaign[securityCampaignB] = "30000000-0000-4000-8000-0000000003ff"
	if _, err := service.GetCampaign(ctx, actor, securityCampaignB); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("GetCampaign for a task assigned to somebody else err = %v, want ErrNotFound", err)
	}
}

// TestGetCampaignParkCheckOrsOverPlanAndMonitor closes a latent lockout.
//
// GetCampaign's role gate is plan-OR-monitor, so its park check has to resolve against the SAME
// either/or set. It used to route through checkParkScope, which hardcodes WeighingMonitor -- the
// reopen/close authority. An actor holding weighing.plan WITHOUT weighing.monitor in the
// task's park would therefore pass the role gate and then be refused by the park check, as
// ErrNotFound: indistinguishable from a task that does not exist, and so the hardest possible
// failure to diagnose. No shipped role holds plan without monitor today, which is precisely why
// this is pinned -- the failure would arrive with a grant-matrix edit, not a code change.
//
// It is asserted on the helper rather than through GetCampaign because constructing a
// plan-without-monitor PRINCIPAL needs a role that does not exist and cannot be registered from
// outside the permissions package. What the helper proves is the part that was wrong: the check
// ORs over the capability set instead of collapsing to one capability. The fixture is the mirror
// image of the lockout (a grant carrying monitor but NOT plan), so a helper that silently used
// only capabilities[0] would pass the monitor case and fail this one.
func TestGetCampaignParkCheckOrsOverPlanAndMonitor(t *testing.T) {
	service := NewService(newSingleTaskRepo())
	// growth_director carries WeighingMonitor and deliberately NOT WeighingPlan.
	ctx := httpmiddleware.WithAuthGrants(context.Background(), []permissions.ActiveGrant{
		{Role: permissions.RoleGrowthDirector, ScopeType: "park", ScopeID: securityParkB},
	})
	if permissions.RoleHasPermission(permissions.RoleGrowthDirector, permissions.WeighingPlan) {
		t.Fatalf("fixture drift: growth_director now holds WeighingPlan, so this test no longer discriminates the two capabilities")
	}

	// Single-capability arm: plan alone is genuinely absent here, so it must refuse.
	if err := service.checkCampaignParkScopeForAny(ctx, securityTenant, securityCampaignB, permissions.WeighingPlan); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("plan-only check on a monitor-only grant err = %v, want ErrNotFound", err)
	}
	// Either/or arm: the SAME actor and park must be admitted through the other capability. A
	// check that collapsed the set to its first element would refuse here.
	if err := service.checkCampaignParkScopeForAny(ctx, securityTenant, securityCampaignB, planOrMonitorParkCapabilities...); err != nil {
		t.Fatalf("plan-or-monitor check on a monitor grant err = %v, want nil", err)
	}
	// Widening the capability set must not widen the PARK boundary.
	if err := service.checkCampaignParkScopeForAny(ctx, securityTenant, securityCampaignA, planOrMonitorParkCapabilities...); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("plan-or-monitor check in an unauthorized park err = %v, want ErrNotFound", err)
	}
	// And GetCampaign is wired to the either/or helper, not to the monitor-only one: the
	// monitor-carrying actor above resolves their own park's task unfiltered.
	if _, err := service.GetCampaign(ctx, escalationActor(), securityCampaignB); err != nil {
		t.Fatalf("GetCampaign in the actor's own park err = %v, want nil", err)
	}
}

// TestGetCampaignRejectsANonUUIDId keeps the id a real id: the campaign id arrives from a push
// payload, so a malformed one belongs in a 400 and never in a database predicate.
func TestGetCampaignRejectsANonUUIDId(t *testing.T) {
	repo := newSingleTaskRepo()
	service := NewService(repo)
	if _, err := service.GetCampaign(escalationContext(), escalationActor(), "not-a-uuid"); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("GetCampaign with a malformed id err = %v, want ErrInvalidArgument", err)
	}
	if repo.campaignByIDCalls != 0 {
		t.Fatalf("GetCampaign reached the repository %d times with a malformed id, want 0", repo.campaignByIDCalls)
	}
}

// TestListParksIsReadableByAMonitorWhoCannotPlan is the capability the endpoint exists for. The
// Growth Director holds WeighingMonitor and WeighingOverseeOperators and never WeighingPlan, so
// the planner catalog 403s them; this read must not.
func TestListParksIsReadableByAMonitorWhoCannotPlan(t *testing.T) {
	repo := newSingleTaskRepo()
	service := NewService(repo)
	actor := escalationActor()

	if permissions.RoleHasPermission(permissions.RoleGrowthDirector, permissions.WeighingPlan) {
		t.Fatalf("fixture drift: growth_director now holds WeighingPlan, so this test no longer proves a plan-less actor can read parks")
	}
	// The planner catalog's refusal is at the ROUTE, not in its service gate: the route names
	// WeighingPlan, so a plan-less actor never reaches the handler. Asserted through
	// AuthorizeRoute, the exact predicate the auth middleware evaluates.
	catalogRoute, ok := permissions.Match("GET", "/app/weighing/planner/catalog")
	if !ok {
		t.Fatalf("planner catalog route is not registered")
	}
	if permissions.AuthorizeRoute(catalogRoute, []string{permissions.RoleGrowthDirector}) {
		t.Fatalf("planner catalog admitted a plan-less actor; the park chips would not have needed a second read")
	}
	if _, err := service.ListParks(escalationContext(), actor); err != nil {
		t.Fatalf("ListParks err = %v, want nil for a monitor/overseer", err)
	}
}

// TestListParksReturnsOnlyCapabilityScopedParks holds the park vocabulary to the same rule as
// every other weighing park surface: the actor's authorized parks, not the tenant's. A chip for a
// park they do not oversee leaks that park's name and existence, and the list read behind the chip
// would 403 anyway.
func TestListParksReturnsOnlyCapabilityScopedParks(t *testing.T) {
	repo := newSingleTaskRepo()
	service := NewService(repo)

	parks, err := service.ListParks(escalationContext(), escalationActor())
	if err != nil {
		t.Fatalf("ListParks err = %v, want nil", err)
	}
	if len(parks) != 1 || parks[0].ParkID != securityParkB {
		t.Fatalf("ListParks = %#v, want only park B -- park A is backed by an unrelated grant", parks)
	}
	// The park set must reach the QUERY, not a post-filter: the repository is the layer that can
	// keep an authorization boundary from colliding with a page boundary.
	if len(repo.parksArg) != 1 || repo.parksArg[0] != securityParkB {
		t.Fatalf("ListParks pushed park set %#v into the query, want [park B]", repo.parksArg)
	}
}

// TestListParksIsUnrestrictedForATenantWideMonitor pins the nil-versus-empty contract this
// repository declares. A tenant-wide actor is authorized EVERYWHERE, so passing their (empty)
// park set through as an empty slice would answer zero chips instead of every park.
func TestListParksIsUnrestrictedForATenantWideMonitor(t *testing.T) {
	repo := newSingleTaskRepo()
	service := NewService(repo)
	ctx := httpmiddleware.WithAuthGrants(context.Background(), []permissions.ActiveGrant{
		{Role: permissions.RoleGrowthDirector, ScopeType: "tenant", ScopeID: securityTenant},
	})

	parks, err := service.ListParks(ctx, escalationActor())
	if err != nil {
		t.Fatalf("ListParks err = %v, want nil", err)
	}
	if !repo.parksCalled || repo.parksArg != nil {
		t.Fatalf("ListParks pushed park set %#v for a tenant-wide actor, want nil (unrestricted)", repo.parksArg)
	}
	if len(parks) != 2 {
		t.Fatalf("ListParks = %#v, want every park for a tenant-wide monitor", parks)
	}
}

// TestListParksAnswersNoChipsWhenTheActorOwnsNoPark closes the fail-open case: an actor who
// passes the flat role gate on some grant but holds the capability in no park here must get an
// EMPTY vocabulary, never the unrestricted read.
func TestListParksAnswersNoChipsWhenTheActorOwnsNoPark(t *testing.T) {
	repo := newSingleTaskRepo()
	service := NewService(repo)
	ctx := httpmiddleware.WithAuthGrants(context.Background(), []permissions.ActiveGrant{
		{Role: permissions.RoleOperator, ScopeType: "park", ScopeID: securityParkA},
	})

	parks, err := service.ListParks(ctx, escalationActor())
	if err != nil {
		t.Fatalf("ListParks err = %v, want nil", err)
	}
	if len(parks) != 0 {
		t.Fatalf("ListParks = %#v, want no chips", parks)
	}
	if repo.parksCalled {
		t.Fatalf("ListParks reached the repository with no authorized park, which is the unrestricted arm")
	}
}

// TestListParksRefusesAnActorWithNoWeighingOversight keeps the vocabulary off surfaces that have
// no park chips: an execute-only operator filters nothing, and the park names are not theirs.
func TestListParksRefusesAnActorWithNoWeighingOversight(t *testing.T) {
	service := NewService(newSingleTaskRepo())
	actor := domain.Actor{TenantID: securityTenant, UserID: securityActorID, Roles: []string{permissions.RoleOperator}}
	if _, err := service.ListParks(escalationContext(), actor); !errors.Is(err, ports.ErrForbidden) {
		t.Fatalf("ListParks for an execute-only actor err = %v, want ErrForbidden", err)
	}
}
