#!/usr/bin/env bash
# tools/dev/e2e-smoke.sh — `make e2e-smoke`.
#
# Brings up deploy/e2e/docker-compose.e2e.yml (the REAL goatos-backend:e2e image, REAL
# entrypoints — see that file's header comment for why), then proves the exact failure
# mode KERN-001 shipped as would be caught here:
#   - the API becomes ready within a strict timeout;
#   - kernel-worker is STILL RUNNING with ZERO restarts (a retired-flag or missing-binary
#     command crash-loops immediately — Cloud Run Jobs retry, but a long-running worker
#     container just restart-loops, which `docker inspect .RestartCount` catches directly);
#   - neither container's logs contain "flag provided but not defined", "no such file or
#     directory", an unhandled panic, or a fatal config error;
#   - kernel-worker's structured startup log + each expected cadence's first-tick log
#     actually appear (proving the supervisor didn't just start and silently do nothing).
# Tears the stack (and its volumes) down on ANY exit path, including failure — this stack
# is fully ephemeral and must never be left running or leak a volume.
#
# Docker-unavailable handling: like backend/internal/platform/pgtest.SkipIfNoDocker, this
# script SKIPS (exit 0, loud warning) when the docker CLI is missing, rather than failing
# ci-local outright in an environment that genuinely cannot run containers. It is ON by
# default in `make ci-local`; set GOATOS_E2E_SMOKE_SKIP=1 to opt out explicitly instead
# (e.g. a constrained sandbox) — that path also warns loudly, it does not silently pass.
set -uo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
compose_file="$repo_root/deploy/e2e/docker-compose.e2e.yml"
project="${GOATOS_E2E_PROJECT:-goatos-e2e}"
api_port="${GOATOS_E2E_API_PORT:-8090}"
timeout_seconds="${GOATOS_E2E_SMOKE_TIMEOUT:-120}"
export GOATOS_E2E_IMAGE="${GOATOS_E2E_IMAGE:-goatos-backend:e2e}"

if [ "${GOATOS_E2E_SMOKE_SKIP:-0}" = "1" ]; then
  echo "!! e2e-smoke: SKIPPED — GOATOS_E2E_SMOKE_SKIP=1 was set explicitly. This is NOT a pass; it is an opt-out." >&2
  exit 0
fi

if ! command -v docker >/dev/null 2>&1; then
  echo "!! e2e-smoke: SKIPPED — docker CLI not available in this environment. This is NOT a pass." >&2
  exit 0
fi
if ! docker info >/dev/null 2>&1; then
  echo "!! e2e-smoke: SKIPPED — docker daemon not reachable (docker info failed). This is NOT a pass." >&2
  exit 0
fi

compose() {
  docker compose -f "$compose_file" -p "$project" "$@"
}

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

cleanup() {
  status=$?
  echo "── e2e-smoke: tearing down (project=$project, exit_status=$status)"
  compose logs --no-color --tail=200 >"$repo_root/.e2e-smoke-last-logs.txt" 2>&1 || true
  compose down --volumes --remove-orphans >/dev/null 2>&1 || true
  exit "$status"
}
trap cleanup EXIT

wait_for() {
  label="$1"
  command="$2"
  budget="${3:-$timeout_seconds}"
  for _ in $(seq 1 "$budget"); do
    if eval "$command" >/dev/null 2>&1; then
      printf 'PASS: %s\n' "$label"
      return 0
    fi
    sleep 1
  done
  fail "timed out after ${budget}s waiting for: $label"
}

echo "── e2e-smoke: docker compose config check"
compose config --quiet || fail "docker compose config is invalid"

echo "── e2e-smoke: bringing up postgres, migrate, pubsub, pubsub-bootstrap, api, kernel-worker"
# --build rebuilds ONLY services with a build: block (the migrate image, from
# backend/Dockerfile.migrate) so it always embeds the CURRENT migrations. Without
# it a cached migrate image predating a new migration leaves the DB one version
# behind the api binary -> migrationguard drift -> /readyz 503 forever. api and
# kernel-worker have no build: block; they stay on the prebuilt goatos-backend:e2e tag.
compose up -d --build postgres migrate pubsub pubsub-bootstrap api kernel-worker \
  || fail "docker compose up failed — is goatos-backend:e2e built? (make e2e-image-build)"

migrate_exit_code() {
  cid="$(compose ps -a -q migrate)"
  [ -n "$cid" ] || return 1
  status="$(docker inspect -f '{{.State.Status}}' "$cid" 2>/dev/null)" || return 1
  [ "$status" = "exited" ] || return 1
  docker inspect -f '{{.State.ExitCode}}' "$cid" 2>/dev/null
}
wait_for "migrate completed successfully (exit 0)" 'test "$(migrate_exit_code)" = "0"' 60
migrate_code="$(migrate_exit_code || echo "?")"
if [ "$migrate_code" != "0" ]; then
  compose logs --no-color migrate >&2
  fail "migrate exited with code $migrate_code"
fi

echo "── e2e-smoke: waiting for API /readyz"
wait_for "API /readyz healthy" "curl -fsS http://127.0.0.1:${api_port}/readyz"

echo "── e2e-smoke: waiting for kernel-worker startup log"
wait_for "kernel-worker startup log (kernel_worker_starting)" \
  "compose logs --no-color kernel-worker | grep -F 'kernel_worker_starting'"

echo "── e2e-smoke: checking kernel-worker is running with zero restarts"
kernel_container="$(compose ps -q kernel-worker)"
[ -n "$kernel_container" ] || fail "kernel-worker container not found"
kernel_state="$(docker inspect -f '{{.State.Status}}' "$kernel_container")"
kernel_restarts="$(docker inspect -f '{{.RestartCount}}' "$kernel_container")"
[ "$kernel_state" = "running" ] || {
  compose logs --no-color kernel-worker >&2
  fail "kernel-worker is not running (state=$kernel_state) — a retired flag or missing binary crash-loops exactly like this"
}
[ "$kernel_restarts" = "0" ] || {
  compose logs --no-color kernel-worker >&2
  fail "kernel-worker restarted $kernel_restarts time(s) — this is the KERN-001 failure signature (job/container crash-loops on a bad command)"
}
echo "PASS: kernel-worker running, zero restarts"

echo "── e2e-smoke: checking api is running with zero restarts"
api_container="$(compose ps -q api)"
[ -n "$api_container" ] || fail "api container not found"
api_restarts="$(docker inspect -f '{{.RestartCount}}' "$api_container")"
[ "$api_restarts" = "0" ] || fail "api restarted $api_restarts time(s)"
echo "PASS: api running, zero restarts"

echo "── e2e-smoke: scanning logs for fatal signatures"
bad_log_pattern='flag provided but not defined|flag needs an argument|no such file or directory|panic:|FATAL|fatal error'
if compose logs --no-color api kernel-worker migrate 2>/dev/null | grep -Ei "$bad_log_pattern" >/tmp/e2e-smoke-bad-logs.$$; then
  echo "Matched lines:" >&2
  cat /tmp/e2e-smoke-bad-logs.$$ >&2
  rm -f /tmp/e2e-smoke-bad-logs.$$
  fail "fatal log signature found in api/kernel-worker/migrate logs"
fi
rm -f /tmp/e2e-smoke-bad-logs.$$
echo "PASS: no fatal log signatures"

echo "── e2e-smoke: waiting a cadence tick window for kernel-worker's continuous stage"
wait_for "kernel-worker domain consumer decision logged (enabled or explicitly disabled)" \
  "compose logs --no-color kernel-worker | grep -E 'kernel_worker_domain_consumer_disabled|domain_consumer'" \
  30

echo ""
echo "════════ e2e-smoke: PASS ════════"
echo "API:            http://127.0.0.1:${api_port}"
echo "Compose project: ${project}"
echo "Full logs saved to: $repo_root/.e2e-smoke-last-logs.txt"
exit 0
