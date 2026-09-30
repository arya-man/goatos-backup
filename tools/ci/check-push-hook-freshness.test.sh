#!/usr/bin/env bash
# Self-test for check-push-hook-freshness.sh, the shared pre-push shim, the admin-web push gate,
# the skip ledger and the push-gate receipts.
#
# Runs the REAL guard inside a throwaway git repo with its own core.hooksPath, so every case is
# exercised end to end (the guard itself makes REAL test pushes through the installed shim).
#   (a) shim installed -> pass       (b) drifted shim -> fail       (c) a COPIED branch hook -> fail
#   (d) repo source missing          (f) no pre-push hook           (g) branch hook stops calling the evidence guard
#   (i) branch hook stops calling the admin-web push gate           (j) push gate drops a lane
#   (s) legacy snapshot missing      (t) installer: shim installed, foreign hook chained, old copies removed
#   (k) visual-gate skip flag without GOATOS_SKIP_REASON is refused; with one it is ledgered
#   (l) a feature push that changes no admin-web input is "not applicable" AND leaves a receipt
#   (m) a feature push that changes admin-web input from a tree without the app is BLOCKED
#   (n) GOATOS_ADMIN_WEB_BASE_URL without a reason is refused
#   (r) receipts: a skipped lane needs a reason; check-range refuses an uncovered commit, accepts a
#       covered one and a rebased (patch-identical) copy
#   (h) CI runner skip path          (e) the sources of this checkout pass
set -uo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
sandbox="$(mktemp -d "${TMPDIR:-/tmp}/goatos-hookfresh.XXXXXX")"
trap 'rm -rf "$sandbox"' EXIT

fails=0
check() { # label, expected_exit, actual_exit
  if [ "$2" = "$3" ]; then
    echo "ok    $1 (exit=$3)"
  else
    echo "FAIL  $1 — expected exit=$2, got exit=$3" >&2
    fails=$((fails + 1))
  fi
}
fresh() { env -u CI -u GITHUB_ACTIONS -u CI_ENVIRONMENT bash tools/ci/check-push-hook-freshness.sh >"$sandbox/fresh.log" 2>&1; }
expect_log() { # label, regex
  if grep -q "$2" "$sandbox/fresh.log"; then echo "ok    $1"; else echo "FAIL  $1" >&2; sed 's/^/      /' "$sandbox/fresh.log" | tail -12 >&2; fails=$((fails + 1)); fi
}

# repo files the guard / shim / installer need in the sandbox repo
SOURCES="tools/agent-hooks/pre-push.shim tools/agent-hooks/pre-push.hook tools/agent-hooks/pre-push.bundle
tools/agent-hooks/install-stg-push-guard.sh tools/ci/crg-worktree-hook.sh
tools/ci/check-local-ci-evidence.mjs tools/ci/check-stg-promotion.mjs tools/ci/step-input-digest.mjs
tools/ci/admin-web-visual-gate.sh tools/ci/admin-web-push-gate.sh tools/ci/goatos-skip-ledger.sh tools/ci/admin-web-push-receipt.mjs"

install_all() { # hooks_dir — what the installer lays down: the shim + legacy-fallback snapshots
  cp tools/agent-hooks/pre-push.shim "$1/pre-push"
  cp tools/ci/check-stg-promotion.mjs "$1/goatos-check-stg-promotion.mjs"
  cp tools/ci/check-local-ci-evidence.mjs "$1/goatos-check-local-ci-evidence.mjs"
  cp tools/ci/step-input-digest.mjs "$1/step-input-digest.mjs"
  chmod +x "$1"/*
}
setup_repo() { # dir hooks_dir
  mkdir -p "$1/tools/ci" "$1/tools/agent-hooks" "$2"
  cd "$1" || exit 1
  git init -q -b main .
  git config user.email guard@local
  git config user.name guard
  git config commit.gpgsign false
  git config core.hooksPath "$2"
  cp "$repo/tools/ci/check-push-hook-freshness.sh" tools/ci/
  for src in $SOURCES; do cp "$repo/$src" "$src"; done
  git remote add origin git@github.com:vgoats/goatos.git
  install_all "$2"
}

export GOATOS_PUSH_RECEIPT_PUBLISH=0 GOATOS_PUSH_RECEIPT_DIR="$sandbox/receipts"
unset GOATOS_PUSH_SHIM_ACTIVE GOATOS_SKIP_ADMIN_WEB_VISUAL_GATE GOATOS_SKIP_REASON
setup_repo "$sandbox/repo" "$sandbox/hooks"

# (a) the shim is installed
fresh; check "(a) shim installed" 0 $?

# (b) drifted shim
printf '\n# DRIFTED\n' >> "$sandbox/hooks/pre-push"
fresh; check "(b) drifted shim REJECTED" 1 $?
install_all "$sandbox/hooks"

# (c) the incident: a COPIED branch hook in the shared hooks dir (pre-shim installer)
cp tools/agent-hooks/pre-push.hook "$sandbox/hooks/pre-push"
fresh; check "(c) copied branch hook instead of the shim REJECTED" 1 $?
expect_log "(c) names the copied hook" 'COPIED branch hook'
install_all "$sandbox/hooks"

# (d) repo source missing — a renamed guard must not silently pass
mv tools/ci/check-stg-promotion.mjs tools/ci/renamed.mjs
fresh; check "(d) missing repo source REJECTED" 1 $?
mv tools/ci/renamed.mjs tools/ci/check-stg-promotion.mjs

# (f) NO pre-push hook installed
mv "$sandbox/hooks/pre-push" "$sandbox/pre-push.parked"
fresh; check "(f) missing pre-push hook fails closed" 1 $?
mv "$sandbox/pre-push.parked" "$sandbox/hooks/pre-push"

# (s) a legacy-fallback snapshot missing
rm -f "$sandbox/hooks/goatos-check-local-ci-evidence.mjs"
fresh; check "(s) missing legacy snapshot REJECTED" 1 $?
install_all "$sandbox/hooks"

# (g) the branch hook stops invoking the evidence guard -> the real main push goes through -> red
cp tools/agent-hooks/pre-push.hook "$sandbox/hook.intact"
grep -v 'evidence_guard" --pre-push' "$sandbox/hook.intact" > tools/agent-hooks/pre-push.hook
fresh; check "(g) branch hook that no longer invokes the evidence guard REJECTED" 1 $?
expect_log "(g) names the main hole" 'does not gate main'
cp "$sandbox/hook.intact" tools/agent-hooks/pre-push.hook

# (i) the branch hook stops invoking the admin-web push gate (feature push must go red)
grep -v 'push_gate" --pre-push' "$sandbox/hook.intact" > tools/agent-hooks/pre-push.hook
fresh; check "(i) branch hook that no longer runs the admin-web push gate REJECTED" 1 $?
expect_log "(i) names the feature-branch hole" 'DOES NOT RUN THE ADMIN-WEB LANES'
cp "$sandbox/hook.intact" tools/agent-hooks/pre-push.hook

# (j) the push gate drops a lane
cp tools/ci/admin-web-push-gate.sh "$sandbox/gate.intact"
sed 's/^LANES=(design-guard typecheck unit-tests next-build visual-gate storybook-build)$/LANES=(design-guard typecheck next-build visual-gate storybook-build)/' "$sandbox/gate.intact" > tools/ci/admin-web-push-gate.sh
fresh; check "(j) push gate missing the unit-tests lane REJECTED" 1 $?
expect_log "(j) names the missing lane" "MISSING LANE 'unit-tests'"
cp "$sandbox/gate.intact" tools/ci/admin-web-push-gate.sh
fresh; check "(a2) restored install passes again" 0 $?

# (t) the installer: installs the shim, chains a foreign hook once, removes pre-shim copies; and a
#     shim that an older installer chained as ITS prior is dropped when the shim goes back in front
tdir="$sandbox/hooks-t"; mkdir -p "$tdir"
printf '#!/bin/sh\necho foreign\n' > "$tdir/pre-push"; : > "$tdir/goatos-admin-web-push-gate.sh"
git config core.hooksPath "$tdir"
bash tools/agent-hooks/install-stg-push-guard.sh >/dev/null 2>&1; check "(t) installer runs" 0 $?
if cmp -s tools/agent-hooks/pre-push.shim "$tdir/pre-push" && grep -q foreign "$tdir/pre-push.before-goatos-stg-guard" \
  && [ ! -e "$tdir/goatos-admin-web-push-gate.sh" ] && [ -f "$tdir/goatos-check-local-ci-evidence.mjs" ]; then
  echo "ok    (t) shim installed, foreign hook chained, old copy removed, snapshots present"
else echo "FAIL  (t) installer layout wrong" >&2; fails=$((fails + 1)); fi
cp tools/agent-hooks/pre-push.shim "$tdir/pre-push.before-goatos-stg-guard"; cp "$sandbox/hook.intact" "$tdir/pre-push"
bash tools/agent-hooks/install-stg-push-guard.sh >/dev/null 2>&1
if cmp -s tools/agent-hooks/pre-push.shim "$tdir/pre-push" && [ ! -e "$tdir/pre-push.before-goatos-stg-guard" ]; then
  echo "ok    (t) re-install over an old copied hook drops the chained shim"
else echo "FAIL  (t) re-install layout wrong" >&2; fails=$((fails + 1)); fi
git config core.hooksPath "$sandbox/hooks"

# (k) skip flag without a reason is refused; with a reason it is recorded in the ledger
ledger="$sandbox/ledger.tsv"
GOATOS_SKIP_LEDGER="$ledger" GOATOS_SKIP_ADMIN_WEB_VISUAL_GATE=1 bash tools/ci/admin-web-visual-gate.sh --fast >/dev/null 2>&1
check "(k) GOATOS_SKIP_ADMIN_WEB_VISUAL_GATE without a reason REFUSED" 1 $?
GOATOS_SKIP_LEDGER="$ledger" GOATOS_SKIP_ADMIN_WEB_VISUAL_GATE=1 GOATOS_SKIP_REASON="ok" bash tools/ci/admin-web-visual-gate.sh --fast >/dev/null 2>&1
check "(k) a too-short reason REFUSED" 1 $?
GOATOS_SKIP_LEDGER="$ledger" GOATOS_SKIP_ADMIN_WEB_VISUAL_GATE=1 GOATOS_SKIP_REASON="local API down during outage" \
  bash tools/ci/admin-web-visual-gate.sh --fast >/dev/null 2>&1
check "(k) skip with a written reason allowed" 0 $?
grep -q $'\tGOATOS_SKIP_ADMIN_WEB_VISUAL_GATE\tadmin-web-visual-gate\t.*\tlocal API down during outage$' "$ledger" 2>/dev/null \
  && echo "ok    (k) skip recorded in the ledger" || { echo "FAIL  (k) skip not recorded in the ledger" >&2; fails=$((fails + 1)); }

# (n) auditing an already-running app without a reason is refused (it may be stale)
mkdir -p apps/admin-web/scripts && echo '// stub' > apps/admin-web/scripts/r2-visual-audit.mjs
GOATOS_SKIP_LEDGER="$ledger" GOATOS_ADMIN_WEB_BASE_URL=http://127.0.0.1:9 bash tools/ci/admin-web-visual-gate.sh --fast >/dev/null 2>&1
check "(n) GOATOS_ADMIN_WEB_BASE_URL without a reason REFUSED" 1 $?
rm -rf apps

# (l) a feature push with no admin-web change is not applicable, and still leaves a receipt
echo seed > seed.txt && git add -A >/dev/null && git commit -qm base
head="$(git rev-parse HEAD)"
printf 'refs/heads/feature %s refs/heads/feature 0000000000000000000000000000000000000000\n' "$head" \
  | GOATOS_SKIP_LEDGER="$ledger" bash tools/ci/admin-web-push-gate.sh --pre-push >"$sandbox/l.log" 2>&1
rc=$?
# no origin/main in the sandbox => no merge base => treated as touching everything => BLOCKED.
# Give it a base so the "not applicable" branch is what runs.
git update-ref refs/remotes/origin/main "$head"
echo more >> seed.txt && git commit -qam more
head="$(git rev-parse HEAD)"
printf 'refs/heads/feature %s refs/heads/feature 0000000000000000000000000000000000000000\n' "$head" \
  | GOATOS_SKIP_LEDGER="$ledger" bash tools/ci/admin-web-push-gate.sh --pre-push >"$sandbox/l.log" 2>&1
check "(l) feature push without admin-web changes passes" 0 $?
grep -q 'not applicable' "$sandbox/l.log" && echo "ok    (l) says not applicable" || { echo "FAIL  (l) silent pass" >&2; fails=$((fails + 1)); }
[ "$rc" = 1 ] && echo "ok    (l0) no merge base counts as touching admin-web (BLOCKED)" || { echo "FAIL  (l0) no-merge-base push passed (rc=$rc)" >&2; fails=$((fails + 1)); }
[ -f "$sandbox/receipts/by-commit/$head" ] && echo "ok    (l) the not-applicable push left a receipt" \
  || { echo "FAIL  (l) no receipt for the not-applicable push" >&2; fails=$((fails + 1)); }
node tools/ci/admin-web-push-receipt.mjs check-range --base "$head~1" --head "$head" >/dev/null 2>&1
check "(r) check-range accepts a commit a push covered" 0 $?

# (m) a feature push that changes admin-web input without the app present is BLOCKED (fail closed)
mkdir -p apps/admin-web && echo 'export const x = 1;' > apps/admin-web/x.tsx && git add -A >/dev/null && git commit -qm aw
head="$(git rev-parse HEAD)"
printf 'refs/heads/feature %s refs/heads/feature 0000000000000000000000000000000000000000\n' "$head" \
  | GOATOS_SKIP_LEDGER="$ledger" bash tools/ci/admin-web-push-gate.sh --pre-push >"$sandbox/m.log" 2>&1
check "(m) admin-web change without package.json BLOCKED" 1 $?
grep -q 'admin-web-push-gate: BLOCKED' "$sandbox/m.log" && echo "ok    (m) says BLOCKED" || { echo "FAIL  (m) no BLOCKED line" >&2; fails=$((fails + 1)); }
[ ! -f "$sandbox/receipts/by-commit/$head" ] && echo "ok    (m) a refused push writes no receipt" \
  || { echo "FAIL  (m) a refused push wrote a receipt" >&2; fails=$((fails + 1)); }
# ...and pushing an admin-web commit that is not HEAD is refused (the lanes judge the work tree)
echo n > note.txt && git add -A >/dev/null && git commit -qm note
printf 'refs/heads/feature %s refs/heads/feature 0000000000000000000000000000000000000000\n' "$(git rev-parse HEAD~1)" \
  | GOATOS_SKIP_LEDGER="$ledger" bash tools/ci/admin-web-push-gate.sh --pre-push >"$sandbox/m2.log" 2>&1
check "(m2) pushing an admin-web commit that is not HEAD BLOCKED" 1 $?
grep -q "but this work tree's HEAD is" "$sandbox/m2.log" && echo "ok    (m2) names the HEAD mismatch" || { echo "FAIL  (m2) wrong refusal" >&2; fails=$((fails + 1)); }

# (r) receipts: a skip needs a reason; an uncovered commit is refused; a rebased copy is covered
rt=tools/ci/admin-web-push-receipt.mjs
node "$rt" --self-test >/dev/null 2>&1; check "(r) receipt self-test" 0 $?
printf 'design-guard\tpass\t3\t\nvisual-gate\tskip\t0\t\n' > "$sandbox/res-bad.tsv"
node "$rt" write --sha "$(git rev-parse HEAD)" --ref refs/heads/feature --results "$sandbox/res-bad.tsv" >/dev/null 2>&1
check "(r) a skipped lane without a reason is REFUSED by the receipt" 1 $?
printf 'design-guard\tpass\t3\t\nvisual-gate\tskip\t0\tlocal API down during the outage\n' > "$sandbox/res-ok.tsv"
rfile="$(node "$rt" write --sha "$(git rev-parse HEAD)" --ref refs/heads/feature --remote-sha "$(git rev-parse HEAD~1)" --results "$sandbox/res-ok.tsv" 2>/dev/null)"
if grep -q '"reason": "local API down during the outage"' "$rfile" 2>/dev/null && grep -q '"state": "failure"' "$rfile"; then
  echo "ok    (r) the receipt carries the skip reason and a failure verdict"
else echo "FAIL  (r) skip reason not in the receipt" >&2; fails=$((fails + 1)); fi
covered="$(git rev-parse HEAD)"
echo unreceipted > u.txt && git add u.txt && git commit -qm unreceipted
node "$rt" check-range --base HEAD~1 --head HEAD >"$sandbox/cr.log" 2>&1
check "(r) check-range REFUSES a commit no receipt covers" 1 $?
grep -q 'have NO gate receipt' "$sandbox/cr.log" && echo "ok    (r) names the uncovered commit" || { echo "FAIL  (r) no refusal text" >&2; fails=$((fails + 1)); }
git reset -q --hard HEAD~1
git checkout -q -b rebased "$covered~2" && git cherry-pick "$covered" >/dev/null 2>&1
node "$rt" check-range --base HEAD~1 --head HEAD >/dev/null 2>&1
check "(r) a rebased copy of a covered commit is covered by patch-id" 0 $?
git checkout -q main

# (h) CI skip path: on CI runners, the guard must skip with exit 0
CI=true GITHUB_ACTIONS=true bash tools/ci/check-push-hook-freshness.sh >/dev/null 2>&1
check "(h) CI skip path works" 0 $?

# (e) the sources of THIS checkout, installed fresh, pass the guard
if [ "${GITHUB_ACTIONS:-}" = true ] && [ "${RUNNER_ENVIRONMENT:-}" = github-hosted ]; then
  echo "SKIP  (e) this checkout passes (fresh GitHub checkout has no installed hooks; sandbox test preferred)"
else
  setup_repo "$sandbox/checkout-test" "$sandbox/hooks-e"
  fresh; check "(e) this checkout passes" 0 $?
  cd "$repo"
fi

if [ "$fails" -ne 0 ]; then
  echo "check-push-hook-freshness.test.sh: ${fails} case(s) FAILED" >&2
  exit 1
fi
echo "check-push-hook-freshness.test.sh: all cases ok"
