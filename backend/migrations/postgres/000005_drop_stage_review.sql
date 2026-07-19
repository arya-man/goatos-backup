-- +goose Up
DROP TABLE IF EXISTS public.vaccination_stage_review_items CASCADE;

-- +goose Down
-- Removal is one-way; no down-migration available for this table.
