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
  # Capacity gate must exist: config read, active-status counting, animal summation,
  # and per-goat boundary-evidence join.
  grep -q "max_per_day" "$0"
  grep -q "'in_progress'" "$0"
  grep -q "sum(animals)" "$0"
  grep -q "first_batching_hold_until" "$0"
  # Round-9: small-drive test must be park/date grain and the merge-target join
  # must check the target date's free capacity / last-safe overflow evidence.
  grep -q "park_date_animals" "$0"
  grep -q "total_animals <= :'max_small'" "$0"
  grep -q "target_cells" "$0"
  grep -q "s.cells <= :'capacity_max'" "$0"
  grep -q "s.safe_latest" "$0"
  grep -q "g.drive_date = s.binding_safe_until" "$0"
  # Round-10: park attribution must come from the BATCH's own scope, not goat
  # residence (duplicate shed names / mid-shift goats fabricate phantom drives).
  grep -q "ob.scope_type = 'park' THEN bs.name" "$0"
  grep -q "ob.scope_type = 'shed' THEN bsp.name" "$0"
  echo "vaccination-drive-clubbing-proof: self-test passed"
  exit 0
fi

# Executable fixture test (C-4): proves the merge-capacity math counts CELLS.
# Fixture: 1 animal with 2 vaccine obligations (1 animal, 2 cells) on D1; a
# 2-cell target drive on D2; cap 3 leaves 1 free cell on D2. Cell math must NOT
# suggest the merge (2 movers > 1 free) => proof passes. Raising cap to 4 makes
# the merge legal => proof must fail. Animal math (1 mover) would wrongly
# suggest the merge at cap 3 and break case 1.
if [ "${1:-}" = "--fixture-test" ]; then
  if [ -z "${DATABASE_URL:-}" ]; then
    echo "vaccination-drive-clubbing-proof: fixture-test skipped (DATABASE_URL not set)"
    exit 0
  fi
  if ! psql "$DATABASE_URL" -qAt -c "SELECT 1" >/dev/null 2>&1; then
    echo "vaccination-drive-clubbing-proof: fixture-test skipped (database not reachable)"
    exit 0
  fi
  fixture_schema="fixture_clubproof_$$"
  cleanup_fixture() {
    psql "$DATABASE_URL" -qAt -c "DROP SCHEMA IF EXISTS ${fixture_schema} CASCADE" >/dev/null 2>&1 || true
  }
  trap cleanup_fixture EXIT
  psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -qAt >/dev/null <<SQL
CREATE SCHEMA ${fixture_schema};
SET search_path = ${fixture_schema};
CREATE TABLE protocol_definitions (protocol_id uuid, category text);
CREATE TABLE protocol_versions (protocol_version_id uuid, protocol_id uuid);
CREATE TABLE protocol_rules (rule_id uuid, dose_code text, due_window_days int, eligibility_json jsonb);
CREATE TABLE locations (location_id uuid, name text, parent_location_id uuid);
CREATE TABLE goats (goat_id uuid, tenant_id uuid, species text, management_stage text, shed_id uuid, park_id uuid);
CREATE TABLE shed_profiles (tenant_id uuid, location_id uuid, animal_stage_id uuid);
CREATE TABLE animal_stage_lookup (tenant_id uuid, animal_stage_id uuid, stage_code text, status text);
CREATE TABLE vaccination_capacity_config (tenant_id uuid, max_per_day int);
CREATE TABLE obligation_batches (batch_id uuid, tenant_id uuid, protocol_version_id uuid, status text, scope_type text, scope_id uuid, planned_date timestamptz, planned_quantity numeric);
CREATE TABLE obligation_instances (obligation_id uuid, tenant_id uuid, batch_id uuid, rule_id uuid, target_id uuid, status text, due_at timestamptz, window_end timestamptz, first_batching_hold_until timestamptz);
INSERT INTO protocol_definitions VALUES ('a0000000-0000-4000-8000-000000000001', 'vaccination');
INSERT INTO protocol_versions VALUES ('a0000000-0000-4000-8000-000000000002', 'a0000000-0000-4000-8000-000000000001');
INSERT INTO protocol_rules VALUES
  ('a0000000-0000-4000-8000-000000000011', 'PPR', 6, '{}'::jsonb),
  ('a0000000-0000-4000-8000-000000000012', 'FMD', 6, '{}'::jsonb);
INSERT INTO locations VALUES
  ('b0000000-0000-4000-8000-000000000001', 'FixturePark', NULL),
  ('b0000000-0000-4000-8000-000000000002', 'FixtureShed', 'b0000000-0000-4000-8000-000000000001');
INSERT INTO goats VALUES
  ('c0000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000001', 'goat', 'K1', 'b0000000-0000-4000-8000-000000000002', 'b0000000-0000-4000-8000-000000000001'),
  ('c0000000-0000-4000-8000-000000000002', '00000000-0000-4000-8000-000000000001', 'goat', 'K1', 'b0000000-0000-4000-8000-000000000002', 'b0000000-0000-4000-8000-000000000001'),
  ('c0000000-0000-4000-8000-000000000003', '00000000-0000-4000-8000-000000000001', 'goat', 'K1', 'b0000000-0000-4000-8000-000000000002', 'b0000000-0000-4000-8000-000000000001');
INSERT INTO vaccination_capacity_config VALUES ('00000000-0000-4000-8000-000000000001', 3);
INSERT INTO obligation_batches VALUES
  ('d0000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000001', 'a0000000-0000-4000-8000-000000000002', 'planned', 'park', 'b0000000-0000-4000-8000-000000000001', '2026-03-02 06:00+00', NULL),
  ('d0000000-0000-4000-8000-000000000002', '00000000-0000-4000-8000-000000000001', 'a0000000-0000-4000-8000-000000000002', 'planned', 'park', 'b0000000-0000-4000-8000-000000000001', '2026-03-05 06:00+00', NULL);
INSERT INTO obligation_instances VALUES
  ('e0000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000001', 'd0000000-0000-4000-8000-000000000001', 'a0000000-0000-4000-8000-000000000011', 'c0000000-0000-4000-8000-000000000001', 'scheduled', '2026-03-02 06:00+00', '2026-03-08 06:00+00', NULL),
  ('e0000000-0000-4000-8000-000000000002', '00000000-0000-4000-8000-000000000001', 'd0000000-0000-4000-8000-000000000001', 'a0000000-0000-4000-8000-000000000012', 'c0000000-0000-4000-8000-000000000001', 'scheduled', '2026-03-02 06:00+00', '2026-03-08 06:00+00', NULL),
  ('e0000000-0000-4000-8000-000000000003', '00000000-0000-4000-8000-000000000001', 'd0000000-0000-4000-8000-000000000002', 'a0000000-0000-4000-8000-000000000011', 'c0000000-0000-4000-8000-000000000002', 'scheduled', '2026-03-04 06:00+00', '2026-03-10 06:00+00', NULL),
  ('e0000000-0000-4000-8000-000000000004', '00000000-0000-4000-8000-000000000001', 'd0000000-0000-4000-8000-000000000002', 'a0000000-0000-4000-8000-000000000012', 'c0000000-0000-4000-8000-000000000003', 'scheduled', '2026-03-04 06:00+00', '2026-03-10 06:00+00', NULL);
SQL
  # Case 1: cap 3, target holds 2 cells => 1 free cell < 2 mover cells.
  # Merge must NOT be suggested; proof must pass.
  if ! PGOPTIONS="-c search_path=${fixture_schema}" bash "$0" >/dev/null 2>&1; then
    echo "vaccination-drive-clubbing-proof: fixture-test FAILED (cap 3: merge wrongly suggested or proof errored; cell math broken)" >&2
    exit 1
  fi
  # Case 2: cap 4 => 2 free cells >= 2 mover cells; merge is legal, so the
  # unclubbed small drive must now be flagged and the proof must fail.
  psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -qAt -c "UPDATE ${fixture_schema}.vaccination_capacity_config SET max_per_day = 4" >/dev/null
  if PGOPTIONS="-c search_path=${fixture_schema}" bash "$0" >/dev/null 2>&1; then
    echo "vaccination-drive-clubbing-proof: fixture-test FAILED (cap 4: legal merge target not flagged)" >&2
    exit 1
  fi
  echo "vaccination-drive-clubbing-proof: fixture-test passed"
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

# Capacity gate: fail closed when the tenant has no published capacity config.
operator_tables_exist="$(
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -qAt <<'SQL'
SELECT (
  to_regclass('workforce_members') IS NOT NULL
  AND to_regclass('workforce_positions') IS NOT NULL
  AND to_regclass('position_module_duties') IS NOT NULL
)::text;
SQL
)"

if [ "$operator_tables_exist" = "true" ]; then
capacity_max="$(
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -qAt \
  -v tenant_id="$tenant_id" <<'SQL'
WITH cfg AS (
  SELECT max_per_day FROM vaccination_capacity_config WHERE tenant_id = :'tenant_id'::uuid
), operators AS (
  SELECT count(DISTINCT wm.workforce_member_id)::int AS operator_count
  FROM workforce_members wm
  JOIN workforce_positions wp
    ON wp.tenant_id = wm.tenant_id
   AND wp.workforce_member_id = wm.workforce_member_id
   AND wp.status = 'active'
  JOIN position_module_duties pmd
    ON pmd.tenant_id = wp.tenant_id
   AND pmd.position_code = wp.position_code
   AND pmd.module_code IN ('vaccination', 'pc.vaccination')
   AND pmd.duty_type = 'execute'
   AND pmd.status = 'active'
  WHERE wm.tenant_id = :'tenant_id'::uuid
    AND wm.status = 'active'
)
SELECT (cfg.max_per_day * GREATEST(operators.operator_count, 1))::int
FROM cfg, operators;
SQL
)"
else
capacity_max="$(
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -qAt \
  -v tenant_id="$tenant_id" <<'SQL'
SELECT max_per_day FROM vaccination_capacity_config WHERE tenant_id = :'tenant_id'::uuid;
SQL
)"
fi
if [ -z "${capacity_max//[[:space:]]/}" ]; then
  echo "vaccination-drive-clubbing-proof: FAILED" >&2
  echo "No vaccination_capacity_config row for tenant ${tenant_id}. Capacity proof cannot run; publish capacity config first." >&2
  exit 1
fi

# Reject non-integral planned quantities before using them as dose-cell counts.
non_integral="$(
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -qAt \
  -v tenant_id="$tenant_id" \
  -v from_date="$from_date" \
  -v to_date="$to_date" <<'SQL'
SELECT ob.batch_id::text || ' | planned_quantity=' || ob.planned_quantity::text
FROM obligation_batches ob
JOIN protocol_versions pv ON pv.protocol_version_id = ob.protocol_version_id
JOIN protocol_definitions pd ON pd.protocol_id = pv.protocol_id
WHERE ob.tenant_id = :'tenant_id'::uuid
  AND pd.category = 'vaccination'
  AND ob.planned_quantity IS NOT NULL
  AND ob.planned_quantity <> floor(ob.planned_quantity)
  AND ob.planned_date >= (:'from_date'::date AT TIME ZONE 'Asia/Kolkata')
  AND ob.planned_date < ((:'to_date'::date + interval '1 day') AT TIME ZONE 'Asia/Kolkata')
ORDER BY ob.batch_id;
SQL
)"
if [ -n "${non_integral//[[:space:]]/}" ]; then
  echo "vaccination-drive-clubbing-proof: FAILED" >&2
  echo "Non-integral planned_quantity on vaccination batches (cells must be whole administrations):" >&2
  echo "$non_integral" >&2
  exit 1
fi

# Capacity + overflow-evidence check: sum distinct animals per park/date across all vaccines/batches/rules
# (planned + in_progress + completed; canceled/superseded excluded). A park/date may exceed
# total available operator capacity ONLY when every non-canceled obligation on it is pinned by its own boundary:
# medical safe-window end (window_end, fallback due_at) or the due+7 batching hold
# (first_batching_hold_until, fallback due_at + 7 days) is <= planned_date. Any movable goat
# on an over-cap park/date is a failure and is listed.
capacity_violations="$(
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -qAt \
  -v tenant_id="$tenant_id" \
  -v from_date="$from_date" \
  -v to_date="$to_date" \
  -v capacity_max="$capacity_max" <<'SQL'
WITH drive_batches AS (
  SELECT
    ob.batch_id,
    ob.planned_date,
    (ob.planned_date AT TIME ZONE 'Asia/Kolkata')::date AS drive_date,
    COALESCE(
      CASE WHEN ob.scope_type = 'park' THEN ob.scope_id END,
      gs.parent_location_id,
      g.park_id
    ) AS park_id
  FROM obligation_batches ob
  JOIN protocol_versions pv ON pv.protocol_version_id = ob.protocol_version_id
  JOIN protocol_definitions pd ON pd.protocol_id = pv.protocol_id
  LEFT JOIN locations gs ON gs.location_id = ob.scope_id AND ob.scope_type = 'shed'
  LEFT JOIN LATERAL (
    SELECT g0.park_id
    FROM obligation_instances oi0
    JOIN goats g0 ON g0.goat_id = oi0.target_id
    WHERE oi0.batch_id = ob.batch_id AND g0.park_id IS NOT NULL
    LIMIT 1
  ) g ON TRUE
  WHERE ob.tenant_id = :'tenant_id'::uuid
    AND pd.category = 'vaccination'
    AND ob.status IN ('planned', 'in_progress', 'completed')
    AND ob.planned_date >= (:'from_date'::date AT TIME ZONE 'Asia/Kolkata')
    AND ob.planned_date < ((:'to_date'::date + interval '1 day') AT TIME ZONE 'Asia/Kolkata')
),
batch_animals AS (
  SELECT
    db.park_id,
    db.drive_date,
    db.batch_id,
    count(DISTINCT oi.target_id) FILTER (WHERE oi.status NOT IN ('canceled', 'superseded'))::int AS animals
  FROM drive_batches db
  JOIN obligation_batches ob ON ob.batch_id = db.batch_id
  LEFT JOIN obligation_instances oi ON oi.batch_id = db.batch_id
  GROUP BY db.park_id, db.drive_date, db.batch_id
),
park_date_animals AS (
  SELECT park_id, drive_date, sum(animals)::int AS total_animals
  FROM batch_animals
  GROUP BY park_id, drive_date
),
over_cap AS (
  SELECT * FROM park_date_animals WHERE total_animals > :'capacity_max'::int
),
movable AS (
  -- Goats on an over-cap park/date whose OWN boundaries would allow moving later:
  -- both the medical safe-window end and the due+7 hold boundary are strictly after
  -- planned_date, so this cell has no last-safe/hold evidence.
  SELECT
    oc.park_id,
    oc.drive_date,
    oc.total_animals,
    oi.target_id,
    (COALESCE(oi.window_end, oi.due_at) AT TIME ZONE 'Asia/Kolkata')::date AS safe_until,
    (COALESCE(oi.first_batching_hold_until, oi.due_at + interval '7 days') AT TIME ZONE 'Asia/Kolkata')::date AS hold_until
  FROM over_cap oc
  JOIN drive_batches db ON db.park_id = oc.park_id AND db.drive_date = oc.drive_date
  JOIN obligation_instances oi ON oi.batch_id = db.batch_id
  WHERE oi.status NOT IN ('canceled', 'superseded')
    AND (COALESCE(oi.window_end, oi.due_at) AT TIME ZONE 'Asia/Kolkata')::date > oc.drive_date
    AND (COALESCE(oi.first_batching_hold_until, oi.due_at + interval '7 days') AT TIME ZONE 'Asia/Kolkata')::date > oc.drive_date
)
SELECT
  m.drive_date || ' | park=' || COALESCE(p.name, m.park_id::text, 'unknown') ||
  ' | animals=' || m.total_animals || ' | capacity=' || :'capacity_max' ||
  ' | movable_goat=' || m.target_id::text ||
  ' | safe_until=' || m.safe_until || ' | hold_until=' || m.hold_until
FROM movable m
LEFT JOIN locations p ON p.location_id = m.park_id
ORDER BY m.drive_date, m.park_id, m.target_id;
SQL
)"

if [ -n "${capacity_violations//[[:space:]]/}" ]; then
  echo "vaccination-drive-clubbing-proof: FAILED" >&2
  echo "Over-cap park/date drives contain movable goats (no last-safe/hold-boundary evidence):" >&2
  echo "$capacity_violations" >&2
  exit 1
fi

offenders="$(
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -qAt \
  -v tenant_id="$tenant_id" \
  -v from_date="$from_date" \
  -v to_date="$to_date" \
  -v max_small="$max_small" \
  -v capacity_max="$capacity_max" \
  -v species_policy="$species_policy" <<'SQL'
WITH event_rows AS (
  SELECT
    ob.batch_id,
    (ob.planned_date AT TIME ZONE 'Asia/Kolkata')::date AS drive_date,
    -- Batch-scope park attribution: a drive belongs to the park the BATCH is
    -- scoped to (park scope -> itself; shed scope -> that shed parent park).
    -- Goat residence is only a last-resort fallback: duplicate shed names across
    -- parks and mid-shift goats otherwise fabricate phantom cross-park drives.
    COALESCE(
      CASE WHEN ob.scope_type = 'park' THEN bs.name END,
      CASE WHEN ob.scope_type = 'shed' THEN bsp.name END,
      lp.name, gp.name, 'unknown') AS park,
    COALESCE(gs.name, 'unknown') AS shed,
    oi.obligation_id,
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
  LEFT JOIN locations bs ON bs.location_id = ob.scope_id
  LEFT JOIN locations bsp ON bsp.location_id = bs.parent_location_id
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
    -- Capacity math is in administration CELLS: one obligation row = one dose
    -- cell. A 1-animal/2-vaccine group is 1 animal but 2 cells.
    count(DISTINCT obligation_id) AS cells,
    min(due_date) AS due_from,
    max(due_date) AS due_latest,
    min(safe_until) AS binding_safe_until,
    max(safe_until) AS safe_latest,
    string_agg(DISTINCT shed, ', ' ORDER BY shed) AS sheds,
    string_agg(DISTINCT vaccine, ', ' ORDER BY vaccine) AS vaccines
  FROM event_rows
  GROUP BY drive_date, park, planner_group
),
-- A drive is park/date-grain: a planner_group partition is storage, not a drive.
-- The small-drive test applies only when the WHOLE park/date visit is small.
park_date_animals AS (
  SELECT drive_date, park, count(DISTINCT target_id) AS total_animals
  FROM event_rows
  GROUP BY drive_date, park
),
small AS (
  SELECT dg.*
  FROM drive_groups dg
  JOIN park_date_animals pda
    ON pda.drive_date = dg.drive_date AND pda.park = dg.park
  WHERE pda.total_animals <= :'max_small'::int
),
-- Persisted dose-cell load per park/date across ALL active batches (planned +
-- in_progress + completed), so a merge target free capacity can be checked.
capacity_batches AS (
  SELECT
    ob.batch_id,
    (ob.planned_date AT TIME ZONE 'Asia/Kolkata')::date AS drive_date,
    -- Batch-scope park attribution first; goat residence only as last resort.
    COALESCE(
      CASE WHEN ob.scope_type = 'park' THEN bs.name END,
      CASE WHEN ob.scope_type = 'shed' THEN bsp.name END,
      pk.park, 'unknown') AS park,
    GREATEST(
      COALESCE(ob.planned_quantity, 1)::int,
      count(oi.obligation_id) FILTER (WHERE oi.status NOT IN ('canceled', 'superseded'))::int
    ) AS cells
  FROM obligation_batches ob
  JOIN protocol_versions pv ON pv.protocol_version_id = ob.protocol_version_id
  JOIN protocol_definitions pd ON pd.protocol_id = pv.protocol_id
  LEFT JOIN obligation_instances oi ON oi.batch_id = ob.batch_id
  LEFT JOIN locations bs ON bs.location_id = ob.scope_id
  LEFT JOIN locations bsp ON bsp.location_id = bs.parent_location_id
  LEFT JOIN LATERAL (
    SELECT COALESCE(lp0.name, gp0.name, 'unknown') AS park
    FROM obligation_instances oi0
    JOIN goats g0 ON g0.goat_id = oi0.target_id
    LEFT JOIN locations gs0 ON gs0.location_id = g0.shed_id
    LEFT JOIN locations gp0 ON gp0.location_id = g0.park_id
    LEFT JOIN locations lp0 ON lp0.location_id = gs0.parent_location_id
    WHERE oi0.batch_id = ob.batch_id
    LIMIT 1
  ) pk ON TRUE
  WHERE ob.tenant_id = :'tenant_id'::uuid
    AND pd.category = 'vaccination'
    AND ob.status IN ('planned', 'in_progress', 'completed')
  GROUP BY ob.batch_id, ob.planned_date, ob.scope_type, bs.name, bsp.name, pk.park, ob.planned_quantity
),
target_cells AS (
  SELECT drive_date, park, sum(cells)::int AS total_cells
  FROM capacity_batches
  GROUP BY drive_date, park
)
SELECT
  s.drive_date || ' | ' ||
  s.park || ' | ' ||
  s.planner_group || ' | animals=' || s.animals || ' | cells=' || s.cells || ' | sheds=' || s.sheds ||
  ' | due_from=' || s.due_from || ' | safe_until=' || s.binding_safe_until ||
  ' | compatible_nearby=' ||
  string_agg(
    g.drive_date::text || ':animals=' || g.animals || ':cells=' || COALESCE(tc.total_cells, 0) ||
    ':sheds=' || g.sheds || ':vaccines=' || g.vaccines,
    ' ; ' ORDER BY g.drive_date)
FROM small s
JOIN drive_groups g
  ON g.park = s.park
 AND g.planner_group = s.planner_group
 AND g.drive_date <> s.drive_date
 -- Every mover must itself be movable to the target date: at/after the latest
 -- due date in the small group and at/before its binding safe-until.
 AND g.drive_date BETWEEN s.due_latest AND s.binding_safe_until
LEFT JOIN target_cells tc
  ON tc.park = g.park AND tc.drive_date = g.drive_date
-- Merge target is only a genuine option when it has free capacity for the
-- movers, or the target date IS the mover last safe day (overflow justified
-- by per-goat last-safe evidence).
WHERE (
  -- Capacity math in CELLS, not animals: the moving group consumes one cell per
  -- obligation (dose), so a 1-animal/2-vaccine group needs 2 free cells.
  COALESCE(tc.total_cells, 0) + s.cells <= :'capacity_max'::int
  -- Last-safe overflow is per obligation: the target may exceed cap only when
  -- EVERY moving obligation is at its own last safe day on the target date
  -- (min AND max of safe_until both equal the target date).
  OR (g.drive_date = s.binding_safe_until AND g.drive_date = s.safe_latest)
)
GROUP BY s.drive_date, s.park, s.planner_group, s.animals, s.cells, s.sheds, s.due_from, s.binding_safe_until
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
