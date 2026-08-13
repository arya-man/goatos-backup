#!/usr/bin/env bash
# check-parallel-dispatch-cleanup.sh — Ctrl-C during a parallel ci-local run must
# not leave orphan children or leaked run dirs.
#
# WHY THIS EXISTS:
#   dispatch_jobs backgrounds up to N jobs. Without INT/TERM/EXIT traps, killing
#   the parent orphans every child. In this repo that specifically means orphan
#   GradleWorkerMain JVMs, which hold Gradle build locks and wedge the NEXT build
#   — a documented hazard. It also leaks $TMPDIR/goatos-ci-local.* run dirs.
#
#   The traps were added and measured (137 procs / 3 orphans without them, 130 /
#   0 with them) but NOTHING CHECKED THEM: deleting every _dispatch_install_traps
#   call changed zero check results. An unguarded fix silently rots. This is the
#   guard.
#
# BEHAVIOURAL, NOT TEXTUAL: this does not grep for the word `trap`. It sources the
# real parallel-dispatch.sh, starts a real dispatch whose jobs are long sleeps,
# sends a real signal to the parent, and then asserts on real process and
# filesystem state. Deleting the traps makes it fail; adding a `# trap` comment
# does not make it pass.
set -uo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo"

DISPATCH="${GOATOS_DISPATCH_UNDER_TEST:-tools/ci/parallel-dispatch.sh}"
# TERM, not INT: bash sets SIGINT to SIG_IGN in background children of a
# non-interactive shell, so a backgrounded harness can never receive INT. The
# traps cover INT/TERM/EXIT identically, so TERM exercises the same handler.
SIGNAL="${GOATOS_DISPATCH_GUARD_SIGNAL:-TERM}"
rc=0
fail() { echo "!! parallel-dispatch cleanup guard: $*" >&2; rc=1; }

sandbox="$(mktemp -d "${TMPDIR:-/tmp}/goatos-dispatch-guard.XXXXXX")"
marker="$sandbox/child-marker"
trap 'rm -rf "$sandbox"' EXIT

# A harness that mimics run-local-ci.sh's contract just enough to drive
# dispatch_jobs: each "job" is a long sleep that writes a PID file we can hunt for.
cat >"$sandbox/harness.sh" <<HARNESS
set -uo pipefail
cd "$repo"
fail=0
declare -a RESULTS TIMINGS FAILED_JOBS
screenshots_ran="not-applicable"
current_job=""
step() { shift; "\$@"; }
record_timing() { :; }
ci_parallel_width() { echo 3; }
run_job() {
  # Long-lived grandchild, recorded by PID so the guard can detect an orphan.
  # NOTE: \$BASHPID does not exist in macOS bash 3.2 and \$\$ in a subshell still
  # reports the PARENT's pid, so neither identifies this child. Backgrounding and
  # capturing \$! records the real process — and a bare \`sleep\` is exactly the
  # shape (a grandchild under the job subshell) that a plain \`kill \$child\`
  # misses and only a process-GROUP kill reaps.
  sleep 120 &
  echo \$! >>"$marker"
  wait \$!
}
# F-E: an absolute override must not become "$repo//abs/path" — the harness would
# die from a missing file, i.e. red for an INCIDENTAL reason, not the defect.
case "$DISPATCH" in /*) . "$DISPATCH" ;; *) . "$repo/$DISPATCH" ;; esac
echo "\$\$" >"$sandbox/parent.pid"
dispatch_jobs alpha beta gamma
HARNESS

before_dirs="$(ls -d "${TMPDIR:-/tmp}"/goatos-ci-local.* 2>/dev/null | wc -l | tr -d ' ')"

bash "$sandbox/harness.sh" >"$sandbox/out.log" 2>&1 &
harness_pid=$!

# Wait for all three children to actually be running before interrupting.
for _ in $(seq 1 100); do
  [ "$( { wc -l <"$marker" ; } 2>/dev/null || echo 0)" -ge 3 ] && break
  sleep 0.1
done
started="$( { wc -l <"$marker" ; } 2>/dev/null | tr -d ' ' || echo 0)"
if [ "${started:-0}" -lt 3 ]; then
  fail "harness never started 3 jobs (started=${started:-0}); cannot test interrupt cleanup"
  exit "$rc"
fi

kill -"$SIGNAL" "$harness_pid" 2>/dev/null
for _ in $(seq 1 100); do
  kill -0 "$harness_pid" 2>/dev/null || break
  sleep 0.1
done
kill -0 "$harness_pid" 2>/dev/null && { kill -KILL "$harness_pid" 2>/dev/null; fail "parent did not exit within 10s of SIG${SIGNAL}"; }
wait "$harness_pid" 2>/dev/null

sleep 0.5

# --- assertion 1: no orphan children survive the interrupt ---
orphans=0
while read -r pid; do
  [ -n "$pid" ] || continue
  if kill -0 "$pid" 2>/dev/null; then
    orphans=$((orphans + 1))
    kill -KILL "$pid" 2>/dev/null   # never leave the guard's own mess behind
  fi
done <"$marker"
if [ "$orphans" -ne 0 ]; then
  fail "${orphans} child process(es) SURVIVED SIG${SIGNAL} to the parent. In a real run these are Gradle workers holding build locks. Install INT/TERM/EXIT traps that kill the child process group."
fi

# --- assertion 2: no leaked run dir ---
after_dirs="$(ls -d "${TMPDIR:-/tmp}"/goatos-ci-local.* 2>/dev/null | wc -l | tr -d ' ')"
if [ "${after_dirs:-0}" -gt "${before_dirs:-0}" ]; then
  fail "an interrupted run LEAKED $(( after_dirs - before_dirs )) run dir(s) under \$TMPDIR (goatos-ci-local.*). The EXIT trap must remove the run dir."
fi

if [ "$rc" -eq 0 ]; then
  echo "parallel-dispatch cleanup guard: SIG${SIGNAL} left 0 orphans and 0 leaked run dirs"
fi
exit "$rc"
