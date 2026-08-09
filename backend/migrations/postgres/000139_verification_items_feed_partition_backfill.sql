-- +goose Up
-- A feed verification item rendered its shed without the PEN, so a verifier reviewing a packing
-- video for "Mandela 1 - Part 2" saw only "Mandela 1" and could not tell which of ten pens the clip
-- was from.
--
-- The item's partition_label was never populated by the feed bridges: they folded the pen into the
-- display SubjectLabel and left the column NULL, so oploc.Display() at the wire boundary composed
-- shed_label with a NULL partition and degraded to the bare shed name. The producers now set the
-- field, which fixes every item enqueued from here on; this backfills the ones already queued.
--
-- This RECOVERS A KNOWN FACT rather than inventing one, which is why it is a backfill at all. The
-- source completion row carries the pen the operator actually worked (migration 000137), and the
-- verification item points at that exact row by (tenant_id, source_ref_id). The join is 1:1 on the
-- completion's primary key, so no item can take another pen's label.
--
-- Contrast with 000137, which deliberately did NOT back-fill a pen onto pre-existing COMPLETIONS: a
-- completion recorded before that migration genuinely did not know its pen, and stamping one would
-- have fabricated proof provenance. Here the pen is recorded on the row we are copying FROM. Items
-- whose completion predates 000137 still have no pen to copy and are correctly left NULL -- they
-- keep rendering the bare shed, which is honest.
--
-- Feed TRANSPORT is deliberately absent: a transport task is one per physical shed per day and has
-- no pen grain at all, so there is nothing to copy.
UPDATE public.verification_items vi
SET partition_label = c.partition_label,
    updated_at = now()
FROM public.feed_packing_completions c
WHERE vi.tenant_id = c.tenant_id
  AND vi.source_ref_type = 'feed_packing_completion'
  AND vi.source_ref_id = c.completion_id
  AND vi.partition_label IS NULL
  AND c.partition_label IS NOT NULL
  AND btrim(c.partition_label) <> '';

UPDATE public.verification_items vi
SET partition_label = c.partition_label,
    updated_at = now()
FROM public.feed_distribution_completions c
WHERE vi.tenant_id = c.tenant_id
  AND vi.source_ref_type = 'feed_distribution_completion'
  AND vi.source_ref_id = c.completion_id
  AND vi.partition_label IS NULL
  AND c.partition_label IS NOT NULL
  AND btrim(c.partition_label) <> '';

-- +goose Down
-- Irreversible by design: the column is shared with producers that have always populated it
-- (vaccination, shifting), so a blanket NULL-out would destroy pens this migration never touched,
-- and there is no marker distinguishing a backfilled row from one written correctly at enqueue.
-- Re-running the Up is safe and idempotent, which is the recovery path that matters.
SELECT 1;
