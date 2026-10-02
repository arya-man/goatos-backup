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

// ASSUMED VALUE = LATEST WEIGHT x SALES CONFIG ₹/KG (maintainer decision 2026-10-02). Each live
// animal of a load is valued at its latest weight -- its own latest scan, or the latest whole-pen
// weigh of the pen it stands in -- x the ₹/kg of its stage-and-sex bucket on Sales Config's Farm
// valuation. An animal with no weight yet carries the load's current average weight; an animal
// whose stage no valuation stage names is left out and named. Read-time only.
func TestLoadwiseAssumedValueIsLatestWeightTimesTheSalesConfigPrice(t *testing.T) {
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
	animal := func(species, stage, sex, shed, tag string) string {
		var id string
		if err := pool.QueryRow(ctx, `
INSERT INTO goats (tenant_id, species, sex, management_stage, lifecycle_status, custodian_party_id, park_id, shed_id)
SELECT tenant_id, $7, $3, $4, 'alive', source_party_id, $5::uuid, $6::uuid
FROM procurement_loads WHERE tenant_id = $1 AND load_id = $2::uuid
RETURNING goat_id::text`, testTenant, loadX, sex, stage, cbe, shed, species).Scan(&id); err != nil {
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
	goat := func(stage, sex, shed, tag string) string { return animal("goat", stage, sex, shed, tag) }
	firstGoat := goat("K3", "male", shedP, "lw-tag-1")
	goat("K3", "female", shedP, "lw-tag-2")
	goat("K3", "male", penQ, "")
	scan := func(tag string, kg float64, at, status string) {
		exec(`
INSERT INTO weighing_observations (tenant_id, campaign_id, campaign_shed_id, scanned_identifier, weight_kg, proof_artifact_id, recorded_by, idempotency_key, accepted_at, submitted_at, verification_status)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6::uuid, $7::uuid, $8, $9::timestamptz, $9::timestamptz, $10)`,
			testTenant, campaign, scanBucket, tag, kg, proof, operator, fmt.Sprintf("lw:%s:%s", tag, at), at, status)
	}
	// Sales Config's Farm valuation: K3 goat male 500 ₹/kg, goat female 400; sheep male 430, sheep
	// female 380. A Weighing Assumptions
	// price must play no part, so one is set and must be ignored.
	exec(`
INSERT INTO sales_valuation_assumptions (tenant_id, buckets, stages)
VALUES ($1::uuid,
  '[{"bucket":"K3_goat_female","label":"K3 · Goat · Female","fixed_weight_kg":15,"price_per_kg":400,"display_order":1},
    {"bucket":"K3_goat_male","label":"K3 · Goat · Male","fixed_weight_kg":15,"price_per_kg":500,"display_order":2},
    {"bucket":"K3_sheep_female","label":"K3 · Sheep · Female","fixed_weight_kg":15,"price_per_kg":380,"display_order":3},
    {"bucket":"K3_sheep_male","label":"K3 · Sheep · Male","fixed_weight_kg":15,"price_per_kg":430,"display_order":4}]'::jsonb,
  '[{"stage":"K3","label":"K3","display_order":1,"matches":["K3"]}]'::jsonb)
ON CONFLICT (tenant_id) DO UPDATE SET buckets = EXCLUDED.buckets, stages = EXCLUDED.stages`, testTenant)
	exec(`INSERT INTO growth_sale_price_assumptions (tenant_id, species, management_stage, sex, price_per_kg_inr, effective_from, set_by)
VALUES ($1::uuid, 'goat', 'K3', 'male', 9999, '2026-09-10', 'test')`, testTenant)

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

	// 1. Animal 3 has no weight yet: it carries the load's average (20 + 18) / 2 = 19 kg.
	// 20 x 500 + 18 x 400 + 19 x 500 = 26700. The fixed_weight_kg on the bucket is not used.
	scan("lw-tag-1", 20, "2026-09-15T05:00:00Z", "verified")
	scan("lw-tag-2", 18, "2026-09-15T05:00:00Z", "pending")
	one := read()
	if one.AssumedValue == nil || math.Abs(*one.AssumedValue-26700) > 0.01 {
		t.Fatalf("an unweighed animal must carry the load average: value=%v basis=%q", one.AssumedValue, one.AssumedValueBasis)
	}
	if want := "3 animals × latest weight × ₹/kg by stage and sex on Sales Config = ₹26,700"; one.AssumedValueBasis != want {
		t.Fatalf("basis = %q, want %q", one.AssumedValueBasis, want)
	}

	// 2. The whole pen is weighed (1 animal, 22 kg) and a newer scan of tag 1 is a rework, which
	// is NOT trusted and must not replace the verified 20 kg: 20 x 500 + 18 x 400 + 22 x 500.
	exec(`
INSERT INTO weighing_shed_observations (tenant_id, campaign_id, campaign_shed_id, weight_kg, average_weight_kg, animal_count, proof_artifact_id, recorded_by, idempotency_key, accepted_at, verification_status)
VALUES ($1::uuid, $2::uuid, $3::uuid, 22, 22, 1, $4::uuid, $5::uuid, 'lw-pen-q', '2026-09-16T05:00:00Z', 'verified')`,
		testTenant, campaign, penBucket, proof, operator)
	scan("lw-tag-1", 99, "2026-09-17T05:00:00Z", "rework")
	all := read()
	if all.AssumedValue == nil || math.Abs(*all.AssumedValue-28200) > 0.01 {
		t.Fatalf("every animal weighed: value=%v basis=%q", all.AssumedValue, all.AssumedValueBasis)
	}
	if want := "3 animals × latest weight × ₹/kg by stage and sex on Sales Config = ₹28,200"; all.AssumedValueBasis != want {
		t.Fatalf("basis = %q, want %q", all.AssumedValueBasis, want)
	}
	if all.ProfitLoss == nil || math.Abs(*all.ProfitLoss-(28200-30000)) > 0.01 {
		t.Fatalf("profit must carry the weight-based stock: %v", all.ProfitLoss)
	}

	// 3. A stage Sales Config does not name (Warmup): that animal is left out and named, never
	// priced at a guess; the rest keep their value.
	goat("Warmup", "female", shedP, "lw-tag-4")
	exec(`UPDATE procurement_loads SET expected_count = 4 WHERE load_id = $1::uuid`, loadX)
	scan("lw-tag-4", 25, "2026-09-15T06:00:00Z", "verified")
	unpriced := read()
	if unpriced.AssumedValue == nil || math.Abs(*unpriced.AssumedValue-28200) > 0.01 {
		t.Fatalf("an unpriced animal adds nothing: value=%v", unpriced.AssumedValue)
	}
	if unpriced.AssumedValueBasis != "3 animals × latest weight × ₹/kg by stage and sex on Sales Config = ₹28,200" {
		t.Fatalf("the unpriced animal stays out of the count: %q", unpriced.AssumedValueBasis)
	}

	valueOf := func(l domain.LoadwiseLoad) float64 {
		if l.AssumedValue == nil {
			return -1
		}
		return *l.AssumedValue
	}

	// 4. One animal, two RFIDs, and both its milk cohort and its stage naming the SAME valuation
	// stage: still ONE animal at ONE weight. Its secondary tag's newer 21 kg scan becomes its
	// latest weight (20 -> 21 kg, +500), and nothing is counted twice.
	t.Run("OneToManyTwoTagsAndMultipleDimensionsStayOneAnimal", func(t *testing.T) {
		exec(`
INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, normalizer_version)
VALUES ($1::uuid, $2::uuid, 'animal_identifier_2', 'lw-tag-1b', 'LW-TAG-1B', 'tenant', false, 'active', now(), 'test')`, testTenant, firstGoat)
		exec(`UPDATE goats SET milk_cohort = 'K3' WHERE goat_id = $1::uuid`, firstGoat)
		scan("lw-tag-1b", 21, "2026-09-18T05:00:00Z", "verified")
		got := read()
		if math.Abs(valueOf(got)-28700) > 0.01 || !strings.HasPrefix(got.AssumedValueBasis, "3 animals ×") {
			t.Fatalf("two tags / two matching dimensions must stay one animal: value=%v basis=%q", got.AssumedValue, got.AssumedValueBasis)
		}
	})

	// 5. Only LIVE animals are stock: a dead animal's weight is not valued; a sick one still is.
	t.Run("StatusMatrixOnlyLiveAnimalsAreValued", func(t *testing.T) {
		dead := goat("K3", "male", shedP, "lw-tag-dead")
		exec(`UPDATE goats SET lifecycle_status = 'dead', exit_reason = 'died', exited_at = now() WHERE goat_id = $1::uuid`, dead)
		sick := goat("K3", "female", shedP, "lw-tag-sick")
		exec(`UPDATE goats SET lifecycle_status = 'sick' WHERE goat_id = $1::uuid`, sick)
		exec(`UPDATE procurement_loads SET expected_count = 6 WHERE load_id = $1::uuid`, loadX)
		scan("lw-tag-dead", 30, "2026-09-15T07:00:00Z", "verified")
		scan("lw-tag-sick", 10, "2026-09-15T07:00:00Z", "verified")
		got := read()
		// 28700 + the sick K3 female's 10 kg x 400; the dead animal's 30 kg adds nothing.
		if math.Abs(valueOf(got)-32700) > 0.01 || !strings.HasPrefix(got.AssumedValueBasis, "4 animals ×") {
			t.Fatalf("status matrix: value=%v basis=%q", got.AssumedValue, got.AssumedValueBasis)
		}
	})

	// 6. A smaller page never changes a load's value: every load served in a window of one reads
	// the same assumed value it reads in the full window.
	t.Run("PaginationWindowOfOneValuesEachLoadAsTheFullWindowDoes", func(t *testing.T) {
		full, err := repo.LoadwiseSales(ctx, testTenant, "", 60)
		if err != nil {
			t.Fatal(err)
		}
		one, err := repo.LoadwiseSales(ctx, testTenant, "", 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(one.Loads) != 1 {
			t.Fatalf("window of one served %d loads", len(one.Loads))
		}
		for _, l := range full.Loads {
			if l.LoadID == one.Loads[0].LoadID && valueOf(l) != valueOf(one.Loads[0]) {
				t.Fatalf("the page window moved a load's stock value: %v vs %v", valueOf(l), valueOf(one.Loads[0]))
			}
		}
	})

	// 7. Narrowing to the load's own park keeps its stock value exactly.
	t.Run("ParkScopeKeepsTheLoadsStockValue", func(t *testing.T) {
		scoped, err := repo.LoadwiseSales(ctx, testTenant, cbe, 60)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range scoped.Loads {
			if l.LoadID == loadX {
				if math.Abs(valueOf(l)-32700) > 0.01 {
					t.Fatalf("park scope moved the stock value: %v", valueOf(l))
				}
				return
			}
		}
		t.Fatalf("load %s missing from its own park's view", loadX)
	})

	// 8. SPECIES (maintainer decision 2026-10-02): a sheep in the same stage and gender as a goat is
	// priced at the SHEEP rate. A 10 kg K3 sheep male adds 10 x 430, never the goat male's 10 x 500.
	t.Run("MultipleDimensionsSheepIsPricedAtTheSheepRate", func(t *testing.T) {
		animal("sheep", "K3", "male", shedP, "lw-tag-sheep")
		exec(`UPDATE procurement_loads SET expected_count = 7 WHERE load_id = $1::uuid`, loadX)
		scan("lw-tag-sheep", 10, "2026-09-15T08:00:00Z", "verified")
		got := read()
		if math.Abs(valueOf(got)-(32700+4300)) > 0.01 {
			t.Fatalf("sheep must take the sheep rate: value=%v (goat rate would be %v)", valueOf(got), 32700+5000)
		}
	})

	// 9. An UNWEIGHED sheep carries the SHEEP average of its load (10 kg, the one weighed sheep),
	// never the goats' 24 kg: + 10 x 380 for a K3 sheep female.
	t.Run("MultipleDimensionsUnweighedSheepCarriesTheSheepAverage", func(t *testing.T) {
		animal("sheep", "K3", "female", shedP, "")
		exec(`UPDATE procurement_loads SET expected_count = 8 WHERE load_id = $1::uuid`, loadX)
		got := read()
		if math.Abs(valueOf(got)-(37000+3800)) > 0.01 {
			t.Fatalf("an unweighed sheep must carry the sheep average: value=%v", valueOf(got))
		}
	})
}
