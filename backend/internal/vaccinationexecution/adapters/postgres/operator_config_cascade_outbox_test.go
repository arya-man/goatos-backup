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
// operator-config producer path end to end, and that it emits PRECISELY the right cascade event(s):
//   - a first write emits BOTH vaccination.capacity.changed AND vaccination.roster.changed,
//   - a default-operator-only change emits ONLY vaccination.roster.changed,
//   - an N (active-operators-per-day)-only change emits ONLY vaccination.capacity.changed.
//
// Each emitted event (a) satisfies the outbox validate_outbox_event_tenant trigger for
// aggregate_type='park', (b) matches its per-type partial unique ON CONFLICT arbiter (migration
// 000038), and (c) conforms to the domain-event envelope schema so the real outbox relay publishes it.
func TestUpsertOperatorAssignmentConfigEnqueuesConformantCapacityChangedEvent(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const (
		tenant    = "00000000-0000-4000-8000-000000000001"
		park      = "00000000-0000-4000-8000-0000000ca001"
		operator  = "00000000-0000-4000-8000-0000000ca002"
		operator2 = "00000000-0000-4000-8000-0000000ca003"
	)

	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($1::uuid, $2::uuid, 'park', 'PARK-CAP', 'Cap Park', 'active')
ON CONFLICT (location_id) DO NOTHING`, park, tenant); err != nil {
		t.Fatalf("seed park: %v", err)
	}
	for i, op := range []string{operator, operator2} {
		if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, primary_location_id)
VALUES ($1::uuid, $2::uuid, $3, $4, 'active', 'operator', $5::uuid)
ON CONFLICT (workforce_member_id) DO NOTHING`, op, tenant, "OP-CAP-"+string(rune('A'+i)), "Cap Operator", park); err != nil {
			t.Fatalf("seed operator %d: %v", i, err)
		}
	}

	// drainCascade drives the REAL outbox relay (schema-validated) and returns how many
	// capacity.changed / roster.changed events it delivered to the bus in this drain. A
	// non-conformant envelope or a mismatched ON CONFLICT arbiter would surface as FailedCount.
	drainCascade := func() (capacity, roster int) {
		t.Helper()
		bus := eventbus.NewInProcessBus()
		bus.Subscribe("vaccination.capacity.changed", eventbus.HandlerFunc(func(context.Context, eventbus.Event) error { capacity++; return nil }))
		bus.Subscribe("vaccination.roster.changed", eventbus.HandlerFunc(func(context.Context, eventbus.Event) error { roster++; return nil }))
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
			t.Fatalf("outbox relay rejected %d message(s) as invalid envelope -- a cascade event does not conform to the domain-event schema", result.FailedCount)
		}
		return capacity, roster
	}

	repo := NewRepository(pool, 5*time.Second)

	// Scenario 1: first write (row_version 0 -> 1) emits BOTH capacity and roster.
	cfg, err := repo.UpsertOperatorAssignmentConfig(ctx, tenant, domain.OperatorAssignmentConfig{
		ParkID:                park,
		ActiveOperatorsPerDay: 1,
		DefaultOperatorID:     operator,
	})
	if err != nil {
		t.Fatalf("insert config (real production write path): %v", err)
	}
	if c, r := drainCascade(); c < 1 || r < 1 {
		t.Fatalf("first write: capacity=%d roster=%d, want both >=1", c, r)
	}

	// Scenario 2: default-operator-only change (N unchanged) emits ONLY roster.changed.
	cfg, err = repo.UpsertOperatorAssignmentConfig(ctx, tenant, domain.OperatorAssignmentConfig{
		ParkID:                park,
		ActiveOperatorsPerDay: 1,
		DefaultOperatorID:     operator2,
		RowVersion:            cfg.RowVersion,
	})
	if err != nil {
		t.Fatalf("default-operator-swap update: %v", err)
	}
	if c, r := drainCascade(); c != 0 || r < 1 {
		t.Fatalf("default-operator swap: capacity=%d roster=%d, want capacity=0 and roster>=1", c, r)
	}

	// Scenario 3: N-only change (default operator unchanged) emits ONLY capacity.changed.
	if _, err = repo.UpsertOperatorAssignmentConfig(ctx, tenant, domain.OperatorAssignmentConfig{
		ParkID:                park,
		ActiveOperatorsPerDay: 2,
		DefaultOperatorID:     operator2,
		RowVersion:            cfg.RowVersion,
	}); err != nil {
		t.Fatalf("N-change update: %v", err)
	}
	if c, r := drainCascade(); c < 1 || r != 0 {
		t.Fatalf("N change: capacity=%d roster=%d, want capacity>=1 and roster=0", c, r)
	}
}
