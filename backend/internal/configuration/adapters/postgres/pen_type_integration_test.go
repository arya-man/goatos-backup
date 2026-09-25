package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/configuration/domain"
	"github.com/vgoats/goatos/backend/internal/configuration/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// PEN TYPE IS SET ON THE PARTITION -- THE PEN THE FARM ACTUALLY WORKS -- AND NOWHERE ELSE
// (maintainer instructions 2026-09-22: configured, then "assignment will be per partition only not
// pen"), and the KINDS of pen are the farm's own Pen types register (2026-09-25, migration 000428).
//
// The things this pins, in the order they would break:
//
//  1. a pen type is authored on the register, and a partition takes it -- including a THIRD kind
//     the farm adds, which is the whole point of the register;
//  2. it round-trips: what the screen writes is what the reports read, and the partition list
//     shows the type's NAME, so a rename shows at once;
//  3. CLEARING it writes NULL rather than leaving the last answer standing;
//  4. a code nobody authored, or one the farm archived, is refused on the Pen type field;
//  5. a type still given to a pen cannot be removed (archive it instead).
func TestPenTypeIsConfiguredOnThePartitionFromThePenTypesRegister(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	seedConfigurationFixture(t, ctx, pool)

	repo := NewRepository(pool, 15*time.Second)
	write := func(key string) ports.WriteParams {
		return ports.WriteParams{TenantID: cfgTenant, ActorID: cfgActor, IdempotencyKey: key, TraceID: "trace-" + key}
	}

	for _, pt := range []map[string]any{
		{"name": "Elevated", "code": "elevated", "sort_order": 10},
		{"name": "Non-elevated", "code": "non_elevated", "sort_order": 20},
		{"name": "Slatted floor", "sort_order": 30}, // code left blank: made from the name
	} {
		if _, err := repo.Create(ctx, write("pen-type-"+pt["name"].(string)), domain.RegPenTypes, pt); err != nil {
			t.Fatalf("author pen type %v: %v", pt["name"], err)
		}
	}
	options, err := repo.Options(ctx, cfgTenant, domain.RegPenTypes)
	if err != nil {
		t.Fatalf("pen type options: %v", err)
	}
	var offered []string
	for _, option := range options {
		offered = append(offered, option.ID)
	}
	if strings.Join(offered, ",") != "elevated,non_elevated,slatted_floor" {
		t.Fatalf("the Partitions dropdown offers %v, want the register in its order", offered)
	}

	created, err := repo.Create(ctx, write("pen-type-create"), domain.RegPartitions, map[string]any{
		"park_id": cfgParkCBE, "pen_id": cfgShed, "label": "Part 9", "shed_type": "slatted_floor",
	})
	if err != nil {
		t.Fatalf("create a pen of the farm-added type: %v", err)
	}
	if created.Fields["shed_type"] != "slatted_floor" || created.Labels["shed_type"] != "Slatted floor" {
		t.Fatalf("created pen reads back %v / %q, want slatted_floor shown as Slatted floor", created.Fields["shed_type"], created.Labels["shed_type"])
	}
	stored := func() *string {
		var v *string
		if err := pool.QueryRow(ctx, `SELECT shed_type FROM shed_partitions WHERE tenant_id = $1::uuid AND shed_id = split_part($2, ':', 1)::uuid AND normalized_label = split_part($2, ':', 2)`, cfgTenant, created.ID).Scan(&v); err != nil {
			t.Fatalf("read the stored pen type: %v", err)
		}
		return v
	}
	if v := stored(); v == nil || *v != "slatted_floor" {
		t.Fatalf("stored pen type = %v, want slatted_floor", v)
	}

	// A rename changes the NAME everywhere and never the key the reports group by.
	renamed, err := repo.Update(ctx, write("pen-type-rename"), domain.RegPenTypes, "slatted_floor", map[string]any{"name": "Slatted"}, 0)
	if err != nil {
		t.Fatalf("rename the pen type: %v", err)
	}
	if renamed.ID != "slatted_floor" {
		t.Fatalf("a rename moved the key to %q", renamed.ID)
	}
	again, err := repo.Get(ctx, cfgTenant, domain.RegPartitions, created.ID)
	if err != nil {
		t.Fatalf("re-read the pen: %v", err)
	}
	if again.Labels["shed_type"] != "Slatted" {
		t.Fatalf("the pen shows %q after the rename, want Slatted", again.Labels["shed_type"])
	}

	// A type still given to a pen cannot be removed.
	if err := repo.Delete(ctx, write("pen-type-delete-used"), domain.RegPenTypes, "slatted_floor", 0); err == nil {
		t.Fatal("a pen type still given to a pen was removed")
	}

	flipped, err := repo.Update(ctx, write("pen-type-flip"), domain.RegPartitions, created.ID,
		map[string]any{"shed_type": "non_elevated"}, again.RowVersion)
	if err != nil {
		t.Fatalf("flip the pen type: %v", err)
	}
	if flipped.Fields["shed_type"] != "non_elevated" {
		t.Fatalf("flipped pen reads back %v, want non_elevated", flipped.Fields["shed_type"])
	}

	// CLEARED, not left standing.
	cleared, err := repo.Update(ctx, write("pen-type-clear"), domain.RegPartitions, created.ID,
		map[string]any{"shed_type": nil}, flipped.RowVersion)
	if err != nil {
		t.Fatalf("clear the pen type: %v", err)
	}
	if got, ok := cleared.Fields["shed_type"]; ok && got != nil {
		t.Fatalf("cleared pen still reads %v; a cleared type must be absent", got)
	}
	if v := stored(); v != nil {
		t.Fatalf("stored pen type = %v after clearing, want NULL", *v)
	}

	// A code nobody authored is refused ON THE FIELD, not as a database error.
	_, err = repo.Update(ctx, write("pen-type-bogus"), domain.RegPartitions, created.ID,
		map[string]any{"shed_type": "raised"}, cleared.RowVersion)
	var refErr *ports.RefError
	if !errors.As(err, &refErr) || refErr.Field != "shed_type" {
		t.Fatalf("an unauthored pen type = %v, want a RefError on shed_type", err)
	}

	// An ARCHIVED type cannot be given to a pen that does not already carry it.
	if _, err := repo.SetStatus(ctx, write("pen-type-archive"), domain.RegPenTypes, "slatted_floor", domain.StatusArchived, 0); err != nil {
		t.Fatalf("archive the pen type: %v", err)
	}
	_, err = repo.Update(ctx, write("pen-type-archived"), domain.RegPartitions, created.ID,
		map[string]any{"shed_type": "slatted_floor"}, cleared.RowVersion)
	if !errors.As(err, &refErr) || refErr.Field != "shed_type" {
		t.Fatalf("an archived pen type = %v, want a RefError on shed_type", err)
	}
}

// The register DEFINITION is what the screen renders. Pen types sit after Parks and before Pens
// (maintainer instruction 2026-09-25), and the Partitions pen-type column is a REFERENCE to that
// register -- never a closed list typed in Go, which is what made adding a kind of pen a code change.
func TestPartitionsTakeTheirPenTypeFromThePenTypesRegister(t *testing.T) {
	var order []string
	var column *domain.Column
	for _, register := range domain.Registers {
		if register.Group == domain.GroupFarmPlaces {
			order = append(order, register.Key)
		}
		if register.Key != domain.RegPartitions {
			continue
		}
		for i := range register.Columns {
			if register.Columns[i].Key == "shed_type" {
				column = &register.Columns[i]
			}
		}
	}
	if got := strings.Join(order, ","); got != "parks,pen_types,pens,partitions" {
		t.Fatalf("farm places rail = %s, want parks,pen_types,pens,partitions", got)
	}
	if column == nil {
		t.Fatal("the Partitions register has no pen-type column; there is then nowhere on the farm to set it")
	}
	if column.Type != domain.TypeRef || column.Ref != domain.RegPenTypes {
		t.Fatalf("pen type is a %q column referencing %q, want a reference to the Pen types register", column.Type, column.Ref)
	}
	if len(column.Options) != 0 {
		t.Fatalf("pen type carries a typed option list %v; the choices are the register's rows", column.Options)
	}
	if column.Required {
		t.Fatal("pen type must stay optional: not knowing yet is a real answer, and it is reported as unclassified")
	}
}
