#!/usr/bin/env bash
# Adversarial self-test for tools/ci/check-gradle-config-cache.sh.
#
# Builds a standalone throwaway Gradle project (no AGP — fast) that reproduces
# the EXACT defect class from commit 67c15e18e:
#   a bare ProcessBuilder(...).start() evaluated at configuration time, behind a
#   `gradle.startParameter.taskNames` gate, under org.gradle.configuration-cache=true.
#
# Case (a) RED   : fixture contains the broken pattern -> guard MUST exit non-zero
#                  AND name the configuration-time-exec cause. This also proves the
#                  guard's multi-task-shape list reaches a task-name-gated branch:
#                  the exec is gated on "assemble", so a `help`-only probe would
#                  pass and the guard would be worthless.
# Case (b) GREEN : same fixture with the exec moved into a ValueSource -> guard
#                  MUST exit zero.
#
# A guard that cannot go red here is not a guard.
set -uo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
guard="$repo_root/tools/ci/check-gradle-config-cache.sh"
gradlew="$repo_root/apps/goatos-android/gradlew"

if [ ! -x "$gradlew" ]; then
  echo "FAIL self-test: gradle wrapper missing at $gradlew" >&2
  exit 1
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

fixture="$work/fixture"
mkdir -p "$fixture"
cat > "$fixture/settings.gradle.kts" <<'EOF'
rootProject.name = "configCacheFixture"
EOF
cat > "$fixture/gradle.properties" <<'EOF'
org.gradle.configuration-cache=true
org.gradle.jvmargs=-Xmx512m
EOF

write_broken() {
  cat > "$fixture/build.gradle.kts" <<'EOF'
import java.io.ByteArrayOutputStream

// DEFECT UNDER TEST: bare external process at CONFIGURATION time, behind a
// task-name gate (exactly the shape of commit 67c15e18e).
val needsMetadata = gradle.startParameter.taskNames.any {
    it.contains("assemble", ignoreCase = true)
}

fun gitOutput(vararg args: String): String {
    if (!needsMetadata) return ""
    return runCatching {
        val stdout = ByteArrayOutputStream()
        val process = ProcessBuilder(listOf("git", *args))
            .directory(rootDir)
            .redirectError(ProcessBuilder.Redirect.DISCARD)
            .start()
        process.inputStream.use { it.copyTo(stdout) }
        if (process.waitFor() == 0) stdout.toString().trim() else ""
    }.getOrDefault("")
}

val sourceCommit: String = gitOutput("rev-parse", "HEAD").ifBlank { "unknown" }

tasks.register("assembleFixture") {
    val commit = sourceCommit
    doLast { println("commit=$commit") }
}
EOF
}

write_fixed() {
  cat > "$fixture/build.gradle.kts" <<'EOF'
import java.io.ByteArrayOutputStream

// SANCTIONED SHAPE: the same external process, obtained through a ValueSource.
abstract class GitOutputValueSource : ValueSource<String, GitOutputValueSource.Params> {
    interface Params : ValueSourceParameters {
        val workingDir: DirectoryProperty
        val args: ListProperty<String>
    }

    override fun obtain(): String = runCatching {
        val stdout = ByteArrayOutputStream()
        val process = ProcessBuilder(listOf("git") + parameters.args.get())
            .directory(parameters.workingDir.get().asFile)
            .redirectError(ProcessBuilder.Redirect.DISCARD)
            .start()
        process.inputStream.use { it.copyTo(stdout) }
        if (process.waitFor() == 0) stdout.toString().trim() else ""
    }.getOrDefault("")
}

fun gitOutput(vararg args: String): String =
    providers.of(GitOutputValueSource::class) {
        parameters.workingDir.set(layout.projectDirectory)
        parameters.args.set(args.toList())
    }.get()

val sourceCommit: String = gitOutput("rev-parse", "HEAD").ifBlank { "unknown" }

tasks.register("assembleFixture") {
    val commit = sourceCommit
    doLast { println("commit=$commit") }
}
EOF
}

run_guard() {
  bash "$guard" --project "$fixture" --gradlew "$gradlew" --tasks "help assembleFixture" 2>&1
}

fail=0

echo "── case (a) RED: broken fixture must FAIL the guard"
write_broken
red_out="$(run_guard)"; red_rc=$?
if [ "$red_rc" -eq 0 ]; then
  echo "FAIL (a): guard PASSED a build.gradle.kts with a configuration-time ProcessBuilder." >&2
  echo "$red_out" >&2
  fail=1
elif ! printf '%s' "$red_out" | grep -q 'CAUSE: an external process is started during the CONFIGURATION phase'; then
  echo "FAIL (a): guard failed, but not for the configuration-time-exec reason." >&2
  echo "$red_out" >&2
  fail=1
else
  echo "ok  (a) guard went red on the real broken pattern (rc=$red_rc)"
fi

echo "── case (b) GREEN: ValueSource fixture must PASS the guard"
write_fixed
green_out="$(run_guard)"; green_rc=$?
if [ "$green_rc" -ne 0 ]; then
  echo "FAIL (b): guard rejected the sanctioned ValueSource shape (rc=$green_rc)." >&2
  echo "$green_out" >&2
  fail=1
else
  echo "ok  (b) guard green on the ValueSource shape"
fi

if [ "$fail" -ne 0 ]; then
  echo "check-gradle-config-cache.test.sh: FAILED" >&2
  exit 1
fi
echo "check-gradle-config-cache.test.sh: PASSED"
