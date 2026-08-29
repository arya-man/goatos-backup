-- +goose Up
-- Feed packing PACKED-AGAINST snapshot (maintainer decision 2026-08-29, extending -- not changing --
-- the 2026-08-10 afternoon-correction reopen).
--
-- The STG incident this closes: a pen's sheet was issued at 07:00 for two animals and packed and
-- filmed that morning; an afternoon shifting moved animals into the pen before the 14:00 correction,
-- which (correctly, per the 2026-08-10 rule) reopened the packing -- but the reopened card showed
-- ONLY the corrected quantities, with a generic sentence. Nothing anywhere kept what the operator
-- had actually packed against, so the card silently rewrote the number under the packer's feet and
-- the record of the bag that was really filled was gone from every screen.
--
-- Fix: the submit SNAPSHOTS the frozen sheet's directed quantities onto the completion row, frozen
-- at the moment the operator recorded the bag. The afternoon correction then composes its reopen
-- reason from this snapshot plus the corrected sheet ("2 -> 12 animals ... you packed 4 kg, this bag
-- is now 24 kg") and the feed.packing.reopened push notification carries the same numbers.
--
-- These columns are a HISTORICAL RECORD, not serving truth for the worklist: the packing worklist
-- still renders the FROZEN ISSUED SHEET's quantities (the live instruction), and this snapshot only
-- says what the sheet directed WHEN THIS ROW WAS SUBMITTED. A rework re-submit refreshes it, because
-- the operator repacked against the then-current sheet. All three are nullable: rows submitted
-- before this migration have no snapshot, and a submit whose sheet cannot be read at snapshot time
-- still lands (the snapshot is decoration on the completion, never a precondition for recording an
-- operator's work).
ALTER TABLE public.feed_packing_completions
  -- packed_head_count is the pen's projected head count on the frozen sheet at submit time (the
  -- denominator the operator saw on the card).
  ADD COLUMN IF NOT EXISTS packed_head_count bigint,
  -- packed_total_kg is the session's directed total at submit time, mirroring the worklist row's
  -- total_kg ("4.000").
  ADD COLUMN IF NOT EXISTS packed_total_kg numeric(12,3),
  -- packed_items is the per-item directed quantity list at submit time:
  --   [{"key": "<normalized feed item key>", "label": "<display label>", "quantity_kg": "4.000"}]
  -- The key is domain.NormalizeConfigKey output -- the same key the frozen sheet rows and the
  -- verifier's entered readings (feed_packing_verified_quantities.feed_item_key) join by.
  ADD COLUMN IF NOT EXISTS packed_items jsonb;

-- A present head count is a real count (a pen with zero projected animals has no packable bag, but
-- 0 is kept representable for the vanished-grain edge); a present total is non-negative. NULL means
-- "not snapshotted", never zero.
ALTER TABLE public.feed_packing_completions
  DROP CONSTRAINT IF EXISTS feed_packing_completions_packed_head_count_ck,
  ADD CONSTRAINT feed_packing_completions_packed_head_count_ck
    CHECK (packed_head_count IS NULL OR packed_head_count >= 0);
ALTER TABLE public.feed_packing_completions
  DROP CONSTRAINT IF EXISTS feed_packing_completions_packed_total_kg_ck,
  ADD CONSTRAINT feed_packing_completions_packed_total_kg_ck
    CHECK (packed_total_kg IS NULL OR packed_total_kg >= 0);

-- +goose Down
ALTER TABLE public.feed_packing_completions
  DROP CONSTRAINT IF EXISTS feed_packing_completions_packed_head_count_ck,
  DROP CONSTRAINT IF EXISTS feed_packing_completions_packed_total_kg_ck;
ALTER TABLE public.feed_packing_completions
  DROP COLUMN IF EXISTS packed_head_count,
  DROP COLUMN IF EXISTS packed_total_kg,
  DROP COLUMN IF EXISTS packed_items;
