-- +goose Up
-- SOP-driven questionnaires (2026-09-13): when a verifier sends a step back, the operator must
-- see WHY on the step itself, not only on the owning module's card. The reason is copied onto
-- the reopened step by the tasks engine and cleared when the step is completed again.
-- seed-migration-guard:ignore owner=manohar issue=sop-driven-herd-operations reason=additive-nullable-copy-column;no-seed-path-writes-it expiry=2026-12-31
ALTER TABLE public.workflow_actions ADD COLUMN IF NOT EXISTS rework_reason text;

-- +goose Down
ALTER TABLE public.workflow_actions DROP COLUMN IF EXISTS rework_reason;
