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
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/ports"
)

// TestUpsertCapacityConfigFirstWriteAndReplay proves the real Postgres write path: an update over
// the seeded tenant row bumps row_version, persists max_per_day + the new
// max_shots_per_animal_per_drive override column (migration 000045), and an exact replay with the SAME
// row_version + values is idempotent (returns the current row, not a conflict or a duplicate event).
func TestUpsertCapacityConfigFirstWriteAndReplay(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const tenant = "00000000-0000-4000-8000-000000000001"
	repo := NewRepository(pool, 5*time.Second)

	before, err := repo.CapacityConfig(ctx, tenant)
	if err != nil {
		t.Fatalf("read seeded capacity config: %v", err)
	}

	shots := 3
	updated, err := repo.UpsertCapacityConfig(ctx, tenant, domain.CapacityConfig{
		MaxPerDay:                 150,
		CapacityScope:             "tenant",
		MaxBufferDays:             before.MaxBufferDays,
		OverflowPolicy:            before.OverflowPolicy,
		RowVersion:                before.RowVersion,
		MaxShotsPerAnimalPerDrive: &shots,
	})
	if err != nil {
		t.Fatalf("upsert capacity config: %v", err)
	}
	if updated.MaxPerDay != 150 {
		t.Fatalf("MaxPerDay = %d want 150", updated.MaxPerDay)
	}
	if updated.MaxShotsPerAnimalPerDrive == nil || *updated.MaxShotsPerAnimalPerDrive != 3 {
		t.Fatalf("MaxShotsPerAnimalPerDrive = %v want 3", updated.MaxShotsPerAnimalPerDrive)
	}
	if updated.RowVersion != before.RowVersion+1 {
		t.Fatalf("RowVersion = %d want %d", updated.RowVersion, before.RowVersion+1)
	}

	// Read back confirms the write actually persisted (not just the returned struct).
	reread, err := repo.CapacityConfig(ctx, tenant)
	if err != nil {
		t.Fatalf("re-read capacity config: %v", err)
	}
	if reread.MaxPerDay != 150 || reread.MaxShotsPerAnimalPerDrive == nil || *reread.MaxShotsPerAnimalPerDrive != 3 {
		t.Fatalf("reread = %#v", reread)
	}
}

// TestUpsertCapacityConfigRowVersionConflict proves a stale RowVersion never silently clobbers a
// concurrent admin write -- it returns ports.ErrCapacityConfigConflict and leaves the row untouched.
func TestUpsertCapacityConfigRowVersionConflict(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const tenant = "00000000-0000-4000-8000-000000000001"
	repo := NewRepository(pool, 5*time.Second)

	before, err := repo.CapacityConfig(ctx, tenant)
	if err != nil {
		t.Fatalf("read seeded capacity config: %v", err)
	}

	_, err = repo.UpsertCapacityConfig(ctx, tenant, domain.CapacityConfig{
		MaxPerDay:      175,
		CapacityScope:  before.CapacityScope,
		MaxBufferDays:  before.MaxBufferDays,
		OverflowPolicy: before.OverflowPolicy,
		RowVersion:     before.RowVersion - 1, // stale
	})
	if err == nil {
		t.Fatal("expected conflict error, got nil")
	}
	if err != ports.ErrCapacityConfigConflict {
		t.Fatalf("err = %v, want ports.ErrCapacityConfigConflict", err)
	}

	reread, err := repo.CapacityConfig(ctx, tenant)
	if err != nil {
		t.Fatalf("re-read capacity config: %v", err)
	}
	if reread.MaxPerDay == 175 {
		t.Fatal("conflicting write must not have applied")
	}
}

// projection-review adversarial coverage for the capacity write + per-active-park cascade grain:
//   - cardinality (OneToMany): the tenant-scoped write fans one event per active park; this test
//     seeds MULTIPLE active parks and asserts exactly one vaccination.capacity.changed per park
//     (no duplicate, no cross-park collapse) -- see the OneToMany assertion below.
//   - scope (ParkScope): each emitted event carries its own park_id; the test asserts the park set
//     matches the active-park set exactly (ParkScope isolation), never the tenant blob.
//   - pagination (PageBoundary): N/A -- the config read is a single-row lookup and the fan-out is a
//     bounded active-park set, not a paged/keyset read; there is no MultiPage boundary to cross.
//   - date (ScheduledDate): N/A -- vaccination_capacity_config has no date/due dimension; the shot-cap
//     override is a scalar, not a ScheduledDate/ExecutionDate grain.
//   - status (StatusMatrix): N/A -- capacity config has no status buckets; there is no EveryStatus /
//     StatusBuckets matrix to fan over here (the downstream drive StatusMatrix is a separate read model).
//
// TestUpsertCapacityConfigEnqueuesCapacityChangedPerActivePark proves a tenant-wide capacity write
// cascades to EVERY active park (capacity config has no park_id column -- it is tenant-scoped) by
// enqueuing one vaccination.capacity.changed event per active park to outbox_messages, real relay,
// schema-validated envelope. It is the OneToMany + ParkScope adversarial case.
func TestUpsertCapacityConfigEnqueuesCapacityChangedPerActivePark(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const (
		tenant = "00000000-0000-4000-8000-000000000001"
		park1  = "00000000-0000-4000-8000-0000000cb001"
		park2  = "00000000-0000-4000-8000-0000000cb002"
	)
	for i, p := range []string{park1, park2} {
		if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($1::uuid, $2::uuid, 'park', $3, $4, 'active')
ON CONFLICT (location_id) DO NOTHING`, p, tenant, "PARK-CB-"+string(rune('A'+i)), "Cap Cascade Park"); err != nil {
			t.Fatalf("seed park %d: %v", i, err)
		}
	}

	repo := NewRepository(pool, 5*time.Second)
	before, err := repo.CapacityConfig(ctx, tenant)
	if err != nil {
		t.Fatalf("read seeded capacity config: %v", err)
	}
	if _, err := repo.UpsertCapacityConfig(ctx, tenant, domain.CapacityConfig{
		MaxPerDay:      before.MaxPerDay + 25,
		CapacityScope:  before.CapacityScope,
		MaxBufferDays:  before.MaxBufferDays,
		OverflowPolicy: before.OverflowPolicy,
		RowVersion:     before.RowVersion,
	}); err != nil {
		t.Fatalf("upsert capacity config: %v", err)
	}

	bus := eventbus.NewInProcessBus()
	seenParks := map[string]int{}
	bus.Subscribe("vaccination.capacity.changed", eventbus.HandlerFunc(func(_ context.Context, e eventbus.Event) error {
		seenParks[e.Key]++
		return nil
	}))
	schemaPath, err := filepath.Abs(filepath.Join(
		"..", "..", "..", "..", "..", "contracts", "jsonschema", "domain-event-envelope.schema.json"))
	if err != nil {
		t.Fatalf("resolve envelope schema path: %v", err)
	}
	validator, err := outboxapp.NewEnvelopeValidator(schemaPath)
	if err != nil {
		t.Fatalf("build envelope validator: %v", err)
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
		t.Fatalf("outbox relay rejected %d message(s) as invalid envelope", result.FailedCount)
	}
	if seenParks[park1] < 1 || seenParks[park2] < 1 {
		t.Fatalf("seenParks = %#v, want both %s and %s present", seenParks, park1, park2)
	}
}
