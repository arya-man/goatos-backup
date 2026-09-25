#!/usr/bin/env bash
# Tests for tools/ci/java21.sh with fake JDKs (no real Java needed).
set -euo pipefail
cd "$(dirname "$0")/../.."
. tools/ci/java21.sh
T="$(mktemp -d)"; trap 'rm -rf "$T"' EXIT
mkjdk() { mkdir -p "$T/$1/bin"; printf '#!/bin/sh\necho "openjdk version \\"%s\\" 2024" >&2\n' "$2" >"$T/$1/bin/java"; chmod +x "$T/$1/bin/java"; }
mkjdk jbr17 17.0.11
mkjdk jdk21 21.0.4
mkjdk jdk21b 21.0.2
fails=0
check() { if [ "$2" = "$3" ]; then echo "ok   $1"; else echo "FAIL $1: want [$3] got [$2]"; fails=$((fails+1)); fi; }

export GOATOS_JAVA21_CANDIDATES="$T/missing $T/jdk21"
check "inherited 21 is kept" "$(java21_resolve "$T/jdk21b" 2>/dev/null)" "$T/jdk21b"
check "inherited 17 is replaced by a 21 candidate" "$(java21_resolve "$T/jbr17" 2>/dev/null)" "$T/jdk21"
check "unset JAVA_HOME finds a 21 candidate" "$(java21_resolve "" 2>/dev/null)" "$T/jdk21"
msg="$(java21_resolve "$T/jbr17" 2>&1 >/dev/null)"
case "$msg" in *"Java 17, not 21"*) echo "ok   says why the inherited JDK was rejected";; *) echo "FAIL no reason: $msg"; fails=$((fails+1));; esac

export GOATOS_JAVA21_CANDIDATES="$T/jbr17 $T/missing"
if java21_resolve "$T/jbr17" >/dev/null 2>&1; then echo "FAIL resolved without any JDK 21"; fails=$((fails+1)); else echo "ok   no JDK 21 anywhere fails loudly"; fi

export GOATOS_JAVA21_CANDIDATES="$T/jbr17 $T/jdk21"
check "a registered jbr-17 candidate (java_home fallback) is skipped" "$(java21_resolve "" 2>/dev/null)" "$T/jdk21"

# android-env.sh must export a verified 21 even when JAVA_HOME points at 17 or is unset.
export GOATOS_JAVA21_CANDIDATES="$T/jdk21"
check "android-env: JAVA_HOME=17 is replaced" "$(JAVA_HOME="$T/jbr17" ANDROID_HOME="$T" bash -c '. tools/dev/android-env.sh 2>/dev/null; android_resolve_env >/dev/null 2>&1; echo "$JAVA_HOME"')" "$T/jdk21"
check "android-env: JAVA_HOME unset gets 21" "$(env -u JAVA_HOME ANDROID_HOME="$T" bash -c '. tools/dev/android-env.sh 2>/dev/null; android_resolve_env >/dev/null 2>&1; echo "$JAVA_HOME"')" "$T/jdk21"
export GOATOS_JAVA21_CANDIDATES="$T/jbr17"
out="$(env -u JAVA_HOME ANDROID_HOME="$T" bash -c '. tools/dev/android-env.sh 2>/dev/null; android_resolve_env' 2>&1 >/dev/null || true)"
case "$out" in *"JDK 21 not found"*) echo "ok   android-env fails with 'JDK 21 not found' when only Java 17 exists";; *) echo "FAIL android-env did not refuse Java 17: $out"; fails=$((fails+1));; esac

# java21_export_or_die: exits before Gradle when only 17 exists; exports 21 otherwise.
if GOATOS_JAVA21_CANDIDATES="$T/jbr17" JAVA_HOME="$T/jbr17" bash -c '. tools/ci/java21.sh; java21_export_or_die; echo started' 2>/dev/null | grep -q started; then echo "FAIL export_or_die let Gradle start on 17"; fails=$((fails+1)); else echo "ok   export_or_die stops before Gradle on 17"; fi
check "export_or_die exports 21 over inherited 17" "$(GOATOS_JAVA21_CANDIDATES="$T/jdk21" JAVA_HOME="$T/jbr17" bash -c '. tools/ci/java21.sh; java21_export_or_die 2>/dev/null; echo "$JAVA_HOME"')" "$T/jdk21"
for f in tools/deploy/stg-mobile-distribution.sh tools/local/e2e-devices.sh tools/local/multi-role-emulators.sh; do
  grep -q 'java21_export_or_die' "$f" && echo "ok   $f enforces JDK 21" || { echo "FAIL $f runs Gradle without JDK 21 enforcement"; fails=$((fails+1)); }
done

# run-local-ci.sh must use the resolver, not a bare ${JAVA_HOME:-...} default.
if grep -q 'local jdk="${JAVA_HOME:-' tools/ci/run-local-ci.sh; then echo "FAIL run-local-ci.sh still trusts JAVA_HOME without a version check"; fails=$((fails+1)); else echo "ok   run-local-ci.sh has no unchecked JAVA_HOME default"; fi
grep -q 'java21_resolve' tools/ci/run-local-ci.sh && echo "ok   run-local-ci.sh calls java21_resolve" || { echo "FAIL run-local-ci.sh does not call java21_resolve"; fails=$((fails+1)); }
grep -qx 'toolchainVersion=21' apps/goatos-android/gradle/gradle-daemon-jvm.properties && echo "ok   daemon JVM pinned to 21" || { echo "FAIL gradle-daemon-jvm.properties missing toolchainVersion=21"; fails=$((fails+1)); }

[ "$fails" -eq 0 ] && echo "java21 test: PASS" || { echo "java21 test: $fails failure(s)"; exit 1; }
