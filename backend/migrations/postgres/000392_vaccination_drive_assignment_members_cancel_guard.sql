-- +goose Up
-- seed-fixture-guard:ignore: vaccination_drive_assignment_members is written by the drive
-- assignment producer at runtime, never by a seed fixture. This adds an operational cancel
-- marker to rows that producer already owns -- no vaccination/HRMS seed contract, source
-- fixture schema or read-model table changes.
--
-- Add an active/canceled marker for vaccination drive membership rows.
-- Live tracker and proof-completion reconciliation must ignore stale members
-- when a drive assignment is rebuilt or reworked.
ALTER TABLE public.vaccination_drive_assignment_members
  ADD COLUMN IF NOT EXISTS canceled_at timestamp with time zone;

CREATE INDEX IF NOT EXISTS vaccination_drive_assignment_members_active_assignment_idx
  ON public.vaccination_drive_assignment_members (tenant_id, assignment_id)
  WHERE canceled_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS vaccination_drive_assignment_members_active_assignment_idx;

ALTER TABLE public.vaccination_drive_assignment_members
  DROP COLUMN IF EXISTS canceled_at;
