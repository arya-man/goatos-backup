-- name: CreateProtocolDefinition :one
INSERT INTO protocol_definitions (tenant_id, code, name, category, status, created_by)
VALUES (@tenant_id, @code, @name, @category, @status, @created_by)
RETURNING protocol_id::text AS protocol_id;

-- name: CreateProtocolVersion :one
INSERT INTO protocol_versions (
  tenant_id, protocol_id, scope_type, scope_id, version, version_label, status,
  effective_from, effective_to, rule_dsl, proof_policy, sop_version_id, drafted_by
) VALUES (
  @tenant_id, @protocol_id, @scope_type, @scope_id, @version, @version_label, @status,
  @effective_from, @effective_to, @rule_dsl, @proof_policy, @sop_version_id, @drafted_by
)
RETURNING protocol_version_id::text AS protocol_version_id;

-- name: CreateProtocolRule :one
INSERT INTO protocol_rules (
  tenant_id, protocol_version_id, dose_code, "sequence", trigger_type, offset_days,
  due_window_days, min_gap_days, "repeat", repeat_until_after_age, catch_up,
  eligibility_json, sop_version_id, proof_policy, withdrawal_days, sort_order
) SELECT
  @tenant_id, @protocol_version_id, @dose_code, @sequence, @trigger_type, @offset_days,
  @due_window_days, @min_gap_days, @repeat, @repeat_until_after_age, @catch_up,
  @eligibility_json, @sop_version_id, @proof_policy, @withdrawal_days, @sort_order
FROM protocol_versions pv
WHERE pv.tenant_id = @tenant_id
  AND pv.protocol_version_id = @protocol_version_id
  AND pv.status = 'draft'
RETURNING rule_id::text AS rule_id;

-- name: CreateProtocolTrigger :one
INSERT INTO protocol_triggers (tenant_id, protocol_version_id, trigger_type, trigger_config, is_active)
SELECT @tenant_id, @protocol_version_id, @trigger_type, @trigger_config, @is_active
FROM protocol_versions pv
WHERE pv.tenant_id = @tenant_id
  AND pv.protocol_version_id = @protocol_version_id
  AND pv.status = 'draft'
RETURNING trigger_id::text AS trigger_id;

-- name: PublishProtocolVersion :execrows
-- Status flip only; the source-backed approval gate is enforced in the app layer.
UPDATE protocol_versions
SET status = 'published',
    published_by = @published_by,
    published_at = now(),
    row_version = row_version + 1,
    updated_at = now()
WHERE tenant_id = @tenant_id
  AND protocol_version_id = @protocol_version_id
  AND status = 'draft';
