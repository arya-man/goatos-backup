#!/usr/bin/env bash
# run-local-ci.sh — run the SAME required CI gates as .github/workflows/ci.yml,
# locally, job-for-job. Per AGENTS.md: a GitHub Actions billing/platform failure
# (synthetic BuildFailed / (Unknown event) / zero-job startup_failure) is NEVER a
# closure blocker — a green `make ci-local` on the exact pushed SHA is the
# authoritative current-SHA gate. Record the SHA + result as proof.
#
# Mirrors ci.yml:
#   job `guardrails`  -> agent guardrails, scale-guard(+self-test), clinical-defer
#                        guard, mobile-guard, telemetry-guard,
#                        admin-web-request-reads-guard,
#                        android-bounded-memory-guard, large-file guard,
#                        go test ./..., sqlc/migration validation
#   job `admin-web`   -> lint, typecheck, mock-fidelity, request-plan, build
#   android           -> :app compile + unit gate; JDK/SDK are mandatory, no USB device required
#
# Usage:
#   tools/ci/run-local-ci.sh            # all jobs
#   tools/ci/run-local-ci.sh guardrails # one job: guardrails | admin-web | android
set -uo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo"

sha="$(git rev-parse HEAD 2>/dev/null || echo unknown)"
only="${1:-all}"
fail=0
declare -a RESULTS

step() { # name, command...
  local name="$1"; shift
  echo "── ci-local: ${name}"
  if "$@"; then
    RESULTS+=("PASS  ${name}")
  else
    RESULTS+=("FAIL  ${name}")
    fail=1
    echo "!! ci-local step FAILED: ${name}"
  fi
}

# run_e2e_docker_chain: the Docker-image E2E foundation (KERN-001 follow-up). Builds the
# real goatos-backend:e2e image, proves deploy/runtime/workers.json against it
# (e2e-parity), boots deploy/e2e/docker-compose.e2e.yml and proves the real topology starts
# clean with zero worker restarts (e2e-smoke), then (Phase 2a) drives the REAL business chain
# + resilience/idempotency/concurrency scenarios end-to-end against that same image and compose
# file (e2e-business-chain). Skips (exit 0, loud warning, NOT a silent pass) when docker is
# unavailable — same posture as backend/internal/platform/pgtest.SkipIfNoDocker — because
# ci-local must stay usable in a sandboxed environment that genuinely cannot run containers.
# Default ON; set GOATOS_E2E_SMOKE_SKIP=1 to opt the smoke stage out and
# GOATOS_E2E_BUSINESS_CHAIN_SKIP=1 to opt the business-chain stage out explicitly (image-build/
# parity still run, since those need no long-running containers beyond `docker run`).
# e2e-business-chain reuses the already-built image (no second build) and manages its own
# ephemeral stack lifecycle (its own compose project + volumes, torn down on exit) so it does
# not collide with e2e-smoke's stack.
run_e2e_docker_chain() {
  if ! command -v docker >/dev/null 2>&1 || ! docker info >/dev/null 2>&1; then
    echo "!! e2e docker chain: SKIPPED — docker not available/reachable in this environment. This is NOT a pass." >&2
    return 0
  fi
  make e2e-image-build \
    && make e2e-parity \
    && make e2e-smoke \
    && make e2e-business-chain
}

run_guardrails() {
  step "agent: ai-doctor"          make ai-doctor
  step "agent: stg-promotion"      make stg-promotion-guard
  step "agent: boundaries self-test" bash tools/agent-hooks/check-boundaries.sh --self-test
  step "agent: boundaries"        bash tools/agent-hooks/check-boundaries.sh
  step "agent: refresh-binding"   node tools/agent-hooks/check-refresh-binding.mjs
  step "agent: contract-drift"    bash tools/agent-hooks/check-contract-drift.sh
  step "agent: aggregate-projection" make aggregate-projection-guard
  step "agent: scale-certification-docs" make scale-certification-docs-guard
  step "agent: e2e-kernel-integrity" bash tools/agent-hooks/check-e2e-kernel-integrity.sh
  step "agent: api-latency-policy" make api-latency-policy-test
  step "scale-guard"              make scale-guard
  step "scale-guard self-test"    bash -c 'cd tools/scale-guard && go test ./...'
  step "clinical-defer-guard"     make clinical-defer-guard
  step "sweeper-deployment-guard" make sweeper-deployment-guard
  step "deployed-job-flags-guard" make deployed-job-flags-guard
  step "secret-accessors-guard"   make secret-accessors-guard
  step "worker-stage-budgets-guard" make worker-stage-budgets-guard
  step "e2e docker chain (image-build + parity + smoke)" run_e2e_docker_chain
  step "idempotency-writes-guard" make idempotency-writes-guard
  step "atomic-readmodel-sync-guard" make atomic-readmodel-sync-guard
  step "config-validate-guard"    make config-validate-guard
  step "seed-migration-guard"     make seed-migration-guard
  step "india-date-guard"         make india-date-guard
  step "offline-first-guard"      make offline-first-guard
  step "local-single-db-guard"    make local-single-db-guard
  step "mobile-guard"             make mobile-guard
  step "telemetry-guard"          make telemetry-guard
  step "admin-web-request-reads-guard" make admin-web-request-reads-guard
  step "android-bounded-memory-guard"  make android-bounded-memory-guard
  step "room-migration-guard"     make room-migration-guard
  step "large-file guard self-test" node tools/ci/check-large-files.mjs --self-test
  step "large-file guard"         node tools/ci/check-large-files.mjs
  # GOATOS_REQUIRE_DOCKER=1: the required Postgres integration gate must RUN, not skip. A missing
  # docker turns SkipIfNoDocker into a hard failure so CI can never false-green by skipping the
  # kernel Postgres/E2E tests (VACC-REV-04).
  step "go test ./... (docker-required)" bash -c 'cd backend && GOATOS_REQUIRE_DOCKER=1 go test ./...'
  step "sqlc-check"               make sqlc-check
  step "validate-sqlc-plans"      make validate-sqlc-plans
  step "validate-migrations"      make validate-migrations
  step "git diff --check"         git diff --check
}

run_admin_web() {
  if [ ! -d apps/admin-web/node_modules ]; then
    step "admin-web deps" npm --prefix apps/admin-web ci
  fi
  step "admin-web lint"          npm --prefix apps/admin-web run lint
  step "admin-web typecheck"     npm --prefix apps/admin-web run typecheck
  step "admin-web unit tests"    npm --prefix apps/admin-web run test
  step "admin-web mock-fidelity" npm --prefix apps/admin-web run check:mock-fidelity
  step "admin-web request-plan"  npm --prefix apps/admin-web run check:action-center-request-plan
  step "admin-web production build + token leak" env GOATOS_BEARER_TOKEN=sentinel-mesha-admin-token npm --prefix apps/admin-web run build
}

run_android() {
  local jdk="${JAVA_HOME:-/opt/homebrew/opt/openjdk@21}"
  local sdk="${ANDROID_HOME:-$HOME/Library/Android/sdk}"
  if [ ! -x "$jdk/bin/java" ] || [ ! -d "$sdk" ]; then
    RESULTS+=("FAIL  android toolchain (no JDK/SDK: jdk=$jdk sdk=$sdk)")
    fail=1
    return
  fi
  export JAVA_HOME="$jdk" ANDROID_HOME="$sdk" ANDROID_SDK_ROOT="$sdk"
  [ -f apps/goatos-android/local.properties ] || echo "sdk.dir=$sdk" > apps/goatos-android/local.properties
  step "android :app compile" bash -c 'cd apps/goatos-android && ./gradlew :app:compileStgReleaseKotlin --no-daemon --console=plain'
  step "android :app unit"    bash -c 'cd apps/goatos-android && ./gradlew :app:testStgReleaseUnitTest --no-daemon --console=plain'
}

case "$only" in
  guardrails) run_guardrails ;;
  admin-web)  run_admin_web ;;
  android)    run_android ;;
  all)        run_guardrails; run_admin_web; run_android ;;
  *) echo "unknown job: $only (guardrails|admin-web|android|all)"; exit 2 ;;
esac

echo ""
echo "════════ ci-local summary @ ${sha} ════════"
for r in "${RESULTS[@]}"; do echo "  $r"; done
if [ "$fail" -eq 0 ]; then
  echo "ci-local: GREEN @ ${sha}"
else
  echo "ci-local: RED @ ${sha}"
fi
exit "$fail"
