#!/usr/bin/env bash
# check-run-local-ci-parallel.test.sh — proves the parallel dispatcher cannot
# report a false GREEN.
#
# Two layers:
#   layer 1 — dispatcher failure semantics directly (child-only fail=1, a
#             SIGKILLed job with no status file, a non-zero run_job return,
#             missing android screenshot coverage, accounting).
#   layer 2 — the SHIPPED script, in a throwaway git repo, exits non-zero and
#             writes NO receipt when a job fails — with a POSITIVE CONTROL so the
#             test cannot pass vacuously.
set -uo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo"
rc=0
ok()  { echo "  ok   — $1"; }
bad() { echo "  FAIL — $1" >&2; rc=1; }

# ───────────────────── layer 1: dispatcher semantics ─────────────────────
layer1() {
  local out
  out="$(bash -c '
    set -uo pipefail
    . tools/ci/parallel-dispatch.sh
    declare -a RESULTS TIMINGS FAILED_JOBS; fail=0; screenshots_ran=""
    run_job() {
      case "$1" in
        common)      RESULTS+=("PASS  common") ;;
        # child-only mutation: the classic bug. Parent must still see RED.
        admin-web)   RESULTS+=("FAIL  admin-web injected"); fail=1 ;;
        # dies by signal; never writes a status file.
        backend)     sh -c "kill -9 \$PPID"; sleep 5 ;;
        query-plans) RESULTS+=("PASS  query-plans") ;;
        android)     RESULTS+=("PASS  android"); screenshots_ran="skipped" ;;
      esac
    }
    dispatch_jobs common admin-web backend query-plans android
    echo "VERDICT fail=$fail failed=[${FAILED_JOBS[*]:-}]"
    printf "RESULT %s\n" "${RESULTS[@]}"
  ' 2>&1)"

  case "$out" in
    *"VERDICT fail=1"*) ok "a child-only fail=1 reaches the parent as RED" ;;
    *) bad "false GREEN with a failing job"; printf '%s\n' "$out" >&2 ;;
  esac
  case "$out" in
    *"failed=["*admin-web*"]"*) ok "the failing job is named in FAILED_JOBS" ;;
    *) bad "the failing job was not attributed to FAILED_JOBS" ;;
  esac
  case "$out" in
    *"RESULT FAIL  job backend produced NO status file"*) ok "a SIGKILLed job with no status file is FAILED" ;;
    *) bad "a missing status file did not fail the run"; printf '%s\n' "$out" >&2 ;;
  esac
  case "$out" in
    *"failed=["*backend*) ok "the killed job is named in FAILED_JOBS" ;;
    *) bad "the killed job was not attributed" ;;
  esac
  case "$out" in
    *"RESULT PASS  common"*) ok "a passing job's RESULTS survive the process boundary" ;;
    *) bad "RESULTS were lost across the process boundary" ;;
  esac

  # a job that returns non-zero WITHOUT setting fail must still be RED
  bash -c '
    set -uo pipefail; . tools/ci/parallel-dispatch.sh
    declare -a RESULTS TIMINGS FAILED_JOBS; fail=0; screenshots_ran=""
    run_job() { return 3; }
    dispatch_jobs common; exit "$fail"' >/dev/null 2>&1 \
    && bad "a non-zero run_job rc reported GREEN" || ok "a non-zero run_job rc is RED"

  # android with no screenshot coverage must be RED (this protects the receipt field)
  bash -c '
    set -uo pipefail; . tools/ci/parallel-dispatch.sh
    declare -a RESULTS TIMINGS FAILED_JOBS; fail=0; screenshots_ran=""
    run_job() { :; }
    dispatch_jobs android; exit "$fail"' >/dev/null 2>&1 \
    && bad "android without screenshot coverage reported GREEN" || ok "android without screenshot coverage is RED"

  # POSITIVE CONTROL: an all-green run must stay GREEN, or every assertion above
  # would pass for the wrong reason.
  bash -c '
    set -uo pipefail; . tools/ci/parallel-dispatch.sh
    declare -a RESULTS TIMINGS FAILED_JOBS; fail=0; screenshots_ran=""
    run_job() { RESULTS+=("PASS  $1"); }
    dispatch_jobs common admin-web; exit "$fail"' >/dev/null 2>&1 \
    && ok "an all-green run stays GREEN" || bad "an all-green run reported RED"

  # group exclusion: backend and query-plans (group `docker`) never overlap.
  local sched
  sched="$(bash -c '
    set -uo pipefail; . tools/ci/parallel-dispatch.sh
    declare -a RESULTS TIMINGS FAILED_JOBS; fail=0; screenshots_ran="skipped"
    run_job() { sleep 0.5; }
    dispatch_jobs backend query-plans common' 2>&1 | grep -c "launched job")"
  [ "$sched" = "3" ] || bad "expected 3 launches, saw ${sched}"
  bash -c '
    set -uo pipefail; . tools/ci/parallel-dispatch.sh
    declare -a RESULTS TIMINGS FAILED_JOBS; fail=0; screenshots_ran="skipped"
    run_job() { case "$1" in backend|query-plans) [ -e "$rundir/docker.lock" ] && exit 9; : >"$rundir/docker.lock"; sleep 0.6; rm -f "$rundir/docker.lock" ;; esac; }
    dispatch_jobs backend query-plans; exit "$fail"' >/dev/null 2>&1 \
    && ok "docker-group jobs never overlap" || bad "backend and query-plans ran concurrently"
}

# ── layer 2: the real script never writes a receipt on a RED parallel run ──
harness() { # <dest.sh> <common-body>
  awk -v stub="$2" '
    /^run_job\(\) \{/ && !done {
      print "run_common()      { current_job=common; " stub " }";
      print "run_backend()     { current_job=backend; step \"stub backend\" true; }";
      print "run_query_plans() { current_job=query-plans; step \"stub qp\" true; }";
      print "run_admin_web()   { current_job=admin-web; step \"stub aw\" true; }";
      print "run_android()     { current_job=android; screenshots_ran=skipped; step \"stub android\" true; }";
      done = 1
    } { print }' tools/ci/run-local-ci.sh >"$1"
}

layer2() {
  local tmp; tmp="$(mktemp -d "${TMPDIR:-/tmp}/goatos-ci-parallel-test.XXXXXX")"
  mkdir -p "$tmp/repo/tools/ci"
  cp tools/ci/*.mjs tools/ci/*.json tools/ci/*.sh "$tmp/repo/tools/ci/" 2>/dev/null
  # Two commits + a real refs/remotes/origin/main: the receipt-writing modes now
  # require a resolvable diff base that is an ancestor of remote main (an
  # unresolvable base falls back to HEAD~1 and is FATAL — see
  # tools/ci/check-ci-base-provenance.test.sh). Without a remote main this
  # synthetic repo would exit 4 before any stubbed job ran, which would make the
  # positive control below vacuous.
  ( cd "$tmp/repo" && git init -q . \
      && git -c user.email=t@t -c user.name=t -c commit.gpgsign=false commit -q --allow-empty -m harness-base \
      && git update-ref refs/remotes/origin/main HEAD \
      && git -c user.email=t@t -c user.name=t -c commit.gpgsign=false commit -q --allow-empty -m harness ) >/dev/null 2>&1
  local receipt="$tmp/repo/.git/goatos-ci-local-receipt.json"
  local status

  # (i) RED: one injected failing step must exit non-zero AND write no receipt.
  harness "$tmp/repo/tools/ci/run-local-ci.sh" 'step "INJECTED FAILURE" false;'
  ( cd "$tmp/repo" && env -u GOATOS_FAST_LOCAL_CI -u GOATOS_CI_TRACE_ONLY bash tools/ci/run-local-ci.sh all ) >"$tmp/red.log" 2>&1
  status=$?
  [ "$status" -ne 0 ] && ok "injected failure => non-zero exit" || bad "injected failure exited 0"
  [ ! -e "$receipt" ] && ok "injected failure => NO receipt" || bad "RECEIPT WRITTEN ON A RED RUN"
  grep -q 'ci-local: RED' "$tmp/red.log" && ok "summary says RED" || bad "summary did not say RED"

  # (ii) POSITIVE CONTROL: all-green must exit 0 AND write a receipt. Without
  #      this, layer 2 would pass even if the script were broken to always fail.
  harness "$tmp/repo/tools/ci/run-local-ci.sh" 'step "stub common" true;'
  ( cd "$tmp/repo" && env -u GOATOS_FAST_LOCAL_CI -u GOATOS_CI_TRACE_ONLY bash tools/ci/run-local-ci.sh all ) >"$tmp/green.log" 2>&1
  status=$?
  [ "$status" -eq 0 ] && ok "all-green => exit 0" || { bad "all-green exited ${status} (test would be vacuous)"; tail -25 "$tmp/green.log" >&2; }
  [ -e "$receipt" ] && ok "all-green => receipt written" || bad "all-green wrote no receipt (test would be vacuous)"

  # (iii) deterministic replay: job logs appear in SELECTION order, not finish order.
  grep "════════ job '" "$tmp/green.log" | sed "s/.*job '\([a-z-]*\)'.*/\1/" | tr '\n' ' ' \
    | grep -q '^common backend query-plans admin-web android $' \
    && ok "job logs replay in deterministic selection order" || bad "log replay order is non-deterministic"

  rm -rf "$tmp"
}

echo "run-local-ci parallel dispatch self-test"
layer1
layer2
[ "$rc" -eq 0 ] && echo "run-local-ci-parallel: self-test passed" || echo "run-local-ci-parallel: self-test FAILED" >&2
exit "$rc"
