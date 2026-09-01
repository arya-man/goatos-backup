-- Vaccination safety audit.
-- Usage:
--   psql "$DATABASE_URL" -v tenant_id="'00000000-0000-4000-8000-000000000001'" -f tools/dev/vaccination-safety-audit.sql
--
-- Reports the invariants that must stay clean after vaccination anchor/sweeper
-- work. Dates are India business dates.

-- projection-review: membership=active vaccination obligations and active vaccination anchor events; group_key=named invariant check/fingerprint; join_cardinality=obligation:rule:goat/location joins are N:1 and duplicate audit intentionally groups by animal/vaccine/date; pagination=whole tenant audit without LIMIT; scope=one tenant supplied by psql variable.
WITH active_version AS (
  SELECT protocol_version_id
  FROM protocol_versions
  WHERE tenant_id = :tenant_id::uuid
    AND status = 'published'
  ORDER BY published_at DESC NULLS LAST, created_at DESC
  LIMIT 1
), active AS (
  SELECT
    oi.obligation_id,
    oi.status,
    (oi.due_at AT TIME ZONE 'Asia/Kolkata')::date AS due_date,
    g.goat_id,
    g.species,
    g.dob,
    coalesce(prd.vaccine_code, pr.eligibility_json->'vaccine'->>'code') AS vaccine_code,
    pr.dose_code
  FROM obligation_instances oi
  JOIN protocol_rules pr ON pr.rule_id = oi.rule_id
  LEFT JOIN protocol_rule_dimensions prd ON prd.rule_id = pr.rule_id
  JOIN goats g ON g.goat_id = oi.target_id
  WHERE oi.tenant_id = :tenant_id::uuid
    AND oi.target_type = 'goat'
    AND oi.status IN ('scheduled', 'due', 'in_progress', 'deferred')
    AND pr.protocol_version_id = (SELECT protocol_version_id FROM active_version)
)
SELECT 'sep1_active' AS check_name, count(*)::bigint AS count
FROM active
WHERE due_date = DATE '2026-09-01'
UNION ALL
SELECT 'pre_sep8_ppr_fmd_hs', count(*)::bigint
FROM active
WHERE vaccine_code IN ('PPR', 'FMD', 'HS')
  AND due_date BETWEEN DATE '2026-08-31' AND DATE '2026-09-07'
UNION ALL
SELECT 'pre_oct15_z1_z3', count(*)::bigint
FROM active
WHERE vaccine_code = 'Z1_Z3'
  AND due_date < DATE '2026-10-15'
UNION ALL
SELECT 'duplicate_same_animal_vaccine_date', coalesce(sum(extra), 0)::bigint
FROM (
  SELECT greatest(count(*) - 1, 0) AS extra
  FROM active
  GROUP BY goat_id, vaccine_code, due_date
  HAVING count(*) > 1
) d
UNION ALL
SELECT 'wrong_species', count(*)::bigint
FROM active
WHERE (vaccine_code = 'GOAT_POX' AND species <> 'goat')
   OR (vaccine_code IN ('SHEEP_POX', 'BLUE_TONGUE') AND species <> 'sheep')
UNION ALL
SELECT 'underage', count(*)::bigint
FROM active
WHERE (vaccine_code IN ('PPR', 'GOAT_POX', 'SHEEP_POX', 'BLUE_TONGUE') AND due_date < (dob::date + 112))
   OR (vaccine_code IN ('FMD', 'HS') AND due_date < (dob::date + 84))
UNION ALL
SELECT 'deferred', count(*)::bigint
FROM active
WHERE status = 'deferred'
ORDER BY check_name;

-- projection-review: membership=active vaccination obligations and active vaccination anchor events; group_key=fingerprint label; join_cardinality=obligation:rule:goat joins are N:1 and anchor rows are independent; pagination=whole tenant fingerprint without LIMIT; scope=one tenant supplied by psql variable.
WITH active_version AS (
  SELECT protocol_version_id
  FROM protocol_versions
  WHERE tenant_id = :tenant_id::uuid
    AND status = 'published'
  ORDER BY published_at DESC NULLS LAST, created_at DESC
  LIMIT 1
), active AS (
  SELECT
    oi.obligation_id,
    oi.status,
    (oi.due_at AT TIME ZONE 'Asia/Kolkata')::date AS due_date,
    g.goat_id,
    coalesce(prd.vaccine_code, pr.eligibility_json->'vaccine'->>'code') AS vaccine_code,
    pr.dose_code
  FROM obligation_instances oi
  JOIN protocol_rules pr ON pr.rule_id = oi.rule_id
  LEFT JOIN protocol_rule_dimensions prd ON prd.rule_id = pr.rule_id
  JOIN goats g ON g.goat_id = oi.target_id
  WHERE oi.tenant_id = :tenant_id::uuid
    AND oi.target_type = 'goat'
    AND oi.status IN ('scheduled', 'due', 'in_progress', 'deferred')
    AND pr.protocol_version_id = (SELECT protocol_version_id FROM active_version)
)
SELECT
  'active_vaccination_rows' AS fingerprint,
  count(*)::bigint AS rows,
  md5(string_agg(md5(row(obligation_id, status, due_date, goat_id, vaccine_code, dose_code)::text), '' ORDER BY obligation_id::text)) AS hash
FROM active
UNION ALL
SELECT
  'active_anchors',
  count(*)::bigint,
  md5(string_agg(md5(row(vaccination_anchor_event_id, vaccine_code, coalesce(dose_code, ''), anchor_date, scope_type, scope_payload::text, canceled_at)::text), '' ORDER BY vaccination_anchor_event_id::text))
FROM vaccination_anchor_events
WHERE tenant_id = :tenant_id::uuid
  AND canceled_at IS NULL;
