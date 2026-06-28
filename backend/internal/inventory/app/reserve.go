package app

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/vgoats/goatos/backend/internal/inventory/ports"
)

var (
	ErrStockUnavailable  = errors.New("inventory: stock unavailable")
	ErrInsufficientStock = errors.New("inventory: insufficient stock")
)

// ReserveForBatch reserves exactly qty doses for a batch. The drive's (possibly shed-scoped) location
// is rolled up to the nearest ancestor whose active FEFO lots TOGETHER cover qty, and those lots are
// consumed earliest-expiry first across as many lots as needed. Missing, expired, or quarantined stock
// — or a location chain where no single ancestor can fully cover qty — is a hard block. The reservation
// is atomic and idempotent per batch: a replay records no second movement and does not re-adjust
// balances, even when the original reservation spanned several lots (see ports.Repository.ReserveForBatch).
func (s *Service) ReserveForBatch(ctx context.Context, tenantID, batchID, locationID, itemID string, qty int64) error {
	if qty <= 0 {
		return nil
	}
	err := s.repo.ReserveForBatch(ctx, tenantID, batchID, locationID, itemID, qty)
	switch {
	case errors.Is(err, ports.ErrNotFound):
		return fmt.Errorf("%w: no active stock for item %s at or above location %s", ErrStockUnavailable, itemID, locationID)
	case errors.Is(err, ports.ErrInsufficientStock):
		return fmt.Errorf("%w: required %d doses of item %s, but no location at or above %s holds enough", ErrInsufficientStock, qty, itemID, locationID)
	}
	return err
}

// parseQty parses a decimal quantity string to a floored int64 (doses are whole units).
func parseQty(s string) int64 {
	if s == "" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return int64(math.Floor(f))
}
