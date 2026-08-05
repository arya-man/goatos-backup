-- +goose Up
-- +goose NO TRANSACTION

-- Recoverability for exhausted notifications (defect: an 'exhausted' row is a dead end today --
-- ClaimDue only ever claims status IN ('queued','failed'), so once ErrChannelNotConfigured (or
-- max-attempts) parks a row at 'exhausted' nothing in the codebase can ever move it back into the
-- claimable set). This migration adds the columns the operator-driven requeue path
-- (cmd/notification-requeue, notification/ports.RequeueRepository) needs to record WHO requeued a
-- row and HOW MANY TIMES, so a requeue is auditable and so a runaway repeated-requeue of the same
-- permanently-broken row is visible instead of silently retried forever.
--
-- ADD COLUMN ... DEFAULT is a metadata-only change on Postgres 11+ (no table rewrite, no long
-- lock), so this runs in NO TRANSACTION only for consistency with this migration set's convention
-- of one DDL statement per file; it does not itself need CONCURRENTLY.
ALTER TABLE public.notification_requests
  ADD COLUMN IF NOT EXISTS requeued_at timestamptz,
  ADD COLUMN IF NOT EXISTS requeued_by text,
  ADD COLUMN IF NOT EXISTS requeue_count integer NOT NULL DEFAULT 0;

-- Guarded rather than a bare ADD CONSTRAINT. Postgres has no ADD CONSTRAINT IF NOT EXISTS, so a
-- bare form makes this file fatal to re-run: the columns above skip cleanly via IF NOT EXISTS and
-- then the constraint raises "already exists" and aborts the whole migration run. That is not
-- hypothetical -- this file was applied by hand to the E2E database without a
-- goatos_schema_migrations row, so the next migrate run saw it as pending and crashed here.
-- Every statement in a migration must be safe to re-execute, because "applied" and "recorded as
-- applied" can always drift.
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conname = 'notification_requests_requeue_count_check'
      AND conrelid = 'public.notification_requests'::regclass
  ) THEN
    ALTER TABLE public.notification_requests
      ADD CONSTRAINT notification_requests_requeue_count_check CHECK (requeue_count >= 0);
  END IF;
END $$;

-- +goose Down
-- +goose NO TRANSACTION
ALTER TABLE public.notification_requests
  DROP CONSTRAINT IF EXISTS notification_requests_requeue_count_check;

ALTER TABLE public.notification_requests
  DROP COLUMN IF EXISTS requeue_count,
  DROP COLUMN IF EXISTS requeued_by,
  DROP COLUMN IF EXISTS requeued_at;
