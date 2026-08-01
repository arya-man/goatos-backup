#!/usr/bin/env bash
# Reap orphaned Gradle test workers.
#
# Killing a Gradle build does NOT kill the test JVMs it forked. They are children of the
# daemon, not of the shell, so a Ctrl-C or `kill` on the build leaves `GradleWorkerMain`
# processes running forever, each holding ~512MB and a slice of CPU. This has happened
# three times on this machine and the survivors were only noticed after five hours.
#
# The blast radius is not just wasted RAM: a live worker keeps a lock on the module's
# build directory, so the NEXT run blocks on a lock held by a process nobody is watching,
# which reads as "the test suite is slow" rather than "there is a corpse in the way".
#
# Run this BEFORE starting a Gradle test run and AFTER killing one. Safe to run anytime:
# it only touches workers older than the threshold, so a healthy in-flight run survives.
#
# Usage:
#   tools/agent-hooks/reap-stale-gradle-workers.sh            # report + kill > 30m old
#   tools/agent-hooks/reap-stale-gradle-workers.sh --check    # report only, non-zero if any
#   MAX_AGE_MINUTES=5 tools/agent-hooks/reap-stale-gradle-workers.sh
set -euo pipefail

max_age="${MAX_AGE_MINUTES:-30}"
check_only=0
[ "${1:-}" = "--check" ] && check_only=1

# etime is [[dd-]hh:]mm:ss — normalize to minutes.
age_minutes() {
  local e="$1" d=0 h=0 m=0
  case "$e" in
    *-*) d="${e%%-*}"; e="${e#*-}" ;;
  esac
  case "$e" in
    *:*:*) h="${e%%:*}"; e="${e#*:}"; m="${e%%:*}" ;;
    *:*)   m="${e%%:*}" ;;
  esac
  echo $(( 10#$d * 1440 + 10#$h * 60 + 10#$m ))
}

found=0
killed=0
while read -r pid etime _; do
  [ -n "${pid:-}" ] || continue
  age="$(age_minutes "$etime")"
  [ "$age" -ge "$max_age" ] || continue
  found=$((found + 1))
  echo "stale Gradle test worker: pid $pid, alive ${etime} (>= ${max_age}m)"
  if [ "$check_only" -eq 0 ]; then
    kill -9 "$pid" 2>/dev/null && killed=$((killed + 1))
  fi
done < <(ps -Ao pid,etime,command | grep '[G]radleWorkerMain' || true)

if [ "$found" -eq 0 ]; then
  echo "no stale Gradle test workers"
  exit 0
fi

if [ "$check_only" -eq 1 ]; then
  echo "$found stale worker(s); run tools/agent-hooks/reap-stale-gradle-workers.sh to reap" >&2
  exit 1
fi

echo "reaped $killed stale Gradle test worker(s)"
