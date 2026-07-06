-- +goose Up
-- Surface per-goat generation failures separately from whole-run failures. A single poison goat
-- must not abort a 1M-goat repair pass, but operators still need a durable counter to alert on.
-- Do not rewrite the hot-table counts CHECK here; the post-floor hot-table guard rejects direct
-- constraint churn on populated operational tables.

ALTER TABLE vaccination_generation_runs
  ADD COLUMN failed_goat_count int NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE vaccination_generation_runs
  DROP COLUMN IF EXISTS failed_goat_count;
