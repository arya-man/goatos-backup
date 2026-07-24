package kernelstages

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

// TestBuildDomainBusDispatchesOperatorConfigReplanEvents is the real-path regression for the
// operator-config auto-cascade: BuildDomainBus is the bus every real deployment mode actually
// dispatches outbox-relayed domain events onto (DomainConsumerStage over Pub/Sub in staging, and
// the local/dev in-process outbox publisher -- see outbox_relay.go's BuildOutboxPublisher). A prior
// session proved the vaccination.capacity.changed / vaccination.roster.changed /
// vaccination.leave.changed cascade end-to-end using a hand-wired bus+handler+relay test harness
// (operator_config_replan_leave_e2e_test.go), which only proves the HANDLER logic is correct -- it
// never proves BuildDomainBus itself registers that handler. It did not: OperatorConfigReplanHandler
// was absent from BuildDomainBus's handler list, so a real admin-web config/leave write's durably
// enqueued outbox event is silently never delivered to it in any real running deployment.
//
// This test publishes all three event types directly onto the bus BuildDomainBus returns and
// asserts each left a 'succeeded' row in obligation_operator_config_replan_watermarks -- the
// concrete, durable proof that OperatorConfigReplanHandler.HandleEvent actually ran for each event
// type on the REAL production wiring path, not a test-only substitute.
func TestBuildDomainBusDispatchesOperatorConfigReplanEvents(t *testing.T) {
	pgtest.SkipIfNoDocker(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := pgtest.StartPostgres(t, ctx)
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	pgCfg := platformpg.Config{QueryTimeout: 5 * time.Second}

	bus := BuildDomainBus(pool, pgCfg, logger)

	const tenantID = "00000000-0000-4000-8000-0000000000f2"
	const parkID = "00000000-0000-4000-8000-0000000000f3"

	cases := []struct {
		eventType string
		idSuffix  string
	}{
		{"vaccination.capacity.changed", "cap"},
		{"vaccination.roster.changed", "roster"},
		{"vaccination.leave.changed", "leave"},
	}
	for _, tc := range cases {
		t.Run(tc.eventType, func(t *testing.T) {
			payload, _ := json.Marshal(map[string]string{"park_id": parkID})
			eventID := "bus-registration-proof-" + tc.idSuffix
			now := time.Now().UTC()
			if err := bus.Publish(ctx, eventbus.Event{
				ID:         eventID,
				Type:       tc.eventType,
				TenantID:   tenantID,
				Key:        parkID,
				Payload:    payload,
				OccurredAt: now,
				RecordedAt: now,
			}); err != nil {
				t.Fatalf("bus.Publish(%s): %v", tc.eventType, err)
			}

			var status string
			if err := pool.QueryRow(ctx, `
SELECT status FROM obligation_operator_config_replan_watermarks
WHERE tenant_id = $1::uuid AND event_id = $2`, tenantID, eventID).Scan(&status); err != nil {
				t.Fatalf("%s: no replan watermark row written -- OperatorConfigReplanHandler is not registered on BuildDomainBus's returned bus, so the real durable-relay dispatch path silently drops this event: %v", tc.eventType, err)
			}
			if status != "succeeded" {
				t.Fatalf("%s: replan watermark status = %q, want succeeded", tc.eventType, status)
			}
		})
	}
}
