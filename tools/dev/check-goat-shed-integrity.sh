#!/usr/bin/env bash
# Post-seed/post-import DB proof for vaccination placement invariants.
# Every live goat must have a real active shed, and every open goat vaccination
# obligation must be scoped to that same shed. Park is a drive execution scope,
# not a fallback animal scope.
set -euo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
tenant_id="${GOATOS_TENANT_ID:-00000000-0000-4000-8000-000000000001}"

if [ "${1:-}" = "--self-test" ]; then
  bash -n "$0"
  grep -q "active_goat_shed_invariant" "$0"
  grep -q "vaccination_obligation_shed_scope_invariant" "$0"
  if [ -n "${DATABASE_URL:-}" ]; then
    semantic_count="$(
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -qAt <<'SQL'
BEGIN;
CREATE TEMP TABLE goats (
  tenant_id uuid,
  goat_id uuid,
  display_id text,
  lifecycle_status text,
  health_status text,
  park_id uuid,
  shed_id uuid,
  current_location_id uuid,
  merged_into_goat_id uuid
);
CREATE TEMP TABLE locations (
  tenant_id uuid,
  location_id uuid,
  parent_location_id uuid,
  location_type text,
  status text
);
CREATE TEMP TABLE goat_shed_partitions (
  tenant_id uuid,
  goat_id uuid,
  shed_id uuid,
  partition_label text
);
CREATE TEMP TABLE shed_partitions (
  tenant_id uuid,
  shed_id uuid,
  normalized_label text,
  operational_location_id uuid,
  status text
);

INSERT INTO locations VALUES
  ('00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000010', NULL, 'park', 'active'),
  ('00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000011', '00000000-0000-4000-8000-000000000010', 'shed', 'active'),
  ('00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000012', '00000000-0000-4000-8000-000000000011', 'pen', 'active'),
  ('00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000013', '00000000-0000-4000-8000-000000000011', 'pen', 'active'),
  ('00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000014', '00000000-0000-4000-8000-000000000010', 'shed', 'active'),
  ('00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000015', '00000000-0000-4000-8000-000000000014', 'pen', 'active');
INSERT INTO shed_partitions VALUES
  ('00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000011', '1', '00000000-0000-4000-8000-000000000012', 'active');
INSERT INTO goats VALUES
  ('00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000101', 'OK', 'alive', 'healthy', '00000000-0000-4000-8000-000000000010', '00000000-0000-4000-8000-000000000011', '00000000-0000-4000-8000-000000000012', NULL),
  ('00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000102', 'NULL', 'alive', 'healthy', '00000000-0000-4000-8000-000000000010', '00000000-0000-4000-8000-000000000011', NULL, NULL),
  ('00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000103', 'PARENT', 'alive', 'healthy', '00000000-0000-4000-8000-000000000010', '00000000-0000-4000-8000-000000000011', '00000000-0000-4000-8000-000000000011', NULL),
  ('00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000104', 'SIBLING', 'alive', 'healthy', '00000000-0000-4000-8000-000000000010', '00000000-0000-4000-8000-000000000011', '00000000-0000-4000-8000-000000000013', NULL),
  ('00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000105', 'OTHER', 'alive', 'healthy', '00000000-0000-4000-8000-000000000010', '00000000-0000-4000-8000-000000000011', '00000000-0000-4000-8000-000000000015', NULL);
INSERT INTO goat_shed_partitions VALUES
  ('00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000101', '00000000-0000-4000-8000-000000000011', 'Part 1'),
  ('00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000102', '00000000-0000-4000-8000-000000000011', 'Part 1'),
  ('00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000103', '00000000-0000-4000-8000-000000000011', 'Part 1'),
  ('00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000104', '00000000-0000-4000-8000-000000000011', 'Part 1'),
  ('00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000105', '00000000-0000-4000-8000-000000000011', 'Part 1');

WITH active_goat_shed_invariant AS (
  SELECT g.display_id
  FROM goats g
  LEFT JOIN locations shed
    ON shed.tenant_id = g.tenant_id
   AND shed.location_id = g.shed_id
   AND shed.location_type = 'shed'
   AND shed.status = 'active'
  LEFT JOIN locations current_loc
    ON current_loc.tenant_id = g.tenant_id
   AND current_loc.location_id = g.current_location_id
  LEFT JOIN goat_shed_partitions gsp
    ON gsp.tenant_id = g.tenant_id
   AND gsp.goat_id = g.goat_id
  LEFT JOIN shed_partitions sp
    ON sp.tenant_id = g.tenant_id
   AND sp.shed_id = g.shed_id
   AND sp.status = 'active'
   AND sp.normalized_label = regexp_replace(lower(btrim(COALESCE(gsp.partition_label, 'whole'))), '^part[[:space:]]+', '')
  LEFT JOIN locations park
    ON park.tenant_id = g.tenant_id
   AND park.location_id = g.park_id
   AND park.location_type = 'park'
   AND park.status = 'active'
  WHERE g.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
    AND g.lifecycle_status = 'alive'
    AND g.merged_into_goat_id IS NULL
    AND (
      g.shed_id IS NULL
      OR g.park_id IS NULL
      OR g.current_location_id IS NULL
      OR CASE
        WHEN regexp_replace(lower(btrim(COALESCE(gsp.partition_label, 'whole'))), '^part[[:space:]]+', '') = 'whole' THEN
          g.current_location_id IS DISTINCT FROM g.shed_id
        ELSE
          sp.operational_location_id IS NULL
          OR g.current_location_id IS DISTINCT FROM sp.operational_location_id
      END
      OR (
        sp.operational_location_id IS NOT NULL
        AND NOT (
          current_loc.location_type = 'pen'
          AND current_loc.status = 'active'
          AND current_loc.parent_location_id = g.shed_id
        )
      )
      OR shed.location_id IS NULL
      OR park.location_id IS NULL
      OR shed.parent_location_id IS DISTINCT FROM g.park_id
    )
)
SELECT count(*) FROM active_goat_shed_invariant;
ROLLBACK;
SQL
)"
    semantic_count="$(printf '%s' "$semantic_count" | tr -d '[:space:]')"
    if [ "$semantic_count" != "4" ]; then
      echo "goat-shed-integrity proof: semantic self-test failed, got $semantic_count offenders; want 4" >&2
      exit 1
    fi
  fi
  echo "goat-shed-integrity proof: self-test passed"
  exit 0
fi

if [ -z "${DATABASE_URL:-}" ]; then
  echo "goat-shed-integrity proof: DATABASE_URL is required" >&2
  exit 2
fi

offenders="$(
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -qAt \
  -v tenant_id="$tenant_id" <<'SQL'
WITH active_goat_shed_invariant AS (
  SELECT
    'active_goat_shed_invariant' AS invariant,
    COALESCE(g.display_id, g.goat_id::text) AS goat,
    COALESCE(g.lifecycle_status, '') AS lifecycle,
    COALESCE(g.health_status, '') AS health,
    COALESCE(g.park_id::text, 'missing') AS park_id,
    COALESCE(g.shed_id::text, 'missing') AS shed_id,
    COALESCE(g.current_location_id::text, 'missing') AS current_location_id,
    CASE
      WHEN g.shed_id IS NULL THEN 'missing shed_id'
      WHEN g.park_id IS NULL THEN 'missing park_id'
      WHEN g.current_location_id IS NULL THEN 'missing current_location_id'
      WHEN regexp_replace(lower(btrim(COALESCE(gsp.partition_label, 'whole'))), '^part[[:space:]]+', '') = 'whole'
        AND g.current_location_id IS DISTINCT FROM g.shed_id
        THEN 'whole-shed goat current_location_id is not shed_id'
      WHEN regexp_replace(lower(btrim(COALESCE(gsp.partition_label, 'whole'))), '^part[[:space:]]+', '') <> 'whole'
        AND sp.operational_location_id IS NULL
        THEN 'partitioned goat has no mapped operational pen'
      WHEN regexp_replace(lower(btrim(COALESCE(gsp.partition_label, 'whole'))), '^part[[:space:]]+', '') <> 'whole'
        AND g.current_location_id IS DISTINCT FROM sp.operational_location_id
        THEN 'partitioned goat current_location_id is not mapped operational pen'
      WHEN sp.operational_location_id IS NOT NULL
        AND NOT (
          current_loc.location_type = 'pen'
          AND current_loc.status = 'active'
          AND current_loc.parent_location_id = g.shed_id
        ) THEN 'mapped operational pen is not active under shed_id'
      WHEN shed.location_id IS NULL THEN 'shed_id is not an active shed'
      WHEN park.location_id IS NULL THEN 'park_id is not an active park'
      WHEN shed.parent_location_id IS DISTINCT FROM g.park_id THEN 'shed parent is not goat park'
      ELSE 'unknown'
    END AS reason
  FROM goats g
  LEFT JOIN locations shed
    ON shed.tenant_id = g.tenant_id
   AND shed.location_id = g.shed_id
   AND shed.location_type = 'shed'
   AND shed.status = 'active'
  LEFT JOIN locations current_loc
    ON current_loc.tenant_id = g.tenant_id
   AND current_loc.location_id = g.current_location_id
  LEFT JOIN goat_shed_partitions gsp
    ON gsp.tenant_id = g.tenant_id
   AND gsp.goat_id = g.goat_id
  LEFT JOIN shed_partitions sp
    ON sp.tenant_id = g.tenant_id
   AND sp.shed_id = g.shed_id
   AND sp.status = 'active'
   AND sp.normalized_label = regexp_replace(lower(btrim(COALESCE(gsp.partition_label, 'whole'))), '^part[[:space:]]+', '')
  LEFT JOIN locations park
    ON park.tenant_id = g.tenant_id
   AND park.location_id = g.park_id
   AND park.location_type = 'park'
   AND park.status = 'active'
  WHERE g.tenant_id = :'tenant_id'::uuid
    AND g.lifecycle_status = 'alive'
    AND g.merged_into_goat_id IS NULL
    AND (
      g.shed_id IS NULL
      OR g.park_id IS NULL
      OR g.current_location_id IS NULL
      OR CASE
        WHEN regexp_replace(lower(btrim(COALESCE(gsp.partition_label, 'whole'))), '^part[[:space:]]+', '') = 'whole' THEN
          g.current_location_id IS DISTINCT FROM g.shed_id
        ELSE
          sp.operational_location_id IS NULL
          OR g.current_location_id IS DISTINCT FROM sp.operational_location_id
      END
      OR (
        sp.operational_location_id IS NOT NULL
        AND NOT (
          current_loc.location_type = 'pen'
          AND current_loc.status = 'active'
          AND current_loc.parent_location_id = g.shed_id
        )
      )
      OR shed.location_id IS NULL
      OR park.location_id IS NULL
      OR shed.parent_location_id IS DISTINCT FROM g.park_id
    )
),
vaccination_obligation_shed_scope_invariant AS (
  SELECT
    'vaccination_obligation_shed_scope_invariant' AS invariant,
    COALESCE(g.display_id, oi.target_id::text) AS goat,
    COALESCE(g.lifecycle_status, '') AS lifecycle,
    COALESCE(g.health_status, '') AS health,
    COALESCE(g.park_id::text, 'missing') AS park_id,
    COALESCE(g.shed_id::text, 'missing') AS shed_id,
    COALESCE(oi.scope_id::text, 'missing') AS current_location_id,
    'vaccination obligation scope is not the goat shed' AS reason
  FROM obligation_instances oi
  JOIN protocol_versions pv
    ON pv.tenant_id = oi.tenant_id
   AND pv.protocol_version_id = oi.protocol_version_id
  JOIN protocol_definitions pd
    ON pd.tenant_id = pv.tenant_id
   AND pd.protocol_id = pv.protocol_id
   AND pd.category = 'vaccination'
  JOIN goats g
    ON g.tenant_id = oi.tenant_id
   AND g.goat_id = oi.target_id
   AND g.lifecycle_status = 'alive'
   AND g.merged_into_goat_id IS NULL
  WHERE oi.tenant_id = :'tenant_id'::uuid
    AND oi.target_type = 'goat'
    AND oi.status IN ('scheduled', 'due', 'in_progress', 'deferred', 'missed')
    AND (
      oi.scope_type <> 'shed'
      OR oi.scope_id IS DISTINCT FROM g.shed_id
      OR g.shed_id IS NULL
    )
)
SELECT invariant || ' | goat=' || goat || ' | lifecycle=' || lifecycle ||
       ' | health=' || health || ' | park=' || park_id ||
       ' | shed=' || shed_id || ' | observed=' || current_location_id ||
       ' | reason=' || reason
FROM (
  SELECT * FROM active_goat_shed_invariant
  UNION ALL
  SELECT * FROM vaccination_obligation_shed_scope_invariant
) bad
ORDER BY invariant, goat
LIMIT 50;
SQL
)"

if [ -n "${offenders//[[:space:]]/}" ]; then
  echo "goat-shed-integrity proof: FAILED" >&2
  echo "Seed/import left goats or vaccination obligations outside the shed-scoped contract:" >&2
  echo "$offenders" >&2
  exit 1
fi

summary="$(
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -qAt \
  -v tenant_id="$tenant_id" <<'SQL'
SELECT
  count(*) FILTER (WHERE g.lifecycle_status = 'alive' AND g.merged_into_goat_id IS NULL) AS active_goats,
  count(*) FILTER (WHERE g.lifecycle_status = 'alive' AND g.merged_into_goat_id IS NULL AND g.shed_id IS NOT NULL) AS active_goats_with_shed
FROM goats g
WHERE g.tenant_id = :'tenant_id'::uuid;
SQL
)"

echo "goat-shed-integrity proof: passed (${summary})"
