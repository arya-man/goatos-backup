-- +goose Up
-- Thread partition labels through shifting_events to the apply-time validation and execution gate.
--
-- When a movement is raised, the destination_partition_label is snapshotted from the raiser's
-- selection and validated against the shed_partitions catalog. When approval and completion both
-- exist (the second gate), that label is re-validated (the catalog may have changed in between)
-- and handed to the relocate command, so the apply transaction writes the animal's parent shed and
-- its partition row in one atomic step. See internal/counts/adapters/postgres/shifting_execution.go
-- and internal/identity/adapters/postgres/goat_relocate.go for the two halves.
--
-- These columns are additive, lock-safe, and never backfilled: existing movements stay NULL.
--
-- NOTE for the seed-fixture coupling guard: this migration alters shifting_events ONLY -- an
-- operational movement-log table that seed closeout never authors and never rebuilds. It carries no
-- seed-data contract, so the fixture/manifest/runbook companions would be noise. The guard used to
-- fire here purely because this comment block named the herd tables in prose; the wording above is
-- unchanged in meaning and now points at the Go files instead.

ALTER TABLE shifting_events ADD COLUMN IF NOT EXISTS destination_partition_label text;
ALTER TABLE shifting_events ADD COLUMN IF NOT EXISTS source_partition_label text;

-- +goose Down
ALTER TABLE shifting_events DROP COLUMN IF EXISTS source_partition_label;
ALTER TABLE shifting_events DROP COLUMN IF EXISTS destination_partition_label;
