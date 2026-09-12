package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/sales/domain"
	"github.com/vgoats/goatos/backend/internal/sales/ports"
)

// A mixed sale (sheep and goats of different breeds to one buyer, maintainer decision 2026-09-12)
// is ONE deal with several lines: the lines land in the same transaction, come back in entry
// order on every read path, the deal row carries their rollup, and the closed-deal overview
// splits the sale by line.
func TestSalesDealLinesPostgresPaths(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)

	write := domain.DealWrite{
		SaleDate: "2026-09-12", Farm: "CPT",
		BuyerName: "Tanveer", BuyerPlace: "Madur",
		BuyerVendorID: "3f1c2a5e-9b04-4d67-8a11-2c7e5d9f0b34",
		AdvanceAmount: f64(50000),
		Lines: []domain.DealLineWrite{
			{ProductType: "Sheep", Breed: "Anantapur", AnimalCount: f64(10), TotalWeightKg: f64(300), SalesValue: 120000},
			{ProductType: "Sheep", Breed: "Kenguri", AnimalCount: f64(5), TotalWeightKg: f64(140), SalesValue: 56000},
			{ProductType: "Goat", Breed: "Sirohi", AnimalCount: f64(4), MaleCount: f64(4), TotalWeightKg: f64(100), SalesValue: 45000},
		},
	}
	created := seedDeal(t, repo, ctx, "key-mixed", write)

	assertLines := func(t *testing.T, d domain.Deal, where string) {
		t.Helper()
		if len(d.Lines) != 3 {
			t.Fatalf("%s: lines = %d, want 3", where, len(d.Lines))
		}
		for i, want := range []string{"Anantapur", "Kenguri", "Sirohi"} {
			if d.Lines[i].LineNo != i+1 || d.Lines[i].Breed != want || d.Lines[i].LineID == "" {
				t.Fatalf("%s: line %d = %+v, want line_no %d breed %s", where, i, d.Lines[i], i+1, want)
			}
		}
		if d.Lines[2].SalesValue != 45000 || d.Lines[2].MaleCount == nil || *d.Lines[2].MaleCount != 4 {
			t.Fatalf("%s: goat line = %+v", where, d.Lines[2])
		}
	}

	t.Run("the deal row is the rollup and the lines come back in order", func(t *testing.T) {
		assertLines(t, created, "create")
		if created.ProductType != domain.ProductMixed || created.Breed != domain.ProductMixed {
			t.Fatalf("rollup product/breed = %s/%s", created.ProductType, created.Breed)
		}
		if created.SalesValue != 221000 || created.AnimalCount == nil || *created.AnimalCount != 19 || created.TotalWeightKg == nil || *created.TotalWeightKg != 540 {
			t.Fatalf("rollup numbers = value %v animals %v weight %v", created.SalesValue, created.AnimalCount, created.TotalWeightKg)
		}
		// Only the goat line recorded a sex split, so the rollup carries that split and nothing
		// invented for the sheep.
		if created.MaleCount == nil || *created.MaleCount != 4 || created.FemaleCount != nil {
			t.Fatalf("sex rollup = %v/%v, want 4/nil", created.MaleCount, created.FemaleCount)
		}
		if created.PaymentReceived == nil || *created.PaymentReceived != 50000 {
			t.Fatalf("advance seeded payment_received = %v", created.PaymentReceived)
		}
	})

	t.Run("the ledger page carries the lines", func(t *testing.T) {
		page, err := repo.ListDeals(ctx, salesTestTenant, "CPT", 25, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Deals) != 1 {
			t.Fatalf("deals = %d, want 1 (three lines are still one deal)", len(page.Deals))
		}
		assertLines(t, page.Deals[0], "list")
	})

	t.Run("exact replay returns the same deal with its lines; a changed line is refused", func(t *testing.T) {
		replay, err := repo.CreateDeal(ctx, salesTestTenant, write.Normalize(), "", "key-mixed")
		if err != nil {
			t.Fatal(err)
		}
		if replay.DealID != created.DealID {
			t.Fatalf("replay created a second deal")
		}
		assertLines(t, replay, "replay")
		mutated := write
		mutated.Lines = append([]domain.DealLineWrite(nil), write.Lines...)
		mutated.Lines[1].SalesValue = 56001
		if _, err := repo.CreateDeal(ctx, salesTestTenant, mutated.Normalize(), "", "key-mixed"); !errors.Is(err, ports.ErrIdempotencyConflict) {
			t.Fatalf("same key, changed line: got %v, want ErrIdempotencyConflict", err)
		}
	})

	t.Run("the overview splits the closed mixed deal by line", func(t *testing.T) {
		overview, err := repo.GetOverview(ctx, salesTestTenant, "CPT")
		if err != nil {
			t.Fatal(err)
		}
		if overview.Summary.Deals != 1 || overview.Summary.Sheep != 15 || overview.Summary.Goats != 4 || overview.Summary.Revenue != 221000 {
			t.Fatalf("summary = %+v", overview.Summary)
		}
		if len(overview.PriceBands) != 3 {
			t.Fatalf("price bands = %d, want one per (product, breed) line", len(overview.PriceBands))
		}
	})

	t.Run("a legacy single-product write lands as one line", func(t *testing.T) {
		single := seedDeal(t, repo, ctx, "key-legacy", domain.DealWrite{
			SaleDate: "2026-09-12", Farm: "CBE", ProductType: "Goat", Breed: "Sojat",
			BuyerName: "Ramesh", BuyerVendorID: "3f1c2a5e-9b04-4d67-8a11-2c7e5d9f0b34",
			AnimalCount: f64(2), SalesValue: 30000,
		})
		if len(single.Lines) != 1 || single.Lines[0].Breed != "Sojat" || single.ProductType != "Goat" {
			t.Fatalf("legacy deal = %+v lines %+v", single, single.Lines)
		}
	})
}
