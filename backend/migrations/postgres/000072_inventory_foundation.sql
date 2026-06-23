-- +goose Up
-- Phase 0 · Generic inventory foundation (FEFO, append-only ledger). Schema only.
-- inventory_stock rows are lots (item + location + lot_code + expiry); movements is the
-- authoritative append-only ledger. Tenant safety + lot consistency are enforced by
-- composite FKs (mirrors the 000002 hardening pattern); balances carry non-negative CHECKs.

CREATE TABLE inventory_items (
  item_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  item_code text NOT NULL,
  name text NOT NULL,
  category text NOT NULL,
  base_unit text NOT NULL,
  status text NOT NULL DEFAULT 'active',
  context jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  row_version int NOT NULL DEFAULT 1,
  CONSTRAINT inventory_items_category_check CHECK (category IN ('vaccine', 'dewormer', 'medicine', 'feed', 'supplement', 'consumable', 'other')),
  CONSTRAINT inventory_items_status_check CHECK (status IN ('active', 'inactive', 'retired')),
  CONSTRAINT inventory_items_row_version_check CHECK (row_version >= 1),
  CONSTRAINT inventory_items_code_unique UNIQUE (tenant_id, item_code),
  CONSTRAINT inventory_items_tenant_id_unique UNIQUE (tenant_id, item_id)
);

CREATE TABLE vaccines (
  vaccine_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  item_id uuid NOT NULL,
  disease text NULL,
  manufacturer text NULL,
  doses_per_vial int NULL,
  withdrawal_days int NULL,
  context jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT vaccines_doses_check CHECK (doses_per_vial IS NULL OR doses_per_vial > 0),
  CONSTRAINT vaccines_withdrawal_check CHECK (withdrawal_days IS NULL OR withdrawal_days >= 0),
  CONSTRAINT vaccines_item_unique UNIQUE (tenant_id, item_id),
  CONSTRAINT vaccines_item_tenant_fk FOREIGN KEY (tenant_id, item_id) REFERENCES inventory_items(tenant_id, item_id)
);

CREATE TABLE inventory_stock (
  stock_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  item_id uuid NOT NULL,
  location_id uuid NOT NULL,
  lot_code text NULL,
  expiry_date date NULL,
  quantity_in_stock numeric NOT NULL DEFAULT 0,
  quantity_reserved numeric NOT NULL DEFAULT 0,
  quantity_unit text NOT NULL,
  status text NOT NULL DEFAULT 'active',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  row_version int NOT NULL DEFAULT 1,
  CONSTRAINT inventory_stock_qty_check CHECK (quantity_in_stock >= 0),
  CONSTRAINT inventory_stock_reserved_check CHECK (quantity_reserved >= 0),
  CONSTRAINT inventory_stock_reserved_le_check CHECK (quantity_reserved <= quantity_in_stock),
  CONSTRAINT inventory_stock_status_check CHECK (status IN ('active', 'expired', 'quarantined', 'depleted')),
  CONSTRAINT inventory_stock_row_version_check CHECK (row_version >= 1),
  CONSTRAINT inventory_stock_item_tenant_fk FOREIGN KEY (tenant_id, item_id) REFERENCES inventory_items(tenant_id, item_id),
  CONSTRAINT inventory_stock_location_tenant_fk FOREIGN KEY (tenant_id, location_id) REFERENCES locations(tenant_id, location_id),
  -- Referenceable by lot alone, and by the (lot,item,location) tuple so a movement cannot
  -- claim a lot belongs to a different item/location.
  CONSTRAINT inventory_stock_tenant_id_unique UNIQUE (tenant_id, stock_id),
  CONSTRAINT inventory_stock_lot_identity_unique UNIQUE (tenant_id, stock_id, item_id, location_id)
);

-- FEFO pick: earliest expiry first, only lots with stock on hand.
CREATE INDEX inventory_stock_fefo_idx
  ON inventory_stock(tenant_id, location_id, item_id, expiry_date)
  WHERE quantity_in_stock > 0;

CREATE TABLE inventory_stock_movements (
  movement_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  lot_id uuid NOT NULL,
  item_id uuid NOT NULL,
  location_id uuid NOT NULL,
  movement_type text NOT NULL,
  quantity numeric NOT NULL,
  quantity_unit text NOT NULL,
  batch_id uuid NULL, -- FK to obligation_batches added in 000074 (created there)
  occurred_at timestamptz NOT NULL DEFAULT now(),
  recorded_at timestamptz NOT NULL DEFAULT now(),
  actor_id uuid NULL,
  reason text NULL,
  idempotency_key text NOT NULL,
  context jsonb NOT NULL DEFAULT '{}'::jsonb,
  CONSTRAINT inventory_stock_movements_type_check CHECK (movement_type IN ('receive', 'reserve', 'consume', 'release', 'adjust', 'expire', 'transfer_out', 'transfer_in')),
  CONSTRAINT inventory_stock_movements_quantity_check CHECK (quantity > 0),
  CONSTRAINT inventory_stock_movements_idempotency_unique UNIQUE (tenant_id, idempotency_key),
  -- A movement's lot, item, location and tenant must all match the referenced stock lot.
  CONSTRAINT inventory_stock_movements_lot_tenant_fk FOREIGN KEY (tenant_id, lot_id, item_id, location_id)
    REFERENCES inventory_stock(tenant_id, stock_id, item_id, location_id)
);

CREATE INDEX inventory_stock_movements_lot_idx ON inventory_stock_movements(tenant_id, lot_id, occurred_at);

-- +goose Down
DROP INDEX IF EXISTS inventory_stock_movements_lot_idx;
DROP TABLE IF EXISTS inventory_stock_movements;
DROP INDEX IF EXISTS inventory_stock_fefo_idx;
DROP TABLE IF EXISTS inventory_stock;
DROP TABLE IF EXISTS vaccines;
DROP TABLE IF EXISTS inventory_items;
