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
}
