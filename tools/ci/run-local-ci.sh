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

run_guardrails() {
  step "agent: ai-doctor"          make ai-doctor
  step "agent: boundaries"        bash tools/agent-hooks/check-boundaries.sh
  step "agent: contract-drift"    bash tools/agent-hooks/check-contract-drift.sh
  step "agent: e2e-kernel-integrity" bash tools/agent-hooks/check-e2e-kernel-integrity.sh
  step "agent: api-latency-policy" make api-latency-policy-test
  step "scale-guard"              make scale-guard
  step "scale-guard self-test"    bash -c 'cd tools/scale-guard && go test ./...'
  step "clinical-defer-guard"     make clinical-defer-guard
  step "sweeper-deployment-guard" make sweeper-deployment-guard
  step "idempotency-writes-guard" make idempotency-writes-guard
  step "atomic-readmodel-sync-guard" make atomic-readmodel-sync-guard
  step "config-validate-guard"    make config-validate-guard
  step "india-date-guard"         make india-date-guard
  step "offline-first-guard"      make offline-first-guard
  step "mobile-guard"             make mobile-guard
  step "telemetry-guard"          make telemetry-guard
  step "admin-web-request-reads-guard" make admin-web-request-reads-guard
  step "android-bounded-memory-guard"  make android-bounded-memory-guard
  step "large-file guard self-test" node tools/ci/check-large-files.mjs --self-test
  step "large-file guard"         node tools/ci/check-large-files.mjs
  step "go test ./..."            bash -c 'cd backend && go test ./...'
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
