-- +goose Up
-- Lifecycle stages are edited through Configuration -> Items and settings and exported for
-- update-capable sheet imports. Give the tenant-scoped table the same optimistic fence the
-- other editable registers expose as row_version.
ALTER TABLE public.animal_stage_lookup
  ADD COLUMN IF NOT EXISTS row_version integer NOT NULL DEFAULT 1 CHECK (row_version >= 1);

-- +goose Down
ALTER TABLE public.animal_stage_lookup
  DROP COLUMN IF EXISTS row_version;
