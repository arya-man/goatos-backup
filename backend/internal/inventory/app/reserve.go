package app

import (
	"context"
	"errors"
	"math"
	"strconv"

	"github.com/vgoats/goatos/backend/internal/inventory/domain"
	"github.com/vgoats/goatos/backend/internal/inventory/ports"
)

// ReserveForBatch reserves up to qty doses for a batch from the FEFO lot at the location. Best-
// effort: no stock → no-op; reserves min(qty, available) from the earliest-expiring lot. Idempotent
// per batch via movement idempotency_key=batch:reserve (a replay records no second movement and does
// not re-adjust balances). reserved is bounded by the inventory_stock CHECK(reserved<=in_stock).
func (s *Service) ReserveForBatch(ctx context.Context, tenantID, batchID, locationID, itemID string, qty int64) error {
	if qty <= 0 {
		return nil
	}
	pick, err := s.repo.PickFEFOLot(ctx, tenantID, locationID, itemID)
	if errors.Is(err, ports.ErrNotFound) {
		return nil // no available lot; reserve is best-effort
	}
	if err != nil {
		return err
	}
	available := parseQty(pick.AvailableQuantity)
	reserveQty := qty
	if available < reserveQty {
		reserveQty = available
	}
	if reserveQty <= 0 {
		return nil
	}
	qstr := strconv.FormatInt(reserveQty, 10)
	_, applied, err := s.repo.RecordMovement(ctx, domain.Movement{
		TenantID:       tenantID,
		LotID:          pick.StockID,
		ItemID:         itemID,
		LocationID:     locationID,
		MovementType:   "reserve",
		Quantity:       qstr,
		QuantityUnit:   pick.QuantityUnit,
		BatchID:        &batchID,
		Reason:         "batch reserve",
		IdempotencyKey: batchID + ":reserve",
	})
	if err != nil {
		return err
	}
	if !applied {
		return nil // already reserved for this batch
	}
	return s.repo.AdjustBalances(ctx, tenantID, pick.StockID, "0", qstr)
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
