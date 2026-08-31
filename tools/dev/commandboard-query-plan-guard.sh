#!/usr/bin/env bash
# Executable query-plan gate for GET /vaccination/command and its drilldowns.
#
# WHY THIS IS A SCRIPT AND NOT A BARE `go test`: the gate must FAIL when it cannot run, and it must
# be runnable on a machine where Docker/Colima is disallowed. A Postgres-gated Go test skips
# silently by default, so wiring `go test` straight into CI produced a guard that reported green
# having asserted nothing — the exact failure mode this repo already documented once (see
# uuidFromSuffix in commandboard_kpi_animal_grain_test.go) and then reproduced here.
#
# Resolution order matches validate-sqlc-query-plans.sh, which solved the same problem for the SQLC
# plan proof:
#   1. GOATOS_PGTEST_ADMIN_DSN, if the caller supplied one.
#   2. GOATOS_SQLC_PLAN_ADMIN_DSN, so one export serves both plan gates.
#   3. The maintainer OCI dev Postgres clone, from the local-only gitignored env file.
#   4. Docker, via the ordinary pgtest container harness.
# If none resolve, this FAILS. A plan gate that cannot reach a database has not passed.
#
# The harness creates its own template and per-test clone databases (goatos_tmpl_* / goatos_test_*)
# and drops them on teardown. It never touches an existing application database.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"

admin_dsn="${GOATOS_PGTEST_ADMIN_DSN:-${GOATOS_SQLC_PLAN_ADMIN_DSN:-}}"

# Resolution 3: an operator-named env file. GOATOS_OCI_DB_ENV / GOATOS_OCI_ENV_FILE are deliberate
# exports, so naming one is consent. The well-known $HOME path is NOT: this harness CREATEs a
# template database, pipes every migration through it, clones it per test and DROPs it on teardown,
# and auto-discovering a shared dev server for that is not something an operator opted into by
# having a file on disk. It requires GOATOS_PGTEST_ALLOW_EXTERNAL=1, said out loud.
if [[ -z "$admin_dsn" ]]; then
  default_env_file=""
  if [[ "${GOATOS_PGTEST_ALLOW_EXTERNAL:-}" == "1" ]]; then
    default_env_file="$HOME/mesha/local-data/goatos-stg-to-oci/oci-goatos-db.env"
  fi
  for env_file in \
    "${GOATOS_OCI_DB_ENV:-}" \
    "${GOATOS_OCI_ENV_FILE:-}" \
    "$default_env_file"
  do
    if [[ -n "$env_file" && -f "$env_file" ]]; then
      # shellcheck disable=SC1090
      . "$env_file"
      admin_dsn="${GOATOS_OCI_DATABASE_URL:-${DATABASE_URL:-}}"
      [[ -n "$admin_dsn" ]] && break
    fi
  done
fi

# The tests this gate MUST have run. Named explicitly, and verified below, because
# `go test -run` exits 0 when its pattern matches NOTHING: renaming or deleting these would
# otherwise turn the gate green while it asserted nothing -- the same skip-green failure that made
# the Docker-only version of this gate worthless.
REQUIRED_TESTS=(
  TestCommandBoardQueryPlansAreBoundedByTheAnswerNotTheTenant
  TestCommandBoardPlanGuardRejectsThePreFixShapes
  TestCommandBoardPlanGuardRejectsTheTenantWideDrilldown
  TestCommandBoardTileAndDrilldownRangeOverTheSameAnimals
  TestCohortExceptionTileAndDrawerRangeOverTheSameAnimals
  TestCommandBoardPlanGateCoversEverySQLConst
)
# Both predicate-drift tests are named explicitly rather than caught by one prefix: the cohort one
# does not start with "TestCommandBoard", and relying on a prefix is how a test quietly stops being
# part of the gate.
TESTS='TestCommandBoard|TestCohortExceptionTileAndDrawer'
# --self-test is the DB-FREE half of this gate, and it is what `make guardrails` runs.
#
# The full gate needs a PostgreSQL server, so it cannot live in the static guardrail sweep that runs
# on every machine. Its WIRING can be checked without one: that every test this gate claims to
# require actually exists, and that every command-board SQL statement is plan-covered. Without this,
# deleting a row from the plan table left all the named tests passing and the gate satisfied.
if [[ "${1:-}" == "--self-test" ]]; then
  missing=()
  for name in "${REQUIRED_TESTS[@]}"; do
    if ! grep -qR "func ${name}(" backend/internal/vaccinationexecution/adapters/postgres/; then
      missing+=("$name")
    fi
  done
  if (( ${#missing[@]} > 0 )); then
    echo "command-board query plans: REQUIRED_TESTS names tests that do not exist:" >&2
    printf '  %s\n' "${missing[@]}" >&2
    exit 1
  fi
  ( cd backend && go test ./internal/vaccinationexecution/adapters/postgres/ \
      -run TestCommandBoardPlanGateCoversEverySQLConst -count=1 ) || exit 1
  echo "command-board query plans: wiring self-test passed (${#REQUIRED_TESTS[@]} required tests exist; every statement plan-covered)."
  exit 0
fi

LOG="$(mktemp -t commandboard-query-plan-guard)"
trap 'rm -f "$LOG"' EXIT

# assert_ran_all fails when any required test did not report a PASS, however the run exited.
assert_ran_all() {
  local missing=()
  local name
  for name in "${REQUIRED_TESTS[@]}"; do
    if ! grep -qE "^(--- )?PASS: ${name}([[:space:]]|$)" "$LOG"; then
      missing+=("$name")
    fi
  done
  if (( ${#missing[@]} > 0 )); then
    echo "command-board query plans: these required tests did not run and pass:" >&2
    printf '  %s\n' "${missing[@]}" >&2
    echo "A plan gate that asserts nothing is not a passing plan gate. If a test was renamed," >&2
    echo "update REQUIRED_TESTS in $0 deliberately." >&2
    exit 1
  fi
  echo "command-board query plans: all ${#REQUIRED_TESTS[@]} required tests ran and passed."
}

if [[ -n "$admin_dsn" ]]; then
  # Never echo the DSN: it carries credentials.
  echo "── command-board query plans: using a supplied PostgreSQL server (no Docker)"
  cd backend
  set +e
  GOATOS_RUN_POSTGRES_TESTS=1 \
  GOATOS_PGTEST_ADMIN_DSN="$admin_dsn" \
    go test ./internal/vaccinationexecution/adapters/postgres/ -run "$TESTS" -count=1 -v 2>&1 | tee "$LOG"
  status=${PIPESTATUS[0]}
  set -e
  [[ $status -eq 0 ]] || exit "$status"
  assert_ran_all
  exit 0
fi

if command -v docker >/dev/null 2>&1; then
  echo "── command-board query plans: using the Docker pgtest harness"
  cd backend
  set +e
  GOATOS_RUN_POSTGRES_TESTS=1 GOATOS_REQUIRE_DOCKER=1 \
    go test ./internal/vaccinationexecution/adapters/postgres/ -run "$TESTS" -count=1 -v 2>&1 | tee "$LOG"
  status=${PIPESTATUS[0]}
  set -e
  [[ $status -eq 0 ]] || exit "$status"
  assert_ran_all
  exit 0
fi

echo "command-board query plans: no PostgreSQL available." >&2
echo "Set GOATOS_PGTEST_ADMIN_DSN (or GOATOS_SQLC_PLAN_ADMIN_DSN), provide the OCI dev env file," >&2
echo "or make Docker available. This gate fails rather than skipping: a plan gate that cannot" >&2
echo "reach a database has not passed." >&2
exit 1
