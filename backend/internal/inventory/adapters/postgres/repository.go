// Package postgres implements the inventory Repository over generated sqlc queries.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
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
	if err := checkStockMovementFingerprint(ctx, r.pool, m, tenant, lot, item, location, qty); err != nil {
		return "", false, err
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
		if ferr := checkStockMovementFingerprint(ctx, r.pool, m, tenant, lot, item, location, qty); ferr != nil {
			return "", false, ferr
		}
		// ON CONFLICT DO NOTHING: movement already recorded for this idempotency key.
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("inventory: record movement: %w", err)
	}
	return id, true, nil
}

// MovementExists reports whether an inventory movement already exists for an idempotency key.
func (r *Repository) MovementExists(ctx context.Context, tenantID, idempotencyKey string) (bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return false, fmt.Errorf("inventory: tenant id: %w", err)
	}
	count, err := r.queries.CountStockMovementByIdempotencyKey(ctx, inventorydb.CountStockMovementByIdempotencyKeyParams{
		TenantID:       tenant,
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		return false, fmt.Errorf("inventory: movement exists: %w", err)
	}
	return count > 0, nil
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
	if err := checkStockMovementFingerprint(ctx, tx, m, tenant, lot, item, location, qty); err != nil {
		return "", false, err
	}
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
		if ferr := checkStockMovementFingerprint(ctx, tx, m, tenant, lot, item, location, qty); ferr != nil {
			return "", false, ferr
		}
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

// ReserveForBatch atomically reserves qty for batchID against the nearest ancestor location whose
// active FEFO lots together cover qty, consuming lots earliest-expiry first across as many lots as
// needed. Walking up the location hierarchy AND spanning multiple lots is what stops a shed-scoped
// drive from falsely stock-blocking when the vaccine is held at the park/farm or split across lots.
// Idempotent per batch: the per-batch advisory lock serializes retries and the reserve-movement count
// guard makes a replay a no-op even when the original reservation spanned several lots.
func (r *Repository) ReserveForBatch(ctx context.Context, tenantID, batchID, locationID, itemID string, qty int64) error {
	if qty <= 0 {
		return nil
	}
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return fmt.Errorf("inventory: tenant id: %w", err)
	}
	batch, err := pgconv.UUID(batchID)
	if err != nil {
		return fmt.Errorf("inventory: batch id: %w", err)
	}
	startLoc, err := pgconv.UUID(locationID)
	if err != nil {
		return fmt.Errorf("inventory: location id: %w", err)
	}
	item, err := pgconv.UUID(itemID)
	if err != nil {
		return fmt.Errorf("inventory: item id: %w", err)
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	qtx := r.queries.WithTx(tx)

	// Serialize concurrent reservations for this batch so the count guard below is authoritative — a
	// retry that races the first reservation waits here, then observes its movements and no-ops instead
	// of double-reserving across a different lot/ancestor.
	if err := qtx.AcquireBatchReserveLock(ctx, batchID); err != nil {
		return fmt.Errorf("inventory: acquire batch reserve lock: %w", err)
	}
	already, err := qtx.CountBatchReserveMovements(ctx, inventorydb.CountBatchReserveMovementsParams{TenantID: tenant, BatchID: batch})
	if err != nil {
		return fmt.Errorf("inventory: count batch reserve movements: %w", err)
	}
	if already > 0 {
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		committed = true
		return nil
	}

	// Pick the NEAREST ancestor whose lots together cover qty (a near location short on stock is skipped
	// for a farther flush one). sums is ordered nearest-first; an empty result means no stock anywhere.
	sums, err := qtx.ChainLocationAvailableSums(ctx, inventorydb.ChainLocationAvailableSumsParams{TenantID: tenant, ItemID: item, LocationID: startLoc})
	if err != nil {
		return fmt.Errorf("inventory: chain location available sums: %w", err)
	}
	chosen := ""
	anyStock := false
	for _, row := range sums {
		if floorNumericToInt64(row.AvailableQuantity) > 0 {
			anyStock = true
		}
		if floorNumericToInt64(row.AvailableQuantity) >= qty {
			chosen = row.LocationID
			break
		}
	}
	if chosen == "" {
		if !anyStock {
			return ports.ErrNotFound
		}
		return ports.ErrInsufficientStock
	}

	chosenLoc, err := pgconv.UUID(chosen)
	if err != nil {
		return fmt.Errorf("inventory: resolved location id: %w", err)
	}
	lots, err := qtx.ListFEFOLotsForUpdate(ctx, inventorydb.ListFEFOLotsForUpdateParams{TenantID: tenant, LocationID: chosenLoc, ItemID: item})
	if err != nil {
		return fmt.Errorf("inventory: list fefo lots for update: %w", err)
	}
	zero, err := pgconv.Numeric("0")
	if err != nil {
		return fmt.Errorf("inventory: zero delta: %w", err)
	}
	remaining := qty
	for _, lot := range lots {
		if remaining <= 0 {
			break
		}
		take := floorNumericToInt64(lot.AvailableQuantity) // recomputed under the FOR UPDATE lock
		if take > remaining {
			take = remaining
		}
		if take <= 0 {
			continue
		}
		lotUUID, err := pgconv.UUID(lot.StockID)
		if err != nil {
			return fmt.Errorf("inventory: lot id: %w", err)
		}
		takeQty, err := pgconv.Numeric(strconv.FormatInt(take, 10))
		if err != nil {
			return fmt.Errorf("inventory: reserve quantity: %w", err)
		}
		// One reserve movement per consumed lot, each with its own idempotency key, so a reservation that
		// spans lots stays idempotent (a single batch-level key would collapse the second lot's insert).
		if _, err := qtx.InsertStockMovement(ctx, inventorydb.InsertStockMovementParams{
			TenantID:       tenant,
			LotID:          lotUUID,
			ItemID:         item,
			LocationID:     chosenLoc,
			MovementType:   "reserve",
			Quantity:       takeQty,
			QuantityUnit:   lot.QuantityUnit,
			BatchID:        pgconv.NullableUUID(&batchID),
			Reason:         pgconv.Text("batch reserve"),
			IdempotencyKey: batchID + ":reserve:" + lot.StockID,
		}); errors.Is(err, pgx.ErrNoRows) {
			// A reserve movement for this lot already exists though the batch counted none — under the
			// advisory lock that is an invariant violation; fail closed rather than risk silent miscount.
			return fmt.Errorf("inventory: unexpected duplicate reserve movement for batch %s lot %s", batchID, lot.StockID)
		} else if err != nil {
			return fmt.Errorf("inventory: record reserve movement: %w", err)
		}
		if err := qtx.AdjustStockBalances(ctx, inventorydb.AdjustStockBalancesParams{
			InDelta:       zero,
			ReservedDelta: takeQty,
			TenantID:      tenant,
			StockID:       lotUUID,
		}); err != nil {
			return fmt.Errorf("inventory: adjust reserve balances: %w", err)
		}
		remaining -= take
	}
	if remaining > 0 {
		// A concurrent reservation drained the chosen location between the (unlocked) resolve and the
		// (locked) consume, so it no longer covers qty. Roll back the partial inserts and fail closed.
		return ports.ErrInsufficientStock
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}

// ReleaseBatchReconcileRemainders releases excess reserved doses for batches whose membership
// changed after reservation. The exact excess quantity is recorded by the obligation layer when it
// marks a defer/shift/cancel repair; this worker releases only that amount and marks the repair done.
func (r *Repository) ReleaseBatchReconcileRemainders(ctx context.Context, tenantID string, limit int) (domain.BatchStockReconcileSummary, error) {
	var summary domain.BatchStockReconcileSummary
	if limit <= 0 {
		limit = 1000
	}
	if limit > 5000 {
		limit = 5000
	}
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return summary, fmt.Errorf("inventory: tenant id: %w", err)
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return summary, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	rows, err := tx.Query(ctx, `
SELECT batch_id::text,
       row_version::text AS repair_token,
       (
         CASE WHEN context #>> '{defer_repair,state}' = 'stock_reconcile_required'
              THEN COALESCE(NULLIF(context #>> '{defer_repair,release_qty}', '')::numeric, 0)
              ELSE 0 END
         + CASE WHEN context #>> '{shift_repair,state}' = 'stock_reconcile_required'
              THEN COALESCE(NULLIF(context #>> '{shift_repair,release_qty}', '')::numeric, 0)
              ELSE 0 END
         + CASE WHEN context #>> '{cancel_repair,state}' = 'stock_reconcile_required'
              THEN COALESCE(NULLIF(context #>> '{cancel_repair,release_qty}', '')::numeric, 0)
              ELSE 0 END
         + CASE WHEN context #>> '{missed_repair,state}' = 'stock_reconcile_required'
              THEN COALESCE(NULLIF(context #>> '{missed_repair,release_qty}', '')::numeric, 0)
              ELSE 0 END
       )::numeric AS release_qty
FROM obligation_batches
WHERE tenant_id = $1
  AND (
    context #>> '{defer_repair,state}' = 'stock_reconcile_required'
    OR context #>> '{shift_repair,state}' = 'stock_reconcile_required'
    OR context #>> '{cancel_repair,state}' = 'stock_reconcile_required'
    OR context #>> '{missed_repair,state}' = 'stock_reconcile_required'
  )
ORDER BY updated_at ASC, batch_id ASC
LIMIT $2
FOR UPDATE SKIP LOCKED`, tenant, limit)
	if err != nil {
		return summary, fmt.Errorf("inventory: list stock reconcile batches: %w", err)
	}
	type candidate struct {
		batchID     string
		repairToken string
		releaseQty  pgtype.Numeric
	}
	candidates := make([]candidate, 0)
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.batchID, &c.repairToken, &c.releaseQty); err != nil {
			rows.Close()
			return summary, fmt.Errorf("inventory: scan stock reconcile batch: %w", err)
		}
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return summary, fmt.Errorf("inventory: read stock reconcile batches: %w", err)
	}
	rows.Close()

	for _, c := range candidates {
		targetQty := floorNumericToInt64(c.releaseQty)
		released, movements, err := r.releaseBatchReconcileQty(ctx, tx, tenant, c.batchID, c.repairToken, targetQty)
		if err != nil {
			return summary, err
		}
		if _, err := tx.Exec(ctx, `
UPDATE obligation_batches
SET context = jsonb_set(
        jsonb_set(
          jsonb_set(
            jsonb_set(context, '{defer_repair,state}', to_jsonb('stock_reconciled'::text), false),
            '{shift_repair,state}', to_jsonb('stock_reconciled'::text), false
          ),
          '{cancel_repair,state}', to_jsonb('stock_reconciled'::text), false
        ),
        '{missed_repair,state}', to_jsonb('stock_reconciled'::text), false
      )
      || jsonb_build_object(
        'stock_reconcile',
        jsonb_build_object('state', 'stock_reconciled', 'released_qty', $3::numeric, 'reconciled_at', now()::text)
      ),
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1
  AND batch_id = $2::uuid`, tenant, c.batchID, released); err != nil {
			return summary, fmt.Errorf("inventory: mark batch stock reconciled: %w", err)
		}
		summary.Batches++
		summary.Movements += movements
		summary.Released += released
	}
	if err := tx.Commit(ctx); err != nil {
		return summary, err
	}
	committed = true
	return summary, nil
}

func (r *Repository) releaseBatchReconcileQty(ctx context.Context, tx pgx.Tx, tenant pgtype.UUID, batchID, repairToken string, targetQty int64) (int64, int, error) {
	if targetQty <= 0 {
		return 0, 0, nil
	}
	if repairToken == "" {
		repairToken = "unknown"
	}
	rows, err := tx.Query(ctx, `
SELECT ledger.lot_id::text,
       ledger.item_id::text,
       ledger.location_id::text,
       ledger.quantity_unit,
       ledger.remaining_qty,
       stock.quantity_reserved
FROM (
  SELECT lot_id,
         item_id,
         location_id,
         quantity_unit,
         SUM(CASE
           WHEN movement_type = 'reserve' THEN quantity
           WHEN movement_type IN ('consume', 'release') THEN -quantity
           ELSE 0
         END)::numeric AS remaining_qty
  FROM inventory_stock_movements
  WHERE tenant_id = $1
    AND batch_id = $2::uuid
    AND movement_type IN ('reserve', 'consume', 'release')
  GROUP BY lot_id, item_id, location_id, quantity_unit
  HAVING SUM(CASE
    WHEN movement_type = 'reserve' THEN quantity
    WHEN movement_type IN ('consume', 'release') THEN -quantity
    ELSE 0
  END) > 0
) ledger
JOIN inventory_stock stock
  ON stock.tenant_id = $1
 AND stock.stock_id = ledger.lot_id
 AND stock.item_id = ledger.item_id
 AND stock.location_id = ledger.location_id
ORDER BY stock.expiry_date DESC NULLS FIRST, ledger.lot_id
FOR UPDATE OF stock`, tenant, batchID)
	if err != nil {
		return 0, 0, fmt.Errorf("inventory: list batch stock remainders: %w", err)
	}

	type stockRemainder struct {
		lotID           string
		itemID          string
		locationID      string
		unit            string
		ledgerRemaining pgtype.Numeric
		stockReserved   pgtype.Numeric
	}
	remainders := make([]stockRemainder, 0)
	for rows.Next() {
		var row stockRemainder
		if err := rows.Scan(&row.lotID, &row.itemID, &row.locationID, &row.unit, &row.ledgerRemaining, &row.stockReserved); err != nil {
			rows.Close()
			return 0, 0, fmt.Errorf("inventory: scan batch stock remainder: %w", err)
		}
		remainders = append(remainders, row)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, 0, fmt.Errorf("inventory: read batch stock remainders: %w", err)
	}
	rows.Close()

	remainingTarget := targetQty
	var released int64
	var movements int
	for _, row := range remainders {
		if remainingTarget <= 0 {
			break
		}
		qty := floorNumericToInt64(row.ledgerRemaining)
		if reserved := floorNumericToInt64(row.stockReserved); reserved < qty {
			qty = reserved
		}
		if qty > remainingTarget {
			qty = remainingTarget
		}
		if qty <= 0 {
			continue
		}
		lot, err := pgconv.UUID(row.lotID)
		if err != nil {
			return released, movements, fmt.Errorf("inventory: release lot id: %w", err)
		}
		item, err := pgconv.UUID(row.itemID)
		if err != nil {
			return released, movements, fmt.Errorf("inventory: release item id: %w", err)
		}
		location, err := pgconv.UUID(row.locationID)
		if err != nil {
			return released, movements, fmt.Errorf("inventory: release location id: %w", err)
		}
		qtyNumeric, err := pgconv.Numeric(strconv.FormatInt(qty, 10))
		if err != nil {
			return released, movements, fmt.Errorf("inventory: release quantity: %w", err)
		}
		idempotencyKey := batchID + ":release-reconcile:" + repairToken + ":" + row.lotID
		movement := domain.Movement{
			TenantID:       pgconv.UUIDString(tenant),
			LotID:          row.lotID,
			ItemID:         row.itemID,
			LocationID:     row.locationID,
			MovementType:   "release",
			Quantity:       strconv.FormatInt(qty, 10),
			QuantityUnit:   row.unit,
			BatchID:        &batchID,
			Reason:         "batch stock reconcile",
			IdempotencyKey: idempotencyKey,
		}
		if err := checkStockMovementFingerprint(ctx, tx, movement, tenant, lot, item, location, qtyNumeric); err != nil {
			return released, movements, err
		}
		var movementID string
		err = tx.QueryRow(ctx, `
INSERT INTO inventory_stock_movements (
  tenant_id, lot_id, item_id, location_id, movement_type,
  quantity, quantity_unit, batch_id, reason, idempotency_key
) VALUES (
  $1, $2, $3, $4, 'release',
  $5, $6, $7::uuid, 'batch stock reconcile', $8
)
			ON CONFLICT (tenant_id, idempotency_key) DO NOTHING
	RETURNING movement_id::text`, tenant, lot, item, location, qtyNumeric, row.unit, batchID, idempotencyKey).Scan(&movementID)
		if errors.Is(err, pgx.ErrNoRows) {
			if ferr := checkStockMovementFingerprint(ctx, tx, movement, tenant, lot, item, location, qtyNumeric); ferr != nil {
				return released, movements, ferr
			}
			remainingTarget -= qty
			continue
		}
		if err != nil {
			return released, movements, fmt.Errorf("inventory: record batch release: %w", err)
		}
		if _, err := tx.Exec(ctx, `
UPDATE inventory_stock
SET quantity_reserved = quantity_reserved - $3,
    row_version = row_version + 1,
    updated_at = now()
WHERE tenant_id = $1
  AND stock_id = $2`, tenant, lot, qtyNumeric); err != nil {
			return released, movements, fmt.Errorf("inventory: adjust batch release balance: %w", err)
		}
		released += qty
		movements++
		remainingTarget -= qty
	}
	return released, movements, nil
}

// floorNumericToInt64 floors a Postgres numeric to a whole int64 (doses are whole units). Invalid or
// unparseable numerics floor to 0, which the reserve path treats as "no available stock here".
func floorNumericToInt64(n pgtype.Numeric) int64 {
	if !n.Valid {
		return 0
	}
	f, err := n.Float64Value()
	if err != nil || !f.Valid {
		return 0
	}
	return int64(math.Floor(f.Float64))
}

type stockMovementQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func checkStockMovementFingerprint(ctx context.Context, q stockMovementQuerier, m domain.Movement, tenant, lot, item, location pgtype.UUID, qty pgtype.Numeric) error {
	var conflict bool
	if err := q.QueryRow(ctx, `
	SELECT EXISTS (
	  SELECT 1
	  FROM inventory_stock_movements
	  WHERE tenant_id = $1
	    AND idempotency_key = $2
	    AND (
	      lot_id IS DISTINCT FROM $3 OR
	      item_id IS DISTINCT FROM $4 OR
	      location_id IS DISTINCT FROM $5 OR
	      movement_type IS DISTINCT FROM $6 OR
	      quantity IS DISTINCT FROM $7 OR
	      quantity_unit IS DISTINCT FROM $8 OR
	      batch_id IS DISTINCT FROM $9 OR
	      actor_id IS DISTINCT FROM $10 OR
	      COALESCE(reason, '') IS DISTINCT FROM $11
	    )
	)`,
		tenant,
		m.IdempotencyKey,
		lot,
		item,
		location,
		m.MovementType,
		qty,
		m.QuantityUnit,
		pgconv.NullableUUID(m.BatchID),
		pgconv.NullableUUID(m.ActorID),
		m.Reason,
	).Scan(&conflict); err != nil {
		return fmt.Errorf("inventory: check movement idempotency fingerprint: %w", err)
	}
	if conflict {
		return ports.ErrMovementIdempotencyConflict
	}
	return nil
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
