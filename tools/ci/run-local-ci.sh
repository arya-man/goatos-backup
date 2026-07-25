#!/usr/bin/env bash
# run-local-ci.sh — run the SAME required CI gates as .github/workflows/ci.yml,
# locally, job-for-job. Per AGENTS.md: a GitHub Actions billing/platform failure
# (synthetic BuildFailed / (Unknown event) / zero-job startup_failure) is NEVER a
# closure blocker — a green `make ci-local` on the exact pushed SHA is the
# authoritative current-SHA gate. The default is path-scoped against origin/main;
# CI/shared-tooling changes conservatively force every component job.
#
# Mirrors ci.yml:
#   job `common`      -> repository/agent/contract/large-file/diff guards
#   job `backend`     -> backend/kernel/scale/E2E/Go/sqlc/migration gates
#   job `query-plans` -> mandatory PostgreSQL production-query plan gates
#   job `admin-web`   -> lint, typecheck, mock-fidelity, request-plan, build
#   job `android`     -> mobile static guards + :app compile/unit gate
#
# Usage:
#   tools/ci/run-local-ci.sh             # auto: common + affected components
#   tools/ci/run-local-ci.sh all         # force every component job
#   tools/ci/run-local-ci.sh backend     # one partial job (no push receipt)
#   tools/ci/run-local-ci.sh query-plans # one required DB-plan job (no push receipt)
#   tools/ci/run-local-ci.sh guardrails  # compatibility: common + backend + mobile static guards
#   GOATOS_RUN_POSTGRES_TESTS=1 tools/ci/run-local-ci.sh  # explicit DB/Docker opt-in
#   GOATOS_FAST_LOCAL_CI=1 tools/ci/run-local-ci.sh android  # faster developer loop; no landing receipt
set -uo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo"

sha="$(git rev-parse HEAD 2>/dev/null || echo unknown)"
only="${1:-${MODE:-auto}}"
fail=0
receipt_mode=""
receipt_base=""
receipt_jobs=""
declare -a RESULTS

fast_local_ci_enabled() {
  case "${GOATOS_FAST_LOCAL_CI:-0}" in
    1|true|TRUE|True) return 0 ;;
    *) return 1 ;;
  esac
}

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

optional_step() { # name, command...
  local name="$1"; shift
  echo "── ci-local: ${name} (non-blocking)"
  if "$@"; then
    RESULTS+=("PASS  ${name} (optional)")
  else
    RESULTS+=("WARN  ${name} (optional, non-blocking)")
    echo "!! ci-local optional step FAILED: ${name}"
  fi
}

postgres_tests_enabled() {
  case "${GOATOS_RUN_POSTGRES_TESTS:-0}" in
    1|true|TRUE|True) return 0 ;;
    *) return 1 ;;
  esac
}

changed_since_base() {
  local base_ref="${GOATOS_CI_BASE:-origin/main}"
  if ! git rev-parse --verify "${base_ref}^{commit}" >/dev/null 2>&1; then
    base_ref="HEAD~1"
  fi
  git diff --name-only --diff-filter=ACMRD "${base_ref}...HEAD" 2>/dev/null || true
  git diff --name-only --diff-filter=ACMRD --cached 2>/dev/null || true
  git diff --name-only --diff-filter=ACMRD 2>/dev/null || true
}

# ceo_ai_eval_live_enabled: the CEO-AI answer-quality eval calls a live assistant
# endpoint (Vertex/Gemini) and a Postgres oracle, so it is opt-in only — same
# posture as the e2e docker chain. It never runs in the default local/PR gate.
ceo_ai_eval_live_enabled() {
  case "${CEO_AI_EVAL_LIVE:-0}" in
    1|true|TRUE|True) return 0 ;;
    *) return 1 ;;
  esac
}

# run_ceo_ai_eval: always run the cheap, dependency-free structural self-test
# (golden-set integrity + deterministic scorer unit tests); run the LIVE
# answer-quality regression only when explicitly opted in. A missing live
# prerequisite is a loud SKIP, never a silent pass.
run_ceo_ai_eval() {
  step "ceo-ai-eval golden self-test" make ceo-ai-eval-selftest
  if ceo_ai_eval_live_enabled; then
    step "ceo-ai-eval live answer-quality (opt-in)" make ceo-ai-eval
  else
    RESULTS+=("SKIP  ceo-ai-eval live answer-quality (set CEO_AI_EVAL_LIVE=1 + MESHA_ASSISTANT_URL/GOATOS_EVAL_DATABASE_URL/GOATOS_EVAL_TENANT_ID to run)")
    echo "── ci-local: ceo-ai-eval live SKIPPED by default (opt-in via CEO_AI_EVAL_LIVE=1)"
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
# This chain is opt-in only through GOATOS_RUN_POSTGRES_TESTS=1. It never runs in the default
# local or pull-request gate.
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

run_sqlc_static_checks() {
  local version bin
  version="$(cat tools/sqlc/sqlc.version)"
  bin="$(go env GOPATH)/bin/sqlc"
  if [ ! -x "$bin" ] || ! "$bin" version 2>/dev/null | grep -Fq "$version"; then
    go install "github.com/sqlc-dev/sqlc/cmd/sqlc@${version}" || return 1
  fi
  (cd backend && "$bin" vet -f sqlc.yaml && "$bin" diff -f sqlc.yaml)
}

run_common() {
  step "git-identity-guard" make git-identity-guard
  step "guardrail-registration-guard" make guardrail-registration-guard
  step "local-stack-service-guard" make local-stack-service-guard
  step "local-ci-evidence-guard"   make local-ci-evidence-guard
  step "domain-event-architecture-guard" make domain-event-architecture-guard
  step "leadership-assistant-coverage-guard" make leadership-assistant-coverage-guard
  step "assistant-route-closure-guard" make assistant-route-closure-guard
  step "agent: ai-doctor"          make ai-doctor
  step "agent: stg-promotion"      make stg-promotion-guard
  step "agent: boundaries self-test" bash tools/agent-hooks/check-boundaries.sh --self-test
  step "agent: boundaries"        bash tools/agent-hooks/check-boundaries.sh
  step "agent: refresh-binding"   node tools/agent-hooks/check-refresh-binding.mjs
  step "agent: UI vaccine labels" make ui-vaccine-labels-guard
  step "agent: vaccination shared source sync" make vaccination-shared-source-sync-guard
  step "agent: calendar endpoint grain" make calendar-endpoint-grain-guard
  step "agent: contract-drift"    bash tools/agent-hooks/check-contract-drift.sh
  step "large-file guard self-test" node tools/ci/check-large-files.mjs --self-test
  step "large-file guard"         node tools/ci/check-large-files.mjs
  step "git diff --check"         git diff --check
}

run_backend() {
  step "backend-foundations-guard" make backend-foundations-guard
  step "test-execution-integrity-guard" make test-execution-integrity-guard
  step "operator-cap-fail-closed-guard" make operator-cap-fail-closed-guard
  step "cascade-event-wiring-guard" make cascade-event-wiring-guard
  step "backend go mod verify" bash -c 'cd backend && go mod verify'
  step "backend go vet" bash -c 'cd backend && go vet ./...'
  step "backend govulncheck" bash -c 'cd backend && go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...'
  step "backend sqlc vet + diff" run_sqlc_static_checks
  step "backend targeted race" bash -c 'cd backend && GOATOS_RUN_POSTGRES_TESTS=0 go test -race ./internal/platform/worker ./internal/platform/postgres ./internal/kernelstages ./internal/domainconsumer/app ./internal/outbox/adapters/postgres'
  step "agent: aggregate-projection" make aggregate-projection-guard
  step "agent: scale-certification-docs" make scale-certification-docs-guard
  step "agent: e2e-kernel-integrity" bash tools/agent-hooks/check-e2e-kernel-integrity.sh
  step "agent: api-latency-policy" make api-latency-policy-test
  step "scale-guard"              make scale-guard
  step "scale-guard self-test"    bash -c 'cd tools/scale-guard && go test ./...'
  step "clinical-defer-guard"     make clinical-defer-guard
  step "ceo-ai-boundary-guard"    make ceo-ai-boundary-guard
  step "goat-shed-scope-guard"    make goat-shed-scope-guard
  step "vaccination-drive-clubbing-guard" make vaccination-drive-clubbing-guard
  step "vaccination-shed-ack-guard" make vaccination-shed-ack-guard
  step "sweeper-deployment-guard" make sweeper-deployment-guard
  step "deployed-job-flags-guard" make deployed-job-flags-guard
  step "kernel-worker-cutover-guard" make kernel-worker-cutover-guard
  step "stg-disposable-topology-guard" make stg-disposable-topology-guard
  step "secret-accessors-guard"   make secret-accessors-guard
  step "worker-stage-budgets-guard" make worker-stage-budgets-guard
  if postgres_tests_enabled; then
    step "e2e docker chain (explicit Postgres opt-in)" run_e2e_docker_chain
  else
    RESULTS+=("SKIP  Postgres Docker/E2E chain (set GOATOS_RUN_POSTGRES_TESTS=1 to run)")
    echo "── ci-local: Postgres Docker/E2E chain SKIPPED by default (explicit opt-in required)"
  fi
  step "idempotency-writes-guard" make idempotency-writes-guard
  step "atomic-readmodel-sync-guard" make atomic-readmodel-sync-guard
  step "config-validate-guard"    make config-validate-guard
  step "no-mismatch-review-queue-guard" make no-mismatch-review-queue-guard
  step "review-lens-ledger-guard" make review-lens-ledger-guard
  step "seed-migration-guard"     make seed-migration-guard
  step "vaccination-hrms-seed-fixture-guard" make vaccination-hrms-seed-fixture-guard
  step "vaccination-schedule-canonical-guard" make vaccination-schedule-canonical-guard
  step "vaccination-shared-source-sync-guard" make vaccination-shared-source-sync-guard
  step "india-date-guard"         make india-date-guard
  step "local-single-db-guard"    make local-single-db-guard
  if postgres_tests_enabled; then
    # A deliberate Postgres run is fail-closed if Docker is unavailable.
    step "go test ./... (explicit Postgres opt-in)" bash -c 'cd backend && GOATOS_RUN_POSTGRES_TESTS=1 GOATOS_REQUIRE_DOCKER=1 go test ./...'
  else
    # Unit/package tests still compile and run; pgtest-backed and direct Docker Postgres tests
    # skip through the central opt-in policy even when Docker happens to be installed.
    step "go test ./... (Postgres disabled)" bash -c 'cd backend && GOATOS_RUN_POSTGRES_TESTS=0 go test ./...'
  fi
  if postgres_tests_enabled; then
    step "sqlc-check (explicit Postgres opt-in)" make sqlc-check
    step "validate-migrations (explicit Postgres opt-in)" make validate-migrations
  else
	RESULTS+=("SKIP  Postgres sqlc/migration integration gates (explicit opt-in required)")
	echo "── ci-local: Postgres sqlc/migration gates SKIPPED by default"
  fi
  # CEO-AI answer-quality eval: cheap golden self-test always; live regression opt-in.
  run_ceo_ai_eval
}

run_query_plans() {
  # Required for every backend diff. This deliberately stays outside the broad Postgres/E2E opt-in:
  # index regressions in production queries must fail ordinary PR, push, and local landing CI.
  step "required PostgreSQL query plans" make validate-sqlc-plans
}

run_admin_web() {
  if [ ! -d apps/admin-web/node_modules ]; then
    step "admin-web deps" npm --prefix apps/admin-web ci
  fi
  step "frontend-foundations-guard" make frontend-foundations-guard
  step "admin-web lint"          npm --prefix apps/admin-web run lint
  step "admin-web typecheck"     npm --prefix apps/admin-web run typecheck
  step "admin-web unit tests"    npm --prefix apps/admin-web run test
  step "telemetry-guard"         make telemetry-guard
  step "admin-web request reads" make admin-web-request-reads-guard
  step "admin-web prefetch"      make admin-web-prefetch-guard
  step "admin-web local overlays" make admin-web-local-overlay-guard
  step "admin-web mock-fidelity" npm --prefix apps/admin-web run check:mock-fidelity
  step "admin-web request-plan"  npm --prefix apps/admin-web run check:action-center-request-plan
  step "admin-web production build + token leak" env GOATOS_BEARER_TOKEN=sentinel-mesha-admin-token npm --prefix apps/admin-web run build
}

run_android_guards() {
  step "offline-first-guard"          make offline-first-guard
  step "mobile-guard"                 make mobile-guard
  step "android-compose-lists-guard"  make android-compose-lists-guard
  step "android-navigation-stack-guard" make android-navigation-stack-guard
  step "telemetry-guard"              make telemetry-guard
  step "android-bounded-memory-guard" make android-bounded-memory-guard
  step "room-migration-guard"         make room-migration-guard
}

run_android() {
  run_android_guards
  local jdk="${JAVA_HOME:-/opt/homebrew/opt/openjdk@21}"
  local sdk="${ANDROID_HOME:-$HOME/Library/Android/sdk}"
  if [ ! -x "$jdk/bin/java" ] || [ ! -d "$sdk" ]; then
    RESULTS+=("FAIL  android toolchain (no JDK/SDK: jdk=$jdk sdk=$sdk)")
    fail=1
    return
  fi
  export JAVA_HOME="$jdk" ANDROID_HOME="$sdk" ANDROID_SDK_ROOT="$sdk"
  [ -f apps/goatos-android/local.properties ] || echo "sdk.dir=$sdk" > apps/goatos-android/local.properties
  if fast_local_ci_enabled; then
    echo "── ci-local: android FAST mode enabled (Gradle daemon + combined tasks; no landing receipt)"
    step "android fast compile/unit/lint" bash -c 'cd apps/goatos-android && ./gradlew :app:compileStgReleaseKotlin :app:testStgReleaseUnitTest :app:lintStgRelease --console=plain'
    case "${GOATOS_FORCE_ANDROID_SCREENSHOTS:-0}" in
      1|true|TRUE|True)
        optional_step "android screenshots" bash -c 'cd apps/goatos-android && mkdir -p app/build/test-results/testDevDebugUnitTest/binary && touch app/build/test-results/testDevDebugUnitTest/binary/in-progress-results-generic.bin && ./gradlew :app:verifyPaparazziDevDebug --tests "sg.mesha.goatos.ui.ScreenshotTest" --tests "sg.mesha.goatos.ui.RoleChromeScreenshotTest" --tests "sg.mesha.goatos.ui.ScanProofCompactScreenshotTest" --tests "sg.mesha.goatos.ui.ScanProofExpandedScreenshotTest" --console=plain --no-configuration-cache --rerun-tasks --max-workers=1 -Dkotlin.compiler.execution.strategy=in-process -Dkotlin.daemon.enabled=false -Pkotlin.compiler.execution.strategy=in-process'
        ;;
      *)
        echo "── ci-local: android screenshots SKIPPED by GOATOS_FAST_LOCAL_CI=1 (set GOATOS_FORCE_ANDROID_SCREENSHOTS=1 to run)"
        RESULTS+=("SKIP  android screenshots (GOATOS_FAST_LOCAL_CI=1)")
        ;;
    esac
    if changed_since_base | grep -Eq '(^apps/goatos-android/(benchmark|buildSrc)/|^apps/goatos-android/.+\.gradle\.kts$|^apps/goatos-android/settings\.gradle\.kts$)'; then
      step "android benchmark compile" bash -c 'cd apps/goatos-android && ./gradlew :benchmark:compileDevNonMinifiedBenchmarkKotlin --console=plain'
    else
      echo "── ci-local: android benchmark compile SKIPPED by GOATOS_FAST_LOCAL_CI=1 (no Android build/benchmark diff)"
      RESULTS+=("SKIP  android benchmark compile (GOATOS_FAST_LOCAL_CI=1)")
    fi
    return
  fi
  step "android :app compile" bash -c 'cd apps/goatos-android && ./gradlew :app:compileStgReleaseKotlin --no-daemon --console=plain'
  step "android :app unit"    bash -c 'cd apps/goatos-android && ./gradlew :app:testStgReleaseUnitTest --no-daemon --console=plain'
  step "android :app lint"    bash -c 'cd apps/goatos-android && ./gradlew :app:lintStgRelease --no-daemon --console=plain'
  if [ "${GOATOS_SKIP_ANDROID_SCREENSHOTS:-0}" = "1" ]; then
    echo "── ci-local: android screenshots SKIPPED by GOATOS_SKIP_ANDROID_SCREENSHOTS=1"
    RESULTS+=("SKIP  android screenshots (GOATOS_SKIP_ANDROID_SCREENSHOTS=1)")
  else
    optional_step "android screenshots"  bash -c 'cd apps/goatos-android && mkdir -p app/build/test-results/testDevDebugUnitTest/binary && touch app/build/test-results/testDevDebugUnitTest/binary/in-progress-results-generic.bin && ./gradlew :app:verifyPaparazziDevDebug --tests "sg.mesha.goatos.ui.ScreenshotTest" --tests "sg.mesha.goatos.ui.RoleChromeScreenshotTest" --tests "sg.mesha.goatos.ui.ScanProofCompactScreenshotTest" --tests "sg.mesha.goatos.ui.ScanProofExpandedScreenshotTest" --no-daemon --console=plain --no-configuration-cache --rerun-tasks --max-workers=1 -Dkotlin.compiler.execution.strategy=in-process -Dkotlin.daemon.enabled=false -Pkotlin.compiler.execution.strategy=in-process'
  fi
  step "android benchmark compile" bash -c 'cd apps/goatos-android && ./gradlew :benchmark:compileDevNonMinifiedBenchmarkKotlin --no-daemon --console=plain'
}

run_guardrails() {
  run_common
  run_backend
  run_android_guards
}

run_job() {
  case "$1" in
    common)    run_common ;;
    backend)   run_backend ;;
    query-plans) run_query_plans ;;
    admin-web) run_admin_web ;;
    android)   run_android ;;
    *) echo "unknown selected CI job: $1"; exit 2 ;;
  esac
}

case "$only" in
  auto)
    base_ref="${GOATOS_CI_BASE:-origin/main}"
    if ! git rev-parse --verify "${base_ref}^{commit}" >/dev/null 2>&1; then
      base_ref="HEAD~1"
    fi
    scope="$(node tools/ci/ci-scope.mjs --base "$base_ref" --head HEAD --format github)" || exit 2
    selected="$(printf '%s\n' "$scope" | sed -n 's/^selected_jobs=//p')"
    receipt_base="$(printf '%s\n' "$scope" | sed -n 's/^base=//p')"
    is_full="$(printf '%s\n' "$scope" | sed -n 's/^full=//p')"
    [ -n "$selected" ] || { echo "ci-local: classifier returned no selected jobs" >&2; exit 2; }
    echo "ci-local: auto scope against ${receipt_base:-$base_ref} -> ${selected}"
    IFS=',' read -r -a selected_array <<< "$selected"
    for job in "${selected_array[@]}"; do run_job "$job"; done
    receipt_jobs="$selected"
    if [ "$is_full" = "true" ]; then receipt_mode="all"; else receipt_mode="scoped"; fi
    ;;
  common)      run_common ;;
  backend)     run_backend ;;
  query-plans) run_query_plans ;;
  guardrails) run_guardrails ;;
  admin-web)  run_admin_web ;;
  android)    run_android ;;
  all)
	run_common; run_backend; run_query_plans; run_admin_web; run_android
    receipt_mode="all"
	receipt_jobs="common,backend,query-plans,admin-web,android"
    ;;
	*) echo "unknown job/mode: $only (auto|common|backend|query-plans|guardrails|admin-web|android|all)"; exit 2 ;;
esac

echo ""
echo "════════ ci-local summary @ ${sha} ════════"
for r in "${RESULTS[@]}"; do echo "  $r"; done
if [ "$fail" -eq 0 ]; then
  echo "ci-local: GREEN @ ${sha}"
  # Exact-SHA push evidence: an auto-scoped run records the exact base + selected
  # jobs; a forced full run records mode=all. Explicit JOB=... runs stay partial.
  if fast_local_ci_enabled; then
    echo "ci-local: FAST local run — no main-push evidence receipt written."
  elif [ -n "$receipt_mode" ]; then
    receipt_args=(--record "$sha" --mode "$receipt_mode" --jobs "$receipt_jobs")
    if [ "$receipt_mode" = "scoped" ]; then receipt_args+=(--base "$receipt_base"); fi
    node tools/ci/check-local-ci-evidence.mjs "${receipt_args[@]}" || \
      echo "!! warning: could not record local-CI evidence receipt for ${sha}" >&2
  else
    echo "ci-local: explicit partial run ('${only}') — no main-push evidence receipt written."
  fi
else
  echo "ci-local: RED @ ${sha}"
fi
exit "$fail"
