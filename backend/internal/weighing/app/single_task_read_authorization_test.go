package app

import (
	"context"
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// parkMovingRepo is the TOCTOU fixture. A campaign's park is MUTABLE -- UpdateCampaign moves a
// task between parks -- and this fake models the one thing a static fixture cannot: the value
// changing between two reads of it.
//
// The move lands AFTER the first repository call has answered and BEFORE the second one is
// evaluated -- the exact window an authorize-then-read pair leaves open. A caller that resolves
// the park in one statement sees the old park (authorized), and the row it then fetches is in
// the new one, which this fake surfaces as a returned campaign in a park the actor has no
// authority in. A caller that re-checked the park after the read would be no better: the
// re-check is a third read of a value that is still moving. Only a single statement that admits
// and returns the same row snapshot survives -- it evaluates its predicate on the post-move park
// and correctly refuses.
type parkMovingRepo struct {
	singleTaskRepo
	// moveTo maps a campaign to the park a concurrent UpdateCampaign puts it in.
	moveTo map[string]string
	moved  bool
}

// applyPendingMove runs the concurrent update ONCE, on whichever repository call happens first.
// It is deliberately not attached to one named method: the fixture models a task moving in the
// gap between any two reads, so an implementation cannot dodge it by choosing a different first
// call.
func (r *parkMovingRepo) applyPendingMove() {
	if r.moved {
		return
	}
	r.moved = true
	for campaignID, parkID := range r.moveTo {
		r.parkByCampaign[campaignID] = parkID
	}
}

func (r *parkMovingRepo) CampaignParkID(ctx context.Context, tenantID, campaignID string) (string, error) {
	parkID, err := r.singleTaskRepo.CampaignParkID(ctx, tenantID, campaignID)
	if err != nil {
		return "", err
	}
	// Answer with the park as it was, then let the update land: the caller now holds a park value
	// that is already stale.
	r.applyPendingMove()
	return parkID, nil
}

func (r *parkMovingRepo) CampaignByID(ctx context.Context, tenantID, campaignID string, access ports.CampaignAccess) (domain.Campaign, error) {
	r.applyPendingMove()
	return r.singleTaskRepo.CampaignByID(ctx, tenantID, campaignID, access)
}

// TestGetCampaignCannotBeRacedByATaskChangingPark is the regression for the read's
// time-of-check/time-of-use hole.
//
// The actor monitors park B only. The task is in park B when its park is looked up and in park A
// by the time the row is read, so authorization and retrieval disagree about which park was
// approved -- and the answer served would be a park the actor has no authority in, operator names
// included. The only shape that survives this fixture is one where the predicate that admits the
// row and the row itself come from a single query, which is why the authority is now pushed into
// CampaignByID instead of being checked before it.
func TestGetCampaignCannotBeRacedByATaskChangingPark(t *testing.T) {
	repo := &parkMovingRepo{
		singleTaskRepo: *newSingleTaskRepo(),
		moveTo:         map[string]string{securityCampaignB: securityParkA},
	}
	// The actor must NOT be the assignee here, or the assignment arm would legitimately admit the
	// task and the park race would be invisible.
	repo.assignedByCampaign[securityCampaignB] = "30000000-0000-4000-8000-0000000004ff"
	service := NewService(repo)

	campaign, err := service.GetCampaign(escalationContext(), escalationActor(), securityCampaignB)
	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("GetCampaign on a task that moved into an unauthorized park err = %v (campaign park %q), want ErrNotFound -- CROSS-PARK LEAK if nil", err, campaign.ParkID)
	}
	if campaign.ParkID == securityParkA {
		t.Fatalf("GetCampaign returned a task in park %q, which the actor holds no weighing capability in", campaign.ParkID)
	}
}

// TestGetCampaignResolvesTheAssigneesOwnTaskInAnUnmonitoredPark pins the behaviour of 2c78f87f1
// against the authorization rewrite.
//
// A Growth Director holds WeighingMonitor AND WeighingExecute at once, deliberately: they
// execute weighing as well as oversee it. One who monitors park B while being ASSIGNED work in
// park A must still resolve their own park-A task, through the assignment rather than through
// park authority. The arms are alternatives; treating park authority as a precondition would
// 404 them on their own work.
func TestGetCampaignResolvesTheAssigneesOwnTaskInAnUnmonitoredPark(t *testing.T) {
	repo := newSingleTaskRepo()
	// Park A is the park the actor holds only an unrelated grant in -- and it is where their own
	// assignment lives.
	repo.assignedByCampaign[securityCampaignA] = securityActorID
	service := NewService(repo)

	campaign, err := service.GetCampaign(escalationContext(), escalationActor(), securityCampaignA)
	if err != nil {
		t.Fatalf("GetCampaign on the actor's OWN assigned task err = %v, want nil -- a director 404'd from their own work", err)
	}
	if campaign.CampaignID != securityCampaignA {
		t.Fatalf("GetCampaign returned %q, want %q", campaign.CampaignID, securityCampaignA)
	}
	if repo.campaignByIDAccess.AssigneeUserID != securityActorID {
		t.Fatalf("GetCampaign pushed assignee %q, want %q -- without the assignee arm the read has no way to admit this task", repo.campaignByIDAccess.AssigneeUserID, securityActorID)
	}
	// Admitted as an ASSIGNEE, so the task is narrowed to their own bucket rather than opened as
	// an oversight read of a park they do not monitor.
	if repo.campaignByIDOperator != securityActorID {
		t.Fatalf("GetCampaign resolved the task unnarrowed (operator filter %q); an assignment must not widen into oversight of the whole park", repo.campaignByIDOperator)
	}
}

// TestGetCampaignPushesOnlyTheAuthorizedParkSetIntoTheRead proves the park authority reaches the
// query as DATA rather than being applied around it, and that it stays capability-bound: the
// escalation fixture holds an unrelated grant in park A and the capability-carrying one in park
// B, so only park B may appear.
func TestGetCampaignPushesOnlyTheAuthorizedParkSetIntoTheRead(t *testing.T) {
	repo := newSingleTaskRepo()
	service := NewService(repo)

	if _, err := service.GetCampaign(escalationContext(), escalationActor(), securityCampaignB); err != nil {
		t.Fatalf("GetCampaign in the actor's own park err = %v, want nil", err)
	}
	access := repo.campaignByIDAccess
	if access.Unrestricted {
		t.Fatalf("GetCampaign pushed UNRESTRICTED access for a park-scoped actor; the read would answer every park in the tenant")
	}
	if len(access.AuthorizedParkIDs) != 1 || access.AuthorizedParkIDs[0] != securityParkB {
		t.Fatalf("GetCampaign pushed park set %v, want exactly [%s] -- the park-A grant carries no weighing capability", access.AuthorizedParkIDs, securityParkB)
	}
}

// TestCampaignCapabilitiesAreAnsweredForTheTasksOwnPark is the regression for the live button
// that answers "not found".
//
// can_end/can_reopen used to be a park-blind role check while CloseCampaign/ReopenCampaign
// enforce park scope and refuse an unauthorized park with ErrNotFound. A monitor scoped to park
// B therefore rendered a live Abandon/Close button on a park-A task, and tapping it failed --
// the same defect the capability map exists to prevent, displaced from permission grain to park
// grain.
func TestCampaignCapabilitiesAreAnsweredForTheTasksOwnPark(t *testing.T) {
	service := NewService(newSingleTaskRepo())
	ctx := escalationContext()
	actor := escalationActor()

	own := service.CampaignCapabilities(ctx, actor, domain.Campaign{CampaignID: securityCampaignB, ParkID: securityParkB})
	if !own.CanEnd || !own.CanReopen {
		t.Fatalf("capabilities in the actor's OWN monitored park = %+v, want end/reopen true -- the buttons the write would allow must not be hidden", own)
	}

	foreign := service.CampaignCapabilities(ctx, actor, domain.Campaign{CampaignID: securityCampaignA, ParkID: securityParkA})
	if foreign.CanEnd || foreign.CanReopen {
		t.Fatalf("capabilities on a task in an unmonitored park = %+v, want end/reopen false -- a live button whose write answers ErrNotFound", foreign)
	}

	// Publish is a DIFFERENT permission (WeighingPlan) and growth_director does not hold it, so
	// it stays false even in the park the actor fully monitors.
	if permissions.RoleHasPermission(permissions.RoleGrowthDirector, permissions.WeighingPlan) {
		t.Fatalf("fixture drift: growth_director now holds WeighingPlan, so this test no longer separates the two permissions")
	}
	if own.CanPublish {
		t.Fatalf("capabilities reported can_publish for an actor without WeighingPlan")
	}

	// A failed read leaves the handler holding a zero campaign. There is no park to authorize
	// and no task to act on, so every answer must be no rather than an accidental park-blind yes.
	if empty := service.CampaignCapabilities(ctx, actor, domain.Campaign{}); empty.CanEnd || empty.CanReopen || empty.CanPublish {
		t.Fatalf("capabilities for an unresolved task = %+v, want all false", empty)
	}
}
