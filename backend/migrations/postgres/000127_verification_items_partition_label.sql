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
-- real keyset batching with a COMMIT after every batch instead of one unbounded UPDATE.
-- The ALTER is wrapped in a short lock/statement timeout; the DO block below disables
-- statement_timeout because it is ONE top-level statement that internally loops and commits
-- (inner COMMITs do not reset outer statement_timeout, so a per-batch COMMIT safety mechanism
-- requires the outer statement timeout to be absent).
SET lock_timeout = '2s';
SET statement_timeout = '30s';

ALTER TABLE public.verification_items
    ADD COLUMN partition_label text;

-- Backfill partition_label for vaccination items from goat_shed_partitions.
-- KEYSET BATCHING: processes verification_items in stable item_id order, BATCH_SIZE rows at a
-- time, COMMITting after each batch so no single transaction holds locks across the whole
-- (potentially production-sized) verifier queue. The DO block is one top-level statement,
-- so statement_timeout must be cleared here; inner COMMITs do not reset outer timeout.
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
SET statement_timeout = '0';  -- clear for batching loop; per-batch COMMIT provides lock safety
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
              -- Only rows that will ACTUALLY update. Without this, a row with no
              -- matching goat_shed_partitions (a goat that moved: allowed to stay
              -- NULL by design) is re-selected at the head of EVERY batch, never
              -- updates, and drives rows_updated to 0 -- exiting the loop while
              -- eligible rows with higher item_ids are still unprocessed. A few
              -- old moved-goat rows near the front would strand the rest of a
              -- production verifier queue.
              AND EXISTS (
                    SELECT 1
                    FROM public.goat_shed_partitions g
                    WHERE g.tenant_id = candidate.tenant_id
                      AND g.goat_id = (candidate.source_ref_id::uuid)
                      AND g.shed_id = candidate.shed_id
              )
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
