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

// Regressions for the two remaining authorize-then-read races on this file's park-blind read
// surfaces: the task-detail BUCKET PAGE (buckets plus their assigned operator display names --
// another park's roster) and the LEADERSHIP EVIDENCE read (another park's proof footage).
//
// Both used to resolve the campaign's park with one statement and read the data with a second.
// park_id is MUTABLE -- UpdateCampaign moves a task between parks -- so the park that was
// authorized and the park the data came from were two reads of a moving value.
//
// The fixture below is built so the fix CANNOT be a re-check after the read: the move lands on
// whichever repository call happens first, so every authorize-then-read ordering loses and a
// re-check would just be a third read of the same moving value. Each test is paired with a
// replay of the PRE-FIX algorithm against the identical fixture, which must return the
// unauthorized data with a nil error -- otherwise the test proves nothing.

// ListCampaignSheds moves the park BEFORE answering, so a caller that authorized a park in an
// earlier statement is handed buckets from a different one.
func (r *parkMovingRepo) ListCampaignSheds(ctx context.Context, tenantID, campaignID, cursor string, limit int, access ports.CampaignAccess) (domain.CampaignShedPage, error) {
	r.applyPendingMove()
	return r.parkRoutedRepo.ListCampaignSheds(ctx, tenantID, campaignID, cursor, limit, access)
}

// GetLeadershipShedVideos moves the park BEFORE answering, for the same reason.
func (r *parkMovingRepo) GetLeadershipShedVideos(ctx context.Context, tenantID, campaignID, campaignShedID, cursor string, limit int, access ports.CampaignAccess) (domain.LeadershipShedVideos, error) {
	r.applyPendingMove()
	return r.parkRoutedRepo.GetLeadershipShedVideos(ctx, tenantID, campaignID, campaignShedID, cursor, limit, access)
}

// newParkMovingRepo is the shared race fixture: the task lives in park B (the ONLY park the
// escalation actor holds a weighing capability in) and a concurrent UpdateCampaign moves it into
// park A, where the actor holds nothing but an unrelated operator grant.
//
// The actor is deliberately NOT the assignee of the task. The assignee arm would legitimately
// admit it, and the park race would become invisible.
func newParkMovingRepo() *parkMovingRepo {
	repo := &parkMovingRepo{
		singleTaskRepo: *newSingleTaskRepo(),
		moveTo:         map[string]string{securityCampaignB: securityParkA},
	}
	repo.assignedByCampaign[securityCampaignB] = "30000000-0000-4000-8000-0000000004ff"
	return repo
}

func TestListCampaignShedsCannotBeRacedByATaskChangingPark(t *testing.T) {
	repo := newParkMovingRepo()

	page, err := NewService(repo).ListCampaignSheds(escalationContext(), escalationActor(), securityCampaignB, "", 20)
	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("ListCampaignSheds on a task that moved into an unauthorized park err = %v (%d buckets), want ErrNotFound -- CROSS-PARK ROSTER LEAK if nil", err, len(page.Items))
	}
	if len(page.Items) != 0 {
		t.Fatalf("ListCampaignSheds returned %d buckets of a task now in park %q, which the actor holds no weighing capability in", len(page.Items), repo.parkByCampaign[securityCampaignB])
	}
}

// TestListCampaignShedsPreFixAlgorithmLosesTheSameRace proves the fixture above DISCRIMINATES.
//
// This is the exact shape ListCampaignSheds had before: resolve the campaign's park through
// checkCampaignParkScopeForAny (which calls repo.CampaignParkID), and on success read the
// buckets in a second statement -- with no authority in it, because the old repository applied
// none. Against the identical fixture it returns another park's buckets AND a nil error. A test
// that passed with this implementation would be pinning nothing.
func TestListCampaignShedsPreFixAlgorithmLosesTheSameRace(t *testing.T) {
	repo := newParkMovingRepo()
	svc := NewService(repo)
	ctx, actor := escalationContext(), escalationActor()

	if err := svc.checkCampaignParkScopeForAny(ctx, actor.TenantID, securityCampaignB, planOrMonitorParkCapabilities...); err != nil {
		t.Fatalf("pre-fix park check refused before the move landed (%v); the fixture no longer reproduces the race", err)
	}
	// The pre-fix repository call carried NO authority at all -- the park check above was the
	// whole of it -- which is faithfully modelled as an unrestricted read.
	page, err := repo.ListCampaignSheds(ctx, actor.TenantID, securityCampaignB, "", 20, ports.CampaignAccess{Unrestricted: true})
	if err != nil || len(page.Items) == 0 {
		t.Fatalf("pre-fix replay err = %v with %d buckets; it must LEAK (nil error, buckets returned) for the regression test above to be discriminating", err, len(page.Items))
	}
	if repo.parkByCampaign[securityCampaignB] != securityParkA {
		t.Fatalf("pre-fix replay served a task still in park %q; the move must have landed inside the window for this to be the real defect", repo.parkByCampaign[securityCampaignB])
	}
}

func TestGetLeadershipShedVideosCannotBeRacedByATaskChangingPark(t *testing.T) {
	repo := newParkMovingRepo()

	videos, err := NewService(repo).GetLeadershipShedVideos(escalationContext(), escalationActor(), securityCampaignB, securityShed, "", 0)
	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("GetLeadershipShedVideos on a task that moved into an unauthorized park err = %v (bucket %q), want ErrNotFound -- CROSS-PARK EVIDENCE LEAK if nil", err, videos.CampaignShedID)
	}
	if videos.CampaignShedID != "" {
		t.Fatalf("GetLeadershipShedVideos returned proof footage for bucket %q of a task now in park %q", videos.CampaignShedID, repo.parkByCampaign[securityCampaignB])
	}
}

// TestGetLeadershipShedVideosPreFixAlgorithmLosesTheSameRace is the discrimination proof for the
// evidence read, replaying its pre-fix shape: repo.CampaignParkID, then
// checkParkScopeForCapability on that value, then an unauthorized second read.
func TestGetLeadershipShedVideosPreFixAlgorithmLosesTheSameRace(t *testing.T) {
	repo := newParkMovingRepo()
	svc := NewService(repo)
	ctx, actor := escalationContext(), escalationActor()

	parkID, err := repo.CampaignParkID(ctx, actor.TenantID, securityCampaignB)
	if err != nil {
		t.Fatalf("pre-fix park lookup: %v", err)
	}
	if err := svc.checkParkScopeForCapability(ctx, actor.TenantID, parkID, permissions.WeighingMonitor); err != nil {
		t.Fatalf("pre-fix park check refused before the move landed (%v); the fixture no longer reproduces the race", err)
	}
	videos, err := repo.GetLeadershipShedVideos(ctx, actor.TenantID, securityCampaignB, securityShed, "", 0, ports.CampaignAccess{Unrestricted: true})
	if err != nil || videos.CampaignShedID == "" {
		t.Fatalf("pre-fix replay err = %v with bucket %q; it must LEAK for the regression test above to be discriminating", err, videos.CampaignShedID)
	}
	if repo.parkByCampaign[securityCampaignB] != securityParkA {
		t.Fatalf("pre-fix replay served a task still in park %q; the move must have landed inside the window", repo.parkByCampaign[securityCampaignB])
	}
}

// TestListCampaignShedsResolvesTheAssigneesOwnBucketsInAnUnmonitoredPark pins the behaviour of
// 2c78f87f1 against this rewrite, on the surface that until now did NOT have it.
//
// A Growth Director holds WeighingMonitor AND WeighingExecute at once, deliberately: they
// execute weighing as well as oversee it. One who monitors park B while being ASSIGNED work in
// park A must still open their own park-A buckets. The arms are alternatives; treating park
// authority as a precondition 404s them on their own work -- and, worse, the task HEADER
// (GetCampaign) already resolves for them, so the screen would show a task whose own contents
// answer "not found".
func TestListCampaignShedsResolvesTheAssigneesOwnBucketsInAnUnmonitoredPark(t *testing.T) {
	repo := newSingleTaskRepo()
	// Park A is the park the actor holds only an unrelated grant in -- and it is where their own
	// assignment lives.
	repo.assignedByCampaign[securityCampaignA] = securityActorID

	page, err := NewService(repo).ListCampaignSheds(escalationContext(), escalationActor(), securityCampaignA, "", 20)
	if err != nil {
		t.Fatalf("ListCampaignSheds on the actor's OWN assigned task err = %v, want nil -- a director 404'd from their own buckets", err)
	}
	if page.CampaignID != securityCampaignA {
		t.Fatalf("ListCampaignSheds returned %q, want %q", page.CampaignID, securityCampaignA)
	}
	if repo.campaignShedsAccess.AssigneeUserID != securityActorID {
		t.Fatalf("ListCampaignSheds pushed assignee %q, want %q -- without the assignee arm the read has no way to admit this task", repo.campaignShedsAccess.AssigneeUserID, securityActorID)
	}
	// Admitted as an ASSIGNEE, so the page is narrowed to their own buckets rather than opened
	// as oversight of a park they do not monitor.
	if repo.campaignShedsOperator != securityActorID {
		t.Fatalf("ListCampaignSheds resolved the page unnarrowed (operator filter %q); an assignment must not widen into oversight of the whole park", repo.campaignShedsOperator)
	}
}

// TestListCampaignShedsPushesThePlanOrMonitorParkSetIntoTheRead proves the park authority
// reaches the query as DATA, and that the capability set matches what this surface's OWN role
// gate admits: plan-OR-monitor.
//
// Getting that set wrong in the NARROW direction is the dangerous one -- it fails closed as
// ErrNotFound, which is indistinguishable from a task that does not exist and which no
// higher-level test notices. It is pinned here by construction rather than by example: the
// expected set is computed with the same helper the role gate's alternatives imply, so a future
// narrowing to WeighingMonitor alone (the mistake checkParkScope's hardcoding invites) fails
// this test instead of silently locking a planner out of the buckets of a task whose header
// they can already read.
func TestListCampaignShedsPushesThePlanOrMonitorParkSetIntoTheRead(t *testing.T) {
	repo := newSingleTaskRepo()
	ctx := escalationContext()

	if _, err := NewService(repo).ListCampaignSheds(ctx, escalationActor(), securityCampaignB, "", 20); err != nil {
		t.Fatalf("ListCampaignSheds in the actor's own park err = %v, want nil", err)
	}
	access := repo.campaignShedsAccess
	if access.Unrestricted {
		t.Fatal("ListCampaignSheds pushed UNRESTRICTED access for a park-scoped actor; the read would answer every park in the tenant")
	}
	wantParks, tenantWide := authorizedParkSet(ctx, securityTenant, planOrMonitorParkCapabilities...)
	if tenantWide {
		t.Fatal("fixture drift: the escalation grants are no longer park-scoped")
	}
	if len(access.AuthorizedParkIDs) != len(wantParks) {
		t.Fatalf("ListCampaignSheds pushed park set %v, want the plan-or-monitor set %v", access.AuthorizedParkIDs, wantParks)
	}
	for _, parkID := range access.AuthorizedParkIDs {
		if _, ok := wantParks[parkID]; !ok {
			t.Fatalf("ListCampaignSheds pushed park %q, which carries no plan-or-monitor capability", parkID)
		}
	}
	// The park-A grant carries an unrelated role, so it must not appear -- the set stays
	// capability-bound, not merely park-scoped.
	for _, parkID := range access.AuthorizedParkIDs {
		if parkID == securityParkA {
			t.Fatal("ListCampaignSheds pushed park A, whose grant carries no weighing capability")
		}
	}
}

// TestGetLeadershipShedVideosPushesTheMonitorParkSetIntoTheRead pins the evidence read's
// capability set to WeighingMonitor -- exactly what its role gate admits.
//
// Widening it to plan-or-monitor would admit a planner the gate itself refuses. Narrowing it
// further, or adding an assignee arm, would change who may review other people's proof footage.
// Neither drift is visible from the outside, so both are pinned here.
func TestGetLeadershipShedVideosPushesTheMonitorParkSetIntoTheRead(t *testing.T) {
	repo := newSingleTaskRepo()
	ctx := escalationContext()

	if _, err := NewService(repo).GetLeadershipShedVideos(ctx, escalationActor(), securityCampaignB, securityShed, "", 0); err != nil {
		t.Fatalf("GetLeadershipShedVideos in the actor's own monitored park err = %v, want nil -- a monitor locked out of their own park's evidence", err)
	}
	access := repo.leadershipAccess
	if access.Unrestricted {
		t.Fatal("GetLeadershipShedVideos pushed UNRESTRICTED access for a park-scoped monitor")
	}
	if access.AssigneeUserID != "" {
		t.Fatalf("GetLeadershipShedVideos pushed an assignee arm (%q); this surface is monitor-only and an assignee reads their own captures through the roster", access.AssigneeUserID)
	}
	wantParks, tenantWide := authorizedParkSet(ctx, securityTenant, permissions.WeighingMonitor)
	if tenantWide {
		t.Fatal("fixture drift: the escalation grants are no longer park-scoped")
	}
	if len(access.AuthorizedParkIDs) != len(wantParks) {
		t.Fatalf("GetLeadershipShedVideos pushed park set %v, want the monitor set %v", access.AuthorizedParkIDs, wantParks)
	}
	for _, parkID := range access.AuthorizedParkIDs {
		if _, ok := wantParks[parkID]; !ok {
			t.Fatalf("GetLeadershipShedVideos pushed park %q, which carries no WeighingMonitor capability", parkID)
		}
	}
}

// TestBucketPageAndEvidenceReadTenantWideMonitorIsNotLockedOut is the fail-closed check in the
// other direction. Pushing authority into a query is exactly how a surface accidentally starts
// answering 404 to someone who is fully authorized -- and a 404 lockout is silent, because it
// looks like missing data rather than a refusal.
func TestBucketPageAndEvidenceReadTenantWideMonitorIsNotLockedOut(t *testing.T) {
	repo := newSingleTaskRepo()
	svc := NewService(repo)
	ctx := httpmiddleware.WithAuthGrants(context.Background(), []permissions.ActiveGrant{
		{Role: permissions.RoleGrowthDirector, ScopeType: "tenant", ScopeID: securityTenant},
	})

	for _, campaignID := range []string{securityCampaignA, securityCampaignB} {
		if _, err := svc.ListCampaignSheds(ctx, escalationActor(), campaignID, "", 20); err != nil {
			t.Fatalf("tenant-wide monitor ListCampaignSheds(%s) err = %v, want nil", campaignID, err)
		}
		if !repo.campaignShedsAccess.Unrestricted {
			t.Fatalf("tenant-wide monitor reached the bucket page with a park-restricted access %+v", repo.campaignShedsAccess)
		}
		if _, err := svc.GetLeadershipShedVideos(ctx, escalationActor(), campaignID, securityShed, "", 0); err != nil {
			t.Fatalf("tenant-wide monitor GetLeadershipShedVideos(%s) err = %v, want nil", campaignID, err)
		}
		if !repo.leadershipAccess.Unrestricted {
			t.Fatalf("tenant-wide monitor reached the evidence read with a park-restricted access %+v", repo.leadershipAccess)
		}
	}

	// An INTERNAL caller (CLI, seeder, integration test) carries no grants at all and must stay
	// unrestricted, the same escape hatch every park check in this file already has.
	if _, err := svc.ListCampaignSheds(context.Background(), escalationActor(), securityCampaignA, "", 20); err != nil {
		t.Fatalf("internal caller ListCampaignSheds err = %v, want nil", err)
	}
	if !repo.campaignShedsAccess.Unrestricted {
		t.Fatalf("internal caller reached the bucket page with a park-restricted access %+v", repo.campaignShedsAccess)
	}
}
