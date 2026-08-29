#!/usr/bin/env bash
# Deterministic post-seed/post-migration closeout.
#
# This script rebuilds derived read models from canonical source data. It must
# not insert fake business truth. If a new app-visible projection table is added,
# register its projector/backfill here in the same patch.

set -euo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
tenant_id="${GOATOS_TENANT_ID:-00000000-0000-4000-8000-000000000001}"
dry_run=0

for arg in "$@"; do
  case "$arg" in
    --dry-run) dry_run=1 ;;
    *) echo "unknown argument: $arg" >&2; exit 2 ;;
  esac
done

if [ -z "${tenant_id// }" ]; then
  echo "GOATOS_TENANT_ID is required for seed closeout" >&2
  exit 2
fi

run_cmd() {
  local label="$1"; shift
  echo "==> seed-closeout: ${label}"
  if [ "$dry_run" -eq 1 ]; then
    printf '    cd backend &&'
    printf ' %q' "$@"
    printf '\n'
    return
  fi
  (cd "$repo/backend" && "$@")
}

run_go_cmd() {
  local cmd="$1"; shift
  if [ ! -d "$repo/backend/cmd/$cmd" ]; then
    echo "==> seed-closeout: skip ${cmd} (command not present in this checkout)"
    return
  fi
  run_cmd "$cmd" go run "./cmd/$cmd" "$@"
}

apply_expected_drive_variant_inputs() {
  local expected="${GOATOS_EXPECTED_DRIVE_SCHEDULES:-}"
  local variant="${GOATOS_EXPECTED_DRIVE_VARIANT:-}"
  if [ -z "${expected// }" ] || [ -z "${variant// }" ]; then
    return
  fi
  if [ "$variant" != "final_discussed_plan_et_tt_only_no_ppr" ]; then
    return
  fi
  if [ ! -f "$expected" ]; then
    echo "seed-closeout: GOATOS_EXPECTED_DRIVE_SCHEDULES=${expected} does not exist" >&2
    exit 2
  fi
  echo "==> seed-closeout: apply expected-drive variant input (${variant})"
  if [ "$dry_run" -eq 1 ]; then
    printf '    # derive CPT ET+TT original due dates from unbatched vaccination obligations and upsert reviewed-date overrides\n'
    return
  fi
  if [ -z "${DATABASE_URL:-}" ]; then
    echo "seed-closeout: DATABASE_URL is required to apply expected drive variant inputs" >&2
    exit 2
  fi
  local actor_id="${GOATOS_SWEEPER_ACTOR_ID:-${GOATOS_LOCAL_USER_ID:-90000000-0000-4000-8000-000000000101}}"
  psql "$DATABASE_URL" -qAt -v ON_ERROR_STOP=1 <<SQL
WITH park AS (
  SELECT location_id
  FROM locations
  WHERE tenant_id = '${tenant_id}'::uuid
    AND location_type = 'park'
    AND (lower(name) = 'channapatna' OR upper(location_code) = 'CPT')
  LIMIT 1
),
candidate_vaccines AS (
  SELECT DISTINCT
         CASE
           WHEN replace(replace(lower(btrim(v.vaccine_code)), '_', ' '), '+', ' ') = 'et tt' THEN 'ET_TT'
           ELSE NULL
         END AS override_vaccine_code,
         (oi.due_at AT TIME ZONE 'Asia/Kolkata')::date AS original_drive_date,
         CASE
           WHEN replace(replace(lower(btrim(v.vaccine_code)), '_', ' '), '+', ' ') = 'et tt' THEN DATE '2026-07-24'
           ELSE NULL
         END AS override_date,
         CASE
           WHEN replace(replace(lower(btrim(v.vaccine_code)), '_', ' '), '+', ' ') = 'et tt'
             THEN 'CPT validation override: ET+TT first on reviewed business date'
           ELSE NULL
         END AS reason
  FROM obligation_instances oi
  JOIN protocol_versions pv
    ON pv.tenant_id = oi.tenant_id
   AND pv.protocol_version_id = oi.protocol_version_id
  JOIN protocol_definitions pd
    ON pd.tenant_id = pv.tenant_id
   AND pd.protocol_id = pv.protocol_id
   AND pd.category = 'vaccination'
  JOIN protocol_rules pr
    ON pr.tenant_id = oi.tenant_id
   AND pr.rule_id = oi.rule_id
  LEFT JOIN protocol_rule_dimensions prd
    ON prd.tenant_id = oi.tenant_id
   AND prd.protocol_version_id = oi.protocol_version_id
   AND prd.rule_id = oi.rule_id
  LEFT JOIN goats g
    ON g.tenant_id = oi.tenant_id
   AND g.goat_id = oi.target_id
   AND oi.target_type = 'goat'
  LEFT JOIN locations shed
    ON shed.tenant_id = oi.tenant_id
   AND shed.location_id = COALESCE(g.shed_id, CASE WHEN oi.scope_type = 'shed' THEN oi.scope_id END)
   AND shed.location_type = 'shed'
  CROSS JOIN LATERAL (
    SELECT COALESCE(
      NULLIF(pr.eligibility_json -> 'vaccine' ->> 'code', ''),
      NULLIF(prd.vaccine_code, '')
    ) AS vaccine_code
  ) v
  WHERE oi.tenant_id = '${tenant_id}'::uuid
    AND oi.batch_id IS NULL
    AND oi.status IN ('scheduled', 'due', 'missed')
    AND COALESCE(shed.parent_location_id, g.park_id, CASE WHEN oi.scope_type = 'park' THEN oi.scope_id END) = (SELECT location_id FROM park)
),
override_rows AS (
  SELECT override_vaccine_code, original_drive_date, override_date, reason
  FROM candidate_vaccines
  WHERE override_vaccine_code IS NOT NULL
    AND override_date > original_drive_date
)
,
upserted AS (
INSERT INTO vaccination_drive_date_overrides (
  tenant_id, park_id, vaccine_code, original_drive_date, override_date, reason, created_by, created_at
)
SELECT '${tenant_id}'::uuid, park.location_id, override_rows.override_vaccine_code,
       override_rows.original_drive_date, override_rows.override_date, override_rows.reason,
       '${actor_id}'::uuid, now()
FROM park
JOIN override_rows ON true
ON CONFLICT (tenant_id, park_id, (lower(btrim(vaccine_code))), original_drive_date)
WHERE canceled_at IS NULL
DO UPDATE SET
  override_date = EXCLUDED.override_date,
  reason = EXCLUDED.reason,
  created_by = EXCLUDED.created_by,
  created_at = EXCLUDED.created_at
RETURNING 1
)
SELECT count(*) FROM upserted;
SQL
}

run_goat_shed_integrity_proof() {
  echo "==> seed-closeout: goat-shed-integrity proof"
  if [ "$dry_run" -eq 1 ]; then
    printf '    bash tools/dev/check-goat-shed-integrity.sh\n'
    return
  fi
  bash "$repo/tools/dev/check-goat-shed-integrity.sh"
}

india_schedule_window() {
  python3 - <<'PY'
from datetime import datetime
try:
    from zoneinfo import ZoneInfo
    now = datetime.now(ZoneInfo("Asia/Kolkata"))
except Exception:
    now = datetime.now().astimezone()
print(f"{now.year}-12-31T23:59:59+05:30")
PY
}

run_required_projectors() {
  # U7 (operational-kernel-5k-50k-scale-envelope ADR): the process-integrity, vaccination
  # shed/execution/operations screens are served from canonical indexed SQL at the 5k-50k
  # envelope, so their projection tables + projector commands were dropped/removed. No
  # recompute is required for those screens (seed-green is a canonical-read 200). Only the
  # SURVIVING indexed summary — the vaccination eligibility rollup — is recomputed here.
  run_go_cmd vaccination-eligibility-rollup-recompute -tenant-id "$tenant_id"
}

# Herd Signals projections (migration 000192): herd_signal_tag_latest and
# herd_signal_activity_windows are both DERIVED from herd_signal_packets --
# 000192 says so in its own comment ("Materialized from herd_signal_packets").
# The operational-kernel-5k-50k-scale-envelope ADR requires a new projection
# table to be rebuildable from canonical data through this closeout, so it is
# registered here rather than existing only as a one-off script.
#
# The rebuild runs the REAL ingest service (backend/cmd/seed-herd-signals-oci
# replays the capture through app.Service.IngestPackets). It must never be
# replaced with SQL that re-derives movement/pattern state: a seed that computes
# the classification itself agrees only with itself, and the previous hand-rolled
# version had already invented a `stationary` state that is not in the vocabulary.
#
# Herd Signals is a local-capture dev feature today, so the rebuild is skipped
# when the capture is not on this machine -- and the skip is PRINTED, never
# silent. Point it at a specific capture with GOATOS_HERD_SIGNALS_CAPTURE_CSV.
#
# Partitioning and retention for these two tables are recorded in the Herd
# Signals system-design document, not here; this is the rebuild path only.
run_herd_signals_projections() {
  if [ "${GOATOS_SEED_CLOSEOUT_RUN_HERD_SIGNALS:-1}" = "0" ]; then
    echo "==> seed-closeout: skip herd-signals projections (GOATOS_SEED_CLOSEOUT_RUN_HERD_SIGNALS=0)"
    return
  fi
  local csv="${GOATOS_HERD_SIGNALS_CAPTURE_CSV:-/Users/ravi/mesha/local-data/honeycomm-gateway-capture/decoded_ear_tags.csv}"
  local gateway="${GOATOS_HERD_SIGNALS_GATEWAY_ID:-honeycomm-gateway-001}"
  if [ "$dry_run" -eq 0 ] && [ ! -f "$csv" ]; then
    echo "==> seed-closeout: skip herd-signals projections (capture not on this machine: ${csv})"
    return
  fi
  run_go_cmd seed-herd-signals-oci -tenant-id "$tenant_id" -gateway-id "$gateway" -csv "$csv"
}

run_vaccination_drive_batching() {
  if [ "${GOATOS_SEED_CLOSEOUT_RUN_VACCINATION_SWEEPER:-1}" = "0" ]; then
    echo "==> seed-closeout: skip vaccination drive batching (GOATOS_SEED_CLOSEOUT_RUN_VACCINATION_SWEEPER=0)"
    return
  fi
  if [ ! -d "$repo/backend/cmd/generate-vaccination-obligations" ] || [ ! -d "$repo/backend/cmd/obligation-sweeper" ]; then
    echo "==> seed-closeout: skip vaccination drive batching (generation/sweeper commands not present)"
    return
  fi

  local actor_id="${GOATOS_SWEEPER_ACTOR_ID:-${GOATOS_LOCAL_USER_ID:-90000000-0000-4000-8000-000000000101}}"
  local as_of="${GOATOS_SEED_CLOSEOUT_SWEEP_AS_OF:-${GOATOS_SWEEPER_AS_OF:-}}"
  local due_before="${GOATOS_SEED_CLOSEOUT_SWEEP_DUE_BEFORE:-${GOATOS_SWEEPER_DUE_BEFORE:-}}"
  if [ -z "${due_before// }" ]; then
    due_before="$(india_schedule_window)"
  fi

  if [ "$dry_run" -eq 1 ]; then
    run_go_cmd generate-vaccination-obligations -tenant-id "$tenant_id"
    apply_expected_drive_variant_inputs
    run_goat_shed_integrity_proof
    if [ -n "${as_of// }" ]; then
      run_go_cmd obligation-sweeper -tenant-id "$tenant_id" -actor-id "$actor_id" -as-of "$as_of" -due-before "$due_before" -mark-missed=false -sweep-reminders=false -sweep-escalations=false -timeout 5m
    else
      run_go_cmd obligation-sweeper -tenant-id "$tenant_id" -actor-id "$actor_id" -due-before "$due_before" -mark-missed=false -sweep-reminders=false -sweep-escalations=false -timeout 5m
    fi
    echo "==> seed-closeout: vaccination-drive-clubbing-proof"
    printf '    GOATOS_DRIVE_CLUBBING_TO=%q bash tools/dev/check-vaccination-drive-clubbing-proof.sh\n' "${GOATOS_DRIVE_CLUBBING_TO:-${due_before%%T*}}"
    return
  fi

  if [ -n "${as_of// }" ]; then
    run_go_cmd generate-vaccination-obligations -tenant-id "$tenant_id" -as-of "$as_of"
  else
    run_go_cmd generate-vaccination-obligations -tenant-id "$tenant_id"
  fi
  apply_expected_drive_variant_inputs
  run_goat_shed_integrity_proof
  local sweep_limit="${GOATOS_SEED_CLOSEOUT_SWEEP_PASSES:-8}"
  local previous_leftover=""
  local leftover=""
  local pass
  for pass in $(seq 1 "$sweep_limit"); do
    if [ "$pass" -gt 1 ]; then
      echo "==> seed-closeout: obligation-sweeper fixed-point pass ${pass}/${sweep_limit}"
    fi
    if [ -n "${as_of// }" ]; then
      run_go_cmd obligation-sweeper -tenant-id "$tenant_id" -actor-id "$actor_id" -as-of "$as_of" -due-before "$due_before" -mark-missed=false -sweep-reminders=false -sweep-escalations=false -timeout 5m
    else
      run_go_cmd obligation-sweeper -tenant-id "$tenant_id" -actor-id "$actor_id" -due-before "$due_before" -mark-missed=false -sweep-reminders=false -sweep-escalations=false -timeout 5m
    fi
    leftover="$(
      cd "$repo/backend"
      go run ./cmd/seed-state-check -tenant-id "$tenant_id" -mode closeout >/dev/null
      psql "$DATABASE_URL" -qAt -v ON_ERROR_STOP=1 -c "
      SELECT count(*)
      FROM obligation_instances oi
      JOIN protocol_versions pv ON pv.protocol_version_id = oi.protocol_version_id
      JOIN protocol_definitions pd ON pd.protocol_id = pv.protocol_id
      WHERE oi.tenant_id = '$tenant_id'::uuid
        AND pd.category = 'vaccination'
        AND oi.batch_id IS NULL
        AND oi.status IN ('scheduled', 'due')
        AND oi.due_at <= '$due_before'::timestamptz
    "
    )"
    leftover="${leftover//[[:space:]]/}"
    if [ "$leftover" = "0" ]; then
      break
    fi
    if [ -n "$previous_leftover" ] && [ "$leftover" -ge "$previous_leftover" ]; then
      break
    fi
    previous_leftover="$leftover"
  done
  if [ "${leftover//[[:space:]]/}" != "0" ]; then
    echo "seed-closeout: vaccination drive batching left ${leftover} in-window scheduled obligations unbatched through ${due_before}" >&2
    exit 1
  fi

  if [ "${GOATOS_SEED_CLOSEOUT_RUN_VACCINATION_CLUBBING_PROOF:-1}" != "0" ]; then
    echo "==> seed-closeout: vaccination-drive-clubbing-proof"
    GOATOS_DRIVE_CLUBBING_TO="${GOATOS_DRIVE_CLUBBING_TO:-${due_before%%T*}}" \
      bash "$repo/tools/dev/check-vaccination-drive-clubbing-proof.sh"
  fi
  run_goat_shed_integrity_proof
}

# BUG-010: the CPT operator-drive contract documented a DB comparison against
# expected-drive-schedules.json that nothing ever ran, so a reseed could finish "clean" while
# breaching the per-operator animal cap, fanning out past active_operators_per_day, assigning
# an operator outside the contract, materializing pre-business-date drive work, or presenting
# superseded / zero-obligation shell batches as the schedule. This runs that comparison
# against real rows and FAILS the closeout. Opt-in by expectation file, because the
# expectation set is packet-specific: set GOATOS_EXPECTED_DRIVE_SCHEDULES (the CPT reseed
# target does). Set GOATOS_EXPECTED_DRIVE_VARIANT=<variant id> to additionally compare the
# exact per-date rows of one named variant after applying that variant's drive-policy input.
run_expected_drive_schedule_proof() {
  local expected="${GOATOS_EXPECTED_DRIVE_SCHEDULES:-}"
  if [ -z "${expected// }" ]; then
    echo "==> seed-closeout: skip expected-drive-schedules proof (GOATOS_EXPECTED_DRIVE_SCHEDULES not set)"
    return
  fi
  if [ ! -f "$expected" ]; then
    echo "seed-closeout: GOATOS_EXPECTED_DRIVE_SCHEDULES=${expected} does not exist" >&2
    exit 2
  fi
  local checker
  checker="$(cd "$(dirname "$expected")" && pwd)/check-expected-drive-schedules.mjs"
  if [ ! -f "$checker" ]; then
    echo "seed-closeout: expected-drive packet $(dirname "$expected") has no check-expected-drive-schedules.mjs" >&2
    exit 2
  fi
  echo "==> seed-closeout: expected-drive-schedules proof (${expected})"
  if [ "$dry_run" -eq 1 ]; then
    printf '    node %q --self-test\n' "$checker"
    printf '    GOATOS_TENANT_ID=%q node %q --expected %q\n' "$tenant_id" "$checker" "$expected"
    return
  fi
  node "$checker" --self-test
  GOATOS_TENANT_ID="$tenant_id" node "$checker" --expected "$expected"
}

run_cpt_passport_display_proof() {
  if [ "${GOATOS_RUN_CPT_PASSPORT_DISPLAY_PROOF:-0}" != "1" ]; then
    echo "==> seed-closeout: skip CPT passport display proof (GOATOS_RUN_CPT_PASSPORT_DISPLAY_PROOF=1 not set)"
    return
  fi
  local checker="$repo/tools/dev/check-cpt-vaccination-passport-display.mjs"
  if [ ! -f "$checker" ]; then
    echo "seed-closeout: missing CPT passport display checker at ${checker}" >&2
    exit 2
  fi
  if [ -z "${GOATOS_API_BASE_URL:-}" ]; then
    echo "==> seed-closeout: skip CPT passport display proof (GOATOS_API_BASE_URL not set)"
    return
  fi
  if [ "$dry_run" -eq 1 ]; then
    printf '    GOATOS_TENANT_ID=%q node %q\n' "$tenant_id" "$checker"
    return
  fi
  echo "==> seed-closeout: CPT passport display proof"
  GOATOS_TENANT_ID="$tenant_id" node "$checker"
}

# The vaccination reminder ladder addresses its audience by MODULE DUTY (position_module_duties,
# module pc.vaccination, duty execute|manage) -- see backend/internal/kernelstages/reminder_cadence.go.
# Before that, the ladder addressed a literal position-code list that had silently drifted away from
# what the roster seeder writes, so the operator who runs the drive was never reminded and NOTHING
# failed: the sweep queued the one seat that still resolved and reported success.
#
# A documented audience with no executable check is not a gate. This asserts, on real rows, that every
# configured reminder target actually resolves to at least one ACTIVE seat -- tenant-wide for each
# duty, and per park for any park that carries vaccination work. A park with vaccination obligations
# and no execute-duty seat is a broken seed, not a warning.
assert_reminder_audience_resolves() {
  if [ "${GOATOS_SEED_CLOSEOUT_ASSERT_REMINDER_AUDIENCE:-1}" = "0" ]; then
    echo "==> seed-closeout: skip reminder-audience assertion (GOATOS_SEED_CLOSEOUT_ASSERT_REMINDER_AUDIENCE=0)"
    return
  fi
  echo "==> seed-closeout: reminder-audience resolves to active seats"
  if [ "$dry_run" -eq 1 ]; then
    printf '    # assert every pc.vaccination execute|manage reminder target resolves to >=1 active seat\n'
    return
  fi
  if [ -z "${DATABASE_URL:-}" ]; then
    echo "seed-closeout: DATABASE_URL is required to assert the reminder audience" >&2
    exit 2
  fi

  local missing_duties
  missing_duties="$(psql "$DATABASE_URL" -qAt -v ON_ERROR_STOP=1 -c "
    SELECT string_agg(wanted.duty_type, ',' ORDER BY wanted.duty_type)
    FROM (VALUES ('execute'), ('manage')) AS wanted(duty_type)
    WHERE NOT EXISTS (
      SELECT 1
      FROM position_module_duties pmd
      JOIN workforce_positions p
        ON p.tenant_id = pmd.tenant_id
       AND p.position_code = pmd.position_code
       AND p.status = 'active'
      JOIN workforce_members m
        ON m.tenant_id = p.tenant_id
       AND m.workforce_member_id = p.workforce_member_id
       AND m.status = 'active'
      WHERE pmd.tenant_id = '${tenant_id}'::uuid
        AND pmd.module_code = 'pc.vaccination'
        AND pmd.duty_type = wanted.duty_type
        AND pmd.status = 'active'
    )
  ")"
  missing_duties="${missing_duties//[[:space:]]/}"
  if [ -n "$missing_duties" ]; then
    echo "seed-closeout: vaccination reminder audience unresolvable -- no active seat holds pc.vaccination duty: ${missing_duties}" >&2
    echo "seed-closeout: reminders would be queued to nobody for that duty. Run seed-position-duties / seed-roster-real." >&2
    exit 1
  fi

  local parks_without_operator
  parks_without_operator="$(psql "$DATABASE_URL" -qAt -v ON_ERROR_STOP=1 -c "
    WITH work_parks AS (
      SELECT DISTINCT COALESCE(shed.parent_location_id, g.park_id,
                               CASE WHEN oi.scope_type IN ('park','center') THEN oi.scope_id END) AS park_id
      FROM obligation_instances oi
      JOIN protocol_versions pv
        ON pv.tenant_id = oi.tenant_id
       AND pv.protocol_version_id = oi.protocol_version_id
      JOIN protocol_definitions pd
        ON pd.tenant_id = pv.tenant_id
       AND pd.protocol_id = pv.protocol_id
       AND pd.category = 'vaccination'
      LEFT JOIN goats g
        ON g.tenant_id = oi.tenant_id
       AND oi.target_type = 'goat'
       AND g.goat_id = oi.target_id
      LEFT JOIN locations shed
        ON shed.tenant_id = oi.tenant_id
       AND shed.location_id = COALESCE(CASE WHEN oi.scope_type = 'shed' THEN oi.scope_id END, g.shed_id)
       AND shed.location_type = 'shed'
      WHERE oi.tenant_id = '${tenant_id}'::uuid
        AND oi.status IN ('scheduled','due','in_progress')
    )
    SELECT count(*)
    FROM work_parks wp
    WHERE wp.park_id IS NOT NULL
      AND NOT EXISTS (
        SELECT 1
        FROM workforce_positions p
        JOIN position_module_duties pmd
          ON pmd.tenant_id = p.tenant_id
         AND pmd.position_code = p.position_code
         AND pmd.module_code = 'pc.vaccination'
         AND pmd.duty_type = 'execute'
         AND pmd.status = 'active'
        JOIN workforce_members m
          ON m.tenant_id = p.tenant_id
         AND m.workforce_member_id = p.workforce_member_id
         AND m.status = 'active'
        WHERE p.tenant_id = '${tenant_id}'::uuid
          AND p.scope_type = 'center'
          AND p.scope_id = wp.park_id
          AND p.status = 'active'
      )
  ")"
  parks_without_operator="${parks_without_operator//[[:space:]]/}"
  if [ "${parks_without_operator:-0}" != "0" ]; then
    echo "seed-closeout: ${parks_without_operator} park(s) carry open vaccination work but have no active pc.vaccination execute seat" >&2
    echo "seed-closeout: the reminder ladder would notify no operator for those parks." >&2
    exit 1
  fi
}

run_calendar_projectors() {
  if [ "${GOATOS_SEED_CLOSEOUT_RUN_CALENDAR:-1}" = "0" ]; then
    echo "==> seed-closeout: skip calendar projectors (GOATOS_SEED_CLOSEOUT_RUN_CALENDAR=0)"
    return
  fi
}

run_counts_projectors() {
  if [ "${GOATOS_SEED_CLOSEOUT_RUN_COUNTS:-0}" = "0" ]; then
    echo "==> seed-closeout: skip counts projector (set GOATOS_SEED_CLOSEOUT_RUN_COUNTS=1 with GOATOS_COUNTS_PARK_ID and GOATOS_COUNTS_TARGET_DATE)"
    return
  fi
  if [ -z "${GOATOS_COUNTS_PARK_ID:-}" ] || [ -z "${GOATOS_COUNTS_TARGET_DATE:-}" ]; then
    echo "GOATOS_COUNTS_PARK_ID and GOATOS_COUNTS_TARGET_DATE are required when GOATOS_SEED_CLOSEOUT_RUN_COUNTS=1" >&2
    exit 2
  fi
  run_go_cmd counts-projection-recompute \
    -tenant-id "$tenant_id" \
    -park-id "$GOATOS_COUNTS_PARK_ID" \
    -target-date "$GOATOS_COUNTS_TARGET_DATE"
}

echo "seed-closeout: tenant=${tenant_id}"
# VACC-REV-02 gate: refuse to run closeout against a RESET_REQUIRED (failed) seed. A half-seeded
# database must never be dressed up as healthy by rebuilding read models on top of it.
if [ "$dry_run" -eq 0 ] && [ -d "$repo/backend/cmd/seed-state-check" ]; then
  echo "==> seed-closeout: seed-state-check (mode=closeout)"
  (cd "$repo/backend" && go run ./cmd/seed-state-check -tenant-id "$tenant_id" -mode closeout)
fi
# shed_profiles is the authoritative destination-cohort config the shifting completion path reads
# (identity.resolveDestinationTag). Derive it from canonical goats/locations BEFORE the shifting and
# vaccination proofs, so a shed a movement targets already has an active configured profile.
run_go_cmd seed-shed-profiles -tenant-id "$tenant_id"
run_goat_shed_integrity_proof
run_required_projectors
run_herd_signals_projections
run_vaccination_drive_batching
run_expected_drive_schedule_proof
assert_reminder_audience_resolves
run_cpt_passport_display_proof
run_calendar_projectors
run_counts_projectors
echo "seed-closeout: complete"
