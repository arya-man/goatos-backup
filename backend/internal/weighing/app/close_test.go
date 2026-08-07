package app

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

const (
	closeCampaign = "00000000-0000-4000-8000-000000000501"
	closeScopeID  = "00000000-0000-4000-8000-000000000801"
)

// Close is a leadership action. An operator holding only weighing.execute must
// never be able to end a bucket or a task, because closing is allowed to strand
// work the operator still owed.
func TestCloseRequiresMonitorRoleAndNeverAcceptsExecuteOnly(t *testing.T) {
	repo := newScenarioRepo()
	service := NewService(repo)
	operator := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleOperator}}

	if _, err := service.CloseScope(context.Background(), operator, closeCampaign, closeScopeID, "close-1", "monsoon"); !errors.Is(err, ports.ErrForbidden) {
		t.Fatalf("operator CloseScope err=%v, want ErrForbidden", err)
	}
	if _, err := service.CloseCampaign(context.Background(), operator, closeCampaign, "close-1", "monsoon"); !errors.Is(err, ports.ErrForbidden) {
		t.Fatalf("operator CloseCampaign err=%v, want ErrForbidden", err)
	}
	if len(repo.closeScopeCalls)+len(repo.closeCampaignCalls) != 0 {
		t.Fatalf("forbidden close reached the repository: scope=%d campaign=%d", len(repo.closeScopeCalls), len(repo.closeCampaignCalls))
	}
}

// The reason is mandatory: a close that strands not-accepted work with no recorded
// reason is unauditable, so an empty/whitespace reason must be rejected BEFORE the
// repository transaction opens.
func TestCloseValidationRejectsMissingReasonKeyOrIDsBeforeRepository(t *testing.T) {
	tests := []struct {
		name           string
		campaignID     string
		campaignShedID string
		idempotencyKey string
		reason         string
	}{
		{name: "blank_reason", campaignID: closeCampaign, campaignShedID: closeScopeID, idempotencyKey: "close-1", reason: "   "},
		{name: "missing_idempotency_key", campaignID: closeCampaign, campaignShedID: closeScopeID, idempotencyKey: " ", reason: "monsoon"},
		{name: "malformed_campaign_id", campaignID: "not-a-uuid", campaignShedID: closeScopeID, idempotencyKey: "close-1", reason: "monsoon"},
		{name: "malformed_shed_id", campaignID: closeCampaign, campaignShedID: "not-a-uuid", idempotencyKey: "close-1", reason: "monsoon"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newScenarioRepo()
			service := NewService(repo)
			ceo := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleCEOInternal}}
			if _, err := service.CloseScope(context.Background(), ceo, tt.campaignID, tt.campaignShedID, tt.idempotencyKey, tt.reason); !errors.Is(err, ports.ErrInvalidArgument) {
				t.Fatalf("CloseScope err=%v, want ErrInvalidArgument", err)
			}
			if len(repo.closeScopeCalls) != 0 {
				t.Fatalf("invalid close reached the repository %d time(s)", len(repo.closeScopeCalls))
			}
		})
	}
}

// The service must pass through the actor, the trimmed reason, and the trimmed
// idempotency key so the repository fingerprint is stable across whitespace noise
// from the client.
func TestCloseForwardsActorTrimmedReasonAndKeyToRepository(t *testing.T) {
	repo := newScenarioRepo()
	service := NewService(repo)
	ceo := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleCEOInternal}}

	// Set up context with tenant-wide ceo_internal grant (needed for park scope check)
	ceoGrant := permissions.ActiveGrant{
		Role:      permissions.RoleCEOInternal,
		ScopeType: "tenant",
		ScopeID:   testTenant,
	}
	ctx := httpmiddleware.WithAuthGrants(context.Background(), []permissions.ActiveGrant{ceoGrant})
	ctx = httpmiddleware.WithTenantID(ctx, testTenant)

	result, err := service.CloseScope(ctx, ceo, closeCampaign, closeScopeID, "  close-1  ", "  shed emptied early  ")
	if err != nil {
		t.Fatalf("CloseScope errored: %v", err)
	}
	if result.Status != domain.StatusClosed {
		t.Fatalf("close status=%q, want %q", result.Status, domain.StatusClosed)
	}
	if len(repo.closeScopeCalls) != 1 {
		t.Fatalf("repository CloseScope calls=%d, want 1", len(repo.closeScopeCalls))
	}
	got := repo.closeScopeCalls[0]
	if got.TenantID != testTenant || got.ClosedBy != testActor {
		t.Fatalf("close actor tenant=%q closedBy=%q, want %q/%q", got.TenantID, got.ClosedBy, testTenant, testActor)
	}
	if got.Reason != "shed emptied early" || got.IdempotencyKey != "close-1" {
		t.Fatalf("close reason=%q key=%q, want trimmed values", got.Reason, got.IdempotencyKey)
	}
	if strings.TrimSpace(got.CampaignShedID) != closeScopeID {
		t.Fatalf("close campaignShedID=%q, want %q", got.CampaignShedID, closeScopeID)
	}

	if _, err := service.CloseCampaign(ctx, ceo, closeCampaign, " close-2 ", " park shut "); err != nil {
		t.Fatalf("CloseCampaign errored: %v", err)
	}
	if len(repo.closeCampaignCalls) != 1 {
		t.Fatalf("repository CloseCampaign calls=%d, want 1", len(repo.closeCampaignCalls))
	}
	campaignCall := repo.closeCampaignCalls[0]
	if campaignCall.CampaignShedID != "" {
		t.Fatalf("campaign close carried a bucket id %q; a task close is campaign-grain", campaignCall.CampaignShedID)
	}
	if campaignCall.Reason != "park shut" || campaignCall.IdempotencyKey != "close-2" {
		t.Fatalf("campaign close reason=%q key=%q, want trimmed values", campaignCall.Reason, campaignCall.IdempotencyKey)
	}
}

// Test P0 authorization hole: park-scoped WeighingMonitor must NOT be able to close
// campaigns from parks outside their scope. This is a CONFIRMED defect — park scope
// enforcement is missing from CloseScope, CloseCampaign, ReopenScope.
// All three mutations must deny cross-park access with ErrNotFound (not leaking existence).
func TestParkScopeEnforcedOnAllFourMutations(t *testing.T) {
	const (
		parkCBE     = "00000000-0000-4000-8000-000000000210" // CBE park
		parkCPT     = "00000000-0000-4000-8000-000000000211" // CPT park
		campaignCBE = "00000000-0000-4000-8000-000000000510" // campaign in CBE
		campaignCPT = "00000000-0000-4000-8000-000000000511" // campaign in CPT
		shedCBE     = "00000000-0000-4000-8000-000000000810" // shed in CBE campaign
		shedCPT     = "00000000-0000-4000-8000-000000000811" // shed in CPT campaign
	)

	repo := &multiParkScenarioRepo{
		campaigns: map[string]domain.Campaign{
			campaignCBE: {
				CampaignID: campaignCBE,
				TenantID:   testTenant,
				ParkID:     parkCBE,
				Status:     domain.StatusPublished,
			},
			campaignCPT: {
				CampaignID: campaignCPT,
				TenantID:   testTenant,
				ParkID:     parkCPT,
				Status:     domain.StatusPublished,
			},
		},
	}
	service := NewService(repo)

	// WeighingMonitor scoped to CBE only
	// Growth Director has WeighingMonitor permission
	cbeMonitor := domain.Actor{
		TenantID: testTenant,
		UserID:   testActor,
		Roles:    []string{permissions.RoleGrowthDirector},
	}

	// Create context with CBE-scoped grant (park scope restriction)
	cbeGrant := permissions.ActiveGrant{
		Role:      permissions.RoleGrowthDirector,
		ScopeType: "park",
		ScopeID:   parkCBE,
	}
	ctxWithCBEGrant := httpmiddleware.WithAuthGrants(context.Background(), []permissions.ActiveGrant{cbeGrant})

	// BEFORE FIX: This should FAIL but currently succeeds (the bug).
	// AFTER FIX: This should return ErrNotFound (not ErrForbidden, to hide existence).

	// Try to close a CPT campaign with CBE-only access
	if _, err := service.CloseScope(ctxWithCBEGrant, cbeMonitor, campaignCPT, shedCPT, "close-cpt", "test"); err != ports.ErrNotFound {
		t.Errorf("CloseScope CPT campaign: err=%v, want ErrNotFound (got authorization hole)", err)
	}

	// Try to reopen a CPT campaign with CBE-only access
	if err := service.ReopenScope(ctxWithCBEGrant, cbeMonitor, campaignCPT, shedCPT, "reopen-cpt", "test"); err != ports.ErrNotFound {
		t.Errorf("ReopenScope CPT campaign: err=%v, want ErrNotFound (got authorization hole)", err)
	}

	// Try to close a CPT campaign entirely with CBE-only access
	if _, err := service.CloseCampaign(ctxWithCBEGrant, cbeMonitor, campaignCPT, "close-cpt-campaign", "test"); err != ports.ErrNotFound {
		t.Errorf("CloseCampaign CPT campaign: err=%v, want ErrNotFound (got authorization hole)", err)
	}

	// Verify CBE operations still work (same actor, same role, same grant, CBE campaign)
	if _, err := service.CloseScope(ctxWithCBEGrant, cbeMonitor, campaignCBE, shedCBE, "close-cbe", "test"); err != nil {
		t.Errorf("CloseScope CBE campaign: err=%v, want success (same park)", err)
	}
}

// Tenant-wide WeighingMonitor (leadership) should still work across all parks
func TestTenantWideParkScopeAllowsAllParks(t *testing.T) {
	const (
		parkCBE     = "00000000-0000-4000-8000-000000000220" // CBE park
		parkCPT     = "00000000-0000-4000-8000-000000000221" // CPT park
		campaignCBE = "00000000-0000-4000-8000-000000000520" // campaign in CBE
		campaignCPT = "00000000-0000-4000-8000-000000000521" // campaign in CPT
		shedCBE     = "00000000-0000-4000-8000-000000000820" // shed in CBE campaign
		shedCPT     = "00000000-0000-4000-8000-000000000821" // shed in CPT campaign
	)

	repo := &multiParkScenarioRepo{
		campaigns: map[string]domain.Campaign{
			campaignCBE: {
				CampaignID: campaignCBE,
				TenantID:   testTenant,
				ParkID:     parkCBE,
				Status:     domain.StatusPublished,
			},
			campaignCPT: {
				CampaignID: campaignCPT,
				TenantID:   testTenant,
				ParkID:     parkCPT,
				Status:     domain.StatusPublished,
			},
		},
	}
	service := NewService(repo)

	// Tenant-wide WeighingMonitor (leadership with no park restriction)
	// Tenant-scoped grant = can access any park
	// Growth Director has WeighingMonitor permission
	tenantMonitor := domain.Actor{
		TenantID: testTenant,
		UserID:   testActor,
		Roles:    []string{permissions.RoleGrowthDirector},
	}

	// Create context with tenant-scoped grant (no park restriction)
	tenantGrant := permissions.ActiveGrant{
		Role:      permissions.RoleGrowthDirector,
		ScopeType: "tenant",
		ScopeID:   testTenant,
	}
	ctxWithTenantGrant := httpmiddleware.WithAuthGrants(context.Background(), []permissions.ActiveGrant{tenantGrant})

	// Should be able to close both parks
	if _, err := service.CloseScope(ctxWithTenantGrant, tenantMonitor, campaignCBE, shedCBE, "close-cbe", "test"); err != nil {
		t.Errorf("CloseScope CBE campaign (tenant-wide): err=%v, want success", err)
	}

	if _, err := service.CloseScope(ctxWithTenantGrant, tenantMonitor, campaignCPT, shedCPT, "close-cpt", "test"); err != nil {
		t.Errorf("CloseScope CPT campaign (tenant-wide): err=%v, want success", err)
	}
}

// Fake multi-park repository for testing authorization
type multiParkScenarioRepo struct {
	campaigns       map[string]domain.Campaign
	closeScopeCalls []domain.CloseCommand
}

func (r *multiParkScenarioRepo) CreateCampaign(context.Context, domain.CreateCampaign) (domain.Campaign, error) {
	return domain.Campaign{}, nil
}

func (r *multiParkScenarioRepo) UpdateCampaign(context.Context, string, domain.UpdateCampaign) (domain.Campaign, error) {
	return domain.Campaign{}, nil
}

func (r *multiParkScenarioRepo) PublishCampaign(context.Context, string, string, string, string) (domain.Campaign, error) {
	return domain.Campaign{}, nil
}

func (r *multiParkScenarioRepo) CampaignByID(context.Context, string, string, ports.CampaignAccess) (domain.Campaign, error) {
	return domain.Campaign{}, nil
}

func (r *multiParkScenarioRepo) WeighingParks(context.Context, string, []string) ([]domain.WeighingPark, error) {
	return nil, nil
}

func (r *multiParkScenarioRepo) ListCampaigns(context.Context, string, string, string, int) (domain.CampaignPage, error) {
	return domain.CampaignPage{}, nil
}

func (r *multiParkScenarioRepo) ListCampaignsForOperator(context.Context, string, string, string, string, int) (domain.CampaignPage, error) {
	return domain.CampaignPage{}, nil
}

func (r *multiParkScenarioRepo) ListCampaignSheds(context.Context, string, string, string, int, ports.CampaignAccess) (domain.CampaignShedPage, error) {
	return domain.CampaignShedPage{}, nil
}

func (r *multiParkScenarioRepo) GetLeadershipShedVideos(context.Context, string, string, string, string, int, ports.CampaignAccess) (domain.LeadershipShedVideos, error) {
	return domain.LeadershipShedVideos{}, nil
}

func (r *multiParkScenarioRepo) ListLeadershipSheds(context.Context, string, []string, string, int, int) (domain.LeadershipShedPage, error) {
	return domain.LeadershipShedPage{}, nil
}

func (r *multiParkScenarioRepo) PlannerCatalog(context.Context, string, string) (domain.PlannerCatalog, error) {
	return domain.PlannerCatalog{}, nil
}

func (r *multiParkScenarioRepo) PlannerParkBuckets(context.Context, string, string, string, string, string, int) (domain.PlannerParkBuckets, error) {
	return domain.PlannerParkBuckets{}, nil
}

func (r *multiParkScenarioRepo) ListScopeRoster(context.Context, string, string, string, string, int) (domain.RosterPage, error) {
	return domain.RosterPage{}, nil
}

func (r *multiParkScenarioRepo) ListScopeRosterForOperator(context.Context, string, string, string, string, string, int) (domain.RosterPage, error) {
	return domain.RosterPage{}, nil
}

func (r *multiParkScenarioRepo) RecordAnimalObservation(context.Context, domain.RecordAnimalObservation) (domain.Observation, error) {
	return domain.Observation{}, nil
}

func (r *multiParkScenarioRepo) RecordShedObservation(context.Context, domain.RecordShedObservation) (domain.Observation, error) {
	return domain.Observation{}, nil
}

func (r *multiParkScenarioRepo) SubmitIndividualScope(context.Context, string, string, string, string, string, []string) error {
	return nil
}

func (r *multiParkScenarioRepo) ReopenScope(context.Context, string, string, string, string, string, string) ([]string, error) {
	return nil, nil
}

func (r *multiParkScenarioRepo) CloseScope(context.Context, domain.CloseCommand) (domain.CloseResult, error) {
	r.closeScopeCalls = append(r.closeScopeCalls, domain.CloseCommand{})
	return domain.CloseResult{Status: domain.StatusClosed}, nil
}

func (r *multiParkScenarioRepo) CloseCampaign(context.Context, domain.CloseCommand) (domain.CloseResult, error) {
	return domain.CloseResult{Status: domain.StatusClosed}, nil
}

func (r *multiParkScenarioRepo) CampaignParkID(ctx context.Context, tenantID, campaignID string) (string, error) {
	if campaign, ok := r.campaigns[campaignID]; ok {
		if campaign.TenantID == tenantID {
			return campaign.ParkID, nil
		}
	}
	return "", ports.ErrNotFound
}

func (r *multiParkScenarioRepo) RefreshAvailability(context.Context, string, string) error {
	return nil
}

func (r *multiParkScenarioRepo) WeighingProcessState(context.Context, string, string, string, string) (domain.ProcessState, error) {
	return domain.ProcessState{}, nil
}

func (r *multiParkScenarioRepo) ListAlerts(context.Context, string, string, bool, []string, string, int) (domain.AlertPage, error) {
	return domain.AlertPage{}, nil
}

func (r *scenarioRepo) ExportCampaignCSV(context.Context, string, string, io.Writer) error {
	return nil
}

func (r *scenarioRepo) GetWeightHistory(context.Context, string, []string, string, string) (domain.WeightHistory, error) {
	return domain.WeightHistory{}, nil
}

func (r *scenarioRepo) GetLeadershipGrowthADG(context.Context, string, []string, time.Time, time.Time) (domain.GrowthADG, error) {
	return domain.GrowthADG{}, nil
}

func (r *scenarioRepo) GetShedWeights(context.Context, string, []string, time.Time, time.Time) (domain.ShedWeights, error) {
	return domain.ShedWeights{}, nil
}

func (r *scenarioRepo) GetWeightDemographics(context.Context, string, []string, time.Time, time.Time) (domain.WeightDemographics, error) {
	return domain.WeightDemographics{}, nil
}

func (r *scenarioRepo) ListParks(context.Context, string) ([]domain.WeighingPark, error) {
	return nil, nil
}

func (r *multiParkScenarioRepo) ExportCampaignCSV(context.Context, string, string, io.Writer) error {
	return nil
}

func (r *multiParkScenarioRepo) GetWeightHistory(context.Context, string, []string, string, string) (domain.WeightHistory, error) {
	return domain.WeightHistory{}, nil
}

func (r *multiParkScenarioRepo) GetLeadershipGrowthADG(context.Context, string, []string, time.Time, time.Time) (domain.GrowthADG, error) {
	return domain.GrowthADG{}, nil
}

func (r *multiParkScenarioRepo) GetShedWeights(context.Context, string, []string, time.Time, time.Time) (domain.ShedWeights, error) {
	return domain.ShedWeights{}, nil
}

func (r *multiParkScenarioRepo) GetWeightDemographics(context.Context, string, []string, time.Time, time.Time) (domain.WeightDemographics, error) {
	return domain.WeightDemographics{}, nil
}

func (r *multiParkScenarioRepo) ListParks(context.Context, string) ([]domain.WeighingPark, error) {
	return nil, nil
}
