# shellcheck shell=bash
# parallel-dispatch.sh — filesystem-accounted parallel job dispatch for run-local-ci.sh.
#
# SAFETY CONTRACT (this is the whole point of the file):
#   * A job runs in a SUBSHELL. Nothing it sets in memory is trusted by the parent.
#     The obvious bug — "the child sets fail=1 and the parent never sees it" — is
#     eliminated by construction, because the parent never reads a child variable.
#   * Every job's verdict crosses the process boundary as a FILE: <job>.status.
#   * The status file is written LAST and ATOMICALLY (tmp + mv). A job killed by a
#     signal, an OOM kill, or a crashed shell therefore leaves NO status file.
#   * A MISSING or non-numeric status file scores 97 = FAILED. ABSENCE == FAILURE.
#   * The parent also asserts accounted == launched == selected, so "a job silently
#     never ran" cannot be green either.
#   * screenshots_ran is set inside run_android — in the parallel version that is a
#     child variable too, and a lost value would let a receipt claim wrong screenshot
#     coverage. It is persisted per job and its ABSENCE fails the run.
#   * bash 3.2 compatible on purpose (macOS /bin/bash): no `wait -n`, no `declare -A`.
#   * Ctrl-C is a SAFETY event, not a no-op: an interrupted dispatch must not leave
#     orphaned children behind. Gradle in particular survives a bare TERM to its
#     launcher (GradleWorkerMain keeps running and keeps holding build locks), so
#     cleanup walks the whole descendant tree, TERMs it, then KILLs survivors, and
#     removes the $TMPDIR/goatos-ci-local.* run dir. The traps NEVER invent a
#     success status: the EXIT trap re-exits with the status the shell already had,
#     and a signal handler re-raises the signal so the caller sees 128+n.
#
# This changes HOW the selected jobs run, never WHICH ones. Width 1 degrades to
# sequential. No gate is skipped and no new bypass exists.

# Jobs in the same group never run concurrently. Scheduling is done by the PARENT
# (no child-side lock files) so a killed child can never leave a stale lock and
# deadlock the run.
# `backend` shares the postgres group ONLY when it actually opens a database.
# In the DEFAULT path its heavy step is `GOATOS_RUN_POSTGRES_TESTS=0 go test ./...`
# (run-local-ci.sh) -- Postgres disabled, no admin DSN, nothing to contend for --
# so pinning it behind query-plans bought no safety and cost the sum of the two
# instead of the longer one (measured 380s + 326s wall for 380s of work). On the
# opt-in path (GOATOS_RUN_POSTGRES_TESTS=1) it runs the real Postgres/Docker
# chain, and there the contention is real, so the grouping stays exactly as it was.
# THE single definition. run-local-ci.sh sources this file before its first use
# and no longer defines its own copy: two case-lists that must agree is how
# `GOATOS_RUN_POSTGRES_TESTS=True` came to run the whole Docker chain while the
# dispatcher believed no database was open. The accepted set is unchanged from
# the one run-local-ci.sh has always used.
postgres_tests_enabled() {
  case "${GOATOS_RUN_POSTGRES_TESTS:-0}" in
    1|true|TRUE|True) return 0 ;;
    *) return 1 ;;
  esac
}

job_group() {
  case "$1" in
    android)     echo gradle ;;   # Gradle daemon/lock contention
    query-plans) echo postgres ;; # Postgres/admin-DSN/runtime contention
    backend)     if postgres_tests_enabled; then echo postgres; else echo none; fi ;;
    *)           echo none   ;;   # common, admin-web: freely parallel
  esac
}

# Default 5 = every component job at once (20-min land-main cap): with 3, the
# LONGEST job (android) was launched last and queued behind `common`, so the
# critical path was common + android instead of android alone. Each job is
# multi-threaded, but backend/admin-web/common are short; hard-clamped to 1..5.
ci_parallel_width() {
  local n="${GOATOS_CI_LOCAL_JOBS:-5}"
  case "$n" in ''|*[!0-9]*) n=5 ;; esac
  [ "$n" -ge 1 ] 2>/dev/null || n=1
  [ "$n" -le 5 ] || n=5
  printf '%s' "$n"
}

# Launch order: longest pole first, so a narrower width never parks android
# behind a one-minute guard job. Output/accounting stay in SELECTION order.
ci_launch_order() { # job... -> jobs sorted by expected wall time, longest first
  local j p
  for j in "$@"; do
    case "$j" in android) p=1 ;; query-plans) p=2 ;; backend) p=3 ;; admin-web) p=4 ;; *) p=5 ;; esac
    printf '%s %s\n' "$p" "$j"
  done | sort -n -s -k1,1 | awk '{print $2}'
}

# ───────── interrupt-safe cleanup (traps) ─────────
# Tracked at file scope so the trap handlers can see them; `dispatch_jobs` is the
# only writer.
_DISPATCH_RUNDIR=""
_DISPATCH_ALL_PIDS=""   # every pid ever launched by the current dispatch

_dispatch_kill_tree() { # pid signal — leaves first, so nothing is reparented away
  local pid="$1" sig="$2" child
  for child in $(pgrep -P "$pid" 2>/dev/null); do
    _dispatch_kill_tree "$child" "$sig"
  done
  kill "-$sig" "$pid" 2>/dev/null || true
}

_dispatch_any_alive() {
  local pid
  for pid in $_DISPATCH_ALL_PIDS; do
    kill -0 "$pid" 2>/dev/null && return 0
  done
  return 1
}

_dispatch_cleanup() {
  local pid waited=0
  for pid in $_DISPATCH_ALL_PIDS; do
    kill -0 "$pid" 2>/dev/null && _dispatch_kill_tree "$pid" TERM
  done
  while [ "$waited" -lt 25 ] && _dispatch_any_alive; do
    sleep 0.2
    waited=$((waited + 1))
  done
  for pid in $_DISPATCH_ALL_PIDS; do
    kill -0 "$pid" 2>/dev/null && _dispatch_kill_tree "$pid" KILL
  done
  _DISPATCH_ALL_PIDS=""
  [ -n "$_DISPATCH_RUNDIR" ] && rm -rf "$_DISPATCH_RUNDIR"
  _DISPATCH_RUNDIR=""
  return 0
}

_dispatch_on_signal() { # INT|TERM
  local sig="$1" num
  case "$sig" in INT) num=2 ;; TERM) num=15 ;; *) num=15 ;; esac
  trap - INT TERM EXIT
  echo "" >&2
  echo "ci-local: SIG${sig} received — terminating child jobs and removing the run dir" >&2
  _dispatch_cleanup
  # Re-raise with the default disposition so the caller sees a real 128+n signal
  # exit. This can only make the status WORSE, never green.
  kill "-$sig" $$ 2>/dev/null || true
  exit $((128 + num))
}

_dispatch_on_exit() {
  local rc=$?          # captured FIRST: cleanup must never rewrite the verdict
  trap - INT TERM EXIT
  _dispatch_cleanup
  exit "$rc"
}

_dispatch_install_traps() {
  trap '_dispatch_on_signal INT'  INT
  trap '_dispatch_on_signal TERM' TERM
  trap '_dispatch_on_exit'        EXIT
}

_dispatch_clear_traps() {
  trap - INT TERM EXIT
}

# FAIL FAST (default ON; GOATOS_CI_FAIL_FAST=0 runs every job to completion).
# A failing step() writes $GOATOS_CI_FAILFAST_FILE. The parent polls it and stops
# ONLY the pids in live_pids — the children THIS dispatch launched — via the same
# leaves-first tree walk the signal cleanup uses. Unrelated processes (another
# session's Gradle, another ci-local) are never touched: we never pkill by name.
# A stopped job leaves no status file and is scored FAILED, so fail-fast can only
# make a run RED sooner, never green.
ci_fail_fast_enabled() {
  case "${GOATOS_CI_FAIL_FAST:-1}" in 0|false|FALSE|False) return 1 ;; *) return 0 ;; esac
}

_dispatch_stop_pids() { # pid...
  local pid waited=0 alive
  for pid in "$@"; do kill -0 "$pid" 2>/dev/null && _dispatch_kill_tree "$pid" TERM; done
  while [ "$waited" -lt 25 ]; do
    alive=0
    for pid in "$@"; do kill -0 "$pid" 2>/dev/null && alive=1; done
    [ "$alive" = 1 ] || break
    sleep 0.2; waited=$((waited + 1))
  done
  for pid in "$@"; do kill -0 "$pid" 2>/dev/null && _dispatch_kill_tree "$pid" KILL; done
  for pid in "$@"; do wait "$pid" 2>/dev/null; done
  return 0
}

# dispatch_jobs <job>...
# Mutates the caller's fail / RESULTS / TIMINGS / FAILED_JOBS / screenshots_ran
# using ONLY values read back from disk.
dispatch_jobs() {
  local jobs=("$@")
  local width; width="$(ci_parallel_width)"
  local rundir
  rundir="$(mktemp -d "${TMPDIR:-/tmp}/goatos-ci-local.XXXXXX")" || { fail=1; return 1; }
  _DISPATCH_RUNDIR="$rundir"
  _DISPATCH_ALL_PIDS=""
  _dispatch_install_traps
  local failfast_file="" failfast_hit=""
  if ci_fail_fast_enabled; then
    failfast_file="$rundir/failfast"
    export GOATOS_CI_FAILFAST_FILE="$failfast_file"
  fi

  local pending=()
  local _j
  for _j in $(ci_launch_order "${jobs[@]}"); do pending+=("$_j"); done
  local -a live_pids=() live_jobs=() live_groups=()
  local launched=0

  _dispatch_launch() { # job
    local job="$1"
    (
      exec >"$rundir/$job.log" 2>&1          # per-job log; no interleaved output
      local _t0=$SECONDS
      RESULTS=(); TIMINGS=(); FAILED_JOBS=(); FAILURES=(); fail=0
      run_job "$job"
      local rc=$?
      [ "$fail" -eq 0 ] || rc=1              # a step() failure dominates run_job's rc
      : >"$rundir/$job.results"
      [ "${#RESULTS[@]}" -eq 0 ] || printf '%s\n' "${RESULTS[@]}" >"$rundir/$job.results"
      : >"$rundir/$job.timings"
      [ "${#TIMINGS[@]}" -eq 0 ] || printf '%s\n' "${TIMINGS[@]}" >"$rundir/$job.timings"
      # FAILURES is main's failing-step-name list. It MUST cross the subshell like
      # every other collected array, or a parallel run prints "RED (0 failing steps)"
      # and names nothing — the exact cause-free RED that list exists to prevent.
      : >"$rundir/$job.failures"
      [ "${#FAILURES[@]}" -eq 0 ] || printf '%s\n' "${FAILURES[@]}" >"$rundir/$job.failures"
      printf '%s' "${screenshots_ran:-}" >"$rundir/$job.screenshots"
      printf '%s' "$(( SECONDS - _t0 ))" >"$rundir/$job.wall"
      # STATUS LAST, ATOMIC: a torn or absent write is indistinguishable from a
      # crash, and the parent scores both as failure.
      printf '%s' "$rc" >"$rundir/$job.status.tmp" && mv -f "$rundir/$job.status.tmp" "$rundir/$job.status"
      exit "$rc"
    ) &
    live_pids+=("$!"); live_jobs+=("$job"); live_groups+=("$(job_group "$job")")
    _DISPATCH_ALL_PIDS="$_DISPATCH_ALL_PIDS $!"
    launched=$((launched + 1))
    echo "ci-local: launched job '${job}' (pid $!, group $(job_group "$job"))"
  }

  _dispatch_reap() { # drop finished children from the live lists
    local -a np=() nj=() ng=()
    local i
    for i in $(seq 0 $(( ${#live_pids[@]} - 1 )) ); do
      [ "${#live_pids[@]}" -gt 0 ] || break
      if kill -0 "${live_pids[$i]}" 2>/dev/null; then
        np+=("${live_pids[$i]}"); nj+=("${live_jobs[$i]}"); ng+=("${live_groups[$i]}")
      else
        wait "${live_pids[$i]}" 2>/dev/null
      fi
    done
    # `${x[@]+"${x[@]}"}` is the bash-3.2 + `set -u` safe empty-array expansion.
    live_pids=(${np[@]+"${np[@]}"}); live_jobs=(${nj[@]+"${nj[@]}"}); live_groups=(${ng[@]+"${ng[@]}"})
    return 0
  }

  _dispatch_group_busy() { # group
    local g
    for g in ${live_groups[@]+"${live_groups[@]}"}; do [ "$g" = "$1" ] && return 0; done
    return 1
  }

  while [ "${#pending[@]}" -gt 0 ] || [ "${#live_pids[@]}" -gt 0 ]; do
    local -a remaining=()
    local job grp
    for job in ${pending[@]+"${pending[@]}"}; do
      grp="$(job_group "$job")"
      if [ "${#live_pids[@]}" -lt "$width" ] && { [ "$grp" = none ] || ! _dispatch_group_busy "$grp"; }; then
        _dispatch_launch "$job"
      else
        remaining+=("$job")
      fi
    done
    pending=(${remaining[@]+"${remaining[@]}"})
    [ "${#pending[@]}" -eq 0 ] && [ "${#live_pids[@]}" -eq 0 ] && break
    sleep 0.2
    _dispatch_reap
    if [ -n "$failfast_file" ] && [ -f "$failfast_file" ]; then
      failfast_hit="$(cat "$failfast_file" 2>/dev/null)"
      local ff_job="${failfast_hit%%$'\t'*}" ff_step="${failfast_hit#*$'\t'}"
      echo ""
      echo "ci-local: FAIL FAST — step '${ff_step}' (job ${ff_job}) failed; stopping this run's other jobs"
      echo "──────── last 40 lines of job '${ff_job}' ────────"
      tail -n 40 "$rundir/${ff_job}.log" 2>/dev/null || true
      echo "────────────────────────────────────────────────"
      echo "ci-local: re-run just that step:  GOATOS_SKIP_REASON=\"re-run failed step\" GOATOS_CI_ONLY_STEP='${ff_step}' tools/ci/run-local-ci.sh ${ff_job}"
      [ "${#live_pids[@]}" -eq 0 ] || _dispatch_stop_pids "${live_pids[@]}"
      live_pids=(); live_jobs=(); live_groups=()
      for job in ${pending[@]+"${pending[@]}"}; do
        RESULTS+=("FAIL  job ${job} not started (fail-fast after '${ff_step}')")
      done
      pending=()
      [ -f "$rundir/${ff_job}.failures" ] || FAILURES+=("${ff_step}")
      break
    fi
  done
  wait  # belt and braces; every child is already terminal
  unset GOATOS_CI_FAILFAST_FILE

  # ───────── accounting: the only source of truth is the filesystem ─────────
  local accounted=0 status line
  for job in "${jobs[@]}"; do
    status="$(cat "$rundir/$job.status" 2>/dev/null || true)"
    case "$status" in ''|*[!0-9]*) status=97 ;; esac     # ABSENCE == FAILURE

    echo ""
    echo "════════ job '${job}' output ════════"        # replayed in SELECTION order
    cat "$rundir/$job.log" 2>/dev/null || echo "(no log captured for ${job})"

    if [ -s "$rundir/$job.results" ]; then
      while IFS= read -r line; do RESULTS+=("$line"); done <"$rundir/$job.results"
    fi
    if [ -s "$rundir/$job.timings" ]; then
      while IFS= read -r line; do TIMINGS+=("$line"); done <"$rundir/$job.timings"
      [ -f "$rundir/$job.failures" ] && while IFS= read -r line; do [ -n "$line" ] && FAILURES+=("$line"); done <"$rundir/$job.failures"
    fi
    if [ "$job" = android ]; then
      screenshots_ran="$(cat "$rundir/$job.screenshots" 2>/dev/null || true)"
      if [ -z "$screenshots_ran" ]; then
        fail=1
        screenshots_ran="unknown"
        RESULTS+=("FAIL  android job reported NO screenshot coverage — receipt refused")
      fi
    fi
    if [ "$status" -ne 0 ]; then
      fail=1
      case " ${FAILED_JOBS[*]:-} " in *" ${job} "*) ;; *) FAILED_JOBS+=("$job") ;; esac
      [ "$status" -eq 97 ] && \
        RESULTS+=("FAIL  job ${job} produced NO status file (signal/OOM/crash) — treated as FAILED")
    fi
    accounted=$((accounted + 1))
  done

  # Per-job WALL time (the critical path is the max, not the sum). Written for
  # land-main's 20-min budget report; instrumentation only, never a verdict.
  local wallf
  wallf="$(git rev-parse --git-path goatos-ci-local-jobwall.tsv 2>/dev/null || echo /dev/null)"
  : >"$wallf" 2>/dev/null || true
  for job in "${jobs[@]}"; do
    line="$(cat "$rundir/$job.wall" 2>/dev/null || echo "?")"
    echo "ci-local: job wall ${job} ${line}s"
    printf '%s\t%s\n' "$job" "$line" >>"$wallf" 2>/dev/null || true
  done

  if [ -n "$failfast_hit" ]; then
    fail=1
  elif [ "$accounted" -ne "${#jobs[@]}" ] || [ "$accounted" -ne "$launched" ]; then
    fail=1
    RESULTS+=("FAIL  job accounting mismatch: selected=${#jobs[@]} launched=${launched} accounted=${accounted}")
  fi

  _dispatch_clear_traps
  _DISPATCH_ALL_PIDS=""
  _DISPATCH_RUNDIR=""
  case "${GOATOS_CI_KEEP_LOGS:-0}" in
    1|true|TRUE|True)
      echo "ci-local: preserved parallel job logs at $rundir"
      ;;
    *)
      rm -rf "$rundir"
      ;;
  esac
  return 0
}
