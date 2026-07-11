-- +goose Up
-- +goose NO TRANSACTION
-- Calendar month/history reads are bounded by administered_at and accepted
-- status. Keep that path index-first at million-animal scale without retaining
-- every closed dose in the hot calendar projection.
CREATE INDEX CONCURRENTLY IF NOT EXISTS vaccination_completions_accepted_history_calendar_idx
  ON vaccination_completions (tenant_id, administered_at, obligation_id)
  WHERE status = 'accepted';

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS vaccination_completions_accepted_history_calendar_idx;
