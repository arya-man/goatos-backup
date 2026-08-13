#!/usr/bin/env bash
# check-gradle-config-cache.sh — BEHAVIOURAL guard against configuration-time
# external processes in the Android Gradle build.
#
# WHAT IT PROTECTS
# ----------------
# apps/goatos-android/gradle.properties sets `org.gradle.configuration-cache=true`.
# With that on, starting an external process during the CONFIGURATION phase is a
# hard build failure:
#     "Starting an external process ... during configuration time is unsupported"
# Commit 67c15e18e ("chore(android): trace Firebase distribution builds") added a
# bare `ProcessBuilder(listOf("git", ...)).start()` in a top-level `gitOutput()`
# helper in apps/goatos-android/app/build.gradle.kts, evaluated while configuring
# the :app project. The sanctioned escape hatch is a `ValueSource` obtained via
# `providers.of(...)`, which Gradle re-obtains when reusing a cached entry.
#
# HOW IT CHECKS
# -------------
# This is NOT a source regex. It runs a REAL Gradle invocation with the
# configuration cache ON and `--dry-run`, so the CONFIGURATION phase executes in
# full while no task actions run. A configuration-time exec fails there, exactly
# as it would in a real build, and the guard fails with it.
#
# The requested task list deliberately mixes shapes, because config-time work is
# often gated on `gradle.startParameter.taskNames`:
#   - :benchmark:compileDevNonMinifiedBenchmarkKotlin  (the task that went 15/15
#     red and is the only CI step running with the configuration cache ON)
#   - :app:assembleStgRelease                          (trips task-name gates such
#     as 67c15e18e's `needsFirebaseSourceMetadata`, which is FALSE for the
#     benchmark task and so hides the defect from a benchmark-only probe)
# A guard that probed only one shape would be satisfiable by the very gating that
# lets the defect survive. That is the trap this file exists to avoid.
#
# BLIND SPOTS (stated honestly)
# -----------------------------
#   * Only the two task shapes above are configured. A config-time exec reachable
#     ONLY from some third task-name gate (e.g. a `prodRelease`-only branch) is
#     not exercised. Add the task to TASKS below when such a gate is introduced.
#   * `--dry-run` runs configuration, not execution. A process started from inside
#     a task ACTION is legal and correctly not flagged — that is by design.
#   * Needs a JDK and the Android SDK. Callers with neither must skip explicitly
#     rather than treat a missing toolchain as a pass; run_local_ci's android job
#     already gates on that.
#
# USAGE
#   bash tools/ci/check-gradle-config-cache.sh
#   bash tools/ci/check-gradle-config-cache.sh --project DIR --tasks "t1 t2"
#
# SELF-TEST: tools/ci/check-gradle-config-cache.test.sh (red-then-green against a
# standalone fixture project that physically contains the broken pattern).
set -uo pipefail

PROJECT_DIR="apps/goatos-android"
TASKS=":benchmark:compileDevNonMinifiedBenchmarkKotlin :app:assembleStgRelease"
GRADLEW=""

while [ $# -gt 0 ]; do
  case "$1" in
    --project) PROJECT_DIR="$2"; shift 2 ;;
    --tasks)   TASKS="$2"; shift 2 ;;
    --gradlew) GRADLEW="$2"; shift 2 ;;
    *) echo "check-gradle-config-cache: unknown argument '$1'" >&2; exit 2 ;;
  esac
done

[ -n "$GRADLEW" ] || GRADLEW="$PROJECT_DIR/gradlew"

if [ ! -x "$GRADLEW" ]; then
  echo "check-gradle-config-cache: no gradle wrapper at $GRADLEW" >&2
  exit 2
fi

out="$(mktemp)"
trap 'rm -f "$out"' EXIT

# shellcheck disable=SC2086
"$GRADLEW" -p "$PROJECT_DIR" $TASKS \
  --dry-run --configuration-cache --console=plain >"$out" 2>&1
rc=$?

if [ "$rc" -eq 0 ]; then
  echo "OK  gradle configuration phase is configuration-cache clean ($PROJECT_DIR)"
  exit 0
fi

echo "FAIL check-gradle-config-cache: configuration failed for $PROJECT_DIR" >&2
echo "     tasks: $TASKS" >&2

if grep -qiE 'external process.*(configuration time|during configuration)|Starting an external process' "$out"; then
  cat >&2 <<'MSG'

     CAUSE: an external process is started during the CONFIGURATION phase, which
     is illegal with org.gradle.configuration-cache=true.

     FIX: do not call ProcessBuilder / Runtime.exec / project.exec at
     configuration time. Wrap it in a ValueSource and obtain it lazily:

         abstract class GitOutputValueSource :
             ValueSource<String, GitOutputValueSource.Params> { ... }
         providers.of(GitOutputValueSource::class) { ... }

     Gating the call on gradle.startParameter.taskNames is NOT a fix — it only
     hides the failure from the task shapes that happen not to match.
MSG
fi

echo "" >&2
echo "     ---- gradle output (tail) ----" >&2
tail -60 "$out" >&2
exit 1
