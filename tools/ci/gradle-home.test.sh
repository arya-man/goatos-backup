#!/usr/bin/env bash
# Tests for tools/ci/gradle-home.sh and tools/ci/gradle-run.sh (fake gradlew,
# sandboxed lock dir; never touches a real Gradle build or lock).
set -euo pipefail
cd "$(dirname "$0")/../.."
T="$(mktemp -d)"; trap 'rm -rf "$T"' EXIT
fails=0
mkdir -p "$T/home"
ok() { echo "ok   $1"; }
bad() { echo "FAIL $1"; fails=$((fails+1)); }

# ── gradle home guard ──
home_after() { env -u GOATOS_ALLOW_PRIVATE_GRADLE_HOME HOME="$T/home" GRADLE_USER_HOME="$1" GOATOS_ALLOW_PRIVATE_GRADLE_HOME="${2:-0}" bash -c '. tools/ci/gradle-home.sh; gradle_home_normalize 2>/dev/null; echo "${GRADLE_USER_HOME:-}"'; }
[ "$(home_after /private/tmp/claude-501/x/scratchpad/gradle-home)" = "$T/home/.gradle" ] && ok "/private/tmp home reset to ~/.gradle" || bad "/private/tmp home not reset"
[ "$(home_after /var/folders/f5/abc/T/gh)" = "$T/home/.gradle" ] && ok "/var/folders home reset" || bad "/var/folders home not reset"
[ "$(home_after /tmp/gh)" = "$T/home/.gradle" ] && ok "/tmp home reset" || bad "/tmp home not reset"
[ "$(home_after /private/tmp/gh 1)" = "/private/tmp/gh" ] && ok "GOATOS_ALLOW_PRIVATE_GRADLE_HOME=1 keeps it" || bad "opt-out ignored"
[ "$(home_after /Volumes/fast/gradle)" = "/Volumes/fast/gradle" ] && ok "non-temp explicit home is kept" || bad "non-temp home changed"
[ "$(home_after "")" = "" ] && ok "unset home stays unset (Gradle default ~/.gradle)" || bad "unset home was set"
msg="$(env -u GOATOS_ALLOW_PRIVATE_GRADLE_HOME HOME="$T/home" GRADLE_USER_HOME=/private/tmp/gh bash -c '. tools/ci/gradle-home.sh; gradle_home_normalize' 2>&1)"
case "$msg" in *WARNING*temp*) ok "reset prints a warning";; *) bad "no warning: $msg";; esac
mkdir -p "$T/rohome/.gradle"; chmod 555 "$T/rohome/.gradle"
ro="$(env -u GOATOS_ALLOW_PRIVATE_GRADLE_HOME HOME="$T/rohome" GRADLE_USER_HOME=/private/tmp/gh GOATOS_GRADLE_MACHINE_SETUP=0 bash -c '. tools/ci/gradle-home.sh; gradle_home_normalize 2>/dev/null; echo "$GRADLE_USER_HOME"')"
chmod 755 "$T/rohome/.gradle"
[ "$ro" = /private/tmp/gh ] && ok "unwritable ~/.gradle (sandbox): private home kept" || bad "reset to an unwritable ~/.gradle: $ro"
grep -q 'gradle_home_normalize' tools/ci/run-local-ci.sh && ok "run-local-ci normalizes the Gradle home" || bad "run-local-ci does not normalize"

# ── machine-wide lock around developer Gradle calls ──
mkdir -p "$T/android" "$T/locks"
cat >"$T/android/gradlew" <<'G'
#!/bin/sh
echo "start $1 $(date +%s)" >>"$LOG"
sleep 2
echo "end $1 $(date +%s)" >>"$LOG"
G
chmod +x "$T/android/gradlew"
export LOG="$T/log" GOATOS_CI_GRADLE_LOCK_DIR="$T/locks" GRADLE_USER_HOME="$T/gh" GOATOS_ALLOW_PRIVATE_GRADLE_HOME=1
mkdir -p "$GRADLE_USER_HOME"
run() { bash -c '. tools/ci/gradle-run.sh; goatos_gradlew "test $1" "$2" "$1"' _ "$1" "$T/android"; }
run A 2>"$T/a.err" & pa=$!
sleep 0.5
run B 2>"$T/b.err" & pb=$!
wait "$pa"; wait "$pb"
# The second build must start only after the first ended.
awk '{t[$1" "$2]=$3} END{ if (t["start B"] >= t["end A"] || t["start A"] >= t["end B"]) exit 0; exit 1 }' "$LOG" && ok "two sessions' Gradle builds ran one at a time" || { bad "builds overlapped"; cat "$LOG"; }
grep -q 'held .* by pid' "$T/b.err" "$T/a.err" && ok "the waiter printed the holder" || bad "waiter did not print the holder"
kill -0 "$$" && ok "nothing was killed (waiter only waited)"
ls "$T/locks" | grep -q goatos-gradle-lock && bad "lock left behind after both builds" || ok "lock released after the build"

# Liveness: a lock left by a dead pid is reclaimed instead of waited on forever.
lockdir="$(bash -c '. tools/ci/gradle-worktree-lock.sh; gradle_lock_dir')"
mkdir "$lockdir"; printf '999999\t%s\t/x\tdead\t%s\t\n' "$(hostname)" "$(date +%s)" >"$lockdir/owner"
: >"$LOG"
GOATOS_CI_GRADLE_LOCK_TIMEOUT=20 run C 2>/dev/null && grep -q 'end C' "$LOG" && ok "a dead holder's lock is reclaimed" || bad "dead holder blocked the build"

# Default lock dir is /tmp, not a per-session TMPDIR.
d="$(env -u GOATOS_CI_GRADLE_LOCK_DIR TMPDIR=/private/tmp/session-x bash -c '. tools/ci/gradle-worktree-lock.sh; gradle_lock_dir')"
case "$d" in /tmp/goatos-gradle-lock.*) ok "default lock dir is machine-wide /tmp";; *) bad "lock dir follows TMPDIR: $d";; esac

for f in tools/dev/android-dev-run.sh tools/dev/android-e2e-run.sh tools/local/e2e-devices.sh tools/local/multi-role-emulators.sh; do
  if grep -q '\./gradlew' "$f"; then bad "$f still calls ./gradlew outside the lock"; else ok "$f goes through goatos_gradlew"; fi
done
grep -qx 'org.gradle.daemon.idletimeout=600000' apps/goatos-android/gradle.properties && ok "idle daemons exit after 10 min" || bad "daemon idletimeout missing"

[ "$fails" -eq 0 ] && echo "gradle-home/gradle-run test: PASS" || { echo "gradle-home/gradle-run test: $fails failure(s)"; exit 1; }
