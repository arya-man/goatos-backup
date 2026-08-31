package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/sales/domain"
	"github.com/vgoats/goatos/backend/internal/sales/ports"
)

// fakeRepo records what the service actually asked for, so these tests pin the service's
// validation and filter normalization without a database.
type fakeRepo struct {
	paymentDealID string
	payment       domain.DealPaymentWrite
	paymentKey    string

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

// Pipeline methods: thin recorders, same idea as the deal ones.
func (f *fakeRepo) RecordDealPayment(_ context.Context, _ string, dealID string, write domain.DealPaymentWrite, _ string, key string) (domain.Deal, error) {
	f.paymentDealID, f.payment, f.paymentKey = dealID, write, key
	return domain.Deal{DealID: dealID}, nil
}

func (f *fakeRepo) ListBuyerLeads(_ context.Context, _ string, limit, offset int) (ports.BuyerLeadPage, error) {
	f.listLimit, f.listOffset = limit, offset
	return ports.BuyerLeadPage{Total: 208}, nil
}

func (f *fakeRepo) CreateBuyerLead(_ context.Context, _ string, write domain.BuyerLeadWrite, _ string, key string) (domain.BuyerLead, error) {
	f.createCalls++
	f.createdKey = key
	return domain.BuyerLead{LeadID: "l-1", BuyerName: write.BuyerName}, nil
}

func (f *fakeRepo) SetBuyerLeadStatus(_ context.Context, _ string, leadID string, write domain.LeadStatusWrite, _ string, key string) (domain.BuyerLead, error) {
	f.createCalls++
	f.createdKey = key
	status := write.CallStatus
	return domain.BuyerLead{LeadID: leadID, CallStatus: &status}, nil
}

func (f *fakeRepo) ListFPOLeads(_ context.Context, _ string, limit, offset int) (ports.FPOLeadPage, error) {
	f.listLimit, f.listOffset = limit, offset
	return ports.FPOLeadPage{Total: 53}, nil
}

func (f *fakeRepo) CreateFPOLead(_ context.Context, _ string, write domain.FPOLeadWrite, _ string, key string) (domain.FPOLead, error) {
	f.createCalls++
	f.createdKey = key
	return domain.FPOLead{LeadID: "f-1", FPOName: write.FPOName}, nil
}

func (f *fakeRepo) SetFPOLeadStatus(_ context.Context, _ string, leadID string, write domain.LeadStatusWrite, _ string, key string) (domain.FPOLead, error) {
	f.createCalls++
	f.createdKey = key
	status := write.CallStatus
	return domain.FPOLead{LeadID: leadID, CallStatus: &status}, nil
}

func (f *fakeRepo) CreateBenchmark(_ context.Context, _ string, _ domain.BenchmarkWrite, _ string, key string) error {
	f.createCalls++
	f.createdKey = key
	return nil
}

func (f *fakeRepo) CreateSoldTags(_ context.Context, _ string, write domain.SoldTagsWrite, _ string, key string) (int, error) {
	f.createCalls++
	f.createdKey = key
	return len(write.Rows), nil
}

func (f *fakeRepo) CreateWeightCheck(_ context.Context, _ string, _ domain.WeightCheckWrite, _ string, key string) error {
	f.createCalls++
	f.createdKey = key
	return nil
}

const tenant = "00000000-0000-4000-8000-000000000001"

func TestPipelineWritesRequireIdempotencyKeyAndValidate(t *testing.T) {
	repo := &fakeRepo{}
	s := NewSalesService(repo)
	ctx := context.Background()

	// Every pipeline write refuses a blank Idempotency-Key before touching the repository.
	if _, err := s.CreateBuyerLead(ctx, tenant, domain.BuyerLeadWrite{BuyerName: "Firoz"}, "actor", " "); !errors.Is(err, ErrSalesIdempotencyKeyRequired) {
		t.Fatalf("lead blank key: %v", err)
	}
	if err := s.CreateBenchmark(ctx, tenant, domain.BenchmarkWrite{Breed: "Malai"}, "actor", ""); !errors.Is(err, ErrSalesIdempotencyKeyRequired) {
		t.Fatalf("benchmark blank key: %v", err)
	}
	if repo.createCalls != 0 {
		t.Fatalf("repo touched on refused writes: %d", repo.createCalls)
	}

	// Required fields are rejected with the field named.
	var f domain.ErrFieldValidation
	if _, err := s.CreateBuyerLead(ctx, tenant, domain.BuyerLeadWrite{BuyerName: "  "}, "actor", "k1"); !errors.As(err, &f) || f.Field != "buyer_name" {
		t.Fatalf("blank buyer name: %v", err)
	}
	if _, err := s.CreateFPOLead(ctx, tenant, domain.FPOLeadWrite{}, "actor", "k2"); !errors.As(err, &f) || f.Field != "fpo_name" {
		t.Fatalf("blank fpo name: %v", err)
	}
	if _, err := s.CreateSoldTags(ctx, tenant, domain.SoldTagsWrite{}, "actor", "k3"); !errors.As(err, &f) || f.Field != "rows" {
		t.Fatalf("empty tag rows: %v", err)
	}
	if err := s.CreateWeightCheck(ctx, tenant, domain.WeightCheckWrite{BookWeightKg: 0, VideoWeightKg: 20}, "actor", "k4"); !errors.As(err, &f) || f.Field != "book_weight_kg" {
		t.Fatalf("zero book weight: %v", err)
	}
	// A farm outside the vocabulary is rejected, never rewritten.
	if _, err := s.CreateBuyerLead(ctx, tenant, domain.BuyerLeadWrite{BuyerName: "Firoz", Farm: "HF"}, "actor", "k5"); !errors.As(err, &f) || f.Field != "farm" {
		t.Fatalf("unknown farm: %v", err)
	}
	if repo.createCalls != 0 {
		t.Fatalf("repo touched on rejected writes: %d", repo.createCalls)
	}

	// A valid write normalizes (whitespace collapsed) and reaches the repository with its key.
	lead, err := s.CreateBuyerLead(ctx, tenant, domain.BuyerLeadWrite{BuyerName: "  Firoz   Khan "}, "actor", " k6 ")
	if err != nil {
		t.Fatalf("valid lead: %v", err)
	}
	if lead.BuyerName != "Firoz Khan" {
		t.Fatalf("not normalized: %q", lead.BuyerName)
	}
	if repo.createdKey != "k6" {
		t.Fatalf("key not trimmed: %q", repo.createdKey)
	}
}

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
		// Every app-recorded sale names its buyer from the vendor register. Padded here so the
		// same case proves Normalize trims it before Validate checks its shape.
		BuyerVendorID: " 3f1c2a5e-9b04-4d67-8a11-2c7e5d9f0b34 ",
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
	if repo.createdWrite.BuyerVendorID != "3f1c2a5e-9b04-4d67-8a11-2c7e5d9f0b34" {
		t.Fatalf("vendor id must be trimmed before storage: %q", repo.createdWrite.BuyerVendorID)
	}

	// A sale with no vendor never reaches the repo: the farm does not sell to nobody.
	noVendor := write
	noVendor.BuyerVendorID = ""
	if _, err := s.CreateDeal(context.Background(), tenant, noVendor, "actor", "key-vendor"); err == nil {
		t.Fatal("a sale with no vendor must be rejected")
	}
	if repo.createCalls != 1 {
		t.Fatalf("vendorless write reached the repo: %d calls", repo.createCalls)
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

// TestRecordDealPaymentGatesBeforeTheRepository pins the receipt write's service gates: a missing
// idempotency key and an invalid receipt are refused BEFORE any write reaches the database, and a
// good receipt reaches the repository normalized with the trimmed key.
func TestRecordDealPaymentGatesBeforeTheRepository(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewSalesServiceWithClock(repo, func() time.Time { return time.Date(2026, 8, 31, 11, 0, 0, 0, time.UTC) })
	good := domain.DealPaymentWrite{ReceivedOn: "2026-08-30", AmountRupees: 25000, Note: "  on   pickup "}

	if _, err := svc.RecordDealPayment(context.Background(), "t", "d1", good, "actor", "  "); !errors.Is(err, ErrSalesIdempotencyKeyRequired) {
		t.Fatalf("blank key => %v, want ErrSalesIdempotencyKeyRequired", err)
	}
	bad := good
	bad.AmountRupees = 0
	var v domain.ErrDealValidation
	if _, err := svc.RecordDealPayment(context.Background(), "t", "d1", bad, "actor", "key"); !errors.As(err, &v) || v.Field != "amount_rupees" {
		t.Fatalf("zero amount => %v, want an amount_rupees rejection", err)
	}
	future := good
	future.ReceivedOn = "2026-09-01"
	if _, err := svc.RecordDealPayment(context.Background(), "t", "d1", future, "actor", "key"); !errors.As(err, &v) || v.Field != "received_on" {
		t.Fatalf("future date => %v, want a received_on rejection", err)
	}
	if repo.paymentDealID != "" {
		t.Fatal("the repository must not be reached by a gated receipt")
	}

	if _, err := svc.RecordDealPayment(context.Background(), "t", "d1", good, "actor", " key-3 "); err != nil {
		t.Fatalf("record receipt: %v", err)
	}
	if repo.paymentDealID != "d1" || repo.paymentKey != "key-3" || repo.payment.Note != "on pickup" {
		t.Fatalf("repo saw deal=%q key=%q note=%q", repo.paymentDealID, repo.paymentKey, repo.payment.Note)
	}
}
