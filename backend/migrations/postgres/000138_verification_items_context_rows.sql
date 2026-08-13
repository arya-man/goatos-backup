-- +goose Up
-- A verification item told the verifier WHICH work was done and never WHAT was expected of it.
--
-- Reported on STG 2026-08-09: a feed packing item carries the shed, the pen, the session, the
-- operator and the video -- and nothing about the ration. The verifier is asked "is this packing
-- video acceptable?" with no way to know whether the bags hold the right feed in the right amount,
-- so she can only confirm that a video exists, not that the work was right. The same hole exists
-- for every other producer: a weighing proof carries no expected animal count, a vaccination proof
-- no vaccine dose.
--
-- context_rows is the generic answer rather than a feed-specific column: an ORDERED array of
-- backend-composed {label, value} pairs the PRODUCING module attaches at enqueue time, which the
-- verifier surfaces render verbatim. A feed-specific `ration` column would have to be joined by a
-- `weighing_expected` column next quarter and a `vaccine_dose` one after that, each with its own
-- five-handoff chain; one generic column serves every producer and keeps verification ignorant of
-- what its producers actually measure, which is the whole point of the generic queue.
--
-- Composed AT ENQUEUE and stored, deliberately, rather than resolved at read time from the source
-- record. The expected ration is read from the FROZEN issued sheet at the moment the operator
-- submits, so re-authoring the feed config afterwards cannot rewrite what the verifier is judging
-- against -- the item shows what was expected WHEN THE WORK WAS DONE. A read-time join would show
-- today's config against yesterday's video and quietly change a verdict's basis.
--
-- Copy rule: values are backend-owned display strings (the dumb-renderer rule). Producers compose
-- farm language ("Maize 12.5 kg"), never config tokens -- ui-vaccine-labels-guard's rule applies
-- here too.
--
-- Lock safety: a constant DEFAULT makes this metadata-only on PG11+ (no table rewrite). The array
-- CHECK is added NOT VALID and validated separately so the write lock is never held across a scan.
-- Metadata-only DDL still needs ACCESS EXCLUSIVE briefly; fail and retry the deploy instead of
-- waiting unbounded behind a long-running transaction while blocking new work on this hot table.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '30s';

ALTER TABLE public.verification_items
  ADD COLUMN IF NOT EXISTS context_rows jsonb DEFAULT '[]'::jsonb NOT NULL;

ALTER TABLE public.verification_items
  DROP CONSTRAINT IF EXISTS verification_items_context_rows_is_array;
ALTER TABLE public.verification_items
  ADD CONSTRAINT verification_items_context_rows_is_array
  CHECK (jsonb_typeof(context_rows) = 'array') NOT VALID;
ALTER TABLE public.verification_items
  VALIDATE CONSTRAINT verification_items_context_rows_is_array;

COMMENT ON COLUMN public.verification_items.context_rows IS
  'Ordered [{"label","value"}] of backend-composed context the producing module attached at enqueue (e.g. the frozen expected ration for a feed packing session). Rendered verbatim by verifier surfaces; never parsed for business logic.';

-- +goose Down
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '30s';

ALTER TABLE public.verification_items
  DROP CONSTRAINT IF EXISTS verification_items_context_rows_is_array;
ALTER TABLE public.verification_items
  DROP COLUMN IF EXISTS context_rows;
