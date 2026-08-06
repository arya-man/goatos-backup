# shellcheck shell=bash
# gradle-worktree-lock.sh — machine-wide, FAIL-OPEN advisory mutex around the
# Gradle-executing region of the `android` job.
#
# WHY THIS EXISTS
#   parallel-dispatch.sh's job_group() serialises `android` within ONE
#   run-local-ci.sh process. It is entirely in-memory, so two worktrees are
#   invisible to each other. Measured on this box, from the recorded ci-local
#   timings: THREE worktrees whose `:app compile+unit+lint` steps overlapped for
#   ~3.5 min took 340 s, 251 s and 361 s (epochs 1785967083-1785967590), and a
#   two-worktree overlap took 413 s and 212 s (epochs 1785995260 / 1785995293).
#   Runs on the same box with no other recorded run overlapping them cluster at
#   84-181 s. Queuing is faster than racing, and that is the whole claim — this
#   file makes the second worktree WAIT, it does not make any single pass faster.
#
# THE CENTRAL CONTRACT: FAIL OPEN, ALWAYS.
#   No path in this file returns non-zero. No path fails a step, skips the
#   android job, or waits unboundedly. Nothing here reads or writes `fail`,
#   RESULTS, FAILURES, screenshots_ran, receipt_mode, or the receipt. The lock
#   changes WHEN the android job starts — never which steps run, never how any
#   of them is judged. A scheduling aid that can turn a landing RED is strictly
#   worse than a slow landing.
#
# WHY mkdir AND NOT flock
#   macOS has no GNU flock(1) and this tree has no lock primitive (the only
#   `flock` hits under tools/ are inside package-lock.json). `mkdir` is atomic
#   on POSIX and needs no dependency.
#
# WHY KEYED ON realpath(GRADLE_USER_HOME)
#   The contended resource is the shared Gradle caches plus the cores, not the
#   checkout. Two worktrees with the same GRADLE_USER_HOME queue; a genuinely
#   separate Gradle home does not.
#
# KNOWN LIMIT, STATED RATHER THAN HIDDEN: a maintainer's manual `./gradlew` run
# does not take this lock and still contends.
#
# The filename deliberately does NOT start with `check-`, so
# check-guardrail-registration.mjs's auto-enumeration of check-*.sh does not
# claim this library file as a guard.
#
# Guarded behaviourally by tools/ci/check-gradle-worktree-lock.sh (21 cases) and
# its mutation self-test tools/ci/check-gradle-worktree-lock.test.sh (23
# mutants). Every property below names the case that proves it.

_GRADLE_LOCK_SELF="${_GRADLE_LOCK_SELF:-}"
_GRADLE_LOCK_PRIOR_TRAPS="${_GRADLE_LOCK_PRIOR_TRAPS:-}"
# An orphaned break-lock (what a KILLed breaker leaves between its mkdir and its
# rm) must age out, or it wedges the break path. Kept small: holding it spans
# one stat + one rename.
_GRADLE_LOCK_BREAK_TTL=60

gradle_lock_trace() {
  case "${GOATOS_CI_TRACE_ONLY:-0}" in
    1|true|TRUE|True) return 0 ;;
    *) return 1 ;;
  esac
}

gradle_lock_enabled() {
  case "${GOATOS_CI_GRADLE_LOCK:-1}" in
    0|false|FALSE|False|no|NO) return 1 ;;
    *) return 0 ;;
  esac
}

# _gradle_lock_self — the pid of the SHELL THAT CALLS THIS, which is NOT `$$`
# inside parallel-dispatch's `( … ) &` job subshell (bash keeps `$$` as the
# original shell; macOS bash 3.2 has no BASHPID). Two things depend on getting
# this right, and both shipped wrong once:
#   * the owner file, or kill -0 proves the liveness of run-local-ci.sh instead
#     of the lane that actually holds the lock (case k);
#   * the signal re-raise, or TERMing the android lane kills the WHOLE suite
#     mid-dispatch and orphans Gradle workers (cases m/m2).
# `opid=$(sh -c 'echo $PPID')` does NOT work — command substitution forks, so it
# reports the substitution's pid. `exec` inside a process substitution does.
#
# It SETS `_GRADLE_LOCK_SELF` and prints nothing, and callers must read the
# variable. Printing would invite `self="$(_gradle_lock_self)"`, which is the
# same defect wearing a different hat: the function would then run inside a
# command-substitution subshell, record THAT pid, and lose the assignment on the
# way out. Measured — it recorded a pid that was already dead by the next poll.
_gradle_lock_self() {
  [ -z "${_GRADLE_LOCK_SELF:-}" ] || return 0
  local p=""
  read -r p < <(exec sh -c 'echo $PPID') 2>/dev/null || p=""
  case "$p" in ''|*[!0-9]*) p="$$" ;; esac
  _GRADLE_LOCK_SELF="$p"
}

_gradle_lock_host() { hostname 2>/dev/null || echo unknown-host; }

# _gradle_lock_ino — inode of a path (empty when unknown). This is the ONLY
# thing that identifies the lockdir a stale decision was made ABOUT: `mkdir`
# gives a fresh inode at the same path, so a path comparison cannot tell a
# recreated lock from the corrupt one it replaced (case r).
_gradle_lock_ino() { stat -f %i "$1" 2>/dev/null || stat -c %i "$1" 2>/dev/null || true; }

# _gradle_lock_path_unusable — is the lock path structurally unusable, as
# opposed to merely HELD? `mkdir` reports both as plain failure, and treating
# unusable as held costs the FULL timeout (1800 s by default) of polling before
# the fail-open branch fires: a 30-minute stall inside a wall-clock fix, on a
# TMPDIR whose directory was removed under a launchd/tmux session, a read-only
# or full volume, or a GOATOS_CI_GRADLE_LOCK_DIR pointed at a path that does not
# exist yet (case p).
#
# Deliberately NOT `[ ! -d "$lockdir" ]`: the ordinary EEXIST case is a
# directory whose parent is fine, and a holder releasing between our failed
# mkdir and our probe would otherwise read as "unusable" and drop the mutex.
# Both conditions below are stable properties of the PATH, not of the race.
_gradle_lock_path_unusable() { # lockdir -> 0 = unusable
  local lockdir="$1" parent
  [ -e "$lockdir" ] && [ ! -d "$lockdir" ] && return 0
  parent="$(dirname "$lockdir")"
  [ -d "$parent" ] || return 0
  [ -w "$parent" ] || return 0
  return 1
}

_gradle_lock_mtime() { # path -> epoch seconds (0 when unknown)
  local m
  m="$(stat -f %m "$1" 2>/dev/null || stat -c %Y "$1" 2>/dev/null || echo 0)"
  case "$m" in ''|*[!0-9]*) m=0 ;; esac
  printf '%s' "$m"
}

# _gradle_lock_pid_identity — start time of a pid, used to separate "this pid is
# alive" from "this is the SAME process". macOS $TMPDIR survives a reboot, so a
# recorded pid can be recycled by an unrelated live process; `kill -0` then says
# yes forever and the lock is never reclaimed (case j).
_gradle_lock_pid_identity() {
  ps -o lstart= -p "$1" 2>/dev/null | tr -s ' ' | sed 's/^ *//;s/ *$//'
}

# _gradle_lock_num — sanitise a user-facing numeric knob. Shape AND magnitude:
# an all-digit but oversized value (a copy-pasted millisecond/nanosecond value,
# or one fat-fingered extra digit) still breaks `[ "$a" -ge "$b" ]` with status
# 2 on every poll, so the fail-open branch can never fire and acquire hangs
# forever — the exact defect the shape check was added for (cases o1/o2).
_gradle_lock_num() { # value default name
  local v="$1" d="$2" name="$3"
  case "$v" in
    ''|*[!0-9]*)
      [ -z "$v" ] || echo "ci-local: ignoring non-numeric ${name}='${v}' — using ${d}" >&2
      printf '%s' "$d"; return 0 ;;
  esac
  if [ "${#v}" -gt 9 ]; then
    echo "ci-local: ignoring out-of-range ${name}='${v}' — using ${d}" >&2
    printf '%s' "$d"; return 0
  fi
  printf '%s' "$v"
}

gradle_lock_dir() {
  local override="${GOATOS_CI_GRADLE_LOCK_DIR:-${TMPDIR:-/tmp}}"
  local home="${GRADLE_USER_HOME:-$HOME/.gradle}"
  local real key
  real="$(cd "$home" 2>/dev/null && pwd -P)" || real=""
  [ -n "$real" ] || real="$home"
  key="$(printf '%s' "$real" | { md5 -q 2>/dev/null || md5sum 2>/dev/null | cut -d' ' -f1; })"
  [ -n "$key" ] || key="nokey"
  printf '%s/goatos-gradle-lock.%s' "${override%/}" "$key"
}

# _gradle_lock_break — SERIALISED, RE-VALIDATED, ATOMIC removal of a lock the
# caller has decided is stale.
#
# Return status is load-bearing:
#   0 = "I held the break-lock, re-validated, and REMOVED it" — the caller may
#       `continue` straight to a fresh mkdir attempt;
#   1 = "nothing was decided or nothing was removed" — the caller MUST fall
#       through to the timeout check AND the sleep throttle.
#
# Both halves of that contract are defects that shipped:
#   * `continue`ing on 1 jumped over the timeout check and the throttle, so an
#     orphaned $lockdir.break turned acquire into an unbounded 80%-CPU spin that
#     ignored GOATOS_CI_GRADLE_LOCK_TIMEOUT entirely (case n);
#   * returning 0 when the `mv` itself failed (an undeletable lockdir: another
#     user's dir under a sticky shared /tmp, a root-owned leftover, `chflags
#     uchg`) is the same unbounded spin one clause over. Progress, not
#     authority, is what licenses a `continue`.
#
# A plain `mv` is NOT sufficient on its own: rename(2) is atomic with respect to
# the PATH, not the inode the decision was made about, so a losing racer's `mv`
# succeeds — it just moves the WINNER'S FRESH lock. Measured 3 processes inside
# at once with `mv` alone. Hence the break-lock plus the re-read (case h).
_gradle_lock_break() { # lockdir expected_owner_pid reason expected_inode [expected_mtime]
  local lockdir="$1" expect="$2" reason="$3" expect_ino="${4:-}" expect_mtime="${5:-}"
  local brk="${lockdir}.break" graveyard cur_pid="" cur_ino="" cur_mtime="" now bage

  if [ -d "$brk" ]; then
    now="$(date +%s)"
    bage=$(( now - $(_gradle_lock_mtime "$brk") ))
    [ "$bage" -ge "$_GRADLE_LOCK_BREAK_TTL" ] && rm -rf "$brk" 2>/dev/null
  fi
  mkdir "$brk" 2>/dev/null || return 1

  if [ -r "$lockdir/owner" ]; then
    { IFS="$(printf '\t')" read -r cur_pid _; } <"$lockdir/owner" 2>/dev/null || cur_pid=""
  fi
  cur_ino="$(_gradle_lock_ino "$lockdir")"
  cur_mtime="$(_gradle_lock_mtime "$lockdir")"
  # The INODE and MTIME are the load-bearing re-validations. On the
  # malformed-owner decision `expect` is the EMPTY STRING, and an unreadable
  # owner file also yields cur_pid="", so the pid comparison degenerates to
  # `"" != ""` — vacuously true — and the break would `mv` away whatever sits
  # at the path, including a DIFFERENT, FRESH, LIVE lock created by the winner
  # of this same break after our decision (whose own owner write then fails
  # ENOENT and drops IT to the unlocked path, so two builds compile at once —
  # invisibly, because both paths are fail-open). A recreated lockdir has a
  # different inode AND a different mtime, so this returns 1 and the caller
  # re-polls (case r).
  if [ ! -d "$lockdir" ] || [ -z "$cur_ino" ] || [ "${cur_ino}" != "${expect_ino}" ] \
     || { [ -n "$expect_mtime" ] && [ "$cur_mtime" != "$expect_mtime" ]; } \
     || [ "${cur_pid}" != "${expect}" ]; then
    rm -rf "$brk" 2>/dev/null
    return 1
  fi

  graveyard="${lockdir}.stale.$$.${RANDOM}"
  if mv "$lockdir" "$graveyard" 2>/dev/null; then
    rm -rf "$graveyard" 2>/dev/null
    rm -rf "$brk" 2>/dev/null
    echo "ci-local: breaking a STALE Gradle lock (${reason})" >&2
    return 0
  fi
  rm -rf "$brk" 2>/dev/null
  return 1
}

# gradle_lock_acquire LABEL — returns 0 on EVERY path.
gradle_lock_acquire() {
  local label="${1:-android gradle}"

  # (a) TRACE SHORT-CIRCUIT — FIRST, before anything else, and load-bearing.
  # `step` short-circuits under GOATOS_CI_TRACE_ONLY but code OUTSIDE `step`
  # does not, and tools/ci/check-android-screenshot-proof.sh drives
  # `run-local-ci.sh android` under trace mode. Without this the required
  # screenshot-proof guard would block behind a real concurrent Gradle build —
  # a self-inflicted CI hang (case g).
  gradle_lock_trace && return 0
  gradle_lock_enabled || return 0

  local lockdir timeout stale host self t0 waited=0 last_note=0 missing=0
  local ino="" prev_ino=""
  lockdir="$(gradle_lock_dir)"
  # (p) Structurally unusable path: fail open on the FIRST look. Polling it for
  # the whole timeout is indistinguishable from a held lock and costs 30 minutes
  # at the shipped default.
  if _gradle_lock_path_unusable "$lockdir"; then
    echo "ci-local: Gradle lock path ${lockdir} is unusable — PROCEEDING WITHOUT THE LOCK (this run is slower, not weaker)" >&2
    return 0
  fi
  timeout="$(_gradle_lock_num "${GOATOS_CI_GRADLE_LOCK_TIMEOUT:-1800}" 1800 GOATOS_CI_GRADLE_LOCK_TIMEOUT)"
  # Default 1500 < the 1800 s timeout ON PURPOSE: with STALE above TIMEOUT the
  # expiry branch is unreachable at default settings and an unverifiable lock
  # can only ever be escaped by the fail-open path (case k).
  stale="$(_gradle_lock_num "${GOATOS_CI_GRADLE_LOCK_STALE_SECONDS:-1500}" 1500 GOATOS_CI_GRADLE_LOCK_STALE_SECONDS)"
  host="$(_gradle_lock_host)"
  _gradle_lock_self; self="${_GRADLE_LOCK_SELF}"
  t0="$(date +%s)"

  while :; do
    if mkdir "$lockdir" 2>/dev/null; then
      # Owner file BEFORE success is claimed. A best-effort write leaves a
      # PHANTOM lock — a live holder with no owner file, indistinguishable from
      # a corrupt one, which the malformed-owner branch below would then evict
      # while its Gradle build is running. A failed write abandons the lockdir
      # and proceeds unlocked: fail-open, no phantom.
      if printf '%s\t%s\t%s\t%s\t%s\t%s\n' \
          "$self" "$host" "$(pwd -P)" "$label" "$(date +%s)" "$(_gradle_lock_pid_identity "$self")" \
          >"$lockdir/owner" 2>/dev/null; then
        return 0
      fi
      echo "ci-local: could not record Gradle lock ownership — PROCEEDING WITHOUT THE LOCK" >&2
      rm -rf "$lockdir" 2>/dev/null
      return 0
    fi

    # mkdir can fail for reasons other than EEXIST. Re-check before treating the
    # failure as contention. If the path is unusable (case p), fail open
    # IMMEDIATELY rather than polling for the full timeout: EEXIST means held,
    # but ENOENT/EACCES/parent-is-a-file means the path can NEVER become
    # acquirable, so a 30-minute silent stall is worse than a missing lock.
    if ! mkdir "$lockdir" 2>/dev/null && _gradle_lock_path_unusable "$lockdir"; then
      echo "ci-local: Gradle lock path ${lockdir} is unusable — PROCEEDING WITHOUT THE LOCK (this run is slower, not weaker)" >&2
      return 0
    fi

    local opid="" ohost="" owt="" olabel="" oepoch="" oident="" age=0 now
    if [ -r "$lockdir/owner" ]; then
      { IFS="$(printf '\t')" read -r opid ohost owt olabel oepoch oident; } <"$lockdir/owner" 2>/dev/null || opid=""
    fi
    # Captured on the SAME poll as the owner read, so it identifies the lockdir
    # this poll's decision is about (case r). decision_mtime is the FILESYSTEM
    # mtime of the lockdir at decision time — NOT oepoch. oepoch is the owner's
    # self-reported claim (business data written into the owner file, used only
    # for age/staleness math) and can legitimately disagree with the directory's
    # real mtime — a corrupt/hand-planted owner file, a clock-skewed writer, or
    # (in the self-test) a deliberately fabricated stale timestamp. Re-validating
    # a break against oepoch therefore rejects vacuously whenever that claim
    # doesn't match reality, even with no race at all (cases j, o2). decision_mtime
    # is instead the SAME re-validation signal as the inode: a value that can only
    # change if the lockdir at this path was actually replaced between decision and
    # break.
    prev_ino="$ino"; ino="$(_gradle_lock_ino "$lockdir")"
    local decision_mtime; decision_mtime="$(_gradle_lock_mtime "$lockdir")"
    now="$(date +%s)"
    case "$oepoch" in ''|*[!0-9]*) oepoch="$(_gradle_lock_mtime "$lockdir")" ;; esac
    age=$(( now - oepoch ))
    # oepoch 0 means "no owner file and no stat" — a lockdir that vanished under
    # us. `now - 0` is 1.7 billion seconds and the wait message said so.
    [ "$age" -ge 0 ] 2>/dev/null || age=0
    [ "$oepoch" -gt 0 ] 2>/dev/null || age=0

    local decision=""
    case "$opid" in
      ''|*[!0-9]*)
        # Malformed/absent owner. Never break on the first sight of it: a live
        # holder is only ever ownerless for the microseconds between its mkdir
        # and its write, and evicting there restores the very contention this
        # file removes. Three consecutive polls (~3 s) is genuinely corrupt.
        # A DIFFERENT lockdir restarts the count: three polls of "corrupt" only
        # mean anything when they were three polls of the SAME directory.
        if [ -n "$prev_ino" ] && [ "$ino" != "$prev_ino" ]; then missing=0; fi
        missing=$(( missing + 1 ))
        [ "$missing" -ge 3 ] && decision="malformed owner file"
        ;;
      *)
        missing=0
        if [ "$ohost" = "$host" ] && ! kill -0 "$opid" 2>/dev/null; then
          # THE interrupt path, not a nicety: parallel-dispatch's
          # _dispatch_cleanup TERM-then-KILLs the descendant tree and a KILLed
          # holder cannot run its own release (cases b, c).
          decision="owner pid ${opid} is gone"
        elif [ "$ohost" = "$host" ] && [ -n "$oident" ] && \
             [ "$(_gradle_lock_pid_identity "$opid")" != "$oident" ]; then
          # Alive, same host, DIFFERENT process: the pid was recycled. kill -0
          # is proof of liveness, never of identity (case j).
          [ "$age" -ge "$stale" ] && decision="owner pid ${opid} was recycled and the lock is ${age}s old"
        elif [ "$ohost" != "$host" ]; then
          # Unverifiable owner — a foreign host, or this box's own `hostname`
          # flapping between foo.local and foo.lan. Age expiry only; never break
          # a lock whose liveness cannot be checked just because it is old
          # enough to be inconvenient.
          [ "$age" -ge "$stale" ] && decision="owner is unverifiable (host ${ohost}) and the lock is ${age}s old"
        fi
        ;;
    esac

    if [ -n "$decision" ]; then
      if _gradle_lock_break "$lockdir" "$opid" "$decision" "$ino" "$decision_mtime"; then
        continue
      fi
      # Nothing decided or nothing removed: fall through to the throttle and the
      # timeout check. NEVER `continue` here.
    fi

    waited=$(( now - t0 ))
    if [ "$waited" -ge "$timeout" ]; then
      echo "ci-local: Gradle lock wait exceeded ${waited}s — PROCEEDING WITHOUT THE LOCK (this run is slower, not weaker)" >&2
      return 0
    fi
    if [ $(( now - last_note )) -ge 5 ]; then
      last_note="$now"
      echo "ci-local: waiting for the machine-wide Gradle lock — held ${age}s by pid ${opid:-?} in ${owt:-?} running \"${olabel:-?}\" (two worktrees compiling :app concurrently measured ~3x slower; queuing is faster than racing). Set GOATOS_CI_GRADLE_LOCK=0 to opt out." >&2
    fi
    sleep 1
  done
}

# gradle_lock_release — removes the lockdir ONLY when the owner file names this
# exact shell AND this host. A blind `rm -rf` lets one worktree delete another's
# LIVE lock, so both compile at once — the exact failure the lock exists to
# prevent, now invisible because the lock is fail-open (case a). Idempotent.
gradle_lock_release() {
  gradle_lock_trace && return 0
  gradle_lock_enabled || return 0
  local lockdir opid="" ohost="" oident="" self
  lockdir="$(gradle_lock_dir)"
  [ -d "$lockdir" ] || return 0
  [ -r "$lockdir/owner" ] || return 0
  _gradle_lock_self; self="${_GRADLE_LOCK_SELF}"
  { IFS="$(printf '\t')" read -r opid ohost _ _ _ oident; } <"$lockdir/owner" 2>/dev/null || return 0
  [ "$opid" = "$self" ] || return 0
  # Self-ownership needs the pid PLUS one thing that says "same machine". The
  # hostname is the cheap answer but it is not stable: this box's own `hostname`
  # flaps between foo.local and foo.lan across a wifi/VPN transition, and a flap
  # between acquire and release LEAKED the lockdir. The leaked lock then also
  # failed the same-host liveness check in acquire, so the next worktree could
  # only age-break it — a 25-minute wait for a lock nobody held.
  #
  # The start-time identity is the stronger proof and needs no hostname: same
  # pid AND same process start time means this process provably wrote the file.
  # Non-owner-release protection is unchanged — a different process fails the
  # pid check first, and a recycled pid fails the identity (case q). Accept
  # EITHER hostname match OR identity match, so a hostname flap mid-hold does
  # not leak (case q2).
  if [ "$ohost" != "$(_gradle_lock_host)" ]; then
    [ -n "$oident" ] || return 0
    [ "$oident" = "$(_gradle_lock_pid_identity "$self")" ] || return 0
  fi
  rm -rf "$lockdir" 2>/dev/null
  return 0
}

# _gradle_lock_on_signal — mirrors parallel-dispatch.sh's _dispatch_on_signal:
# clear traps, release, re-raise with the DEFAULT disposition, exit 128+n. A
# handler that releases and RETURNS makes SIGINT non-fatal, so Ctrl-C during
# `:app compile+unit+lint` drops the mutex and then launches the benchmark
# compile the maintainer believes they cancelled (cases d, i).
#
# The re-raise targets the ACQUIRING shell, not `$$`. Inside dispatch_jobs'
# `( … ) &` the android lane's `$$` is run-local-ci.sh itself, so `kill -TERM
# $$` killed the entire suite before its 25x0.2 s TERM-then-KILL escalation and
# orphaned GradleWorkerMain JVMs holding build locks — the precise condition
# this whole change exists to avoid (cases m, m2).
_gradle_lock_on_signal() { # INT|TERM
  local sig="$1" num
  case "$sig" in INT) num=2 ;; TERM) num=15 ;; *) num=15 ;; esac
  trap - EXIT INT TERM
  gradle_lock_release
  kill "-$sig" "${_GRADLE_LOCK_SELF:-$$}" 2>/dev/null || true
  exit $(( 128 + num ))
}

gradle_lock_install_trap() {
  # Record the acquiring shell HERE too. When the lock is disabled
  # (GOATOS_CI_GRADLE_LOCK=0) acquire short-circuits before recording it, so
  # without this line the handler falls back to `$$` on exactly the opt-out path
  # — the suite-killing re-raise, unguarded (case m2).
  _gradle_lock_self
  _GRADLE_LOCK_PRIOR_TRAPS="$(trap -p EXIT INT TERM 2>/dev/null)"
  trap 'gradle_lock_release' EXIT
  trap '_gradle_lock_on_signal INT'  INT
  trap '_gradle_lock_on_signal TERM' TERM
}

# gradle_lock_clear_trap — RESTORES the prior dispositions rather than blanking
# them. An unconditional `trap - EXIT INT TERM` silently deletes any EXIT trap
# the caller installed before us (case l).
gradle_lock_clear_trap() {
  trap - EXIT INT TERM
  if [ -n "${_GRADLE_LOCK_PRIOR_TRAPS:-}" ]; then
    eval "$_GRADLE_LOCK_PRIOR_TRAPS" 2>/dev/null || true
  fi
  _GRADLE_LOCK_PRIOR_TRAPS=""
}

# gradle_lock_run LABEL CMD... — single-command wrapper. The wrapped command's
# exit status is returned VERBATIM in every lock state; the lock must never be
# able to change a verdict in either direction (case f). run_android brackets
# several `step`s so it uses acquire/install_trap/release directly; this wrapper
# is what the guard drives.
gradle_lock_run() {
  local label="$1"; shift
  local rc=0
  gradle_lock_acquire "$label"
  gradle_lock_install_trap
  "$@" || rc=$?
  gradle_lock_clear_trap
  gradle_lock_release
  return "$rc"
}
