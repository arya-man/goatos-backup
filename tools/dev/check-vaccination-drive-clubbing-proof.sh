#!/usr/bin/env bash
# Fails when tiny vaccination drives still have compatible same-park animals
# inside their own safe windows. This is the post-reseed proof that the sweeper
# actually clubbed work, not merely generated per-animal obligations.
set -euo pipefail

tenant_id="${GOATOS_TENANT_ID:-00000000-0000-4000-8000-000000000001}"
from_date="${GOATOS_DRIVE_CLUBBING_FROM:-2026-01-01}"
to_date="${GOATOS_DRIVE_CLUBBING_TO:-2026-12-31}"
max_small="${GOATOS_DRIVE_CLUBBING_MAX_SMALL:-2}"
species_policy="${GOATOS_DRIVE_SPECIES_GROUPING_POLICY:-kid_mixed}"

if [ "${1:-}" = "--self-test" ]; then
  bash -n "$0"
  grep -q "ob.status = 'planned'" "$0"
  grep -q "ob.scope_type <> 'park'" "$0"
  grep -q "sp.location_id = g.shed_id" "$0"
  grep -q "GOATOS_DRIVE_SPECIES_GROUPING_POLICY" "$0"
  echo "vaccination-drive-clubbing-proof: self-test passed"
  exit 0
fi

if [ -z "${DATABASE_URL:-}" ]; then
  echo "vaccination-drive-clubbing-proof: DATABASE_URL is required" >&2
  exit 2
fi

non_park_batches="$(
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -qAt \
  -v tenant_id="$tenant_id" \
  -v from_date="$from_date" \
  -v to_date="$to_date" <<'SQL'
SELECT
  (ob.planned_date AT TIME ZONE 'Asia/Kolkata')::date || ' | scope=' ||
  ob.scope_type || ' | batch=' || ob.batch_id::text || ' | animals=' ||
  count(DISTINCT oi.target_id)::text
FROM obligation_batches ob
JOIN protocol_versions pv ON pv.protocol_version_id = ob.protocol_version_id
JOIN protocol_definitions pd ON pd.protocol_id = pv.protocol_id
JOIN obligation_instances oi ON oi.batch_id = ob.batch_id
WHERE ob.tenant_id = :'tenant_id'::uuid
  AND pd.category = 'vaccination'
  AND ob.status = 'planned'
  AND ob.scope_type <> 'park'
  AND ob.planned_date >= (:'from_date'::date AT TIME ZONE 'Asia/Kolkata')
  AND ob.planned_date < ((:'to_date'::date + interval '1 day') AT TIME ZONE 'Asia/Kolkata')
GROUP BY ob.batch_id, ob.planned_date, ob.scope_type
ORDER BY ob.planned_date, ob.batch_id;
SQL
)"

if [ -n "${non_park_batches//[[:space:]]/}" ]; then
  echo "vaccination-drive-clubbing-proof: FAILED" >&2
  echo "Vaccination drive batches must be park-scoped. Non-park planned batches:" >&2
  echo "$non_park_batches" >&2
  exit 1
fi

offenders="$(
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -qAt \
  -v tenant_id="$tenant_id" \
  -v from_date="$from_date" \
  -v to_date="$to_date" \
  -v max_small="$max_small" \
  -v species_policy="$species_policy" <<'SQL'
WITH event_rows AS (
  SELECT
    ob.batch_id,
    (ob.planned_date AT TIME ZONE 'Asia/Kolkata')::date AS drive_date,
    COALESCE(lp.name, gp.name, 'unknown') AS park,
    COALESCE(gs.name, 'unknown') AS shed,
    oi.target_id,
    CASE
      WHEN :'species_policy' = 'species_specific' THEN 'species:' || lower(coalesce(g.species,'goat'))
      WHEN upper(COALESCE(asl.stage_code, g.management_stage, '')) LIKE 'K%'
        OR upper(COALESCE(asl.stage_code, g.management_stage, '')) LIKE '%KID%'
        THEN 'kid_mixed'
      ELSE 'species:' || lower(coalesce(g.species,'goat'))
    END AS planner_group,
    (oi.due_at AT TIME ZONE 'Asia/Kolkata')::date AS due_date,
    (COALESCE(
      oi.window_end,
      oi.due_at + make_interval(days => GREATEST(COALESCE(pr.due_window_days, 0), 0))
    ) AT TIME ZONE 'Asia/Kolkata')::date AS safe_until,
    COALESCE(pr.eligibility_json#>>'{vaccine,name}', pr.eligibility_json#>>'{vaccine,code}', pr.dose_code) AS vaccine
  FROM obligation_batches ob
  JOIN protocol_versions pv ON pv.protocol_version_id = ob.protocol_version_id
  JOIN protocol_definitions pd ON pd.protocol_id = pv.protocol_id
  JOIN obligation_instances oi ON oi.batch_id = ob.batch_id
  JOIN protocol_rules pr ON pr.rule_id = oi.rule_id
  JOIN goats g ON g.goat_id = oi.target_id
  LEFT JOIN locations gs ON gs.location_id = g.shed_id
  LEFT JOIN locations gp ON gp.location_id = g.park_id
  LEFT JOIN locations lp ON lp.location_id = gs.parent_location_id
  LEFT JOIN shed_profiles sp ON sp.tenant_id = g.tenant_id AND sp.location_id = g.shed_id
  LEFT JOIN animal_stage_lookup asl ON asl.tenant_id = sp.tenant_id
    AND asl.animal_stage_id = sp.animal_stage_id
    AND asl.status = 'active'
  WHERE ob.tenant_id = :'tenant_id'::uuid
    AND pd.category = 'vaccination'
    AND ob.planned_date >= (:'from_date'::date AT TIME ZONE 'Asia/Kolkata')
    AND ob.planned_date < ((:'to_date'::date + interval '1 day') AT TIME ZONE 'Asia/Kolkata')
    AND ob.status = 'planned'
    AND g.shed_id IS NOT NULL
),
drive_groups AS (
  SELECT
    drive_date,
    park,
    planner_group,
    count(DISTINCT target_id) AS animals,
    min(due_date) AS due_from,
    min(safe_until) AS binding_safe_until,
    string_agg(DISTINCT shed, ', ' ORDER BY shed) AS sheds,
    string_agg(DISTINCT vaccine, ', ' ORDER BY vaccine) AS vaccines
  FROM event_rows
  GROUP BY drive_date, park, planner_group
),
small AS (
  SELECT *
  FROM drive_groups
  WHERE animals <= :'max_small'::int
)
SELECT
  s.drive_date || ' | ' ||
  s.park || ' | ' ||
  s.planner_group || ' | animals=' || s.animals || ' | sheds=' || s.sheds ||
  ' | due_from=' || s.due_from || ' | safe_until=' || s.binding_safe_until ||
  ' | compatible_nearby=' ||
  string_agg(g.drive_date::text || ':animals=' || g.animals || ':sheds=' || g.sheds || ':vaccines=' || g.vaccines, ' ; ' ORDER BY g.drive_date)
FROM small s
JOIN drive_groups g
  ON g.park = s.park
 AND g.planner_group = s.planner_group
 AND g.drive_date <> s.drive_date
 AND g.drive_date BETWEEN s.due_from AND s.binding_safe_until
GROUP BY s.drive_date, s.park, s.planner_group, s.animals, s.sheds, s.due_from, s.binding_safe_until
ORDER BY s.drive_date, s.park, s.planner_group;
SQL
)"

if [ -n "${offenders//[[:space:]]/}" ]; then
  echo "vaccination-drive-clubbing-proof: FAILED" >&2
  echo "Small vaccination drives still have compatible same-park drives inside their safe window:" >&2
  echo "$offenders" >&2
  exit 1
fi

echo "vaccination-drive-clubbing-proof: passed (${from_date}..${to_date}, small<=${max_small})"
