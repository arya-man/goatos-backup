-- +goose Up
-- seed-fixture-guard:ignore: notification vocabulary widening only.
--
-- PC CARE REPEAT SKIPPED (maintainer instruction 2026-09-30, docs/decisions/pc-care-repeat.md):
-- when a SOP card's "repeat every N days" cannot plan a pen's next task because none of its last
-- operators can still be assigned, the planner of the last task gets ONE push, type
-- 'pc_care_repeat_skipped' (notificationbridge.PCCareRepeatSkippedNotifier).
-- notification_requests_type_check is a closed list, so the type is added here or every insert
-- fails 23514 and the alert silently never arrives (caught by
-- TestEveryNotificationTypeIsAllowedByTheCheckConstraint). Shape and list are 000446's plus ours.
SET lock_timeout = '5s';
-- seed-migration-guard:ignore owner=manohark issue=pc-care-repeat reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
ALTER TABLE public.notification_requests
  DROP CONSTRAINT IF EXISTS notification_requests_type_check;
-- seed-migration-guard:ignore owner=manohark issue=pc-care-repeat reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
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
    'feed_sale_failed_return'::text,
    'pc_care_repeat_skipped'::text
  ]))) NOT VALID;
-- seed-migration-guard:ignore owner=manohark issue=pc-care-repeat reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
ALTER TABLE public.notification_requests
  VALIDATE CONSTRAINT notification_requests_type_check;
RESET lock_timeout;

-- +goose Down
SET lock_timeout = '5s';
-- seed-migration-guard:ignore owner=manohark issue=pc-care-repeat reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
ALTER TABLE public.notification_requests
  DROP CONSTRAINT IF EXISTS notification_requests_type_check;
-- seed-migration-guard:ignore owner=manohark issue=pc-care-repeat reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
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
-- seed-migration-guard:ignore owner=manohark issue=pc-care-repeat reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
ALTER TABLE public.notification_requests
  VALIDATE CONSTRAINT notification_requests_type_check;
RESET lock_timeout;
