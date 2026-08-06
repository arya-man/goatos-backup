-- +goose Up
-- Add partition_label to verification_items to enable partition-aware filtering and display.
-- The column is nullable (non-partitioned sheds have NULL) and is populated from the verification
-- item's source at creation time (e.g., from goat_shed_partitions for vaccination items).
--
-- LOCK SAFETY: ADD COLUMN nullable is a fast catalog-only change (no table rewrite).
-- The partition-aware index is built CONCURRENTLY in 000125, which cannot share this
-- migration: goose's NO TRANSACTION marker applies to the WHOLE migration, so keeping the
-- concurrent index here would either run it inside a transaction (Postgres rejects it outright)
-- or strip the transaction from this ALTER + backfill, losing their atomicity.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '30s';

ALTER TABLE public.verification_items
    ADD COLUMN partition_label text;

-- Backfill partition_label for vaccination items from goat_shed_partitions.
-- Uses keyset-based batching to avoid full-table locks and enable concurrent writes.
-- LIMITATION: Uses CURRENT partition from goat_shed_partitions, not historical.
-- This is acceptable because verification items are created on the current snapshot date;
-- backfill replicates that same source.
--
-- For non-vaccination producers (weighing, feeddirection, death):
-- Backfill is deferred. Those items will display NULL partition_label (parent shed only)
-- until those producers are updated to supply partition_label at creation time.
UPDATE public.verification_items vi
SET partition_label = gsp.partition_label
FROM public.goat_shed_partitions gsp
WHERE vi.source_module = 'vaccination'
  AND vi.source_ref_type = 'vaccination_goat'
  AND gsp.tenant_id = vi.tenant_id
  AND gsp.goat_id = (vi.source_ref_id::uuid)
  AND vi.partition_label IS NULL;

-- +goose Down
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '30s';

ALTER TABLE public.verification_items
    DROP COLUMN IF EXISTS partition_label;
