package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/animalvocab"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestConfiguredThirdSpeciesAndSexAreStorableEverywhere pins migration 000432 (OPEN UP TO NEW
// SPECIES, maintainer decision 2026-09-25): once a farm adds a third species and a third gender on
// Configuration > Items & settings, an animal of that species/gender can be stored in the register,
// on a purchase candidate, on a sale-price row and as a vaccination rule selector -- none of those
// tables may still pin goat/sheep or female/male -- while a BLANK value is still refused, and the
// shared reader lists the new codes beside the built-ins.
func TestConfiguredThirdSpeciesAndSexAreStorableEverywhere(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)

	var tenantID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO tenants (tenant_id, name, status) VALUES (gen_random_uuid(), 'third-species-tenant', 'active') RETURNING tenant_id::text`,
	).Scan(&tenantID); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", strings.TrimSpace(strings.SplitN(sql, "\n", 2)[0]), err)
		}
	}
	// The tenant's lookups: the four built-ins plus a configured third species and gender.
	exec(`INSERT INTO species_lookup (tenant_id, species_code, name, sort_order, is_builtin) VALUES
($1::uuid, 'goat', 'Goat', 10, true), ($1::uuid, 'sheep', 'Sheep', 20, true), ($1::uuid, 'alpaca', 'Alpaca', 30, false),
($1::uuid, 'camel', 'Camel', 40, false)`, tenantID)
	exec(`UPDATE species_lookup SET status = 'archived' WHERE tenant_id = $1::uuid AND species_code = 'camel'`, tenantID)
	exec(`INSERT INTO sex_lookup (tenant_id, sex_code, name, sort_order, is_builtin) VALUES
($1::uuid, 'female', 'Female', 10, true), ($1::uuid, 'male', 'Male', 20, true), ($1::uuid, 'castrated', 'Castrated male', 30, false)`, tenantID)

	vocab, err := animalvocab.Load(ctx, pool, tenantID)
	if err != nil {
		t.Fatalf("load vocabulary: %v", err)
	}
	if got := strings.Join(animalvocab.Codes(vocab.Species), ","); got != "goat,sheep,alpaca" {
		t.Fatalf("active species = %s; want goat,sheep,alpaca (an archived species is not offered)", got)
	}
	if got := strings.Join(animalvocab.Codes(vocab.Sexes), ","); got != "female,male,castrated" {
		t.Fatalf("active sexes = %s", got)
	}
	// A tenant with no lookup rows reads the built-ins.
	unconfigured, err := animalvocab.Load(ctx, pool, "00000000-0000-4000-8000-00000000dead")
	if err != nil {
		t.Fatalf("load unconfigured vocabulary: %v", err)
	}
	if got := strings.Join(animalvocab.Codes(unconfigured.Species), ","); got != "goat,sheep" {
		t.Fatalf("unconfigured species = %s", got)
	}

	// The register.
	var partyID string
	if err := pool.QueryRow(ctx, `INSERT INTO parties (party_id, party_type, display_name, status)
VALUES (gen_random_uuid(), 'org', 'Third species custodian', 'active') RETURNING party_id::text`).Scan(&partyID); err != nil {
		t.Fatalf("insert party: %v", err)
	}
	exec(`INSERT INTO goats (tenant_id, species, sex, lifecycle_status, custodian_party_id)
VALUES ($1::uuid, 'alpaca', 'castrated', 'alive', $2::uuid)`, tenantID, partyID)

	// A purchase candidate.
	var loadID string
	if err := pool.QueryRow(ctx, `INSERT INTO animal_purchase_loads (tenant_id, load_ref, vendor_id, vendor_name, farm_label, idempotency_key)
VALUES ($1::uuid, 'L-1', gen_random_uuid(), 'Vendor', 'CBE', 'third-species-load') RETURNING load_id::text`, tenantID).Scan(&loadID); err != nil {
		t.Fatalf("insert load: %v", err)
	}
	exec(`INSERT INTO animal_purchase_candidates (tenant_id, load_id, seq_no, species, sex, condition, video_proof_ref, idempotency_key)
VALUES ($1::uuid, $2::uuid, 1, 'alpaca', 'castrated', 'healthy', 'proof-1', 'third-species-candidate')`, tenantID, loadID)
	if _, err := pool.Exec(ctx, `INSERT INTO animal_purchase_candidates (tenant_id, load_id, seq_no, species, sex, condition, video_proof_ref, idempotency_key)
VALUES ($1::uuid, $2::uuid, 2, '  ', 'female', 'healthy', 'proof-2', 'blank-species-candidate')`, tenantID, loadID); err == nil {
		t.Fatal("a blank candidate species must still be refused")
	}

	// A sale-price assumption: a species default and a (stage, sex) override for the third gender.
	exec(`INSERT INTO growth_sale_price_assumptions (tenant_id, species, price_per_kg_inr, effective_from, set_by)
VALUES ($1::uuid, 'alpaca', 500, DATE '2026-09-25', 'test')`, tenantID)
	exec(`INSERT INTO growth_sale_price_assumptions (tenant_id, species, management_stage, sex, price_per_kg_inr, effective_from, set_by)
VALUES ($1::uuid, 'alpaca', 'K3', 'castrated', 520, DATE '2026-09-25', 'test')`, tenantID)
	if _, err := pool.Exec(ctx, `INSERT INTO growth_sale_price_assumptions (tenant_id, species, price_per_kg_inr, effective_from, set_by)
VALUES ($1::uuid, '', 500, DATE '2026-09-26', 'test')`, tenantID); err == nil {
		t.Fatal("a blank sale-price species must still be refused")
	}

	// The vaccination rule selector's sex column no longer pins female/male/all.
	var def string
	if err := pool.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = 'protocol_rule_dimensions_sex_check'`).Scan(&def); err != nil {
		t.Fatalf("read protocol_rule_dimensions_sex_check: %v", err)
	}
	if strings.Contains(def, "'female'") || !strings.Contains(def, "btrim") {
		t.Fatalf("protocol_rule_dimensions_sex_check = %s; want a not-blank guard", def)
	}
}
