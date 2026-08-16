#!/usr/bin/env bash
# Self-test for check-push-hook-freshness.sh.
#
# Runs the REAL guard inside a throwaway git repo with its own core.hooksPath, so
# every case is exercised end to end rather than by reasoning about the source.
# Cases: (a) match -> pass  (b) drift -> fail  (c) not installed -> fail
# (d) repo source missing -> fail  (e) this checkout -> pass.
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

# Build a sandbox repo carrying the real guard and stand-in guard sources.
mkdir -p "$sandbox/repo/tools/ci" "$sandbox/hooks"
cd "$sandbox/repo"
git init -q .
git config core.hooksPath "$sandbox/hooks"
cp "$repo/tools/ci/check-push-hook-freshness.sh" tools/ci/
# The REAL guards, not stand-in text: the guard now also EXECUTES the installed
# pre-push hook, and that hook shells out to these files. Stand-in text would
# make the hook exit non-zero from a node syntax error — i.e. "gating" for an
# incidental reason — and case (g) below could never go red.
cp "$repo/tools/ci/check-local-ci-evidence.mjs" tools/ci/
cp "$repo/tools/ci/check-stg-promotion.mjs" tools/ci/
git remote add origin git@github.com:vgoats/goatos.git
# Install a pre-push hook of exactly the shape the installer writes.
sed -n '/^cat >"\$hook" <<.HOOK.$/,/^HOOK$/p' "$repo/tools/agent-hooks/install-stg-push-guard.sh" \
  | sed '1d;$d' > "$sandbox/hooks/pre-push"
chmod +x "$sandbox/hooks/pre-push"

# (a) installed copies match the sources — MUST unset CI env vars so the real logic runs
cp tools/ci/check-local-ci-evidence.mjs "$sandbox/hooks/goatos-check-local-ci-evidence.mjs"
cp tools/ci/check-stg-promotion.mjs "$sandbox/hooks/goatos-check-stg-promotion.mjs"
env -u CI -u GITHUB_ACTIONS -u CI_ENVIRONMENT bash tools/ci/check-push-hook-freshness.sh >/dev/null 2>&1
check "(a) installed copies match" 0 $?

# (b) drift — the incident this guard exists for
printf 'DRIFTED\n' > "$sandbox/hooks/goatos-check-local-ci-evidence.mjs"
env -u CI -u GITHUB_ACTIONS -u CI_ENVIRONMENT bash tools/ci/check-push-hook-freshness.sh >/dev/null 2>&1
check "(b) stale installed copy REJECTED" 1 $?
cp tools/ci/check-local-ci-evidence.mjs "$sandbox/hooks/goatos-check-local-ci-evidence.mjs"

# (c) not installed at all — must fail closed, never skip
rm -f "$sandbox/hooks/goatos-check-local-ci-evidence.mjs"
env -u CI -u GITHUB_ACTIONS -u CI_ENVIRONMENT bash tools/ci/check-push-hook-freshness.sh >/dev/null 2>&1
check "(c) guard NOT INSTALLED fails closed" 1 $?
cp tools/ci/check-local-ci-evidence.mjs "$sandbox/hooks/goatos-check-local-ci-evidence.mjs"

# (d) repo source missing — a renamed guard must not silently pass
mv tools/ci/check-stg-promotion.mjs tools/ci/renamed.mjs
env -u CI -u GITHUB_ACTIONS -u CI_ENVIRONMENT bash tools/ci/check-push-hook-freshness.sh >/dev/null 2>&1
check "(d) missing repo source REJECTED" 1 $?
mv tools/ci/renamed.mjs tools/ci/check-stg-promotion.mjs

# (f) NO pre-push hook installed — the gate is not enforcing; must fail closed
mv "$sandbox/hooks/pre-push" "$sandbox/pre-push.parked"
env -u CI -u GITHUB_ACTIONS -u CI_ENVIRONMENT bash tools/ci/check-push-hook-freshness.sh >/dev/null 2>&1
check "(f) missing pre-push hook fails closed" 1 $?
mv "$sandbox/pre-push.parked" "$sandbox/hooks/pre-push"

# (g) THE HOLE THIS BLOCK EXISTS FOR: the .mjs copies stay byte-identical to
#     their sources, but the hook stops invoking the evidence guard. Before the
#     execution probe this printed "2 installed push guard(s) match" and exited 0
#     while `git push origin main` was completely ungated.
cp "$sandbox/hooks/pre-push" "$sandbox/pre-push.intact"
grep -v 'evidence_guard" --pre-push' "$sandbox/pre-push.intact" > "$sandbox/hooks/pre-push"
chmod +x "$sandbox/hooks/pre-push"
env -u CI -u GITHUB_ACTIONS -u CI_ENVIRONMENT bash tools/ci/check-push-hook-freshness.sh >/dev/null 2>&1
check "(g) hook that no longer invokes the evidence guard REJECTED" 1 $?
cp "$sandbox/pre-push.intact" "$sandbox/hooks/pre-push"
chmod +x "$sandbox/hooks/pre-push"

# (h) CI skip path: on CI runners, the guard must skip with exit 0
CI=true GITHUB_ACTIONS=true bash tools/ci/check-push-hook-freshness.sh >/dev/null 2>&1
check "(h) CI skip path works" 0 $?

# (e) the real checkout must currently pass (LOCAL, not CI)
cd "$repo"
env -u CI -u GITHUB_ACTIONS -u CI_ENVIRONMENT bash tools/ci/check-push-hook-freshness.sh >/dev/null 2>&1
check "(e) this checkout passes" 0 $?

if [ "$fails" -ne 0 ]; then
  echo "check-push-hook-freshness.test.sh: ${fails} case(s) FAILED" >&2
  exit 1
fi
echo "check-push-hook-freshness.test.sh: all cases ok"
