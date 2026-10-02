package app

import (
	"context"
	"strings"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/sales/domain"
	"github.com/vgoats/goatos/backend/internal/sales/ports"
)

// AddDealLines adds what was sold to an ADVANCE-ONLY sale (maintainer decision 2026-10-02). The
// lines are judged by exactly the rules recording a sale applies -- the farm's active registry,
// the configured feed list -- so a sale finished later can never hold a line a sale recorded today
// would have refused. Whether the sale is still advance-only, and whether its value covers the
// money already received, is decided by the repository under the deal's row lock.
func (s *SalesService) AddDealLines(ctx context.Context, tenantID, dealID string, write domain.DealLinesWrite, actorID, idempotencyKey string) (domain.Deal, error) {
	key := strings.TrimSpace(idempotencyKey)
	if key == "" {
		return domain.Deal{}, ErrSalesIdempotencyKeyRequired
	}
	dealID = strings.TrimSpace(dealID)
	if dealID == "" {
		return domain.Deal{}, ports.ErrDealNotFound
	}
	catalog, err := s.productCatalog(ctx, tenantID)
	if err != nil {
		return domain.Deal{}, err
	}
	normalized, rollup := write.Normalize(catalog)
	if err := normalized.Validate(catalog, rollup); err != nil {
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
	return s.repo.AddDealLines(ctx, tenantID, dealID, normalized, rollup, actorID, key)
}

// SettleDealAdvance records what became of a FAILED sale's money: refunded in part or whole, the
// rest kept by the farm. The refund date is judged against the IST business day, never a UTC
// instant, like every receipt.
func (s *SalesService) SettleDealAdvance(ctx context.Context, tenantID, dealID string, write domain.AdvanceSettlementWrite, actorID, idempotencyKey string) (domain.Deal, error) {
	key := strings.TrimSpace(idempotencyKey)
	if key == "" {
		return domain.Deal{}, ErrSalesIdempotencyKeyRequired
	}
	dealID = strings.TrimSpace(dealID)
	if dealID == "" {
		return domain.Deal{}, ports.ErrDealNotFound
	}
	normalized := write.Normalize()
	if err := normalized.Validate(biztime.BusinessDayStart(s.now())); err != nil {
		return domain.Deal{}, err
	}
	return s.repo.SettleDealAdvance(ctx, tenantID, dealID, normalized, actorID, key)
}
