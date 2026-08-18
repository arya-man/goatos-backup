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
run_id="$(date -u +%Y%m%dT%H%M%SZ)-${sha:0:12}-$$"
fail=0
receipt_mode=""
receipt_base=""
receipt_jobs=""
# Screenshot coverage recorded on the push receipt. `not-applicable` is the
# correct initial value: a scoped run without the android job never enters
# run_android, and the receipt must not claim an unknown state.
screenshots_ran="not-applicable"
current_job="ci-local"
declare -a RESULTS
# FAILURES mirrors every blocking failure so the RED verdict can name the failing
# steps immediately above the exit line. The summary can run 130+ rows, so a FAIL
# buried at row 10 is invisible to anyone reading the tail of the output — a gate
# that says RED without saying why costs more time than one that fails loudly.
declare -a FAILURES

record_failure() { # name
  FAILURES+=("$1")
  fail=1
}
declare -a FAILED_JOBS
ci_step_cache_dir="$(git rev-parse --git-path goatos-ci-step-cache 2>/dev/null || echo .git/goatos-ci-step-cache)"

fast_local_ci_enabled() {
  case "${GOATOS_FAST_LOCAL_CI:-0}" in
    1|true|TRUE|True) return 0 ;;
    *) return 1 ;;
  esac
}

# GOATOS_CI_TRACE_ONLY=1 — reachability probe used by
# tools/ci/check-android-screenshot-proof.sh. `step` records the command it WOULD
# run and returns success without executing it, so the guard can assert that the
# Paparazzi proof is actually REACHABLE rather than that its name appears in a
# banner `echo`.
#
# THIS IS NOT A BYPASS, and three properties must hold or it becomes one:
#   (i)   `step` returns 0 without executing, so a trace run never legitimately
#         reaches the GREEN branch on merit;
#   (ii)  the tail exits 3 BEFORE the receipt block, so a trace run can never
#         write a receipt — the trace check MUST stay ahead of the receipt write;
#   (iii) check-android-screenshot-proof.test.sh case (g) asserts (ii).
# check-local-ci-evidence.mjs additionally refuses --record under this variable.
ci_trace_only() {
  case "${GOATOS_CI_TRACE_ONLY:-0}" in
    1|true|TRUE|True) return 0 ;;
    *) return 1 ;;
  esac
}

# Step-level timing instrumentation. `step`/`optional_step` are the single choke
# point every gate flows through, so one instrumented run profiles the whole
# suite. This NEVER reads or writes `fail` and never changes an exit status.
timings_file="$(git rev-parse --git-path goatos-ci-local-timings.tsv 2>/dev/null || echo /dev/null)"
timings_jsonl_file="$(git rev-parse --git-path goatos-ci-local-timings.jsonl 2>/dev/null || echo /dev/null)"
declare -a TIMINGS

json_escape() { # string
  node -e 'process.stdout.write(JSON.stringify(process.argv[1]))' "$1" 2>/dev/null || printf '"%s"' "$1"
}

record_timing() { # name, status, seconds
  TIMINGS+=("$3	$1	$2")
  local epoch iso job step status seconds
  epoch="$(date +%s)"
  iso="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  job="${current_job:-ci-local}"
  step="$1"
  status="$2"
  seconds="$3"
  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\n' "$epoch" "$run_id" "$sha" "$job" "$step" "$status" "$seconds" >>"$timings_file" 2>/dev/null || true
  printf '{"ts":%s,"time":"%s","run_id":%s,"sha":"%s","mode":%s,"job":%s,"step":%s,"status":"%s","seconds":%s}\n' \
    "$epoch" \
    "$iso" \
    "$(json_escape "$run_id")" \
    "$sha" \
    "$(json_escape "$only")" \
    "$(json_escape "$job")" \
    "$(json_escape "$step")" \
    "$status" \
    "$seconds" >>"$timings_jsonl_file" 2>/dev/null || true
}

step_cache_enabled() {
  ci_trace_only && return 1
  case "$only" in
    auto|all) return 1 ;;
  esac
  [ -z "$(git status --porcelain --untracked-files=all 2>/dev/null)" ] || return 1
  case "${GOATOS_CI_STEP_CACHE:-1}" in
    0|false|FALSE|False) return 1 ;;
    *) return 0 ;;
  esac
}

step_cache_key() { # name, command...
  local name="$1"; shift
  {
    printf 'sha=%s\n' "$sha"
    printf 'base=%s\n' "$ci_base_sha"
    printf 'job=%s\n' "$current_job"
    printf 'step=%s\n' "$name"
    printf 'cmd=%q' "$@"
    printf '\nstatus:\n'
    git status --porcelain --untracked-files=all 2>/dev/null || true
  } | shasum -a 256 | awk '{print $1}'
}

step_cached() { # name, command...
  local name="$1"; shift
  if step_cache_enabled; then
    local key marker before_failures
    key="$(step_cache_key "$name" "$@")"
    marker="${ci_step_cache_dir}/${key}.pass"
    if [ -f "$marker" ]; then
      echo "── ci-local: ${name} (cached pass for this SHA/worktree)"
      RESULTS+=("PASS  ${name} (cached)")
      record_timing "$name" PASS 0
      return 0
    fi
    before_failures="${#FAILURES[@]}"
    step "$name" "$@"
    if [ "${#FAILURES[@]}" -eq "$before_failures" ]; then
      mkdir -p "$ci_step_cache_dir" 2>/dev/null || true
      printf '%s\t%s\t%s\n' "$sha" "$current_job" "$name" >"$marker" 2>/dev/null || true
    fi
    return 0
  fi
  step "$name" "$@"
}

step() { # name, command...
  local name="$1"; shift
  # Reachability probe (see ci_trace_only): record, never execute. Note this also
  # short-circuits the nested guard invocations in run_android_guards, so a trace
  # run cannot recurse into check-android-screenshot-proof.sh — that is
  # load-bearing, not incidental.
  if ci_trace_only; then echo "CI-TRACE ${name} :: $*"; return 0; fi
  echo "── ci-local: ${name}"
  local t0=$SECONDS
  if "$@"; then
    local dt=$(( SECONDS - t0 ))
    RESULTS+=("PASS  ${name} (${dt}s)")
    record_timing "$name" PASS "$dt"
  else
      local dt=$(( SECONDS - t0 ))
      RESULTS+=("FAIL  ${name} (${dt}s)")
      record_timing "$name" FAIL "$dt"
      case " ${FAILED_JOBS[*]:-} " in *" ${current_job} "*) ;; *) FAILED_JOBS+=("$current_job") ;; esac
      record_failure "${name}"
    echo "!! ci-local step FAILED: ${name}"
  fi
}

optional_step() { # name, command...
  local name="$1"; shift
  echo "── ci-local: ${name} (non-blocking)"
  local t0=$SECONDS
  if "$@"; then
    local dt=$(( SECONDS - t0 ))
    RESULTS+=("PASS  ${name} (optional, ${dt}s)")
    record_timing "$name" PASS "$dt"
  else
    local dt=$(( SECONDS - t0 ))
    RESULTS+=("WARN  ${name} (optional, non-blocking, ${dt}s)")
    record_timing "$name" WARN "$dt"
    echo "!! ci-local optional step FAILED: ${name}"
  fi
}

postgres_tests_enabled() {
  case "${GOATOS_RUN_POSTGRES_TESTS:-0}" in
    1|true|TRUE|True) return 0 ;;
    *) return 1 ;;
  esac
}

# ── CI diff base: ONE resolver, ONE fallback ─────────────────────────────────
# EVERY consumer (changed_since_base, the Android UI-diff detector, ci-scope.mjs,
# and the push receipt) reads the base from here. Independent copies of
# `${GOATOS_CI_BASE:-origin/main}` with independent HEAD~1 fallbacks are exactly
# how "what we diffed" silently diverged from "what we recorded".
#
# The base is the whole ballgame: an empty diff makes the Android UI-diff
# detector see nothing, so the loud `skipped-with-ui-diff` banner never fires and
# a genuinely green receipt records `screenshots:"skipped"` over a real UI
# change. `GOATOS_CI_BASE=HEAD` did that on purpose; an unresolvable origin/main
# did it by accident. Both are handled: the base is now RECORDED on the receipt
# and validated against real remote main at push time
# (check-local-ci-evidence.mjs -> computeBaseAncestry), and the accidental case
# is made fatal below instead of silent.
ci_base_fallback=0
ci_base_ref=""
ci_base_sha=""
resolve_ci_base() {
  [ -z "$ci_base_ref" ] || return 0
  local requested="${GOATOS_CI_BASE:-origin/main}"
  if git rev-parse --verify "${requested}^{commit}" >/dev/null 2>&1; then
    ci_base_ref="$requested"
  else
    ci_base_ref="HEAD~1"
    ci_base_fallback=1
    echo "!! ci-local: base ref '${requested}' is UNRESOLVABLE (no network, no origin/main, or a bad GOATOS_CI_BASE)."
    echo "!! ci-local: falling back to HEAD~1. The job scope AND the Android UI-diff"
    echo "!! ci-local: detector are now computed against a base that is NOT remote main,"
    echo "!! ci-local: so this run proves less than it appears to."
  fi
  ci_base_sha="$(git rev-parse --verify "${ci_base_ref}^{commit}" 2>/dev/null || true)"
}
resolve_ci_base
receipt_base="$ci_base_sha"

# (c) The HEAD~1 fallback is fatal exactly where it CHANGES GATE MEANING — i.e.
# on the receipt-writing modes. Non-certifying lanes are deliberately preserved:
#   * GOATOS_FAST_LOCAL_CI=1 (developer inner loop) never writes a receipt;
#   * explicit `JOB=...` partial runs never write a receipt;
#   * a detached HEAD is unaffected as long as origin/main resolves — the base
#     is a ref, not the current branch;
#   * offline work still runs: `git fetch origin main` once, or take the
#     explicitly non-certifying lane printed below.
case "$only" in
  auto|all)
    if [ "$ci_base_fallback" = "1" ] || [ -z "$ci_base_sha" ]; then
      if ! fast_local_ci_enabled; then
        echo "!! ci-local: REFUSING to run the receipt-writing gate on the HEAD~1 fallback." >&2
        echo "!!   A receipt recorded against HEAD~1 cannot be validated against remote main" >&2
        echo "!!   at push time, and can hide a skipped Android UI proof." >&2
        echo "!!   Fix the base:   git fetch origin main   (then re-run)" >&2
        echo "!!   Or run it explicitly as a NON-certifying check (writes no receipt):" >&2
        echo "!!     GOATOS_FAST_LOCAL_CI=1 tools/ci/run-local-ci.sh ${only}" >&2
        exit 4
      fi
      echo "!! ci-local: continuing on the HEAD~1 fallback because GOATOS_FAST_LOCAL_CI=1 — this run writes NO receipt."
    fi
    ;;
esac

changed_since_base() {
  local base_ref="$ci_base_ref"
  git diff --name-only --diff-filter=ACMRD "${base_ref}...HEAD" 2>/dev/null || true
  git diff --name-only --diff-filter=ACMRD --cached 2>/dev/null || true
  git diff --name-only --diff-filter=ACMRD 2>/dev/null || true
}

# Parallel job dispatch. Verdicts cross the process boundary only as atomically
# written status FILES, and a missing file is a hard failure — see the safety
# contract at the top of that file.
# shellcheck source=tools/ci/parallel-dispatch.sh
. "$(dirname "${BASH_SOURCE[0]}")/parallel-dispatch.sh"

# Android UI-diff detection (path scope + @Composable content). Sourced so the
# detector is independently testable — see tools/ci/check-android-ui-diff.test.sh.
# shellcheck source=tools/ci/android-ui-diff.sh
. "$(dirname "${BASH_SOURCE[0]}")/android-ui-diff.sh"
# shellcheck source=tools/ci/android-screenshot-scope.sh
. "$(dirname "${BASH_SOURCE[0]}")/android-screenshot-scope.sh"

# Machine-wide advisory Gradle mutex. job_group() serialises `android` within ONE
# dispatch; this serialises it across WORKTREES. FAIL-OPEN on every path — it
# changes WHEN the android job starts, never which steps run or how they are
# judged. See the contract at the top of that file.
# shellcheck source=tools/ci/gradle-worktree-lock.sh
. "$(dirname "${BASH_SOURCE[0]}")/gradle-worktree-lock.sh"

# ci_tooling_changed: true when this run should pay for the tools/ci/** self-tests.
# FAIL-OPEN BY DESIGN: if the diff cannot be determined (no base ref, git failure,
# empty output) we RUN the self-tests. A guard that silently skips itself because
# git hiccuped is the inert-guard failure mode this file already got burned by.
ci_tooling_changed() {
  local changed
  changed="$(changed_since_base 2>/dev/null)" || return 0
  [ -n "$changed" ] || return 0
  printf '%s\n' "$changed" | grep -Eq '^tools/ci/'
}

# gradle_lock_lib_changed / gradle_lock_selftest_changed — same fail-open shape
# as ci_tooling_changed above (undeterminable or empty diff => RUN).
#
# WHY THESE ARE SEPARATE FROM ci_tooling_changed: the lock guard is ~45 s and its
# mutation self-test is ~15 min. Charging both to every `tools/ci/**` commit is a
# wall-clock regression inside a wall-clock fix — the same mistake the ~62 s
# screenshot-proof self-test made before it was diff-scoped.
# run-local-ci.sh is in the GUARD's trigger set too, not only the self-test's:
# case (g) asserts that a trace run acquires no lock and case (g3) that it does
# not run the machine-wide stale-worker reaper. Both are properties of THIS
# file, so a diff here that leaves the library alone must still pay the 47 s.
gradle_lock_lib_changed() {
  local changed
  changed="$(changed_since_base 2>/dev/null)" || return 0
  [ -n "$changed" ] || return 0
  printf '%s\n' "$changed" | grep -Eq '^tools/ci/(gradle-worktree-lock\.sh|run-local-ci\.sh)$'
}

# The 23-mutant suite proves the lock library and guard harness, not the local CI
# dispatcher. Dispatcher wiring is already covered by gradle_lock_lib_changed()
# above, which runs the real guard case (g)/(g3) on run-local-ci.sh edits without
# paying the mutation-suite timeout budget.
gradle_lock_selftest_changed() {
  local changed
  changed="$(changed_since_base 2>/dev/null)" || return 0
  [ -n "$changed" ] || return 0
  printf '%s\n' "$changed" | grep -Eq '^tools/ci/(gradle-worktree-lock\.sh|check-gradle-worktree-lock\.sh|check-gradle-worktree-lock\.test\.sh)$'
}

# The real check lives in its own file so it greps this script from OUTSIDE and
# cannot satisfy itself with its own function body (the previous in-file version
# self-matched on ':app:verifyPaparazziDevDebug' and was inert). Intent is
# unchanged in safety: WHEN the Paparazzi proof runs it must run the full task
# or the guarded diff-mapped target set, and the on-demand opt-in must stay
# reachable.
android_screenshot_proof_coverage_guard() {
  bash tools/ci/check-android-screenshot-proof.sh tools/ci/run-local-ci.sh
}

run_android_screenshots() {
  local filter_args
  if filter_args="$(android_screenshot_gradle_filter_args)"; then
    echo "ci-local: Android screenshot scope mapped to targeted Paparazzi filters: ${filter_args}"
    step_cached "android screenshots (targeted)" bash -c "cd apps/goatos-android && mkdir -p app/build/test-results/testDevDebugUnitTest/binary && touch app/build/test-results/testDevDebugUnitTest/binary/in-progress-results-generic.bin && ./gradlew :app:verifyPaparazziDevDebug --no-daemon --console=plain --no-configuration-cache --rerun-tasks --max-workers=1 -Dkotlin.compiler.execution.strategy=in-process -Dkotlin.daemon.enabled=false -Pkotlin.compiler.execution.strategy=in-process ${filter_args}"
  else
    step_cached "android screenshots" bash -c 'cd apps/goatos-android && mkdir -p app/build/test-results/testDevDebugUnitTest/binary && touch app/build/test-results/testDevDebugUnitTest/binary/in-progress-results-generic.bin && ./gradlew :app:verifyPaparazziDevDebug --no-daemon --console=plain --no-configuration-cache --rerun-tasks --max-workers=1 -Dkotlin.compiler.execution.strategy=in-process -Dkotlin.daemon.enabled=false -Pkotlin.compiler.execution.strategy=in-process'
  fi
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
  current_job="common"
  step "git-identity-guard" make git-identity-guard
  step "guardrail-registration-guard" make guardrail-registration-guard
  step "local-stack-service-guard" make local-stack-service-guard
  step "local-ci-evidence-guard"   make local-ci-evidence-guard
  step "domain-event-architecture-guard" make domain-event-architecture-guard
  step "operational-read-model-contract-guard" make operational-read-model-contract-guard
  step "critical-animal-action-availability-guard" make critical-animal-action-availability-guard
  step "leadership-assistant-coverage-guard" make leadership-assistant-coverage-guard
  step "leadership-verifier-surface-separation-guard" make leadership-verifier-surface-separation-guard
  step "role-scoped-ui-contract-guard" make role-scoped-ui-contract-guard
  step "assistant-route-closure-guard" make assistant-route-closure-guard
  step "telemetry-guard"           make telemetry-guard
  step "agent: ai-doctor"          make ai-doctor
  step "agent: stg-promotion"      make stg-promotion-guard
  step "agent: boundaries self-test" bash tools/agent-hooks/check-boundaries.sh --self-test
  step "agent: boundaries"        bash tools/agent-hooks/check-boundaries.sh
  step "agent: refresh-binding"   node tools/agent-hooks/check-refresh-binding.mjs
  step "agent: UI vaccine labels" make ui-vaccine-labels-guard
  step "agent: notification specificity" make notification-specificity-guard
  step "agent: vaccination shared source sync" make vaccination-shared-source-sync-guard
  step "agent: calendar endpoint grain" make calendar-endpoint-grain-guard
  step "agent: contract-drift"    bash tools/agent-hooks/check-contract-drift.sh
  # CI-tooling self-tests are diff-scoped, same posture as telemetry-guard. They
  # exercise tools/ci/* fixtures (the screenshot-proof suite alone spawns enough
  # subshells to cost ~62s guarding a 0.18s check), and they can only regress when
  # tools/ci/** itself changes. Running them on every unrelated commit made
  # JOB=common ~59s slower than the android collapse saved.
  if ci_tooling_changed; then
    step "ci-local parallel dispatch self-test" bash tools/ci/check-run-local-ci-parallel.test.sh
    step "android ui-diff detector self-test" bash tools/ci/check-android-ui-diff.test.sh
    step "android screenshot scope self-test" bash tools/ci/check-android-screenshot-scope.test.sh
    step "screenshot proof guard self-test" bash tools/ci/check-android-screenshot-proof.test.sh
    step "screenshot remediation guard self-test" bash tools/ci/check-screenshot-remediation.test.sh
    step "ci-local attribution self-test" bash tools/ci/check-run-local-ci-attribution.test.sh
    step "ci base-provenance self-test" bash tools/ci/check-ci-base-provenance.test.sh
    step "large-file guard self-test" node tools/ci/check-large-files.mjs --self-test
    step "push-hook-freshness self-test" bash tools/ci/check-push-hook-freshness.test.sh
    step "parallel-dispatch cleanup self-test" bash tools/ci/check-parallel-dispatch-cleanup.test.sh
  else
    RESULTS+=("SKIP  ci-tooling self-tests (no tools/ci/** diff)")
    echo "── ci-local: ci-tooling self-tests SKIPPED (no tools/ci/** diff vs base)"
  fi
  # NOT inside the ci_tooling_changed block above: this guard's inputs are the
  # Makefile AND tools/ci/**, and ci_tooling_changed only looks at `^tools/ci/`.
  # Diff-scoping it would let a Makefile-only edit silently reopen the receipt
  # hole it exists to close. ~2s, no Gradle, isolated sandbox.
  step "screenshot remediation guard" bash tools/ci/check-screenshot-remediation.sh
  # Unconditional, NOT diff-scoped: the installed hook goes stale because the
  # REPO changed, so gating this on a tools/ci/** diff would silence it in exactly
  # the runs that follow the change which caused the drift.
  step "push-hook-freshness-guard" bash tools/ci/check-push-hook-freshness.sh
  step "parallel-dispatch cleanup guard" bash tools/ci/check-parallel-dispatch-cleanup.sh
  # gradle-worktree-lock guard: ~47s, all of it sandboxed sleeps. The guard runs
  # no Gradle ITSELF, but case (g) does drive `run-local-ci.sh android` under
  # trace — which is why this file is in its trigger set alongside the library.
  # Diff-scoped (fail-open) because 47s x every pass is a meaningful slice of
  # what the lock itself wins back. `make gradle-worktree-lock-guard` and
  # `make guardrails` still run it unconditionally when explicitly asked.
  if gradle_lock_lib_changed; then
    step "gradle-worktree-lock guard" bash tools/ci/check-gradle-worktree-lock.sh
  else
    RESULTS+=("SKIP  gradle-worktree-lock guard (no tools/ci/gradle-worktree-lock.sh diff)")
    echo "── ci-local: gradle-worktree-lock guard SKIPPED (no tools/ci/gradle-worktree-lock.sh diff vs base)"
  fi
  if gradle_lock_selftest_changed; then
    step "gradle-worktree-lock guard self-test" bash tools/ci/check-gradle-worktree-lock.test.sh
  else
    RESULTS+=("SKIP  gradle-worktree-lock guard self-test (no lock/guard/harness diff)")
    echo "── ci-local: gradle-worktree-lock guard self-test SKIPPED (no lock/guard/harness diff vs base)"
  fi
  step "large-file guard"         node tools/ci/check-large-files.mjs
  step "git diff --check"         git diff --check
  # exception-guard: diff-scoped (Kotlin + Go together in one pass — see
  # tools/exception-guard/exception_guard.py:run). Previously this diff-scoped
  # invocation was ONLY reachable via `make guardrails`, never via
  # run-local-ci.sh, so `make ci-local` never caught a new swallowed-exception
  # on a diff line. telemetry-guard's diff-scoped counterpart already runs
  # per-component below (run_admin_web / run_android_guards); exception-guard
  # is not component-split, so it runs once here for every job.
  step "exception-guard"          make exception-guard
  # Whole-tree shrink-only debt ratchets — see docs/observability/GUARDRAIL_RATCHET.md.
  # exception-guard/telemetry-guard themselves are diff-scoped (run per-component
  # below/elsewhere); these two catch NEW violations anywhere in the tree (not just
  # on diff-touched lines) against a committed baseline, and fail if the baseline
  # goes stale. Cheap (well under 1s combined) so they always run here regardless
  # of which component jobs are selected.
  step "exception-guard (whole-tree ratchet)" make exception-guard-ratchet
  step "telemetry-guard (whole-tree ratchet)" make telemetry-guard-ratchet
  # v2 ratchets: separate shrink-only baselines for the newer rule kinds
  # (cancellation_swallowed/bare_exempt_marker on the exception side;
  # screen_view/reserved_names on the telemetry side) — see the Makefile
  # target comments and docs/TELEMETRY.md for why these are split from the
  # original two ratchets instead of folded into the same baseline.
  step "exception-guard (whole-tree ratchet v2)" make exception-guard-ratchet-v2
  step "telemetry-guard (whole-tree ratchet v2)" make telemetry-guard-ratchet-v2
}

run_backend() {
  current_job="backend"
  step "backend-foundations-guard" make backend-foundations-guard
  step "test-execution-integrity-guard" make test-execution-integrity-guard
  step "operator-cap-fail-closed-guard" make operator-cap-fail-closed-guard
  step "stg-operator-scope-guard" make stg-operator-scope-guard
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
  step "operational-location-guard" make operational-location-guard
  step "goat-shed-scope-guard"    make goat-shed-scope-guard
  step "operational-partition-identity-guard" make operational-partition-identity-guard
  step "proof-capture-authorization-guard" make proof-capture-authorization-guard
  step "weighing-free-flow-guard" make weighing-free-flow-guard
  step "feed-submitted-overlay-wiring-guard" make feed-submitted-overlay-wiring-guard
  step "feed-proof-collaboration-guard" make feed-proof-collaboration-guard
  step "weighing-close-gate-guard" make weighing-close-gate-guard
  step "weighing-operator-scope-guard" make weighing-operator-scope-guard
  step "weighing-one-operator-per-bucket-guard" make weighing-one-operator-per-bucket-guard
  step "weighing-partition-composition-guard" make weighing-partition-composition-guard
  step "weighing-kernel-phase2-guard" make weighing-kernel-phase2-guard
  step "migration-duplicate-versions-guard" make migration-duplicate-versions-guard
  step "vaccination-drive-clubbing-guard" make vaccination-drive-clubbing-guard
  step "vaccination-adult-drive-contract-guard" make vaccination-adult-drive-contract-guard
  step "vaccination-shed-ack-guard" make vaccination-shed-ack-guard
  step "module-alerts-tab-guard" make module-alerts-tab-guard
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
  step "fcm-recipient-routing-guard" make fcm-recipient-routing-guard
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
  current_job="query-plans"
  # Required for every backend diff. This deliberately stays outside the broad Postgres/E2E opt-in:
  # index regressions in production queries must fail ordinary PR, push, and local landing CI.
  step "required PostgreSQL query plans" make validate-sqlc-plans
}

run_admin_web() {
  current_job="admin-web"
  if [ ! -d apps/admin-web/node_modules ]; then
    step "admin-web deps" npm --prefix apps/admin-web ci
  fi
  step "frontend-foundations-guard" make frontend-foundations-guard
  step "admin-web lint"          npm --prefix apps/admin-web run lint
  step "admin-web typecheck"     npm --prefix apps/admin-web run typecheck
  step "admin-web unit tests"    npm --prefix apps/admin-web run test
  step "admin-web request reads" make admin-web-request-reads-guard
  step "admin-web prefetch"      make admin-web-prefetch-guard
  step "admin-web local overlays" make admin-web-local-overlay-guard
  step "overlay motion"          make overlay-motion-guard
  step "admin-web mock-fidelity" npm --prefix apps/admin-web run check:mock-fidelity
  step "admin-web request-plan"  npm --prefix apps/admin-web run check:action-center-request-plan
  step "admin-web production build + token leak" env GOATOS_BEARER_TOKEN=sentinel-mesha-admin-token npm --prefix apps/admin-web run build
}

run_android_guards() {
  # NOTE: deliberately NO current_job assignment here. This function runs under
  # BOTH the `android` job (which also builds with Gradle) and the Gradle-free
  # `guardrails` job. Hardcoding "android" sent a guardrails failure's re-run hint
  # into a full :app Gradle build the user never asked for. Callers own attribution.
  case "$current_job" in
    android|guardrails) ;;
    *) echo "!! run_android_guards called with unattributed job '${current_job}'" >&2; record_failure "run_android_guards unattributed job '${current_job}'" ;;
  esac
  step "offline-first-guard"          make offline-first-guard
  step "mobile-guard"                 make mobile-guard
  step "domain-event-envelope-enum-guard" make domain-event-envelope-enum-guard
  step "design-system-guard"          make design-system-guard
  step "android-row-action-scope-guard" make android-row-action-scope-guard
  step "operational-partition-identity-guard" make operational-partition-identity-guard
  step "android-vaccination-submit-gate-guard" make android-vaccination-submit-gate-guard
  step "android-compose-lists-guard"  make android-compose-lists-guard
  step "android-navigation-stack-guard" make android-navigation-stack-guard
  step "l0-root-chrome-guard" make l0-root-chrome-guard
  step "nav-entry-point-placement-guard" make nav-entry-point-placement-guard
  step "mobile-contract-ownership-guard" make mobile-contract-ownership-guard
  step "android screenshot proof coverage guard" android_screenshot_proof_coverage_guard
  step "android-bounded-memory-guard" make android-bounded-memory-guard
  step "room-migration-guard"         make room-migration-guard
}

run_android() {
  # Orphaned test JVMs from a previously-killed Gradle run hold module build locks, so the
  # next run blocks on a lock nobody is watching and reads as "the suite is slow". Reap first.
  #
  # NOT under trace. This sits outside `step`, so trace mode does not short-circuit it, and
  # the reaper `kill -9`s every GradleWorkerMain on the MACHINE older than 30 minutes — other
  # agents' workers included, which AGENTS.md bans outright. A trace run starts no Gradle and
  # so has nothing to reap; two required guards drive this target under trace
  # (check-android-screenshot-proof.sh and check-gradle-worktree-lock.sh case (g)), which
  # made `make guardrails` a machine-wide killer of somebody else's long build.
  ci_trace_only || bash tools/agent-hooks/reap-stale-gradle-workers.sh || true

  current_job="android"
  run_android_guards
  if [ "$fail" -ne 0 ]; then
    echo "── ci-local: android Gradle checks SKIPPED because Android static guards are already red"
    RESULTS+=("SKIP  android Gradle checks (static guards failed)")
    return
  fi
  local jdk="${JAVA_HOME:-/opt/homebrew/opt/openjdk@21}"
  local sdk="${ANDROID_HOME:-$HOME/Library/Android/sdk}"
  # A trace run must reach the screenshot branch even on a machine with no
  # Android toolchain — it executes nothing.
  if ! ci_trace_only && { [ ! -x "$jdk/bin/java" ] || [ ! -d "$sdk" ]; }; then
    RESULTS+=("FAIL  android toolchain (no JDK/SDK: jdk=$jdk sdk=$sdk)")
    record_failure "android toolchain (no JDK/SDK: jdk=$jdk sdk=$sdk)"
    return
  fi
  export JAVA_HOME="$jdk" ANDROID_HOME="$sdk" ANDROID_SDK_ROOT="$sdk"
  [ -f apps/goatos-android/local.properties ] || echo "sdk.dir=$sdk" > apps/goatos-android/local.properties
  # ── machine-wide Gradle lane, acquired ONCE for the whole Gradle region ─────
  # Placement is deliberate: AFTER run_android_guards and the toolchain check, so
  # the ~13 cheap static guards never queue behind another worktree and a
  # toolchain-missing run never takes the lock; BEFORE the config-cache guard,
  # which runs a real Gradle configuration and is therefore inside the contended
  # region. One acquire per job, not per step — five acquire/release cycles
  # would let two worktrees ping-pong and reproduce the contention this removes.
  # NOT skipped under GOATOS_FAST_LOCAL_CI: fast mode uses the Gradle DAEMON,
  # which is precisely the shared resource that contends.
  local lane_t0=$SECONDS
  gradle_lock_acquire "android gradle"
  gradle_lock_install_trap
  # record_timing, so the contention is MEASURABLE in the TSV next session; it
  # never reads or writes `fail`. Suppressed under trace, which by contract
  # executes nothing and must not append rows to the shared timings file.
  ci_trace_only || record_timing "android gradle lane wait" PASS $(( SECONDS - lane_t0 ))
  # Configuration-cache safety. Lives in the `android` job, not run_common or
  # `guardrails`, because it needs a real Gradle configuration run (~5-35s, no
  # task actions). It is BEHAVIOURAL: the same failure a real build would hit.
  # It is not folded into the compile steps below, because those pass
  # --no-configuration-cache and so are structurally blind to this defect class.
  step_cached "android config-cache guard self-test" bash tools/ci/check-gradle-config-cache.test.sh
  step_cached "android config-cache guard" bash tools/ci/check-gradle-config-cache.sh
  if [ "$fail" -ne 0 ]; then
    echo "── ci-local: android Gradle compile/screenshots/benchmark SKIPPED because Android config-cache checks are already red"
    RESULTS+=("SKIP  android Gradle compile/screenshots/benchmark (config-cache checks failed)")
    gradle_lock_clear_trap
    gradle_lock_release
    return
  fi
  if fast_local_ci_enabled; then
    echo "── ci-local: android FAST mode enabled (Gradle daemon + combined tasks; no landing receipt)"
    step_cached "android fast compile/unit/lint" bash -c 'cd apps/goatos-android && ./gradlew :app:compileStgReleaseKotlin :app:testStgReleaseUnitTest :app:lintStgRelease --console=plain'
    if [ "$fail" -ne 0 ]; then
      echo "── ci-local: android screenshots/benchmark SKIPPED because compile/unit/lint is already red"
      RESULTS+=("SKIP  android screenshots/benchmark (compile/unit/lint failed)")
      gradle_lock_clear_trap
      gradle_lock_release
      return
    fi
    case "${GOATOS_RUN_ANDROID_SCREENSHOTS:-0}" in
      1|true|TRUE|True)
        screenshots_ran="yes"
        run_android_screenshots
        ;;
      *)
        # FAST mode writes no receipt, so it cannot authorise anything — but it
        # must warn identically, or the developer loop hides a UI diff that the
        # landing run will block on.
        if android_ui_diff_detected; then
          screenshots_ran="skipped-with-ui-diff"
          echo "── ci-local: android screenshots SKIPPED **WITH AN ANDROID UI DIFF** — run: make ci-local-screenshots"
          RESULTS+=("SKIP  android screenshots (SKIPPED WITH UI DIFF; run make ci-local-screenshots)")
        else
          screenshots_ran="skipped"
          echo "── ci-local: android screenshots SKIPPED (default OFF) — to prove them: make ci-local-screenshots"
          RESULTS+=("SKIP  android screenshots (default OFF; to prove them run make ci-local-screenshots)")
        fi
        ;;
    esac
    if changed_since_base | grep -Eq '(^apps/goatos-android/(benchmark|buildSrc)/|^apps/goatos-android/.+\.gradle\.kts$|^apps/goatos-android/settings\.gradle\.kts$)'; then
      step_cached "android benchmark compile" bash -c 'cd apps/goatos-android && ./gradlew :benchmark:compileDevNonMinifiedBenchmarkKotlin --console=plain'
    else
      echo "── ci-local: android benchmark compile SKIPPED by GOATOS_FAST_LOCAL_CI=1 (no Android build/benchmark diff)"
      RESULTS+=("SKIP  android benchmark compile (GOATOS_FAST_LOCAL_CI=1)")
    fi
    gradle_lock_clear_trap
    gradle_lock_release
    return
  fi
  # ONE Gradle invocation for compile+unit+lint, with every flag byte-for-byte as
  # it was across the three previous invocations. Measured on a 12-core/JDK-21 box:
  # three separate --no-daemon invocations pay JVM start + configuration +
  # up-to-date checking three times (~30s of fixed overhead on an up-to-date tree)
  # versus 12.7s paid once — ~17s saved per android leg. Gradle reports the UNION
  # of the task graphs (712 actionable tasks), not the sum-with-repeats.
  #
  # Flags are deliberately NOT touched. `--no-daemon` mirrors GitHub's ephemeral
  # runner (the receipt attests that fidelity); the in-process Kotlin strategy is
  # load-bearing on testStgReleaseUnitTest (Firebase Perf ASM instrumentation has
  # corrupted unit-test Flow fakes here before — see f4a63345);
  # `--no-configuration-cache` and `--max-workers=1` have no recorded reason in
  # blame, so they stay until someone proves them removable.
  #
  # Failure semantics are unchanged: Gradle stops at the first failing task, just
  # as the three sequential steps did. Adding --continue would report all three in
  # one pass (a strictly stronger gate) but is a separate decision.
  step_cached "android :app compile+unit+lint" bash -c 'cd apps/goatos-android && mkdir -p app/build/generated/ksp/stgRelease/java/hilt_aggregated_deps && ./gradlew :app:compileStgReleaseKotlin :app:testStgReleaseUnitTest :app:lintStgRelease --no-daemon --console=plain --no-configuration-cache --max-workers=1 -Dkotlin.compiler.execution.strategy=in-process -Dkotlin.daemon.enabled=false -Pkotlin.compiler.execution.strategy=in-process'
  if [ "$fail" -ne 0 ]; then
    echo "── ci-local: android screenshots/benchmark SKIPPED because compile/unit/lint is already red"
    RESULTS+=("SKIP  android screenshots/benchmark (compile/unit/lint failed)")
    gradle_lock_clear_trap
    gradle_lock_release
    return
  fi
  # Paparazzi is OPT-IN. The default landing run — the run that writes the push
  # receipt — does not run it, and the receipt records that fact.
  case "${GOATOS_RUN_ANDROID_SCREENSHOTS:-0}" in
    1|true|TRUE|True)
      screenshots_ran="yes"
      run_android_screenshots
      ;;
    *)
      if android_ui_diff_detected; then
        screenshots_ran="skipped-with-ui-diff"
        echo ""
        echo "################################################################"
        echo "##  ci-local: THIS DIFF TOUCHES ANDROID UI OR SNAPSHOTS       ##"
        echo "##  and the Paparazzi screenshot proof did NOT run.           ##"
        echo "##  This receipt does NOT cover screenshot regressions.       ##"
        echo "##  Run: make ci-local-screenshots                            ##"
        echo "################################################################"
        echo ""
        RESULTS+=("SKIP  android screenshots (SKIPPED WITH UI DIFF; run make ci-local-screenshots)")
      else
        screenshots_ran="skipped"
        echo ""
        echo "################################################################"
        echo "##  ci-local: ANDROID PAPARAZZI SCREENSHOT PROOF NOT RUN      ##"
        echo "##  This receipt does NOT cover screenshot regressions.       ##"
        echo "##  To prove them:  make ci-local-screenshots                 ##"
        echo "################################################################"
        echo ""
        RESULTS+=("SKIP  android screenshots (default OFF; to prove them run make ci-local-screenshots)")
      fi
      ;;
  esac
  step_cached "android benchmark compile" bash -c 'cd apps/goatos-android && ./gradlew :benchmark:compileDevNonMinifiedBenchmarkKotlin --no-daemon --console=plain'
  gradle_lock_clear_trap
  gradle_lock_release
}

run_guardrails() {
  run_common
  run_backend
  # `guardrails` is the Gradle-free compatibility lane; a failure here must point
  # the user back at `guardrails`, not at `android` (which builds :app).
  current_job="guardrails"
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
    base_ref="$ci_base_ref"
    scope="$(node tools/ci/ci-scope.mjs --base "$base_ref" --head HEAD --format github)" || exit 2
    selected="$(printf '%s\n' "$scope" | sed -n 's/^selected_jobs=//p')"
    scope_base="$(printf '%s\n' "$scope" | sed -n 's/^base=//p')"
    # The classifier must have resolved the SAME base this script diffed against
    # and will record; a divergence means two resolvers again.
    if [ -n "$scope_base" ] && [ -n "$ci_base_sha" ] && [ "$scope_base" != "$ci_base_sha" ]; then
      echo "ci-local: classifier base ${scope_base} != resolved CI base ${ci_base_sha}" >&2
      exit 2
    fi
    receipt_base="${scope_base:-$ci_base_sha}"
    is_full="$(printf '%s\n' "$scope" | sed -n 's/^full=//p')"
    [ -n "$selected" ] || { echo "ci-local: classifier returned no selected jobs" >&2; exit 2; }
    echo "ci-local: auto scope against ${receipt_base:-$base_ref} -> ${selected}"
    IFS=',' read -r -a selected_array <<< "$selected"
    dispatch_jobs "${selected_array[@]}"
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
    dispatch_jobs common backend query-plans admin-web android
    receipt_mode="all"
	receipt_jobs="common,backend,query-plans,admin-web,android"
    ;;
	*) echo "unknown job/mode: $only (auto|common|backend|query-plans|guardrails|admin-web|android|all)"; exit 2 ;;
esac

# MUST stay ahead of the receipt-writing branch below. See ci_trace_only: moving
# this check after the receipt write turns trace mode into a receipt forger.
if ci_trace_only; then
  echo "ci-local: TRACE-ONLY run — nothing executed, NO receipt, exit 3."
  exit 3
fi

echo ""
echo "════════ ci-local summary @ ${sha} ════════"
for r in "${RESULTS[@]}"; do echo "  $r"; done
if [ "${#TIMINGS[@]}" -gt 0 ]; then
  echo ""
  echo "──────── slowest steps (top 10) ────────"
  printf '%s\n' "${TIMINGS[@]}" | sort -t"$(printf '\t')" -k1,1nr | head -10 \
    | awk -F"\t" '{ printf "  %6ss  %s  [%s]\n", $1, $2, $3 }'
  echo "  full per-step timings TSV: ${timings_file}"
  echo "  full per-step timings JSONL: ${timings_jsonl_file}"
fi
if [ "$fail" -eq 0 ]; then
  echo "ci-local: GREEN @ ${sha}"
  # Exact-SHA push evidence: an auto-scoped run records the exact base + selected
  # jobs; a forced full run records mode=all. Explicit JOB=... runs stay partial.
  if fast_local_ci_enabled; then
    echo "ci-local: fast/partial run — NO receipt written; this cannot authorise a push."
  elif [ -n "$receipt_mode" ]; then
    # --base is recorded for BOTH modes. A mode=all receipt without a base is
    # unvalidatable at push time and re-opens the base-spoof hole.
    if [ -z "$receipt_base" ]; then
      echo "!! ci-local: no resolved CI base to record; refusing to write an unvalidatable receipt." >&2
      exit 4
    fi
    receipt_args=(--record "$sha" --mode "$receipt_mode" --base "$receipt_base" --jobs "$receipt_jobs" --screenshots "$screenshots_ran")
    node tools/ci/check-local-ci-evidence.mjs "${receipt_args[@]}" || \
      echo "!! warning: could not record local-CI evidence receipt for ${sha}" >&2
  else
    echo "ci-local: fast/partial run ('${only}') — NO receipt written; this cannot authorise a push."
  fi
else
  echo ""
  echo "──────── ci-local FAILING STEPS @ ${sha} ────────"
  if [ "${#FAILURES[@]}" -eq 0 ]; then
    # Should be unreachable: every `fail=1` goes through record_failure. If it is
    # ever reached, a new failure site skipped the recorder — say so instead of
    # emitting a cause-free RED.
    echo "  (none recorded — BUG in run-local-ci.sh: a failure site set fail=1 without record_failure)"
  else
    for f in "${FAILURES[@]}"; do echo "  FAIL  ${f}"; done
    echo ""
    echo "  Re-run just the first failure, e.g.:  grep -n '${FAILURES[0]}' tools/ci/run-local-ci.sh"
  fi
  echo "ci-local: RED @ ${sha} (${#FAILURES[@]} failing step(s) named above)"
    if [ "${#FAILED_JOBS[@]}" -gt 0 ]; then
      echo ""
      echo "  Or re-check only the failing JOB (fast, writes NO receipt):"
      for j in "${FAILED_JOBS[@]}"; do
        echo "    GOATOS_FAST_LOCAL_CI=1 tools/ci/run-local-ci.sh ${j}"
      done
      echo "  Then certify once with the full gate: make ci-local"
    fi
fi
exit "$fail"
