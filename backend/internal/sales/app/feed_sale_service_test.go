package app

import (
	"context"
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/sales/domain"
)

// feedStore is a store holding a fixed balance of each feed it has heard of.
type feedStore struct {
	balances map[string]float64
	asked    int
}

func (f *feedStore) FeedBalanceKg(_ context.Context, _, farm, feed string) (float64, bool, error) {
	f.asked++
	kg, ok := f.balances[farm+"/"+feed]
	return kg, ok, nil
}

// A feed sale the store can cover records with no ceremony.
func feedSaleWrite(kg float64) domain.DealWrite {
	return domain.DealWrite{
		SaleDate: "2026-09-23", Farm: "CPT",
		BuyerName: "Ramesh Traders", BuyerVendorID: "3f1c2a5e-9b04-4d67-8a11-2c7e5d9f0b34",
		Lines: []domain.DealLineWrite{{
			ProductType: "Feed", Breed: "Maize",
			Quantity: &kg, RatePerUnit: ptr(21),
		}},
	}
}

func ptr(v float64) *float64 { return &v }

// feedRepo is the fake repository with Feed added to the registry, since a farm that sells feed
// is a farm that put feed in its registry.
type feedRepo struct{ fakeRepo }

func (f *feedRepo) ListSellableProducts(_ context.Context, _ string) ([]domain.Product, error) {
	return []domain.Product{
		{Code: "feed", Name: "Feed", Kind: domain.KindFeed, Unit: "kg", SortOrder: 40},
	}, nil
}

func (f *feedRepo) ListAllSellableProducts(_ context.Context, _ string) ([]domain.ProductRow, error) {
	return []domain.ProductRow{
		{Product: domain.Product{Code: "feed", Name: "Feed", Kind: domain.KindFeed, Unit: domain.UnitKg, SortOrder: 40}, Status: domain.StatusActive},
	}, nil
}

func (f *feedRepo) ListProductVariants(_ context.Context, _ string, _ []domain.Product) (map[string][]string, error) {
	return map[string][]string{"Feed": {"Maize", "Groundnut Cake"}}, nil
}

// THE SHORT SALE IS A CONFIRMATION, NOT A BLOCK (maintainer decision 2026-09-23). The desk is told
// what the store thinks it holds, and the SAME sale re-sent with the acknowledgement records --
// because the feed may genuinely have left while the purchase ledger is behind, and refusing it
// would make the register lie about a load that physically went.
func TestASaleTakingMoreFeedThanTheStoreHoldsIsConfirmedOnceThenRecorded(t *testing.T) {
	repo := &feedRepo{}
	store := &feedStore{balances: map[string]float64{"CPT/Maize": 1400}}
	s := NewSalesService(repo).WithFeedStock(store)

	_, err := s.CreateDeal(context.Background(), tenant, feedSaleWrite(2000), "actor", "key-1")
	var short domain.ErrFeedStockShort
	if !errors.As(err, &short) {
		t.Fatalf("selling 2000kg out of 1400kg must ask for confirmation, got %v", err)
	}
	if repo.createCalls != 0 {
		t.Fatal("nothing may be recorded while the confirmation is outstanding")
	}
	if len(short.Shortfalls) != 1 {
		t.Fatalf("want one shortfall, got %d", len(short.Shortfalls))
	}
	// The desk is told the BALANCE, not just that something is wrong: it is being asked whether
	// the ledger is behind, and it cannot answer that without seeing what the ledger says.
	if got := short.Shortfalls[0]; got.BalanceKg != 1400 || got.RequestedKg != 2000 ||
		got.FeedItem != "Maize" || got.FarmLabel != "CPT" || got.LineNo != 1 {
		t.Fatalf("the shortfall must name the store, the feed and the line: %+v", got)
	}

	// Having looked, the desk says the sale is right.
	acknowledged := feedSaleWrite(2000)
	acknowledged.StockShortfallAcknowledged = true
	if _, err := s.CreateDeal(context.Background(), tenant, acknowledged, "actor", "key-1"); err != nil {
		t.Fatalf("the acknowledged sale must record: %v", err)
	}
	if repo.createCalls != 1 {
		t.Fatalf("the acknowledged sale must reach the repository once, got %d", repo.createCalls)
	}
}

// A sale the store can cover asks nothing, and an ANIMAL sale never asks the store at all.
func TestOnlyAShortFeedLineAsksForConfirmation(t *testing.T) {
	repo := &feedRepo{}
	store := &feedStore{balances: map[string]float64{"CPT/Maize": 5000}}
	s := NewSalesService(repo).WithFeedStock(store)

	if _, err := s.CreateDeal(context.Background(), tenant, feedSaleWrite(2000), "actor", "key-1"); err != nil {
		t.Fatalf("a covered sale must record without ceremony: %v", err)
	}

	animals := &fakeRepo{}
	animalStore := &feedStore{}
	as := NewSalesService(animals).WithFeedStock(animalStore)
	goats := domain.DealWrite{
		SaleDate: "2026-09-23", Farm: "CPT", ProductType: "Goat", Breed: "Sojat",
		BuyerName: "Irshad Bhai", BuyerVendorID: "3f1c2a5e-9b04-4d67-8a11-2c7e5d9f0b34",
		SalesValue: 90000,
	}
	if _, err := as.CreateDeal(context.Background(), tenant, goats, "actor", "key-2"); err != nil {
		t.Fatalf("an animal sale must record: %v", err)
	}
	if animalStore.asked != 0 {
		t.Fatal("selling goats must not ask the feed store anything")
	}
}

// A feed the ledger has never carried raises NOTHING. "The store has never heard of this" is a
// different fact from "the store says none left", and warning on the first would teach the desk to
// tick past the second without reading it.
func TestAFeedTheLedgerNeverCarriedDoesNotWarn(t *testing.T) {
	repo := &feedRepo{}
	store := &feedStore{balances: map[string]float64{}}
	s := NewSalesService(repo).WithFeedStock(store)

	if _, err := s.CreateDeal(context.Background(), tenant, feedSaleWrite(2000), "actor", "key-1"); err != nil {
		t.Fatalf("an unledgered feed must record without a warning, got %v", err)
	}
	if store.asked != 1 {
		t.Fatalf("the store must still be asked once, got %d", store.asked)
	}
}
