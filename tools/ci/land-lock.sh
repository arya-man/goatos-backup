# shellcheck shell=bash
# Landing-queue lock for tools/ci/land-main.sh (sourced; also sourced directly by
# tools/ci/land-main.test.sh so the race cases can be exercised without a landing).
#
# Acquire = atomic `mkdir`. The holder file records pid AND that pid's process
# start time, so a reused PID (same number, different process) reads as stale.
#
# Stale reclaim never deletes the lock in place. `rm -rf` + `mkdir` let two runs
# that both saw the same dead holder both "win". Instead a reclaimer atomically
# RENAMES the lock dir to a unique name, and only while holding a short reclaim
# mutex (`mkdir <dir>.reclaim`) after re-reading the holder: so exactly one run
# reclaims, and it can never rename a lock a winner just created. Every run then
# retries `mkdir` once; the losers are refused as busy. (A reclaimer killed
# mid-reclaim leaves <dir>.reclaim behind; remove it by hand.)
#
# Contract: land_lock_acquire <dir> <holder-extra-lines>; returns 0 acquired,
# 1 busy (holder described on stderr). Never waits, never kills.

land_lock_proc_start() {
  # Empty when the pid does not exist.
  LC_ALL=C ps -o lstart= -p "$1" 2>/dev/null | sed 's/^ *//;s/ *$//'
}

land_lock_holder_stale() {
  # $1 = holder file content. Stale when the pid is gone, or when a start time
  # was recorded and the live pid's start time differs (PID reuse).
  local content="$1" pid start now
  pid="$(printf '%s\n' "$content" | sed -n 's/^pid=//p')"
  start="$(printf '%s\n' "$content" | sed -n 's/^pidstart=//p')"
  [ -n "$pid" ] || return 1 # holder not written yet: treat as live
  kill -0 "$pid" 2>/dev/null || return 0
  if [ -n "$start" ]; then
    now="$(land_lock_proc_start "$pid")"
    [ "$now" != "$start" ] && return 0
  fi
  return 1
}

land_lock_try_mkdir() {
  local dir="$1" extra="$2" tmpf
  mkdir "$dir" 2>/dev/null || return 1
  tmpf="$dir/.holder.$$"
  printf 'pid=%s\npidstart=%s\n%s\n' "$$" "$(land_lock_proc_start "$$")" "$extra" >"$tmpf"
  mv "$tmpf" "$dir/holder"
  return 0
}

land_lock_acquire() {
  local dir="$1" extra="${2:-}" content graveyard
  land_lock_try_mkdir "$dir" "$extra" && return 0
  content="$(cat "$dir/holder" 2>/dev/null || true)"
  if [ -n "$content" ] && land_lock_holder_stale "$content" && mkdir "$dir.reclaim" 2>/dev/null; then
    # Sole reclaimer. Only a reclaimer ever renames the lock dir, so the rename
    # below can only move the exact stale lock we re-read under this mutex.
    if [ "$(cat "$dir/holder" 2>/dev/null || true)" = "$content" ]; then
      graveyard="$dir.stale.$$.$RANDOM$RANDOM"
      if mv "$dir" "$graveyard" 2>/dev/null; then
        echo "land-main: reclaiming stale landing lock from pid $(printf '%s\n' "$content" | sed -n 's/^pid=//p')" >&2
        rm -rf "$graveyard"
      fi
    fi
    rmdir "$dir.reclaim" 2>/dev/null || true
    land_lock_try_mkdir "$dir" "$extra" && return 0
    content="$(cat "$dir/holder" 2>/dev/null || true)"
  fi
  echo "land-main: another land-main holds the landing lock ($dir):" >&2
  if [ -n "$content" ]; then printf '%s\n' "$content" | sed 's/^/land-main:   /' >&2
  else echo "land-main:   (holder details not written yet)" >&2; fi
  return 1
}
