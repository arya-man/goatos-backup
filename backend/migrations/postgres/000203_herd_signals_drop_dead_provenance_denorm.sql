-- Drop the denormalised mapping-provenance columns from herd_signal_tag_latest.
--
-- 000199 added mapped_by/mapped_at to BOTH goat_identifiers (authoritative, written inside the
-- binding transaction) and herd_signal_tag_latest (a denormalised copy for the hot read path).
-- The copy was never maintained: two separate attempts to populate it from the mapping writes left
-- it NULL after a real MAP, verified against the running API each time.
--
-- Nothing reads it. The live query resolves provenance by joining goat_identifiers, which works and
-- is proven -- a freshly bound tag returns its actor and timestamp; the 19 seeded bindings return a
-- timestamp and a null actor, which is honest because they predate the column and nobody knows who
-- made them.
--
-- A denormalised column that nothing writes is worse than no column at all: the next person to read
-- the schema will believe it carries provenance, and it will be silently, permanently stale. If the
-- hot path ever genuinely needs provenance without a join, this can come back WITH the write that
-- maintains it and a test that fails when it drifts -- which is what was missing both times.

-- +goose Up
DROP INDEX IF EXISTS public.herd_signal_tag_latest_mapped_by_idx;
DROP INDEX IF EXISTS public.herd_signal_tag_latest_mapped_at_idx;
ALTER TABLE public.herd_signal_tag_latest DROP COLUMN IF EXISTS mapped_by;
ALTER TABLE public.herd_signal_tag_latest DROP COLUMN IF EXISTS mapped_at;

-- +goose Down
ALTER TABLE public.herd_signal_tag_latest ADD COLUMN IF NOT EXISTS mapped_by text;
ALTER TABLE public.herd_signal_tag_latest ADD COLUMN IF NOT EXISTS mapped_at timestamptz;
