-- +goose Up
-- seed-fixture-guard:ignore: notification vocabulary widening only.
--
-- A FAILED SALE -> FEED DIRECTOR (maintainer decision 2026-09-25, docs/decisions/sales-sop.md):
-- when a sale fails and its tagged animals go back into their pens, the Feed Director gets ONE
-- push, type 'feed_sale_failed_return' (notificationbridge.SaleFeedReduceNotifier, consuming
-- goat.sale_released). notification_requests_type_check is a closed list, so the type is added
-- here or every insert fails 23514 and the message silently never arrives. Enum widening on a
-- table no seed path writes; the lock-timeout / NOT VALID / VALIDATE shape is 000352's, and the
-- array is 000352's current list plus ours.
SET lock_timeout = '5s';
-- seed-migration-guard:ignore owner=manohark issue=sales-failed-return reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
ALTER TABLE public.notification_requests
  DROP CONSTRAINT IF EXISTS notification_requests_type_check;
-- seed-migration-guard:ignore owner=manohark issue=sales-failed-return reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
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
    'leave_request_decided'::text,
    'animal_purchase_decided'::text,
    'market_survey_due'::text,
    'pen_routine_due'::text,
    'leadership_task_mentioned'::text,
    'leadership_task_commented'::text,
    'leadership_task_updated'::text,
    'feed_sale_failed_return'::text
  ]))) NOT VALID;
-- seed-migration-guard:ignore owner=manohark issue=sales-failed-return reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
ALTER TABLE public.notification_requests
  VALIDATE CONSTRAINT notification_requests_type_check;
RESET lock_timeout;

-- +goose Down
SET lock_timeout = '5s';
-- seed-migration-guard:ignore owner=manohark issue=sales-failed-return reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
ALTER TABLE public.notification_requests
  DROP CONSTRAINT IF EXISTS notification_requests_type_check;
-- seed-migration-guard:ignore owner=manohark issue=sales-failed-return reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
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
    'leave_request_decided'::text,
    'animal_purchase_decided'::text,
    'market_survey_due'::text,
    'pen_routine_due'::text,
    'leadership_task_mentioned'::text,
    'leadership_task_commented'::text,
    'leadership_task_updated'::text
  ]))) NOT VALID;
-- seed-migration-guard:ignore owner=manohark issue=sales-failed-return reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
ALTER TABLE public.notification_requests
  VALIDATE CONSTRAINT notification_requests_type_check;
RESET lock_timeout;
