-- +goose Up
-- SOP → operator + verifier parity, herd operations (maintainer decisions 2026-09-16,
-- docs/decisions/sop-driven-herd-operations.md → "Phase 2: capture forms").
--
-- workflow_instances.capture_evidence: the Add death form's SNAPSHOT (proofs under their
-- authored titles with the register's kind, answers in farm words, the older-app missing note),
-- stamped at open from counts.death.reported and leading the death verifier bundle. '{}' for
-- every workflow opened without one (birth tracks, reconcile, every pre-feature row). Never
-- rewritten: a redelivered event hits the natural key and touches nothing.
--
-- pen_reconciliation_cards.verification_media_meta / verification_context_rows: what the
-- reconcile questionnaire captured, carried on the card so the durable enqueue-debt recovery
-- re-enqueues the verifier item with EVERY proof's title and kind and every answer, not the
-- single legacy proof_ref (bug 4). '[]' for legacy one-video cards.
--
-- Additive only; no backfill needed -- an empty snapshot reads as "the form asked nothing".

ALTER TABLE public.workflow_instances
  ADD COLUMN IF NOT EXISTS capture_evidence jsonb NOT NULL DEFAULT '{}'::jsonb;
COMMENT ON COLUMN public.workflow_instances.capture_evidence IS
  'Capture form snapshot {version_label, media:[{ref,kind,label}], rows:[{label,value,group}], missing_note} stamped at open (death: the Add death form); leads the verifier bundle. {} = none.';

ALTER TABLE public.pen_reconciliation_cards
  ADD COLUMN IF NOT EXISTS verification_media_meta jsonb NOT NULL DEFAULT '[]'::jsonb,
  ADD COLUMN IF NOT EXISTS verification_context_rows jsonb NOT NULL DEFAULT '[]'::jsonb;
COMMENT ON COLUMN public.pen_reconciliation_cards.verification_media_meta IS
  '[{label,kind}] positional against proof_refs: the SOP step title and register kind of each proof the verifier item carries (SOP parity, 2026-09-16).';
COMMENT ON COLUMN public.pen_reconciliation_cards.verification_context_rows IS
  '[{label,value,group}] the questionnaire answers in farm words, grouped by step, carried on the verifier item and its recovery re-enqueue.';

-- +goose Down
ALTER TABLE public.pen_reconciliation_cards
  DROP COLUMN IF EXISTS verification_context_rows,
  DROP COLUMN IF EXISTS verification_media_meta;
ALTER TABLE public.workflow_instances DROP COLUMN IF EXISTS capture_evidence;
