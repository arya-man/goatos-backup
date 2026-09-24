#!/usr/bin/env bash
# check-ci-speed.test.sh — proves the Phase-1 local-CI speedups did not weaken a
# gate. Every case has a positive control so it cannot pass vacuously.
#
#   (a) admin-web npm ci skip: skipped ONLY on an identical lockfile/package.json/
#       node/npm; a lockfile change, a node change, a missing stamp, or a failed
#       install all force a real `npm ci`.
#   (b) CI-tooling self-tests: a self-test still runs when ITS OWN scripts change,
#       and a runner change still runs the self-tests that drive the runner.
#   (c) memory-aware dispatch: on a 32 GB laptop android + backend + admin-web
#       never run at the same time, android starts first, and nothing deadlocks.
#   (d) a killed (SIGKILL/OOM) step or concurrent step yields a non-zero verdict.
set -uo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo"
rc=0
ok()  { echo "  ok   — $1"; }
bad() { echo "  FAIL — $1" >&2; rc=1; }
tmp="$(mktemp -d "${TMPDIR:-/tmp}/goatos-ci-speed-test.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT

# ───────────────────────── (a) npm ci skip ─────────────────────────
case_npm() {
  local d="$tmp/npm" bin="$tmp/npm-bin"
  mkdir -p "$d/app" "$bin"
  printf '{"lockfileVersion":3}\n' >"$d/app/package-lock.json"
  printf '{"name":"x"}\n' >"$d/app/package.json"
  # Stub npm/node: count installs; NPM_FAIL=1 makes `npm ci` fail.
  cat >"$bin/npm" <<'STUB'
#!/usr/bin/env bash
if [ "${1:-}" = "-v" ]; then echo "10.0.0"; exit 0; fi
if [ "${1:-}" = "--prefix" ] && [ "${3:-}" = "ci" ]; then
  echo x >>"$NPM_COUNT"
  [ "${NPM_FAIL:-0}" = 1 ] && exit 1
  mkdir -p "$2/node_modules"; exit 0
fi
exit 0
STUB
  cat >"$bin/node" <<'STUB'
#!/usr/bin/env bash
[ "${1:-}" = "-v" ] && echo "${FAKE_NODE:-v23.1.0}"
STUB
  chmod +x "$bin/npm" "$bin/node"
  export NPM_COUNT="$d/count"; : >"$NPM_COUNT"
  run() { PATH="$bin:$PATH" bash -c '
      . tools/ci/admin-web-deps.sh
      if admin_web_deps_current "$1"; then echo SKIP; else admin_web_deps_install "$1" && echo INSTALLED || echo INSTALL_FAILED; fi' _ "$d/app"; }
  count() { wc -l <"$NPM_COUNT" | tr -d ' '; }

  [ "$(run)" = INSTALLED ] && [ "$(count)" = 1 ] && ok "fresh worktree (no node_modules) installs" || bad "fresh worktree did not install"
  [ "$(run)" = SKIP ] && [ "$(count)" = 1 ] && ok "unchanged lockfile/node => npm ci skipped (positive control)" || bad "unchanged inputs did not skip"
  printf '{"lockfileVersion":3,"x":1}\n' >"$d/app/package-lock.json"
  [ "$(run)" = INSTALLED ] && [ "$(count)" = 2 ] && ok "CHANGED lockfile => npm ci re-runs" || bad "a changed lockfile skipped npm ci"
  [ "$(run)" = SKIP ] && ok "…and skips again once re-stamped" || bad "no re-stamp after reinstall"
  printf '{"name":"y"}\n' >"$d/app/package.json"
  [ "$(run)" = INSTALLED ] && ok "CHANGED package.json => npm ci re-runs" || bad "a changed package.json skipped npm ci"
  [ "$(FAKE_NODE=v24.0.0 run)" = INSTALLED ] && ok "CHANGED node version => npm ci re-runs" || bad "a node upgrade skipped npm ci"
  rm -f "$d/app/node_modules/.goatos-ci-install-stamp"
  [ "$(FAKE_NODE=v24.0.0 run)" = INSTALLED ] && ok "missing stamp => npm ci re-runs" || bad "a missing stamp skipped npm ci"
  printf '{"lockfileVersion":3,"x":2}\n' >"$d/app/package-lock.json"
  [ "$(NPM_FAIL=1 FAKE_NODE=v24.0.0 run)" = INSTALL_FAILED ] && ok "failed npm ci is a failure" || bad "failed npm ci reported success"
  [ "$(FAKE_NODE=v24.0.0 run)" = INSTALLED ] && ok "a FAILED install never leaves a stamp (next run reinstalls)" || bad "a failed install was stamped as current"
  [ "$(GOATOS_ADMIN_WEB_FORCE_NPM_CI=1 FAKE_NODE=v24.0.0 run)" = INSTALLED ] && ok "GOATOS_ADMIN_WEB_FORCE_NPM_CI=1 forces a reinstall" || bad "force override ignored"
}

# ─────────────── (b) self-tests run when their own scripts change ───────────────
case_selftests() {
  local r="$tmp/st"
  mkdir -p "$r/tools"
  cp -R tools/ci "$r/tools/ci"
  ( cd "$r" && git init -q . && git add -A \
      && git -c user.email=t@t -c user.name=t -c commit.gpgsign=false commit -q -m base \
      && git update-ref refs/remotes/origin/main HEAD ) >/dev/null 2>&1
  trace_for() { # file-to-touch -> trace output of the common job
    ( cd "$r" && git checkout -q -B t origin/main 2>/dev/null \
        && echo "# touch" >>"$1" && git add "$1" \
        && git -c user.email=t@t -c user.name=t -c commit.gpgsign=false commit -qm touch \
        && GOATOS_CI_TRACE_ONLY=1 bash tools/ci/run-local-ci.sh common 2>&1 )
  }
  local out
  out="$(trace_for tools/ci/android-ui-diff.sh)"
  case "$out" in *"CI-TRACE android ui-diff detector self-test"*) ok "android-ui-diff.sh edit runs its self-test" ;; *) bad "android-ui-diff.sh edit did NOT run its self-test" ;; esac
  case "$out" in *"CI-TRACE android screenshot scope self-test"*) ok "…and the scope self-test that sources it" ;; *) bad "scope self-test skipped though it sources android-ui-diff.sh" ;; esac
  case "$out" in *"CI-TRACE large-file guard self-test"*) bad "unrelated large-file self-test ran (not narrowed)" ;; *) ok "unrelated large-file self-test is skipped" ;; esac
  out="$(trace_for tools/ci/check-large-files.mjs)"
  case "$out" in *"CI-TRACE large-file guard self-test"*) ok "check-large-files.mjs edit runs its self-test" ;; *) bad "check-large-files.mjs edit did NOT run its self-test" ;; esac
  out="$(trace_for tools/ci/check-push-hook-freshness.test.sh)"
  case "$out" in *"CI-TRACE push-hook-freshness self-test"*) ok "editing a self-test file itself runs it" ;; *) bad "editing a self-test did not run it" ;; esac
  out="$(trace_for tools/ci/parallel-dispatch.sh)"
  for t in "ci-local parallel dispatch self-test" "parallel-dispatch cleanup self-test" "ci base-provenance self-test" "ci-local attribution self-test" "screenshot proof guard self-test" "ci-local speed self-test"; do
    case "$out" in *"CI-TRACE ${t}"*) ok "runner lib edit runs '${t}'" ;; *) bad "runner lib edit did NOT run '${t}'" ;; esac
  done
  out="$(trace_for NOTES.txt)"
  case "$out" in *"CI-TRACE large-file guard self-test"*) bad "a non-tools/ci diff still ran self-tests" ;; *) ok "a non-tools/ci diff skips the self-tests (control)" ;; esac
  out="$(trace_for tools/ci/android-ui-diff.sh >/dev/null; cd "$r" && GOATOS_CI_ALL_SELFTESTS=1 GOATOS_CI_TRACE_ONLY=1 bash tools/ci/run-local-ci.sh common 2>&1)"
  case "$out" in *"CI-TRACE large-file guard self-test"*) ok "GOATOS_CI_ALL_SELFTESTS=1 runs every self-test on a tools/ci diff" ;; *) bad "GOATOS_CI_ALL_SELFTESTS=1 did not force all self-tests" ;; esac
}

# ─────────────── (c) memory-aware dispatch ───────────────
case_memory() {
  local b
  b="$(bash -c '. tools/ci/parallel-dispatch.sh; ci_host_mem_gb() { echo 32; }; unset GOATOS_CI_MEM_BUDGET_GB; ci_mem_budget_gb')"
  [ "$b" = 19 ] && ok "32 GB host => 19 GB budget" || bad "32 GB host budget is ${b}, expected 19"
  local trio
  trio=$(( $(bash -c '. tools/ci/parallel-dispatch.sh; echo $(( $(job_mem_weight android) + $(job_mem_weight backend) + $(job_mem_weight admin-web) ))') ))
  [ "$trio" -gt "$b" ] && ok "android+backend+admin-web weight ${trio} > budget ${b}" || bad "forbidden trio fits the 32 GB budget (${trio} <= ${b})"

  local tr="$tmp/dispatch.trace" out
  : >"$tr"
  out="$(GOATOS_CI_DISPATCH_TRACE="$tr" GOATOS_CI_LOCAL_JOBS=4 bash -c '
    set -uo pipefail; . tools/ci/parallel-dispatch.sh
    ci_host_mem_gb() { echo 32; }; unset GOATOS_CI_MEM_BUDGET_GB
    declare -a RESULTS TIMINGS FAILED_JOBS; fail=0; screenshots_ran="skipped"
    heavy() { local n; n=0; for f in android backend admin-web; do [ -e "$rundir/$f.running" ] && n=$((n+1)); done; echo "$n"; }
    run_job() {
      : >"$rundir/$1.running"
      case "$1" in android|backend|admin-web) [ "$(heavy)" -ge 3 ] && { rm -f "$rundir/$1.running"; exit 9; } ;; esac
      sleep 1
      case "$1" in android|backend|admin-web) [ "$(heavy)" -ge 3 ] && { rm -f "$rundir/$1.running"; exit 9; } ;; esac
      rm -f "$rundir/$1.running"
    }
    dispatch_jobs common backend query-plans admin-web android
    echo "VERDICT fail=$fail"' 2>&1)"
  case "$out" in *"VERDICT fail=0"*) ok "full 5-job dispatch completes GREEN under the 32 GB budget (no deadlock)" ;; *) bad "memory-aware dispatch failed or deadlocked"; printf '%s\n' "$out" >&2 ;; esac
  # Replay the START/END trace: at no instant are all three heavy jobs live.
  local maxheavy
  maxheavy="$(sort -k3,3n -k1,1 "$tr" | awk '
    $2=="android"||$2=="backend"||$2=="admin-web" { if ($1=="START") n++; else n--; if (n>m) m=n }
    END { print m+0 }')"
  [ "$maxheavy" -le 2 ] && ok "trace: at most ${maxheavy} of android/backend/admin-web live at once" || bad "trace shows the forbidden trio live together (${maxheavy})"
  [ "$(awk '$1=="START"{print $2; exit}' "$tr")" = android ] && ok "android is launched first (critical path)" || bad "android was not launched first"
  [ "$(grep -c '^START' "$tr")" = 5 ] && ok "all 5 selected jobs launched (none dropped by the budget)" || bad "a job was dropped by memory scheduling"

  # An over-budget job alone must still run (no deadlock on tiny machines).
  GOATOS_CI_MEM_BUDGET_GB=1 bash -c '
    set -uo pipefail; . tools/ci/parallel-dispatch.sh
    declare -a RESULTS TIMINGS FAILED_JOBS; fail=0; screenshots_ran="skipped"
    run_job() { :; }
    dispatch_jobs android backend; exit "$fail"' >/dev/null 2>&1 \
    && ok "over-budget jobs still run one at a time" || bad "over-budget jobs deadlocked or failed"
}

# ─────────────── (d) a killed step fails loudly ───────────────
case_kill() {
  local lib="$tmp/steplib.sh"
  {
    echo 'current_job=backend; declare -a RESULTS TIMINGS FAILURES FAILED_JOBS; fail=0'
    echo 'record_timing() { :; }'
    echo 'ci_trace_only() { return 1; }'
    sed -n '/^record_failure() {/,/^}/p' tools/ci/run-local-ci.sh
    sed -n '/^step() {/,/^}/p' tools/ci/run-local-ci.sh
    echo '. tools/ci/parallel-dispatch.sh'
  } >"$lib"
  grep -q '^step() {' "$lib" || { bad "could not extract step() from run-local-ci.sh"; return; }
  local out
  # 1. a step whose command is SIGKILLed (what the OOM killer does)
  out="$(bash -c ". '$lib'; step 'oom victim' sh -c 'kill -9 \$\$'; echo \"VERDICT fail=\$fail\"; printf 'R %s\n' \"\${RESULTS[@]}\"" 2>&1)"
  case "$out" in *"VERDICT fail=1"*) ok "SIGKILLed step => fail=1" ;; *) bad "SIGKILLed step was GREEN"; printf '%s\n' "$out" >&2 ;; esac
  case "$out" in *"KILLED by signal 9"*) ok "…and says KILLED by signal 9 loudly" ;; *) bad "kill was not reported loudly" ;; esac
  # 2. a concurrent step whose whole subshell is killed (no status file)
  out="$(bash -c ". '$lib'; cstep_add 'ok step' true; cstep_add 'lost step' sh -c 'kill -9 \$PPID; sleep 5'; cstep_run; echo \"VERDICT fail=\$fail jobs=\${FAILED_JOBS[*]:-}\"; printf 'R %s\n' \"\${RESULTS[@]}\"" 2>&1)"
  case "$out" in *"VERDICT fail=1 jobs=backend"*) ok "killed concurrent step (no status file) => fail=1, job attributed" ;; *) bad "killed concurrent step was GREEN"; printf '%s\n' "$out" >&2 ;; esac
  case "$out" in *"R FAIL  lost step produced NO status file"*) ok "…and is named as signal/OOM/crash" ;; *) bad "lost concurrent step not named" ;; esac
  case "$out" in *"R PASS  ok step"*) ok "sibling concurrent step's PASS survives" ;; *) bad "sibling result lost" ;; esac
  # 3. an ordinary failing concurrent step
  bash -c ". '$lib'; cstep_add a true; cstep_add b false; cstep_run; exit \$fail" >/dev/null 2>&1 \
    && bad "a failing concurrent step reported GREEN" || ok "a failing concurrent step is RED"
  # 4. positive control
  bash -c ". '$lib'; cstep_add a true; cstep_add b true; cstep_run; [ \"\${#RESULTS[@]}\" = 2 ] && exit \$fail || exit 5" >/dev/null 2>&1 \
    && ok "all-green concurrent steps stay GREEN (control)" || bad "all-green concurrent steps reported RED"
  # 5. concurrency is real
  local t0=$SECONDS
  bash -c ". '$lib'; cstep_add a sleep 2; cstep_add b sleep 2; cstep_add c sleep 2; cstep_run" >/dev/null 2>&1
  [ $(( SECONDS - t0 )) -lt 5 ] && ok "concurrent steps overlap ($(( SECONDS - t0 ))s for 3x2s)" || bad "concurrent steps ran serially"
}

echo "ci-local speed (phase 1) self-test"
case_npm
case_selftests
case_memory
case_kill
[ "$rc" -eq 0 ] && echo "ci-speed: self-test passed" || echo "ci-speed: self-test FAILED" >&2
exit "$rc"
