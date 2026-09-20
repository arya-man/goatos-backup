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
	// form is the AUTHORED entry form (2026-09-20); nil until wired, which refuses a write
	// carrying answers rather than storing them unchecked.
	form ports.FeedPurchaseFormSource
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
	Farm string
	// Delivery narrows to one delivery state ("purchased" = still in transit, "reached" = delivered);
	// blank or "all" lists every load.
	Delivery string
	Limit    int
	Offset   int
}

// ListFeedPurchases returns one ledger page plus the whole-filter totals.
func (s *FeedPurchaseService) ListFeedPurchases(ctx context.Context, tenantID string, q FeedPurchaseListQuery) (ports.FeedPurchasePage, error) {
	farm, ok := domain.NormalizeFeedFarmFilter(q.Farm)
	if !ok {
		return ports.FeedPurchasePage{}, ErrFeedPurchaseInvalidFarm
	}
	delivery, ok := domain.NormalizeFeedDeliveryFilter(q.Delivery)
	if !ok {
		return ports.FeedPurchasePage{}, ErrFeedPurchaseInvalidDelivery
	}
	if q.Offset < 0 || q.Offset > domain.MaxFeedPurchaseOffset {
		// REJECTED rather than clamped: clamping would serve page 1's rows under page 400's number.
		return ports.FeedPurchasePage{}, ErrFeedPurchaseOffsetOutOfRange
	}
	return s.repo.ListFeedPurchases(ctx, tenantID, farm, delivery, domain.ClampFeedPurchasePageSize(q.Limit), q.Offset)
}

// FeedPurchaseOptions returns the entry form's backend-owned vocabularies.
func (s *FeedPurchaseService) FeedPurchaseOptions(ctx context.Context, tenantID string) (ports.FeedPurchaseOptions, error) {
	return s.repo.FeedPurchaseOptions(ctx, tenantID)
}

// WithFormSource attaches the authored entry form (THE FEED PURCHASE FORM IS AUTHORED,
// 2026-09-20). Without one a write carrying answers is REFUSED rather than stored unchecked:
// answers nobody checked against a form are not answers to anything.
func (s *FeedPurchaseService) WithFormSource(src ports.FeedPurchaseFormSource) *FeedPurchaseService {
	s.form = src
	return s
}

// formCatalog is the choice vocabulary the form's catalog-backed questions read: the ledger's OWN
// farms, its ACTIVE feed catalog and its two payment states, resolved per request so a feed
// retired this morning stops being offered this afternoon.
func (s *FeedPurchaseService) formCatalog(ctx context.Context, tenantID string) ([]domain.VendorCatalogEntry, error) {
	options, err := s.repo.FeedPurchaseOptions(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	feeds := make([]string, 0, len(options.FeedItems))
	for _, f := range options.FeedItems {
		feeds = append(feeds, f.Label)
	}
	return domain.FeedPurchaseFormCatalog(options.Farms, feeds, options.PaymentStatuses), nil
}

// FeedPurchaseForm is the published form a screen renders now.
func (s *FeedPurchaseService) FeedPurchaseForm(ctx context.Context, tenantID string) (domain.VendorForm, error) {
	if s.form == nil {
		return domain.VendorForm{}, ErrFeedPurchaseFormUnavailable
	}
	catalog, err := s.formCatalog(ctx, tenantID)
	if err != nil {
		return domain.VendorForm{}, err
	}
	return s.form.PublishedFeedPurchaseForm(ctx, tenantID, catalog)
}

// applyForm checks a form-driven write's answers against the form version the client rendered and
// maps the typed answers onto the ledger's columns. A write with no answers (an older client)
// passes through untouched.
func (s *FeedPurchaseService) applyForm(ctx context.Context, tenantID string, write domain.FeedPurchaseWrite) (domain.FeedPurchaseWrite, error) {
	if write.SOPAnswers == nil {
		return write, nil
	}
	if s.form == nil {
		return write, ErrFeedPurchaseFormUnavailable
	}
	if write.QuestionnaireVersion <= 0 {
		return write, ErrFeedPurchaseFormVersionRequired
	}
	catalog, err := s.formCatalog(ctx, tenantID)
	if err != nil {
		return write, err
	}
	form, err := s.form.FeedPurchaseFormVersion(ctx, tenantID, write.QuestionnaireVersion, catalog)
	if err != nil {
		return write, err
	}
	if err := domain.ValidateVendorAnswers(form, write.SOPAnswers); err != nil {
		return write, err
	}
	applied, extras := domain.ApplyFeedPurchaseAnswers(write, write.SOPAnswers)
	applied.SOPAnswers = extras
	applied.QuestionnaireVersion = form.Version
	return applied, nil
}

// CreateFeedPurchase validates and records one purchased load.
//
// The idempotency key is mandatory: a feed load is money, and a retried submit must never record
// the same purchase twice -- which on this ledger would also double the farm's available stock.
func (s *FeedPurchaseService) CreateFeedPurchase(ctx context.Context, tenantID string, write domain.FeedPurchaseWrite, actorID, idempotencyKey string) (domain.FeedPurchase, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return domain.FeedPurchase{}, ErrFeedPurchaseIdempotencyKeyRequired
	}
	applied, err := s.applyForm(ctx, tenantID, write)
	if err != nil {
		return domain.FeedPurchase{}, err
	}
	normalized := applied.Normalize()
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

// EditFeedPurchase validates and applies an edit to an already-recorded load's values.
//
// Same IST business-day rule as recording: a load cannot be re-dated into the future, because
// stock the farm does not have yet must not deplete a feed sheet.
func (s *FeedPurchaseService) EditFeedPurchase(ctx context.Context, tenantID, purchaseID string, edit domain.FeedPurchaseEdit, actorID string) (domain.FeedPurchase, error) {
	if strings.TrimSpace(purchaseID) == "" {
		return domain.FeedPurchase{}, ports.ErrFeedPurchaseNotFound
	}
	normalized := edit.Normalize()
	if err := normalized.Validate(biztime.BusinessDayStart(s.now())); err != nil {
		return domain.FeedPurchase{}, err
	}
	return s.repo.UpdateFeedPurchase(ctx, tenantID, purchaseID, normalized, actorID)
}

// RecordFeedPurchaseDelivery marks a load reached, or corrects an already-reached load's arrival.
//
// Only the shape is validated here (a date, a positive weight): the rule that a load cannot reach
// before it was bought needs the load's own purchase date, which the repository reads under the row
// lock and judges with the same domain rule -- so a concurrent edit of the purchase date cannot
// slip an arrival in before it.
func (s *FeedPurchaseService) RecordFeedPurchaseDelivery(ctx context.Context, tenantID, purchaseID string, write domain.FeedPurchaseDeliveryWrite, actorID string) (domain.FeedPurchase, error) {
	if strings.TrimSpace(purchaseID) == "" {
		return domain.FeedPurchase{}, ports.ErrFeedPurchaseNotFound
	}
	normalized := write.Normalize()
	if err := normalized.Validate("", biztime.BusinessDayStart(s.now())); err != nil {
		return domain.FeedPurchase{}, err
	}
	return s.repo.RecordFeedPurchaseDelivery(ctx, tenantID, purchaseID, normalized, actorID)
}
