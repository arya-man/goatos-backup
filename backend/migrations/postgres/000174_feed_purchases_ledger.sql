-- +goose Up
-- seed-fixture-guard:ignore: read-only analytics ledger bootstrapped from the legacy feed sheet; no vaccination/HRMS seed contract change
--
-- FEED PURCHASES LEDGER (maintainer decisions 2026-08-17).
--
-- One row per feed purchase (a "load"/batch) as the farm's legacy Feed DB sheet
-- records them: farm, feed, batch number, quantity, the landed-cost split, the
-- vendor and payment state. Three locked decisions shape this table:
--
--   1. ONE-TIME BOOTSTRAP, READ-ONLY FOR NOW. Rows arrive through
--      cmd/import-feed-purchases from the sheet's Purchase rows. There is no
--      authoring UI; purchase/vendor entry screens belong to the future
--      Procurement vertical. Nothing else writes here.
--   2. CURRENT-CATALOG FEEDS ONLY. The importer resolves each sheet feed label
--      against feed_item_catalog via feed_config_norm and SKIPS feeds the
--      catalog does not carry (historical items stay in the sheet).
--   3. STOCK DEPLETES AT SHEET LOCK. Stock/days-left reads subtract DIRECTED kg
--      of LOCKED feed_direction_issues (packed and staged by ~15:00 the day
--      before service) from these purchased quantities. This table stores
--      purchases only; depletion is computed at read time against the frozen
--      sheet, batch-wise FIFO by purchase_date.
--
-- park_id is nullable: the sheet keys purchases by farm label, and a purchase
-- can predate the park's registration. The importer resolves CBE/CPT to the
-- tenant's park locations and stores BOTH the resolved id and the raw label.
CREATE TABLE IF NOT EXISTS public.feed_purchases (
  feed_purchase_id  uuid DEFAULT gen_random_uuid() NOT NULL,
  tenant_id         uuid NOT NULL REFERENCES tenants (tenant_id),
  park_id           uuid,
  farm_label        text NOT NULL,
  feed_item_label   text NOT NULL,
  feed_item_key     text GENERATED ALWAYS AS (feed_config_norm(feed_item_label)) STORED,
  batch_no          integer NOT NULL,
  purchase_date     date NOT NULL,
  quantity_kg       numeric(12, 3) NOT NULL,
  -- Landed-cost split as the sheet keeps it; total_cost is the authoritative
  -- figure (the sheet's "Cost"), the split columns are informational detail.
  feed_cost         numeric(14, 2),
  transport_cost    numeric(14, 2),
  loading_cost      numeric(14, 2),
  unloading_cost    numeric(14, 2),
  total_cost        numeric(14, 2),
  per_kg_cost       numeric(12, 4),
  -- Consumption the SHEET had already recorded against this batch at import
  -- time. GoatOS directed kg only covers feed days after the bootstrap, so the
  -- balance read is: quantity - consumed_at_import - directed(locked sheets,
  -- feed_day >= depletes_from). Without this snapshot every old batch would
  -- read as still full.
  consumed_at_import_kg numeric(12, 3) NOT NULL DEFAULT 0,
  -- First feed day GoatOS-directed kg depletes this ledger (the bootstrap
  -- cutoff, supplied by the importer).
  depletes_from     date NOT NULL,
  vendor            text NOT NULL DEFAULT '',
  payment_released  numeric(14, 2),
  payment_status    text NOT NULL DEFAULT '',
  -- Provenance: which sheet row produced this record.
  source_ref        text NOT NULL DEFAULT '',
  imported_at       timestamptz NOT NULL DEFAULT now(),
  created_at        timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT feed_purchases_pkey PRIMARY KEY (feed_purchase_id),
  CONSTRAINT feed_purchases_quantity_check CHECK (quantity_kg > 0),
  CONSTRAINT feed_purchases_consumed_check CHECK (consumed_at_import_kg >= 0),
  CONSTRAINT feed_purchases_farm_check CHECK (btrim(farm_label) <> '')
);

-- Idempotent bootstrap: the sheet identifies a load by (farm, feed, batch no).
-- Re-running the importer upserts rather than duplicating.
CREATE UNIQUE INDEX IF NOT EXISTS feed_purchases_natural_uq
  ON public.feed_purchases (tenant_id, farm_label, feed_item_key, batch_no);

-- Serving read: stock/expenditure rollups scan a tenant's purchases per feed
-- item in date order (FIFO), optionally narrowed by park.
CREATE INDEX IF NOT EXISTS feed_purchases_serve_idx
  ON public.feed_purchases (tenant_id, feed_item_key, purchase_date);

COMMENT ON TABLE public.feed_purchases IS
  'One feed purchase (load/batch) bootstrapped from the legacy Feed DB sheet. Read-only until the Procurement vertical builds purchase entry. Stock = purchases minus directed kg of LOCKED feed sheets (depletion at sheet lock, batch-wise FIFO).';

-- +goose Down
DROP TABLE IF EXISTS public.feed_purchases;
