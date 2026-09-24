-- Auditable scheduling basis for vaccination obligations.
--
-- 'anchored' rows must prove their due date against the clinical anchor (DOB, accepted intake,
-- or previous completion) and fail closed when that anchor is missing. The single approved
-- exception is generation's adult catch-up for a blank vaccine family whose DOB / entry anchor
-- is unknown; those rows are stamped 'anchor_missing_catch_up' so the exception is visible and
-- every later write path (reopen, reschedule, drive override, reconcile, publish carry-over)
-- re-proves the same basis under the row lock instead of trusting a blanket allow.

-- +goose Up

ALTER TABLE public.obligation_instances
  ADD COLUMN IF NOT EXISTS schedule_basis text NOT NULL DEFAULT 'anchored';

ALTER TABLE public.obligation_instances
  DROP CONSTRAINT IF EXISTS obligation_instances_schedule_basis_check;
-- Goose runs this file in one transaction, so ADD COLUMN's ACCESS EXCLUSIVE lock is held through
-- VALIDATE. That is acceptable: the constant default is metadata-only and every existing row is
-- already 'anchored', so validation is one sequential read with no rewrite.
ALTER TABLE public.obligation_instances
  ADD CONSTRAINT obligation_instances_schedule_basis_check
  CHECK (schedule_basis IN ('anchored', 'anchor_missing_catch_up')) NOT VALID;
ALTER TABLE public.obligation_instances
  VALIDATE CONSTRAINT obligation_instances_schedule_basis_check;

COMMENT ON COLUMN public.obligation_instances.schedule_basis IS
  'Why this vaccination row may be scheduled: anchored (proved against DOB/intake/previous completion) or anchor_missing_catch_up (approved adult catch-up for a blank vaccine family with no DOB/entry anchor).';

-- +goose Down

ALTER TABLE public.obligation_instances
  DROP CONSTRAINT IF EXISTS obligation_instances_schedule_basis_check;
ALTER TABLE public.obligation_instances
  DROP COLUMN IF EXISTS schedule_basis;
