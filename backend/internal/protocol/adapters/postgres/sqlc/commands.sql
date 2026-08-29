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
-- Status flip only; executable-contract checks are enforced in the app layer.
UPDATE protocol_versions
SET status = 'published',
    published_by = @published_by,
    published_at = now(),
    row_version = row_version + 1,
    updated_at = now()
WHERE tenant_id = @tenant_id
  AND protocol_version_id = @protocol_version_id
  AND status = 'draft';

-- name: DeleteDraftProtocolRules :execrows
-- protocol_rules_version_tenant_fk has NO ON DELETE CASCADE, so a version row cannot
-- be deleted while rules reference it. They are removed explicitly, in the same
-- transaction as the version, and only ever for a DRAFT.
DELETE FROM protocol_rules r
WHERE r.tenant_id = @tenant_id
  AND r.protocol_version_id = @protocol_version_id
  AND EXISTS (
    SELECT 1 FROM protocol_versions v
    WHERE v.tenant_id = @tenant_id
      AND v.protocol_version_id = @protocol_version_id
      AND v.status = 'draft'
  );

-- name: DeleteDraftProtocolTriggers :execrows
-- Same reason as the rules above: protocol_triggers_version_tenant_fk does not cascade.
DELETE FROM protocol_triggers t
WHERE t.tenant_id = @tenant_id
  AND t.protocol_version_id = @protocol_version_id
  AND EXISTS (
    SELECT 1 FROM protocol_versions v
    WHERE v.tenant_id = @tenant_id
      AND v.protocol_version_id = @protocol_version_id
      AND v.status = 'draft'
  );

-- name: DiscardProtocolVersion :execrows
-- Deletes a DRAFT version. Its rules and triggers must already be gone (see the two
-- statements above) because neither foreign key cascades. The status predicate is the
-- safety property: a published or retired version can never be removed by this
-- statement, so history stays complete no matter what id is supplied. A draft has
-- never reached the field -- no obligation references it -- so deleting it destroys
-- only unpublished authoring work.
DELETE FROM protocol_versions
WHERE tenant_id = @tenant_id
  AND protocol_version_id = @protocol_version_id
  AND status = 'draft';
