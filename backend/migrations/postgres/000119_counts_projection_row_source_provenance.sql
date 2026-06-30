-- +goose Up
-- Feed Direction G2: row-level projection provenance required by the
-- CountProjectionProvider contract.

CREATE UNIQUE INDEX count_base_anchors_tenant_id_unique
  ON count_base_anchors (tenant_id, base_count_anchor_id);

ALTER TABLE count_projection_snapshot_rows
  ADD COLUMN base_count_anchor_id uuid NULL,
  ADD COLUMN included_shifting_event_ids_hash text NOT NULL DEFAULT 'no-shifting-events';

ALTER TABLE count_projection_snapshot_rows
  ADD CONSTRAINT count_projection_snapshot_rows_tenant_anchor_fk
    FOREIGN KEY (tenant_id, base_count_anchor_id)
    REFERENCES count_base_anchors (tenant_id, base_count_anchor_id),
  ADD CONSTRAINT count_projection_snapshot_rows_included_shift_hash_check
    CHECK (btrim(included_shifting_event_ids_hash) <> '');

CREATE INDEX count_projection_snapshot_rows_anchor_idx
  ON count_projection_snapshot_rows (tenant_id, base_count_anchor_id)
  WHERE base_count_anchor_id IS NOT NULL;

CREATE INDEX count_projection_snapshot_rows_resolution_feed_idx
  ON count_projection_snapshot_rows (
    tenant_id,
    target_date,
    park_id,
    ration_context_resolution_state,
    shed_id,
    lower(breed_key),
    count_projection_snapshot_row_id
  );

ALTER TABLE count_projection_exceptions
  DROP CONSTRAINT count_projection_exceptions_type_check,
  ADD CONSTRAINT count_projection_exceptions_type_check CHECK (
    exception_type IN (
      'missing_base_count',
      'missing_structured_impact',
      'unreported_shifting',
      'count_mismatch',
      'alias_conflict',
      'ration_context_unresolved',
      'destination_shortage',
      'unsafe_surplus',
      'query_plan_unproven',
      'missing_projection_snapshot',
      'stale_projection'
    )
  );

-- +goose Down
ALTER TABLE count_projection_exceptions
  DROP CONSTRAINT count_projection_exceptions_type_check,
  ADD CONSTRAINT count_projection_exceptions_type_check CHECK (
    exception_type IN (
      'missing_base_count',
      'missing_structured_impact',
      'unreported_shifting',
      'count_mismatch',
      'alias_conflict',
      'ration_context_unresolved',
      'destination_shortage',
      'unsafe_surplus',
      'query_plan_unproven'
    )
  );

DROP INDEX IF EXISTS count_projection_snapshot_rows_resolution_feed_idx;
DROP INDEX IF EXISTS count_projection_snapshot_rows_anchor_idx;

ALTER TABLE count_projection_snapshot_rows
  DROP CONSTRAINT IF EXISTS count_projection_snapshot_rows_included_shift_hash_check,
  DROP CONSTRAINT IF EXISTS count_projection_snapshot_rows_tenant_anchor_fk,
  DROP COLUMN IF EXISTS included_shifting_event_ids_hash,
  DROP COLUMN IF EXISTS base_count_anchor_id;

DROP INDEX IF EXISTS count_base_anchors_tenant_id_unique;
