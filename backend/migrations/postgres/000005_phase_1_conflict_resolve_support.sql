-- +goose Up
ALTER TABLE identity_conflicts
  ADD COLUMN row_version integer NOT NULL DEFAULT 1,
  ADD CONSTRAINT identity_conflicts_row_version_check CHECK (row_version >= 1);

-- +goose Down
ALTER TABLE identity_conflicts
  DROP CONSTRAINT IF EXISTS identity_conflicts_row_version_check,
  DROP COLUMN IF EXISTS row_version;
