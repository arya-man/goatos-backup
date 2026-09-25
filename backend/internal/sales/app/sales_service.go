// Package app serves the sales module's use cases.
package app

import (
	"context"
	"errors"
	"github.com/google/uuid"
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
	// feedStock answers what the store holds, for the short-feed-sale confirmation. It is OPTIONAL
	// -- a service built without it records feed sales with no warning, which is the behaviour
	// before this existed and is what the fakes in the unit tests exercise. Production wires it.
	feedStock ports.FeedStockReader
	// now is injectable so a test pins the business date rather than depending on the wall clock.
	now func() time.Time
}

// WithFeedStock wires the reader that answers how much feed the store holds, so a sale taking more
// than that can ask the desk to confirm it once.
func (s *SalesService) WithFeedStock(reader ports.FeedStockReader) *SalesService {
	s.feedStock = reader
	return s
}

func NewSalesService(repo ports.SalesRepository) *SalesService {
	return &SalesService{repo: repo, now: time.Now}
}

// NewSalesServiceWithClock builds the service against a pinned clock, for tests.
func NewSalesServiceWithClock(repo ports.SalesRepository, now func() time.Time) *SalesService {
	return &SalesService{repo: repo, now: now}
}

// ListFarms returns the tenant's active park codes: every farm a sale may be recorded against.
func (s *SalesService) ListFarms(ctx context.Context, tenantID string) ([]string, error) {
	return s.repo.ListFarms(ctx, tenantID)
}

// GetOverview returns the whole page contract for one farm scope.
func (s *SalesService) GetOverview(ctx context.Context, tenantID, farmRaw string) (domain.Overview, error) {
	farms, err := s.repo.ListFarms(ctx, tenantID)
	if err != nil {
		return domain.Overview{}, err
	}
	farm, ok := domain.NormalizeFarmFilter(farmRaw, farms)
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
	farms, err := s.repo.ListFarms(ctx, tenantID)
	if err != nil {
		return ports.DealPage{}, err
	}
	farm, ok := domain.NormalizeFarmFilter(q.Farm, farms)
	if !ok {
		return ports.DealPage{}, ErrSalesInvalidFarm
	}
	if q.Offset < 0 || q.Offset > domain.MaxDealOffset {
		// REJECTED rather than clamped: clamping would serve page 1's rows under page 400's number.
		return ports.DealPage{}, ErrSalesOffsetOutOfRange
	}
	return s.repo.ListDeals(ctx, tenantID, farm, domain.ClampDealPageSize(q.Limit), q.Offset)
}

// GetDeal reads one sale, tenant-scoped, in the same shape as a ledger row. An id that is not even
// a uuid is answered as not-found here rather than reaching Postgres, where it would be a type
// error and a 500.
func (s *SalesService) GetDeal(ctx context.Context, tenantID, dealID string) (domain.Deal, error) {
	id := strings.TrimSpace(dealID)
	if _, err := uuid.Parse(id); err != nil {
		return domain.Deal{}, ports.ErrDealNotFound
	}
	return s.repo.GetDeal(ctx, tenantID, id)
}

// CreateDeal validates and records a sale.
//
// Normalize runs BEFORE Validate so the rules apply to the values that will actually be stored: a
// buyer name of "   " must fail the required check, not pass it because it was non-empty before
// trimming. The idempotency key is mandatory -- a sale is money, and a retried submit must never
// record it twice.
//
// The registry of what the farm sells (migration 000422) is read here so the desk gets a FIELD
// error naming the line it got wrong. The repository re-resolves it under the writing
// transaction, which is where a product archived in between is caught; this read is for the
// message, never for the guarantee.
func (s *SalesService) CreateDeal(ctx context.Context, tenantID string, write domain.DealWrite, actorID, idempotencyKey string) (domain.Deal, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return domain.Deal{}, ErrSalesIdempotencyKeyRequired
	}
	// A REPLAY IS ANSWERED BEFORE THE STORE IS ASKED. The first send commits and takes its
	// kilograms off; if its response is lost, the phone re-sends the same key and the store is
	// now short by exactly what THIS sale removed -- so asking the store first refused a sale that
	// had already happened. The phone read that refusal as the short-stock question, minted a
	// fresh key to answer it, and recorded the sale again: one sale, two deals, twice the
	// depletion. Settling the replay here returns the deal that already exists.
	// Do this before catalog lookup too: archiving or renaming a product after commit
	// cannot turn a completed sale into a rejected outbox item. Completed keys return
	// the original result without new effects, even if a retry carries changed fields.
	key := strings.TrimSpace(idempotencyKey)
	if existing, found, err := s.repo.CompletedDealForIdempotencyKey(ctx, tenantID, key); err != nil {
		return domain.Deal{}, err
	} else if found {
		return existing, nil
	}
	catalog, err := s.productCatalog(ctx, tenantID)
	if err != nil {
		return domain.Deal{}, err
	}
	farms, err := s.repo.ListFarms(ctx, tenantID)
	if err != nil {
		return domain.Deal{}, err
	}
	normalized := write.Normalize(catalog)
	if err := normalized.Validate(catalog, farms); err != nil {
		return domain.Deal{}, err
	}
	if len(domain.AggregateFeedDemand(normalized.Lines)) > 0 {
		items, err := s.repo.ListFeedItems(ctx, tenantID)
		if err != nil {
			return domain.Deal{}, err
		}
		if err := domain.ValidateFeedItems(normalized.Lines, items); err != nil {
			return domain.Deal{}, err
		}
	}
	if err := s.confirmFeedStock(ctx, tenantID, normalized); err != nil {
		return domain.Deal{}, err
	}
	return s.repo.CreateDeal(ctx, tenantID, normalized, actorID, key)
}

// ErrNothingSellable is returned when a tenant's registry carries no active product. A sale is
// refused rather than recorded against a guess: an empty registry means nobody has said what this
// farm sells, and defaulting to 'Sheep' on its behalf is how a constant grows back.
var ErrNothingSellable = errors.New("sales: this farm has no products set up to sell")

// productCatalog reads the tenant's active registry.
func (s *SalesService) productCatalog(ctx context.Context, tenantID string) (domain.ProductCatalog, error) {
	rows, err := s.repo.ListSellableProducts(ctx, tenantID)
	if err != nil {
		return domain.ProductCatalog{}, err
	}
	catalog := domain.NewProductCatalog(rows)
	if catalog.IsEmpty() {
		return domain.ProductCatalog{}, ErrNothingSellable
	}
	return catalog, nil
}

// SellableProducts serves the tenant's registry, with each product's variants, to the forms that
// offer them. Both halves come from the farm's own live data: what it sells, and what each of
// those may be sold as.
func (s *SalesService) SellableProducts(ctx context.Context, tenantID string) ([]domain.Product, map[string][]string, error) {
	rows, err := s.repo.ListSellableProducts(ctx, tenantID)
	if err != nil {
		return nil, nil, err
	}
	products := domain.NewProductCatalog(rows).Products()
	variants, err := s.repo.ListProductVariants(ctx, tenantID, products)
	if err != nil {
		return nil, nil, err
	}
	return products, variants, nil
}

// confirmFeedStock warns -- once -- when a feed line sells more than the store's ledger holds.
//
// It is a CONFIRMATION and not a gate (maintainer decision 2026-09-23): the feed may genuinely
// have left while the purchase ledger is behind, and refusing the sale would make the register lie
// about a load that physically went. Once the desk has seen the balance and re-sent the same sale,
// it records.
//
// A farm with no stock reader wired, or a feed the store has no ledger for at all, raises NOTHING.
// "The ledger has never heard of this feed" is a different fact from "the ledger says you have
// none left", and warning on the first would teach the desk to tick past the second.
func (s *SalesService) confirmFeedStock(ctx context.Context, tenantID string, write domain.DealWrite) error {
	if write.StockShortfallAcknowledged {
		return nil
	}
	// STOCK CHECK ONLY AT CLOSE (maintainer decision 2026-09-25): an OPEN sale (In Discussion,
	// Advance Paid) takes nothing off the store until it closes, and the status change to Deal
	// Closed asks then. Only a sale recorded AS closed -- a blank status records the sheet's
	// default, Deal Closed -- is weighed here.
	if st := strings.TrimSpace(write.Status); st != "" && st != domain.StatusDealClosed {
		return nil
	}
	return s.weighAgainstTheStore(ctx, tenantID, write.Farm, domain.AggregateFeedDemand(write.Lines))
}

// weighAgainstTheStore asks the store, once per feed, whether it holds what is about to leave it.
//
// Both moments that take feed off the store come through here -- recording a sale, and CLOSING an
// expected one, which is when a sale recorded as in-discussion finally depletes. Closing used to
// skip the question entirely: a sale recorded in March against a full store closed in June against
// an empty one, and the kilograms came off with nobody told.
func (s *SalesService) weighAgainstTheStore(ctx context.Context, tenantID, farm string, demand []domain.FeedDemand) error {
	if s.feedStock == nil || len(demand) == 0 {
		return nil
	}
	// Aggregate at the store's identity, not the sold label: legacy concentrate
	// siblings and their successor spend one shared balance. Comparing each line
	// separately would approve two 60 kg lines against the same 100 kg store.
	grouped := []domain.FeedDemand{}
	positions := map[string]int{}
	for _, d := range demand {
		key, label := s.feedStock.FeedStockIdentity(d.FeedItem)
		if i, ok := positions[key]; ok {
			grouped[i].Kg += d.Kg
			continue
		}
		positions[key] = len(grouped)
		d.FeedItem = label
		grouped = append(grouped, d)
	}
	balances, err := s.feedStock.FeedBalancesKg(ctx, tenantID, farm)
	if err != nil {
		return err
	}
	short := []domain.FeedStockShortfall{}
	for _, d := range grouped {
		key, _ := s.feedStock.FeedStockIdentity(d.FeedItem)
		balance, known := balances[key]
		if !known || balance >= d.Kg {
			continue
		}
		short = append(short, domain.FeedStockShortfall{
			LineNo: d.LineNo, FeedItem: d.FeedItem, FarmLabel: farm,
			RequestedKg: d.Kg, BalanceKg: balance,
		})
	}
	if len(short) == 0 {
		return nil
	}
	return domain.ErrFeedStockShort{Shortfalls: short}
}

// ListSellableProducts serves the registry to its EDITOR, archived rows included: a list a person
// maintains must show what is switched off, or they cannot switch it back on.
func (s *SalesService) ListSellableProducts(ctx context.Context, tenantID string) ([]domain.ProductRow, error) {
	return s.repo.ListAllSellableProducts(ctx, tenantID)
}

// FeedItems is the farm's configured feed list -- the same feed_item_catalog rows the ration grid
// and the feed purchases are authored against. It is what the record-sale form offers when a feed
// item is being sold, and what the item editor names beside the Feed row so a reader can see the
// list is theirs.
func (s *SalesService) FeedItems(ctx context.Context, tenantID string) ([]string, error) {
	return s.repo.ListFeedItems(ctx, tenantID)
}

// SaveSellableProduct adds an item to the farm's registry, or edits one.
//
// The code is derived from the name when adding and kept when editing, so renaming an item leaves
// every sale recorded under it pointing at the same row.
func (s *SalesService) SaveSellableProduct(ctx context.Context, tenantID string, write domain.ProductWrite, actorID string) (domain.Product, error) {
	normalized := write.Normalize()
	if err := normalized.Validate(); err != nil {
		return domain.Product{}, err
	}
	return s.repo.SaveSellableProduct(ctx, tenantID, normalized, actorID)
}

// DeleteSellableProduct removes an item, refusing when the farm has already sold any of it.
func (s *SalesService) DeleteSellableProduct(ctx context.Context, tenantID, code, actorID string) error {
	if strings.TrimSpace(code) == "" {
		return ports.ErrProductNotFound
	}
	return s.repo.DeleteSellableProduct(ctx, tenantID, strings.TrimSpace(code), actorID)
}

// SetDealStatus sets a deal's lifecycle status -- the edit that closes an expected sale on the
// day the animals actually leave. The vocabulary is closed; an unrecognised word is rejected,
// never rewritten to a default.
func (s *SalesService) SetDealStatus(ctx context.Context, tenantID, dealID, status string, stockShortfallAcknowledged bool, actorID string) (domain.Deal, error) {
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
	// CLOSING IS WHEN AN EXPECTED SALE TAKES ITS FEED. The depletion ledger is written only for a
	// closed deal, so a sale recorded as in-discussion leaves the store untouched until this
	// moment -- and by this moment the store has moved. The same question the record asked is
	// asked again here, against today's balance, and answered the same way: shown once, and
	// re-sent with the acknowledgement by a desk that has checked.
	if canonical == domain.StatusDealClosed && !stockShortfallAcknowledged {
		farm, currentStatus, demand, err := s.repo.FeedDemandForDeal(ctx, tenantID, dealID)
		if err != nil {
			return domain.Deal{}, err
		}
		// A failed sale is final: refuse before asking the store any question about it.
		if !domain.StatusChangeAllowed(currentStatus, canonical) {
			return domain.Deal{}, domain.ErrDealFailedIsFinal
		}
		// A completed close has already depleted stock. A lost response must replay
		// through the repository's no-op status update, not weigh that sale twice.
		if currentStatus != domain.StatusDealClosed {
			if err := s.weighAgainstTheStore(ctx, tenantID, farm, demand); err != nil {
				return domain.Deal{}, err
			}
		}
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
	farms, err := s.repo.ListFarms(ctx, tenantID)
	if err != nil {
		return domain.BuyerLead{}, err
	}
	normalized := write.Normalize()
	if err := normalized.Validate(farms); err != nil {
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
	farms, err := s.repo.ListFarms(ctx, tenantID)
	if err != nil {
		return domain.BuyerLead{}, err
	}
	normalized := write.Normalize()
	if err := normalized.Validate(farms); err != nil {
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
	farms, err := s.repo.ListFarms(ctx, tenantID)
	if err != nil {
		return 0, err
	}
	normalized := write.Normalize()
	if err := normalized.Validate(farms); err != nil {
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

// GetValuationAssumptions serves the farm valuation assumptions (maintainer instruction 2026-09-19).
func (s *SalesService) GetValuationAssumptions(ctx context.Context, tenantID string) (domain.ValuationAssumptions, error) {
	return s.repo.GetValuationAssumptions(ctx, tenantID)
}

// ListStageRegister serves the pickable stages for the valuation screen.
func (s *SalesService) ListStageRegister(ctx context.Context, tenantID string) ([]domain.StageRegisterEntry, error) {
	return s.repo.ListStageRegister(ctx, tenantID)
}

// SellableSpecies is the species an animal item may be sold as.
func (s *SalesService) SellableSpecies(ctx context.Context, tenantID string) ([]string, error) {
	return s.repo.ListSellableSpecies(ctx, tenantID)
}

// PutValuationAssumptions validates and replaces them. Figures outside their business band are
// REFUSED (400), never clamped; a stale row_version is a 409.
func (s *SalesService) PutValuationAssumptions(ctx context.Context, tenantID string, write domain.ValuationAssumptions, actorID string) (domain.ValuationAssumptions, error) {
	domain.NormalizeValuationAssumptions(&write)
	if err := domain.ValidateValuationAssumptions(write); err != nil {
		return domain.ValuationAssumptions{}, err
	}
	return s.repo.PutValuationAssumptions(ctx, tenantID, write, actorID)
}
