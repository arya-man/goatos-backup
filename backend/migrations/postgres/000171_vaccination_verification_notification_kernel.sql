-- +goose Up
-- +goose StatementBegin
-- Vaccination verification/rework notification kernel (docs/decisions/vaccination-notification-rules.md
-- §4c): wires the missing "who gets a push when a proof is submitted / rejected" seam. Two lock-safe,
-- additive schema changes:
--
-- 1. workforce_member_devices.fcm_token: the RAW FCM registration token the push gateway needs as
--    message.token (internal/notification/adapters/gateway/gateway.go setFCMTarget). push_token_hash
--    stays as the existing identity/dedup hash; fcm_token is the new delivery ADDRESS, nullable-only
--    (ADD COLUMN, no default, no rewrite -- lock-safe on this small workforce table).
-- 2. notification_requests_type_check: adds 'verification_pending' and 'rework' to the allowed
--    notification_type values (was only 'reminder', 'nudge', 'escalation'). The CHECK only ADDS
--    allowed values, so every existing row already satisfies it -- added NOT VALID (brief
--    catalog-only ACCESS EXCLUSIVE, no table scan) then VALIDATE CONSTRAINT (SHARE UPDATE EXCLUSIVE,
--    concurrent reads/writes allowed), mirroring 000166's pattern for the same reason.
ALTER TABLE workforce_member_devices
  ADD COLUMN IF NOT EXISTS fcm_token text NULL;

ALTER TABLE notification_requests
  DROP CONSTRAINT IF EXISTS notification_requests_type_check;

ALTER TABLE notification_requests
  ADD CONSTRAINT notification_requests_type_check
  CHECK (notification_type IN ('reminder', 'nudge', 'escalation', 'verification_pending', 'rework'))
  NOT VALID;

ALTER TABLE notification_requests
  VALIDATE CONSTRAINT notification_requests_type_check;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE notification_requests
  DROP CONSTRAINT IF EXISTS notification_requests_type_check;

ALTER TABLE notification_requests
  ADD CONSTRAINT notification_requests_type_check
  CHECK (notification_type IN ('reminder', 'nudge', 'escalation'))
  NOT VALID;

ALTER TABLE notification_requests
  VALIDATE CONSTRAINT notification_requests_type_check;

ALTER TABLE workforce_member_devices
  DROP COLUMN IF EXISTS fcm_token;
-- +goose StatementEnd
