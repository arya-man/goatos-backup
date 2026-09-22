package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/configuration/domain"
	"github.com/vgoats/goatos/backend/internal/configuration/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// PEN TYPE IS SET ON THE PARTITION -- THE PEN THE FARM ACTUALLY WORKS -- AND NOWHERE ELSE
// (maintainer instructions 2026-09-22: configured, then "assignment will be per partition only not
// pen"). Castro 1 and Castro 2 are different pens inside one building and can be built
// differently, which a single value on the building cannot say.
//
// It used to be inferred inside the weighing query from a pen's notes and, failing that, from a
// hardcoded list of building names -- so a pen built after that list was written could not be
// classified at all, and nobody on the farm could correct one that was classified wrong.
//
// The three things this pins, in the order they would break:
//
//  1. it round-trips: what the screen writes is what the reports read;
//  2. CLEARING it writes NULL rather than leaving the last answer standing -- the reports then
//     report the pen as unclassified, which is the whole point of being able to clear it;
//  3. the column refuses a value that is neither side, so a typo cannot create a third kind of
//     pen that silently disappears from both bars.
func TestPenTypeIsConfiguredOnThePartitionAndClearsToUnclassified(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	seedConfigurationFixture(t, ctx, pool)

	repo := NewRepository(pool, 15*time.Second)
	write := func(key string) ports.WriteParams {
		return ports.WriteParams{TenantID: cfgTenant, ActorID: cfgActor, IdempotencyKey: key, TraceID: "trace-" + key}
	}

	created, err := repo.Create(ctx, write("pen-type-create"), domain.RegPartitions, map[string]any{
		"park_id": cfgParkCBE, "pen_id": cfgShed, "label": "Part 9", "shed_type": "elevated",
	})
	if err != nil {
		t.Fatalf("create an elevated pen: %v", err)
	}
	if created.Fields["shed_type"] != "elevated" {
		t.Fatalf("created pen reads back %v, want elevated", created.Fields["shed_type"])
	}

	var stored *string
	if err := pool.QueryRow(ctx, `SELECT shed_type FROM shed_partitions WHERE tenant_id = $1::uuid AND shed_id = split_part($2, ':', 1)::uuid AND normalized_label = split_part($2, ':', 2)`, cfgTenant, created.ID).Scan(&stored); err != nil {
		t.Fatalf("read the stored pen type: %v", err)
	}
	if stored == nil || *stored != "elevated" {
		t.Fatalf("stored pen type = %v, want elevated", stored)
	}

	flipped, err := repo.Update(ctx, write("pen-type-flip"), domain.RegPartitions, created.ID,
		map[string]any{"shed_type": "non_elevated"}, created.RowVersion)
	if err != nil {
		t.Fatalf("flip the pen type: %v", err)
	}
	if flipped.Fields["shed_type"] != "non_elevated" {
		t.Fatalf("flipped pen reads back %v, want non_elevated", flipped.Fields["shed_type"])
	}

	// CLEARED, not left standing. A pen the farm un-types is reported as unclassified and is
	// counted into neither side; leaving the old value would make the clear look broken.
	cleared, err := repo.Update(ctx, write("pen-type-clear"), domain.RegPartitions, created.ID,
		map[string]any{"shed_type": nil}, flipped.RowVersion)
	if err != nil {
		t.Fatalf("clear the pen type: %v", err)
	}
	if got, ok := cleared.Fields["shed_type"]; ok && got != nil {
		t.Fatalf("cleared pen still reads %v; a cleared type must be absent", got)
	}
	if err := pool.QueryRow(ctx, `SELECT shed_type FROM shed_partitions WHERE tenant_id = $1::uuid AND shed_id = split_part($2, ':', 1)::uuid AND normalized_label = split_part($2, ':', 2)`, cfgTenant, created.ID).Scan(&stored); err != nil {
		t.Fatalf("read the cleared pen type: %v", err)
	}
	if stored != nil {
		t.Fatalf("stored pen type = %v after clearing, want NULL", *stored)
	}

	// A third kind of pen cannot be invented by a typo: it would pass silently through every
	// reader and then vanish from both bars, which reads as missing animals.
	if _, err := repo.Update(ctx, write("pen-type-bogus"), domain.RegPartitions, created.ID,
		map[string]any{"shed_type": "raised"}, cleared.RowVersion); err == nil {
		t.Fatal("a pen type outside the two configured values was accepted")
	}
}

// The register DEFINITION is what the screen renders, so the two options and their farm copy
// are part of the contract, not decoration. "Non-elevated" is the farm's word; the retired
// weighing vocabulary called this side Crown/Ground and must not come back.
func TestPartitionsRegisterOffersExactlyTheTwoPenTypes(t *testing.T) {
	var column *domain.Column
	for _, register := range domain.Registers {
		if register.Key != domain.RegPartitions {
			continue
		}
		for i := range register.Columns {
			if register.Columns[i].Key == "shed_type" {
				column = &register.Columns[i]
			}
		}
	}
	if column == nil {
		t.Fatal("the Partitions register has no pen-type column; there is then nowhere on the farm to set it")
	}
	if column.Type != domain.TypeEnum {
		t.Fatalf("pen type is a %q column, want an enum: free text would let a typo invent a third kind of pen", column.Type)
	}
	if column.Required {
		t.Fatal("pen type must stay optional: not knowing yet is a real answer, and it is reported as unclassified")
	}
	want := map[string]string{"elevated": "Elevated", "non_elevated": "Non-elevated"}
	if len(column.Options) != len(want) {
		t.Fatalf("pen type offers %d options, want exactly %d", len(column.Options), len(want))
	}
	for _, option := range column.Options {
		label, ok := want[option.Value]
		if !ok {
			t.Fatalf("unexpected pen type %q", option.Value)
		}
		if option.Label != label {
			t.Fatalf("pen type %q is labelled %q, want %q", option.Value, option.Label, label)
		}
	}
}
