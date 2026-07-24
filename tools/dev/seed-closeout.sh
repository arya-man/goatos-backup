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
  if [ "$variant" != "final_discussed_plan_et_tt_then_ppr_after_14_days" ]; then
    return
  fi
  if [ ! -f "$expected" ]; then
    echo "seed-closeout: GOATOS_EXPECTED_DRIVE_SCHEDULES=${expected} does not exist" >&2
    exit 2
  fi
  echo "==> seed-closeout: apply expected-drive variant input (${variant})"
  if [ "$dry_run" -eq 1 ]; then
    printf '    # derive CPT ET+TT/PPR original due dates from unbatched vaccination obligations and upsert postpone overrides\n'
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
           WHEN lower(btrim(v.vaccine_code)) = 'ppr' THEN 'PPR'
           WHEN replace(lower(btrim(v.vaccine_code)), '_', ' ') = 'blue tongue' THEN 'BLUE_TONGUE'
           ELSE NULL
         END AS override_vaccine_code,
         (oi.due_at AT TIME ZONE 'Asia/Kolkata')::date AS original_drive_date,
         CASE
           WHEN replace(replace(lower(btrim(v.vaccine_code)), '_', ' '), '+', ' ') = 'et tt' THEN DATE '2026-07-24'
           WHEN lower(btrim(v.vaccine_code)) = 'ppr' THEN DATE '2026-08-07'
           WHEN replace(lower(btrim(v.vaccine_code)), '_', ' ') = 'blue tongue' THEN DATE '2026-08-07'
           ELSE NULL
         END AS override_date,
         CASE
           WHEN replace(replace(lower(btrim(v.vaccine_code)), '_', ' '), '+', ' ') = 'et tt'
             THEN 'CPT validation override: ET+TT first on reviewed business date'
           WHEN lower(btrim(v.vaccine_code)) = 'ppr'
             THEN 'CPT validation override: ET+TT first, PPR after 14 days'
           WHEN replace(lower(btrim(v.vaccine_code)), '_', ' ') = 'blue tongue'
             THEN 'CPT validation override: align Blue Tongue with deferred PPR cohort'
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

  run_go_cmd generate-vaccination-obligations -tenant-id "$tenant_id"
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
run_vaccination_drive_batching
run_expected_drive_schedule_proof
run_calendar_projectors
run_counts_projectors
echo "seed-closeout: complete"
