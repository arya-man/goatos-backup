-- Auditable scheduling basis for vaccination obligations.
--
-- 'anchored' rows must prove their due date against the clinical anchor (DOB, accepted intake,
-- or previous completion) and fail closed when that anchor is missing. The single approved
-- exception is generation's adult catch-up for a blank vaccine family whose DOB / entry anchor
-- is unknown; those rows are stamped 'anchor_missing_catch_up' so the exception is visible and
-- every later write path (reopen, reschedule, drive override, reconcile, publish carry-over)
-- re-proves the same basis under the row lock instead of trusting a blanket allow.

-- +goose NO TRANSACTION

-- +goose Up

ALTER TABLE public.obligation_instances
  ADD COLUMN IF NOT EXISTS schedule_basis text NOT NULL DEFAULT 'anchored';

-- Every statement commits independently: the metadata-only column addition releases its
-- ACCESS EXCLUSIVE lock before validation scans the populated table. The conditional add
-- makes an interrupted nontransactional migration retryable without removing enforcement.
-- +goose StatementBegin
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conrelid = 'public.obligation_instances'::regclass
      AND conname = 'obligation_instances_schedule_basis_check'
  ) THEN
    ALTER TABLE public.obligation_instances
      ADD CONSTRAINT obligation_instances_schedule_basis_check
      CHECK (schedule_basis IN ('anchored', 'anchor_missing_catch_up')) NOT VALID;
  END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE public.obligation_instances
  VALIDATE CONSTRAINT obligation_instances_schedule_basis_check;

COMMENT ON COLUMN public.obligation_instances.schedule_basis IS
  'Why this vaccination row may be scheduled: anchored (proved against DOB/intake/previous completion) or anchor_missing_catch_up (approved adult catch-up for a blank vaccine family with no DOB/entry anchor).';

-- +goose Down

ALTER TABLE public.obligation_instances
  DROP COLUMN IF EXISTS schedule_basis;
