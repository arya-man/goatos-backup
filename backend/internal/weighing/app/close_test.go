package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
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

	result, err := service.CloseScope(context.Background(), ceo, closeCampaign, closeScopeID, "  close-1  ", "  shed emptied early  ")
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

	if _, err := service.CloseCampaign(context.Background(), ceo, closeCampaign, " close-2 ", " park shut "); err != nil {
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
