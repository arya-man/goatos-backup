-- +goose Up
-- Thread partition labels through shifting_events to the apply-time validation and execution gate.
--
-- When a movement is raised, the destination_partition_label is snapshotted from the raiser's
-- selection and validated against the shed_partitions catalog. When approval and completion both
-- exist (the second gate), that label is re-validated (catalog may have changed) and passed to
-- the RelocateGoatsCommand so the apply transaction can update goat_shed_partitions.partition_label
-- atomically with goats.shed_id.
--
-- These columns are additive, lock-safe, and never backfilled: existing movements stay NULL.

ALTER TABLE shifting_events ADD COLUMN IF NOT EXISTS destination_partition_label text;
ALTER TABLE shifting_events ADD COLUMN IF NOT EXISTS source_partition_label text;

-- +goose Down
ALTER TABLE shifting_events DROP COLUMN IF EXISTS source_partition_label;
ALTER TABLE shifting_events DROP COLUMN IF EXISTS destination_partition_label;
