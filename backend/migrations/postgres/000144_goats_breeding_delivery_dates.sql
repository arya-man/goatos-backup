-- +goose Up
-- Pregnancy timing facts. Without these, pregnancyDeferReason
-- (internal/vaccination/app/schedule_policy.go) can only emit
-- "pregnancy_month_review" and post-delivery catch-up never fires. Nullable adds
-- on a hot table: no rewrite, no long lock. Read per-goat (goat_id already
-- filtered by the eligible-goat query), so no new index is required.
ALTER TABLE goats
  ADD COLUMN IF NOT EXISTS breeding_date date NULL,
  ADD COLUMN IF NOT EXISTS last_delivery_date date NULL;

-- +goose Down
ALTER TABLE goats
  DROP COLUMN IF EXISTS last_delivery_date,
  DROP COLUMN IF EXISTS breeding_date;
