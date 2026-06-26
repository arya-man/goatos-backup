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

-- name: ListActiveAnimalStages :many
-- Active animal-stage reference data for a tenant, ordered for display. Drives the Config authoring
-- stage picker (e.g. K1/K2) so stage bands live in animal_stage_lookup, NOT in frontend literals
-- (PHC vaccination TRD: stage bands must not be hardcoded). Tenant-scoped (uses the
-- (tenant_id, stage_code) unique index) and bounded by @row_limit; the lookup is inherently tiny.
SELECT
  animal_stage_id::text AS animal_stage_id,
  stage_code            AS stage_code,
  name                  AS name,
  min_age_days          AS min_age_days,
  max_age_days          AS max_age_days,
  sort_order            AS sort_order
FROM animal_stage_lookup
WHERE tenant_id = @tenant_id AND status = 'active'
ORDER BY sort_order ASC, stage_code ASC
LIMIT @row_limit::int;

-- name: ListProtocolConfigsForCategory :many
-- Config authority list (B3): every version (draft/published/retired) of every protocol definition
-- in a category for a tenant, with the rule-row count, source-review state lifted out of rule_dsl,
-- linked SOP, effective window, and publisher/updated metadata. Scoped by tenant+category and
-- bounded by @row_limit; protocol versions per tenant/category are inherently small, so no cursor.
SELECT
  pd.protocol_id::text                                          AS protocol_id,
  pd.code                                                       AS code,
  pd.name                                                       AS name,
  pd.category                                                   AS category,
  pv.protocol_version_id::text                                  AS protocol_version_id,
  pv.version                                                    AS version,
  pv.version_label                                              AS version_label,
  pv.scope_type                                                 AS scope_type,
  COALESCE(pv.scope_id::text, '')::text                         AS scope_id,
  pv.status                                                     AS status,
  pv.effective_from                                             AS effective_from,
  pv.effective_to                                               AS effective_to,
  COALESCE(pv.sop_version_id::text, '')::text                   AS sop_version_id,
  COALESCE(pv.published_by::text, '')::text                     AS published_by,
  pv.published_at                                               AS published_at,
  pv.updated_at                                                 AS updated_at,
  COALESCE(pv.rule_dsl -> 'source' ->> 'source_system', '')::text AS source_system,
  COALESCE(pv.rule_dsl -> 'source' ->> 'source_ref', '')::text    AS source_ref,
  COALESCE(pv.rule_dsl -> 'source' ->> 'review_status', '')::text AS review_status,
  COALESCE(pv.rule_dsl -> 'source' ->> 'approved_by', '')::text   AS approved_by,
  (
    SELECT COUNT(*)
    FROM protocol_rules pr
    WHERE pr.tenant_id = pv.tenant_id
      AND pr.protocol_version_id = pv.protocol_version_id
  )::int                                                        AS rule_count
FROM protocol_versions pv
JOIN protocol_definitions pd
  ON pd.tenant_id = pv.tenant_id AND pd.protocol_id = pv.protocol_id
WHERE pv.tenant_id = @tenant_id AND pd.category = @category
ORDER BY pd.code ASC, pv.version DESC, pv.protocol_version_id DESC
LIMIT @row_limit::int;
