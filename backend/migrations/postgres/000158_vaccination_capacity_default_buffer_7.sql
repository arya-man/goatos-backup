-- +goose Up
-- Business-rule change (2026-07-11): default max_buffer_days for vaccination capacity is 7 (was 3).
-- Capacity is now authored inside the versioned rule (rule_dsl.capacity) and synced into
-- vaccination_capacity_config on publish; that table is a DERIVED operational read model. This bumps
-- any row still at the old seeded default so staging seed is coherent BEFORE the first publish. Rows an
-- operator's publish has already set to a deliberate value are left untouched (only the old 3 default).
UPDATE vaccination_capacity_config
SET max_buffer_days = 7,
    updated_at = now(),
    row_version = row_version + 1
WHERE max_buffer_days = 3;

-- +goose Down
-- No-op: the pre-7 seed default is not restorable per-row without provenance, and 7 is the current
-- business rule. Down leaves rows at 7.
SELECT 1;
