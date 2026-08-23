-- Herd Signals: tag mapping provenance (who and when).
--
-- When a BLE tag is mapped to an animal identifier, the mapping must record:
-- 1. mapped_by: the user_id of the person who performed the mapping
-- 2. mapped_at: the server timestamp when the mapping was recorded
--
-- These fields enable the Tag Mapping tab to answer "which operator bound this tag,
-- and when?" for every row without guessing from the updated_at noise.

-- +goose Up

ALTER TABLE public.goat_identifiers
  ADD COLUMN IF NOT EXISTS mapped_by text;

ALTER TABLE public.goat_identifiers
  ADD COLUMN IF NOT EXISTS mapped_at timestamptz;

COMMENT ON COLUMN public.goat_identifiers.mapped_by IS
  'User ID of the operator who bound this identifier to a smart tag (mapping provenance).';

COMMENT ON COLUMN public.goat_identifiers.mapped_at IS
  'Server timestamp when this identifier was bound to a smart tag (mapping provenance).';

-- When mapping_since is set (smart tag was mapped), also backfill the provenance columns
-- with the same timestamp (we did not record the actor, so use a sentinel or the
-- identifier''s created_at). This maintains consistency for historical mappings.
UPDATE public.goat_identifiers
   SET mapped_at = COALESCE(mapped_at, smart_tag_mapped_at, updated_at)
 WHERE smart_tag_capable IS TRUE
   AND mapped_at IS NULL;

-- Denormalise onto herd_signal_tag_latest for efficient read path.
ALTER TABLE public.herd_signal_tag_latest
  ADD COLUMN IF NOT EXISTS mapped_by text;

ALTER TABLE public.herd_signal_tag_latest
  ADD COLUMN IF NOT EXISTS mapped_at timestamptz;

COMMENT ON COLUMN public.herd_signal_tag_latest.mapped_by IS
  'Denormalised from goat_identifiers.mapped_by for the live read path.';

COMMENT ON COLUMN public.herd_signal_tag_latest.mapped_at IS
  'Denormalised from goat_identifiers.mapped_at for the live read path.';

-- Partial index for efficient lookups of mapped tags with provenance.
CREATE INDEX IF NOT EXISTS herd_signal_tag_latest_mapped_provenance_idx
  ON public.herd_signal_tag_latest (tenant_id, mapped_at DESC)
  WHERE mapped_at IS NOT NULL;

-- +goose Down

DROP INDEX IF EXISTS public.herd_signal_tag_latest_mapped_provenance_idx;
ALTER TABLE public.herd_signal_tag_latest DROP COLUMN IF EXISTS mapped_at;
ALTER TABLE public.herd_signal_tag_latest DROP COLUMN IF EXISTS mapped_by;
ALTER TABLE public.goat_identifiers DROP COLUMN IF EXISTS mapped_at;
ALTER TABLE public.goat_identifiers DROP COLUMN IF EXISTS mapped_by;
