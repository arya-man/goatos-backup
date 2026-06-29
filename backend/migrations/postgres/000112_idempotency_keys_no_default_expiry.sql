-- +goose Up
UPDATE idempotency_keys
SET expires_at = NULL
WHERE expires_at IS NOT NULL;

ALTER TABLE idempotency_keys
  ALTER COLUMN expires_at DROP DEFAULT;

-- +goose Down
ALTER TABLE idempotency_keys
  ALTER COLUMN expires_at SET DEFAULT (now() + interval '7 days');
