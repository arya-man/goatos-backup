#!/usr/bin/env bash
set -euo pipefail
# Grep/sed patterns intentionally match literal shell variables.
# shellcheck disable=SC2016

repo_root="${GOATOS_LOCAL_STACK_GUARD_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
service_script="$repo_root/tools/dev/local-stack-service.sh"
supervisor_script="$repo_root/tools/dev/run-local-stack-supervised.sh"
seed_closeout_script="$repo_root/tools/dev/seed-closeout.sh"
local_kernel_loop="$repo_root/tools/dev/local-kernel-loop.sh"
admin_guard="$repo_root/apps/admin-web/scripts/lib/origin-main-local-stack-guard.mjs"
backend_main="$repo_root/backend/cmd/api/main.go"
agent_rules="$repo_root/AGENTS.md"
build_skill="$repo_root/.agents/skills/goatos-build/SKILL.md"
rehearsal_runbook="$repo_root/docs/runbooks/local-full-stack-rehearsal.md"
self_test_dir=""

fail() {
  echo "local-stack-service-guard: FAIL: $*" >&2
  exit 1
}

self_test() {
  local test_dir fixture
  test_dir="$(mktemp -d "${TMPDIR:-/tmp}/goatos-local-stack-guard.XXXXXX")"
  self_test_dir="$test_dir"
  fixture="$test_dir/fixture"
  trap cleanup_self_test EXIT

  local files=(
    tools/dev/local-stack-service.sh
    tools/dev/run-local-stack-supervised.sh
    tools/dev/seed-closeout.sh
    tools/dev/local-kernel-loop.sh
    apps/admin-web/scripts/run-local-next.mjs
    apps/admin-web/scripts/lib/origin-main-local-stack-guard.mjs
    backend/cmd/api/main.go
    AGENTS.md
    .agents/skills/goatos-build/SKILL.md
    docs/runbooks/local-full-stack-rehearsal.md
  )

  build_fixture() {
    rm -rf "$fixture"
    local file
    for file in "${files[@]}"; do
      mkdir -p "$fixture/$(dirname "$file")"
      cp "$repo_root/$file" "$fixture/$file"
    done
  }

  expect_rejected() {
    local label="$1"
    shift
    build_fixture
    "$@"
    if GOATOS_LOCAL_STACK_GUARD_ROOT="$fixture" bash "${BASH_SOURCE[0]}" >/dev/null 2>&1; then
      fail "self-test accepted mutant: $label"
    fi
  }

  build_fixture
  GOATOS_LOCAL_STACK_GUARD_ROOT="$fixture" bash "${BASH_SOURCE[0]}" >/dev/null

  expect_rejected "LaunchAgent without libpq" \
    sed -i.bak 's#/opt/homebrew/opt/libpq/bin:##g' "$fixture/tools/dev/local-stack-service.sh"
  expect_rejected "shared backend without local media signing" \
    sed -i.bak 's/export GOATOS_LOCAL_MEDIA_SIGNING_SECRET=/: missing local media signing secret=/' "$fixture/tools/dev/run-local-stack-supervised.sh"
  expect_rejected "shared stack inheriting ambient database" \
    sed -i.bak 's/unset DATABASE_URL GOATOS_E2E_DATABASE_URL/: ambient database leak/' "$fixture/tools/dev/run-local-stack-supervised.sh"
  expect_rejected "shared calendar retaining the overload-prone 3s deadline" \
    sed -i.bak 's/export GOATOS_PG_QUERY_TIMEOUT="15s"/: missing shared calendar headroom/' "$fixture/tools/dev/run-local-stack-supervised.sh"
  # shellcheck disable=SC2016
  expect_rejected "broad cleanup that can kill isolated E2E" \
    sed -i.bak 's/index(\$0, script)/index(\$0, "run-local-stack-supervised.sh")/' "$fixture/tools/dev/local-stack-service.sh"
  expect_rejected "missing origin drift watchdog" \
    sed -i.bak 's/origin_main_drifted/origin_ref_check_removed/g' "$fixture/tools/dev/run-local-stack-supervised.sh"

  echo "local-stack-service-guard self-test: OK"
}

cleanup_self_test() {
  if [ -n "${self_test_dir:-}" ]; then
    rm -rf "$self_test_dir"
    self_test_dir=""
  fi
}

if [ "${1:-}" = "--self-test" ]; then
  self_test
  exit 0
fi

grep -q 'kill_orphan_supervisors' "$service_script" \
  || fail "local service restart must stop orphan supervisors before port cleanup"

# shellcheck disable=SC2016
grep -q 'index(\$0, script)' "$service_script" \
  || fail "shared service cleanup must target only its exact supervisor script, never isolated E2E supervisors"

# shellcheck disable=SC2016
grep -q 'kill_port_listeners "$api_port"' "$service_script" \
  || fail "local service restart must free the API port"

# shellcheck disable=SC2016
grep -q 'kill_port_listeners "$web_port"' "$service_script" \
  || fail "local service restart must free the admin-web port"

# shellcheck disable=SC2016
grep -q 'show_port_owner "$api_port"' "$service_script" \
  || fail "local service status must show the API port owner"

# shellcheck disable=SC2016
grep -q 'show_port_owner "$web_port"' "$service_script" \
  || fail "local service status must show the admin-web port owner"

grep -q 'Refusing to reuse an existing Goat OS API' "$supervisor_script" \
  || fail "supervisor must not silently reuse an existing API process"

grep -q 'Refusing to reuse an existing Mesha admin-web' "$supervisor_script" \
  || fail "supervisor must not silently reuse an existing admin-web process"

grep -q 'GOATOS_ALLOW_TEMP_WORKTREE_LOCAL_STACK' "$supervisor_script" \
  || fail "supervisor must refuse temporary worktree local-stack launches by default"

grep -q '/opt/homebrew/opt/libpq/bin' "$service_script" \
  || fail "LaunchAgent PATH must include Homebrew libpq so psql-backed closeout cannot boot-loop"

grep -q 'export GOATOS_LOCAL_MEDIA_SIGNING_SECRET=' "$supervisor_script" \
  || fail "shared supervisor must provide the local media signing secret required by the backend"

grep -q 'sync_exact_origin_main' "$supervisor_script" \
  || fail "shared supervisor must fetch and fast-forward a clean checkout to exact origin/main before boot"

grep -q 'origin_main_drifted' "$supervisor_script" \
  || fail "shared supervisor must detect origin/main advancing after boot"

grep -q 'GOATOS_ORIGIN_MAIN_PREVERIFIED' "$supervisor_script" \
  || fail "shared supervisor must pass its authenticated origin/main proof to FE and BE children"

grep -q 'GOATOS_ORIGIN_MAIN_PREVERIFIED' "$admin_guard" \
  || fail "admin-web guard must accept supervisor proof while still comparing HEAD to origin/main"

grep -q 'GOATOS_ORIGIN_MAIN_PREVERIFIED' "$backend_main" \
  || fail "backend guard must accept supervisor proof while still comparing HEAD to origin/main"

grep -q 'unset DATABASE_URL GOATOS_E2E_DATABASE_URL' "$supervisor_script" \
  || fail "shared supervisor must discard ambient/E2E database URLs before resolving goatos-local-current"

grep -q 'export GOATOS_PG_QUERY_TIMEOUT="15s"' "$supervisor_script" \
  || fail "shared supervisor must keep calendar reads from false-failing at the production 3s deadline under local load"

grep -q 'calendar_data_plane_ready' "$supervisor_script" \
  || fail "shared supervisor must verify the authenticated vaccination calendar data plane, not only /readyz"

grep -q 'authenticated vaccination calendar data plane failed' "$supervisor_script" \
  || fail "shared supervisor must fail closed when readiness is green but the calendar data plane is broken"

grep -q 'kill_process_tree' "$supervisor_script" \
  || fail "supervisor cleanup must terminate go run/npm descendants so FE and BE restart atomically"

grep -q 'Refusing to start Mesha admin-web from a temporary worktree' "$repo_root/apps/admin-web/scripts/run-local-next.mjs" \
  || fail "admin-web direct dev wrapper must refuse temporary worktree launches by default"

grep -q 'run_vaccination_drive_batching' "$seed_closeout_script" \
  || fail "seed closeout must run vaccination generation + drive batching"

grep -q 'GOATOS_SEED_CLOSEOUT_SWEEP_DUE_BEFORE' "$seed_closeout_script" \
  || fail "seed closeout batching must expose an explicit schedule horizon override"

grep -q 'in-window scheduled obligations unbatched' "$seed_closeout_script" \
  || fail "seed closeout must fail if it leaves visible schedule-window obligations unbatched"

grep -q 'GOATOS_SWEEPER_ACTOR_ID' "$local_kernel_loop" \
  || fail "local kernel loop must provide an audited sweeper actor"

if grep -Eq -- '-project-calendar|-project-vaccination-read-models|vaccination-projection-worker' "$local_kernel_loop"; then
  fail "local kernel loop must not use removed vaccination projection worker/flags"
fi

if grep -q 'Using existing Goat OS API' "$supervisor_script"; then
  fail "stale API reuse shortcut is forbidden"
fi

if grep -q 'Using existing Mesha admin-web' "$supervisor_script"; then
  fail "stale admin-web reuse shortcut is forbidden"
fi

for rules_file in "$agent_rules" "$build_skill" "$rehearsal_runbook"; do
  grep -qi 'isolated E2E' "$rules_file" \
    || fail "$rules_file must document the isolated-E2E boundary"
  grep -qi 'exact origin/main' "$rules_file" \
    || fail "$rules_file must document the exact-origin/main shared-stack rule"
done

echo "local-stack-service-guard: OK"
