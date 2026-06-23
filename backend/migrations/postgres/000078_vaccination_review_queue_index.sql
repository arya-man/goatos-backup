-- +goose Up
-- Phase 1A · Verification queue. Index completions awaiting review (status='recorded') by administered
-- time so the queue read is an indexed, ordered scan on the large completions table. Partial: only
-- rows still awaiting review (the queue shrinks as they are accepted/rejected).
CREATE INDEX vaccination_completions_review_idx
  ON vaccination_completions (tenant_id, administered_at)
  WHERE status = 'recorded';

-- +goose Down
DROP INDEX IF EXISTS vaccination_completions_review_idx;
