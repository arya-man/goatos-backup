package app

import (
	"context"
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/sales/domain"
)

// feedStore is a store holding a fixed balance of each feed it has heard of.
type feedStore struct {
	balances   map[string]float64
	asked      int
	identities map[string]string
}

func (f *feedStore) FeedStockIdentity(feed string) (string, string) {
	if family, ok := f.identities[feed]; ok {
		return family, family
	}
	return feed, feed
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

func (f *feedRepo) ListFeedItems(_ context.Context, _ string) ([]string, error) {
	return []string{"Maize", "Groundnut Cake"}, nil
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

// ONE SALE MAY CARRY THE SAME FEED TWICE, and the store must be asked about the SALE rather than
// about each line. Two lots at two rates is an ordinary way to write a load; asked one line at a
// time, two 9,000 kg lines walked through a 13,790 kg store because neither exceeded it alone, and
// the store went to -4,289.9 kg with nobody warned (found reviewing PR #397 on the live API).
func TestTwoLinesOfOneFeedAreWeighedAgainstTheStoreTogether(t *testing.T) {
	repo := &feedRepo{}
	store := &feedStore{balances: map[string]float64{"CPT/Maize": 1000}}
	s := NewSalesService(repo).WithFeedStock(store)

	write := feedSaleWrite(600)
	second := 600.0
	write.Lines = append(write.Lines, domain.DealLineWrite{
		ProductType: "Feed", Breed: "Maize", Quantity: &second, RatePerUnit: ptr(21),
	})

	_, err := s.CreateDeal(context.Background(), tenant, write, "actor", "two-lines")
	var short domain.ErrFeedStockShort
	if !errors.As(err, &short) {
		t.Fatalf("600kg + 600kg out of 1000kg must ask for confirmation, got %v", err)
	}
	if len(short.Shortfalls) != 1 {
		t.Fatalf("one feed short once, not once per line: got %d", len(short.Shortfalls))
	}
	if got := short.Shortfalls[0].RequestedKg; got != 1200 {
		t.Fatalf("the sale takes 1200kg, the desk must be told 1200 not %v", got)
	}
	if repo.createCalls != 0 {
		t.Fatal("nothing may be recorded while the confirmation is outstanding")
	}
}

// ...and a feed the store can still cover across both lines is not queried twice into a refusal.
func TestTwoLinesTheStoreCanCoverTogetherRecord(t *testing.T) {
	repo := &feedRepo{}
	store := &feedStore{balances: map[string]float64{"CPT/Maize": 1000}}
	s := NewSalesService(repo).WithFeedStock(store)

	write := feedSaleWrite(400)
	second := 400.0
	write.Lines = append(write.Lines, domain.DealLineWrite{
		ProductType: "Feed", Breed: "Maize", Quantity: &second, RatePerUnit: ptr(21),
	})

	if _, err := s.CreateDeal(context.Background(), tenant, write, "actor", "two-ok"); err != nil {
		t.Fatalf("800kg out of 1000kg is covered and must record: %v", err)
	}
	if repo.createCalls != 1 {
		t.Fatalf("want one create, got %d", repo.createCalls)
	}
}

// CLOSING IS WHEN AN EXPECTED SALE TAKES ITS FEED. The depletion ledger is written only for a
// closed deal, so a sale recorded in March against a full store closed in June against an empty one
// used to take its kilograms with nobody told. The close asks the same question the record asked,
// against today's balance, and is answered the same way.
func TestClosingAnExpectedSaleWeighsItAgainstTodaysStore(t *testing.T) {
	repo := &feedRepo{}
	repo.demandFarm = "CPT"
	repo.demand = []domain.FeedDemand{{LineNo: 1, FeedItem: "Maize", Kg: 2000}}
	store := &feedStore{balances: map[string]float64{"CPT/Maize": 1400}}
	s := NewSalesService(repo).WithFeedStock(store)

	_, err := s.SetDealStatus(context.Background(), tenant, "d1", domain.StatusDealClosed, false, "actor")
	var short domain.ErrFeedStockShort
	if !errors.As(err, &short) {
		t.Fatalf("closing a sale that takes 2000kg out of 1400kg must ask for confirmation, got %v", err)
	}
	if repo.dealStatus != "" {
		t.Fatalf("nothing may close while the confirmation is outstanding, status = %q", repo.dealStatus)
	}
	if got := short.Shortfalls[0]; got.BalanceKg != 1400 || got.RequestedKg != 2000 || got.FeedItem != "Maize" {
		t.Fatalf("the shortfall must name the store and the feed: %+v", got)
	}

	// Having looked, the desk closes it anyway -- the feed did leave, the purchase ledger is behind.
	if _, err := s.SetDealStatus(context.Background(), tenant, "d1", domain.StatusDealClosed, true, "actor"); err != nil {
		t.Fatalf("the acknowledged close must go through: %v", err)
	}
	if repo.dealStatus != domain.StatusDealClosed {
		t.Fatalf("status = %q, want the deal closed", repo.dealStatus)
	}
}

// Only CLOSING takes feed off the store. Marking a deal failed, or moving it to advance paid, gives
// its feed back or never took it, so neither may be held up by a store that is short.
func TestOnlyClosingAsksTheStore(t *testing.T) {
	repo := &feedRepo{}
	repo.demandFarm = "CPT"
	repo.demand = []domain.FeedDemand{{LineNo: 1, FeedItem: "Maize", Kg: 9000}}
	s := NewSalesService(repo).WithFeedStock(&feedStore{balances: map[string]float64{"CPT/Maize": 10}})

	for _, status := range []string{domain.StatusDealFailed, domain.StatusAdvancePaid, domain.StatusInDiscussion} {
		if _, err := s.SetDealStatus(context.Background(), tenant, "d1", status, false, "actor"); err != nil {
			t.Fatalf("%s must not be held up by the feed store: %v", status, err)
		}
	}
}

// A response may be lost after closing committed. Retrying must neither ask the
// store again nor turn the successful close into a terminal outbox rejection.
func TestClosedFeedSaleReplaySkipsStockButReopenedSaleChecksAgain(t *testing.T) {
	repo := &feedRepo{}
	repo.demandFarm = "CPT"
	repo.dealStatus = domain.StatusInDiscussion
	repo.demand = []domain.FeedDemand{{LineNo: 1, FeedItem: "Maize", Kg: 600}}
	store := &feedStore{balances: map[string]float64{"CPT/Maize": 1000}}
	service := NewSalesService(repo).WithFeedStock(store)
	ctx := context.Background()
	if _, err := service.SetDealStatus(ctx, tenant, "d1", domain.StatusDealClosed, false, "actor"); err != nil {
		t.Fatal(err)
	}
	store.balances["CPT/Maize"] = 400
	if _, err := service.SetDealStatus(ctx, tenant, "d1", domain.StatusDealClosed, false, "actor"); err != nil {
		t.Fatalf("completed close replay: %v", err)
	}
	if store.asked != 1 {
		t.Fatalf("store asked %d times, want once", store.asked)
	}
	if _, err := service.SetDealStatus(ctx, tenant, "d1", domain.StatusInDiscussion, false, "actor"); err != nil {
		t.Fatal(err)
	}
	// Other consumption can leave the reopened sale short even after its stock is restored.
	_, err := service.SetDealStatus(ctx, tenant, "d1", domain.StatusDealClosed, false, "actor")
	var short domain.ErrFeedStockShort
	if !errors.As(err, &short) {
		t.Fatalf("reopened sale must check current stock: %v", err)
	}
	if repo.dealStatus != domain.StatusInDiscussion {
		t.Fatalf("short sale closed without confirmation: %s", repo.dealStatus)
	}
	if store.asked != 2 {
		t.Fatalf("reopened sale did not ask the store: %d", store.asked)
	}
}

// A lost response remains a successful sale after the catalog is renamed, archived or unavailable.
type completedCatalogReplayRepo struct {
	feedRepo
	catalogCalls int
}

func (r *completedCatalogReplayRepo) CompletedDealForIdempotencyKey(context.Context, string, string) (domain.Deal, bool, error) {
	return domain.Deal{DealID: "already-recorded"}, true, nil
}
func (r *completedCatalogReplayRepo) ListSellableProducts(context.Context, string) ([]domain.Product, error) {
	r.catalogCalls++
	return nil, errors.New("catalog unavailable after commit")
}
func TestCompletedCreateReplayDoesNotDependOnCurrentCatalog(t *testing.T) {
	repo := &completedCatalogReplayRepo{}
	store := &feedStore{}
	got, err := NewSalesService(repo).WithFeedStock(store).CreateDeal(context.Background(), tenant, feedSaleWrite(600), "actor", "completed-key")
	if err != nil || got.DealID != "already-recorded" {
		t.Fatalf("completed replay = %+v, %v", got, err)
	}
	if repo.catalogCalls != 0 || repo.createCalls != 0 || store.asked != 0 {
		t.Fatalf("replay revalidated or wrote: catalog=%d writes=%d stock=%d", repo.catalogCalls, repo.createCalls, store.asked)
	}
}

func TestCompletedCreateReplayReturnsOriginalForChangedPayload(t *testing.T) {
	repo := &completedCatalogReplayRepo{}
	service := NewSalesService(repo)
	for _, kg := range []float64{600, 900} {
		got, err := service.CreateDeal(context.Background(), tenant, feedSaleWrite(kg), "actor", "completed-key")
		if err != nil || got.DealID != "already-recorded" {
			t.Fatalf("replay: %+v %v", got, err)
		}
	}
	if repo.createCalls != 0 || repo.catalogCalls != 0 {
		t.Fatal("completed replay repeated side effects")
	}
}

func TestUnknownFeedCannotBypassStockValidation(t *testing.T) {
	for _, acknowledged := range []bool{false, true} {
		repo := &feedRepo{}
		store := &feedStore{}
		write := feedSaleWrite(100)
		write.Lines[0].Breed = "Unknown feed"
		write.StockShortfallAcknowledged = acknowledged
		_, err := NewSalesService(repo).WithFeedStock(store).CreateDeal(context.Background(), tenant, write, "actor", "invalid-feed")
		var invalid domain.ErrDealValidation
		if !errors.As(err, &invalid) || invalid.Field != "lines[1].breed" {
			t.Fatalf("expected feed field rejection, got %v", err)
		}
		if repo.createCalls != 0 || store.asked != 0 {
			t.Fatal("invalid feed reached stock or write")
		}
	}
}

func TestFamilyFeedDemandSharesOneBalance(t *testing.T) {
	store := &feedStore{balances: map[string]float64{"CPT/Concentrate": 100, "CPT/Goat concentrate": 100, "CPT/Sheep concentrate": 100}, identities: map[string]string{"Goat concentrate": "Concentrate", "Sheep concentrate": "Concentrate"}}
	s := NewSalesService(&feedRepo{}).WithFeedStock(store)
	err := s.weighAgainstTheStore(context.Background(), tenant, "CPT", []domain.FeedDemand{{LineNo: 1, FeedItem: "Goat concentrate", Kg: 60}, {LineNo: 2, FeedItem: "Sheep concentrate", Kg: 60}})
	var short domain.ErrFeedStockShort
	if !errors.As(err, &short) || len(short.Shortfalls) != 1 {
		t.Fatalf("combined 120kg against 100kg must warn once: %v", err)
	}
	got := short.Shortfalls[0]
	if got.RequestedKg != 120 || got.BalanceKg != 100 || got.FeedItem != "Concentrate" || store.asked != 1 {
		t.Fatalf("wrong family shortage: %+v, queries=%d", got, store.asked)
	}
}
