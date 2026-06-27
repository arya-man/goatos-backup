-- name: GetInventoryItem :one
SELECT item_id::text AS item_id, item_code, name, category, base_unit, status, row_version
FROM inventory_items
WHERE tenant_id = @tenant_id AND item_id = @item_id;

-- name: GetStockLot :one
SELECT stock_id::text AS stock_id, item_id::text AS item_id, location_id::text AS location_id,
       quantity_in_stock, quantity_reserved, quantity_unit, row_version
FROM inventory_stock
WHERE tenant_id = @tenant_id AND stock_id = @stock_id;

-- name: PickFEFOLot :one
-- FEFO: earliest-expiring lot with AVAILABLE (unreserved) stock at a location for an item.
-- Keeps an explicit quantity_in_stock > 0 so the partial index inventory_stock_fefo_idx
-- (tenant_id, location_id, item_id, expiry_date) WHERE quantity_in_stock > 0 is used; the
-- quantity_in_stock > quantity_reserved guard skips fully-reserved lots; returns available qty.
-- Expired/quarantined/depleted lots are excluded so vaccination drives cannot silently proceed on
-- bad stock.
SELECT stock_id::text AS stock_id, lot_code, expiry_date,
       quantity_in_stock, quantity_reserved,
       (quantity_in_stock - quantity_reserved)::numeric AS available_quantity,
       quantity_unit
FROM inventory_stock
WHERE tenant_id = @tenant_id
  AND location_id = @location_id
  AND item_id = @item_id
  AND quantity_in_stock > 0
  AND quantity_in_stock > quantity_reserved
  AND status = 'active'
  AND (expiry_date IS NULL OR expiry_date >= CURRENT_DATE)
ORDER BY expiry_date ASC NULLS LAST, stock_id ASC
LIMIT 1;
