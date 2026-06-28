// Package ports declares the inventory domain's repository boundary.
package ports

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/inventory/domain"
)

// ErrNotFound is returned when a requested inventory row does not exist.
var ErrNotFound = errors.New("inventory: not found")

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

	// RecordMovement appends a ledger movement. It is idempotent on (tenant_id, idempotency_key):
	// applied is false when the movement was already recorded (replay).
	RecordMovement(ctx context.Context, m domain.Movement) (movementID string, applied bool, err error)

	// RecordMovementAndAdjustBalances appends an idempotent ledger movement and applies stock
	// balance deltas in the same transaction. If the movement idempotency key already exists,
	// applied is false and balances are not adjusted again.
	RecordMovementAndAdjustBalances(ctx context.Context, m domain.Movement, inDelta, reservedDelta string) (movementID string, applied bool, err error)

	// AdjustBalances applies signed deltas to a lot's on-hand and reserved quantities.
	AdjustBalances(ctx context.Context, tenantID, stockID, inDelta, reservedDelta string) error
}
