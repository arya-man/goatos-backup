package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/growthdirector/domain"
	"github.com/vgoats/goatos/backend/internal/growthdirector/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// SALE PRICE PER STAGE AND SEX (maintainer decision 2026-09-24, migration 000399).
//
// The FCR fixture's lump pen holds 25 goats at management_stage 'kid', all male; its scanned pen
// holds three 'kid' females. A goat/kid/male override must value the lump pen's gain at the
// override and leave the scanned pen on the goat default -- the per-animal resolution, end to end
// through the real SQL mix and the domain resolver.
func TestFCRValuesEachPenAtItsAnimalsStageAndSexPrice(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedFCRFixture(t, ctx, pool)
	execGD(t, ctx, pool, `
INSERT INTO growth_sale_price_assumptions (tenant_id, species, management_stage, sex, price_per_kg_inr, effective_from, set_by)
VALUES ($1::uuid, 'goat', 'kid', 'male', 480, '2026-07-01', 'maintainer')`, gdTenant)

	repo := NewRepository(pool, 30*time.Second)
	got, err := repo.GetFCR(ctx, gdTenant, []string{gdPark},
		time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC), time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC), "", "", "")
	if err != nil {
		t.Fatalf("GetFCR: %v", err)
	}
	byDisplay := map[string]domain.FCRPen{}
	for _, pen := range got.Pens {
		byDisplay[pen.OperationalLocationDisplay] = pen
	}
	lump, scan := byDisplay["Coimbatore · Lump 1"], byDisplay["Coimbatore · Fcr Shed - Part 2"]
	fcrNear(t, "lump gain valued at the kid/male override", lump.GainValueINR, 17.5*480)
	fcrNear(t, "scanned kid females stay on the goat default", scan.GainValueINR, 2.1*425)

	// The served price list carries the override beside the defaults, with its stage and sex.
	var override *domain.SalePrice
	for i, p := range got.SalePrices {
		if !p.IsDefault() {
			override = &got.SalePrices[i]
		}
	}
	if override == nil || override.Species != "goat" || override.ManagementStage != "kid" || override.Sex != "male" || override.PricePerKgINR != 480 {
		t.Fatalf("served override = %+v", override)
	}
}

// The drawer's write path for an override: it must name a stage the tenant's vocabulary holds
// (stored in the vocabulary's own spelling), fence on the loaded figure like a default, and an
// emptied box (nil price) CLEARS it so the combination falls back to the species default --
// append-only, so the day's history still reads.
func TestPutAssumptionsSetsFencesAndClearsAStageSexOverride(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedFCRFixture(t, ctx, pool)
	execGD(t, ctx, pool, `
INSERT INTO animal_stage_lookup (tenant_id, stage_code, name, sort_order, status)
VALUES ($1::uuid, 'K3', 'Kid 3', 3, 'active'), ($1::uuid, 'OLD', 'Retired stage', 9, 'retired')
ON CONFLICT DO NOTHING`, gdTenant)
	repo := NewRepository(pool, 30*time.Second)
	today := time.Now()
	f := func(v float64) *float64 { return &v }
	put := func(u domain.SalePriceUpdate) error {
		_, err := repo.PutAssumptions(ctx, gdTenant, gdOperator, today, domain.AssumptionsUpdate{SalePrices: []domain.SalePriceUpdate{u}})
		return err
	}
	override := func() (float64, bool) {
		out, err := repo.GetSalePrices(ctx, gdTenant, today)
		if err != nil {
			t.Fatalf("GetSalePrices: %v", err)
		}
		for _, p := range out.Prices {
			if p.Species == "goat" && p.ManagementStage == "K3" && p.Sex == "male" {
				return p.PricePerKgINR, true
			}
		}
		return 0, false
	}

	// The drawer offers only stages weighed animals sit in (2026-09-24) -- through BOTH arms:
	// the lump pen's goats reached through the bucket->pen bridge, the scanned kids through their
	// tags -- plus any stage already carrying a price. Retired stages never.
	execGD(t, ctx, pool, `
INSERT INTO animal_stage_lookup (tenant_id, stage_code, name, sort_order, status)
VALUES ($1::uuid, 'kid', 'Kid', 1, 'active'), ($1::uuid, 'K2', 'Kid 2', 2, 'active'), ($1::uuid, 'ICU', 'ICU', 8, 'active')
ON CONFLICT DO NOTHING`, gdTenant)
	// The scanned kids move to K2, so K2 can only come from the scanned arm, 'kid' only from the pen.
	execGD(t, ctx, pool, `UPDATE goats SET management_stage = 'K2' WHERE tenant_id = $1::uuid AND goat_id::text LIKE '33333333-%'`, gdTenant)
	stageCodes := func() []string {
		t.Helper()
		a, err := repo.GetAssumptions(ctx, gdTenant, today, true)
		if err != nil {
			t.Fatalf("GetAssumptions: %v", err)
		}
		var codes []string
		for _, s := range a.Stages {
			codes = append(codes, s.Code)
		}
		return codes
	}
	codes := stageCodes()
	if !containsString(codes, "kid") || !containsString(codes, "K2") {
		t.Fatalf("stages = %v, want 'kid' (whole-pen arm) and 'K2' (scanned arm)", codes)
	}
	if containsString(codes, "K3") || containsString(codes, "ICU") || containsString(codes, "OLD") {
		t.Fatalf("stages = %v, want no unweighed stage (K3, ICU) and no retired one (OLD)", codes)
	}

	// Set, typed in another case: stored as the vocabulary spells it.
	if err := put(domain.SalePriceUpdate{Species: "goat", ManagementStage: "k3", Sex: "male", PricePerKgINR: f(500)}); err != nil {
		t.Fatalf("set override: %v", err)
	}
	if got, ok := override(); !ok || got != 500 {
		t.Fatalf("override after set = %v %v", got, ok)
	}
	// A stage with a price in force stays listed even though no weighed animal sits in it.
	if codes := stageCodes(); !containsString(codes, "K3") {
		t.Fatalf("stages = %v, want K3 listed while its price is in force", codes)
	}
	// A stage outside the vocabulary (or retired) is refused, never stored.
	for _, stage := range []string{"K9", "OLD"} {
		if err := put(domain.SalePriceUpdate{Species: "goat", ManagementStage: stage, Sex: "male", PricePerKgINR: f(500)}); !errors.Is(err, ports.ErrInvalidArgument) {
			t.Fatalf("stage %s: want ErrInvalidArgument, got %v", stage, err)
		}
	}
	// Fence: a drawer that loaded no override is stale once one exists; the loaded 500 lands.
	if err := put(domain.SalePriceUpdate{Species: "goat", ManagementStage: "K3", Sex: "male", PricePerKgINR: f(510)}); !errors.Is(err, ports.ErrAssumptionConflict) {
		t.Fatalf("stale override save: want conflict, got %v", err)
	}
	if err := put(domain.SalePriceUpdate{Species: "goat", ManagementStage: "K3", Sex: "male", PricePerKgINR: f(510), LoadedPricePerKgINR: f(500)}); err != nil {
		t.Fatalf("fenced override save: %v", err)
	}
	// Clear: nil price, loaded 510 -> the override is gone from the served list.
	if err := put(domain.SalePriceUpdate{Species: "goat", ManagementStage: "K3", Sex: "male", LoadedPricePerKgINR: f(510)}); err != nil {
		t.Fatalf("clear override: %v", err)
	}
	if got, ok := override(); ok {
		t.Fatalf("a cleared override must not be served, got %v", got)
	}
	// Clearing again is a no-op replay, not a conflict.
	if err := put(domain.SalePriceUpdate{Species: "goat", ManagementStage: "K3", Sex: "male", LoadedPricePerKgINR: f(510)}); err != nil {
		t.Fatalf("clear replay: %v", err)
	}
	// Every change was audited: set, change, clear.
	var audits int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE tenant_id = $1::uuid AND action = 'growth.sale_price.set' AND metadata->>'management_stage' = 'K3'`, gdTenant).Scan(&audits); err != nil {
		t.Fatalf("audit count: %v", err)
	}
	if audits != 3 {
		t.Fatalf("audit rows = %d, want 3 (set, change, clear)", audits)
	}
	// The table refuses a cleared DEFAULT outright, whatever the service does.
	if _, err := pool.Exec(ctx, `INSERT INTO growth_sale_price_assumptions (tenant_id, species, price_per_kg_inr, effective_from) VALUES ($1::uuid, 'goat', NULL, '2027-01-01')`, gdTenant); err == nil {
		t.Fatal("a species default with no price must violate the price check")
	}
}

func containsString(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// A pen whose residents span several (species, stage, sex) groups is still ONE pen row: the head
// mix is grouped per group and folded per pen, so it must neither fan the pen out nor move its
// feed, gain or FCR -- only the price, which becomes the head-weighted mean of each animal's own.
func TestFCRHeadMixOneToManyKeepsOnePenRow(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedFCRFixture(t, ctx, pool)
	// The lump pen's 25 'kid' males become 10 kid males, 10 K3 males and 5 kid females.
	execGD(t, ctx, pool, `
WITH lump AS (SELECT goat_id, row_number() OVER (ORDER BY display_id) AS n FROM goats WHERE tenant_id = $1::uuid AND shed_id = $2::uuid)
UPDATE goats g SET management_stage = CASE WHEN l.n <= 10 THEN 'K3' ELSE g.management_stage END,
                   sex = CASE WHEN l.n > 20 THEN 'female' ELSE g.sex END
FROM lump l WHERE g.goat_id = l.goat_id`, gdTenant, gdShedLump)
	execGD(t, ctx, pool, `
INSERT INTO growth_sale_price_assumptions (tenant_id, species, management_stage, sex, price_per_kg_inr, effective_from, set_by)
VALUES ($1::uuid, 'goat', 'K3', 'male', 500, '2026-07-01', 'test'), ($1::uuid, 'goat', 'kid', 'female', 440, '2026-07-01', 'test')`, gdTenant)

	repo := NewRepository(pool, 30*time.Second)
	window := func(parks []string) domain.FCRReport {
		t.Helper()
		got, err := repo.GetFCR(ctx, gdTenant, parks,
			time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC), time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC), "", "", "")
		if err != nil {
			t.Fatalf("GetFCR: %v", err)
		}
		return got
	}
	got := window([]string{gdPark})
	lumps := 0
	var lump domain.FCRPen
	for _, pen := range got.Pens {
		if pen.OperationalLocationDisplay == "Coimbatore · Lump 1" {
			lumps++
			lump = pen
		}
	}
	if lumps != 1 || len(got.Pens) != 2 {
		t.Fatalf("lump pen served %d times among %d pens, want once among 2", lumps, len(got.Pens))
	}
	if lump.Animals != 25 {
		t.Fatalf("lump animals = %d, want 25 (the mix must not multiply the cohort)", lump.Animals)
	}
	fcrNear(t, "lump fcr unchanged by the mix", lump.FCR, 4.0)
	fcrNear(t, "lump feed unchanged by the mix", lump.FeedKg, 70)
	// (10 x 425 kid male default + 10 x 500 K3 male + 5 x 440 kid female) / 25 = 458.
	fcrNear(t, "lump gain valued at the head-weighted stage x sex price", lump.GainValueINR, 17.5*458)

	t.Run("ParkScopeServesNoPenOutsideTheAskedPark", func(t *testing.T) {
		// Another park's scope reads none of this park's pens, mix or no mix.
		if other := window([]string{"99999999-0000-4000-8000-000000000099"}); len(other.Pens) != 0 {
			t.Fatalf("a foreign park scope served %d pens", len(other.Pens))
		}
	})
}
