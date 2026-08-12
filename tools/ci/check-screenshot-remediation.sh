#!/usr/bin/env bash
# check-screenshot-remediation.sh — the remediation named by the screenshot
# block must ACTUALLY clear the screenshot block.
#
# The defect this exists for: `make ci-local-screenshots` used to run the
# explicit `android` job. Explicit-job runs are partial and, by design, write NO
# receipt — so the command printed by the pre-push block message
# (check-local-ci-evidence.mjs) and by the ci-local SKIP banner (run-local-ci.sh)
# could never unblock the push. The only working exits were an undocumented env
# var and GOATOS_BYPASS_LOCAL_CI. A remediation message that cannot remediate is
# worse than none: it trains bypass habits.
#
# WHY THIS IS NOT A STRING CHECK. This file has two ancestors that shipped inert
# (a guard that grepped the file it lived in; a receipt field nothing read), so
# nothing here asserts that a string exists somewhere:
#   1. The remediation command is not hardcoded. It is READ OUT of the real
#      messages a user sees, by DRIVING them: the pre-push block is driven with a
#      blocking receipt, the ci-local banner is driven with a real Android UI
#      diff, and `make <target>` is parsed out of the text they print.
#   2. That extracted target is resolved through `make -n` to the real recipe.
#   3. The real recipe is then EXECUTED, and the gate is re-driven. The guard
#      passes only if the push that was blocked is now allowed.
# Rename the target, revert it to a partial job, or make a message name some
# other command, and this guard goes red.
#
# The execution in (3) happens in a throwaway sandbox git repo with `step`
# stubbed, so it costs ~1s and never runs Gradle, never touches this worktree's
# receipt, and never needs a JDK/SDK. Everything between the dispatch `case` and
# the receipt write — scope classification, receipt_mode selection, the
# screenshots_ran state machine, the receipt call — is the REAL code from the
# real tools/ci/run-local-ci.sh.
set -uo pipefail

# $1 = repo to check (default: this checkout). The parameter exists so
# check-screenshot-remediation.test.sh can point the guard at a deliberately
# broken fixture repo and prove it goes RED — it narrows the target, it never
# relaxes a check.
repo="$(cd "${1:-$(dirname "${BASH_SOURCE[0]}")/../..}" && pwd)"
cd "$repo"

rc=0
fail() { echo "!! screenshot remediation guard: $*" >&2; rc=1; }
note() { [ -n "${GOATOS_SCREENSHOT_REMEDIATION_VERBOSE:-}" ] && echo "   $*"; return 0; }

SANDBOX=""
cleanup() { [ -n "$SANDBOX" ] && rm -rf "$SANDBOX"; }
trap cleanup EXIT

ZERO="0000000000000000000000000000000000000000"
UI_FILE="apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/ui/GuardSandboxScreen.kt"

# ---------------------------------------------------------------- sandbox ----
# A minimal git repo carrying the REAL tools/ci/, whose HEAD commit is an Android
# UI diff against its own origin/main. `step` is overridden to a no-op right
# before the dispatch `case`, so job bodies cost nothing while every decision
# path that decides "receipt or no receipt" runs for real.
build_sandbox() {
  SANDBOX="$(mktemp -d)"
  mkdir -p "$SANDBOX/tools" "$SANDBOX/fakejdk/bin" "$SANDBOX/fakesdk"
  cp -R tools/ci "$SANDBOX/tools/ci"
  : >"$SANDBOX/fakejdk/bin/java"; chmod +x "$SANDBOX/fakejdk/bin/java"

  # Stub the two choke points every gate flows through. Inserted immediately
  # before the dispatch `case` so it overrides the real definitions without
  # editing any logic below it.
  local target="$SANDBOX/tools/ci/run-local-ci.sh"
  awk '
    /^case "\$only" in$/ && !done {
      print "step() { local n=\"$1\"; shift; echo \"STUB-STEP ${n}\"; RESULTS+=(\"PASS  ${n} (stub)\"); }"
      print "optional_step() { local n=\"$1\"; shift; echo \"STUB-STEP ${n}\"; RESULTS+=(\"PASS  ${n} (stub)\"); }"
      done=1
    }
    /gradle-worktree-lock\.sh"/ && !lock_done {
      print
      print "gradle_lock_acquire() { return 0; }"
      print "gradle_lock_install_trap() { return 0; }"
      print "gradle_lock_clear_trap() { return 0; }"
      print "gradle_lock_release() { return 0; }"
      lock_done=1
      next
    }
    { print }
  ' "$SANDBOX/tools/ci/run-local-ci.sh" >"$target.stubbed" && mv "$target.stubbed" "$target"
  grep -q '^step() { local n=' "$target" || { fail "could not stub run-local-ci.sh in the sandbox"; return 1; }

  (
    cd "$SANDBOX" || exit 1
    git init -q -b main .
    git config user.email guard@local; git config user.name guard
    git config commit.gpgsign false
    echo seed >seed.txt
    git add -A >/dev/null && git commit -qm base
    git update-ref refs/remotes/origin/main HEAD
    mkdir -p "$(dirname "$UI_FILE")"
    printf '@Composable\nfun GuardSandboxScreen() { Text("x") }\n' >"$UI_FILE"
    git add -A >/dev/null && git commit -qm "android ui change"
  ) || { fail "could not build sandbox git repo"; return 1; }

  SANDBOX_HEAD="$(git -C "$SANDBOX" rev-parse HEAD)"
  SANDBOX_BASE="$(git -C "$SANDBOX" rev-parse refs/remotes/origin/main)"
  return 0
}

# Drive the REAL pre-push gate in the sandbox against the receipt currently
# present there, and print exactly what a user pushing main would see.
drive_pre_push() {
  ( cd "$SANDBOX" \
      && printf 'refs/heads/main %s refs/heads/main %s\n' "$SANDBOX_HEAD" "$SANDBOX_BASE" \
       | node tools/ci/check-local-ci-evidence.mjs --pre-push 2>&1 )
}

record_blocking_receipt() {
  ( cd "$SANDBOX" && node tools/ci/check-local-ci-evidence.mjs \
      --record "$SANDBOX_HEAD" --mode all --base "$SANDBOX_BASE" \
      --jobs common,backend,query-plans,admin-web,android \
      --screenshots skipped-with-ui-diff >/dev/null 2>&1 )
}

receipt_file() { echo "$SANDBOX/$(cd "$SANDBOX" && git rev-parse --git-path goatos-ci-local-receipt.json)"; }
clear_receipt() { rm -f "$(receipt_file)" 2>/dev/null; return 0; }

# `make <target>` mentioned in a message. Fails loudly on zero or on disagreement.
extract_make_target() { # <text>
  printf '%s\n' "$1" | grep -oE 'make [a-z0-9][a-z0-9-]*' | sed 's/^make //' | sort -u
}

build_sandbox || { echo "!! screenshot remediation guard: sandbox unavailable" >&2; exit 1; }

# ------------------------------------------- 1. what do the messages SAY? ----
# (a) the pre-push block, driven for real with a blocking receipt.
record_blocking_receipt || fail "could not record a skipped-with-ui-diff receipt in the sandbox"
block_text="$(drive_pre_push)"
if ! printf '%s' "$block_text" | grep -q 'skipped-with-ui-diff'; then
  fail "a receipt recording screenshots=skipped-with-ui-diff did NOT block a main push; the screenshot evidence gate is inert"
  printf '%s\n' "$block_text" >&2
fi
note "block message: $block_text"

# (b) the ci-local SKIP banner, driven for real: android job, screenshots OFF,
#     on a commit that genuinely touches Android UI.
banner_text="$( cd "$SANDBOX" && env -u GOATOS_RUN_ANDROID_SCREENSHOTS \
  JAVA_HOME="$SANDBOX/fakejdk" ANDROID_HOME="$SANDBOX/fakesdk" \
  bash tools/ci/run-local-ci.sh android 2>&1 )"
printf '%s' "$banner_text" | grep -q 'SKIPPED WITH UI DIFF\|TOUCHES ANDROID UI' \
  || fail "the android job did not emit the UI-diff screenshot warning on a real Android UI diff; the banner path is dead"

msg_targets="$(printf '%s\n%s\n' "$block_text" "$banner_text" | grep -E 'screenshot|Paparazzi|SKIP' | { grep -oE 'make [a-z0-9][a-z0-9-]*' || true; } | sed 's/^make //' | sort -u)"
count="$(printf '%s\n' "$msg_targets" | grep -c . || true)"
if [ "${count:-0}" -eq 0 ]; then
  fail "neither the pre-push block nor the ci-local banner names a \`make <target>\` to run; a block with no remediation is not actionable"
  exit "$rc"
fi
if [ "${count:-0}" -gt 1 ]; then
  fail "the screenshot messages name MORE THAN ONE remediation command ($(printf '%s' "$msg_targets" | tr '\n' ' ')); an inconsistent remediation is the same defect one file over"
fi
TARGET="$(printf '%s\n' "$msg_targets" | head -1)"
note "remediation target named by the messages: make $TARGET"

# ------------------------------ 2. resolve that target to its real recipe ----
recipe="$(make -n "$TARGET" 2>/dev/null | grep -E 'run-local-ci\.sh' | head -1)"
[ -n "$recipe" ] || {
  fail "\`make $TARGET\` (named by the block message) does not exist or does not run tools/ci/run-local-ci.sh"
  exit "$rc"
}
note "recipe: $recipe"

resolved_arg="$(printf '%s\n' "$recipe" | sed -E 's/.*run-local-ci\.sh[[:space:]]*//; s/[[:space:]].*//')"
case "$resolved_arg" in
  auto|all|"")
    ;;
  *)
    fail "\`make $TARGET\` resolves to \`run-local-ci.sh $resolved_arg\` — an explicit-job PARTIAL run, which writes NO receipt. Following the instruction would have left the receipt UNTOUCHED and can never clear the block."
    exit "$rc"
    ;;
esac

# ---------- 2b. the target must stay receipt-writing under HOSTILE make vars ---
# The first version of this target passed $(MODE) straight through, so
# `MODE=android make ci-local-screenshots` resolved to an explicit-job PARTIAL
# run — no receipt — and the documented remediation silently stopped working
# again. Resolving the target with no vars set could not see that, because the
# default path was fine. So probe the injectable vars directly: every resolution
# must land on a receipt-writing mode (`auto` or `all`), never a named job.
for hostile in MODE=android MODE=backend MODE=common MODE=admin-web MODE=guardrails MODE=query-plans JOB=android JOB=backend; do
  hostile_recipe="$(make -n "$hostile" "$TARGET" 2>/dev/null | grep -E 'run-local-ci\.sh' | head -1)"
  [ -n "$hostile_recipe" ] || continue
  resolved_arg="$(printf '%s\n' "$hostile_recipe" | sed -E 's/.*run-local-ci\.sh[[:space:]]*//; s/[[:space:]].*//')"
  case "$resolved_arg" in
    auto|all|"")
      ;;
    *)
      fail "\`$hostile make $TARGET\` resolves to \`run-local-ci.sh $resolved_arg\` — an explicit-job PARTIAL run, which writes NO receipt. The remediation command must be receipt-writing under every injectable make variable, or the block message becomes unfollowable again. Clamp the variable in the recipe."
      ;;
  esac
done

# --------------------- 3. EXECUTE it, then re-drive the gate it must clear ----
clear_receipt
record_blocking_receipt || fail "could not re-arm the blocking receipt"
armed="$(cat "$(receipt_file)" 2>/dev/null)"
run_out="$( cd "$SANDBOX" && env -u GOATOS_FAST_LOCAL_CI -u GOATOS_CI_TRACE_ONLY \
  JAVA_HOME="$SANDBOX/fakejdk" ANDROID_HOME="$SANDBOX/fakesdk" \
  bash -c "$recipe" 2>&1 )"
if [ "$(cat "$(receipt_file)" 2>/dev/null)" = "$armed" ]; then
  fail "\`make $TARGET\` is the command the screenshot block tells users to run, but it left the receipt UNTOUCHED — it is a partial run, and partial runs write no receipt by design. Following the instruction can never clear the block; the only exits left are an env var or GOATOS_BYPASS_LOCAL_CI"
fi
if ! printf '%s' "$run_out" | grep -q 'ci-local: GREEN'; then
  fail "\`make $TARGET\` did not reach a GREEN run in the sandbox; cannot prove it clears the block"
  printf '%s\n' "$run_out" | tail -20 >&2
  exit "$rc"
fi

receipt="$(receipt_file)"
if [ ! -f "$receipt" ]; then
  fail "\`make $TARGET\` is the command the screenshot block tells users to run, but it writes NO receipt (partial run), so following the instruction can never clear the block — the only exits left are an env var or the bypass"
  exit "$rc"
fi
shots="$(node -e 'process.stdout.write(String(JSON.parse(require("fs").readFileSync(process.argv[1],"utf8")).screenshots))' "$receipt" 2>/dev/null)"
[ "$shots" = "yes" ] || fail "\`make $TARGET\` wrote a receipt recording screenshots=\"$shots\"; the remediation must actually PROVE screenshots, not re-record the gap"

after="$(drive_pre_push)"
after_rc=$?
if [ "$after_rc" -ne 0 ] || printf '%s' "$after" | grep -q 'skipped-with-ui-diff'; then
  fail "after running \`make $TARGET\` the main push is STILL blocked; the remediation does not remediate"
  printf '%s\n' "$after" >&2
fi

exit $rc
