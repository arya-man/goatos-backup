-- +goose Up
-- seed-fixture-guard:ignore: one nullable column on workflow_instances written by the opener; no
-- seed contract, source fixture or read-model shape change.
--
-- A PLANNED SALE'S WORK IS DUE ON THE SALE DAY (2026-09-26, docs/decisions/sales-sop.md). Every
-- sales.deal step is due "immediately", which counted from the RECORDING instant, so an open sale
-- planned for a later day read "Overdue" the moment it was saved. A workflow whose clock counts from
-- somewhere other than event_at stores that instant here: a sale dated after its recording day
-- anchors at the start of that day (Asia/Kolkata). NULL = the clock counts from event_at, which is
-- every other workflow and every existing row. Kept so a later re-anchor (the sale closing early,
-- which restamps its date) can move the unfinished steps exactly once.
-- Nullable with no default: a metadata-only ALTER, no table rewrite.
ALTER TABLE public.workflow_instances ADD COLUMN IF NOT EXISTS clock_anchor_at timestamptz;
COMMENT ON COLUMN public.workflow_instances.clock_anchor_at IS
  'The instant step due times count from when it is not event_at (a sale planned for a later day, 2026-09-26). NULL = event_at.';

-- +goose Down
ALTER TABLE public.workflow_instances DROP COLUMN IF EXISTS clock_anchor_at;
