#!/usr/bin/env bash
# Tests for tools/ci/gradle-machine-setup.sh and the machine-lock init script.
# Sandboxed HOME / Gradle home; never touches the real ~/.gradle and runs no
# Gradle build.
set -euo pipefail
cd "$(dirname "$0")/../.."
T="$(mktemp -d)"; trap 'rm -rf "$T"; [ -n "${holder:-}" ] && kill -9 "$holder" 2>/dev/null || true' EXIT
fails=0
ok() { echo "ok   $1"; }
bad() { echo "FAIL $1"; fails=$((fails+1)); }

mkdir -p "$T/jdk21/bin" "$T/jbr17/bin"
printf '#!/bin/sh\necho "openjdk version \\"21.0.4\\"" >&2\n' >"$T/jdk21/bin/java"
printf '#!/bin/sh\necho "openjdk version \\"17.0.11\\"" >&2\n' >"$T/jbr17/bin/java"
chmod +x "$T/jdk21/bin/java" "$T/jbr17/bin/java"

GH="$T/gradle-home"
install() { env -u JAVA_HOME GRADLE_USER_HOME="$GH" GOATOS_GRADLE_BREW_JDK21="${BREW:-$T/jdk21}" GOATOS_JAVA21_CANDIDATES="${CANDS:-$T/none}" bash tools/ci/gradle-machine-setup.sh 2>/dev/null; }

mkdir -p "$GH"
printf 'goatosDevApiBaseUrl=http://x/\norg.gradle.java.home=/old/jbr-17\n' >"$GH/gradle.properties"
install
cmp -s tools/ci/gradle-init/goatos-machine-lock.init.gradle "$GH/init.d/goatos-machine-lock.init.gradle" && ok "init script installed into init.d" || bad "init script not installed"
grep -qx 'goatosDevApiBaseUrl=http://x/' "$GH/gradle.properties" && ok "existing user properties kept" || bad "user properties lost"
grep -qx 'org.gradle.daemon.idletimeout=600000' "$GH/gradle.properties" && ok "idletimeout in managed block" || bad "idletimeout missing"
[ "$(tail -2 "$GH/gradle.properties" | head -1)" = "org.gradle.java.home=$T/jdk21" ] && ok "java.home pinned to the verified JDK 21, after the old value (last one wins)" || { bad "java.home not pinned last"; cat "$GH/gradle.properties"; }

cp "$GH/gradle.properties" "$T/p1"; cp "$GH/init.d/goatos-machine-lock.init.gradle" "$T/i1"
install; install
cmp -s "$T/p1" "$GH/gradle.properties" && ok "second and third run leave gradle.properties unchanged" || bad "gradle.properties changed on rerun"
[ "$(grep -c '^# >>> goatos managed' "$GH/gradle.properties")" = 1 ] && ok "managed block appears once" || bad "managed block duplicated"
cmp -s "$T/i1" "$GH/init.d/goatos-machine-lock.init.gradle" && ok "init script stable on rerun" || bad "init script changed on rerun"

echo "// stale" >"$GH/init.d/goatos-machine-lock.init.gradle"
install
cmp -s tools/ci/gradle-init/goatos-machine-lock.init.gradle "$GH/init.d/goatos-machine-lock.init.gradle" && ok "a stale installed copy is replaced" || bad "stale copy kept"

BREW="$T/jbr17" install
grep -q '^org.gradle.java.home=.*jbr17' "$GH/gradle.properties" && bad "pinned a Java 17 as org.gradle.java.home" || ok "never pins a non-21 JDK"
grep -q "^org.gradle.java.home=$T/jdk21" "$GH/gradle.properties" && bad "kept a stale 21 pin with no JDK 21 found" || ok "no JDK 21 found: no java.home pin written"

rm -rf "$GH"; GOATOS_GRADLE_MACHINE_SETUP=0 install
[ -e "$GH/init.d" ] && bad "GOATOS_GRADLE_MACHINE_SETUP=0 still installed" || ok "GOATOS_GRADLE_MACHINE_SETUP=0 skips"

grep -q 'gradle_machine_install' tools/ci/gradle-home.sh && ok "every Gradle entrypoint installs it (gradle-home.sh)" || bad "gradle-home.sh does not install"
grep -q 'gradle-machine-setup.sh' Makefile && ok "make ai-setup installs it" || bad "ai-setup does not install"

# Init script scope + shape.
f=tools/ci/gradle-init/goatos-machine-lock.init.gradle
grep -q 'endsWith("/apps/goatos-android")' "$f" && ok "init script only affects the goatos Android build" || bad "init script not scoped"
grep -q 'channel.tryLock()' "$f" && grep -q 'void close()' "$f" && ok "OS file lock, released in close() at build end" || bad "lock/release shape missing"
grep -q 'gradle.parent != null' "$f" && ok "nested builds do not re-take the lock (no self-deadlock)" || bad "nested builds would self-deadlock"
grep -q 'GOATOS_GRADLE_MACHINE_LOCK_TIMEOUT_MIN' "$f" && grep -q 'throw new GoatosMachineLockTimeout' "$f" && grep -q 'getOrElse(45L)' "$f" && ok "wait times out (default 45 min, configurable) and fails the build" || bad "no lock wait timeout"
grep -q 'now - lastNote >= 60000' "$f" && ok "waiting is logged every 60s" || bad "wait log cadence not 60s"
grep -q 'NOT killed' "$f" && ! grep -Eq 'destroy|kill\(' "$f" && ok "timeout never kills the holder" || bad "timeout may kill the holder"

# The init script compiles against the Gradle API, when a local Gradle 9 distribution exists.
lib="$(ls -d "$HOME"/.gradle/wrapper/dists/gradle-9*-bin/*/gradle-9*/lib 2>/dev/null | head -1 || true)"
J="$(bash -c '. tools/ci/java21.sh; java21_resolve "${JAVA_HOME:-}"' 2>/dev/null || true)"
if [ -n "$lib" ] && [ -n "$J" ]; then
  cp "$f" "$T/lockinit.groovy"
  if "$J/bin/java" -cp "$(ls "$lib"/*.jar "$lib"/plugins/*.jar | tr '\n' ':')" org.codehaus.groovy.tools.FileSystemCompiler -d "$T/out" "$T/lockinit.groovy" >"$T/compile.log" 2>&1; then
    ok "init script compiles against $(basename "$(dirname "$lib")")"
  else
    bad "init script does not compile"; cat "$T/compile.log"
  fi
  # OS lock semantics the init script relies on: exclusive across processes,
  # dropped when the holder is killed -9 (no stale lock).
  cat >"$T/Lk.java" <<'J'
import java.nio.channels.*; import java.nio.file.*;
public class Lk { public static void main(String[] a) throws Exception {
  FileChannel c = FileChannel.open(Paths.get(a[0]), StandardOpenOption.CREATE, StandardOpenOption.WRITE);
  FileLock l = c.tryLock();
  if (a[1].equals("hold")) { if (l == null) System.exit(2); System.out.println("held"); System.out.flush(); Thread.sleep(60000); }
  System.out.println(l == null ? "busy" : "free"); } }
J
  "$J/bin/java" "$T/Lk.java" "$T/build.lock" hold >"$T/hold.out" & holder=$!
  for _ in $(seq 1 100); do grep -q held "$T/hold.out" 2>/dev/null && break; sleep 0.2; done
  [ "$("$J/bin/java" "$T/Lk.java" "$T/build.lock" probe)" = busy ] && ok "a second process cannot take the lock while it is held" || bad "lock not exclusive"
  kill -9 "$holder"; wait "$holder" 2>/dev/null || true; holder=""
  [ "$("$J/bin/java" "$T/Lk.java" "$T/build.lock" probe)" = free ] && ok "kill -9 of the holder frees the lock (no stale lock)" || bad "lock survived holder death"
else
  echo "skip init-script compile + lock semantics (no local Gradle 9 distribution or JDK 21)"
fi

[ "$fails" -eq 0 ] && echo "gradle-machine-setup test: PASS" || { echo "gradle-machine-setup test: $fails failure(s)"; exit 1; }
