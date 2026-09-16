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

type reviewReplayRepo struct {
	fakeRepo
	saved domain.Campaign
}

func (r *reviewReplayRepo) CampaignByIdempotencyKey(context.Context, domain.CreateCampaign) (domain.Campaign, bool, error) {
	return r.saved, true, nil
}

func TestReview274ReplayMustCheckCurrentParkGrant(t *testing.T) {
	cmd := validCreate()
	otherPark := "00000000-0000-4000-8000-000000000299"
	ctx := httpmiddleware.WithAuthGrants(context.Background(), []permissions.ActiveGrant{{Role: permissions.RoleCEOInternal, ScopeType: "park", ScopeID: otherPark}})
	svc := NewService(&reviewReplayRepo{saved: domain.Campaign{CampaignID: "saved-task", ParkID: cmd.ParkID}})
	actor := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleCEOInternal}}
	if err := svc.checkParkScopeForCapability(ctx, testTenant, cmd.ParkID, permissions.WeighingPlan); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("fixture must deny target park: %v", err)
	}
	got, err := svc.CreateCampaign(ctx, actor, cmd)
	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("replay exposed campaign %q without current park access: err=%v", got.CampaignID, err)
	}
	// The current stored park wins even if an edit moved the task since create.
	for _, tc := range []struct {
		name, savedPark, grantPark string
		allowed                    bool
	}{
		{"same park", cmd.ParkID, cmd.ParkID, true},
		{"moved away", otherPark, cmd.ParkID, false},
		{"moved into scope", otherPark, otherPark, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewService(&reviewReplayRepo{saved: domain.Campaign{CampaignID: "saved-task", ParkID: tc.savedPark}})
			ctx := httpmiddleware.WithAuthGrants(context.Background(), []permissions.ActiveGrant{{Role: permissions.RoleCEOInternal, ScopeType: "park", ScopeID: tc.grantPark}})
			got, err := svc.CreateCampaign(ctx, actor, cmd)
			if tc.allowed {
				if err != nil || got.CampaignID != "saved-task" {
					t.Fatalf("authorized replay: %+v %v", got, err)
				}
			} else if !errors.Is(err, ports.ErrNotFound) {
				t.Fatalf("unauthorized moved task: %v", err)
			}
		})
	}
}

func TestReview274OwnCutoffMustNotRequireFarmDefaultForCards(t *testing.T) {
	rules := rulesWithMode(7, domain.RemovalModeRequired)
	rules.Rules.FeedWaterRemoval.CutoffTime = "21:30"
	store := &fakeFastingStore{cardVersions: []int{7}}
	svc := NewService(&capturingRepo{}).WithSOPRules(rules, pinnedVersion(7)).WithFastingStore(store).WithClock(beforeCutoffClock("2026-07-29"))
	if _, err := svc.removalCutoff(context.Background(), testTenant, rules.Rules); err != nil {
		t.Fatal(err)
	}
	_, err := svc.ListMyFastingShedCards(context.Background(), domain.Actor{TenantID: testTenant, UserID: testOp, Roles: []string{permissions.RoleOperator}}, "", 20)
	if err != nil {
		t.Fatalf("SOP defines 21:30, but card list failed: %v", err)
	}
}
