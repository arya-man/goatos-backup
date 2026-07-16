#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
service_script="$repo_root/tools/dev/local-stack-service.sh"
supervisor_script="$repo_root/tools/dev/run-local-stack-supervised.sh"

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

if grep -q 'Using existing Goat OS API' "$supervisor_script"; then
  fail "stale API reuse shortcut is forbidden"
fi

if grep -q 'Using existing Mesha admin-web' "$supervisor_script"; then
  fail "stale admin-web reuse shortcut is forbidden"
fi

echo "local-stack-service-guard: OK"
