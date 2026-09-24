package postgres

import (
	"context"
	"errors"
	"testing"

	salesapp "github.com/vgoats/goatos/backend/internal/sales/app"
	"github.com/vgoats/goatos/backend/internal/sales/domain"
	"github.com/vgoats/goatos/backend/internal/sales/ports"
)

func TestQueuedSaleSurvivesRenamesWithoutReassigningNames(t *testing.T) {
	ctx := context.Background()
	repo := feedSaleRepo(t, ctx)
	service := salesapp.NewSalesService(repo)
	queued := domain.DealWrite{
		SaleDate: "2026-09-23", Farm: "CPT", Status: domain.StatusDealClosed,
		BuyerName: "Queued buyer", BuyerVendorID: feedSaleBuyerVendorID,
		Lines: []domain.DealLineWrite{{ProductType: "Feed", Breed: "Maize", Quantity: kg(20), RatePerUnit: kg(5)}},
	}
	rename := func(name string) {
		t.Helper()
		_, err := service.SaveSellableProduct(ctx, salesTestTenant, domain.ProductWrite{Code: "feed", Name: name, Kind: domain.KindFeed, Unit: domain.UnitKg}, "")
		if err != nil {
			t.Fatal(err)
		}
	}
	rename("Feed from store")
	rename("Stored feed")
	deal, err := service.CreateDeal(ctx, salesTestTenant, queued, "", "queued-before-rename")
	if err != nil {
		t.Fatal(err)
	}
	if len(deal.Lines) != 1 || deal.Lines[0].ProductCode != "feed" || deal.Lines[0].ProductType != "Stored feed" {
		t.Fatalf("wrong recorded identity: %+v", deal)
	}
	if got := soldKg(t, ctx, repo, salesTestTenant, "CPT", "Maize"); got != 20 {
		t.Fatalf("depleted %v", got)
	}
	// Response-loss replay must still return the original result without another depletion.
	if _, err := service.CreateDeal(ctx, salesTestTenant, queued, "", "queued-before-rename"); err != nil {
		t.Fatal(err)
	}
	if got := soldKg(t, ctx, repo, salesTestTenant, "CPT", "Maize"); got != 20 {
		t.Fatalf("replay depleted %v", got)
	}
	// Another product cannot take either a current or a historical name.
	for _, name := range []string{"Feed", "Feed from store", "Stored feed"} {
		_, err := service.SaveSellableProduct(ctx, salesTestTenant, domain.ProductWrite{Code: "goat", Name: name, Kind: domain.KindAnimal, Unit: domain.UnitNumber, SpeciesCode: "goat"}, "")
		if !errors.Is(err, ports.ErrProductNameTaken) {
			t.Fatalf("name %q: %v", name, err)
		}
	}
	// Switching a product off must still refuse new writes under every retained name.
	if _, err := service.SaveSellableProduct(ctx, salesTestTenant, domain.ProductWrite{Code: "feed", Name: "Stored feed", Kind: domain.KindFeed, Unit: domain.UnitKg, Status: domain.StatusArchived}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateDeal(ctx, salesTestTenant, queued, "", "new-after-archive"); err == nil {
		t.Fatal("archived alias accepted")
	}
}
