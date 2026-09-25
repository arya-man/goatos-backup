package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/procurement/app"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
)

// TestProcurementLoadAcceptsAConfiguredThirdSpecies pins OPEN UP TO NEW SPECIES (maintainer decision
// 2026-09-25) on the real schema and the real repository: once the farm adds a species and a gender
// on Configuration > Items & settings, the procurement service puts an animal of that species and
// gender on a load (and the row lands), while a species the farm never configured is refused.
func TestProcurementLoadAcceptsAConfiguredThirdSpecies(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedProcurementCommon(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	if _, err := pool.Exec(ctx, `
INSERT INTO species_lookup (tenant_id, species_code, name, sort_order) VALUES ($1::uuid, 'alpaca', 'Alpaca', 30)
ON CONFLICT DO NOTHING`, testTenant); err != nil {
		t.Fatalf("seed species: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO sex_lookup (tenant_id, sex_code, name, sort_order) VALUES ($1::uuid, 'castrated', 'Castrated male', 30)
ON CONFLICT DO NOTHING`, testTenant); err != nil {
		t.Fatalf("seed sex: %v", err)
	}
	load := createProcurementLoad(t, ctx, repo, "third-species-load", 2)
	svc := app.NewService(repo)

	goat, err := svc.AddGoatToLoad(ctx, ports.AddGoatToLoad{
		TenantID:          testTenant,
		LoadID:            load.LoadID,
		AnimalIdentifier1: strPtr("ALPACA-1"),
		Species:           "alpaca",
		Sex:               "castrated",
		ProofRefs:         []byte("[]"),
		Metadata:          []byte("{}"),
		IdempotencyKey:    "third-species-goat",
	})
	if err != nil {
		t.Fatalf("add a configured third species: %v", err)
	}
	var species, sex string
	if err := pool.QueryRow(ctx, `SELECT species, sex FROM goats WHERE goat_id = $1::uuid`, goat.GoatID).Scan(&species, &sex); err != nil {
		t.Fatalf("read goat: %v", err)
	}
	if species != "alpaca" || sex != "castrated" {
		t.Fatalf("stored %q/%q, want alpaca/castrated", species, sex)
	}

	_, err = svc.AddGoatToLoad(ctx, ports.AddGoatToLoad{
		TenantID:          testTenant,
		LoadID:            load.LoadID,
		AnimalIdentifier1: strPtr("CAMEL-1"),
		Species:           "camel",
		Sex:               "female",
		ProofRefs:         []byte("[]"),
		Metadata:          []byte("{}"),
		IdempotencyKey:    "unknown-species-goat",
	})
	var appErr *app.Error
	if !errors.As(err, &appErr) || appErr.Code != "invalid_species" {
		t.Fatalf("unknown species err = %v, want invalid_species", err)
	}
}
