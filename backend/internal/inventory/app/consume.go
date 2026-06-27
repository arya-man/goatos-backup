package app

import (
	"context"
	"strconv"

	"github.com/vgoats/goatos/backend/internal/inventory/domain"
)

// ConsumeForBatch records actual consumption of qty units for a batch from a lot: it appends a
// 'consume' movement and moves qty out of both on-hand and reserved (in_stock -= qty, reserved -=
// qty). The lot's item/location/unit are read from the lot itself, so the movement always matches
// the (lot,item,location) tuple. qty is capped at the lot's current reserved so the reserved>=0 /
// reserved<=in_stock CHECKs hold. Idempotent per (batch, key) via movement idempotency_key key; a
// replay records nothing and does not re-adjust balances. key disambiguates per-goat consumption
// (e.g. batch:consume:<goat>).
func (s *Service) ConsumeForBatch(ctx context.Context, tenantID, batchID, lotID, key string, qty int64) error {
	return s.settle(ctx, tenantID, batchID, lotID, key, "consume", qty, true)
}

// ReleaseForBatch releases qty unused reserved units for a batch back to available (reserved -= qty,
// on-hand unchanged) via a 'release' movement. Used at batch close for no-show remainder. qty is
// capped at current reserved. Idempotent per (batch, key).
func (s *Service) ReleaseForBatch(ctx context.Context, tenantID, batchID, lotID, key string, qty int64) error {
	return s.settle(ctx, tenantID, batchID, lotID, key, "release", qty, false)
}

// settle is the shared consume/release path: read the lot (for item/location/unit + current
// reserved), cap qty at reserved, append the movement, then adjust balances. consumeOnHand controls
// whether on-hand drops with reserved (consume) or stays (release). Best-effort: zero/over-reserved
// qty caps to a no-op.
func (s *Service) settle(ctx context.Context, tenantID, batchID, lotID, key, movementType string, qty int64, consumeOnHand bool) error {
	if qty <= 0 {
		return nil
	}
	lot, err := s.repo.GetStockLot(ctx, tenantID, lotID)
	if err != nil {
		return err
	}
	reserved := parseQty(lot.QuantityReserved)
	if reserved < qty {
		qty = reserved // never drive reserved below zero
	}
	if qty <= 0 {
		return nil
	}
	qstr := strconv.FormatInt(qty, 10)
	batch := batchID
	inDelta := "0"
	if consumeOnHand {
		inDelta = "-" + qstr
	}
	_, _, err = s.repo.RecordMovementAndAdjustBalances(ctx, domain.Movement{
		TenantID:       tenantID,
		LotID:          lotID,
		ItemID:         lot.ItemID,
		LocationID:     lot.LocationID,
		MovementType:   movementType,
		Quantity:       qstr,
		QuantityUnit:   lot.QuantityUnit,
		BatchID:        &batch,
		Reason:         "batch " + movementType,
		IdempotencyKey: key,
	}, inDelta, "-"+qstr)
	return err
}
