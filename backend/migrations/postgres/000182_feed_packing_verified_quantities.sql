-- +goose Up
-- THE VERIFIER RECORDS WHAT WAS PACKED, PER FEED ITEM (maintainer decision 2026-08-21).
--
-- Feed packing verification is no longer "watch the video against the printed expected ration".
-- The verifier is shown the pen-session's feed item NAMES ONLY (blind entry -- the planned
-- quantities are hidden so she cannot copy them) and types the packed weight she can read off the
-- video for each item; the approve carries those numbers (the 2026-08-20 "THE APPROVE CARRIES THE
-- NUMBER" rule, extended to one value per feed item). A verifier who cannot see a usable video
-- rejects, which sends the bag back for rework exactly as before.
--
-- This table is the producing module's record of those readings: one row per (completion, feed
-- item). It is feeddirection-owned -- verification hands the entries through the registered
-- measurement applier and never writes here itself. A rework re-submit gets a fresh approve whose
-- entries UPSERT over the previous reading, so the row always carries the verdict that stands.
--
-- The intended-vs-entered variance is NEVER shown to the verifier. It is computed at read time by
-- the leadership feed analytics execution view, joining these rows to the FROZEN issued sheet
-- (feed_direction_issue_rows) on the same normalized feed_item_key both sides carry.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '30s';

CREATE TABLE IF NOT EXISTS public.feed_packing_verified_quantities (
  tenant_id uuid NOT NULL,
  completion_id uuid NOT NULL,
  -- feed_item_key is the normalized config key (feed_config_norm / NormalizeConfigKey output), the
  -- SAME key feed_direction_issue_rows carries, so the variance join needs no label parsing.
  feed_item_key text NOT NULL,
  -- feed_item_label is the display label the entry box was captioned with, denormalized so the
  -- reading stays renderable even if the config vocabulary is re-labelled later.
  feed_item_label text NOT NULL,
  -- entered_kg is the verifier's reading. ZERO IS VALID -- "I can see this item was not packed"
  -- is a real observation, distinct from an absent row (never asked / not yet recorded).
  entered_kg numeric(10,3) NOT NULL,
  recorded_by uuid NOT NULL,
  recorded_at timestamptz DEFAULT now() NOT NULL,
  CONSTRAINT feed_packing_verified_quantities_pkey
    PRIMARY KEY (tenant_id, completion_id, feed_item_key),
  CONSTRAINT feed_packing_verified_quantities_completion_fk
    FOREIGN KEY (completion_id) REFERENCES public.feed_packing_completions (completion_id)
    ON DELETE CASCADE,
  CONSTRAINT feed_packing_verified_quantities_kg_range
    CHECK (entered_kg >= 0 AND entered_kg <= 10000),
  CONSTRAINT feed_packing_verified_quantities_key_nonblank
    CHECK (btrim(feed_item_key) <> '')
);

COMMENT ON TABLE public.feed_packing_verified_quantities IS
  'Verifier-entered packed weight per feed item of one feed-packing completion (blind entry; the approve carries the numbers). Joined to feed_direction_issue_rows on feed_item_key for the leadership-only intended-vs-entered variance.';

-- +goose Down
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '30s';

DROP TABLE IF EXISTS public.feed_packing_verified_quantities;
