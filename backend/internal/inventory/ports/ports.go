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

	// RecordMovement appends a ledger movement. It is idempotent on (tenant_id, idempotency_key):
	// applied is false when the movement was already recorded (replay).
	RecordMovement(ctx context.Context, m domain.Movement) (movementID string, applied bool, err error)

	// AdjustBalances applies signed deltas to a lot's on-hand and reserved quantities.
	AdjustBalances(ctx context.Context, tenantID, stockID, inDelta, reservedDelta string) error
}
