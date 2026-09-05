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

	// CreateDeal records a sale. idempotencyKey is the client's Idempotency-Key: the reservation,
	// the insert, and the audit row commit in ONE transaction. An exact replay returns the
	// original deal with zero new side effects; a same-key/different-payload replay returns
	// ErrIdempotencyConflict.
	CreateDeal(ctx context.Context, tenantID string, write domain.DealWrite, actorID, idempotencyKey string) (domain.Deal, error)

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
}

// BuyerLeadPage is one page of the buyer pipeline plus the whole-filter total and the existing
// call-status vocabulary (distinct stored statuses, for the picker's suggestions).
type BuyerLeadPage struct {
	Leads         []domain.BuyerLead
	Total         int
	StatusOptions []string
}

// FPOLeadPage mirrors BuyerLeadPage for farmer groups.
type FPOLeadPage struct {
	Leads         []domain.FPOLead
	Total         int
	StatusOptions []string
}
