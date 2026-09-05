// Package app serves the sales module's use cases.
package app

import (
	"context"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/sales/domain"
	"github.com/vgoats/goatos/backend/internal/sales/ports"
)

// SalesService serves the sales overview and ledger.
//
// It is deliberately thin, the same shape as procurement's VendorService: the ledger is a
// commercial record with no state machine, no clock, no obligation and no proof, so there is
// nothing for a service layer to orchestrate beyond validating a write and the read filters.
type SalesService struct {
	repo ports.SalesRepository
	// now is injectable so a test pins the business date rather than depending on the wall clock.
	now func() time.Time
}

func NewSalesService(repo ports.SalesRepository) *SalesService {
	return &SalesService{repo: repo, now: time.Now}
}

// NewSalesServiceWithClock builds the service against a pinned clock, for tests.
func NewSalesServiceWithClock(repo ports.SalesRepository, now func() time.Time) *SalesService {
	return &SalesService{repo: repo, now: now}
}

// GetOverview returns the whole page contract for one farm scope.
func (s *SalesService) GetOverview(ctx context.Context, tenantID, farmRaw string) (domain.Overview, error) {
	farm, ok := domain.NormalizeFarmFilter(farmRaw)
	if !ok {
		// REJECTED rather than widened: an unknown farm silently treated as "all" would show the
		// caller company numbers under a farm label.
		return domain.Overview{}, ErrSalesInvalidFarm
	}
	return s.repo.GetOverview(ctx, tenantID, farm)
}

// DealListQuery is one page request against the ledger.
type DealListQuery struct {
	Farm   string
	Limit  int
	Offset int
}

// ListDeals returns one ledger page plus the whole-filter total.
func (s *SalesService) ListDeals(ctx context.Context, tenantID string, q DealListQuery) (ports.DealPage, error) {
	farm, ok := domain.NormalizeFarmFilter(q.Farm)
	if !ok {
		return ports.DealPage{}, ErrSalesInvalidFarm
	}
	if q.Offset < 0 || q.Offset > domain.MaxDealOffset {
		// REJECTED rather than clamped: clamping would serve page 1's rows under page 400's number.
		return ports.DealPage{}, ErrSalesOffsetOutOfRange
	}
	return s.repo.ListDeals(ctx, tenantID, farm, domain.ClampDealPageSize(q.Limit), q.Offset)
}

// CreateDeal validates and records a sale.
//
// Normalize runs BEFORE Validate so the rules apply to the values that will actually be stored: a
// buyer name of "   " must fail the required check, not pass it because it was non-empty before
// trimming. The idempotency key is mandatory -- a sale is money, and a retried submit must never
// record it twice.
func (s *SalesService) CreateDeal(ctx context.Context, tenantID string, write domain.DealWrite, actorID, idempotencyKey string) (domain.Deal, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return domain.Deal{}, ErrSalesIdempotencyKeyRequired
	}
	normalized := write.Normalize()
	if err := normalized.Validate(); err != nil {
		return domain.Deal{}, err
	}
	return s.repo.CreateDeal(ctx, tenantID, normalized, actorID, strings.TrimSpace(idempotencyKey))
}

// SetDealStatus sets a deal's lifecycle status -- the edit that closes an expected sale on the
// day the animals actually leave. The vocabulary is closed; an unrecognised word is rejected,
// never rewritten to a default.
func (s *SalesService) SetDealStatus(ctx context.Context, tenantID, dealID, status, actorID string) (domain.Deal, error) {
	if strings.TrimSpace(dealID) == "" {
		return domain.Deal{}, ports.ErrDealNotFound
	}
	trimmed := strings.TrimSpace(status)
	canonical := ""
	for _, known := range domain.Statuses {
		if strings.EqualFold(trimmed, known) {
			canonical = known
			break
		}
	}
	if canonical == "" {
		return domain.Deal{}, domain.ErrDealValidation{Field: "status", Reason: "must be Deal Closed, Deal Failed, In Discussion or Advance Paid"}
	}
	return s.repo.SetDealStatus(ctx, tenantID, dealID, canonical, actorID)
}

// RecordDealPayment validates and records one receipt against one deal.
//
// The idempotency key is mandatory for the same reason CreateDeal's is: a receipt is money, and a
// retried submit must never count the same amount from the buyer twice.
func (s *SalesService) RecordDealPayment(ctx context.Context, tenantID, dealID string, write domain.DealPaymentWrite, actorID, idempotencyKey string) (domain.Deal, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return domain.Deal{}, ErrSalesIdempotencyKeyRequired
	}
	if strings.TrimSpace(dealID) == "" {
		return domain.Deal{}, ports.ErrDealNotFound
	}
	normalized := write.Normalize()
	// The received date is judged against the IST BUSINESS day, never a UTC instant, same as the
	// feed-purchase instalment rule.
	if err := normalized.Validate(biztime.BusinessDayStart(s.now())); err != nil {
		return domain.Deal{}, err
	}
	return s.repo.RecordDealPayment(ctx, tenantID, dealID, normalized, actorID, strings.TrimSpace(idempotencyKey))
}

// UpdateDealPayment edits a receipt against a deal.
//
// The edited amount is an absolute replacement, not another receipt, so the repository applies the
// old/new delta under the deal lock.
func (s *SalesService) UpdateDealPayment(ctx context.Context, tenantID, dealID, paymentID string, write domain.DealPaymentWrite, actorID, idempotencyKey string) (domain.Deal, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return domain.Deal{}, ErrSalesIdempotencyKeyRequired
	}
	if strings.TrimSpace(dealID) == "" {
		return domain.Deal{}, ports.ErrDealNotFound
	}
	if strings.TrimSpace(paymentID) == "" {
		return domain.Deal{}, ports.ErrDealPaymentNotFound
	}
	normalized := write.Normalize()
	if err := normalized.Validate(biztime.BusinessDayStart(s.now())); err != nil {
		return domain.Deal{}, err
	}
	return s.repo.UpdateDealPayment(ctx, tenantID, strings.TrimSpace(dealID), strings.TrimSpace(paymentID), normalized, actorID, strings.TrimSpace(idempotencyKey))
}

// DeleteDealPayment removes a receipt from a deal.
func (s *SalesService) DeleteDealPayment(ctx context.Context, tenantID, dealID, paymentID string, actorID, idempotencyKey string) (domain.Deal, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return domain.Deal{}, ErrSalesIdempotencyKeyRequired
	}
	if strings.TrimSpace(dealID) == "" {
		return domain.Deal{}, ports.ErrDealNotFound
	}
	if strings.TrimSpace(paymentID) == "" {
		return domain.Deal{}, ports.ErrDealPaymentNotFound
	}
	return s.repo.DeleteDealPayment(ctx, tenantID, strings.TrimSpace(dealID), strings.TrimSpace(paymentID), actorID, strings.TrimSpace(idempotencyKey))
}

// LeadListQuery is one page request against a pipeline list.
type LeadListQuery struct {
	// Filter narrows the board to what the caller is looking for -- a name/place/phone search and a
	// call-status facet. Empty lists the board unfiltered, which is what every caller did before the
	// boards could reach past their newest 20 rows.
	Filter domain.LeadFilter
	Limit  int
	Offset int
}

func (q LeadListQuery) validate() error {
	if q.Offset < 0 || q.Offset > domain.MaxLeadOffset {
		return ErrSalesOffsetOutOfRange
	}
	return nil
}

// requireKey enforces the mandatory Idempotency-Key on every pipeline write.
func requireKey(idempotencyKey string) (string, error) {
	trimmed := strings.TrimSpace(idempotencyKey)
	if trimmed == "" {
		return "", ErrSalesIdempotencyKeyRequired
	}
	return trimmed, nil
}

// ListBuyerLeads returns one buyer-pipeline page plus total and status vocabulary.
func (s *SalesService) ListBuyerLeads(ctx context.Context, tenantID string, q LeadListQuery) (ports.BuyerLeadPage, error) {
	if err := q.validate(); err != nil {
		return ports.BuyerLeadPage{}, err
	}
	return s.repo.ListBuyerLeads(ctx, tenantID, q.Filter, domain.ClampLeadPageSize(q.Limit), q.Offset)
}

// CreateBuyerLead validates and records a buyer lead.
func (s *SalesService) CreateBuyerLead(ctx context.Context, tenantID string, write domain.BuyerLeadWrite, actorID, idempotencyKey string) (domain.BuyerLead, error) {
	key, err := requireKey(idempotencyKey)
	if err != nil {
		return domain.BuyerLead{}, err
	}
	normalized := write.Normalize()
	if err := normalized.Validate(); err != nil {
		return domain.BuyerLead{}, err
	}
	return s.repo.CreateBuyerLead(ctx, tenantID, normalized, actorID, key)
}

// SetBuyerLeadStatus validates and applies a buyer lead's new call status.
func (s *SalesService) SetBuyerLeadStatus(ctx context.Context, tenantID, leadID string, write domain.LeadStatusWrite, actorID, idempotencyKey string) (domain.BuyerLead, error) {
	key, err := requireKey(idempotencyKey)
	if err != nil {
		return domain.BuyerLead{}, err
	}
	normalized := write.Normalize()
	if err := normalized.Validate(); err != nil {
		return domain.BuyerLead{}, err
	}
	return s.repo.SetBuyerLeadStatus(ctx, tenantID, leadID, normalized, actorID, key)
}

// UpdateBuyerLead validates and applies an edit to a buyer lead.
//
// Held to the SAME bar as a create (ValidateForCreate's shared Validate): an edit that could store
// what a create refuses would be a way around the rules, one screen over.
func (s *SalesService) UpdateBuyerLead(ctx context.Context, tenantID, leadID string, write domain.BuyerLeadWrite, actorID, idempotencyKey string) (domain.BuyerLead, error) {
	key, err := requireKey(idempotencyKey)
	if err != nil {
		return domain.BuyerLead{}, err
	}
	normalized := write.Normalize()
	if err := normalized.Validate(); err != nil {
		return domain.BuyerLead{}, err
	}
	return s.repo.UpdateBuyerLead(ctx, tenantID, leadID, normalized, actorID, key)
}

// UpdateFPOLead validates and applies an edit to a farmer-group lead.
func (s *SalesService) UpdateFPOLead(ctx context.Context, tenantID, leadID string, write domain.FPOLeadWrite, actorID, idempotencyKey string) (domain.FPOLead, error) {
	key, err := requireKey(idempotencyKey)
	if err != nil {
		return domain.FPOLead{}, err
	}
	normalized := write.Normalize()
	if err := normalized.Validate(); err != nil {
		return domain.FPOLead{}, err
	}
	return s.repo.UpdateFPOLead(ctx, tenantID, leadID, normalized, actorID, key)
}

// ListFPOLeads returns one farmer-group page plus total and status vocabulary.
func (s *SalesService) ListFPOLeads(ctx context.Context, tenantID string, q LeadListQuery) (ports.FPOLeadPage, error) {
	if err := q.validate(); err != nil {
		return ports.FPOLeadPage{}, err
	}
	return s.repo.ListFPOLeads(ctx, tenantID, q.Filter, domain.ClampLeadPageSize(q.Limit), q.Offset)
}

// CreateFPOLead validates and records a farmer-group lead.
func (s *SalesService) CreateFPOLead(ctx context.Context, tenantID string, write domain.FPOLeadWrite, actorID, idempotencyKey string) (domain.FPOLead, error) {
	key, err := requireKey(idempotencyKey)
	if err != nil {
		return domain.FPOLead{}, err
	}
	normalized := write.Normalize()
	if err := normalized.Validate(); err != nil {
		return domain.FPOLead{}, err
	}
	return s.repo.CreateFPOLead(ctx, tenantID, normalized, actorID, key)
}

// SetFPOLeadStatus validates and applies a farmer-group lead's new call status.
func (s *SalesService) SetFPOLeadStatus(ctx context.Context, tenantID, leadID string, write domain.LeadStatusWrite, actorID, idempotencyKey string) (domain.FPOLead, error) {
	key, err := requireKey(idempotencyKey)
	if err != nil {
		return domain.FPOLead{}, err
	}
	normalized := write.Normalize()
	if err := normalized.Validate(); err != nil {
		return domain.FPOLead{}, err
	}
	return s.repo.SetFPOLeadStatus(ctx, tenantID, leadID, normalized, actorID, key)
}

// CreateBenchmark validates and records a market quote.
func (s *SalesService) CreateBenchmark(ctx context.Context, tenantID string, write domain.BenchmarkWrite, actorID, idempotencyKey string) error {
	key, err := requireKey(idempotencyKey)
	if err != nil {
		return err
	}
	normalized := write.Normalize()
	if err := normalized.Validate(); err != nil {
		return err
	}
	return s.repo.CreateBenchmark(ctx, tenantID, normalized, actorID, key)
}

// CreateSoldTags validates and records a handed-over tag list, returning how many animals landed.
func (s *SalesService) CreateSoldTags(ctx context.Context, tenantID string, write domain.SoldTagsWrite, actorID, idempotencyKey string) (int, error) {
	key, err := requireKey(idempotencyKey)
	if err != nil {
		return 0, err
	}
	normalized := write.Normalize()
	if err := normalized.Validate(); err != nil {
		return 0, err
	}
	return s.repo.CreateSoldTags(ctx, tenantID, normalized, actorID, key)
}

// CreateWeightCheck validates and records one video-vs-book weight audit row.
func (s *SalesService) CreateWeightCheck(ctx context.Context, tenantID string, write domain.WeightCheckWrite, actorID, idempotencyKey string) error {
	key, err := requireKey(idempotencyKey)
	if err != nil {
		return err
	}
	normalized := write.Normalize()
	if err := normalized.Validate(); err != nil {
		return err
	}
	return s.repo.CreateWeightCheck(ctx, tenantID, normalized, actorID, key)
}
