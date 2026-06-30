-- +goose Up
-- Feed Direction G2 / CSG10: index paths for bounded stale/imported Base Count
-- mismatch scans. The worker pages by tenant + counted_at + anchor cursor, with
-- optional park/shed filters, and must not degrade into a full anchor scan at
-- million-goat scale.

CREATE INDEX count_base_anchors_mismatch_scan_idx
  ON count_base_anchors (tenant_id, counted_at, base_count_anchor_id)
  WHERE anchor_state = 'adopted'
    AND discrepancy_state <> 'resolved';

CREATE INDEX count_base_anchors_mismatch_scan_scope_idx
  ON count_base_anchors (tenant_id, park_id, shed_id, counted_at, base_count_anchor_id)
  WHERE anchor_state = 'adopted'
    AND discrepancy_state <> 'resolved';

-- +goose Down
DROP INDEX IF EXISTS count_base_anchors_mismatch_scan_scope_idx;
DROP INDEX IF EXISTS count_base_anchors_mismatch_scan_idx;
