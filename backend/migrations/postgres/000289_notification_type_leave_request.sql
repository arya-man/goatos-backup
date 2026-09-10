-- +goose Up
-- Notification vocabulary for leave requests (000288, maintainer decisions 2026-09-10): the push
-- telling a park head / HR that a person asked for leave rides 'leave_request_raised', and the
-- push telling the requester the outcome rides 'leave_request_decided'. Enum widening on a table
-- no seed path writes; the lock-timeout / NOT VALID / VALIDATE shape is 000252's and 000279's.
SET lock_timeout = '5s';
-- seed-migration-guard:ignore owner=manohark issue=leave-requests reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
ALTER TABLE public.notification_requests
  DROP CONSTRAINT IF EXISTS notification_requests_type_check;
-- seed-migration-guard:ignore owner=manohark issue=leave-requests reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
ALTER TABLE public.notification_requests
  ADD CONSTRAINT notification_requests_type_check
  CHECK ((notification_type = ANY (ARRAY[
    'reminder'::text,
    'nudge'::text,
    'escalation'::text,
    'verification_pending'::text,
    'verification_approved'::text,
    'verification_closed'::text,
    'verification_withdrawn'::text,
    'rework'::text,
    'advance_notice'::text,
    'due_today'::text,
    'leadership_task_raised'::text,
    'leadership_task_done'::text,
    'obligation_missed'::text,
    'feed_low_stock'::text,
    'procurement_load_overdue'::text,
    'feed_proof_times_daily'::text,
    'feed_sale_reduce'::text,
    'feed_sale_reduce_reminder'::text,
    'pen_visit_due'::text,
    'leave_request_raised'::text,
    'leave_request_decided'::text
  ]))) NOT VALID;
-- seed-migration-guard:ignore owner=manohark issue=leave-requests reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
ALTER TABLE public.notification_requests
  VALIDATE CONSTRAINT notification_requests_type_check;
RESET lock_timeout;

-- +goose Down
SET lock_timeout = '5s';
-- seed-migration-guard:ignore owner=manohark issue=leave-requests reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
ALTER TABLE public.notification_requests
  DROP CONSTRAINT IF EXISTS notification_requests_type_check;
-- seed-migration-guard:ignore owner=manohark issue=leave-requests reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
ALTER TABLE public.notification_requests
  ADD CONSTRAINT notification_requests_type_check
  CHECK ((notification_type = ANY (ARRAY[
    'reminder'::text,
    'nudge'::text,
    'escalation'::text,
    'verification_pending'::text,
    'verification_approved'::text,
    'verification_closed'::text,
    'verification_withdrawn'::text,
    'rework'::text,
    'advance_notice'::text,
    'due_today'::text,
    'leadership_task_raised'::text,
    'leadership_task_done'::text,
    'obligation_missed'::text,
    'feed_low_stock'::text,
    'procurement_load_overdue'::text,
    'feed_proof_times_daily'::text,
    'feed_sale_reduce'::text,
    'feed_sale_reduce_reminder'::text,
    'pen_visit_due'::text
  ]))) NOT VALID;
-- seed-migration-guard:ignore owner=manohark issue=leave-requests reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
ALTER TABLE public.notification_requests
  VALIDATE CONSTRAINT notification_requests_type_check;
RESET lock_timeout;
