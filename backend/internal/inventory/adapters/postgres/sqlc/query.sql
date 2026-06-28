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

-- name: ResolveStockLocation :one
-- Resolve the location that actually HOLDS available stock for an item, starting at @location_id and
-- walking UP the location hierarchy (shed -> park -> farm) via parent_location_id. Vaccine stock is
-- held at park/farm while drives are shed-scoped, so a shed-scoped reservation must roll up to the
-- nearest ancestor (including itself) that has active, unexpired, unreserved stock. Returns the
-- nearest such location; no row when no ancestor holds stock (caller treats as stock-unavailable).
WITH RECURSIVE chain AS (
  SELECT b.location_id, b.parent_location_id, 0 AS depth
  FROM locations b
  WHERE b.tenant_id = @tenant_id AND b.location_id = @location_id
  UNION ALL
  SELECT l.location_id, l.parent_location_id, c.depth + 1
  FROM locations l
  JOIN chain c ON l.location_id = c.parent_location_id
  WHERE l.tenant_id = @tenant_id AND c.depth < 8
)
SELECT c.location_id::text AS location_id
FROM chain c
JOIN inventory_stock s
  ON s.tenant_id = @tenant_id
 AND s.location_id = c.location_id
 AND s.item_id = @item_id
 AND s.quantity_in_stock > s.quantity_reserved
 AND s.status = 'active'
 AND (s.expiry_date IS NULL OR s.expiry_date >= CURRENT_DATE)
ORDER BY c.depth ASC
LIMIT 1;

-- name: ChainLocationAvailableSums :many
-- Per-location TOTAL available (in_stock - reserved) active, unexpired stock for the location chain
-- starting at @location_id and walking UP parent_location_id (self at depth 0), ordered nearest-first.
-- Lets the reserve path pick the NEAREST ancestor whose lots TOGETHER cover the requested qty: a single
-- FEFO lot can be short even when the location holds enough across several lots, and a near ancestor can
-- be short even when a farther one is flush. Bounded chain depth (< 8). Locations with no available lot
-- are dropped (inner JOIN), so an empty result means no ancestor holds any stock at all.
WITH RECURSIVE chain AS (
  SELECT b.location_id, b.parent_location_id, 0 AS depth
  FROM locations b
  WHERE b.tenant_id = @tenant_id AND b.location_id = @location_id
  UNION ALL
  SELECT l.location_id, l.parent_location_id, c.depth + 1
  FROM locations l
  JOIN chain c ON l.location_id = c.parent_location_id
  WHERE l.tenant_id = @tenant_id AND c.depth < 8
)
SELECT c.location_id::text AS location_id,
       c.depth AS depth,
       SUM(s.quantity_in_stock - s.quantity_reserved)::numeric AS available_quantity
FROM chain c
JOIN inventory_stock s
  ON s.tenant_id = @tenant_id
 AND s.location_id = c.location_id
 AND s.item_id = @item_id
 AND s.quantity_in_stock > s.quantity_reserved
 AND s.status = 'active'
 AND (s.expiry_date IS NULL OR s.expiry_date >= CURRENT_DATE)
GROUP BY c.location_id, c.depth
ORDER BY c.depth ASC;

-- name: ListFEFOLotsForUpdate :many
-- Active, unexpired lots with available stock at @location_id for @item_id, earliest-expiry first
-- (FEFO), LOCKED FOR UPDATE so concurrent reservers at the same location serialize — the second waits,
-- then re-reads each row's CURRENT reserved level under the lock (READ COMMITTED EvalPlanQual) so it
-- cannot over-reserve past the quantity_reserved <= quantity_in_stock guard. The caller recomputes
-- available from the locked rows and consumes greedily in FEFO order.
SELECT stock_id::text AS stock_id,
       (quantity_in_stock - quantity_reserved)::numeric AS available_quantity,
       quantity_unit
FROM inventory_stock
WHERE tenant_id = @tenant_id
  AND location_id = @location_id
  AND item_id = @item_id
  AND status = 'active'
  AND quantity_in_stock > quantity_reserved
  AND (expiry_date IS NULL OR expiry_date >= CURRENT_DATE)
ORDER BY expiry_date ASC NULLS LAST, stock_id ASC
FOR UPDATE;

-- name: AcquireBatchReserveLock :exec
-- Serialize all reservation attempts for one batch within their transactions so the reserve-movement
-- count guard is authoritative: a concurrent retry/replay blocks here until the first reservation's
-- transaction ends, then observes the prior movements and no-ops instead of double-reserving across a
-- different lot/ancestor. The advisory lock auto-releases at transaction end.
SELECT pg_advisory_xact_lock(hashtext(@batch_id::text));

-- name: CountBatchReserveMovements :one
-- How many reserve movements already exist for this batch — the idempotency guard for the multi-lot
-- reserve (a single per-lot movement key cannot cover a reservation that spans lots/ancestors).
SELECT count(*)::bigint AS reserve_count
FROM inventory_stock_movements
WHERE tenant_id = @tenant_id AND batch_id = @batch_id AND movement_type = 'reserve';
