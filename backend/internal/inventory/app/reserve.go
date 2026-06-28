package app

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/vgoats/goatos/backend/internal/inventory/domain"
	"github.com/vgoats/goatos/backend/internal/inventory/ports"
)

var (
	ErrStockUnavailable  = errors.New("inventory: stock unavailable")
	ErrInsufficientStock = errors.New("inventory: insufficient stock")
)

// ReserveForBatch reserves exactly qty doses for a batch from the FEFO lot at the location. Missing,
// expired, quarantined, or insufficient stock is a hard block. Idempotent per batch via movement
// idempotency_key=batch:reserve (a replay records no second movement and does not re-adjust
// balances). reserved is bounded by the inventory_stock CHECK(reserved<=in_stock).
func (s *Service) ReserveForBatch(ctx context.Context, tenantID, batchID, locationID, itemID string, qty int64) error {
	if qty <= 0 {
		return nil
	}
	// Roll the requested (drive) location up to the nearest ancestor that holds stock: shed-scoped
	// drives reserve against park/farm-held vaccine stock. ErrNotFound = no location in the chain has
	// an active lot -> a hard stock-unavailable block. The reservation movement is recorded at the
	// resolved stock location (where the lot actually lives), not the drive's shed.
	stockLocation, err := s.repo.ResolveStockLocation(ctx, tenantID, locationID, itemID)
	if errors.Is(err, ports.ErrNotFound) {
		return fmt.Errorf("%w: no active stock for item %s at or above location %s", ErrStockUnavailable, itemID, locationID)
	}
	if err != nil {
		return err
	}
	pick, err := s.repo.PickFEFOLot(ctx, tenantID, stockLocation, itemID)
	if errors.Is(err, ports.ErrNotFound) {
		return fmt.Errorf("%w: no active FEFO lot for item %s at location %s", ErrStockUnavailable, itemID, stockLocation)
	}
	if err != nil {
		return err
	}
	available := parseQty(pick.AvailableQuantity)
	reserveQty := qty
	if available < reserveQty {
		return fmt.Errorf("%w: required %d, available %d for item %s at location %s", ErrInsufficientStock, qty, available, itemID, stockLocation)
	}
	if reserveQty <= 0 {
		return fmt.Errorf("%w: no available quantity for item %s at location %s", ErrStockUnavailable, itemID, stockLocation)
	}
	qstr := strconv.FormatInt(reserveQty, 10)
	_, _, err = s.repo.RecordMovementAndAdjustBalances(ctx, domain.Movement{
		TenantID:       tenantID,
		LotID:          pick.StockID,
		ItemID:         itemID,
		LocationID:     stockLocation,
		MovementType:   "reserve",
		Quantity:       qstr,
		QuantityUnit:   pick.QuantityUnit,
		BatchID:        &batchID,
		Reason:         "batch reserve",
		IdempotencyKey: batchID + ":reserve",
	}, "0", qstr)
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
