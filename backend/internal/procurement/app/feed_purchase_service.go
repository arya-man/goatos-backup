package app

import (
	"context"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
)

// FeedPurchaseService serves the feed-purchase ledger and its entry form.
//
// Deliberately thin, the same shape as VendorService and the sales module: a purchase is a
// commercial record with no state machine, no clock, no obligation and no proof, so there is
// nothing to orchestrate beyond validating a write and the read filters. The one rule that needs
// the database -- the feed must exist in the ACTIVE catalog -- is enforced inside the write
// transaction, not here, so it cannot be raced by a catalog retirement between check and insert.
type FeedPurchaseService struct {
	repo ports.FeedPurchaseRepository
	// now is injectable so a test pins the business date rather than depending on the wall clock.
	now func() time.Time
}

func NewFeedPurchaseService(repo ports.FeedPurchaseRepository) *FeedPurchaseService {
	return &FeedPurchaseService{repo: repo, now: time.Now}
}

// NewFeedPurchaseServiceWithClock builds the service against a pinned clock, for tests.
func NewFeedPurchaseServiceWithClock(repo ports.FeedPurchaseRepository, now func() time.Time) *FeedPurchaseService {
	return &FeedPurchaseService{repo: repo, now: now}
}

// FeedPurchaseListQuery is one page request against the ledger.
type FeedPurchaseListQuery struct {
	Farm   string
	Limit  int
	Offset int
}

// ListFeedPurchases returns one ledger page plus the whole-filter totals.
func (s *FeedPurchaseService) ListFeedPurchases(ctx context.Context, tenantID string, q FeedPurchaseListQuery) (ports.FeedPurchasePage, error) {
	farm, ok := domain.NormalizeFeedFarmFilter(q.Farm)
	if !ok {
		return ports.FeedPurchasePage{}, ErrFeedPurchaseInvalidFarm
	}
	if q.Offset < 0 || q.Offset > domain.MaxFeedPurchaseOffset {
		// REJECTED rather than clamped: clamping would serve page 1's rows under page 400's number.
		return ports.FeedPurchasePage{}, ErrFeedPurchaseOffsetOutOfRange
	}
	return s.repo.ListFeedPurchases(ctx, tenantID, farm, domain.ClampFeedPurchasePageSize(q.Limit), q.Offset)
}

// FeedPurchaseOptions returns the entry form's backend-owned vocabularies.
func (s *FeedPurchaseService) FeedPurchaseOptions(ctx context.Context, tenantID string) (ports.FeedPurchaseOptions, error) {
	return s.repo.FeedPurchaseOptions(ctx, tenantID)
}

// CreateFeedPurchase validates and records one purchased load.
//
// The idempotency key is mandatory: a feed load is money, and a retried submit must never record
// the same purchase twice -- which on this ledger would also double the farm's available stock.
func (s *FeedPurchaseService) CreateFeedPurchase(ctx context.Context, tenantID string, write domain.FeedPurchaseWrite, actorID, idempotencyKey string) (domain.FeedPurchase, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return domain.FeedPurchase{}, ErrFeedPurchaseIdempotencyKeyRequired
	}
	normalized := write.Normalize()
	// The purchase date is judged against the IST BUSINESS day, never a UTC instant: a load bought
	// on the evening of the 24th in India is the 24th, and comparing in UTC would call it the 25th
	// for five and a half hours every night.
	if err := normalized.Validate(biztime.BusinessDayStart(s.now())); err != nil {
		return domain.FeedPurchase{}, err
	}
	return s.repo.CreateFeedPurchase(ctx, tenantID, normalized, actorID, strings.TrimSpace(idempotencyKey))
}

// RecordFeedPurchasePayment validates and records one instalment against one load.
//
// The idempotency key is mandatory for the same reason CreateFeedPurchase's is: an instalment is
// money, and a retried submit must never hand the vendor the same amount twice on the ledger.
func (s *FeedPurchaseService) RecordFeedPurchasePayment(ctx context.Context, tenantID, purchaseID string, write domain.FeedPurchasePaymentWrite, actorID, idempotencyKey string) (domain.FeedPurchase, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return domain.FeedPurchase{}, ErrFeedPurchaseIdempotencyKeyRequired
	}
	if strings.TrimSpace(purchaseID) == "" {
		return domain.FeedPurchase{}, ports.ErrFeedPurchaseNotFound
	}
	normalized := write.Normalize()
	if err := normalized.Validate(biztime.BusinessDayStart(s.now())); err != nil {
		return domain.FeedPurchase{}, err
	}
	return s.repo.RecordFeedPurchasePayment(ctx, tenantID, purchaseID, normalized, actorID, strings.TrimSpace(idempotencyKey))
}

// SetFeedPurchasePaymentStatus sets a load's payment status directly -- the edit control for a
// status recorded wrong, or a load settled outside the instalment ledger.
func (s *FeedPurchaseService) SetFeedPurchasePaymentStatus(ctx context.Context, tenantID, purchaseID, status, actorID string) (domain.FeedPurchase, error) {
	if strings.TrimSpace(purchaseID) == "" {
		return domain.FeedPurchase{}, ports.ErrFeedPurchaseNotFound
	}
	canonical, ok := domain.NormalizeFeedPaymentStatus(status)
	if !ok {
		// Rejected, never rewritten to a default: a silently defaulted payment state is a money
		// fact nobody entered.
		return domain.FeedPurchase{}, domain.ErrFeedPurchaseValidation{Field: "payment_status", Reason: "must be Paid or Pending"}
	}
	return s.repo.SetFeedPurchasePaymentStatus(ctx, tenantID, purchaseID, canonical, actorID)
}
