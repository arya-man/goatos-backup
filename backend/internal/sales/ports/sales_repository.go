package ports

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/sales/domain"
)

var (
	// ErrDealNotFound is returned when a deal id does not resolve inside the caller's tenant.
	// Deliberately indistinguishable from "exists in another tenant" so the ledger cannot be probed.
	ErrDealNotFound = errors.New("sales: deal not found")
	// ErrDealPaymentNotFound is returned when a receipt id does not belong to the named deal
	// inside the caller's tenant. Same probing rule as ErrDealNotFound.
	ErrDealPaymentNotFound = errors.New("sales: deal payment not found")
	// ErrLeadNotFound is returned when a buyer/FPO lead id does not resolve inside the caller's
	// tenant. Same probing rule as ErrDealNotFound.
	ErrLeadNotFound = errors.New("sales: lead not found")
	// ErrIdempotencyConflict is returned when an Idempotency-Key is replayed with a different
	// request payload. Surfaced as a 409 so the client knows its retry does not match what was
	// originally recorded.
	ErrIdempotencyConflict = errors.New("sales: idempotency key reused with different payload")

	// ErrProductHasSales is returned when deleting an item the farm has already sold. The row
	// leaving would not corrupt the ledger -- a line stores the name and code it was sold under --
	// but it would leave a recorded sale with nothing to look up. Switching the item off is the
	// answer there: gone from every dropdown, history still readable.
	ErrProductHasSales = errors.New("sales: this item has recorded sales and can be switched off but not deleted")

	// ErrProductNotFound is returned when a delete names an item the registry does not carry.
	ErrProductNotFound = errors.New("sales: product not found")

	// ErrProductNameTaken is returned when two products would share one name. A sale stores the
	// NAME it was sold under, so two products cannot answer to one word.
	ErrProductNameTaken = errors.New("sales: another product already has that name")

	// ErrProductNotSellable is returned when a line names a product the tenant's registry no
	// longer carries as active, detected UNDER the writing transaction. It is a conflict rather
	// than a validation failure: the body was right when the form opened, and the farm changed
	// its mind in between. Telling the desk that is more useful than a field error implying they
	// typed something wrong.
	ErrProductNotSellable = errors.New("sales: product is no longer one this farm sells")

	// ErrFeedStockShort is returned when a feed line sells more kilograms than the store's ledger
	// holds, and the caller did not acknowledge it. It is a CONFIRMATION, not a block (maintainer
	// decision 2026-09-23): the sale may genuinely have happened while the purchase ledger is
	// behind, and refusing it outright would make the register lie about feed that physically
	// left. Re-sent with the acknowledgement, the same body records.
	ErrFeedStockShort = errors.New("sales: less feed in the store than this sale takes")
)

// DealPage is one page of the ledger plus the whole-filter total.
//
// Total is the count across the ENTIRE filter, not the page -- per the operational read-model
// contract, a summary is a whole-filter aggregate and pagination changes rows only.
type DealPage struct {
	Deals []domain.Deal
	Total int
}

// SalesRepository is the sales module's persistence boundary.
type SalesRepository interface {
	// GetOverview returns the whole page contract in one read. farm is "" for the whole company
	// or an exact farm code; the caller has already validated it.
	GetOverview(ctx context.Context, tenantID, farm string) (domain.Overview, error)

	// ListDeals returns one ledger page (all statuses), ordered (sale_date DESC, id), plus the
	// whole-filter total.
	ListDeals(ctx context.Context, tenantID, farm string, limit, offset int) (DealPage, error)

	// ListSellableProducts is the tenant's ACTIVE registry of what the farm sells (migration
	// 000402), in farm order. It is read for validation before the write and RE-READ inside the
	// writing transaction: a product archived between the form opening and the save landing must
	// not get through, which is the feed_item_catalog rule one layer up.
	ListSellableProducts(ctx context.Context, tenantID string) ([]domain.Product, error)

	// ListProductVariants answers what each product may be sold AS: an animal product's breeds,
	// a feed product's feed items, an `other` product's own name. Every list is a LIVE vocabulary
	// the farm already maintains, never one typed into this module.
	ListProductVariants(ctx context.Context, tenantID string, products []domain.Product) (map[string][]string, error)

	// ListFeedItems is the farm's configured feed list (feed_item_catalog): the closed set the
	// record-sale form offers when a feed item is sold, and the list the Feed row names.
	ListFeedItems(ctx context.Context, tenantID string) ([]string, error)

	// ListAllSellableProducts reads the registry INCLUDING archived rows, for the editor.
	ListAllSellableProducts(ctx context.Context, tenantID string) ([]domain.ProductRow, error)

	// SaveSellableProduct adds an item to the farm's registry, or edits the one carrying its code.
	SaveSellableProduct(ctx context.Context, tenantID string, write domain.ProductWrite, actorID string) (domain.Product, error)

	// DeleteSellableProduct removes an item, refusing when the farm has already sold any of it.
	DeleteSellableProduct(ctx context.Context, tenantID, code, actorID string) error

	// CreateDeal records a sale. idempotencyKey is the client's Idempotency-Key: the reservation,
	// the insert, and the audit row commit in ONE transaction. An exact replay returns the
	// original deal with zero new side effects; a same-key/different-payload replay returns
	// ErrIdempotencyConflict.
	//
	// The lines' products are re-resolved under the transaction; a product no longer active there
	// returns ErrProductNotSellable rather than recording a sale against a word the farm retired.
	// A feed line writes its kilograms to feed_sale_depletions in the SAME transaction, so a
	// recorded sale and the stock it took off the store can never disagree.
	CreateDeal(ctx context.Context, tenantID string, write domain.DealWrite, actorID, idempotencyKey string) (domain.Deal, error)

	// FeedDemandForDeal is what a RECORDED deal's feed lines take off the store, summed per feed,
	// together with the farm they leave and the persisted status. An already-closed deal
	// needs no further stock check; an expected sale depletes only when it closes.
	//
	// A deal with no feed line returns an empty demand and no error.
	FeedDemandForDeal(ctx context.Context, tenantID, dealID string) (farm string, status string, demand []domain.FeedDemand, err error)

	// SetDealStatus sets a deal's lifecycle status directly (closing an expected sale on the day
	// it happens, or marking one failed). status must already be a canonical vocabulary word.
	SetDealStatus(ctx context.Context, tenantID, dealID, status, actorID string) (domain.Deal, error)

	// RecordDealPayment records one receipt against one deal and, in the SAME transaction,
	// advances the deal's running payment_received total. Same idempotency contract as CreateDeal.
	RecordDealPayment(ctx context.Context, tenantID, dealID string, write domain.DealPaymentWrite, actorID, idempotencyKey string) (domain.Deal, error)

	// UpdateDealPayment edits one receipt and adjusts the running payment_received total by the
	// old/new delta in the SAME transaction.
	UpdateDealPayment(ctx context.Context, tenantID, dealID, paymentID string, write domain.DealPaymentWrite, actorID, idempotencyKey string) (domain.Deal, error)

	// DeleteDealPayment removes one receipt and subtracts its amount from payment_received in the
	// SAME transaction. An idempotent replay returns the already-updated deal.
	DeleteDealPayment(ctx context.Context, tenantID, dealID, paymentID string, actorID, idempotencyKey string) (domain.Deal, error)

	// ListBuyerLeads returns one page of the buyer pipeline (newest first) plus the whole-filter
	// total and the tenant's existing call-status vocabulary (for the status picker).
	ListBuyerLeads(ctx context.Context, tenantID string, filter domain.LeadFilter, limit, offset int) (BuyerLeadPage, error)

	// CreateBuyerLead records a buyer lead. Same one-transaction idempotency contract as CreateDeal.
	CreateBuyerLead(ctx context.Context, tenantID string, write domain.BuyerLeadWrite, actorID, idempotencyKey string) (domain.BuyerLead, error)

	// SetBuyerLeadStatus updates one buyer lead's call status. Same idempotency contract.
	SetBuyerLeadStatus(ctx context.Context, tenantID, leadID string, write domain.LeadStatusWrite, actorID, idempotencyKey string) (domain.BuyerLead, error)
	// UpdateBuyerLead replaces a lead's editable fields -- the path that lets a caller attach the
	// phone number no imported lead carries, and correct a name or place that used to be permanent.
	UpdateBuyerLead(ctx context.Context, tenantID, leadID string, write domain.BuyerLeadWrite, actorID, idempotencyKey string) (domain.BuyerLead, error)

	// ListFPOLeads mirrors ListBuyerLeads for the farmer-group pipeline.
	ListFPOLeads(ctx context.Context, tenantID string, filter domain.LeadFilter, limit, offset int) (FPOLeadPage, error)

	// CreateFPOLead records a farmer-group lead. Same idempotency contract.
	CreateFPOLead(ctx context.Context, tenantID string, write domain.FPOLeadWrite, actorID, idempotencyKey string) (domain.FPOLead, error)

	// SetFPOLeadStatus updates one farmer-group lead's call status. Same idempotency contract.
	SetFPOLeadStatus(ctx context.Context, tenantID, leadID string, write domain.LeadStatusWrite, actorID, idempotencyKey string) (domain.FPOLead, error)
	// UpdateFPOLead replaces a farmer-group lead's editable fields. Same reasoning as the buyer twin.
	UpdateFPOLead(ctx context.Context, tenantID, leadID string, write domain.FPOLeadWrite, actorID, idempotencyKey string) (domain.FPOLead, error)

	// CreateBenchmark records one market quote. Same idempotency contract.
	CreateBenchmark(ctx context.Context, tenantID string, write domain.BenchmarkWrite, actorID, idempotencyKey string) error

	// CreateSoldTags records a handed-over tag list (set-based insert). Same idempotency contract;
	// the whole batch commits or none of it does.
	CreateSoldTags(ctx context.Context, tenantID string, write domain.SoldTagsWrite, actorID, idempotencyKey string) (int, error)

	// CreateWeightCheck records one video-vs-book weight audit row. Same idempotency contract.
	CreateWeightCheck(ctx context.Context, tenantID string, write domain.WeightCheckWrite, actorID, idempotencyKey string) error

	// GetValuationAssumptions reads the tenant's farm valuation assumptions (seeded defaults when
	// no row exists); PutValuationAssumptions replaces them under a row_version fence.
	GetValuationAssumptions(ctx context.Context, tenantID string) (domain.ValuationAssumptions, error)
	PutValuationAssumptions(ctx context.Context, tenantID string, write domain.ValuationAssumptions, actorID string) (domain.ValuationAssumptions, error)
	// ListStageRegister reads the herd's own stage register with each entry's live head count, so
	// the valuation screen offers the farm's stages rather than a typed string.
	ListStageRegister(ctx context.Context, tenantID string) ([]domain.StageRegisterEntry, error)
	// ListSellableSpecies is the species an animal item may name -- those the breed register has
	// live breeds for, so an item added against one is sellable the moment it is saved.
	ListSellableSpecies(ctx context.Context, tenantID string) ([]string, error)
	// CompletedDealForIdempotencyKey answers whether a finished write with this key already
	// produced a deal, so a REPLAY can be settled before the sale is weighed against the feed
	// store -- the store is short by exactly what that committed sale removed, and refusing the
	// replay for it is how one sale became two.
	CompletedDealForIdempotencyKey(ctx context.Context, tenantID, idempotencyKey string) (domain.Deal, bool, error)
}

// ErrValuationVersionConflict is a valuation write carrying a row_version the row has moved past.
var ErrValuationVersionConflict = errors.New("sales: valuation assumptions changed since they were loaded")

// BuyerLeadPage is one page of the buyer pipeline plus the whole-filter total and the existing
// call-status vocabulary (distinct stored statuses, for the picker's suggestions).
type BuyerLeadPage struct {
	Leads         []domain.BuyerLead
	Total         int
	StatusOptions []string
	// StatusFilters is the FACET, which is not the same list as StatusOptions: it leads with the
	// not-yet-called bucket, which is stored as NULL and so can never appear in the write
	// vocabulary. See domain.LeadStatusFilters.
	StatusFilters []domain.LeadStatusFilter
}

// FPOLeadPage mirrors BuyerLeadPage for farmer groups.
type FPOLeadPage struct {
	Leads         []domain.FPOLead
	Total         int
	StatusOptions []string
	StatusFilters []domain.LeadStatusFilter
}

// FeedStockReader answers how many kilograms of one feed the store holds at one farm.
//
// It is a PORT the sales module declares and the feed module implements, rather than sales reading
// the purchase ledger itself: the stock arithmetic -- purchased, less fed, less already sold, with
// the transitional concentrate fold on top -- belongs to feeddirection, and a second
// implementation of it here would be a number that disagrees with the Stock tab the moment either
// side changes.
type FeedStockReader interface {
	// FeedStockIdentity maps substitutable feeds to one stock key and its display label.
	FeedStockIdentity(feedItemLabel string) (key, stockLabel string)
	// FeedBalanceKg is the current balance for (farm, feed). known is false when the store has no
	// ledger for that feed at that farm at all -- which is a different fact from a balance of
	// zero, and must not be reported to the desk as "you have none left".
	FeedBalanceKg(ctx context.Context, tenantID, farmLabel, feedItemLabel string) (kg float64, known bool, err error)
}
