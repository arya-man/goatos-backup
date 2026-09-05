-- +goose Up
-- notification_requests.notification_type IS A CLOSED ENUM, AND THREE DAILY ALERTS ARE OUTSIDE IT.
--
-- The CHECK was last written in the edited baseline with nine values, all of them belonging to the
-- vaccination cadence the table was built for: reminder, nudge, escalation, the four verification
-- states, advance_notice, due_today. Every notifier written since has had to pick one of those or
-- add its own -- and three chose their own without widening the constraint:
--
--   obligation_missed          notificationbridge/obligation_missed_notify.go
--   feed_low_stock             notificationbridge/feed_low_stock_notify.go   (2026-08-24)
--   procurement_load_overdue   notificationbridge/load_age_notify.go         (2026-09-01)
--   feed_proof_times_daily     notificationbridge/feed_proof_times_notify.go (2026-09-05)
--
-- The first three are ALREADY LIVE and their INSERT can only have been failing 23514 on every tick.
-- That failure is quiet in exactly the way that matters: the notifier returns an error into a
-- cadence log, nothing is queued, and the alert simply never arrives -- which is indistinguishable
-- from "there was nothing to alert about". A daily alert that has never once fired looks identical
-- to a farm with no low stock and no overdue load.
--
-- So this widens the enum for all four in one statement. Fixing only the new one would leave the
-- constraint still rejecting three shipped notifiers, which is not a smaller change -- it is the
-- same change with three known defects deliberately left in.
--
-- The set was not assembled by reading the notifiers; it is what
-- TestEveryNotificationTypeIsAllowedByTheCheckConstraint reports, which is why obligation_missed --
-- missed by eye twice -- is in it.
--
-- WHY THE ENUM IS KEPT AT ALL rather than dropped: the value is the join key every recipient-routing
-- and digest read groups by, and a typo'd type is a notification that silently addresses nobody. A
-- closed set turns that into a write-time failure. The cost is this migration, once per new alert,
-- which is the intended trade.
--
-- Lock-safe on a hot table, mirroring the baseline's own repair of this same constraint: bounded
-- lock_timeout so a contended DROP/ADD fails fast and is retried on the next migration run rather
-- than hanging behind a long reader, then ADD ... NOT VALID (which takes no full-table scan under
-- the lock) followed by a separate VALIDATE. Widening an enum cannot invalidate an existing row --
-- every value already stored is still permitted -- so the VALIDATE is a formality that keeps the
-- constraint trusted by the planner.
-- NO SEED IMPACT, and this was checked rather than assumed: no backend/cmd/seed-* command and no
-- seed-closeout step writes notification_requests at all. Its only writers are runtime paths (the
-- calendar repository's queue writes) and tests. Widening an enum also cannot invalidate a stored
-- row, so there is no backfill and nothing for a seed companion to do.
SET lock_timeout = '5s';

-- seed-migration-guard:ignore owner=manohark issue=feed-proof-times-slack-report reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
ALTER TABLE notification_requests
  DROP CONSTRAINT IF EXISTS notification_requests_type_check;

-- seed-migration-guard:ignore owner=manohark issue=feed-proof-times-slack-report reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
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
    -- Alerts raised by notificationbridge notifiers. None has a cadence/reminder shape, which is
    -- why none of the nine values above fits; the three daily ones are each queued once per
    -- business date by a notifier riding the shared operational cadence.
    'obligation_missed'::text,
    'feed_low_stock'::text,
    'procurement_load_overdue'::text,
    'feed_proof_times_daily'::text
  ]))) NOT VALID;

-- seed-migration-guard:ignore owner=manohark issue=feed-proof-times-slack-report reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
ALTER TABLE notification_requests
  VALIDATE CONSTRAINT notification_requests_type_check;

-- +goose Down
SET lock_timeout = '5s';

-- The Down narrows the enum back to the pre-alert set. Rows already written under the added
-- types would violate it, so they are removed first: a queued notification is transient work, not
-- history (the audit trail of what was actually delivered lives in the delivery log), and leaving
-- them would make the VALIDATE fail and the rollback impossible.
-- seed-migration-guard:ignore owner=manohark issue=feed-proof-times-slack-report reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
DELETE FROM notification_requests
WHERE notification_type IN ('obligation_missed', 'feed_low_stock', 'procurement_load_overdue',
                            'feed_proof_times_daily');

-- seed-migration-guard:ignore owner=manohark issue=feed-proof-times-slack-report reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
ALTER TABLE notification_requests
  DROP CONSTRAINT IF EXISTS notification_requests_type_check;

-- seed-migration-guard:ignore owner=manohark issue=feed-proof-times-slack-report reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
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
    'leadership_task_done'::text
  ]))) NOT VALID;

-- seed-migration-guard:ignore owner=manohark issue=feed-proof-times-slack-report reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
ALTER TABLE notification_requests
  VALIDATE CONSTRAINT notification_requests_type_check;
