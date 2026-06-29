-- +goose Up
-- Phase 1B · Feed Direction module.
--
-- DESIGN: feed direction reuses the generic Phase 0/1A engine instead of parallel tables —
--   * config:     protocol_* with category='feed_direction' (the source-backed publish gate is generic;
--                 ration policy lives in the version rule_dsl, draft until source-backed).
--   * directions: obligation_instances with target_type='shed' (per-shed/per-ration directions, not
--                 per-goat doses) + scope shed/park; SM-4 sweep groups them into drive batches.
--   * stock:      inventory_* with feed items (inventory_items.category='feed'); reserve/consume via
--                 the inventory app.
-- The only feed-specific table is the per-shed execution + verification record below (analog to
-- vaccination_completions, but shed-scoped and quantity-fed). All cross-table refs use tenant-safe
-- composite (tenant_id, *) FKs. NO ration/feed values are seeded — structure only.

-- Register feed as a covered feature module.
ALTER TABLE feature_coverage_registry DROP CONSTRAINT IF EXISTS feature_coverage_registry_module_check;
ALTER TABLE feature_coverage_registry
  ADD CONSTRAINT feature_coverage_registry_module_check
    CHECK (feature_module IN ('counts', 'mortality', 'locations', 'vaccination', 'feed'));

CREATE TABLE feed_direction_completions (
  completion_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  obligation_id uuid NOT NULL,
  batch_id uuid NULL,
  shed_id uuid NOT NULL,
  ration_protocol_version_id uuid NULL, -- the applied feed/ration protocol version; no FK (engine-set)
  sop_submission_item_id uuid NULL,
  feed_inventory_lot_id uuid NULL,
  quantity_fed numeric NULL,
  quantity_unit text NULL,
  head_count int NULL, -- goats present in the shed for this direction
  fed_at timestamptz NOT NULL,
  status text NOT NULL DEFAULT 'recorded',
  verified_by uuid NULL,
  verified_at timestamptz NULL,
  rejection_reason text NULL,
  recorded_by uuid NULL,
  idempotency_key text NOT NULL,
  row_version int NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT feed_direction_completions_status_check CHECK (status IN ('recorded', 'accepted', 'rejected', 'reversed')),
  CONSTRAINT feed_direction_completions_quantity_check CHECK (quantity_fed IS NULL OR quantity_fed > 0),
  CONSTRAINT feed_direction_completions_head_count_check CHECK (head_count IS NULL OR head_count >= 0),
  CONSTRAINT feed_direction_completions_row_version_check CHECK (row_version >= 1),
  -- Double-submit guard: one execution per shed direction (obligation).
  CONSTRAINT feed_direction_completions_obligation_unique UNIQUE (tenant_id, obligation_id),
  CONSTRAINT feed_direction_completions_idempotency_unique UNIQUE (tenant_id, idempotency_key),
  CONSTRAINT feed_direction_completions_obligation_tenant_fk FOREIGN KEY (tenant_id, obligation_id) REFERENCES obligation_instances(tenant_id, obligation_id),
  CONSTRAINT feed_direction_completions_batch_tenant_fk FOREIGN KEY (tenant_id, batch_id) REFERENCES obligation_batches(tenant_id, batch_id),
  CONSTRAINT feed_direction_completions_shed_tenant_fk FOREIGN KEY (tenant_id, shed_id) REFERENCES locations(tenant_id, location_id),
  CONSTRAINT feed_direction_completions_submission_item_tenant_fk FOREIGN KEY (tenant_id, sop_submission_item_id) REFERENCES sop_submission_items(tenant_id, item_id),
  CONSTRAINT feed_direction_completions_lot_tenant_fk FOREIGN KEY (tenant_id, feed_inventory_lot_id) REFERENCES inventory_stock(tenant_id, stock_id)
);

-- Per-shed feed history (most-recent first) + per-obligation / per-batch lookups.
CREATE INDEX feed_direction_completions_shed_history_idx ON feed_direction_completions(tenant_id, shed_id, fed_at DESC);
CREATE INDEX feed_direction_completions_obligation_idx ON feed_direction_completions(tenant_id, obligation_id);
CREATE INDEX feed_direction_completions_batch_idx ON feed_direction_completions(tenant_id, batch_id, status);
-- Verification queue: directions awaiting review (status='recorded'), earliest fed first.
CREATE INDEX feed_direction_completions_review_idx
  ON feed_direction_completions (tenant_id, fed_at)
  WHERE status = 'recorded';

-- Feed direction SOP execution/proof skeleton (structure only — NO ration values).
INSERT INTO sop_definitions (sop_id, tenant_id, code, name, description, status)
VALUES ('b0000000-0000-4000-8000-000000000003', '00000000-0000-4000-8000-000000000001',
        'feed.direction', 'Feed direction', 'Shed feed direction execution + proof skeleton.', 'draft')
ON CONFLICT (tenant_id, code) DO NOTHING;

INSERT INTO sop_versions (sop_version_id, tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy)
VALUES ('b0000000-0000-4000-8000-000000000004', '00000000-0000-4000-8000-000000000001',
        'b0000000-0000-4000-8000-000000000003', 1, 'v1-skeleton', 'draft',
        '{"steps":[{"key":"shed_feed_video","type":"video","label":"Shed feed video"},{"key":"feed_lot","type":"scan","label":"Feed lot proof"},{"key":"quantity_fed","type":"number","label":"Quantity fed"},{"key":"head_count","type":"number","label":"Head count"},{"key":"fed_at","type":"datetime","label":"Fed at"},{"key":"est_vs_used","type":"number","label":"Estimated vs used quantity"},{"key":"verifier_review","type":"review","label":"Verifier review"}]}'::jsonb,
        '{"required":["shed_feed_video","feed_lot","quantity_fed"],"verify_capability":"proof.verify"}'::jsonb)
ON CONFLICT DO NOTHING;

-- +goose Down
DROP INDEX IF EXISTS feed_direction_completions_review_idx;
DROP INDEX IF EXISTS feed_direction_completions_batch_idx;
DROP INDEX IF EXISTS feed_direction_completions_obligation_idx;
DROP INDEX IF EXISTS feed_direction_completions_shed_history_idx;
DROP TABLE IF EXISTS feed_direction_completions;
DELETE FROM sop_versions WHERE sop_version_id = 'b0000000-0000-4000-8000-000000000004';
DELETE FROM sop_definitions WHERE sop_id = 'b0000000-0000-4000-8000-000000000003';
ALTER TABLE feature_coverage_registry DROP CONSTRAINT IF EXISTS feature_coverage_registry_module_check;
ALTER TABLE feature_coverage_registry
  ADD CONSTRAINT feature_coverage_registry_module_check
    CHECK (feature_module IN ('counts', 'mortality', 'locations', 'vaccination'));
