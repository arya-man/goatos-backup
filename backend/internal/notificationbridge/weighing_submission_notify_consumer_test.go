package notificationbridge_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	calendarpg "github.com/vgoats/goatos/backend/internal/calendar/adapters/postgres"
	calendarapp "github.com/vgoats/goatos/backend/internal/calendar/app"
	"github.com/vgoats/goatos/backend/internal/notificationbridge"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	workforcepg "github.com/vgoats/goatos/backend/internal/workforce/adapters/postgres"
	workforceapp "github.com/vgoats/goatos/backend/internal/workforce/app"
	workforcedomain "github.com/vgoats/goatos/backend/internal/workforce/domain"
)

const (
	weighingSubmissionScope = "fa000000-0000-4000-8000-0000000000c1"
	weighingReopenScope     = "fa000000-0000-4000-8000-0000000000c2"
)

func TestWeighingSubmissionEventConsumerQueuesDirectorAndCEOIdempotently(t *testing.T) {
	ctx := context.Background()
	pool, _ := vecSetup(t)

	workforceRepo := workforcepg.NewRepository(pool, 5*time.Second)
	seedWeighingGrowthDirectorPosition(t, ctx, pool)
	rosterService := workforceapp.NewRosterService(workforceRepo, workforceRepo)
	calendarService := calendarapp.NewService(calendarpg.NewRepository(pool, 5*time.Second))
	consumer := notificationbridge.NewWeighingSubmissionEventConsumer(rosterService, calendarService, slog.Default())

	payload, err := json.Marshal(map[string]any{
		"tenant_id":        vnTenant,
		"campaign_id":      vecBatchReady,
		"campaign_shed_id": weighingSubmissionScope,
		"park_id":          vnPark,
		"shed_id":          vnPark,
		"shed_label":       "Gandhi",
		"completed_at":     "2026-07-29T10:00:00Z",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	event := eventbus.Event{
		Type:     notificationbridge.EventWeighingShedSubmissionCompleted,
		TenantID: vnTenant,
		Key:      weighingSubmissionScope,
		Payload:  payload,
	}
	bus := eventbus.NewInProcessBus()
	consumer.Register(bus)

	if err := bus.Publish(ctx, event); err != nil {
		t.Fatalf("first publish: %v", err)
	}
	if err := bus.Publish(ctx, event); err != nil {
		t.Fatalf("replay publish: %v", err)
	}

	recipientRefs := vecRecipientRefs(t, ctx, pool, weighingSubmissionScope)
	if len(recipientRefs) != 2 || !recipientRefs[vnLeadershipToken] || !recipientRefs[vnCEOToken] {
		t.Fatalf("weighing completion recipients = %v, want exactly Director %q and CEO %q",
			recipientRefs, vnLeadershipToken, vnCEOToken)
	}
}

func TestWeighingSubmissionEventConsumerPassesEventParkToAudienceResolver(t *testing.T) {
	recipients := &fakeRecipients{
		byMember:   map[string][]workforcedomain.NotificationRecipient{},
		byPosition: map[string][]workforcedomain.NotificationRecipient{},
	}
	queue := &fakeQueue{}
	audience := &recordingAudience{}
	consumer := notificationbridge.NewWeighingSubmissionEventConsumer(recipients, queue, slog.Default()).WithAudience(audience)

	payload, err := json.Marshal(map[string]any{
		"tenant_id":        lifecycleTenant,
		"campaign_id":      lifecycleCampaign,
		"campaign_shed_id": lifecycleBucketA,
		"park_id":          lifecyclePark,
		"shed_id":          lifecyclePark,
		"shed_label":       "Gandhi",
		"completed_at":     "2026-07-29T10:00:00Z",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	event := eventbus.Event{
		Type:     notificationbridge.EventWeighingShedSubmissionCompleted,
		TenantID: lifecycleTenant,
		Key:      lifecycleBucketA,
		Payload:  payload,
	}
	if err := consumer.HandleEvent(context.Background(), event); err != nil {
		t.Fatalf("HandleEvent errored: %v", err)
	}
	if len(audience.calls) != 1 {
		t.Fatalf("audience calls=%d, want 1", len(audience.calls))
	}
	call := audience.calls[0]
	if call.tenantID != lifecycleTenant || call.parkID != lifecyclePark {
		t.Fatalf("audience call=%+v, want tenant %s and park %s", call, lifecycleTenant, lifecyclePark)
	}
}

func TestWeighingReopenEventConsumerQueuesOperatorGrowthDirectorAndCEO(t *testing.T) {
	ctx := context.Background()
	pool, _ := vecSetup(t)
	seedWeighingGrowthDirectorPosition(t, ctx, pool)

	workforceRepo := workforcepg.NewRepository(pool, 5*time.Second)
	rosterService := workforceapp.NewRosterService(workforceRepo, workforceRepo)
	calendarService := calendarapp.NewService(calendarpg.NewRepository(pool, 5*time.Second))
	consumer := notificationbridge.NewWeighingSubmissionEventConsumer(rosterService, calendarService, slog.Default())

	payload, err := json.Marshal(map[string]any{
		"tenant_id":        vnTenant,
		"campaign_id":      vecBatchReady,
		"campaign_shed_id": weighingReopenScope,
		"park_id":          vnPark,
		"shed_id":          vnPark,
		"shed_label":       "Gandhi",
		"operator_id":      vecOperatorUser,
		"reopened_by":      vnLeadershipMember,
		"reason":           "missed tags",
		"reopened_at":      "2026-07-30T10:00:00Z",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	event := eventbus.Event{
		Type:     notificationbridge.EventWeighingShedReopened,
		TenantID: vnTenant,
		Key:      weighingReopenScope,
		Payload:  payload,
	}
	bus := eventbus.NewInProcessBus()
	consumer.Register(bus)

	if err := bus.Publish(ctx, event); err != nil {
		t.Fatalf("publish: %v", err)
	}

	recipientRefs := vecRecipientRefs(t, ctx, pool, weighingReopenScope)
	if len(recipientRefs) != 3 || !recipientRefs[vnOperatorToken] || !recipientRefs[vnLeadershipToken] || !recipientRefs[vnCEOToken] {
		t.Fatalf("weighing reopen recipients = %v, want operator %q, Growth Director %q, CEO %q",
			recipientRefs, vnOperatorToken, vnLeadershipToken, vnCEOToken)
	}
}

func seedWeighingGrowthDirectorPosition(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	exec(t, ctx, pool, "tenant position growth_director",
		`INSERT INTO workforce_positions (tenant_id, workforce_member_id, scope_type, scope_id, position_code, position_tier, status, valid_from)
		 VALUES ($1, $2, 'tenant', $1, 'growth_director', 'director', 'active', now() - interval '1 hour')
		 ON CONFLICT DO NOTHING`,
		vnTenant, vnLeadershipMember)
}
