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

const (
	securityParkA     = "30000000-0000-4000-8000-00000000000a"
	securityParkB     = "30000000-0000-4000-8000-00000000000b"
	securityCampaignA = "30000000-0000-4000-8000-00000000010a"
	securityCampaignB = "30000000-0000-4000-8000-00000000010b"
	securityShed      = "30000000-0000-4000-8000-000000000201"
	securityTenant    = testTenant
	securityActorID   = "30000000-0000-4000-8000-000000000301"
)

// parkRoutedRepo is a fakeRepo whose CampaignParkID resolves per-campaign, so tests can
// prove per-park authorization instead of the single fixed testPark every other fakeRepo
// in this package answers with.
type parkRoutedRepo struct {
	fakeRepo
	parkByCampaign map[string]string
	// assignedByCampaign is the operator each campaign's buckets belong to. It is per-campaign,
	// not global, because a single `assignedTo` would make one actor the assignee of BOTH parks
	// -- a state the database itself forbids (weighing_operator_park_bound_guard: an operator is
	// park-bound). A fixture that violates a DB-enforced invariant cannot tell a real refusal
	// from a fake one.
	assignedByCampaign map[string]string

	// campaignShedsAccess / leadershipAccess record the authority the service pushed DOWN, so a
	// test can prove the service handed the read its authority rather than merely that the read
	// refused.
	campaignShedsAccess ports.CampaignAccess
	campaignShedsCalls  int
	// campaignShedsOperator is the effective bucket narrowing: empty when park authority
	// admitted the task (unfiltered), the caller's own id when only their assignment did.
	campaignShedsOperator string
	leadershipAccess      ports.CampaignAccess
	leadershipCalls       int
}

// ListCampaignSheds models the real repository contract: authorization is INSIDE the read, as a
// disjunction over the access arms evaluated against the task's park AT ANSWER TIME. Reading the
// park here, rather than closing over a value captured earlier, is what makes this fake able to
// expose a service that authorized some earlier value of it.
func (r *parkRoutedRepo) ListCampaignSheds(_ context.Context, _, campaignID, _ string, _ int, access ports.CampaignAccess) (domain.CampaignShedPage, error) {
	r.campaignShedsCalls++
	r.campaignShedsAccess = access
	r.campaignShedsOperator = ""
	parkID, ok := r.parkByCampaign[campaignID]
	if !ok {
		return domain.CampaignShedPage{}, ports.ErrNotFound
	}
	switch {
	case access.AdmitsPark(parkID):
	case access.AssigneeUserID != "" && access.AssigneeUserID == r.assignedByCampaign[campaignID]:
		r.campaignShedsOperator = access.AssigneeUserID
	default:
		return domain.CampaignShedPage{}, ports.ErrNotFound
	}
	return domain.CampaignShedPage{
		CampaignID: campaignID,
		Items:      []domain.CampaignShed{{CampaignShedID: securityShed, CampaignID: campaignID, OperatorUserID: r.assignedByCampaign[campaignID]}},
		TotalCount: 1,
	}, nil
}

// GetLeadershipShedVideos has NO assignee arm -- leadership evidence review is monitor-only, so
// park authority over the task's park at answer time is the whole test.
func (r *parkRoutedRepo) GetLeadershipShedVideos(_ context.Context, _, campaignID, campaignShedID, _ string, _ int, access ports.CampaignAccess) (domain.LeadershipShedVideos, error) {
	r.leadershipCalls++
	r.leadershipAccess = access
	parkID, ok := r.parkByCampaign[campaignID]
	if !ok {
		return domain.LeadershipShedVideos{}, ports.ErrNotFound
	}
	if !access.AdmitsPark(parkID) {
		return domain.LeadershipShedVideos{}, ports.ErrNotFound
	}
	return domain.LeadershipShedVideos{CampaignID: campaignID, CampaignShedID: campaignShedID, ParkName: parkID}, nil
}

func (r *parkRoutedRepo) CampaignParkID(_ context.Context, _ string, campaignID string) (string, error) {
	parkID, ok := r.parkByCampaign[campaignID]
	if !ok {
		return "", ports.ErrNotFound
	}
	return parkID, nil
}

// ListLeadershipSheds models the real repository contract: it returns one bucket per campaign it
// knows about, RESTRICTED to parkIDs when the service supplies one (nil meaning unrestricted).
// Honouring the filter here is the point -- a fake that ignored it would let the service pass this
// test on a post-filter that the production query no longer performs.
func (r *parkRoutedRepo) ListLeadershipSheds(_ context.Context, _ string, parkIDs []string, _ string, _, _ int) (domain.LeadershipShedPage, error) {
	allowed := map[string]struct{}{}
	for _, parkID := range parkIDs {
		allowed[parkID] = struct{}{}
	}
	items := make([]domain.LeadershipShedVideos, 0, len(r.parkByCampaign))
	for campaignID, parkID := range r.parkByCampaign {
		if len(parkIDs) > 0 {
			if _, ok := allowed[parkID]; !ok {
				continue
			}
		}
		items = append(items, domain.LeadershipShedVideos{CampaignID: campaignID, CampaignShedID: securityShed})
	}
	return domain.LeadershipShedPage{Items: items}, nil
}

// escalationGrants is the exact shape the three P0 bugs were built around: an actor holds a
// grant in Park A carrying a role with NO weighing/vaccination authority, plus a SEPARATE
// grant scoped to Park B whose role DOES carry the capability being exercised. The
// capability-blind AuthorizedParkIDs helper collected both parks' ids into one list and let
// the actor exercise the capability in Park A too; the fix must keep each grant's role bound
// to its own park.
func escalationGrants() []permissions.ActiveGrant {
	return []permissions.ActiveGrant{
		// Unrelated role in Park A: RoleOperator carries no WeighingMonitor/VaccinationOverseeExecution.
		{Role: permissions.RoleOperator, ScopeType: "park", ScopeID: securityParkA},
		// Capability-carrying role, but scoped ONLY to Park B.
		{Role: permissions.RoleGrowthDirector, ScopeType: "park", ScopeID: securityParkB},
	}
}

func escalationContext() context.Context {
	return httpmiddleware.WithAuthGrants(context.Background(), escalationGrants())
}

func escalationActor() domain.Actor {
	return domain.Actor{
		TenantID: securityTenant,
		UserID:   securityActorID,
		// RolesAuthorize (the coarse, flat role gate that runs BEFORE park scoping) only cares
		// that the actor holds WeighingMonitor SOMEWHERE; growth_director grants it. This
		// mirrors production: the flat gate never saw park scope at all, that job was
		// AuthorizedParkIDs/checkParkScope's alone -- which is exactly what was broken.
		Roles: []string{permissions.RoleGrowthDirector},
	}
}

func newEscalationRepo() *parkRoutedRepo {
	return &parkRoutedRepo{
		parkByCampaign: map[string]string{
			securityCampaignA: securityParkA,
			securityCampaignB: securityParkB,
		},
	}
}

// TestCloseScopeDeniesUnrelatedParkGrantAndAllowsCapabilityCarryingPark proves fix #2:
// checkParkScope/checkParkScopeForCapability must deny WeighingMonitor-gated mutations
// (close/reopen/abandon/campaign-close) in a park the actor only holds an UNRELATED grant
// in, while allowing them in the park backed by the capability-carrying grant.
func TestCloseScopeDeniesUnrelatedParkGrantAndAllowsCapabilityCarryingPark(t *testing.T) {
	repo := newEscalationRepo()
	service := NewService(repo)
	ctx := escalationContext()
	actor := escalationActor()

	tests := []struct {
		name       string
		campaignID string
		wantDenied bool
	}{
		{name: "denied in park backed only by an unrelated grant", campaignID: securityCampaignA, wantDenied: true},
		{name: "allowed in park backed by the capability-carrying grant", campaignID: securityCampaignB, wantDenied: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := service.CloseScope(ctx, actor, tt.campaignID, securityShed, "idem-"+tt.campaignID, "reason")
			if tt.wantDenied {
				if !errors.Is(err, ports.ErrNotFound) {
					t.Fatalf("CloseScope(%s) err = %v, want ErrNotFound (PRIVILEGE ESCALATION if nil)", tt.campaignID, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("CloseScope(%s) err = %v, want nil", tt.campaignID, err)
			}
		})
	}
}

// TestReopenAbandonCampaignCloseAllRouteThroughParkCapabilityCheck proves reopen/abandon/
// campaign-close all inherit the same capability-aware park check as CloseScope -- none of
// them may be band-aided independently of checkParkScope.
func TestReopenAbandonCampaignCloseAllRouteThroughParkCapabilityCheck(t *testing.T) {
	repo := newEscalationRepo()
	service := NewService(repo)
	ctx := escalationContext()
	actor := escalationActor()

	if err := service.ReopenScope(ctx, actor, securityCampaignA, securityShed, "idem-reopen-a", "reason"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("ReopenScope in unrelated-grant park err = %v, want ErrNotFound", err)
	}
	if err := service.ReopenScope(ctx, actor, securityCampaignB, securityShed, "idem-reopen-b", "reason"); err != nil {
		t.Fatalf("ReopenScope in capability-carrying park err = %v, want nil", err)
	}

	if _, err := service.AbandonScope(ctx, actor, securityCampaignA, securityShed, "idem-abandon-a", "reason"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("AbandonScope in unrelated-grant park err = %v, want ErrNotFound", err)
	}
	if _, err := service.AbandonScope(ctx, actor, securityCampaignB, securityShed, "idem-abandon-b", "reason"); err != nil {
		t.Fatalf("AbandonScope in capability-carrying park err = %v, want nil", err)
	}

	if _, err := service.CloseCampaign(ctx, actor, securityCampaignA, "idem-close-camp-a", "reason"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("CloseCampaign in unrelated-grant park err = %v, want ErrNotFound", err)
	}
	if _, err := service.CloseCampaign(ctx, actor, securityCampaignB, "idem-close-camp-b", "reason"); err != nil {
		t.Fatalf("CloseCampaign in capability-carrying park err = %v, want nil", err)
	}
}

// TestLeadershipShedVideosAndListDenyCrossPark proves fix #3: GetLeadershipShedVideos and
// ListLeadershipSheds had NO park check at all before this fix (only a flat WeighingMonitor
// role check), so a park-scoped monitor could read another park's weighing proof by campaign
// ID, and the tenant-wide leadership gallery leaked every park's buckets to a single-park
// monitor.
func TestLeadershipShedVideosAndListDenyCrossPark(t *testing.T) {
	repo := newEscalationRepo()
	service := NewService(repo)
	ctx := escalationContext()
	actor := escalationActor()

	if _, err := service.GetLeadershipShedVideos(ctx, actor, securityCampaignA, securityShed, "", 0); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("GetLeadershipShedVideos in unrelated-grant park err = %v, want ErrNotFound (BUG: no park check at all)", err)
	}
	if _, err := service.GetLeadershipShedVideos(ctx, actor, securityCampaignB, securityShed, "", 0); err != nil {
		t.Fatalf("GetLeadershipShedVideos in capability-carrying park err = %v, want nil", err)
	}

	page, err := service.ListLeadershipSheds(ctx, actor, "", 0)
	if err != nil {
		t.Fatalf("ListLeadershipSheds err = %v, want nil", err)
	}
	for _, item := range page.Items {
		if item.CampaignID == securityCampaignA {
			t.Fatalf("PRIVILEGE ESCALATION: ListLeadershipSheds returned bucket %#v from a park the actor is not authorized for", item)
		}
	}
}
