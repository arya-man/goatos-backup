-- +goose Up
-- A phone can be registered, active, and holding a live FCM token and still show the person
-- nothing: on Android 13+ the OS notification permission defaults to DENIED, and a person can
-- also switch our notifications off later in system settings. FCM accepts the send, reports it
-- delivered, and the OS drops it on the floor. Until now the backend had no way to tell those
-- two cases apart, so every dropped push was counted as delivered.
--
-- notifications_enabled is what the phone itself reports on device register and on every
-- heartbeat (NotificationManagerCompat.areNotificationsEnabled()):
--   true  -> the phone will show what we send
--   false -> push-muted; sending is a lie, do not address it and do not count it as delivered
--   NULL  -> not reported yet (older app build, never heartbeated). Treated as reachable, so an
--            app that predates this column keeps receiving exactly as before.
--
-- Nullable, no default, no backfill: adding a nullable column without a default is a catalog-only
-- change in Postgres (no table rewrite, no long ACCESS EXCLUSIVE hold) and the three-state
-- true/false/unknown is the honest model -- defaulting to true would assert something about every
-- existing row that nobody has actually observed.
ALTER TABLE workforce_member_devices
  ADD COLUMN IF NOT EXISTS notifications_enabled boolean;

COMMENT ON COLUMN workforce_member_devices.notifications_enabled IS
  'Phone-reported OS notification switch (register/heartbeat). false = push-muted, do not address; NULL = never reported, treat as reachable.';

-- Push fan-out reads active devices with a token and now also skips the push-muted ones. Partial
-- index keeps that predicate cheap on the existing member/status lookup path.
CREATE INDEX IF NOT EXISTS workforce_member_devices_push_reachable_idx
  ON workforce_member_devices (tenant_id, workforce_member_id)
  WHERE status = 'active'
    AND fcm_token IS NOT NULL
    AND notifications_enabled IS DISTINCT FROM false;

-- +goose Down
DROP INDEX IF EXISTS workforce_member_devices_push_reachable_idx;
ALTER TABLE workforce_member_devices
  DROP COLUMN IF EXISTS notifications_enabled;
