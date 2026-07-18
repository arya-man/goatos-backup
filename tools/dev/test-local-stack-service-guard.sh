#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
service_script="$repo_root/tools/dev/local-stack-service.sh"
supervisor_script="$repo_root/tools/dev/run-local-stack-supervised.sh"
seed_closeout_script="$repo_root/tools/dev/seed-closeout.sh"
local_kernel_loop="$repo_root/tools/dev/local-kernel-loop.sh"

fail() {
  echo "local-stack-service-guard: FAIL: $*" >&2
  exit 1
}

grep -q 'kill_orphan_supervisors' "$service_script" \
  || fail "local service restart must stop orphan supervisors before port cleanup"

grep -q 'kill_port_listeners "$api_port"' "$service_script" \
  || fail "local service restart must free the API port"

grep -q 'kill_port_listeners "$web_port"' "$service_script" \
  || fail "local service restart must free the admin-web port"

grep -q 'show_port_owner "$api_port"' "$service_script" \
  || fail "local service status must show the API port owner"

grep -q 'show_port_owner "$web_port"' "$service_script" \
  || fail "local service status must show the admin-web port owner"

grep -q 'Refusing to reuse an existing Goat OS API' "$supervisor_script" \
  || fail "supervisor must not silently reuse an existing API process"

grep -q 'Refusing to reuse an existing Mesha admin-web' "$supervisor_script" \
  || fail "supervisor must not silently reuse an existing admin-web process"

grep -q 'GOATOS_ALLOW_TEMP_WORKTREE_LOCAL_STACK' "$supervisor_script" \
  || fail "supervisor must refuse temporary worktree local-stack launches by default"

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

echo "local-stack-service-guard: OK"
