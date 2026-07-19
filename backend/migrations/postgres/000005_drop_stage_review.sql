-- +goose Up
-- no-mismatch-review-queue:ignore: owner=ravi issue=goatos-r50-closure scope=legacy_stage_review_feature_removal expiry=2026-08-31
DROP TABLE IF EXISTS public.vaccination_stage_review_items CASCADE;

-- +goose Down
-- Removal is one-way; no down-migration available for this table.
