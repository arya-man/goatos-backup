-- name: GetProtocolDefinitionByCode :one
SELECT protocol_id::text AS protocol_id, code, name, category, status, row_version
FROM protocol_definitions
WHERE tenant_id = @tenant_id AND code = @code;

-- name: GetProtocolVersion :one
SELECT protocol_version_id::text AS protocol_version_id, protocol_id::text AS protocol_id,
       scope_type, COALESCE(scope_id::text, '')::text AS scope_id, version, status,
       effective_from, effective_to, rule_dsl, proof_policy,
       COALESCE(sop_version_id::text, '')::text AS sop_version_id, row_version
FROM protocol_versions
WHERE tenant_id = @tenant_id AND protocol_version_id = @protocol_version_id;

-- name: ListPublishedVersionsForProtocol :many
SELECT protocol_version_id::text AS protocol_version_id, scope_type,
       COALESCE(scope_id::text, '')::text AS scope_id, version, effective_from, effective_to
FROM protocol_versions
WHERE tenant_id = @tenant_id AND protocol_id = @protocol_id AND status = 'published'
ORDER BY effective_from DESC, protocol_version_id DESC;

-- name: ListPublishedVaccinationVersions :many
-- Published vaccination protocol versions for a tenant (drives per-goat SM-1 on goat.created).
SELECT pv.protocol_version_id::text AS protocol_version_id
FROM protocol_versions pv
JOIN protocol_definitions pd ON pd.tenant_id = pv.tenant_id AND pd.protocol_id = pv.protocol_id
WHERE pv.tenant_id = @tenant_id AND pv.status = 'published' AND pd.category = 'vaccination'
ORDER BY pv.protocol_version_id;

-- name: ListRulesForVersion :many
SELECT rule_id::text AS rule_id, dose_code, "sequence", trigger_type, offset_days,
       due_window_days, min_gap_days, "repeat", COALESCE(repeat_until_after_age, '')::text AS repeat_until_after_age,
       catch_up, sort_order
FROM protocol_rules
WHERE tenant_id = @tenant_id AND protocol_version_id = @protocol_version_id
ORDER BY sort_order ASC, "sequence" ASC;
