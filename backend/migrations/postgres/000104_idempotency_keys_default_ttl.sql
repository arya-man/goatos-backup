-- +goose Up
UPDATE idempotency_keys
SET expires_at = COALESCE(completed_at, first_seen_at, now()) + interval '7 days'
WHERE expires_at IS NULL;

ALTER TABLE idempotency_keys
  ALTER COLUMN expires_at SET DEFAULT (now() + interval '7 days');

-- +goose Down
ALTER TABLE idempotency_keys
  ALTER COLUMN expires_at DROP DEFAULT;
