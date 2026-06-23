-- name: CreateInventoryItem :one
INSERT INTO inventory_items (tenant_id, item_code, name, category, base_unit, status)
VALUES (@tenant_id, @item_code, @name, @category, @base_unit, @status)
RETURNING item_id::text AS item_id;

-- name: CreateInventoryStockLot :one
INSERT INTO inventory_stock (
  tenant_id, item_id, location_id, lot_code, expiry_date,
  quantity_in_stock, quantity_reserved, quantity_unit, status
) VALUES (
  @tenant_id, @item_id, @location_id, @lot_code, @expiry_date,
  @quantity_in_stock, @quantity_reserved, @quantity_unit, @status
)
RETURNING stock_id::text AS stock_id;

-- name: InsertStockMovement :one
INSERT INTO inventory_stock_movements (
  tenant_id, lot_id, item_id, location_id, movement_type,
  quantity, quantity_unit, batch_id, actor_id, reason, idempotency_key
) VALUES (
  @tenant_id, @lot_id, @item_id, @location_id, @movement_type,
  @quantity, @quantity_unit, @batch_id, @actor_id, @reason, @idempotency_key
)
ON CONFLICT (tenant_id, idempotency_key) DO NOTHING
RETURNING movement_id::text AS movement_id;

-- name: AdjustStockBalances :exec
UPDATE inventory_stock
SET quantity_in_stock = quantity_in_stock + @in_delta,
    quantity_reserved = quantity_reserved + @reserved_delta,
    row_version = row_version + 1,
    updated_at = now()
WHERE tenant_id = @tenant_id AND stock_id = @stock_id;
