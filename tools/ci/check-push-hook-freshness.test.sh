#!/usr/bin/env bash
# Self-test for check-push-hook-freshness.sh, the admin-web push gate and the skip ledger.
#
# Runs the REAL guard inside a throwaway git repo with its own core.hooksPath, so every case is
# exercised end to end rather than by reasoning about the source.
#   (a) match -> pass            (b) drifted copy -> fail       (c) copy not installed -> fail
#   (d) repo source missing      (f) no pre-push hook           (g) hook stops calling evidence guard
#   (i) hook stops calling the admin-web push gate              (j) push gate drops a lane
#   (k) visual-gate skip flag without GOATOS_SKIP_REASON is refused; with one it is ledgered
#   (l) a feature push that changes no admin-web input is "not applicable"
#   (m) a feature push that changes admin-web input from a tree without the app is BLOCKED
#   (n) GOATOS_ADMIN_WEB_BASE_URL without a reason is refused
#   (h) CI runner skip path      (e) the sources of this checkout pass
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

# repo source -> installed name (must match check-push-hook-freshness.sh)
PAIRS="tools/ci/check-local-ci-evidence.mjs:goatos-check-local-ci-evidence.mjs
tools/ci/check-stg-promotion.mjs:goatos-check-stg-promotion.mjs
tools/ci/step-input-digest.mjs:step-input-digest.mjs
tools/ci/admin-web-visual-gate.sh:goatos-admin-web-visual-gate.sh
tools/ci/admin-web-push-gate.sh:goatos-admin-web-push-gate.sh
tools/ci/goatos-skip-ledger.sh:goatos-skip-ledger.sh
tools/agent-hooks/pre-push.hook:pre-push"

install_all() { # hooks_dir
  while IFS=: read -r src dst; do cp "$src" "$1/$dst"; done <<EOF
$PAIRS
EOF
  chmod +x "$1"/*
}

mkdir -p "$sandbox/repo/tools/ci" "$sandbox/repo/tools/agent-hooks" "$sandbox/hooks"
cd "$sandbox/repo"
git init -q -b main .
git config user.email guard@local
git config user.name guard
git config commit.gpgsign false
git config core.hooksPath "$sandbox/hooks"
cp "$repo/tools/ci/check-push-hook-freshness.sh" tools/ci/
while IFS=: read -r src _; do cp "$repo/$src" "$src"; done <<EOF
$PAIRS
EOF
git remote add origin git@github.com:vgoats/goatos.git
install_all "$sandbox/hooks"

# (a) installed copies match the sources
fresh; check "(a) installed copies match" 0 $?

# (b) drift — the incident this guard exists for
printf 'DRIFTED\n' > "$sandbox/hooks/goatos-check-local-ci-evidence.mjs"
fresh; check "(b) stale installed copy REJECTED" 1 $?
install_all "$sandbox/hooks"

# (c) not installed at all — must fail closed, never skip
rm -f "$sandbox/hooks/goatos-admin-web-push-gate.sh"
fresh; check "(c) push gate NOT INSTALLED fails closed" 1 $?
install_all "$sandbox/hooks"

# (d) repo source missing — a renamed guard must not silently pass
mv tools/ci/check-stg-promotion.mjs tools/ci/renamed.mjs
fresh; check "(d) missing repo source REJECTED" 1 $?
mv tools/ci/renamed.mjs tools/ci/check-stg-promotion.mjs

# (f) NO pre-push hook installed
mv "$sandbox/hooks/pre-push" "$sandbox/pre-push.parked"
fresh; check "(f) missing pre-push hook fails closed" 1 $?
mv "$sandbox/pre-push.parked" "$sandbox/hooks/pre-push"

# (g) the hook stops invoking the evidence guard — even when the repo template is edited the same
#     way (so the cmp passes), the main probe must go red.
cp tools/agent-hooks/pre-push.hook "$sandbox/hook.intact"
grep -v 'evidence_guard" --pre-push' "$sandbox/hook.intact" > tools/agent-hooks/pre-push.hook
install_all "$sandbox/hooks"
fresh; check "(g) hook that no longer invokes the evidence guard REJECTED" 1 $?
cp "$sandbox/hook.intact" tools/agent-hooks/pre-push.hook

# (i) the hook stops invoking the admin-web push gate (feature-branch probe must go red)
grep -v 'push_gate" --pre-push' "$sandbox/hook.intact" > tools/agent-hooks/pre-push.hook
install_all "$sandbox/hooks"
fresh; check "(i) hook that no longer runs the admin-web push gate REJECTED" 1 $?
grep -q 'DOES NOT RUN THE ADMIN-WEB LANES' "$sandbox/fresh.log" && echo "ok    (i) names the feature-branch hole" \
  || { echo "FAIL  (i) did not name the feature-branch hole" >&2; fails=$((fails + 1)); }
cp "$sandbox/hook.intact" tools/agent-hooks/pre-push.hook
install_all "$sandbox/hooks"

# (j) the push gate drops a lane (source and installed copy identical, so only the lane check sees it)
cp tools/ci/admin-web-push-gate.sh "$sandbox/gate.intact"
sed 's/^LANES=(design-guard typecheck unit-tests next-build visual-gate)$/LANES=(design-guard typecheck next-build visual-gate)/' "$sandbox/gate.intact" > tools/ci/admin-web-push-gate.sh
install_all "$sandbox/hooks"
fresh; check "(j) push gate missing the unit-tests lane REJECTED" 1 $?
grep -q "MISSING LANE 'unit-tests'" "$sandbox/fresh.log" && echo "ok    (j) names the missing lane" \
  || { echo "FAIL  (j) did not name the missing lane" >&2; fails=$((fails + 1)); }
cp "$sandbox/gate.intact" tools/ci/admin-web-push-gate.sh
install_all "$sandbox/hooks"
fresh; check "(a2) restored install passes again" 0 $?

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

# (l) a feature push with no admin-web change is not applicable
echo seed > seed.txt && git add -A >/dev/null && git commit -qm base
head="$(git rev-parse HEAD)"
printf 'refs/heads/feature %s refs/heads/feature 0000000000000000000000000000000000000000\n' "$head" \
  | GOATOS_SKIP_LEDGER="$ledger" bash "$sandbox/hooks/goatos-admin-web-push-gate.sh" --pre-push >"$sandbox/l.log" 2>&1
rc=$?
# no origin/main in the sandbox => no merge base => treated as touching everything => BLOCKED.
# Give it a base so the "not applicable" branch is what runs.
git update-ref refs/remotes/origin/main "$head"
echo more >> seed.txt && git commit -qam more
head="$(git rev-parse HEAD)"
printf 'refs/heads/feature %s refs/heads/feature 0000000000000000000000000000000000000000\n' "$head" \
  | GOATOS_SKIP_LEDGER="$ledger" bash "$sandbox/hooks/goatos-admin-web-push-gate.sh" --pre-push >"$sandbox/l.log" 2>&1
check "(l) feature push without admin-web changes passes" 0 $?
grep -q 'not applicable' "$sandbox/l.log" && echo "ok    (l) says not applicable" || { echo "FAIL  (l) silent pass" >&2; fails=$((fails + 1)); }
[ "$rc" = 1 ] && echo "ok    (l0) no merge base counts as touching admin-web (BLOCKED)" || { echo "FAIL  (l0) no-merge-base push passed (rc=$rc)" >&2; fails=$((fails + 1)); }

# (m) a feature push that changes admin-web input without the app present is BLOCKED (fail closed)
mkdir -p apps/admin-web && echo 'export const x = 1;' > apps/admin-web/x.tsx && git add -A >/dev/null && git commit -qm aw
head="$(git rev-parse HEAD)"
printf 'refs/heads/feature %s refs/heads/feature 0000000000000000000000000000000000000000\n' "$head" \
  | GOATOS_SKIP_LEDGER="$ledger" bash "$sandbox/hooks/goatos-admin-web-push-gate.sh" --pre-push >"$sandbox/m.log" 2>&1
check "(m) admin-web change without package.json BLOCKED" 1 $?
grep -q 'admin-web-push-gate: BLOCKED' "$sandbox/m.log" && echo "ok    (m) says BLOCKED" || { echo "FAIL  (m) no BLOCKED line" >&2; fails=$((fails + 1)); }
# ...and pushing an admin-web commit that is not HEAD is refused (the lanes judge the work tree)
echo n > note.txt && git add -A >/dev/null && git commit -qm note
printf 'refs/heads/feature %s refs/heads/feature 0000000000000000000000000000000000000000\n' "$(git rev-parse HEAD~1)" \
  | GOATOS_SKIP_LEDGER="$ledger" bash "$sandbox/hooks/goatos-admin-web-push-gate.sh" --pre-push >"$sandbox/m2.log" 2>&1
check "(m2) pushing an admin-web commit that is not HEAD BLOCKED" 1 $?
grep -q "but this work tree's HEAD is" "$sandbox/m2.log" && echo "ok    (m2) names the HEAD mismatch" || { echo "FAIL  (m2) wrong refusal" >&2; fails=$((fails + 1)); }

# (h) CI skip path: on CI runners, the guard must skip with exit 0
CI=true GITHUB_ACTIONS=true bash tools/ci/check-push-hook-freshness.sh >/dev/null 2>&1
check "(h) CI skip path works" 0 $?

# (e) the sources of THIS checkout, installed fresh, pass the guard
if [ "${GITHUB_ACTIONS:-}" = true ] && [ "${RUNNER_ENVIRONMENT:-}" = github-hosted ]; then
  echo "SKIP  (e) this checkout passes (fresh GitHub checkout has no installed hooks; sandbox test preferred)"
else
  e_test_dir="$sandbox/checkout-test"
  mkdir -p "$e_test_dir/tools/ci" "$e_test_dir/tools/agent-hooks" "$sandbox/hooks-e"
  cd "$e_test_dir"
  git init -q .
  git config core.hooksPath "$sandbox/hooks-e"
  cp "$repo/tools/ci/check-push-hook-freshness.sh" tools/ci/
  while IFS=: read -r src _; do cp "$repo/$src" "$src"; done <<EOF
$PAIRS
EOF
  git remote add origin git@github.com:vgoats/goatos.git
  install_all "$sandbox/hooks-e"
  fresh; check "(e) this checkout passes" 0 $?
  cd "$repo"
fi

if [ "$fails" -ne 0 ]; then
  echo "check-push-hook-freshness.test.sh: ${fails} case(s) FAILED" >&2
  exit 1
fi
echo "check-push-hook-freshness.test.sh: all cases ok"
