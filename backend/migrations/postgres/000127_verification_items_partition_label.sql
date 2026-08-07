-- +goose Up
-- +goose NO TRANSACTION
-- Add partition_label to verification_items to enable partition-aware filtering and display.
-- The column is nullable (non-partitioned sheds have NULL) and is populated from the verification
-- item's source at creation time (e.g., from goat_shed_partitions for vaccination items).
--
-- LOCK SAFETY: ADD COLUMN nullable is a fast catalog-only change (no table rewrite).
-- The partition-aware index is built CONCURRENTLY in 000128, which cannot share this
-- migration: goose's NO TRANSACTION marker applies to the WHOLE migration, so keeping the
-- concurrent index here would either run it inside a transaction (Postgres rejects it outright)
-- or strip the transaction from this ALTER + backfill, losing their atomicity.
--
-- This migration itself now runs NO TRANSACTION (autocommit) so the backfill below can use
-- real keyset batching with a COMMIT after every batch instead of one unbounded UPDATE. Each
-- individual statement (the ALTER and each backfill batch) is still wrapped in its own short
-- lock/statement timeout, so no single statement can hold a broad lock or exceed 30s.
SET lock_timeout = '2s';
SET statement_timeout = '30s';

ALTER TABLE public.verification_items
    ADD COLUMN partition_label text;

-- Backfill partition_label for vaccination items from goat_shed_partitions.
-- KEYSET BATCHING: processes verification_items in stable item_id order, BATCH_SIZE rows at a
-- time, COMMITting after each batch so no single transaction holds locks across the whole
-- (potentially production-sized) verifier queue or risks hitting statement_timeout mid-run.
-- LIMITATION: Uses CURRENT partition from goat_shed_partitions, not historical.
-- This is acceptable because verification items are created on the current snapshot date;
-- backfill replicates that same source.
--
-- SHED MATCH REQUIRED: a goat can move sheds after its verification item was created. The item
-- keeps the OLD vi.shed_id it was raised against, so the backfill must only take the partition
-- when goat_shed_partitions reports the SAME shed (gsp.shed_id = vi.shed_id) as the item, and
-- only when the item actually carries a shed_id. Joining by tenant+goat alone would staple the
-- goat's CURRENT shed's partition onto a row that belongs to an OLD shed -- an impossible
-- shed+partition pair.
--
-- For non-vaccination producers (weighing, feeddirection, death):
-- Backfill is deferred. Those items will display NULL partition_label (parent shed only)
-- until those producers are updated to supply partition_label at creation time.
DO $$
DECLARE
    batch_size CONSTANT integer := 500;
    rows_updated integer;
    batches_run integer := 0;
BEGIN
    LOOP
        UPDATE public.verification_items vi
        SET partition_label = gsp.partition_label
        FROM public.goat_shed_partitions gsp
        WHERE vi.item_id IN (
            SELECT candidate.item_id
            FROM public.verification_items candidate
            WHERE candidate.source_module = 'vaccination'
              AND candidate.source_ref_type = 'vaccination_goat'
              AND candidate.partition_label IS NULL
              AND candidate.shed_id IS NOT NULL
            ORDER BY candidate.item_id
            LIMIT batch_size
        )
          AND gsp.tenant_id = vi.tenant_id
          AND gsp.goat_id = (vi.source_ref_id::uuid)
          AND gsp.shed_id = vi.shed_id
          AND vi.shed_id IS NOT NULL;

        GET DIAGNOSTICS rows_updated = ROW_COUNT;
        batches_run := batches_run + 1;
        EXIT WHEN rows_updated = 0;
        COMMIT;
    END LOOP;
    RAISE NOTICE 'verification_items partition_label backfill: % batch(es)', batches_run;
END $$;

-- +goose Down
-- +goose NO TRANSACTION
SET lock_timeout = '2s';
SET statement_timeout = '30s';

ALTER TABLE public.verification_items
    DROP COLUMN IF EXISTS partition_label;
