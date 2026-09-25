package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/configuration/domain"
	"github.com/vgoats/goatos/backend/internal/configuration/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// BREEDS ARE PER FARM (maintainer decision 2026-09-25, migration 000442). A breed is added on
// Configuration > Items & settings like a park or a species, and it is that farm's alone: another
// farm neither sees it nor is blocked from a breed of the same name. A breed must name one of the
// farm's species, and a rename carries along the animals that name the breed only as text -- which
// is how every animal registered on the web or born on the phone names it.
func TestBreedsAreEachFarmsOwnList(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	seedConfigurationFixture(t, ctx, pool)
	repo := NewRepository(pool, 15*time.Second)
	const otherFarm = "0c0c0c0c-0000-4000-8000-0000000000f2"
	if _, err := pool.Exec(ctx, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'Second farm', 'active')`, otherFarm); err != nil {
		t.Fatalf("seed second farm: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO species_lookup (tenant_id, species_code, name, sort_order, is_builtin) VALUES ($1::uuid,'goat','Goat',10,true)`, otherFarm); err != nil {
		t.Fatalf("seed second farm species: %v", err)
	}
	write := func(tenant, key string) ports.WriteParams {
		return ports.WriteParams{TenantID: tenant, ActorID: cfgActor, IdempotencyKey: key, TraceID: "trace-" + key}
	}
	breedsOf := func(tenant string) []string {
		t.Helper()
		page, err := repo.List(ctx, tenant, domain.RegBreeds, ports.ListParams{Status: "all", Limit: 100})
		if err != nil {
			t.Fatalf("list breeds: %v", err)
		}
		var names []string
		for _, r := range page.Rows {
			names = append(names, r.Display)
		}
		return names
	}

	// The migrated catalogue belongs to the farm that existed then; a farm created later starts
	// with its own, empty list rather than someone else's.
	if got := breedsOf(cfgTenant); len(got) != 0 {
		t.Fatalf("a farm created after the migration starts with no breeds, got %v", got)
	}

	boer, err := repo.Create(ctx, write(cfgTenant, "breed-boer"), domain.RegBreeds, map[string]any{"name": "Boer", "species": "goat"})
	if err != nil {
		t.Fatalf("add a breed on Configuration: %v", err)
	}
	if boer.Counts["animals"] != 1 {
		t.Fatalf("the fixture's Boer animal names the breed by text and must count, got %v", boer.Counts)
	}
	if _, err := repo.Create(ctx, write(cfgTenant, "breed-boer-again"), domain.RegBreeds, map[string]any{"name": "Boer", "species": "goat"}); err == nil {
		t.Fatal("the same farm cannot hold Boer twice for one species")
	}
	if _, err := repo.Create(ctx, write(cfgTenant, "breed-alpaca"), domain.RegBreeds, map[string]any{"name": "Huacaya", "species": "alpaca"}); err == nil {
		t.Fatal("a breed must name one of the farm's species; alpaca is not one yet")
	}

	// Another farm may use the same name, and never sees the first farm's breed.
	if _, err := repo.Create(ctx, write(otherFarm, "breed-boer-2"), domain.RegBreeds, map[string]any{"name": "Boer", "species": "goat"}); err != nil {
		t.Fatalf("another farm may have its own Boer: %v", err)
	}
	if got := breedsOf(otherFarm); len(got) != 1 {
		t.Fatalf("the second farm sees only its own breed, got %v", got)
	}

	// A rename carries the animal that names the breed only as text.
	if _, err := repo.Update(ctx, write(cfgTenant, "breed-rename"), domain.RegBreeds, boer.ID, map[string]any{"name": "Boer Cross"}, boer.RowVersion); err != nil {
		t.Fatalf("rename breed: %v", err)
	}
	var breed string
	if err := pool.QueryRow(ctx, `SELECT breed FROM goats WHERE goat_id = $1::uuid`, cfgGoat).Scan(&breed); err != nil {
		t.Fatalf("read goat: %v", err)
	}
	if breed != "Boer Cross" {
		t.Fatalf("the animal registered as Boer must follow the rename, got %q", breed)
	}
	var otherName string
	if err := pool.QueryRow(ctx, `SELECT canonical_name FROM breeds WHERE tenant_id = $1::uuid`, otherFarm).Scan(&otherName); err != nil || otherName != "Boer" {
		t.Fatalf("renaming one farm's breed must not touch another's, got %q (%v)", otherName, err)
	}
}
