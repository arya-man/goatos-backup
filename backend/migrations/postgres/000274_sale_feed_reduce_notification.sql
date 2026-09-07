-- +goose Up
-- SALE -> FEED DIRECTOR NOTICE (maintainer decision 2026-09-07).
--
-- When animals are tagged to a sale they leave the register, so the pens they stood in need less
-- feed from the next sheet. Two pushes tell the Feed Director so, both written by
-- notificationbridge.SaleFeedReduceNotifier: one the moment the sale is confirmed (naming each pen
-- and its count, and the feed day the reduction lands on), and one reminder on that feed day asking
-- the director to confirm the pens' feed did reduce. notification_requests.notification_type is a
-- CLOSED enum (see 000267 for why it stays closed), so both types are added here, in the same
-- change as the notifier -- a type outside the enum fails 23514 on every write and the push simply
-- never arrives, indistinguishable from "no sale today".
--
-- Lock-safe on a hot table, mirroring 000267 exactly: bounded lock_timeout, DROP/ADD ... NOT VALID,
-- then VALIDATE separately. Widening an enum cannot invalidate a stored row.
-- NO SEED IMPACT: no seed command or closeout step writes notification_requests (checked in 000267,
-- unchanged since); the index below is on a table only the sales confirm writes.
SET lock_timeout = '5s';

-- seed-migration-guard:ignore owner=manohark issue=sale-feed-reduce-notify reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
ALTER TABLE notification_requests
  DROP CONSTRAINT IF EXISTS notification_requests_type_check;

-- seed-migration-guard:ignore owner=manohark issue=sale-feed-reduce-notify reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
ALTER TABLE notification_requests
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
    -- Sale -> feed reduction (2026-09-07): the confirm-time notice and the feed-day reminder.
    'feed_sale_reduce'::text,
    'feed_sale_reduce_reminder'::text
  ]))) NOT VALID;

-- seed-migration-guard:ignore owner=manohark issue=sale-feed-reduce-notify reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
ALTER TABLE notification_requests
  VALIDATE CONSTRAINT notification_requests_type_check;

-- The feed-day reminder rides the shared 5-minute cadence and reads the last few days of confirms
-- each tick. That read is keyed on (tenant, allocated_at) over live rows; without this index it is a
-- per-tick scan of every tagging ever recorded, which grows with every sale the farm makes.
CREATE INDEX IF NOT EXISTS goat_sale_allocations_recent_tagged_idx
    ON goat_sale_allocations (tenant_id, allocated_at DESC)
    WHERE status = 'tagged';

-- +goose Down

SET lock_timeout = '5s';

DROP INDEX IF EXISTS goat_sale_allocations_recent_tagged_idx;

-- Rows written under the two added types would violate the narrowed enum; a queued notification is
-- transient work, not history (delivery is logged separately), so they are removed first.
-- seed-migration-guard:ignore owner=manohark issue=sale-feed-reduce-notify reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
DELETE FROM notification_requests
WHERE notification_type IN ('feed_sale_reduce', 'feed_sale_reduce_reminder');

-- seed-migration-guard:ignore owner=manohark issue=sale-feed-reduce-notify reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
ALTER TABLE notification_requests
  DROP CONSTRAINT IF EXISTS notification_requests_type_check;

-- seed-migration-guard:ignore owner=manohark issue=sale-feed-reduce-notify reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
ALTER TABLE notification_requests
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
    'feed_proof_times_daily'::text
  ]))) NOT VALID;

-- seed-migration-guard:ignore owner=manohark issue=sale-feed-reduce-notify reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
ALTER TABLE notification_requests
  VALIDATE CONSTRAINT notification_requests_type_check;
