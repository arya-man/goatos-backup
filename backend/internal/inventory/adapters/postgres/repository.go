// Package postgres implements the inventory Repository over generated sqlc queries.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	inventorydb "github.com/vgoats/goatos/backend/internal/inventory/adapters/postgres/sqlc"
	"github.com/vgoats/goatos/backend/internal/inventory/domain"
	"github.com/vgoats/goatos/backend/internal/inventory/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgconv"
)

const defaultQueryTimeout = 3 * time.Second

// Repository is the Postgres-backed inventory repository.
type Repository struct {
	pool         *pgxpool.Pool
	queries      *inventorydb.Queries
	queryTimeout time.Duration
}

// NewRepository builds a Repository bound to a pgx pool.
func NewRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *Repository {
	if queryTimeout <= 0 {
		queryTimeout = defaultQueryTimeout
	}
	return &Repository{pool: pool, queries: inventorydb.New(pool), queryTimeout: queryTimeout}
}

var _ ports.Repository = (*Repository)(nil)

func (r *Repository) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, r.queryTimeout)
}

// Ping checks pool connectivity.
func (r *Repository) Ping(ctx context.Context) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	return r.pool.Ping(ctx)
}

// CreateItem inserts an inventory item and returns its id.
func (r *Repository) CreateItem(ctx context.Context, in domain.NewItem) (string, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(in.TenantID)
	if err != nil {
		return "", fmt.Errorf("inventory: tenant id: %w", err)
	}
	id, err := r.queries.CreateInventoryItem(ctx, inventorydb.CreateInventoryItemParams{
		TenantID: tenant,
		ItemCode: in.ItemCode,
		Name:     in.Name,
		Category: in.Category,
		BaseUnit: in.BaseUnit,
		Status:   in.Status,
	})
	if err != nil {
		return "", fmt.Errorf("inventory: create item: %w", err)
	}
	return id, nil
}

// GetItem fetches an item by id within a tenant.
func (r *Repository) GetItem(ctx context.Context, tenantID, itemID string) (domain.Item, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return domain.Item{}, fmt.Errorf("inventory: tenant id: %w", err)
	}
	item, err := pgconv.UUID(itemID)
	if err != nil {
		return domain.Item{}, fmt.Errorf("inventory: item id: %w", err)
	}
	row, err := r.queries.GetInventoryItem(ctx, inventorydb.GetInventoryItemParams{TenantID: tenant, ItemID: item})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Item{}, ports.ErrNotFound
	}
	if err != nil {
		return domain.Item{}, fmt.Errorf("inventory: get item: %w", err)
	}
	return domain.Item{
		ItemID:   row.ItemID,
		TenantID: tenantID,
		ItemCode: row.ItemCode,
		Name:     row.Name,
		Category: row.Category,
		BaseUnit: row.BaseUnit,
		Status:   row.Status,
	}, nil
}

// CreateStockLot inserts a stock lot and returns its id.
func (r *Repository) CreateStockLot(ctx context.Context, in domain.NewStockLot) (string, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(in.TenantID)
	if err != nil {
		return "", fmt.Errorf("inventory: tenant id: %w", err)
	}
	item, err := pgconv.UUID(in.ItemID)
	if err != nil {
		return "", fmt.Errorf("inventory: item id: %w", err)
	}
	location, err := pgconv.UUID(in.LocationID)
	if err != nil {
		return "", fmt.Errorf("inventory: location id: %w", err)
	}
	inStock, err := pgconv.Numeric(in.QuantityInStock)
	if err != nil {
		return "", fmt.Errorf("inventory: quantity_in_stock: %w", err)
	}
	reserved, err := pgconv.Numeric(in.QuantityReserved)
	if err != nil {
		return "", fmt.Errorf("inventory: quantity_reserved: %w", err)
	}
	id, err := r.queries.CreateInventoryStockLot(ctx, inventorydb.CreateInventoryStockLotParams{
		TenantID:         tenant,
		ItemID:           item,
		LocationID:       location,
		LotCode:          pgconv.Text(in.LotCode),
		ExpiryDate:       pgconv.Date(in.ExpiryDate),
		QuantityInStock:  inStock,
		QuantityReserved: reserved,
		QuantityUnit:     in.QuantityUnit,
		Status:           in.Status,
	})
	if err != nil {
		return "", fmt.Errorf("inventory: create stock lot: %w", err)
	}
	return id, nil
}

// GetStockLot fetches a stock lot by id within a tenant.
func (r *Repository) GetStockLot(ctx context.Context, tenantID, stockID string) (domain.StockLot, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return domain.StockLot{}, fmt.Errorf("inventory: tenant id: %w", err)
	}
	stock, err := pgconv.UUID(stockID)
	if err != nil {
		return domain.StockLot{}, fmt.Errorf("inventory: stock id: %w", err)
	}
	row, err := r.queries.GetStockLot(ctx, inventorydb.GetStockLotParams{TenantID: tenant, StockID: stock})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.StockLot{}, ports.ErrNotFound
	}
	if err != nil {
		return domain.StockLot{}, fmt.Errorf("inventory: get stock lot: %w", err)
	}
	return domain.StockLot{
		StockID:          row.StockID,
		ItemID:           row.ItemID,
		LocationID:       row.LocationID,
		QuantityInStock:  pgconv.NumericString(row.QuantityInStock),
		QuantityReserved: pgconv.NumericString(row.QuantityReserved),
		QuantityUnit:     row.QuantityUnit,
		RowVersion:       row.RowVersion,
	}, nil
}

// ResolveStockLocation walks up the location hierarchy from locationID to the nearest ancestor
// (including itself) that holds available stock for the item. Returns ErrNotFound when none does.
func (r *Repository) ResolveStockLocation(ctx context.Context, tenantID, locationID, itemID string) (string, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return "", fmt.Errorf("inventory: tenant id: %w", err)
	}
	location, err := pgconv.UUID(locationID)
	if err != nil {
		return "", fmt.Errorf("inventory: location id: %w", err)
	}
	item, err := pgconv.UUID(itemID)
	if err != nil {
		return "", fmt.Errorf("inventory: item id: %w", err)
	}
	resolved, err := r.queries.ResolveStockLocation(ctx, inventorydb.ResolveStockLocationParams{TenantID: tenant, LocationID: location, ItemID: item})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ports.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("inventory: resolve stock location: %w", err)
	}
	return resolved, nil
}

// PickFEFOLot returns the earliest-expiring lot with available stock.
func (r *Repository) PickFEFOLot(ctx context.Context, tenantID, locationID, itemID string) (domain.FEFOPick, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return domain.FEFOPick{}, fmt.Errorf("inventory: tenant id: %w", err)
	}
	location, err := pgconv.UUID(locationID)
	if err != nil {
		return domain.FEFOPick{}, fmt.Errorf("inventory: location id: %w", err)
	}
	item, err := pgconv.UUID(itemID)
	if err != nil {
		return domain.FEFOPick{}, fmt.Errorf("inventory: item id: %w", err)
	}
	row, err := r.queries.PickFEFOLot(ctx, inventorydb.PickFEFOLotParams{TenantID: tenant, LocationID: location, ItemID: item})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.FEFOPick{}, ports.ErrNotFound
	}
	if err != nil {
		return domain.FEFOPick{}, fmt.Errorf("inventory: pick fefo lot: %w", err)
	}
	return domain.FEFOPick{
		StockID:           row.StockID,
		LotCode:           pgconv.TextValue(row.LotCode),
		ExpiryDate:        pgconv.DateValue(row.ExpiryDate),
		QuantityInStock:   pgconv.NumericString(row.QuantityInStock),
		QuantityReserved:  pgconv.NumericString(row.QuantityReserved),
		AvailableQuantity: pgconv.NumericString(row.AvailableQuantity),
		QuantityUnit:      row.QuantityUnit,
	}, nil
}

// RecordMovement appends an idempotent ledger movement.
func (r *Repository) RecordMovement(ctx context.Context, m domain.Movement) (string, bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(m.TenantID)
	if err != nil {
		return "", false, fmt.Errorf("inventory: tenant id: %w", err)
	}
	lot, err := pgconv.UUID(m.LotID)
	if err != nil {
		return "", false, fmt.Errorf("inventory: lot id: %w", err)
	}
	item, err := pgconv.UUID(m.ItemID)
	if err != nil {
		return "", false, fmt.Errorf("inventory: item id: %w", err)
	}
	location, err := pgconv.UUID(m.LocationID)
	if err != nil {
		return "", false, fmt.Errorf("inventory: location id: %w", err)
	}
	qty, err := pgconv.Numeric(m.Quantity)
	if err != nil {
		return "", false, fmt.Errorf("inventory: quantity: %w", err)
	}
	id, err := r.queries.InsertStockMovement(ctx, inventorydb.InsertStockMovementParams{
		TenantID:       tenant,
		LotID:          lot,
		ItemID:         item,
		LocationID:     location,
		MovementType:   m.MovementType,
		Quantity:       qty,
		QuantityUnit:   m.QuantityUnit,
		BatchID:        pgconv.NullableUUID(m.BatchID),
		ActorID:        pgconv.NullableUUID(m.ActorID),
		Reason:         pgconv.Text(m.Reason),
		IdempotencyKey: m.IdempotencyKey,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// ON CONFLICT DO NOTHING: movement already recorded for this idempotency key.
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("inventory: record movement: %w", err)
	}
	return id, true, nil
}

// RecordMovementAndAdjustBalances records a movement and applies its balance impact atomically.
func (r *Repository) RecordMovementAndAdjustBalances(ctx context.Context, m domain.Movement, inDelta, reservedDelta string) (string, bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, lot, item, location, qty, err := movementParams(m)
	if err != nil {
		return "", false, err
	}
	in, err := pgconv.Numeric(inDelta)
	if err != nil {
		return "", false, fmt.Errorf("inventory: in_delta: %w", err)
	}
	reserved, err := pgconv.Numeric(reservedDelta)
	if err != nil {
		return "", false, fmt.Errorf("inventory: reserved_delta: %w", err)
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", false, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	qtx := r.queries.WithTx(tx)
	id, err := qtx.InsertStockMovement(ctx, inventorydb.InsertStockMovementParams{
		TenantID:       tenant,
		LotID:          lot,
		ItemID:         item,
		LocationID:     location,
		MovementType:   m.MovementType,
		Quantity:       qty,
		QuantityUnit:   m.QuantityUnit,
		BatchID:        pgconv.NullableUUID(m.BatchID),
		ActorID:        pgconv.NullableUUID(m.ActorID),
		Reason:         pgconv.Text(m.Reason),
		IdempotencyKey: m.IdempotencyKey,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("inventory: record movement: %w", err)
	}
	if err := qtx.AdjustStockBalances(ctx, inventorydb.AdjustStockBalancesParams{
		InDelta:       in,
		ReservedDelta: reserved,
		TenantID:      tenant,
		StockID:       lot,
	}); err != nil {
		return "", false, fmt.Errorf("inventory: adjust balances: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", false, err
	}
	committed = true
	return id, true, nil
}

func movementParams(m domain.Movement) (tenant, lot, item, location pgtype.UUID, qty pgtype.Numeric, err error) {
	tenant, err = pgconv.UUID(m.TenantID)
	if err != nil {
		err = fmt.Errorf("inventory: tenant id: %w", err)
		return
	}
	lot, err = pgconv.UUID(m.LotID)
	if err != nil {
		err = fmt.Errorf("inventory: lot id: %w", err)
		return
	}
	item, err = pgconv.UUID(m.ItemID)
	if err != nil {
		err = fmt.Errorf("inventory: item id: %w", err)
		return
	}
	location, err = pgconv.UUID(m.LocationID)
	if err != nil {
		err = fmt.Errorf("inventory: location id: %w", err)
		return
	}
	qty, err = pgconv.Numeric(m.Quantity)
	if err != nil {
		err = fmt.Errorf("inventory: quantity: %w", err)
		return
	}
	return
}

// AdjustBalances applies signed numeric deltas to a lot.
func (r *Repository) AdjustBalances(ctx context.Context, tenantID, stockID, inDelta, reservedDelta string) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return fmt.Errorf("inventory: tenant id: %w", err)
	}
	stock, err := pgconv.UUID(stockID)
	if err != nil {
		return fmt.Errorf("inventory: stock id: %w", err)
	}
	in, err := pgconv.Numeric(inDelta)
	if err != nil {
		return fmt.Errorf("inventory: in_delta: %w", err)
	}
	reserved, err := pgconv.Numeric(reservedDelta)
	if err != nil {
		return fmt.Errorf("inventory: reserved_delta: %w", err)
	}
	if err := r.queries.AdjustStockBalances(ctx, inventorydb.AdjustStockBalancesParams{
		InDelta:       in,
		ReservedDelta: reserved,
		TenantID:      tenant,
		StockID:       stock,
	}); err != nil {
		return fmt.Errorf("inventory: adjust balances: %w", err)
	}
	return nil
}
