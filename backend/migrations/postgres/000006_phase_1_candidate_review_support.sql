-- +goose Up
ALTER TABLE identity_match_candidates
  ADD COLUMN row_version integer NOT NULL DEFAULT 1,
  ADD CONSTRAINT identity_match_candidates_row_version_check CHECK (row_version >= 1);

CREATE INDEX identity_match_candidates_actionable_queue_idx
  ON identity_match_candidates(tenant_id, created_at DESC, candidate_id DESC)
  WHERE state IN ('proposed', 'needs_review');

-- +goose Down
DROP INDEX IF EXISTS identity_match_candidates_actionable_queue_idx;

ALTER TABLE identity_match_candidates
  DROP CONSTRAINT IF EXISTS identity_match_candidates_row_version_check,
  DROP COLUMN IF EXISTS row_version;
