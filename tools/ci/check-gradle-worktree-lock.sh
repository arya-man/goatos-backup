#!/usr/bin/env bash
# check-gradle-worktree-lock.sh — behavioural guard for tools/ci/gradle-worktree-lock.sh.
#
# WHY THIS EXISTS
#   The lock is FAIL-OPEN by design, which means a lock that has stopped
#   excluding anything fails NOTHING. It silently returns the measured ~3x
#   cross-worktree Gradle penalty and no run goes red. That is the definition of
#   a change that needs a guard, and the definition of a guard that must be
#   proven rather than trusted.
#
# BEHAVIOURAL, NOT TEXTUAL: this never greps for `mkdir`, `trap`, or `lock`. It
# sources the real library, drives real processes, sends real signals, and
# asserts on real process and filesystem state. It runs entirely inside a
# `mktemp -d` sandbox pointed at by GOATOS_CI_GRADLE_LOCK_DIR with a pinned
# GRADLE_USER_HOME, so it can never touch a live build's lock.
#
# THE SINGLE MOST IMPORTANT LINE IN THIS FILE is the repeated
# `GOATOS_CI_GRADLE_LOCK_TIMEOUT=300` on the exclusion/reclaim cases. The lock
# fails OPEN, so with a short timeout a library whose acquire always returns 0 —
# i.e. no mutex at all — still lets every waiter proceed and every one of those
# cases goes green while proving nothing. That is the inert-guard shape this
# repo has had to delete four times.
#
# Cases: (a) exclusion + non-owner-release theft, (b) dead-owner break,
# (c) SIGKILL survivability, (d) SIGTERM release + the holder actually DIES,
# (e) fail-open on timeout, (f) exit-status transparency across 6 combinations,
# (g) trace + opt-out neutrality (library probe AND run-local-ci.sh wiring),
# (h) concurrent stale-break admits exactly one, (i) SIGINT release + death,
# (j) a recycled pid is not proof of identity, (k) the owner pid is the
# ACQUIRING SUBSHELL, (l) a pre-existing EXIT trap survives install+clear,
# (m)/(m2) a TERM at the android lane must not signal its parent — with the lock
# enabled AND on the GOATOS_CI_GRADLE_LOCK=0 opt-out path, (n) an orphaned
# break-lock stays bounded and honours the timeout, (o) non-numeric and
# out-of-range knobs are sanitised, (p) an UNUSABLE lock path fails open on the
# first look instead of being polled for the whole timeout, (q) a hostname flap
# between acquire and release does not leak the lock (and does not become a
# general release bypass), (r) a stale break acts only on the lockdir its
# decision was made about.
#
# ONE MACHINE-WIDE SIDE EFFECT, named rather than hidden: case (g) drives the
# real `run-local-ci.sh android` under GOATOS_CI_TRACE_ONLY. Everything this
# guard does ITSELF is sandboxed sleeps with no Gradle, but that target's
# stale-worker reaper sits outside `step` and used to SIGKILL machine-wide
# GradleWorkerMain processes it does not own. It is now trace-gated, and case
# (g3) is what holds it that way.
#
# Self-test: tools/ci/check-gradle-worktree-lock.test.sh drives this guard
# against 22 mutated copies of the library via GOATOS_GRADLE_LOCK_UNDER_TEST and
# requires a RED for each. A guard that has never been observed to fail is a
# guard nobody has proven.
set -uo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo"

# Same seam convention as GOATOS_DISPATCH_UNDER_TEST. An absolute override must
# not become "$repo//abs/path".
LIB_REL="${GOATOS_GRADLE_LOCK_UNDER_TEST:-tools/ci/gradle-worktree-lock.sh}"
case "$LIB_REL" in /*) LIB="$LIB_REL" ;; *) LIB="$repo/$LIB_REL" ;; esac
[ -r "$LIB" ] || { echo "!! gradle-worktree-lock guard: library not readable: $LIB" >&2; exit 1; }

rc=0
fail() { echo "!! gradle-worktree-lock guard: $*" >&2; rc=1; }

SB="$(mktemp -d "${TMPDIR:-/tmp}/goatos-gradle-lock-guard.XXXXXX")"
export GRADLE_USER_HOME="$SB/gradle-home"
export GOATOS_CI_GRADLE_LOCK_DIR="$SB"
mkdir -p "$GRADLE_USER_HOME"
export GUARD_REPO="$repo" GUARD_LIB="$LIB"

# Every process this guard starts is tracked so cleanup can reap it. A guard
# that leaves its own sleeps behind is a guard that eventually gets blamed for
# somebody else's slow machine.
PIDS=""
track() { PIDS="$PIDS $1"; }
cleanup() {
  local p
  for p in $PIDS; do kill -KILL "$p" 2>/dev/null; done
  rm -rf "$SB"
}
trap cleanup EXIT

# shellcheck source=tools/ci/gradle-worktree-lock.sh
. "$LIB"
LOCKDIR="$(gradle_lock_dir)"

t_start=$SECONDS
# phase() is a per-case timing trace. This guard is diff-scoped to a
# tools/ci/gradle-worktree-lock.sh diff, but a case that quietly gets slow still
# has to be visible — an earlier draft of this file cost 133 s and nobody could
# say which case owned it.
phase() { echo "── gradle-worktree-lock guard: case $1 at t=$(( SECONDS - t_start ))s"; }

reset_lock() { rm -rf "$LOCKDIR" "$LOCKDIR".break "$LOCKDIR".stale.* 2>/dev/null; }

wait_for() { # file deadline_seconds
  local f="$1" n="${2:-10}" i=0
  while [ "$i" -lt $(( n * 10 )) ]; do
    [ -e "$f" ] && return 0
    sleep 0.1; i=$(( i + 1 ))
  done
  return 1
}
wait_gone() { # path deadline_seconds
  local f="$1" n="${2:-10}" i=0
  while [ "$i" -lt $(( n * 10 )) ]; do
    [ -e "$f" ] || return 0
    sleep 0.1; i=$(( i + 1 ))
  done
  return 1
}
plant_owner() { # pid host epoch ident
  mkdir -p "$LOCKDIR"
  printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$1" "$2" "$SB" planted "$3" "${4:-}" >"$LOCKDIR/owner"
}
dead_pid() { local p; sleep 0 & p=$!; wait "$p" 2>/dev/null; printf '%s' "$p"; }

# ── fixtures ────────────────────────────────────────────────────────────────
# A holder must NOT block in a FOREGROUND `sleep`: bash defers a trap until the
# foreground command finishes, so `sleep 600` would swallow its own SIGTERM for
# ten minutes and case (d) would misread a working trap as broken.
cat >"$SB/holder.sh" <<'HOLDER'
set -uo pipefail
cd "$GUARD_REPO"
. "$GUARD_LIB"
gradle_lock_acquire "${1:-holder}"
gradle_lock_install_trap
: >"$2"
sleep 600 & wait $!
# THE discriminator for "the handler released and RETURNED". Sabotaging the
# TERM/EXIT trap line alone is NOT observable — bash runs the EXIT trap when a
# caught signal terminates the shell, and a holder with no TERM handler dies on
# the default disposition with the same 143, so an rc check alone cannot tell a
# fatal handler from a non-fatal one. What IS observable is that the run CARRIES
# ON past the interrupt: in production that is `android benchmark compile`
# starting, unlocked, after a Ctrl-C the maintainer believes they issued.
: >"$2.survived"
HOLDER

# dispatchholder.sh mirrors dispatch_jobs' shape exactly: a parent with an
# observable TERM trap forking `( acquire; install_trap; … ) &`. It is the only
# fixture in which `$$` and the acquiring shell differ, which is the whole point
# — cases (k), (m) and (m2) are invisible to a top-level holder.
cat >"$SB/dispatchholder.sh" <<'DISPATCH'
set -uo pipefail
cd "$GUARD_REPO"
. "$GUARD_LIB"
signalled="$1"; ready="$2"; pidfile="$3"; rcfile="$4"; final="$5"
trap 'echo YES >"$signalled"' TERM
(
  gradle_lock_acquire "android gradle"
  gradle_lock_install_trap
  : >"$ready"
  sleep 600 & wait $!
) &
child=$!
echo "$child" >"$pidfile"
wait "$child"; echo "$?" >"$rcfile"
: >"$final"
# The parent deliberately OUTLIVES its child: case (k) asserts that the lock is
# reclaimable while run-local-ci.sh is still running the other lanes, which is
# the whole point of recording the acquiring subshell rather than $$. The guard
# reaps this.
sleep 60 & wait $!
DISPATCH

# ── (a) mutual exclusion + non-owner release is inert ───────────────────────
phase a
reset_lock
GOATOS_CI_GRADLE_LOCK_TIMEOUT=300 bash "$SB/holder.sh" A "$SB/a.ready" >/dev/null 2>&1 &
a_pid=$!; track "$a_pid"
if ! wait_for "$SB/a.ready" 10; then
  fail "(a) holder A never acquired the lock — cannot test exclusion"
else
  ( GOATOS_CI_GRADLE_LOCK_TIMEOUT=300 bash -c '
      cd "$GUARD_REPO"; . "$GUARD_LIB"
      gradle_lock_run B touch "$1"' _ "$SB/a.marker" ) >/dev/null 2>&1 &
  b_pid=$!; track "$b_pid"
  sleep 3
  [ -e "$SB/a.marker" ] && fail "(a) waiter B entered the critical section while holder A still held the lock — there is no mutual exclusion"

  # theft: a NON-owner release must leave the lockdir alone.
  # shellcheck source=tools/ci/gradle-worktree-lock.sh
  ( cd "$repo" || exit 0; . "$LIB"; gradle_lock_release ) >/dev/null 2>&1
  [ -d "$LOCKDIR" ] || fail "(a) a NON-OWNER gradle_lock_release deleted the holder's LIVE lock — one worktree can now delete another's lock and both compile at once"

  kill -TERM "$a_pid" 2>/dev/null
  wait_for "$SB/a.marker" 12 || fail "(a) waiter B never acquired the lock after holder A released it — the queue does not drain"
  kill -KILL "$b_pid" 2>/dev/null
fi
reset_lock

# ── (b) dead-owner break ────────────────────────────────────────────────────
phase b
reset_lock
plant_owner "$(dead_pid)" "$(hostname)" "$(date +%s)" ""
b_err="$SB/b.err"
( GOATOS_CI_GRADLE_LOCK_TIMEOUT=300 bash -c '
    cd "$GUARD_REPO"; . "$GUARD_LIB"
    gradle_lock_acquire b; : >"$1"' _ "$SB/b.got" ) >"$b_err" 2>&1 &
bb=$!; track "$bb"
if ! wait_for "$SB/b.got" 6; then
  fail "(b) a lock owned by a PROVABLY DEAD pid on this host was never reclaimed. parallel-dispatch SIGKILLs the descendant tree, so a killed holder cannot release itself — dead-owner reclaim IS the interrupt path"
elif ! grep -q 'STALE Gradle lock' "$b_err"; then
  fail "(b) the stale lock was taken over SILENTLY — a break must say so on stderr"
fi
kill -KILL "$bb" 2>/dev/null
reset_lock

# ── (c) SIGKILL survivability ───────────────────────────────────────────────
phase c
reset_lock
GOATOS_CI_GRADLE_LOCK_TIMEOUT=300 bash "$SB/holder.sh" C "$SB/c.ready" >/dev/null 2>&1 &
c_pid=$!; track "$c_pid"
if ! wait_for "$SB/c.ready" 10; then
  fail "(c) holder C never acquired the lock"
else
  kill -KILL "$c_pid" 2>/dev/null
  sleep 0.5
  [ -d "$LOCKDIR" ] || fail "(c) fixture broke: a SIGKILLed holder somehow released its lock, so this case cannot test recovery"
  ( GOATOS_CI_GRADLE_LOCK_TIMEOUT=300 bash -c '
      cd "$GUARD_REPO"; . "$GUARD_LIB"
      gradle_lock_acquire c2; : >"$1"' _ "$SB/c.got" ) >/dev/null 2>&1 &
  cc=$!; track "$cc"
  wait_for "$SB/c.got" 8 || fail "(c) a SIGKILLed holder (what _dispatch_kill_tree … KILL does) WEDGED the lock: one Ctrl-C now blocks every android job on this machine"
  kill -KILL "$cc" 2>/dev/null
fi
reset_lock

# ── (d) SIGTERM releases AND the holder dies ────────────────────────────────
phase d
reset_lock
GOATOS_CI_GRADLE_LOCK_TIMEOUT=300 bash "$SB/holder.sh" D "$SB/d.ready" >/dev/null 2>&1 &
d_pid=$!; track "$d_pid"
if ! wait_for "$SB/d.ready" 10; then
  fail "(d) holder D never acquired the lock"
else
  kill -TERM "$d_pid" 2>/dev/null
  wait_gone "$LOCKDIR" 5 || fail "(d) SIGTERM did not release the lock — install the release trap on EXIT, INT and TERM"
  wait "$d_pid" 2>/dev/null; d_rc=$?
  kill -0 "$d_pid" 2>/dev/null && fail "(d) the holder SURVIVED SIGTERM. A handler that releases and RETURNS makes the signal non-fatal: the run drops the mutex and then launches the next Gradle step the maintainer believes they cancelled"
  [ "$d_rc" -eq 143 ] || fail "(d) the holder exited ${d_rc}, not 143 — the handler must re-raise with the default disposition so the caller sees a real 128+n"
  [ -e "$SB/d.ready.survived" ] && fail "(d) the interrupted run CONTINUED past SIGTERM — a handler that releases and RETURNS makes the signal non-fatal, so the mutex is dropped and the NEXT Gradle step starts unlocked"
fi
reset_lock

# ── (e) fail-open on timeout ────────────────────────────────────────────────
phase e
reset_lock
GOATOS_CI_GRADLE_LOCK_TIMEOUT=300 bash "$SB/holder.sh" E "$SB/e.ready" >/dev/null 2>&1 &
e_pid=$!; track "$e_pid"
if ! wait_for "$SB/e.ready" 10; then
  fail "(e) holder E never acquired the lock"
else
  e_out="$SB/e.err"
  GOATOS_CI_GRADLE_LOCK_TIMEOUT=2 bash -c '
    cd "$GUARD_REPO"; . "$GUARD_LIB"
    gradle_lock_run E2 touch "$1"' _ "$SB/e.marker" >"$e_out" 2>&1
  e_rc=$?
  [ "$e_rc" -eq 0 ] || fail "(e) a lock TIMEOUT returned ${e_rc}. The lock is a scheduler: it must never be able to fail a step or turn a landing red"
  [ -e "$SB/e.marker" ] || fail "(e) the wrapped command did NOT run after the lock timed out — the run must proceed unlocked, not be skipped"
  grep -q 'PROCEEDING WITHOUT THE LOCK' "$e_out" || fail "(e) proceeding unlocked was SILENT — it must say so loudly"
  kill -KILL "$e_pid" 2>/dev/null
fi
reset_lock

# ── (f) exit-status transparency, 6 combinations ────────────────────────────
phase f
run_wrapped() { # timeout cmd -> rc
  GOATOS_CI_GRADLE_LOCK_TIMEOUT="$1" bash -c '
    cd "$GUARD_REPO"; . "$GUARD_LIB"
    gradle_lock_run F "$1"' _ "$2" >/dev/null 2>&1
  echo $?
}
reset_lock
[ "$(run_wrapped 300 true)"  = 0 ] || fail "(f) free lock + \`true\`: rc was not 0"
[ "$(run_wrapped 300 false)" = 1 ] || fail "(f) free lock + \`false\`: rc was not 1 — the wrapper swallowed the command's status"
reset_lock
# contended-then-released
for combo in "true 0" "false 1"; do
  set -- $combo
  GOATOS_CI_GRADLE_LOCK_TIMEOUT=300 bash -c '
    cd "$GUARD_REPO"; . "$GUARD_LIB"
    gradle_lock_acquire hold; sleep 1; gradle_lock_release' >/dev/null 2>&1 &
  hp=$!; track "$hp"
  sleep 0.3
  got="$(run_wrapped 300 "$1")"
  [ "$got" = "$2" ] || fail "(f) contended-then-released + \`$1\`: rc was ${got}, expected $2"
  wait "$hp" 2>/dev/null
done
reset_lock
# timed out
GOATOS_CI_GRADLE_LOCK_TIMEOUT=300 bash "$SB/holder.sh" F "$SB/f.ready" >/dev/null 2>&1 &
f_pid=$!; track "$f_pid"
if wait_for "$SB/f.ready" 10; then
  [ "$(run_wrapped 1 true)"  = 0 ] || fail "(f) timed-out lock + \`true\`: rc was not 0"
  [ "$(run_wrapped 1 false)" = 1 ] || fail "(f) timed-out lock + \`false\`: rc was not 1 — a lock bug must never be able to change a verdict"
fi
kill -KILL "$f_pid" 2>/dev/null
reset_lock

# ── (g) trace and opt-out neutrality ────────────────────────────────────────
phase g
reset_lock
GOATOS_CI_GRADLE_LOCK_TIMEOUT=300 bash "$SB/holder.sh" G "$SB/g.ready" >/dev/null 2>&1 &
g_pid=$!; track "$g_pid"
if ! wait_for "$SB/g.ready" 10; then
  fail "(g) holder G never acquired the lock"
else
  # A trace acquire that ENGAGES cannot possibly finish inside 5s against a live
  # holder with a 300s timeout. Driving run-local-ci.sh alone cannot see this:
  # that script always sources the REAL library, so a mutated one is invisible.
  for probe in "GOATOS_CI_TRACE_ONLY=1" "GOATOS_CI_GRADLE_LOCK=0"; do
    ( eval "export $probe"
      GOATOS_CI_GRADLE_LOCK_TIMEOUT=300 bash -c '
        cd "$GUARD_REPO"; . "$GUARD_LIB"
        gradle_lock_run probe touch "$1"' _ "$SB/g.$probe.marker" ) >/dev/null 2>&1 &
    gp=$!; track "$gp"
    wait_for "$SB/g.$probe.marker" 5 || \
      fail "(g) with ${probe} the lock still ENGAGED and blocked behind a live holder. check-android-screenshot-proof.sh drives run-local-ci.sh android under trace, so this turns a required-in-CI guard into a hang"
    kill -KILL "$gp" 2>/dev/null
  done
  kill -KILL "$g_pid" 2>/dev/null
fi
reset_lock
# Wiring probe: the real script under trace must create NO lock and still exit 3.
g_sb="$SB/trace-dir"; mkdir -p "$g_sb"
GOATOS_CI_GRADLE_LOCK_DIR="$g_sb" GOATOS_CI_TRACE_ONLY=1 bash tools/ci/run-local-ci.sh android >/dev/null 2>&1
g_rc=$?
[ "$g_rc" -eq 3 ] || fail "(g) GOATOS_CI_TRACE_ONLY=1 run-local-ci.sh android exited ${g_rc}, not 3 — a trace run must stay ahead of the receipt writer"
[ -z "$(ls -A "$g_sb" 2>/dev/null)" ] || fail "(g) a TRACE run of run-local-ci.sh android created a lock dir; trace executes nothing and must acquire nothing"
reset_lock

# (g3) THIS guard drives run-local-ci.sh android, whose first action used to be
# an unconditional reap that `kill -9`s every GradleWorkerMain on the MACHINE
# older than 30 minutes — other agents' builds included, which AGENTS.md bans
# outright. It sits outside `step`, so trace mode did not short-circuit it, and
# `make guardrails` therefore became a machine-wide killer of somebody else's
# long build. Asserted on the reaper's own output, which it always prints.
g_out="$SB/g.reap.out"
GOATOS_CI_GRADLE_LOCK_DIR="$g_sb" GOATOS_CI_TRACE_ONLY=1 \
  bash tools/ci/run-local-ci.sh android >"$g_out" 2>&1
if grep -qE 'stale Gradle test worker|reaped [0-9]+ stale' "$g_out"; then
  fail "(g3) a TRACE run of run-local-ci.sh android ran the stale-worker reaper, which SIGKILLs machine-wide GradleWorkerMain processes it does not own. A trace run starts no Gradle and has nothing to reap; two required guards drive this target under trace"
fi
reset_lock

# ── (h) concurrent stale-break admits exactly ONE ───────────────────────────
# rename(2) is atomic w.r.t. the PATH, not the inode the decision was made
# about, so a bare `mv` break lets a loser move the WINNER'S FRESH lock and both
# enter. 8 racers x 3 rounds: 4x1 missed it every time.
phase h
h_overlap="$SB/h.overlap"
for round in 1 2 3; do
  reset_lock
  plant_owner "$(dead_pid)" "$(hostname)" "$(date +%s)" ""
  hp_list=""
  for i in 1 2 3 4 5 6 7 8; do
    ( GOATOS_CI_GRADLE_LOCK_TIMEOUT=300 bash -c '
        cd "$GUARD_REPO"; . "$GUARD_LIB"
        gradle_lock_acquire "racer$1"
        if [ -e "$2" ]; then echo "round $3 racer $1" >>"$4"; fi
        : >"$2"; sleep 0.2; rm -f "$2"
        gradle_lock_release' _ "$i" "$SB/h.busy" "$round" "$h_overlap" ) >/dev/null 2>&1 &
    hp_list="$hp_list $!"; track "$!"
  done
  for p in $hp_list; do wait "$p" 2>/dev/null; done
  rm -f "$SB/h.busy"
done
if [ -s "$h_overlap" ]; then
  fail "(h) $(wc -l <"$h_overlap" | tr -d ' ') racer(s) entered the critical section while another was inside, recovering the SAME dead-owner lock. A stale break must be serialised AND must re-validate the owner under that serialisation"
fi
reset_lock

# ── (i) SIGINT releases AND the holder dies ─────────────────────────────────
# bash sets SIGINT to SIG_IGN in background jobs of a NON-interactive shell and
# POSIX forbids un-ignoring it, so the holder is exec'd through perl with the
# default disposition restored. Without this the case can never fire at all.
phase i
reset_lock
if command -v perl >/dev/null 2>&1; then
  perl -e '$SIG{INT}="DEFAULT"; exec @ARGV' bash "$SB/holder.sh" I "$SB/i.ready" >/dev/null 2>&1 &
  i_pid=$!; track "$i_pid"
  if ! wait_for "$SB/i.ready" 10; then
    fail "(i) holder I never acquired the lock"
  else
    kill -INT "$i_pid" 2>/dev/null
    wait_gone "$LOCKDIR" 5 || fail "(i) SIGINT did not release the lock"
    wait "$i_pid" 2>/dev/null; i_rc=$?
    kill -0 "$i_pid" 2>/dev/null && fail "(i) the holder SURVIVED Ctrl-C. A handler that releases and RETURNS means the run keeps building after the abort, unlocked"
    [ "$i_rc" -eq 130 ] || fail "(i) the holder exited ${i_rc}, not 130 — the INT handler must re-raise with the default disposition"
    [ -e "$SB/i.ready.survived" ] && fail "(i) the interrupted run CONTINUED past Ctrl-C — it drops the mutex and keeps building, and a queued second worktree starts compiling alongside it"
  fi
else
  fail "(i) perl is unavailable, so the SIGINT case cannot run — treating an unrunnable case as red rather than as a pass"
fi
reset_lock

# ── (j) a recycled pid is not proof of identity ─────────────────────────────
phase j
reset_lock
sleep 600 & j_live=$!; track "$j_live"
plant_owner "$j_live" "$(hostname)" "$(( $(date +%s) - 4000 ))" "NOT-THE-REAL-START-TIME"
( GOATOS_CI_GRADLE_LOCK_TIMEOUT=300 GOATOS_CI_GRADLE_LOCK_STALE_SECONDS=1 bash -c '
    cd "$GUARD_REPO"; . "$GUARD_LIB"
    gradle_lock_acquire j; : >"$1"' _ "$SB/j.got" ) >/dev/null 2>&1 &
jj=$!; track "$jj"
wait_for "$SB/j.got" 8 || fail "(j) a lock whose recorded pid is ALIVE but is a DIFFERENT process (a recycled pid — \$TMPDIR survives a reboot) was never reclaimed. kill -0 proves liveness, never identity"
kill -KILL "$jj" "$j_live" 2>/dev/null
reset_lock

# ── (k) the owner pid is the ACQUIRING SUBSHELL, not $$ ─────────────────────
phase k
reset_lock
bash "$SB/dispatchholder.sh" "$SB/k.sig" "$SB/k.ready" "$SB/k.pid" "$SB/k.rc" "$SB/k.final" >/dev/null 2>&1 &
k_parent=$!; track "$k_parent"
if ! wait_for "$SB/k.ready" 10; then
  fail "(k) the dispatch-shaped holder never acquired the lock"
else
  k_child="$(cat "$SB/k.pid" 2>/dev/null)"
  kill -KILL "$k_child" 2>/dev/null
  sleep 0.5
  kill -0 "$k_parent" 2>/dev/null || fail "(k) fixture broke: the parent died with the child, so this case cannot test parent-alive reclaim"
  ( GOATOS_CI_GRADLE_LOCK_TIMEOUT=300 bash -c '
      cd "$GUARD_REPO"; . "$GUARD_LIB"
      gradle_lock_acquire k2; : >"$1"' _ "$SB/k.got" ) >/dev/null 2>&1 &
  kk=$!; track "$kk"
  wait_for "$SB/k.got" 8 || fail "(k) the android LANE was SIGKILLed while run-local-ci.sh lived on, and the lock was never reclaimed — the owner file recorded \$\$ (the parent), so kill -0 proved the liveness of the wrong process"
  kill -KILL "$kk" 2>/dev/null
fi
kill -KILL "$k_parent" 2>/dev/null
reset_lock

# ── (l) a pre-existing EXIT trap survives install + clear ───────────────────
phase l
reset_lock
bash -c '
  cd "$GUARD_REPO"; . "$GUARD_LIB"
  trap "touch \"$1\"" EXIT
  gradle_lock_acquire l
  gradle_lock_install_trap
  gradle_lock_clear_trap
  gradle_lock_release
  exit 0' _ "$SB/l.exit" >/dev/null 2>&1
[ -e "$SB/l.exit" ] || fail "(l) gradle_lock_clear_trap DISCARDED an EXIT trap the caller installed first. The moment run-local-ci.sh grows a receipt/tempdir EXIT trap, run_android would silently delete it"
reset_lock

# ── (m)/(m2) TERMing the android lane must not signal its parent ────────────
dispatch_signal_case() { # label extra_env
  local label="$1" env_kv="$2"
  reset_lock
  ( [ -z "$env_kv" ] || eval "export $env_kv"
    bash "$SB/dispatchholder.sh" \
      "$SB/$label.sig" "$SB/$label.ready" "$SB/$label.pid" "$SB/$label.rc" "$SB/$label.final" ) >/dev/null 2>&1 &
  local parent=$!; track "$parent"
  if ! wait_for "$SB/$label.ready" 10; then
    fail "($label) the dispatch-shaped lane never started"
    kill -KILL "$parent" 2>/dev/null; return
  fi
  kill -TERM "$(cat "$SB/$label.pid" 2>/dev/null)" 2>/dev/null
  wait_for "$SB/$label.final" 10 || fail "($label) the parent never finished after its child was TERMed"
  [ -e "$SB/$label.sig" ] && fail "($label) TERMing the android LANE signalled its PARENT too — the handler re-raised at \$\$, which inside \`( … ) &\` is run-local-ci.sh itself. That kills the whole suite mid-dispatch, before parallel-dispatch's TERM-then-KILL escalation, and orphans GradleWorkerMain JVMs holding build locks"
  local crc; crc="$(cat "$SB/$label.rc" 2>/dev/null)"
  [ "${crc:-x}" = 143 ] || fail "($label) the android lane exited ${crc:-<none>}, not 143"
  kill -KILL "$parent" 2>/dev/null
  reset_lock
}
phase m
dispatch_signal_case m ""
# (m2) with the lock DISABLED, gradle_lock_acquire short-circuits before it can
# record the acquiring pid, so gradle_lock_install_trap is the ONLY thing
# recording it. Case (m) alone stays green when that recording is deleted —
# exactly the blind spot that let the suite-killing re-raise survive on the
# documented opt-out path.
phase m2
dispatch_signal_case m2 "GOATOS_CI_GRADLE_LOCK=0"

# ── (n) an orphaned break-lock stays bounded and honours the timeout ────────
phase n
reset_lock
plant_owner "$(dead_pid)" "$(hostname)" "$(date +%s)" ""
mkdir -p "$LOCKDIR.break"   # what a breaker KILLed between its mkdir and its rm leaves
n_t0=$SECONDS
( GOATOS_CI_GRADLE_LOCK_TIMEOUT=3 bash -c '
    cd "$GUARD_REPO"; . "$GUARD_LIB"
    gradle_lock_acquire n; echo "$?" >"$1"' _ "$SB/n.rc" ) >/dev/null 2>&1 &
nn=$!; track "$nn"
if ! wait_for "$SB/n.rc" 10; then
  fail "(n) with an ORPHANED ${LOCKDIR}.break in place, acquire did not return within 10s despite GOATOS_CI_GRADLE_LOCK_TIMEOUT=3 — the stale-break path is spinning past both the timeout check and the throttle"
else
  n_dt=$(( SECONDS - n_t0 ))
  [ "$n_dt" -le 8 ] || fail "(n) acquire took ${n_dt}s against a 3s timeout"
  [ "$(cat "$SB/n.rc" 2>/dev/null)" = 0 ] || fail "(n) acquire returned non-zero — fail-open is the contract"
fi
kill -KILL "$nn" 2>/dev/null
reset_lock

# ── (o) knob sanitisation: shape AND magnitude ──────────────────────────────
# (o1) is asserted on the SHELL'S OWN arithmetic error, which is the defect's
# signature and lands on the first poll. Asserting "returns 0 promptly" is not
# available: correctly sanitised, a garbage timeout falls back to the documented
# 1800s default, which behind a live holder is a 30-minute wait BY DESIGN.
phase o
reset_lock
GOATOS_CI_GRADLE_LOCK_TIMEOUT=300 bash "$SB/holder.sh" O "$SB/o.ready" >/dev/null 2>&1 &
o_pid=$!; track "$o_pid"
if ! wait_for "$SB/o.ready" 10; then
  fail "(o) holder O never acquired the lock"
else
  for bad in abc 99999999999999999999; do
    o_err="$SB/o.$bad.err"
    ( GOATOS_CI_GRADLE_LOCK_TIMEOUT="$bad" bash -c '
        cd "$GUARD_REPO"; . "$GUARD_LIB"; gradle_lock_acquire o' ) >"$o_err" 2>&1 &
    op=$!; track "$op"
    sleep 3
    kill -KILL "$op" 2>/dev/null
    if grep -q 'integer expression expected' "$o_err"; then
      fail "(o1) GOATOS_CI_GRADLE_LOCK_TIMEOUT='${bad}' reached the wait comparison unsanitised — every poll errors with status 2, so the fail-open branch can NEVER fire and acquire hangs forever"
    fi
  done
  kill -KILL "$o_pid" 2>/dev/null
fi
reset_lock
# (o2) is asserted by OUTCOME, not by stderr: sanitised, the expiry branch fires
# on an unverifiable 2000s-old lock and the acquirer really takes it (so its own
# release removes it); unsanitised, the branch is dead, the acquirer times out,
# proceeds unlocked, and the ghost lock is STILL THERE.
plant_owner 999999 "some-other-host.invalid" "$(( $(date +%s) - 2000 ))" ""
GOATOS_CI_GRADLE_LOCK_TIMEOUT=3 GOATOS_CI_GRADLE_LOCK_STALE_SECONDS=xyz bash -c '
  cd "$GUARD_REPO"; . "$GUARD_LIB"
  gradle_lock_acquire o2
  gradle_lock_release' >/dev/null 2>&1
if [ -d "$LOCKDIR" ]; then
  fail "(o2) a non-numeric GOATOS_CI_GRADLE_LOCK_STALE_SECONDS retired the EXPIRY branch: a lockdir ${LOCKDIR} owned by an UNVERIFIABLE host and 2000s old was never reclaimed, so the run proceeded unlocked"
fi
reset_lock

# ── (p) an UNUSABLE lock path fails open on the FIRST look ──────────────────
# `mkdir` reports "held" and "unusable" identically, and treating unusable as
# held costs the WHOLE timeout — 1800s at the shipped default — before the
# fail-open branch fires. A 30-minute silent stall on the android lane, which
# blocks the suite, inside a change whose entire purpose is wall clock.
#
# TIMEOUT=60 with a <=5s deadline is the load-bearing pair: with the early
# fail-open removed each shape polls the full 60s and the deadline catches it,
# while 60s is far too long to pass by accident.
phase p
for shape in file-at-path missing-parent unwritable-parent; do
  p_root="$SB/p.$shape"
  case "$shape" in
    file-at-path)
      mkdir -p "$p_root"
      : >"$(GOATOS_CI_GRADLE_LOCK_DIR="$p_root" gradle_lock_dir)" ;;
    missing-parent)   p_root="$SB/p.$shape/does/not/exist" ;;
    unwritable-parent) mkdir -p "$p_root"; chmod 500 "$p_root" ;;
  esac
  p_t0=$SECONDS
  ( GOATOS_CI_GRADLE_LOCK_DIR="$p_root" GOATOS_CI_GRADLE_LOCK_TIMEOUT=60 bash -c '
      cd "$GUARD_REPO"; . "$GUARD_LIB"; gradle_lock_acquire p; echo "$?" >"$1"' _ "$SB/p.$shape.rc" \
  ) >/dev/null 2>&1 &
  pp=$!; track "$pp"
  p_i=0
  while [ "$p_i" -lt 50 ]; do kill -0 "$pp" 2>/dev/null || break; sleep 0.1; p_i=$(( p_i + 1 )); done
  if kill -0 "$pp" 2>/dev/null; then
    kill -KILL "$pp" 2>/dev/null
    fail "(p) with an unusable lock path (${shape}) acquire did not return within 5s against a 60s timeout — an unusable path is being polled as if it were HELD, so it costs the entire timeout (1800s at the shipped default) before failing open"
  else
    [ "$(cat "$SB/p.$shape.rc" 2>/dev/null)" = 0 ] || \
      fail "(p) acquire returned non-zero on an unusable lock path (${shape}) — fail-open is the contract"
  fi
  [ "$shape" = unwritable-parent ] && chmod 700 "$p_root"
  : $(( SECONDS - p_t0 ))
done
reset_lock

# ── (q) a hostname flap between acquire and release must not LEAK the lock ───
# `hostname` on this box flaps between foo.local and foo.lan across a wifi/VPN
# transition — the library's own comment cites it. A flap mid-build made release
# a no-op, and the leaked lockdir then ALSO failed the same-host liveness check
# in acquire, so the next worktree could only age-break a lock nobody held after
# the 1500s stale window. Driven through a PATH shim so the flap is real to the
# library rather than simulated by editing the owner file.
phase q
reset_lock
q_shim="$SB/q.bin"; mkdir -p "$q_shim"
printf '#!/bin/sh\ncat "%s/q.host"\n' "$SB" >"$q_shim/hostname"; chmod +x "$q_shim/hostname"
echo "guard-host-a.local" >"$SB/q.host"
( PATH="$q_shim:$PATH" bash -c '
    cd "$GUARD_REPO"; . "$GUARD_LIB"
    gradle_lock_acquire q
    echo "guard-host-a.lan" >"$1/q.host"   # the flap, mid-hold
    gradle_lock_release' _ "$SB" ) >/dev/null 2>&1
if [ -d "$LOCKDIR" ]; then
  fail "(q) a hostname flap between acquire and release LEAKED the lockdir ${LOCKDIR}: release demanded an exact hostname match and returned a no-op. The orphan then also fails acquire's same-host liveness check, so the next worktree waits out the full stale window for a lock nobody holds"
fi
# The non-owner-release protection case (a) proves must NOT have been weakened.
reset_lock
plant_owner 999999 "guard-host-a.local" "$(date +%s)" "not-this-process"
( PATH="$q_shim:$PATH" bash -c '
    cd "$GUARD_REPO"; . "$GUARD_LIB"; gradle_lock_release' ) >/dev/null 2>&1
[ -d "$LOCKDIR" ] || \
  fail "(q) the hostname-flap allowance became a general release bypass: a process released a lock owned by ANOTHER pid"
reset_lock

# ── (r) a break must act ONLY on the lockdir its decision was made about ─────
# On the malformed-owner decision the expected pid is the EMPTY STRING, and an
# unreadable owner file also reads empty — so a pid-only re-validation passes
# vacuously and the break can mv away a fresh LIVE lock created after the
# decision. That lock's own owner write then fails ENOENT and drops IT to the
# unlocked path, so two builds compile at once, both fail-open, silently.
# Deterministic rather than raced: the window is sub-millisecond in the wild.
phase r
reset_lock
mkdir -p "$LOCKDIR"                       # phantom: holder KILLed before its write
r_out="$SB/r.out"
bash -c '
  cd "$GUARD_REPO"; . "$GUARD_LIB"
  ld="$1"
  ino_decided="$(_gradle_lock_ino "$ld")"
  # the winner of this same break removes the corrupt dir and re-mkdirs a FRESH
  # live lock, which has not written its owner file yet
  rm -rf "$ld"; mkdir "$ld"
  _gradle_lock_break "$ld" "" "malformed owner file" "$ino_decided" && echo BROKE
  [ -d "$ld" ] && echo SURVIVED' _ "$LOCKDIR" >"$r_out" 2>&1
grep -q SURVIVED "$r_out" || \
  fail "(r) a stale-break decision made about a CORRUPT lockdir evicted a DIFFERENT, fresh, LIVE lockdir at the same path. rename(2) is atomic w.r.t. the PATH, not the inode the decision was about, and an empty expected pid compares equal to an unreadable owner file — so the re-validation passed vacuously"
reset_lock

if [ "$rc" -eq 0 ]; then
  echo "gradle-worktree-lock guard: 19 cases green in $(( SECONDS - t_start ))s (exclusion, dead-owner + concurrent break, SIGKILL/SIGTERM/SIGINT, fail-open, status transparency, trace/opt-out/no-reap, pid identity, subshell ownership, trap hygiene, lane-scoped re-raise, bounded break, knob sanitisation, unusable-path fail-open, hostname-flap release, inode-scoped break)"
fi
exit "$rc"
