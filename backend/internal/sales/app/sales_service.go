// Package app serves the sales module's use cases.
package app

import (
	"context"
	"strings"

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
}

func NewSalesService(repo ports.SalesRepository) *SalesService {
	return &SalesService{repo: repo}
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
