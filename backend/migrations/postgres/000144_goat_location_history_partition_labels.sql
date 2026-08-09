-- +goose Up
-- seed-fixture-guard:ignore: identity audit history schema only; no seed contract change
--
-- goat_location_history records physical shed moves. Partitioned sheds are operational locations,
-- so the history row must snapshot the source/destination partition at write time; otherwise a
-- later goat_shed_partitions update can relabel old moves.

ALTER TABLE public.goat_location_history
  ADD COLUMN IF NOT EXISTS from_partition_label text,
  ADD COLUMN IF NOT EXISTS to_partition_label text;

CREATE INDEX IF NOT EXISTS goat_location_history_tenant_from_partition_idx
  ON public.goat_location_history (tenant_id, from_location_id, COALESCE(from_partition_label, ''), occurred_at DESC)
  WHERE from_location_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS goat_location_history_tenant_to_partition_idx
  ON public.goat_location_history (tenant_id, to_location_id, COALESCE(to_partition_label, ''), occurred_at DESC);

-- +goose Down
DROP INDEX IF EXISTS public.goat_location_history_tenant_to_partition_idx;
DROP INDEX IF EXISTS public.goat_location_history_tenant_from_partition_idx;

ALTER TABLE public.goat_location_history
  DROP COLUMN IF EXISTS to_partition_label,
  DROP COLUMN IF EXISTS from_partition_label;
