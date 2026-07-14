-- +goose Up
/*
U5 (operational-kernel-5k-50k-scale-envelope ADR, step 5): repoint the two foreign keys that
currently reference calendar_event_projections so that table can be dropped later (U7) without
notification_requests/calendar_snoozes losing referential coverage.

Why DROP rather than repoint onto a single canonical table (obligation_instances /
obligation_batches / sop_tasks): calendar_event_projections.event_id is a heterogeneous text key
with six construction namespaces (see backend/internal/calendar/adapters/postgres/repository.go,
calendarVaccinationProjectionRefreshSQL CTEs):
  'obligation:<obligation_id>'        -> 1:1 obligation_instances.obligation_id
  'batch:<batch_id>'                  -> 1:1 obligation_batches.batch_id
  'calendar:<sop_task_id>'            -> 1:1 sop_tasks.task_id
  'calendar:<protocol_version_id>'    -> 1:1 protocol_versions.protocol_version_id
  'catchup:park:<park_id>:due:<day>'  -> a GROUP BY (park_id, due_day) over many
                                         obligation_instances; no single canonical row
  'parkdrive:park:<park_id>:date:<d>' -> an aggregation of many batch/catchup rows for a
                                         (park_id, due_day); no single canonical row
notification_requests.calendar_event_id and calendar_snoozes.calendar_event_id carry this same
mixed key, so a single new FK to one canonical table cannot be added without rejecting the
catchup/park_drive rows that are a normal, currently-live part of the reminder/escalation flow
(see repository.go selectDueReminderEvents/queueDueReminder, which explicitly targets
event_type <> 'vaccination_dose_due', i.e. exactly the drive-shaped catchup/park_drive/batch
events). Per the ADR (docs/decisions/operational-kernel-5k-50k-scale-envelope.md, "Projection
tables removed in the clean restructure"): "Any foreign keys from notification/snooze tables to
calendar_event_projections must be replaced with references to canonical source work ... before
the table is dropped" -- and the recovery section's own fallback language ("if a calendar_event_id
does not cleanly map 1:1 to a canonical id ... drop the projection FK and enforce the relationship
in app code instead") is the option this migration takes.

Enforcement moves to app code: every writer of notification_requests.calendar_event_id /
calendar_snoozes.calendar_event_id already re-selects (FOR UPDATE SKIP LOCKED, in the same
transaction as the insert) the calendar_event_projections/canonical row it targets before
inserting -- see queueDueReminder, queueEscalation, SendNudge, and Snooze in
backend/internal/calendar/adapters/postgres/repository.go. The one caller that did not
(QueueRoleNotifications, used by backend/internal/notificationbridge for verification-item
notifications) never had a real calendar_event_projections row to begin with for its
'verification:<item_id>' key -- see the FK fixture rows the notificationbridge integration/unit
tests had to hand-insert purely to satisfy this constraint. Dropping the FK removes that
test-only fixture requirement without changing any production behavior.

Lock safety: DROP CONSTRAINT is a metadata-only change (no table rewrite, no full scan); it takes
a brief ACCESS EXCLUSIVE lock on the table itself, not a long-held one, so this runs in the normal
transactional migration mode.

No seed-path impact: this migration does not add, rename, or change the shape of any column a seed
command writes -- it only removes a referential-integrity constraint whose target
(calendar_event_projections) is itself a derived projection table, not seed source-of-truth. See
the seed-migration-guard:ignore marker below.
*/

ALTER TABLE notification_requests DROP CONSTRAINT IF EXISTS notification_requests_event_fk; -- seed-migration-guard:ignore owner=ravi issue=U5-kernel-adr reason=fk-drop-no-seed-shape-change expiry=2027-01-01

ALTER TABLE calendar_snoozes DROP CONSTRAINT IF EXISTS calendar_snoozes_event_fk;

-- +goose Down
/*
Reversal per the ADR's recovery section: re-point the notification/snooze foreign keys back to
calendar_event_projections. This assumes the projection table (and its rows) still exist; if U7
already dropped calendar_event_projections, restore that table/data first.
*/

-- seed-migration-guard:ignore owner=ravi issue=U5-kernel-adr reason=fk-drop-no-seed-shape-change expiry=2027-01-01
ALTER TABLE notification_requests
  ADD CONSTRAINT notification_requests_event_fk FOREIGN KEY (tenant_id, calendar_event_id)
    REFERENCES calendar_event_projections(tenant_id, event_id);

ALTER TABLE calendar_snoozes
  ADD CONSTRAINT calendar_snoozes_event_fk FOREIGN KEY (tenant_id, calendar_event_id)
    REFERENCES calendar_event_projections(tenant_id, event_id);
