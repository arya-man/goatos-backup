package postgres

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
)

// ASSUMED VALUE BY WEIGHT WHEN WE HAVE IT (maintainer decision 2026-09-25). A load whose EVERY
// live animal has a weight -- its own latest scan, or the latest whole-pen weigh of the pen it
// stands in -- is valued at sum(weight x the web-configured price per kg for that animal's
// species, stage and sex) from growth_sale_price_assumptions. One animal without a weight, or an
// animal whose stage and sex (and species) has no price, and the load keeps the per-animal
// basis, saying why. Read-time only: nothing stored is rewritten.
func TestLoadwiseAssumedValueIsPricedByWeightOnlyWhenEveryAnimalIsWeighedAndPriced(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	fx := seedLoadwiseFixture(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)
	cbe := parkIDByCode(t, ctx, pool, "CBE")

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v\n%s", err, sql)
		}
	}
	var operator = "11111111-0000-4000-8000-00000000c0de"
	var shedP, penQ, campaign, scanBucket, penBucket, proof string
	if err := pool.QueryRow(ctx, `
INSERT INTO locations (tenant_id, location_type, name, parent_location_id, status, display_order)
VALUES ($1::uuid, 'shed', 'LW Weigh Shed', $2::uuid, 'active', 900) RETURNING location_id::text`, testTenant, cbe).Scan(&shedP); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
INSERT INTO locations (tenant_id, location_type, name, parent_location_id, status, display_order)
VALUES ($1::uuid, 'shed', 'LW Whole Pen', $2::uuid, 'active', 901) RETURNING location_id::text`, testTenant, cbe).Scan(&penQ); err != nil {
		t.Fatal(err)
	}
	exec(`
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'operator', 'park', $3::uuid, 'active', now())
ON CONFLICT DO NOTHING`, testTenant, operator, cbe)
	if err := pool.QueryRow(ctx, `
INSERT INTO weighing_campaigns (tenant_id, park_id, period_start_date, period_end_date, start_business_date, status, planned_cap_per_day, operator_user_id, created_by)
VALUES ($1::uuid, $2::uuid, '2026-09-14', '2026-09-20', '2026-09-14', 'completed', 100, $3::uuid, $3::uuid) RETURNING campaign_id::text`,
		testTenant, cbe, operator).Scan(&campaign); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
INSERT INTO weighing_campaign_sheds (campaign_id, tenant_id, location_id, location_type, display_name, weighing_category, operator_user_id, park_id, start_business_date)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'shed', 'LW Weigh Shed', 'individual_animal', $4::uuid, $5::uuid, '2026-09-14') RETURNING campaign_shed_id::text`,
		campaign, testTenant, shedP, operator, cbe).Scan(&scanBucket); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
INSERT INTO weighing_campaign_sheds (campaign_id, tenant_id, location_id, location_type, display_name, weighing_category, operator_user_id, park_id, start_business_date)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'shed', 'LW Whole Pen', 'per_shed_partition', $4::uuid, $5::uuid, '2026-09-14') RETURNING campaign_shed_id::text`,
		campaign, testTenant, penQ, operator, cbe).Scan(&penBucket); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
INSERT INTO proof_artifacts (tenant_id, storage_provider, object_key, mime_type, upload_state, scope_type, scope_id, subject_type, subject_id, proof_type, uploaded_by, uploaded_at)
VALUES ($1::uuid, 'local', 'lw-weight-test', 'video/mp4', 'completed', 'shed', $2::uuid, 'shed', $2::uuid, 'video', $3::uuid, now()) RETURNING proof_id::text`,
		testTenant, shedP, operator).Scan(&proof); err != nil {
		t.Fatal(err)
	}

	var loadX string
	if err := pool.QueryRow(ctx, `
INSERT INTO procurement_loads (tenant_id, source_party_id, purchase_date, arrived_on, status, idempotency_key, expected_count, animal_cost)
SELECT tenant_id, source_party_id, '2026-09-01', '2026-09-02', status, 'lw-load-weight', 3, 30000
FROM procurement_loads WHERE tenant_id = $1 AND load_id = $2::uuid
RETURNING load_id::text`, testTenant, fx.loadA).Scan(&loadX); err != nil {
		t.Fatal(err)
	}
	// Three live K3 goats: two scanned in LW Weigh Shed, one standing alone in LW Whole Pen.
	goat := func(i int, species, sex, shed, tag string) string {
		var id string
		if err := pool.QueryRow(ctx, `
INSERT INTO goats (tenant_id, species, sex, management_stage, lifecycle_status, custodian_party_id, park_id, shed_id)
SELECT tenant_id, $3, $4, 'K3', 'alive', source_party_id, $5::uuid, $6::uuid
FROM procurement_loads WHERE tenant_id = $1 AND load_id = $2::uuid
RETURNING goat_id::text`, testTenant, loadX, species, sex, cbe, shed).Scan(&id); err != nil {
			t.Fatal(err)
		}
		exec(`
INSERT INTO procurement_load_goats (tenant_id, load_id, goat_id, selection_state, current_state, intake_accepted_at)
VALUES ($1, $2::uuid, $3::uuid, 'accepted_herd_intake', 'accepted_herd_intake', '2026-09-02T10:00:00Z')`, testTenant, loadX, id)
		if tag != "" {
			exec(`
INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, normalizer_version)
VALUES ($1::uuid, $2::uuid, 'animal_identifier_1', $3, upper(btrim($3)), 'tenant', true, 'active', now(), 'test')`, testTenant, id, tag)
		}
		return id
	}
	goat(1, "goat", "male", shedP, "lw-tag-1")
	goat(2, "goat", "female", shedP, "lw-tag-2")
	goat(3, "goat", "male", penQ, "")
	scan := func(tag string, kg float64, at, status string) {
		exec(`
INSERT INTO weighing_observations (tenant_id, campaign_id, campaign_shed_id, scanned_identifier, weight_kg, proof_artifact_id, recorded_by, idempotency_key, accepted_at, submitted_at, verification_status)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6::uuid, $7::uuid, $8, $9::timestamptz, $9::timestamptz, $10)`,
			testTenant, campaign, scanBucket, tag, kg, proof, operator, fmt.Sprintf("lw:%s:%s", tag, at), at, status)
	}
	// Prices set on the web: goat default 425 (seeded), K3 male override 500 (from 2026-09-10).
	exec(`INSERT INTO growth_sale_price_assumptions (tenant_id, species, management_stage, sex, price_per_kg_inr, effective_from, set_by)
VALUES ($1::uuid, 'goat', 'K3', 'male', 500, '2026-09-10', 'test')`, testTenant)

	read := func() domain.LoadwiseLoad {
		t.Helper()
		out, err := repo.LoadwiseSales(ctx, testTenant, "", 60)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range out.Loads {
			if l.LoadID == loadX {
				return l
			}
		}
		t.Fatalf("load %s missing", loadX)
		return domain.LoadwiseLoad{}
	}

	// 1. Animal 3 has no weight yet: per-animal basis, and the sentence says why.
	scan("lw-tag-1", 20, "2026-09-15T05:00:00Z", "verified")
	scan("lw-tag-2", 18, "2026-09-15T05:00:00Z", "pending")
	one := read()
	if one.AssumedValueMethod != domain.AssumedValueMethodPerAnimal {
		t.Fatalf("an unweighed animal must keep the per-animal basis: method=%q basis=%q", one.AssumedValueMethod, one.AssumedValueBasis)
	}
	if !strings.Contains(one.AssumedValueBasis, "1 of 3 animals has no weight yet") {
		t.Fatalf("the basis must say why weight was not used: %q", one.AssumedValueBasis)
	}

	// 2. The whole pen is weighed (1 animal, 22 kg) and a newer scan of tag 1 (a rework, which is
	// NOT trusted and must not replace the verified 20 kg). Every animal weighed + priced -> weight.
	exec(`
INSERT INTO weighing_shed_observations (tenant_id, campaign_id, campaign_shed_id, weight_kg, average_weight_kg, animal_count, proof_artifact_id, recorded_by, idempotency_key, accepted_at, verification_status)
VALUES ($1::uuid, $2::uuid, $3::uuid, 22, 22, 1, $4::uuid, $5::uuid, 'lw-pen-q', '2026-09-16T05:00:00Z', 'verified')`,
		testTenant, campaign, penBucket, proof, operator)
	scan("lw-tag-1", 99, "2026-09-17T05:00:00Z", "rework")
	all := read()
	// 20 kg x 500 (K3 male override) + 18 kg x 425 (goat default) + 22 kg x 500 = 28650.
	if all.AssumedValueMethod != domain.AssumedValueMethodWeight || all.AssumedValue == nil || math.Abs(*all.AssumedValue-28650) > 0.01 {
		t.Fatalf("every animal weighed and priced must value by weight: method=%q value=%v basis=%q",
			all.AssumedValueMethod, all.AssumedValue, all.AssumedValueBasis)
	}
	if want := "3 animals · 60 kg (latest weights) × ₹/kg by stage and sex = ₹28,650"; all.AssumedValueBasis != want {
		t.Fatalf("basis = %q, want %q", all.AssumedValueBasis, want)
	}
	if all.ProfitLoss == nil || math.Abs(*all.ProfitLoss-(28650-30000)) > 0.01 {
		t.Fatalf("profit must carry the weight-based stock: %v", all.ProfitLoss)
	}

	// 3. A species with NO price at all: the sheep default is gone, so a weighed sheep cannot be
	// priced by weight and the load falls back, naming the combination.
	exec(`DELETE FROM growth_sale_price_assumptions WHERE tenant_id = $1::uuid AND species = 'sheep'`, testTenant)
	sheep := goat(4, "sheep", "female", shedP, "lw-tag-4")
	_ = sheep
	exec(`UPDATE procurement_loads SET expected_count = 4 WHERE load_id = $1::uuid`, loadX)
	scan("lw-tag-4", 25, "2026-09-15T06:00:00Z", "verified")
	unpriced := read()
	if unpriced.AssumedValueMethod != domain.AssumedValueMethodPerAnimal {
		t.Fatalf("an animal with no price per kg must keep the per-animal basis: %q", unpriced.AssumedValueBasis)
	}
	if !strings.Contains(unpriced.AssumedValueBasis, "no price per kg set for Sheep K3 female") {
		t.Fatalf("the basis must name the unpriced combination: %q", unpriced.AssumedValueBasis)
	}
}
