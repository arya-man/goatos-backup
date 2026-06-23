-- +goose Up
-- Phase 1A · SOP verify fan-out. Index the completion->submission-item link so resolving a verified
-- SOP task's recorded completions (vaccination_completions JOIN sop_submission_items) uses an indexed
-- path on the large completions table instead of a scan. Partial: only completions tied to a SOP item.
CREATE INDEX vaccination_completions_submission_item_idx
  ON vaccination_completions (tenant_id, sop_submission_item_id)
  WHERE sop_submission_item_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS vaccination_completions_submission_item_idx;
