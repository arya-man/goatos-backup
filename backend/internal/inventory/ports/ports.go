// Package ports declares the inventory domain's repository boundary.
package ports

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/inventory/domain"
)

// ErrNotFound is returned when a requested inventory row does not exist.
var ErrNotFound = errors.New("inventory: not found")

// ErrInsufficientStock is returned by ReserveForBatch when stock for the item exists somewhere in the
// location chain but no single location at or above the requested location holds enough to fully cover
// the requested quantity. Distinct from ErrNotFound, which means no stock exists in the chain at all.
var ErrInsufficientStock = errors.New("inventory: insufficient stock")

// Repository is the persistence boundary for inventory. Implementations wrap generated
// sqlc queries; no hand-written SQL leaks above this interface.
type Repository interface {
	Ping(ctx context.Context) error

	CreateItem(ctx context.Context, in domain.NewItem) (itemID string, err error)
	GetItem(ctx context.Context, tenantID, itemID string) (domain.Item, error)

	CreateStockLot(ctx context.Context, in domain.NewStockLot) (stockID string, err error)
	GetStockLot(ctx context.Context, tenantID, stockID string) (domain.StockLot, error)

	// PickFEFOLot returns the earliest-expiring lot with available (unreserved) stock for the
	// (location, item). Returns ErrNotFound when no lot has available stock.
	PickFEFOLot(ctx context.Context, tenantID, locationID, itemID string) (domain.FEFOPick, error)

	// ResolveStockLocation returns the nearest location (starting at locationID and walking UP the
	// parent_location_id chain) that holds available stock for the item — so a shed-scoped drive
	// reserves against park/farm-held vaccine stock. Returns ErrNotFound when no ancestor holds stock.
	ResolveStockLocation(ctx context.Context, tenantID, locationID, itemID string) (string, error)

	// ReserveForBatch atomically reserves exactly qty for batchID, walking up from locationID to the
	// NEAREST ancestor location whose active, unexpired lots TOGETHER cover qty, then consuming those
	// lots earliest-expiry first (FEFO) — spanning multiple lots within that location as needed. The
	// whole reservation is one transaction. It is idempotent per batch (advisory-locked + guarded by the
	// existing reserve-movement count), so a retry/replay reserves nothing more even when the original
	// reservation spanned several lots. Returns ErrNotFound when no ancestor holds any stock for the
	// item, and ErrInsufficientStock when stock exists but no single ancestor can fully cover qty.
	ReserveForBatch(ctx context.Context, tenantID, batchID, locationID, itemID string, qty int64) error

	// ReleaseBatchReconcileRemainders releases bounded, explicitly-recorded excess reserved doses for
	// batches marked stock_reconcile_required after goat defer/shift/cancel changed planned membership.
	ReleaseBatchReconcileRemainders(ctx context.Context, tenantID string, limit int) (domain.BatchStockReconcileSummary, error)

	// RecordMovement appends a ledger movement. It is idempotent on (tenant_id, idempotency_key):
	// applied is false when the movement was already recorded (replay).
	RecordMovement(ctx context.Context, m domain.Movement) (movementID string, applied bool, err error)

	// MovementExists reports whether an idempotency key has already recorded an inventory movement.
	MovementExists(ctx context.Context, tenantID, idempotencyKey string) (bool, error)

	// RecordMovementAndAdjustBalances appends an idempotent ledger movement and applies stock
	// balance deltas in the same transaction. If the movement idempotency key already exists,
	// applied is false and balances are not adjusted again.
	RecordMovementAndAdjustBalances(ctx context.Context, m domain.Movement, inDelta, reservedDelta string) (movementID string, applied bool, err error)

	// AdjustBalances applies signed deltas to a lot's on-hand and reserved quantities.
	AdjustBalances(ctx context.Context, tenantID, stockID, inDelta, reservedDelta string) error
}
