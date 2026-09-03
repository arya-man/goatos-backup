package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/procurement/domain"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
)

// fakeFeedPurchaseRepo records what the service passed down, so a test can prove the service
// normalized and gated BEFORE any write reached the database.
type fakeFeedPurchaseRepo struct {
	created  domain.FeedPurchaseWrite
	key      string
	calls    int
	listFarm string
	limit    int
	offset   int

	paymentCalls      int
	paymentPurchaseID string
	payment           domain.FeedPurchasePaymentWrite
	statusCalls       int
	statusPurchaseID  string
	status            string

	editCalls      int
	editPurchaseID string
	edit           domain.FeedPurchaseEdit

	listDelivery       string
	deliveryCalls      int
	deliveryPurchaseID string
	delivery           domain.FeedPurchaseDeliveryWrite
}

func (r *fakeFeedPurchaseRepo) ListFeedPurchases(_ context.Context, _, farm, delivery string, limit, offset int) (ports.FeedPurchasePage, error) {
	r.listFarm, r.listDelivery, r.limit, r.offset = farm, delivery, limit, offset
	return ports.FeedPurchasePage{Total: 7}, nil
}

func (r *fakeFeedPurchaseRepo) RecordFeedPurchaseDelivery(_ context.Context, _, purchaseID string, write domain.FeedPurchaseDeliveryWrite, _ string) (domain.FeedPurchase, error) {
	r.deliveryCalls++
	r.deliveryPurchaseID, r.delivery = purchaseID, write
	return domain.FeedPurchase{FeedPurchaseID: purchaseID, DeliveryStatus: domain.FeedDeliveryReached}, nil
}

func (r *fakeFeedPurchaseRepo) FeedPurchaseOptions(context.Context, string) (ports.FeedPurchaseOptions, error) {
	return ports.FeedPurchaseOptions{}, nil
}

func (r *fakeFeedPurchaseRepo) CreateFeedPurchase(_ context.Context, _ string, write domain.FeedPurchaseWrite, _, key string) (domain.FeedPurchase, error) {
	r.calls++
	r.created, r.key = write, key
	return domain.FeedPurchase{FeedPurchaseID: "created"}, nil
}

func (r *fakeFeedPurchaseRepo) RecordFeedPurchasePayment(_ context.Context, _, purchaseID string, write domain.FeedPurchasePaymentWrite, _, key string) (domain.FeedPurchase, error) {
	r.paymentCalls++
	r.paymentPurchaseID, r.payment, r.key = purchaseID, write, key
	return domain.FeedPurchase{FeedPurchaseID: purchaseID}, nil
}

func (r *fakeFeedPurchaseRepo) SetFeedPurchasePaymentStatus(_ context.Context, _, purchaseID, status, _ string) (domain.FeedPurchase, error) {
	r.statusCalls++
	r.statusPurchaseID, r.status = purchaseID, status
	return domain.FeedPurchase{FeedPurchaseID: purchaseID, PaymentStatus: status}, nil
}

func (r *fakeFeedPurchaseRepo) UpdateFeedPurchase(_ context.Context, _, purchaseID string, edit domain.FeedPurchaseEdit, _ string) (domain.FeedPurchase, error) {
	r.editCalls++
	r.editPurchaseID, r.edit = purchaseID, edit
	return domain.FeedPurchase{FeedPurchaseID: purchaseID}, nil
}

func pinnedClock() func() time.Time {
	// A fixed IST business day, so the future-date rule is judged against a known "today" rather
	// than the wall clock -- a vaccination-grade rule the feed ledger follows too.
	return func() time.Time { return time.Date(2026, 8, 24, 11, 0, 0, 0, time.UTC) }
}

func goodWrite() domain.FeedPurchaseWrite {
	q := 5420.0
	return domain.FeedPurchaseWrite{
		PurchaseDate:  "2026-08-20",
		FarmLabel:     " cpt ",
		FeedItemLabel: "  Dry   Sorghum Forage ",
		QuantityKg:    q,
		Vendor:        " Siddi Srilekha ",
		PaymentStatus: "paid",
	}
}

// TestCreateFeedPurchaseRequiresAnIdempotencyKey pins the write-path contract: a feed load is
// money, and a retried submit must never record it twice -- which on this ledger would also double
// the farm's available stock. A missing key must be refused BEFORE the repository is called.
func TestCreateFeedPurchaseRequiresAnIdempotencyKey(t *testing.T) {
	repo := &fakeFeedPurchaseRepo{}
	svc := NewFeedPurchaseServiceWithClock(repo, pinnedClock())
	for _, key := range []string{"", "   "} {
		if _, err := svc.CreateFeedPurchase(context.Background(), "t", goodWrite(), "actor", key); !errors.Is(err, ErrFeedPurchaseIdempotencyKeyRequired) {
			t.Fatalf("key %q => %v, want ErrFeedPurchaseIdempotencyKeyRequired", key, err)
		}
	}
	if repo.calls != 0 {
		t.Fatalf("the repository must not be reached without a key, got %d calls", repo.calls)
	}
}

// TestCreateFeedPurchaseNormalizesBeforeValidating proves the rules are applied to the values that
// will actually be STORED, and that the trimmed key is what reaches the repository.
func TestCreateFeedPurchaseNormalizesBeforeValidating(t *testing.T) {
	repo := &fakeFeedPurchaseRepo{}
	svc := NewFeedPurchaseServiceWithClock(repo, pinnedClock())
	if _, err := svc.CreateFeedPurchase(context.Background(), "t", goodWrite(), "actor", "  key-1  "); err != nil {
		t.Fatalf("create: %v", err)
	}
	if repo.created.FarmLabel != domain.FeedFarmCPT {
		t.Fatalf("farm reached the repo as %q", repo.created.FarmLabel)
	}
	if repo.created.FeedItemLabel != "Dry Sorghum Forage" || repo.created.Vendor != "Siddi Srilekha" {
		t.Fatalf("feed/vendor reached the repo as %q/%q", repo.created.FeedItemLabel, repo.created.Vendor)
	}
	if repo.created.PaymentStatus != domain.FeedPaymentPaid {
		t.Fatalf("payment status reached the repo as %q", repo.created.PaymentStatus)
	}
	if repo.key != "key-1" {
		t.Fatalf("idempotency key reached the repo as %q", repo.key)
	}
}

// TestCreateFeedPurchaseJudgesTheDateAgainstTheISTBusinessDay pins the clock rule: a load bought
// TODAY in India is recordable, and tomorrow is not -- judged against the IST business day rather
// than a UTC instant, which would call an Indian evening "tomorrow" for five and a half hours
// every night.
func TestCreateFeedPurchaseJudgesTheDateAgainstTheISTBusinessDay(t *testing.T) {
	repo := &fakeFeedPurchaseRepo{}
	// 2026-08-24 20:00 UTC is already 2026-08-25 01:30 IST -- the business day has turned.
	svc := NewFeedPurchaseServiceWithClock(repo, func() time.Time {
		return time.Date(2026, 8, 24, 20, 0, 0, 0, time.UTC)
	})
	today := goodWrite()
	today.PurchaseDate = "2026-08-25"
	if _, err := svc.CreateFeedPurchase(context.Background(), "t", today, "actor", "key"); err != nil {
		t.Fatalf("a load bought on the current IST business day must be recordable: %v", err)
	}

	tomorrow := goodWrite()
	tomorrow.PurchaseDate = "2026-08-26"
	_, err := svc.CreateFeedPurchase(context.Background(), "t", tomorrow, "actor", "key")
	var v domain.ErrFeedPurchaseValidation
	if !errors.As(err, &v) || v.Field != "purchase_date" {
		t.Fatalf("a future load => %v, want a purchase_date rejection", err)
	}
}

// TestListFeedPurchasesRejectsRatherThanWidensOrClamps pins the read gates: an unknown farm is
// refused instead of silently meaning "all", and a page beyond the bounded depth is refused instead
// of being clamped to page 1's rows under page 400's number.
func TestListFeedPurchasesRejectsRatherThanWidensOrClamps(t *testing.T) {
	repo := &fakeFeedPurchaseRepo{}
	svc := NewFeedPurchaseServiceWithClock(repo, pinnedClock())

	if _, err := svc.ListFeedPurchases(context.Background(), "t", FeedPurchaseListQuery{Farm: "HYD"}); !errors.Is(err, ErrFeedPurchaseInvalidFarm) {
		t.Fatalf("unknown farm => %v, want ErrFeedPurchaseInvalidFarm", err)
	}
	if _, err := svc.ListFeedPurchases(context.Background(), "t", FeedPurchaseListQuery{Offset: domain.MaxFeedPurchaseOffset + 1}); !errors.Is(err, ErrFeedPurchaseOffsetOutOfRange) {
		t.Fatalf("out-of-range offset => %v, want ErrFeedPurchaseOffsetOutOfRange", err)
	}
	if _, err := svc.ListFeedPurchases(context.Background(), "t", FeedPurchaseListQuery{Offset: -1}); !errors.Is(err, ErrFeedPurchaseOffsetOutOfRange) {
		t.Fatalf("negative offset => %v, want ErrFeedPurchaseOffsetOutOfRange", err)
	}

	// A blank farm means the whole company, and an unset page size resolves to the default rather
	// than reaching the database as zero.
	if _, err := svc.ListFeedPurchases(context.Background(), "t", FeedPurchaseListQuery{}); err != nil {
		t.Fatalf("default list: %v", err)
	}
	if repo.listFarm != "" || repo.limit != domain.ClampFeedPurchasePageSize(0) {
		t.Fatalf("repo saw farm=%q limit=%d", repo.listFarm, repo.limit)
	}
}

// TestRecordFeedPurchasePaymentGatesBeforeTheRepository pins the payment write's service gates: a
// missing idempotency key and an invalid instalment are refused BEFORE any write reaches the
// database, and a good instalment reaches the repository normalized with the trimmed key.
func TestRecordFeedPurchasePaymentGatesBeforeTheRepository(t *testing.T) {
	repo := &fakeFeedPurchaseRepo{}
	svc := NewFeedPurchaseServiceWithClock(repo, pinnedClock())
	good := domain.FeedPurchasePaymentWrite{PaidOn: "2026-08-20", AmountRupees: 5000, Note: "  advance   at loading "}

	if _, err := svc.RecordFeedPurchasePayment(context.Background(), "t", "p1", good, "actor", "  "); !errors.Is(err, ErrFeedPurchaseIdempotencyKeyRequired) {
		t.Fatalf("blank key => %v, want ErrFeedPurchaseIdempotencyKeyRequired", err)
	}
	bad := good
	bad.AmountRupees = 0
	var v domain.ErrFeedPurchaseValidation
	if _, err := svc.RecordFeedPurchasePayment(context.Background(), "t", "p1", bad, "actor", "key"); !errors.As(err, &v) || v.Field != "amount_rupees" {
		t.Fatalf("zero amount => %v, want an amount_rupees rejection", err)
	}
	if repo.paymentCalls != 0 {
		t.Fatalf("the repository must not be reached by a gated payment, got %d calls", repo.paymentCalls)
	}

	if _, err := svc.RecordFeedPurchasePayment(context.Background(), "t", "p1", good, "actor", " key-9 "); err != nil {
		t.Fatalf("record payment: %v", err)
	}
	if repo.paymentPurchaseID != "p1" || repo.key != "key-9" || repo.payment.Note != "advance at loading" {
		t.Fatalf("repo saw purchase=%q key=%q note=%q", repo.paymentPurchaseID, repo.key, repo.payment.Note)
	}
}

// TestSetFeedPurchasePaymentStatusCanonicalizesAndRejects pins the status edit: "paid" stores as
// the sheet's "Paid", and a word outside the closed vocabulary is refused rather than rewritten --
// a silently defaulted payment state is a money fact nobody entered.
func TestSetFeedPurchasePaymentStatusCanonicalizesAndRejects(t *testing.T) {
	repo := &fakeFeedPurchaseRepo{}
	svc := NewFeedPurchaseServiceWithClock(repo, pinnedClock())

	if _, err := svc.SetFeedPurchasePaymentStatus(context.Background(), "t", "p1", " paid ", "actor"); err != nil {
		t.Fatalf("set status: %v", err)
	}
	if repo.status != domain.FeedPaymentPaid || repo.statusPurchaseID != "p1" {
		t.Fatalf("repo saw status=%q purchase=%q", repo.status, repo.statusPurchaseID)
	}

	var v domain.ErrFeedPurchaseValidation
	if _, err := svc.SetFeedPurchasePaymentStatus(context.Background(), "t", "p1", "Partial", "actor"); !errors.As(err, &v) || v.Field != "payment_status" {
		t.Fatalf("unknown status => %v, want a payment_status rejection", err)
	}
	if repo.statusCalls != 1 {
		t.Fatalf("a rejected status must not reach the repository, got %d calls", repo.statusCalls)
	}
}

// TestEditFeedPurchaseGatesBeforeTheRepository pins the edit write's service gates: an invalid
// edit is refused BEFORE any write reaches the database, and a good edit reaches the repository
// normalized, with the same IST business-day rule the record form applies.
func TestEditFeedPurchaseGatesBeforeTheRepository(t *testing.T) {
	repo := &fakeFeedPurchaseRepo{}
	svc := NewFeedPurchaseServiceWithClock(repo, pinnedClock())
	good := domain.FeedPurchaseEdit{PurchaseDate: "2026-08-20", QuantityKg: 12000, Vendor: "  Siddi   Srilekha "}

	bad := good
	bad.PurchaseDate = "2026-08-26"
	var v domain.ErrFeedPurchaseValidation
	if _, err := svc.EditFeedPurchase(context.Background(), "t", "p1", bad, "actor"); !errors.As(err, &v) || v.Field != "purchase_date" {
		t.Fatalf("future date => %v, want a purchase_date rejection", err)
	}
	bad = good
	bad.QuantityKg = 0
	if _, err := svc.EditFeedPurchase(context.Background(), "t", "p1", bad, "actor"); !errors.As(err, &v) || v.Field != "quantity_kg" {
		t.Fatalf("zero quantity => %v, want a quantity_kg rejection", err)
	}
	if repo.editCalls != 0 {
		t.Fatalf("the repository must not be reached by a gated edit, got %d calls", repo.editCalls)
	}

	if _, err := svc.EditFeedPurchase(context.Background(), "t", "p1", good, "actor"); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if repo.editPurchaseID != "p1" || repo.edit.Vendor != "Siddi Srilekha" {
		t.Fatalf("repo saw purchase=%q vendor=%q", repo.editPurchaseID, repo.edit.Vendor)
	}
}

// TestRecordFeedPurchaseDeliveryGatesBeforeTheRepository pins the arrival write's service gate
// (maintainer decision 2026-09-03): a blank id, a malformed date, a future date (judged on the IST
// business day) and a non-positive received weight are all refused BEFORE the repository is
// called, and a good write reaches it normalized.
func TestRecordFeedPurchaseDeliveryGatesBeforeTheRepository(t *testing.T) {
	repo := &fakeFeedPurchaseRepo{}
	svc := NewFeedPurchaseServiceWithClock(repo, pinnedClock())
	ctx := context.Background()
	zero := 0.0

	if _, err := svc.RecordFeedPurchaseDelivery(ctx, "t", "  ", domain.FeedPurchaseDeliveryWrite{ReachedOn: "2026-08-24"}, "a"); !errors.Is(err, ports.ErrFeedPurchaseNotFound) {
		t.Fatalf("blank id: err = %v", err)
	}
	for name, write := range map[string]domain.FeedPurchaseDeliveryWrite{
		"malformed date": {ReachedOn: "24-08-2026"},
		"future date":    {ReachedOn: "2026-08-25"},
		"zero weight":    {ReachedOn: "2026-08-24", ReachedWeightKg: &zero},
	} {
		_, err := svc.RecordFeedPurchaseDelivery(ctx, "t", "p1", write, "a")
		var v domain.ErrFeedPurchaseValidation
		if !errors.As(err, &v) {
			t.Fatalf("%s: err = %v, want a validation error", name, err)
		}
	}
	if repo.deliveryCalls != 0 {
		t.Fatalf("repository reached %d time(s) by refused writes", repo.deliveryCalls)
	}

	// The pinned clock is 11:00 UTC on the 24th = 16:30 IST on the 24th, so the 24th is today.
	kg := 5380.0
	if _, err := svc.RecordFeedPurchaseDelivery(ctx, "t", "p1", domain.FeedPurchaseDeliveryWrite{ReachedOn: " 2026-08-24 ", ReachedWeightKg: &kg}, "a"); err != nil {
		t.Fatalf("good write: %v", err)
	}
	if repo.deliveryCalls != 1 || repo.deliveryPurchaseID != "p1" || repo.delivery.ReachedOn != "2026-08-24" ||
		repo.delivery.ReachedWeightKg == nil || *repo.delivery.ReachedWeightKg != 5380 {
		t.Fatalf("repository received %+v (%d calls)", repo.delivery, repo.deliveryCalls)
	}
}

// TestListFeedPurchasesDeliveryFilterIsRejectedNeverWidened pins the delivery filter alongside the
// farm one: an unknown word is refused, "all"/blank means every load, and an exact state passes
// through to the repository as-is.
func TestListFeedPurchasesDeliveryFilterIsRejectedNeverWidened(t *testing.T) {
	repo := &fakeFeedPurchaseRepo{}
	svc := NewFeedPurchaseServiceWithClock(repo, pinnedClock())
	ctx := context.Background()

	if _, err := svc.ListFeedPurchases(ctx, "t", FeedPurchaseListQuery{Delivery: "dispatched"}); !errors.Is(err, ErrFeedPurchaseInvalidDelivery) {
		t.Fatalf("unknown delivery filter: err = %v", err)
	}
	if _, err := svc.ListFeedPurchases(ctx, "t", FeedPurchaseListQuery{Delivery: "All"}); err != nil || repo.listDelivery != "" {
		t.Fatalf("all: err=%v delivery=%q", err, repo.listDelivery)
	}
	if _, err := svc.ListFeedPurchases(ctx, "t", FeedPurchaseListQuery{Delivery: " Purchased "}); err != nil || repo.listDelivery != domain.FeedDeliveryPurchased {
		t.Fatalf("purchased: err=%v delivery=%q", err, repo.listDelivery)
	}
}

// TestCreateFeedPurchaseCarriesTheArrivalThrough pins that a record form naming the arrival day
// passes it (trimmed) to the repository, and that an arrival before the purchase or a weight with
// no arrival is refused at the service.
func TestCreateFeedPurchaseCarriesTheArrivalThrough(t *testing.T) {
	repo := &fakeFeedPurchaseRepo{}
	svc := NewFeedPurchaseServiceWithClock(repo, pinnedClock())
	ctx := context.Background()

	early := goodWrite()
	early.ReachedOn = "2026-08-19"
	if _, err := svc.CreateFeedPurchase(ctx, "t", early, "a", "k1"); err == nil {
		t.Fatal("an arrival before the purchase date was accepted")
	}
	kg := 5000.0
	orphanWeight := goodWrite()
	orphanWeight.ReachedWeightKg = &kg
	if _, err := svc.CreateFeedPurchase(ctx, "t", orphanWeight, "a", "k2"); err == nil {
		t.Fatal("a received weight with no arrival day was accepted")
	}
	if repo.calls != 0 {
		t.Fatalf("repository reached %d time(s) by refused writes", repo.calls)
	}

	reached := goodWrite()
	reached.ReachedOn = " 2026-08-23 "
	reached.ReachedWeightKg = &kg
	if _, err := svc.CreateFeedPurchase(ctx, "t", reached, "a", "k3"); err != nil {
		t.Fatalf("reached write: %v", err)
	}
	if repo.created.ReachedOn != "2026-08-23" || !repo.created.IsReached() || repo.created.ReachedWeightKg == nil {
		t.Fatalf("repository received %+v", repo.created)
	}
}
