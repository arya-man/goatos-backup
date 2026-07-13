-- +goose Up
-- +goose StatementBegin
-- Vaccination reminder cadence ladder (docs/decisions/vaccination-notification-rules.md §3): T-7
-- advance notice, T-6..T-1 daily 2x reminders, T-0 due-today 2x. Two additive changes:
--
-- 1. notification_requests_type_check: adds 'advance_notice' and 'due_today' to the allowed
--    notification_type values (was 'reminder', 'nudge', 'escalation', 'verification_pending',
--    'rework' -- 'reminder' already covers the daily rungs, these two cover the T-7 and T-0 rungs).
--    Same lock-safe shape as 000166/000179: DROP is catalog-only (brief ACCESS EXCLUSIVE, no table
--    scan), re-add is NOT VALID (catalog-only) then VALIDATE CONSTRAINT (SHARE UPDATE EXCLUSIVE,
--    concurrent reads/writes allowed).
-- 2. vaccination_reminder_cadence_fires: a brand-new, currently-empty table (CREATE TABLE never
--    blocks any other table) tracking which (park, fire day, notification type, slot) cadence
--    batches have already been queued, so the reminder sweeper's collapse/idempotency check does not
--    have to reverse-engineer per-recipient notification_requests.idempotency_key (which is
--    device-scoped, not batch-scoped). Claimed via INSERT ... ON CONFLICT DO NOTHING so two
--    concurrent sweeper runs can never double-fire the same batch.
ALTER TABLE notification_requests
  DROP CONSTRAINT IF EXISTS notification_requests_type_check;

ALTER TABLE notification_requests
  ADD CONSTRAINT notification_requests_type_check
  CHECK (notification_type IN ('reminder', 'nudge', 'escalation', 'verification_pending', 'rework', 'advance_notice', 'due_today'))
  NOT VALID;

ALTER TABLE notification_requests
  VALIDATE CONSTRAINT notification_requests_type_check;

CREATE TABLE vaccination_reminder_cadence_fires (
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  fire_key text NOT NULL,
  park_id uuid NOT NULL,
  fire_day date NOT NULL,
  notification_type text NOT NULL,
  slot text NOT NULL,
  reminder_number int NOT NULL DEFAULT 0,
  obligation_count int NOT NULL DEFAULT 0,
  representative_calendar_event_id text NOT NULL,
  queued_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, fire_key),
  CONSTRAINT vaccination_reminder_cadence_fires_type_check
    CHECK (notification_type IN ('advance_notice', 'reminder', 'due_today')),
  CONSTRAINT vaccination_reminder_cadence_fires_location_fk
    FOREIGN KEY (tenant_id, park_id) REFERENCES locations(tenant_id, location_id)
);

CREATE INDEX vaccination_reminder_cadence_fires_park_day_idx
  ON vaccination_reminder_cadence_fires (tenant_id, park_id, fire_day);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS vaccination_reminder_cadence_fires;

ALTER TABLE notification_requests
  DROP CONSTRAINT IF EXISTS notification_requests_type_check;

ALTER TABLE notification_requests
  ADD CONSTRAINT notification_requests_type_check
  CHECK (notification_type IN ('reminder', 'nudge', 'escalation', 'verification_pending', 'rework'))
  NOT VALID;

ALTER TABLE notification_requests
  VALIDATE CONSTRAINT notification_requests_type_check;
-- +goose StatementEnd
