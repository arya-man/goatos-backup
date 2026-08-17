package main

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestImportSalesDBIsIdempotent proves the whole importer against a real migrated Postgres: the
// committed fixture loads, and a SECOND run updates in place through the (tenant_id,
// source_row_no) partial unique indexes rather than duplicating -- the exact ON CONFLICT syntax a
// unit test cannot exercise.
func TestImportSalesDBIsIdempotent(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const tenant = "00000000-0000-4000-8000-000000000001"

	fixture, err := loadFixture("../../../fixtures/sales-db-2026-08-17/sales-db.json")
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	if err := validateFixture(fixture); err != nil {
		t.Fatalf("validate fixture: %v", err)
	}

	for run := 1; run <= 2; run++ {
		if err := importAll(ctx, pool, tenant, fixture); err != nil {
			t.Fatalf("import run %d: %v", run, err)
		}
	}

	for table, want := range map[string]int{
		"sales_deals":             len(fixture.Deals),
		"sales_buyer_leads":       len(fixture.BuyerLeads),
		"sales_fpo_leads":         len(fixture.FPOLeads),
		"sales_sold_animal_tags":  len(fixture.SoldAnimalTags),
		"sales_weight_audit":      len(fixture.WeightAudit),
		"sales_market_benchmarks": len(fixture.MarketBenchmarks),
	} {
		var got int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE tenant_id = $1`, tenant).Scan(&got); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if got != want {
			t.Fatalf("%s rows = %d want %d after two runs (re-run must update, never duplicate)", table, got, want)
		}
	}

	// The market price is parsed out of the prose at import time.
	var price float64
	if err := pool.QueryRow(ctx, `
		SELECT market_price_per_kg FROM sales_market_benchmarks
		WHERE tenant_id = $1 AND source_row_no = 1`, tenant).Scan(&price); err != nil {
		t.Fatalf("benchmark price: %v", err)
	}
	if price != 370 {
		t.Fatalf("market_price_per_kg = %v want 370 parsed from the market string", price)
	}
}
