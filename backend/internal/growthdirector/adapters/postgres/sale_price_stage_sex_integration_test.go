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

// SALE PRICE PER STAGE AND SEX (maintainer decision 2026-09-24, migration 000398).
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

	// The drawer offers the ACTIVE stages only.
	a, err := repo.GetAssumptions(ctx, gdTenant, today)
	if err != nil {
		t.Fatalf("GetAssumptions: %v", err)
	}
	var codes []string
	for _, s := range a.Stages {
		codes = append(codes, s.Code)
	}
	if !containsString(codes, "K3") || containsString(codes, "OLD") {
		t.Fatalf("stage vocabulary = %v, want K3 and not the retired OLD", codes)
	}

	// Set, typed in another case: stored as the vocabulary spells it.
	if err := put(domain.SalePriceUpdate{Species: "goat", ManagementStage: "k3", Sex: "male", PricePerKgINR: f(500)}); err != nil {
		t.Fatalf("set override: %v", err)
	}
	if got, ok := override(); !ok || got != 500 {
		t.Fatalf("override after set = %v %v", got, ok)
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
