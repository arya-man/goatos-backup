-- +goose Up
-- 000305_pen_reconciliation_sop_workflow.sql
--
-- RECONCILE IS A SOP QUESTIONNAIRE, NOT ONE VIDEO (maintainer decision 2026-09-13,
-- docs/decisions/sop-driven-herd-operations.md; supersedes the "exactly one video" half of
-- docs/decisions/pen-reconciliation.md).
--
-- A reconcile card keeps being the unit of work the weighing consumer raises and the verifier
-- judges (one open card per animal, no approver step, completion never rewrites the register).
-- What changes is HOW the operator closes it: the card now owns ONE `reconcile` workflow
-- instance stamped from the published counts.reconcile SOP -- the questions, photos and videos
-- the maintainer authored -- and the card flips to pending_verification when the workflow's
-- last step completes, carrying every proof the steps captured. The legacy one-video complete
-- route stays served for installed APKs.
--
-- workflow_instances gains a generic subject_ref_id so a workflow can be keyed on a non-goat
-- subject (the card here; the shifting event next) with its own natural-key uniqueness.
-- seed-migration-guard:ignore owner=manohar issue=sop-driven-herd-operations reason=additive-link-columns;no-seed-command-writes-reconcile-cards expiry=2026-12-31

-- The engine now runs reconcile and shifting tracks too. These are tiny tables (one row per
-- workflow); the constraint swap is a metadata-only lock.
ALTER TABLE public.workflow_instances DROP CONSTRAINT IF EXISTS workflow_instances_template_key_check;
ALTER TABLE public.workflow_instances ADD CONSTRAINT workflow_instances_template_key_check
  CHECK (template_key IN ('birth_kid', 'birth_mother', 'death', 'reconcile', 'shifting'));
ALTER TABLE public.workflow_instances DROP CONSTRAINT IF EXISTS workflow_instances_module_check;
ALTER TABLE public.workflow_instances ADD CONSTRAINT workflow_instances_module_check
  CHECK (module IN ('birth', 'death', 'reconcile', 'shifting'));

ALTER TABLE public.workflow_instances ADD COLUMN IF NOT EXISTS subject_ref_id uuid;
CREATE UNIQUE INDEX IF NOT EXISTS workflow_instances_subject_ref_uq
  ON public.workflow_instances (tenant_id, template_key, subject_ref_id)
  WHERE subject_ref_id IS NOT NULL;

ALTER TABLE public.pen_reconciliation_cards ADD COLUMN IF NOT EXISTS workflow_id uuid;
-- Every proof the workflow steps captured, [{"ref","kind"}]; proof_ref keeps the first video.
ALTER TABLE public.pen_reconciliation_cards ADD COLUMN IF NOT EXISTS proof_refs jsonb NOT NULL DEFAULT '[]'::jsonb;
CREATE INDEX IF NOT EXISTS pen_reconciliation_cards_workflow_idx
  ON public.pen_reconciliation_cards (tenant_id, workflow_id)
  WHERE workflow_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS public.pen_reconciliation_cards_workflow_idx;
ALTER TABLE public.pen_reconciliation_cards DROP COLUMN IF EXISTS workflow_id;
ALTER TABLE public.pen_reconciliation_cards DROP COLUMN IF EXISTS proof_refs;
DROP INDEX IF EXISTS public.workflow_instances_subject_ref_uq;
ALTER TABLE public.workflow_instances DROP COLUMN IF EXISTS subject_ref_id;
