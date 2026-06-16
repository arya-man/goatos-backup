-- +goose Up
-- Add row_version for optimistic concurrency on import row review actions.
-- Default 1 covers all existing rows; forward-only, never edit applied migrations.
ALTER TABLE legacy_import_rows
  ADD COLUMN IF NOT EXISTS row_version int NOT NULL DEFAULT 1;

COMMENT ON COLUMN legacy_import_rows.row_version IS
  'Optimistic concurrency guard for import row review actions (reject/fix/reapply). Bumped on every state transition.';

-- +goose Down
ALTER TABLE legacy_import_rows DROP COLUMN IF EXISTS row_version;
