-- +goose Up
-- seed-fixture-guard:ignore: additive provenance columns on an existing analytics ledger; no
-- vaccination/HRMS seed contract change.
--
-- FEED PURCHASE ENTRY (maintainer decision 2026-08-24, SUPERSEDING the READ-ONLY half of the
-- 2026-08-17 lock recorded in 000174).
--
-- 000174 froze feed_purchases as a one-time bootstrap of the legacy Feed DB sheet and said in its
-- own comment: "There is no authoring UI; purchase/vendor entry screens belong to the future
-- Procurement vertical." That vertical now exists (/procurement/vendors, /procurement/sales), so
-- the maintainer retired the read-only half: a feed load bought today is recorded in the app, on
-- /procurement/feed-purchases, with the SAME fields the sheet's Purchase row carries.
--
-- The other two locked decisions from 000174 stand UNCHANGED and are enforced by the write path:
--   - CURRENT-CATALOG FEEDS ONLY. An app-entered purchase must resolve to an ACTIVE
--     feed_item_catalog row; a feed the catalog does not carry is rejected with a message naming
--     the catalog, never invented into it. Same rule the importer applies by skipping.
--   - STOCK DEPLETES AT SHEET LOCK. An app-entered row sets depletes_from = purchase_date and
--     consumed_at_import_kg = 0: GoatOS has directed nothing against a load bought today, so the
--     whole quantity is available and the existing stock read needs no change at all.
--
-- Two additive columns carry provenance, because the stock cards now mix sheet history with
-- app-authored rows and a reader is owed the difference:
ALTER TABLE public.feed_purchases
  ADD COLUMN IF NOT EXISTS entry_source text NOT NULL DEFAULT 'sheet_import',
  ADD COLUMN IF NOT EXISTS recorded_by  uuid;

-- Existing rows keep the default: every row present before this migration arrived through
-- cmd/import-feed-purchases. The importer keeps writing 'sheet_import'; the app writes 'app'.
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'feed_purchases_entry_source_check'
  ) THEN
    ALTER TABLE public.feed_purchases
      ADD CONSTRAINT feed_purchases_entry_source_check
      CHECK (entry_source = ANY (ARRAY['sheet_import'::text, 'app'::text]));
  END IF;
END
$$;

-- The entry form assigns the next batch number within (tenant, farm, feed) and the ledger page
-- lists newest-first. Both read the same three columns the natural key already indexes, so this
-- index exists for the ORDER BY, not the lookup.
CREATE INDEX IF NOT EXISTS feed_purchases_entry_idx
  ON public.feed_purchases (tenant_id, purchase_date DESC, feed_purchase_id);

COMMENT ON TABLE public.feed_purchases IS
  'One feed purchase (load/batch): sheet history bootstrapped by cmd/import-feed-purchases (entry_source=sheet_import) plus loads recorded in the app on /procurement/feed-purchases (entry_source=app). Stock = purchases minus directed kg of LOCKED feed sheets (depletion at sheet lock, batch-wise FIFO).';

COMMENT ON COLUMN public.feed_purchases.entry_source IS
  'How this row arrived: sheet_import (legacy Feed DB bootstrap) or app (recorded on /procurement/feed-purchases).';
COMMENT ON COLUMN public.feed_purchases.recorded_by IS
  'Workforce member who recorded an app-entered purchase. NULL for imported sheet history.';

-- +goose Down
DROP INDEX IF EXISTS feed_purchases_entry_idx;
ALTER TABLE public.feed_purchases
  DROP CONSTRAINT IF EXISTS feed_purchases_entry_source_check;
ALTER TABLE public.feed_purchases
  DROP COLUMN IF EXISTS recorded_by,
  DROP COLUMN IF EXISTS entry_source;
