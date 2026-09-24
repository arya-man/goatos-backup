package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/sales/domain"
)

// P10 (docs/perf/2026-09-24-stg-latency): the sales overview is a dozen small whole-filter
// rollups. It used to fan out 8 goroutines, each on its own pool connection, with the deal lines
// read serially after the deals (13 statements, 2 sequential round trips on the critical path,
// 8 connections per request). Every rollup is independent of the others, so the page is ONE
// pgx.Batch on one connection: one round trip, one connection, the same numbers.
func TestGetOverviewIsOneRoundTripOnOneConnection(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	writer := NewRepository(pool, 5*time.Second)
	seedDeal(t, writer, ctx, "rt-cbe", domain.DealWrite{
		SaleDate: "2025-05-02", Farm: "CBE", ProductType: "Goat", Breed: "Malai", BuyerName: "Rafi",
		AnimalCount: f64(4), MaleCount: f64(4), TotalWeightKg: f64(120), SalesValue: 60000,
	})
	seedDeal(t, writer, ctx, "rt-cpt", domain.DealWrite{
		SaleDate: "2025-06-11", Farm: "CPT", ProductType: "Sheep", Breed: "Anantapur", BuyerName: "Tanveer",
		AnimalCount: f64(9), FemaleCount: f64(9), TotalWeightKg: f64(250), SalesValue: 90000,
	})
	for _, farm := range []string{"", "CBE", "CPT"} {
		counted, trips := pgtest.CountingPool(t, ctx, pool)
		repo := NewRepository(counted, 5*time.Second)
		if _, err := repo.GetOverview(ctx, salesTestTenant, farm); err != nil {
			t.Fatalf("warm overview %q: %v", farm, err)
		}
		trips.Reset()
		got, err := repo.GetOverview(ctx, salesTestTenant, farm)
		if err != nil {
			t.Fatalf("overview %q: %v", farm, err)
		}
		if n := trips.Trips(); n != 1 {
			t.Fatalf("overview %q took %d round trips, want 1:\n%s", farm, n, strings.Join(trips.SQL(), "\n---\n"))
		}
		wantDeals := map[string]int{"": 2, "CBE": 1, "CPT": 1}[farm]
		if got.Summary.Deals != wantDeals || len(got.PriceBands) == 0 {
			t.Fatalf("overview %q: deals=%d want %d, price bands=%d (lines must be attached)", farm, got.Summary.Deals, wantDeals, len(got.PriceBands))
		}
	}
}
