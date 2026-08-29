-- +goose Up
-- seed-fixture-guard:ignore: read-only analytics ledger bootstrapped from the legacy feed sheet; no vaccination/HRMS seed contract change
--
-- FEED EXTERNAL CONSUMPTION LEDGER (maintainer decision 2026-08-22).
--
-- One row per (farm, feed, day) of consumption for feeds the farm serves but
-- GoatOS does NOT direct through feed sheets -- today exactly UHT Milk, the
-- kids' milk feeding, which the legacy Feed DB sheet tracks as two daily
-- Consumption rows (CBE, CPT). The stock analytics read
-- (feeddirection/adapters/postgres/analytics.go stockItemsSQL) UNIONs these
-- rows with locked-sheet directed kg, so a sheet-tracked feed gets the same
-- balance / avg-daily / days-left treatment as a directed feed.
--
-- This supersedes, for UHT Milk specifically, the 2026-08-17 "current-catalog
-- feeds only" skip recorded in cmd/import-feed-purchases: the maintainer asked
-- on 2026-08-22 for UHT Milk to be treated as a feed with its own stock and
-- consumption chart. UHT Milk is added to feed_item_catalog by the importer
-- (cmd/import-feed-external-consumption -ensure-catalog-item), never invented
-- here -- a migration does not own tenant catalog data.
--
-- Rows arrive through cmd/import-feed-external-consumption from the sheet's
-- Consumption rows. There is no authoring UI; ongoing rows are appended by the
-- same operational routine that maintains the sheet. Nothing else writes here.
--
-- park_id is nullable for the same reason as feed_purchases: the sheet keys by
-- farm label, and the importer resolves CBE/CPT to park locations, storing
-- BOTH the resolved id and the raw label.
CREATE TABLE IF NOT EXISTS public.feed_external_consumption (
  feed_external_consumption_id uuid DEFAULT gen_random_uuid() NOT NULL,
  tenant_id        uuid NOT NULL REFERENCES tenants (tenant_id),
  park_id          uuid,
  farm_label       text NOT NULL,
  feed_item_label  text NOT NULL,
  feed_item_key    text GENERATED ALWAYS AS (feed_config_norm(feed_item_label)) STORED,
  feed_day         date NOT NULL,
  quantity_kg      numeric(12, 3) NOT NULL,
  -- The purchase batch the sheet recorded the day's consumption against;
  -- informational provenance, never a join key (depletion is ledger-total).
  batch_no         integer,
  -- Provenance: which sheet row produced this record.
  source_ref       text NOT NULL DEFAULT '',
  imported_at      timestamptz NOT NULL DEFAULT now(),
  created_at       timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT feed_external_consumption_pkey PRIMARY KEY (feed_external_consumption_id),
  CONSTRAINT feed_external_consumption_quantity_check CHECK (quantity_kg >= 0),
  CONSTRAINT feed_external_consumption_farm_check CHECK (btrim(farm_label) <> '')
);

-- Idempotent import at DAY grain: the sheet can carry TWO rows for one
-- (farm, feed, day) across a batch changeover, so the importer's CSV
-- pre-aggregates to one day total before the upsert. Re-running the importer
-- upserts rather than duplicating.
CREATE UNIQUE INDEX IF NOT EXISTS feed_external_consumption_natural_uq
  ON public.feed_external_consumption (tenant_id, farm_label, feed_item_key, feed_day);

-- Serving read: stock/expenditure rollups scan a tenant's rows per feed item
-- in day order, optionally narrowed by park.
CREATE INDEX IF NOT EXISTS feed_external_consumption_serve_idx
  ON public.feed_external_consumption (tenant_id, feed_item_key, feed_day);

COMMENT ON TABLE public.feed_external_consumption IS
  'Daily consumption of sheet-tracked feeds GoatOS does not direct (UHT Milk), bootstrapped and appended from the legacy Feed DB sheet. Stock analytics unions these rows with locked-sheet directed kg for balance/avg-daily/days-left.';

-- +goose Down
DROP TABLE IF EXISTS public.feed_external_consumption;
