#!/usr/bin/env bash
# Self-test for check-gradle-worktree-lock.sh.
#
# The only thing that makes that guard worth having is that it goes RED when the
# lock stops behaving. The lock is FAIL-OPEN, so a broken lock fails nothing on
# its own — it silently restores the measured ~3x cross-worktree Gradle penalty.
# So this harness builds TWENTY-THREE mutated copies of tools/ci/gradle-worktree-lock.sh
# (ids i..xxiii), points the guard at each via GOATOS_GRADLE_LOCK_UNDER_TEST, and
# requires a non-zero exit for every one — plus a zero exit for the pristine
# library, so the harness cannot pass by rejecting everything.
#
# Every sed/perl anchor below is CHECKED: a mutation that no longer applies (a
# renamed variable, a reflowed line) fails this harness loudly instead of
# quietly becoming a mutant that is identical to the original and therefore
# "correctly" green.
#
# Four mutants that a reviewer would reach for first are NOT here, because they
# are not observable and a guard case built on them proves nothing:
#   * sabotaging the EXIT trap alone, or the TERM trap alone — bash runs the
#     EXIT trap when a caught signal terminates the shell, and a holder with no
#     TERM handler dies on the default disposition with the same 143. The
#     observable mutants are "the handler releases and RETURNS" (viii/ix) and
#     "install_trap installs nothing" (v).
#   * a bare `mv` break with no break-lock — it does not reliably admit two
#     racers at 4 racers x 1 round; mutant (x) removes the RE-VALIDATION, which
#     case (h) separates at 8 racers x 3 rounds.
#
# TRAP, WRITTEN DOWN BECAUSE IT COST A ROUND: perl INTERPOLATES `$` in the
# REPLACEMENT half too. An unescaped `$oident` / `$$` / `${VAR:-x}` there becomes
# the empty string or perl's own pid, so the mutant silently mutates something
# other than what its label claims — and mutant (xi) was consequently caught for
# the wrong reason, then not caught at all. Every literal `$` in a replacement is
# `\$`; the only bare `$1` is the capture in (iii). (xvii)/(xviii) use `!`
# delimiters because an escaped `\}` inside `s{}{}` breaks perl's brace counting.
#
# ONE GUARD CASE IS DELIBERATELY NOT MUTATION-COVERED HERE, and saying so is the
# point: case (g3) asserts that a TRACE run of run-local-ci.sh does not run the
# machine-wide stale-Gradle-worker reaper. That behaviour lives in
# run-local-ci.sh, not in the library this harness mutates, so no mutant of the
# library can turn it red. It was proven the other way — by removing the
# `ci_trace_only ||` gate in place, observing (g3) RED, and restoring — and that
# is the check to repeat if that line is ever touched.
#
# NEVER invokes Gradle. Sleeps and shell only.
set -uo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo"
LIB=tools/ci/gradle-worktree-lock.sh
fails=0

work="$(mktemp -d "${TMPDIR:-/tmp}/goatos-gradle-lock-selftest.XXXXXX")"
cleanup() {
  # run_guard escalates to `kill -KILL`, which the guard cannot trap, so its own
  # `trap cleanup EXIT` never runs and its mktemp sandbox would survive. Mutants
  # that hang are DESIGNED to hit the budget, so that leak happened on every
  # clean run. Fixed by giving each guard run a TMPDIR *inside* this harness
  # directory: removing `$work` removes the sandbox too. Deliberately NOT a glob
  # over the real $TMPDIR — that would race a concurrent guard run in another
  # worktree, which is the same class of mistake being fixed.
  rm -rf "$work"
}
trap cleanup EXIT

# run_guard MUTANT_FILE BUDGET -> 0 green, 1 red. A hang counts as RED, never as
# a pass, and never costs the lock's own 300s/1800s timeouts.
run_guard() {
  local mutant="$1" budget="${2:-150}" i=0 pid grc
  local sandbox="$work/tmp.$$.$RANDOM"
  mkdir -p "$sandbox"
  ( TMPDIR="$sandbox" GOATOS_GRADLE_LOCK_UNDER_TEST="$mutant" \
      bash tools/ci/check-gradle-worktree-lock.sh >"$mutant.out" 2>&1 ) &
  pid=$!
  while [ "$i" -lt "$budget" ]; do
    kill -0 "$pid" 2>/dev/null || break
    sleep 1; i=$(( i + 1 ))
  done
  if kill -0 "$pid" 2>/dev/null; then
    kill -KILL "$pid" 2>/dev/null
    wait "$pid" 2>/dev/null
    rm -rf "$sandbox"
    echo "      (guard did not finish in ${budget}s — treated as red)"
    return 1
  fi
  wait "$pid" 2>/dev/null; grc=$?
  rm -rf "$sandbox"
  [ "$grc" -eq 0 ] && return 0
  return 1
}

# mutate ID DESCRIPTION PERL_EXPR
# Builds $work/<id>.sh, verifies the mutation actually changed the file, runs the
# guard against it, and requires RED.
mutate() {
  local id="$1" desc="$2" expr="$3"
  local out="$work/${id}.sh"
  cp "$LIB" "$out"
  perl -0pi -e "$expr" "$out" || { echo "FAIL  (${id}) mutation script errored" >&2; fails=$((fails+1)); return; }
  if cmp -s "$LIB" "$out"; then
    echo "FAIL  (${id}) mutation was a NO-OP — the anchor was renamed or reflowed. Update this harness; a no-op mutant is a guard that proves nothing." >&2
    fails=$((fails+1)); return
  fi
  if run_guard "$out"; then
    echo "FAIL  (${id}) guard PASSED ${desc}" >&2
    fails=$((fails+1))
  else
    echo "ok    (${id}) ${desc} REJECTED"
  fi
}

# (0) the pristine library must be GREEN, or every rejection below is vacuous.
cp "$LIB" "$work/pristine.sh"
if run_guard "$work/pristine.sh" 180; then
  echo "ok    (0) the real lock library passes the guard"
else
  echo "FAIL  (0) the REAL library failed its own guard" >&2
  sed -n '1,40p' "$work/pristine.sh.out" >&2
  fails=$((fails+1))
fi

mutate i    "an acquire that never locks" \
  's{if mkdir "\$lockdir" 2>/dev/null; then}{if true; then}'

mutate ii   "no dead-owner reclaim" \
  's{decision="owner pid \$\{opid\} is gone"}{decision=""}'

mutate iii  "a timeout that FAILS the run" \
  's{(PROCEEDING WITHOUT THE LOCK \(this run is slower, not weaker\)" >&2\n\s*)return 0}{$1return 1}'

mutate iv   "an unconditional release" \
  's{\[ "\$opid" = "\$self" \] \|\| return 0}{:}'

mutate v    "an install_trap that installs nothing" \
  's{  trap \x27gradle_lock_release\x27 EXIT\n  trap \x27_gradle_lock_on_signal INT\x27  INT\n  trap \x27_gradle_lock_on_signal TERM\x27 TERM}{  :}'

mutate vi   "a lock that engages under GOATOS_CI_TRACE_ONLY" \
  's{  gradle_lock_trace && return 0\n  gradle_lock_enabled \|\| return 0\n\n  local lockdir}{  gradle_lock_enabled || return 0\n\n  local lockdir}'

mutate vii  "a wrapper that swallows the command exit status" \
  's{  gradle_lock_release\n  return "\$rc"\n\}}{  gradle_lock_release\n  return 0\n\}}'

mutate viii "a TERM handler that releases and RETURNS instead of dying" \
  's{trap \x27_gradle_lock_on_signal TERM\x27 TERM}{trap \x27gradle_lock_release\x27 TERM}'

mutate ix   "an INT handler that releases and RETURNS instead of dying" \
  's{trap \x27_gradle_lock_on_signal INT\x27  INT}{trap \x27gradle_lock_release\x27 INT}'

mutate x    "a stale break that does not RE-VALIDATE the owner" \
  's{if \[ ! -d "\$lockdir" \] \|\| \[ -z "\$cur_ino" \] \|\| \[ "\$\{cur_ino\}" != "\$\{expect_ino\}" \] \\\n     \|\| \{ \[ -n "\$expect_mtime" \] && \[ "\$cur_mtime" != "\$expect_mtime" \]; \} \\\n     \|\| \[ "\$\{cur_pid\}" != "\$\{expect\}" \]; then}{if false; then}'

mutate xi   "kill -0 treated as proof of identity" \
  's{\[ -n "\$oident" \]}{[ -z "\$oident" ]}'

mutate xii  "an owner pid that is the parent, not the acquiring subshell" \
  's{  host="\$\(_gradle_lock_host\)"\n  _gradle_lock_self; self="\$\{_GRADLE_LOCK_SELF\}"}{  host="\$(_gradle_lock_host)"\n  self="\$\$"}'

mutate xiii "a clear_trap that discards a pre-existing EXIT trap" \
  's{eval "\$_GRADLE_LOCK_PRIOR_TRAPS" 2>/dev/null \|\| true}{:}'

mutate xiv  "a break that reports success when it could not take the break-lock" \
  's{mkdir "\$brk" 2>/dev/null \|\| return 1}{mkdir "\$brk" 2>/dev/null || return 0}'

mutate xv   "a signal re-raise aimed at \$\$ instead of the acquiring shell" \
  's{kill "-\$sig" "\$\{_GRADLE_LOCK_SELF:-\$\$\}"}{kill "-\$sig" \$\$}'

mutate xvi  "an install_trap that does not record the acquiring shell" \
  's{  _gradle_lock_self\n  _GRADLE_LOCK_PRIOR_TRAPS=}{  _GRADLE_LOCK_PRIOR_TRAPS=}'

mutate xvii "an unsanitised GOATOS_CI_GRADLE_LOCK_TIMEOUT" \
  's!timeout="\$\(_gradle_lock_num "\$\{GOATOS_CI_GRADLE_LOCK_TIMEOUT:-1800\}" 1800 GOATOS_CI_GRADLE_LOCK_TIMEOUT\)"!timeout="\${GOATOS_CI_GRADLE_LOCK_TIMEOUT:-1800}"!'

mutate xviii "an unsanitised GOATOS_CI_GRADLE_LOCK_STALE_SECONDS" \
  's!stale="\$\(_gradle_lock_num "\$\{GOATOS_CI_GRADLE_LOCK_STALE_SECONDS:-1500\}" 1500 GOATOS_CI_GRADLE_LOCK_STALE_SECONDS\)"!stale="\${GOATOS_CI_GRADLE_LOCK_STALE_SECONDS:-1500}"!'

mutate xix  "a knob sanitiser that checks shape but not MAGNITUDE" \
  's{if \[ "\$\{#v\}" -gt 9 \]; then}{if false; then}'

# Both call sites, or the surviving one still catches it and the mutant is a
# no-op dressed as a mutation.
mutate xx   "an acquire that polls an UNUSABLE lock path as if it were HELD" \
  's{if _gradle_lock_path_unusable "\$lockdir"; then}{if false; then}g'

mutate xxi  "a release that demands an exact hostname and leaks on a flap" \
  's{  if \[ "\$ohost" != "\$\(_gradle_lock_host\)" \]; then\n    \[ -n "\$oident" \] \|\| return 0\n    \[ "\$oident" = "\$\(_gradle_lock_pid_identity "\$self"\)" \] \|\| return 0\n  fi}{  [ "\$ohost" = "\$(_gradle_lock_host)" ] || return 0}'

mutate xxii "a stale break that re-validates on a pid that can be EMPTY, not the inode" \
  's{ \|\| \[ -z "\$cur_ino" \] \|\| \[ "\$\{cur_ino\}" != "\$\{expect_ino\}" \]}{}'

mutate xxiii "a hostname-flap allowance that degenerates into release-on-pid-alone" \
  's{    \[ "\$oident" = "\$\(_gradle_lock_pid_identity "\$self"\)" \] \|\| return 0\n}{}'

[ "$fails" -eq 0 ] || { echo "check-gradle-worktree-lock.test.sh: ${fails} case(s) FAILED" >&2; exit 1; }
echo "check-gradle-worktree-lock.test.sh: all cases ok"
