package app

import (
	"context"
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/sales/domain"
	"github.com/vgoats/goatos/backend/internal/sales/ports"
)

// fakeRepo records what the service actually asked for, so these tests pin the service's
// validation and filter normalization without a database.
type fakeRepo struct {
	overviewFarm  string
	listFarm      string
	listLimit     int
	listOffset    int
	createdWrite  domain.DealWrite
	createdKey    string
	createCalls   int
	overviewCalls int
}

func (f *fakeRepo) GetOverview(_ context.Context, _ string, farm string) (domain.Overview, error) {
	f.overviewCalls++
	f.overviewFarm = farm
	return domain.Overview{}, nil
}

func (f *fakeRepo) ListDeals(_ context.Context, _ string, farm string, limit, offset int) (ports.DealPage, error) {
	f.listFarm, f.listLimit, f.listOffset = farm, limit, offset
	return ports.DealPage{Total: 63}, nil
}

func (f *fakeRepo) CreateDeal(_ context.Context, _ string, write domain.DealWrite, _ string, key string) (domain.Deal, error) {
	f.createCalls++
	f.createdWrite = write
	f.createdKey = key
	return domain.Deal{DealID: "d-1"}, nil
}

const tenant = "00000000-0000-4000-8000-000000000001"

func TestGetOverviewNormalizesTheFarmFilter(t *testing.T) {
	repo := &fakeRepo{}
	s := NewSalesService(repo)
	if _, err := s.GetOverview(context.Background(), tenant, "all"); err != nil {
		t.Fatalf("all: %v", err)
	}
	if repo.overviewFarm != "" {
		t.Fatalf("'all' must reach the repo as the empty company-wide filter, got %q", repo.overviewFarm)
	}
	if _, err := s.GetOverview(context.Background(), tenant, "CPT"); err != nil {
		t.Fatalf("CPT: %v", err)
	}
	if repo.overviewFarm != "CPT" {
		t.Fatalf("farm filter lost: %q", repo.overviewFarm)
	}
}

func TestGetOverviewRejectsAnUnknownFarmInsteadOfWidening(t *testing.T) {
	repo := &fakeRepo{}
	if _, err := NewSalesService(repo).GetOverview(context.Background(), tenant, "MYSORE"); !errors.Is(err, ErrSalesInvalidFarm) {
		t.Fatalf("want ErrSalesInvalidFarm, got %v", err)
	}
	if repo.overviewCalls != 0 {
		t.Fatal("repo must not be reached with an unknown farm")
	}
}

func TestListDealsClampsLimitAndRejectsDeepOffsets(t *testing.T) {
	repo := &fakeRepo{}
	s := NewSalesService(repo)
	if _, err := s.ListDeals(context.Background(), tenant, DealListQuery{Limit: 5000, Offset: 25}); err != nil {
		t.Fatalf("list: %v", err)
	}
	if repo.listLimit != domain.MaxDealPageSize || repo.listOffset != 25 {
		t.Fatalf("limit/offset = %d/%d", repo.listLimit, repo.listOffset)
	}
	if _, err := s.ListDeals(context.Background(), tenant, DealListQuery{Limit: 0}); err != nil {
		t.Fatalf("default: %v", err)
	}
	if repo.listLimit != domain.DefaultDealPageSize {
		t.Fatalf("default limit = %d", repo.listLimit)
	}
	if _, err := s.ListDeals(context.Background(), tenant, DealListQuery{Offset: domain.MaxDealOffset + 1}); !errors.Is(err, ErrSalesOffsetOutOfRange) {
		t.Fatalf("deep offset must be rejected, got %v", err)
	}
}

func TestCreateDealNormalizesValidatesAndRequiresAKey(t *testing.T) {
	repo := &fakeRepo{}
	s := NewSalesService(repo)
	write := domain.DealWrite{
		SaleDate: "2026-08-17", Farm: "CBE", ProductType: "Goat", Breed: "  Sojat ",
		BuyerName: "  Irshad   Bhai ", SalesValue: 90000,
	}

	// No idempotency key: refused before validation, zero repo calls.
	if _, err := s.CreateDeal(context.Background(), tenant, write, "actor", "  "); !errors.Is(err, ErrSalesIdempotencyKeyRequired) {
		t.Fatalf("want ErrSalesIdempotencyKeyRequired, got %v", err)
	}
	if repo.createCalls != 0 {
		t.Fatal("repo must not be reached without a key")
	}

	if _, err := s.CreateDeal(context.Background(), tenant, write, "actor", " key-1 "); err != nil {
		t.Fatalf("create: %v", err)
	}
	if repo.createdWrite.BuyerName != "Irshad Bhai" || repo.createdWrite.Breed != "Sojat" {
		t.Fatalf("write must be normalized before storage: %+v", repo.createdWrite)
	}
	if repo.createdKey != "key-1" {
		t.Fatalf("key must be trimmed: %q", repo.createdKey)
	}

	// Validation failures never reach the repo: whitespace-only buyer fails AFTER normalization.
	bad := write
	bad.BuyerName = "   "
	if _, err := s.CreateDeal(context.Background(), tenant, bad, "actor", "key-2"); err == nil {
		t.Fatal("blank buyer must be rejected")
	}
	if repo.createCalls != 1 {
		t.Fatalf("invalid write reached the repo: %d calls", repo.createCalls)
	}
}

func TestSalesHTTPErrorMapsTheContract(t *testing.T) {
	if e := SalesHTTPError(ports.ErrIdempotencyConflict); e.HTTPStatus != 409 || e.Code != "idempotency_conflict" {
		t.Fatalf("conflict mapping: %+v", e)
	}
	if e := SalesHTTPError(ErrSalesInvalidFarm); e.HTTPStatus != 400 || e.Code != "invalid_farm" {
		t.Fatalf("farm mapping: %+v", e)
	}
	if e := SalesHTTPError(domain.ErrDealValidation{Field: "sales_value", Reason: "must be more than zero"}); e.Code != "sales_invalid_sales_value" || e.HTTPStatus != 400 {
		t.Fatalf("validation mapping: %+v", e)
	}
	if e := SalesHTTPError(errors.New("pgx: something with table detail")); e.HTTPStatus != 500 || e.Message == "pgx: something with table detail" {
		t.Fatalf("unknown errors must not leak driver text: %+v", e)
	}
}
