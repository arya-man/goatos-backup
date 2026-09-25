package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/animalpurchase/app"
	"github.com/vgoats/goatos/backend/internal/animalpurchase/domain"
	"github.com/vgoats/goatos/backend/internal/animalpurchase/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestAnimalPurchaseOffersAndAcceptsAConfiguredThirdSpecies pins OPEN UP TO NEW SPECIES (maintainer
// decision 2026-09-25) on the real schema: the phone's inspection form offers the farm's
// Configuration species and genders, an animal of a configured third species and gender is
// recorded (the row lands past the loosened CHECKs), and a species the farm never added is refused.
func TestAnimalPurchaseOffersAndAcceptsAConfiguredThirdSpecies(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo, load := seedLoadWithCandidates(t, ctx, pool, 0)
	if _, err := pool.Exec(ctx, `
INSERT INTO species_lookup (tenant_id, species_code, name, sort_order) VALUES
($1::uuid, 'goat', 'Goat', 10), ($1::uuid, 'sheep', 'Sheep', 20), ($1::uuid, 'alpaca', 'Alpaca', 30)
ON CONFLICT DO NOTHING`, apTenant); err != nil {
		t.Fatalf("seed species: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO sex_lookup (tenant_id, sex_code, name, sort_order) VALUES
($1::uuid, 'female', 'Female', 10), ($1::uuid, 'male', 'Male', 20), ($1::uuid, 'castrated', 'Castrated male', 30)
ON CONFLICT DO NOTHING`, apTenant); err != nil {
		t.Fatalf("seed sexes: %v", err)
	}
	svc := app.NewService(repo, nil, nil)

	opts, err := svc.Options(ctx, apTenant)
	if err != nil {
		t.Fatalf("options: %v", err)
	}
	if len(opts.Species) != 3 || opts.Species[2].Value != "alpaca" || opts.Species[2].Label != "Alpaca" {
		t.Fatalf("species options = %+v, want goat, sheep, alpaca", opts.Species)
	}
	offered := false
	for _, q := range opts.Questionnaire {
		if q.ID == "sex" {
			for _, o := range q.Options {
				offered = offered || o.Value == "castrated"
			}
		}
	}
	if !offered {
		t.Fatal("the inspection's sex question does not offer the configured third gender")
	}

	record := func(species, key string) (domain.Candidate, error) {
		w := inspection("male", "Huacaya", 60, "20000000-0000-4000-8000-0000000000"+key)
		w.Answers["species"] = json.RawMessage(`"` + species + `"`)
		w.Answers["sex"] = json.RawMessage(`"castrated"`)
		return svc.AddCandidate(ctx, ports.AddCandidateParams{TenantID: apTenant, LoadID: load.LoadID, ActorID: apUser,
			QuestionnaireVersion: domain.QuestionnaireVersion, IdempotencyKey: "third-" + key, Write: w})
	}
	c, err := record("alpaca", "01")
	if err != nil {
		t.Fatalf("record a configured third species: %v", err)
	}
	var species, sex string
	if err := pool.QueryRow(ctx, `SELECT species, sex FROM animal_purchase_candidates WHERE candidate_id = $1::uuid`, c.CandidateID).Scan(&species, &sex); err != nil {
		t.Fatalf("read candidate: %v", err)
	}
	if species != "alpaca" || sex != "castrated" {
		t.Fatalf("stored %q/%q, want alpaca/castrated", species, sex)
	}

	_, err = record("camel", "02")
	var vErr *domain.ValidationError
	if !errors.As(err, &vErr) || vErr.Field != "species" {
		t.Fatalf("unknown species err = %v, want a species field error", err)
	}
}
