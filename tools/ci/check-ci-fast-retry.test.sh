#!/usr/bin/env bash
# check-ci-fast-retry.test.sh — self-test for "fast fail + fast retry" in ci-local.
#
#   A. input-keyed step cache, against the SHIPPED run-local-ci.sh in a throwaway
#      repo (stub steps count their executions):
#        - nothing changed          -> every step reused (0 executions)
#        - one backend file changed -> backend + whole steps re-run, android reused
#        - the step script changed  -> every step re-runs
#        - the receipt lists every step with a digest that --verify accepts, and a
#          tampered digest is refused.
#   B. fail fast: a failing step stops the dispatch's OTHER jobs within seconds,
#      prints the single-step re-run command, stays RED — and an unrelated process
#      (standing in for another session's build) is left alive.
#   The digest-level matrix is in `node tools/ci/step-input-digest.mjs --self-test`.
set -uo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo"
rc=0
ok()  { echo "  ok   — $1"; }
bad() { echo "  FAIL — $1" >&2; rc=1; }

tmp="$(mktemp -d "${TMPDIR:-/tmp}/goatos-ci-fast-retry.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT
cnt="$tmp/counts"; mkdir -p "$cnt"

harness() { # <dest.sh> <tag>
  awk -v c="$cnt" -v tag="$2" '
    /^run_job\(\) \{/ && !done {
      print "run_common()      { current_job=common; step \"stub common\" bash -c \"echo " tag " >>" c "/common\"; }";
      print "run_backend()     { current_job=backend; step \"backend go vet\" bash -c \"echo x >>" c "/backend\"; }";
      print "run_query_plans() { current_job=query-plans; step \"stub qp\" true; }";
      print "run_admin_web()   { current_job=admin-web; step \"admin-web lint\" bash -c \"echo x >>" c "/adminweb\"; }";
      print "run_android()     { current_job=android; screenshots_ran=skipped; step \"android :app compile+unit+lint\" bash -c \"echo x >>" c "/android\"; }";
      done = 1
    } { print }' tools/ci/run-local-ci.sh >"$1"
}
n() { wc -l <"$cnt/$1" 2>/dev/null | tr -d ' ' || echo 0; }
g() { git -C "$tmp/repo" -c user.email=t@mesha.sg -c user.name=t -c commit.gpgsign=false "$@"; }
run_all() { ( cd "$tmp/repo" && env -u GOATOS_FAST_LOCAL_CI -u GOATOS_CI_TRACE_ONLY -u GOATOS_CI_STEP_CACHE bash tools/ci/run-local-ci.sh all ) >"$tmp/$1.log" 2>&1; }

# ── A. input-keyed cache ─────────────────────────────────────────────────────
mkdir -p "$tmp/repo/tools/ci" "$tmp/repo/backend" "$tmp/repo/apps/goatos-android"
cp tools/ci/*.mjs tools/ci/*.json tools/ci/*.sh "$tmp/repo/tools/ci/" 2>/dev/null
echo "package a" >"$tmp/repo/backend/a.go"; echo "val b = 1" >"$tmp/repo/apps/goatos-android/b.kt"
harness "$tmp/repo/tools/ci/run-local-ci.sh" v1
g init -q . >/dev/null; g add -A; g commit -qm base
g update-ref refs/remotes/origin/main HEAD
g commit -q --allow-empty -m candidate

run_all first || { bad "first run was RED"; tail -30 "$tmp/first.log" >&2; }
[ "$(n backend)$(n android)$(n common)" = "111" ] && ok "first run executes every step" || bad "first run counts b=$(n backend) a=$(n android) c=$(n common)"

run_all second || bad "second run was RED"
[ "$(n backend)$(n android)$(n common)" = "111" ] && ok "unchanged inputs -> every step reused" || bad "unchanged run re-executed: b=$(n backend) a=$(n android) c=$(n common)"
grep -q "reused PASS" "$tmp/second.log" && ok "reuse is reported on the step line" || bad "no reuse message"
( cd "$tmp/repo" && node tools/ci/check-local-ci-evidence.mjs --verify ) >/dev/null 2>&1 \
  && ok "receipt with all-reused steps verifies against the tree" || bad "receipt with reused steps did not verify"
receipt="$tmp/repo/.git/goatos-ci-local-receipt.json"
node -e 'const r=require(process.argv[1]); if(!(r.steps||[]).some(s=>s.status==="reused")) process.exit(1)' "$receipt" \
  && ok "receipt ledger records reused steps" || bad "receipt ledger has no reused steps"

# tampered digest -> refused
cp "$receipt" "$tmp/receipt.bak"
node -e 'const f=process.argv[1],fs=require("fs");const r=JSON.parse(fs.readFileSync(f));r.steps[0].digest="0".repeat(64);fs.writeFileSync(f,JSON.stringify(r))' "$receipt"
( cd "$tmp/repo" && node tools/ci/check-local-ci-evidence.mjs --verify ) >/dev/null 2>&1 \
  && bad "a receipt whose reused step hash mismatches the tree was ACCEPTED" || ok "mismatched step hash is refused at verify"
cp "$tmp/receipt.bak" "$receipt"

echo "package a // fix" >"$tmp/repo/backend/a.go"; g commit -qam "backend fix"
run_all third || bad "third run was RED"
[ "$(n backend)" = "2" ] && ok "backend change -> backend step re-runs" || bad "backend step count $(n backend)"
[ "$(n android)" = "1" ] && ok "backend change -> android step reused" || bad "android re-ran on a backend-only change"
[ "$(n common)" = "2" ] && ok "backend change -> whole-tree guard re-runs" || bad "whole-tree step count $(n common)"

harness "$tmp/repo/tools/ci/run-local-ci.sh" v2; g commit -qam "step script"
run_all fourth || bad "fourth run was RED"
[ "$(n backend)$(n android)$(n common)" = "323" ] && ok "step-script change -> every step re-runs" || bad "script change counts b=$(n backend) a=$(n android) c=$(n common)"

# single-step re-run can never produce a receipt
( cd "$tmp/repo" && GOATOS_CI_ONLY_STEP="backend go vet" bash tools/ci/run-local-ci.sh all ) >/dev/null 2>&1
[ "$?" -eq 2 ] && ok "GOATOS_CI_ONLY_STEP is refused on receipt-writing modes" || bad "GOATOS_CI_ONLY_STEP ran a receipt-writing mode"

# ── B. fail fast + kill scoping ──────────────────────────────────────────────
sleep 300 & bystander=$!
out="$(bash -c '
  set -uo pipefail; . tools/ci/parallel-dispatch.sh
  declare -a RESULTS TIMINGS FAILED_JOBS FAILURES; fail=0; screenshots_ran=skipped; current_job=x
  fail_fast_signal() { printf "%s\t%s\n" "$current_job" "$1" >"$GOATOS_CI_FAILFAST_FILE"; }
  run_job() { current_job="$1"; case "$1" in
    common)  sleep 0.5; echo "tail marker line"; fail_fast_signal "broken guard"; fail=1; sleep 60 ;;
    backend) sleep 60; echo SURVIVED >"'"$tmp"'/backend.survived" ;;
    *) : ;;
  esac; }
  t0=$SECONDS
  dispatch_jobs common backend
  echo "VERDICT fail=$fail elapsed=$((SECONDS - t0))"' 2>&1)"
case "$out" in *"VERDICT fail=1"*) ok "fail-fast run is RED" ;; *) bad "fail-fast run not RED"; printf '%s\n' "$out" >&2 ;; esac
el="$(printf '%s\n' "$out" | sed -n 's/.*elapsed=\([0-9]*\).*/\1/p')"
[ -n "$el" ] && [ "$el" -lt 20 ] && ok "other jobs stopped promptly (${el}s, not 60s)" || bad "fail-fast did not stop other jobs (elapsed ${el:-?})"
[ ! -e "$tmp/backend.survived" ] && ok "the sibling job was killed" || bad "the sibling job ran to completion"
printf '%s\n' "$out" | grep -q "GOATOS_CI_ONLY_STEP='broken guard' tools/ci/run-local-ci.sh common" \
  && ok "prints the one-line single-step re-run command" || bad "no single-step re-run command"
printf '%s\n' "$out" | grep -q "tail marker line" && ok "prints the failing job's log tail" || bad "no log tail"
kill -0 "$bystander" 2>/dev/null && ok "an unrelated process is left alive" || bad "fail-fast killed a process it did not launch"
kill "$bystander" 2>/dev/null; wait "$bystander" 2>/dev/null

# opt-out keeps full-run semantics
GOATOS_CI_FAIL_FAST=0 bash -c '
  set -uo pipefail; . tools/ci/parallel-dispatch.sh
  declare -a RESULTS TIMINGS FAILED_JOBS FAILURES; fail=0; screenshots_ran=skipped
  run_job() { case "$1" in common) fail=1 ;; backend) sleep 1; echo done >"'"$tmp"'/optout" ;; esac; }
  dispatch_jobs common backend; exit "$fail"' >/dev/null 2>&1 && bad "opt-out run was GREEN"
[ -e "$tmp/optout" ] && ok "GOATOS_CI_FAIL_FAST=0 lets every job finish" || bad "opt-out still killed siblings"

[ "$rc" -eq 0 ] && echo "ci fast-retry self-test: passed" || echo "ci fast-retry self-test: FAILED" >&2
exit "$rc"
