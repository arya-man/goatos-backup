package app

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// TestCheckParkScopePrivilegeEscalation reproduces the privilege escalation hole:
// an actor with a tenant-wide grant for an UNRELATED role (health_director)
// plus a park-A-scoped weighing grant can close a park-B campaign because
// checkParkScope only checks "has tenant grant" without verifying the grant
// carries a WEIGHING-relevant permission.
//
// This test FAILS on the buggy code (actor incorrectly authorized for park-B)
// and PASSES after the fix (actor correctly denied for park-B).
func TestCheckParkScopePrivilegeEscalation_TenantGrantWrongRole(t *testing.T) {
	// Setup: two parks
	parkA := "00000000-0000-4000-8000-000000000201"
	parkB := "00000000-0000-4000-8000-000000000202"
	campaignInParkB := "00000000-0000-4000-8000-000000000501"
	testTenant := "00000000-0000-0000-0000-000000000001"

	repo := &parkScopeCheckRepo{
		campaignParkID: parkB, // Campaign is in park B
	}
	service := NewService(repo)

	// Actor has:
	// 1. A tenant-wide grant for health_director (an UNRELATED role)
	// 2. A park-A-scoped grant for weighing
	// This person should ONLY be able to access park A for weighing,
	// not park B (even though they have a tenant-wide grant).
	healthDirectorTenantGrant := permissions.ActiveGrant{
		Role:      permissions.RoleHealthDirector,
		ScopeType: "tenant",
		ScopeID:   testTenant,
	}
	weighingParkAGrant := permissions.ActiveGrant{
		Role:      permissions.RoleGrowthDirector,
		ScopeType: "park",
		ScopeID:   parkA,
	}

	ctx := context.Background()
	ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{
		healthDirectorTenantGrant,
		weighingParkAGrant,
	})
	ctx = httpmiddleware.WithTenantID(ctx, testTenant)

	// Attempt to close a campaign in park B should be DENIED (not found / forbidden)
	shedID := "00000000-0000-4000-8000-000000000801"
	_, err := service.CloseScope(ctx, domain.Actor{
		TenantID: testTenant,
		UserID:   "actor-1",
		Roles:    []string{permissions.RoleHealthDirector, permissions.RoleGrowthDirector},
	}, campaignInParkB, shedID, "close-key-1", "testing")

	// BUGGY CODE: err == nil (actor incorrectly authorized)
	// FIXED CODE: err == ports.ErrNotFound (actor correctly denied)
	if err == nil {
		t.Fatal("actor with tenant-wide health_director + park-A weighing grant incorrectly authorized to close park-B campaign; want ErrNotFound")
	}
	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// TestCheckParkScopeTenantWideWeighingRole verifies that an actor with
// a tenant-wide grant that DOES carry weighing authority (growth_director or ceo_internal)
// IS authorized to access all parks for weighing.
func TestCheckParkScopeTenantWideWeighingRole(t *testing.T) {
	parkB := "00000000-0000-4000-8000-000000000202"
	campaignInParkB := "00000000-0000-4000-8000-000000000501"
	testTenant := "00000000-0000-0000-0000-000000000001"

	repo := &parkScopeCheckRepo{
		campaignParkID: parkB,
	}
	service := NewService(repo)

	// Actor has a tenant-wide growth_director grant (which includes weighing permissions)
	growthDirectorTenantGrant := permissions.ActiveGrant{
		Role:      permissions.RoleGrowthDirector,
		ScopeType: "tenant",
		ScopeID:   testTenant,
	}

	ctx := context.Background()
	ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{
		growthDirectorTenantGrant,
	})
	ctx = httpmiddleware.WithTenantID(ctx, testTenant)

	// Attempt to close a campaign in park B should SUCCEED (or get past checkParkScope)
	// because the tenant-wide grant carries weighing authority
	shedID := "00000000-0000-4000-8000-000000000802"
	_, err := service.CloseScope(ctx, domain.Actor{
		TenantID: testTenant,
		UserID:   "actor-1",
		Roles:    []string{permissions.RoleGrowthDirector},
	}, campaignInParkB, shedID, "close-key-2", "testing")

	// Should NOT be denied by checkParkScope; may fail for other reasons (fake repo)
	// but the important thing is it doesn't get ErrNotFound from park scope check.
	if errors.Is(err, ports.ErrNotFound) {
		t.Fatal("actor with tenant-wide growth_director grant incorrectly denied access to park-B; want authorization granted by checkParkScope")
	}
}

// TestCheckParkScopeParkScopedGrant verifies the normal case: an actor with
// a park-A-scoped weighing grant can access park A but not park B.
func TestCheckParkScopeParkScopedGrant_AccessGrantedForMatchingPark(t *testing.T) {
	parkA := "00000000-0000-4000-8000-000000000201"
	campaignInParkA := "00000000-0000-4000-8000-000000000501"
	testTenant := "00000000-0000-0000-0000-000000000001"

	repo := &parkScopeCheckRepo{
		campaignParkID: parkA,
	}
	service := NewService(repo)

	weighingParkAGrant := permissions.ActiveGrant{
		Role:      permissions.RoleGrowthDirector,
		ScopeType: "park",
		ScopeID:   parkA,
	}

	ctx := context.Background()
	ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{
		weighingParkAGrant,
	})
	ctx = httpmiddleware.WithTenantID(ctx, testTenant)

	// Attempt to close a campaign in park A should succeed (checkParkScope passes)
	shedID := "00000000-0000-4000-8000-000000000803"
	_, err := service.CloseScope(ctx, domain.Actor{
		TenantID: testTenant,
		UserID:   "actor-1",
		Roles:    []string{permissions.RoleGrowthDirector},
	}, campaignInParkA, shedID, "close-key-3", "testing")

	// Should not be denied by checkParkScope
	if errors.Is(err, ports.ErrNotFound) {
		t.Fatal("actor with park-A weighing grant incorrectly denied access to park-A campaign; want authorization granted by checkParkScope")
	}
}

// TestCheckParkScopeParkScopedGrant_AccessDeniedForOtherPark verifies
// that a park-scoped grant does NOT grant access to other parks.
func TestCheckParkScopeParkScopedGrant_AccessDeniedForOtherPark(t *testing.T) {
	parkA := "00000000-0000-4000-8000-000000000201"
	parkB := "00000000-0000-4000-8000-000000000202"
	campaignInParkB := "00000000-0000-4000-8000-000000000501"
	testTenant := "00000000-0000-0000-0000-000000000001"

	repo := &parkScopeCheckRepo{
		campaignParkID: parkB,
	}
	service := NewService(repo)

	weighingParkAGrant := permissions.ActiveGrant{
		Role:      permissions.RoleGrowthDirector,
		ScopeType: "park",
		ScopeID:   parkA,
	}

	ctx := context.Background()
	ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{
		weighingParkAGrant,
	})
	ctx = httpmiddleware.WithTenantID(ctx, testTenant)

	// Attempt to close a campaign in park B should be DENIED
	shedID := "00000000-0000-4000-8000-000000000804"
	_, err := service.CloseScope(ctx, domain.Actor{
		TenantID: testTenant,
		UserID:   "actor-1",
		Roles:    []string{permissions.RoleGrowthDirector},
	}, campaignInParkB, shedID, "close-key-4", "testing")

	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("actor with park-A weighing grant should be denied access to park-B; err = %v, want ErrNotFound", err)
	}
}

// parkScopeCheckRepo is a fake repo that tracks the park a campaign belongs to.
type parkScopeCheckRepo struct {
	campaignParkID string
}

func (r *parkScopeCheckRepo) CampaignParkID(ctx context.Context, tenantID, campaignID string) (string, error) {
	return r.campaignParkID, nil
}

func (r *parkScopeCheckRepo) CreateCampaign(ctx context.Context, cmd domain.CreateCampaign) (domain.Campaign, error) {
	return domain.Campaign{}, nil
}

func (r *parkScopeCheckRepo) UpdateCampaign(ctx context.Context, campaignID string, cmd domain.UpdateCampaign) (domain.Campaign, error) {
	return domain.Campaign{}, nil
}

func (r *parkScopeCheckRepo) PublishCampaign(ctx context.Context, tenantID, campaignID, userID, idempotencyKey string) (domain.Campaign, error) {
	return domain.Campaign{}, nil
}

func (r *parkScopeCheckRepo) CampaignByID(context.Context, string, string, ports.CampaignAccess) (domain.Campaign, error) {
	return domain.Campaign{}, nil
}

func (r *parkScopeCheckRepo) WeighingParks(context.Context, string, []string) ([]domain.WeighingPark, error) {
	return nil, nil
}

func (r *parkScopeCheckRepo) ListCampaigns(ctx context.Context, tenantID, parkID, cursor string, limit int) (domain.CampaignPage, error) {
	return domain.CampaignPage{}, nil
}

func (r *parkScopeCheckRepo) ListCampaignsForOperator(ctx context.Context, tenantID, operatorUserID, parkID, cursor string, limit int) (domain.CampaignPage, error) {
	return domain.CampaignPage{}, nil
}

func (r *parkScopeCheckRepo) PlannerCatalog(ctx context.Context, tenantID, periodStartDate string) (domain.PlannerCatalog, error) {
	return domain.PlannerCatalog{}, nil
}

func (r *parkScopeCheckRepo) PlannerParkBuckets(ctx context.Context, tenantID, parkID, periodStartDate, excludeCampaignID, cursor string, limit int) (domain.PlannerParkBuckets, error) {
	return domain.PlannerParkBuckets{}, nil
}

func (r *parkScopeCheckRepo) ListScopeRoster(ctx context.Context, tenantID, campaignID, campaignShedID, observationsCursor string, limit int) (domain.RosterPage, error) {
	return domain.RosterPage{}, nil
}

func (r *parkScopeCheckRepo) ListScopeRosterForOperator(ctx context.Context, tenantID, campaignID, campaignShedID, operatorUserID, observationsCursor string, limit int) (domain.RosterPage, error) {
	return domain.RosterPage{}, nil
}

func (r *parkScopeCheckRepo) ListCampaignSheds(ctx context.Context, tenantID, campaignID, cursor string, limit int, access ports.CampaignAccess) (domain.CampaignShedPage, error) {
	return domain.CampaignShedPage{}, nil
}

func (r *parkScopeCheckRepo) GetLeadershipShedVideos(ctx context.Context, tenantID, campaignID, campaignShedID, cursor string, limit int, access ports.CampaignAccess) (domain.LeadershipShedVideos, error) {
	return domain.LeadershipShedVideos{}, nil
}

func (r *parkScopeCheckRepo) ListLeadershipSheds(ctx context.Context, tenantID string, parkIDs []string, cursor string, limit int, videosPageSize int) (domain.LeadershipShedPage, error) {
	return domain.LeadershipShedPage{}, nil
}

func (r *parkScopeCheckRepo) RecordAnimalObservation(ctx context.Context, cmd domain.RecordAnimalObservation) (domain.Observation, error) {
	return domain.Observation{}, nil
}

func (r *parkScopeCheckRepo) RecordShedObservation(ctx context.Context, cmd domain.RecordShedObservation) (domain.Observation, error) {
	return domain.Observation{}, nil
}

func (r *parkScopeCheckRepo) SubmitIndividualScope(ctx context.Context, tenantID, campaignID, campaignShedID, operatorUserID, idempotencyKey string, scannedIdentifiers []string) error {
	return nil
}

func (r *parkScopeCheckRepo) ReopenScope(ctx context.Context, tenantID, campaignID, campaignShedID, closedBy, idempotencyKey, reason string) ([]string, error) {
	return nil, nil
}

func (r *parkScopeCheckRepo) CloseScope(ctx context.Context, cmd domain.CloseCommand) (domain.CloseResult, error) {
	return domain.CloseResult{}, nil
}

func (r *parkScopeCheckRepo) CloseCampaign(ctx context.Context, cmd domain.CloseCommand) (domain.CloseResult, error) {
	return domain.CloseResult{}, nil
}

func (r *parkScopeCheckRepo) RefreshAvailability(ctx context.Context, tenantID, campaignID string) error {
	return nil
}

func (r *parkScopeCheckRepo) ListAlerts(context.Context, string, string, bool, []string, string, int) (domain.AlertPage, error) {
	return domain.AlertPage{}, nil
}

func (r *parkScopeCheckRepo) ExportCampaignCSV(context.Context, string, string, io.Writer) error {
	return nil
}

func (r *parkScopeCheckRepo) GetWeightHistory(context.Context, string, []string, string, string) (domain.WeightHistory, error) {
	return domain.WeightHistory{}, nil
}

func (r *parkScopeCheckRepo) GetLeadershipGrowthADG(context.Context, string, []string, time.Time, time.Time) (domain.GrowthADG, error) {
	return domain.GrowthADG{}, nil
}

func (r *parkScopeCheckRepo) ListParks(context.Context, string) ([]domain.WeighingPark, error) {
	return nil, nil
}
