package postgres

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	outboxpg "github.com/vgoats/goatos/backend/internal/outbox/adapters/postgres"
	eventbuspublisher "github.com/vgoats/goatos/backend/internal/outbox/adapters/publisher/eventbus"
	outboxapp "github.com/vgoats/goatos/backend/internal/outbox/app"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// TestUpsertOperatorAssignmentConfigEnqueuesConformantCapacityChangedEvent proves the REAL postgres
// capacity-producer path end to end: UpsertOperatorAssignmentConfig writes the config and durably
// enqueues vaccination.capacity.changed to outbox_messages in the same transaction, and that event
// (a) satisfies the outbox validate_outbox_event_tenant trigger for aggregate_type='park',
// (b) matches the per-type partial unique ON CONFLICT arbiter (migration 000038), and
// (c) conforms to the domain-event envelope schema so the real outbox relay publishes it.
// This is the capacity-side twin of the leave.changed E2E; both hand-rolled envelopes and uuid casts
// were previously untested against the live DB + schema.
func TestUpsertOperatorAssignmentConfigEnqueuesConformantCapacityChangedEvent(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const (
		tenant   = "00000000-0000-4000-8000-000000000001"
		park     = "00000000-0000-4000-8000-0000000ca001"
		operator = "00000000-0000-4000-8000-0000000ca002"
	)

	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($1::uuid, $2::uuid, 'park', 'PARK-CAP', 'Cap Park', 'active')
ON CONFLICT (location_id) DO NOTHING`, park, tenant); err != nil {
		t.Fatalf("seed park: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, primary_location_id)
VALUES ($1::uuid, $2::uuid, 'OP-CAP', 'Cap Operator', 'active', 'operator', $3::uuid)
ON CONFLICT (workforce_member_id) DO NOTHING`, operator, tenant, park); err != nil {
		t.Fatalf("seed operator: %v", err)
	}

	repo := NewRepository(pool, 5*time.Second)
	// Insert (row_version 0 -> 1): real production write path, durable outbox enqueue in-tx.
	if _, err := repo.UpsertOperatorAssignmentConfig(ctx, tenant, domain.OperatorAssignmentConfig{
		ParkID:                park,
		ActiveOperatorsPerDay: 1,
		DefaultOperatorID:     operator,
	}); err != nil {
		t.Fatalf("UpsertOperatorAssignmentConfig (real production write path): %v", err)
	}

	// Drive the real outbox relay: validates the envelope against the domain-event schema and
	// dispatches vaccination.capacity.changed to the bus. A non-conformant envelope or a
	// mismatched ON CONFLICT arbiter would surface here as FailedCount / a DB error.
	bus := eventbus.NewInProcessBus()
	var delivered int
	bus.Subscribe("vaccination.capacity.changed", eventbus.HandlerFunc(func(ctx context.Context, e eventbus.Event) error {
		delivered++
		return nil
	}))

	schemaPath, err := filepath.Abs(filepath.Join(
		"..", "..", "..", "..", "..", "contracts", "jsonschema", "domain-event-envelope.schema.json"))
	if err != nil {
		t.Fatalf("resolve envelope schema path: %v", err)
	}
	validator, err := outboxapp.NewEnvelopeValidator(schemaPath)
	if err != nil {
		t.Fatalf("build envelope validator (%s): %v", schemaPath, err)
	}
	service := outboxapp.NewService(outboxpg.NewRepository(pool, 5*time.Second), eventbuspublisher.New(bus), validator, outboxapp.Config{
		Limit:        100,
		MaxAttempts:  5,
		LeaseTimeout: time.Minute,
	})
	result, err := service.RunUntilDrained(ctx)
	if err != nil {
		t.Fatalf("outbox relay drain: %v", err)
	}
	if result.FailedCount > 0 {
		t.Fatalf("outbox relay rejected %d message(s) as invalid envelope -- capacity.changed does not conform to the domain-event schema", result.FailedCount)
	}
	if result.PublishedCount == 0 || delivered == 0 {
		t.Fatalf("capacity.changed not delivered: published=%d delivered=%d", result.PublishedCount, delivered)
	}
}
