-- +goose Up
-- Notification vocabulary for pen routines (maintainer instruction 2026-09-16): the push telling
-- a routine's assignees that today's pen checks are due rides 'pen_routine_due'. Enum widening
-- on a table no seed path writes; the lock-timeout / NOT VALID / VALIDATE shape is 000252's,
-- 000279's, 000289's, 000301's and 000306's. The array is 000306's current list plus ours.
SET lock_timeout = '5s';
-- seed-migration-guard:ignore owner=manohark issue=pen-routines reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
ALTER TABLE public.notification_requests
  DROP CONSTRAINT IF EXISTS notification_requests_type_check;
-- seed-migration-guard:ignore owner=manohark issue=pen-routines reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
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
    'pen_routine_due'::text
  ]))) NOT VALID;
-- seed-migration-guard:ignore owner=manohark issue=pen-routines reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
ALTER TABLE public.notification_requests
  VALIDATE CONSTRAINT notification_requests_type_check;
RESET lock_timeout;

-- +goose Down
SET lock_timeout = '5s';
-- seed-migration-guard:ignore owner=manohark issue=pen-routines reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
ALTER TABLE public.notification_requests
  DROP CONSTRAINT IF EXISTS notification_requests_type_check;
-- seed-migration-guard:ignore owner=manohark issue=pen-routines reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
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
    'market_survey_due'::text
  ]))) NOT VALID;
-- seed-migration-guard:ignore owner=manohark issue=pen-routines reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
ALTER TABLE public.notification_requests
  VALIDATE CONSTRAINT notification_requests_type_check;
RESET lock_timeout;
