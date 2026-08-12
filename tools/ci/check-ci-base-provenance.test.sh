#!/usr/bin/env bash
# check-ci-base-provenance.test.sh — behavioural proof for the CI diff-base hole.
#
# THE HOLE (HOLE B): `GOATOS_CI_BASE=HEAD make ci-local` produced a genuinely
# green mode:"all" receipt recording screenshots:"skipped" on an Android UI diff.
# Both the job classifier and the Android UI-diff detector diff against the base,
# so base=HEAD makes the diff EMPTY, the "skipped-with-ui-diff" banner never
# fires, and the receipt carried no base for push-time validation to check.
# The same thing happened NON-adversarially whenever origin/main was
# unresolvable: both files silently fell back to HEAD~1.
#
# THIS FILE ASSERTS BEHAVIOUR, NOT STRINGS. It never greps for a marker; every
# case runs the real code and asserts the real verdict/exit status. Each case is
# written so that reverting the fix makes it FAIL:
#   (a) base-spoofed receipt is REFUSED at push time      -> revert => push allowed
#   (b) legitimate base still authorizes the push          -> guards over-blocking
#   (c) a receipt with no base at all is REFUSED           -> revert => allowed
#   (d) an unresolvable base is FATAL before any job runs  -> revert => silent HEAD~1
#   (e) the fatal case does NOT fire on the non-certifying lanes we kept working
#   (f) the UI-diff detector goes blind on a spoofed base  -> the hole's mechanism
set -uo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo"

rc=0
tmp="$(mktemp -d "${TMPDIR:-/tmp}/goatos-ci-base-provenance.XXXXXX")"
cleanup() { rm -rf "$tmp"; }
trap cleanup EXIT

pass() { echo "  ok   $1"; }
fail() { echo "  FAIL $1" >&2; rc=1; }

# ── Watchdog ─────────────────────────────────────────────────────────────────
# NO case in this file may hang. A hanging self-test is worse than a missing
# one: CI stalls instead of reporting, and whoever kills it records "flake".
# This actually happened — case (d) below drove `run-local-ci.sh auto` for real,
# so removing the fatal it exists to prove made the run fall through into
# dispatch_jobs and launch the true parallel CI (go test, npm build,
# query-plans). Unbounded, and RED-BY-TIMEOUT is not a verdict.
#
# macOS ships no `timeout`/`gtimeout`, so this is bash-native: background the
# work, poll with `kill -0`, and on the deadline kill the process TREE (the
# child forks job subprocesses; killing only the parent orphans them and they
# keep burning CPU). Sets `g_status` (124 on timeout) and `g_out`.
kill_tree() { # pid — children first, depth first
  local p="$1" c
  for c in $(pgrep -P "$p" 2>/dev/null); do kill_tree "$c"; done
  kill -9 "$p" 2>/dev/null
}

g_status=0
g_out=""
guarded() { # deadline_seconds, label, command...
  local deadline="$1" label="$2"; shift 2
  local outfile="$tmp/guarded.$$.out"
  : >"$outfile"
  "$@" >"$outfile" 2>&1 &
  local pid=$! waited=0 timed_out=0
  while kill -0 "$pid" 2>/dev/null; do
    if [ "$waited" -ge "$deadline" ]; then timed_out=1; kill_tree "$pid"; break; fi
    sleep 1; waited=$(( waited + 1 ))
  done
  wait "$pid" 2>/dev/null
  local st=$?
  g_out="$(cat "$outfile" 2>/dev/null)"
  rm -f "$outfile"
  if [ "$timed_out" = 1 ]; then
    g_status=124
    fail "${label} HUNG: no exit within ${deadline}s; killed. A self-test that blocks reports nothing."
  else
    g_status=$st
  fi
  return 0
}

# An isolated clone so this test never touches the developer's real receipt.
# `origin` is the real repo, so `origin/main` is a genuine remote main.
git clone --quiet --no-hardlinks --shared "$repo" "$tmp/clone" 2>/dev/null || {
  echo "!! could not clone the repo for the base-provenance test" >&2; exit 1;
}
cd "$tmp/clone"
git remote set-url origin "$repo"
git fetch --quiet origin main 2>/dev/null || true
origin_main="$(git rev-parse --verify refs/remotes/origin/main 2>/dev/null || true)"
if [ -z "$origin_main" ]; then
  echo "!! no origin/main available; cannot run the base-provenance test" >&2
  exit 1
fi

# A local commit ahead of remote main — the shape of every real landing.
git checkout --quiet -B provenance-candidate "$origin_main"
# Test the WORKING TREE tooling, not whatever the last commit happened to hold:
# a clone checks out HEAD, so without this the test would silently certify the
# old code. (Overlaying the pristine committed copy here is also how the
# red-then-green proof is produced.)
rm -rf tools/ci
mkdir -p tools/ci
cp -R "$repo/tools/ci/." tools/ci/
mkdir -p apps/goatos-android/feature/feature-demo/src/main/kotlin
cat >apps/goatos-android/feature/feature-demo/src/main/kotlin/DemoScreen.kt <<'KT'
@Composable
fun DemoScreen() { Text("ui change") }
KT
git add -A >/dev/null
git -c user.email=ci@mesha.sg -c user.name=ci commit --quiet -m "android ui change"
candidate="$(git rev-parse HEAD)"

evidence=tools/ci/check-local-ci-evidence.mjs
push_payload="refs/heads/main ${candidate} refs/heads/main ${origin_main}"

# Every external call in this file goes through `guarded`, and every one that
# does not need stdin gets </dev/null — a tool that ever grows an interactive
# prompt must fail on empty input, not block the suite waiting for a human.
record() { # base
  node "$evidence" --record "$candidate" --mode all --base "$1" \
    --jobs "common,backend,query-plans,admin-web,android" --screenshots skipped \
    </dev/null >/dev/null 2>&1
}
record_guarded() { guarded 60 "record" record "$1"; return $g_status; }

# --pre-push reads the push payload on stdin; feed it explicitly so it can never
# sit waiting on an inherited terminal.
prepush() { printf '%s\n' "$push_payload" | node "$evidence" --pre-push 2>&1; }

echo "── (a) base-spoofed receipt (GOATOS_CI_BASE=HEAD) must NOT authorize main"
# This is exactly what a `GOATOS_CI_BASE=HEAD` run records: base == the commit
# being pushed, so the run diffed nothing.
if ! record_guarded "$candidate"; then
  fail "(a) could not record the spoofed receipt (recording is not the gate; pushing is)"
else
  guarded 60 "(a) pre-push" prepush
  out="$g_out"
  if [ "$g_status" -eq 0 ]; then
    fail "(a) a base-spoofed mode=all receipt still authorized a main push"
    printf '%s\n' "$out"
  else
    case "$out" in
      *"not an ancestor of remote main"*) pass "(a) refused: $(printf '%s' "$out" | grep -c 'not an ancestor') reason(s)" ;;
      *) fail "(a) blocked, but not for the base reason:"; printf '%s\n' "$out" ;;
    esac
  fi
fi

echo "── (b) a legitimate base (real remote main) still authorizes the push"
if ! record_guarded "$origin_main"; then
  fail "(b) could not record a legitimate full receipt"
else
  guarded 60 "(b) pre-push" prepush
  if [ "$g_status" -eq 0 ]; then
    pass "(b) legitimate receipt still lands"
  else
    fail "(b) a legitimate full receipt was blocked (over-blocking)"; printf '%s\n' "$g_out"
  fi
fi

echo "── (c) a receipt with NO base must NOT authorize main"
node -e '
  const { execFileSync } = require("node:child_process");
  const { writeFileSync } = require("node:fs");
  const p = execFileSync("git", ["rev-parse", "--git-path", "goatos-ci-local-receipt.json"]).toString().trim();
  writeFileSync(p, JSON.stringify({ sha: process.argv[1], mode: "all", result: "green", screenshots: "skipped" }) + "\n");
' "$candidate" </dev/null
guarded 60 "(c) pre-push" prepush
out="$g_out"
if [ "$g_status" -eq 0 ]; then
  fail "(c) a base-less mode=all receipt authorized a main push"
else
  case "$out" in
    *"records no diff base"*) pass "(c) refused for the missing base" ;;
    *) fail "(c) blocked, but not for the missing base:"; printf '%s\n' "$out" ;;
  esac
fi

cd "$repo"

echo "── (d) an unresolvable base must be FATAL on a receipt-writing run"
# Same code path as an unresolvable origin/main: resolve_ci_base cannot verify
# the ref, falls back to HEAD~1, and the run must refuse before any job executes.
#
# BOUNDED ON PURPOSE (two ways):
#   1. GOATOS_CI_TRACE_ONLY=1 — the refusal is evaluated at line ~171, long
#      before the trace tail, so the fatal still fires exactly as in a real run
#      and this case keeps its full discriminating power. But when the fatal is
#      REMOVED, the run no longer falls through into dispatch_jobs and the real
#      parallel CI: it traces and exits 3 in under a second, so the case FAILS
#      FAST AND LOUDLY on "expected exit 4, got 3" instead of blocking.
#   2. `guarded` — a hard deadline regardless, so no future edit here can hang.
# "before any job ran" is now asserted directly rather than implied: under trace
# every executed gate prints a CI-TRACE line, so ZERO such lines is the proof.
guarded 60 "(d)" env -u GOATOS_FAST_LOCAL_CI GOATOS_CI_BASE=refs/heads/goatos-no-such-base-ref \
  GOATOS_CI_TRACE_ONLY=1 tools/ci/run-local-ci.sh auto
status=$g_status
out="$g_out"
if [ "$status" -ne 4 ]; then
  fail "(d) expected exit 4 on the HEAD~1 fallback, got ${status}"
  printf '%s\n' "$out" | head -20
# Pure-shell match, NOT `printf ... | grep -q`: under `set -o pipefail`, grep -q exits on first match
# and closes the pipe, so printf takes EPIPE mid-write and the pipeline reports failure. Case (e) hit
# exactly that under a loaded parallel run; this case shares the shape, so both are pipe-free.
elif case "$out" in *UNRESOLVABLE*) false ;; *) true ;; esac; then
  fail "(d) exited 4 but never said the base was unresolvable"
elif ! printf '%s' "$out" | grep -q "REFUSING to run the receipt-writing gate"; then
  fail "(d) exited 4 without naming the receipt-writing refusal"
elif printf '%s' "$out" | grep -q '^CI-TRACE '; then
  fail "(d) exited 4 but a gate had already run (CI-TRACE lines present); the refusal is too late"
else
  pass "(d) fatal + loud, exit 4, before any job ran (zero CI-TRACE lines)"
fi

echo "── (e) the non-certifying lanes we deliberately kept working"
# GOATOS_FAST_LOCAL_CI=1 writes no receipt, so the fallback is loud but not fatal.
# Probed with GOATOS_CI_TRACE_ONLY=1 so no real job executes here (trace exits 3).
guarded 60 "(e)" env GOATOS_CI_BASE=refs/heads/goatos-no-such-base-ref GOATOS_FAST_LOCAL_CI=1 \
  GOATOS_CI_TRACE_ONLY=1 tools/ci/run-local-ci.sh auto
status=$g_status
out="$g_out"
if [ "$status" -eq 4 ]; then
  fail "(e) the fast/non-certifying lane was made fatal; offline dev loop is broken"
# Pipe-free for the same reason as (d) above. THIS is the case that went RED in ci-local with
# "printf: write error: Broken pipe" while passing standalone; the first fix attempt landed on (d)
# instead, leaving the failing case untouched.
elif case "$out" in *UNRESOLVABLE*) false ;; *) true ;; esac; then
  fail "(e) the fast lane fell back to HEAD~1 SILENTLY"
else
  pass "(e) fast lane still runs, still loud, writes no receipt (exit ${status})"
fi

echo "── (f) mechanism: a spoofed base makes the UI-diff detector blind"
# Drives the REAL detector with the REAL diff-base resolution. This is why the
# base has to be validated at all: with base=HEAD the Android UI change vanishes.
cat >"$tmp/ui-probe.sh" <<'PROBE'
cd "$1"
. tools/ci/android-ui-diff.sh
changed_since_base() {
  git diff --name-only --diff-filter=ACMRD "${GOATOS_CI_BASE}...HEAD" 2>/dev/null || true
}
if android_ui_diff_detected; then echo ui; else echo no-ui; fi
PROBE
probe() { # base_ref -> prints "ui" or "no-ui"
  guarded 60 "(f) probe" env GOATOS_CI_BASE="$1" bash "$tmp/ui-probe.sh" "$tmp/clone"
  printf '%s' "$g_out"
}
real="$(probe "$origin_main")"
spoof="$(probe "$candidate")"
if [ "$real" != "ui" ]; then
  fail "(f) the detector missed a real Compose change against the true base"
elif [ "$spoof" != "no-ui" ]; then
  pass "(f) detector saw the change even on a spoofed base (stronger than expected)"
else
  pass "(f) confirmed: base=HEAD hides the UI diff — which is why (a) must block"
fi

if [ "$rc" -eq 0 ]; then
  echo "ci-base-provenance self-test: passed"
else
  echo "ci-base-provenance self-test: FAILED" >&2
fi
exit "$rc"
