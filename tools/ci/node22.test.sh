#!/usr/bin/env bash
# Self-test for tools/ci/node22.sh (fake node binaries) and the land-check /
# land-main fail-fast defaults.
set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."
fails=0
ok()  { echo "ok   $1"; }
bad() { echo "FAIL $1"; fails=$((fails+1)); }
T="$(mktemp -d)"; trap 'rm -rf "$T"' EXIT
mk() { mkdir -p "$T/$1"; printf '#!/bin/sh\necho %s\n' "$2" >"$T/$1/node"; chmod +x "$T/$1/node"; }
mk n20 v20.19.0; mk n22 v22.3.0; mk n24 v24.19.0

# (a) Node 20 first on PATH: the resolver swaps in >= 22 ...
got="$(PATH="$T/n20:$PATH" GOATOS_NODE22_CANDIDATES="$T/n20/node $T/n24/node" bash -c '. tools/ci/node22.sh; node22_export_or_die; command -v node; node --version')"
[ "$got" = "$T/n24/node
v24.19.0" ] && ok "Node 20 first on PATH is replaced by a >=22 candidate" || bad "resolver kept Node 20 (got: $got)"
[ "$(GOATOS_NODE22_CANDIDATES="$T/missing $T/n22/node" bash -c '. tools/ci/node22.sh; node22_resolve' 2>/dev/null)" = "$T/n22/node" ] && ok "22 is accepted, missing candidates skipped" || bad "22 not accepted"
# ... or fails loudly before anything runs.
out="$(PATH="$T/n20:$PATH" GOATOS_NODE22_CANDIDATES="$T/n20/node" bash -c '. tools/ci/node22.sh; node22_export_or_die; echo started' 2>&1)"
case "$out" in *started*) bad "export_or_die let CI start on Node 20" ;; *"Node >= 22"*) ok "only Node 20 anywhere fails loudly" ;; *) bad "no clear message: $out" ;; esac
# Default candidate order: nvm v24, Homebrew, then PATH.
order="$(env -u GOATOS_NODE22_CANDIDATES HOME="$T/home" PATH="$T/n20:/usr/bin:/bin" bash -c 'mkdir -p "$HOME/.nvm/versions/node/v24.1.0/bin"; . tools/ci/node22.sh; node22_candidates' | tr '\n' ' ')"
case "$order" in "$T/home/.nvm/versions/node/v24.1.0/bin/node /opt/homebrew/bin/node /usr/local/bin/node $T/n20/node ") ok "candidate order nvm v24 > Homebrew > PATH" ;; *) bad "candidate order: $order" ;; esac
# Wiring: every entrypoint that spawns node on its own enforces it.
for f in tools/ci/run-local-ci.sh tools/ci/land-main.sh tools/ci/land-check.sh; do
  grep -q '^node22_export_or_die' "$f" && ok "$f enforces Node >= 22" || bad "$f does not call node22_export_or_die"
done

# (b) land-check defaults to run-everything; land-main stays fail-fast.
. tools/ci/parallel-dispatch.sh 2>/dev/null || true
lc_default="$(env -u GOATOS_CI_FAIL_FAST bash -c "$(sed -n '/^export GOATOS_CI_FAIL_FAST=/p' tools/ci/land-check.sh); printf %s \"\$GOATOS_CI_FAIL_FAST\"")"
[ "$lc_default" = 0 ] && ok "land-check default is GOATOS_CI_FAIL_FAST=0 (run all jobs)" || bad "land-check default is '$lc_default'"
lc_override="$(GOATOS_CI_FAIL_FAST=1 bash -c "$(sed -n '/^export GOATOS_CI_FAIL_FAST=/p' tools/ci/land-check.sh); printf %s \"\$GOATOS_CI_FAIL_FAST\"")"
[ "$lc_override" = 1 ] && ok "GOATOS_CI_FAIL_FAST=1 still overrides land-check" || bad "override lost ('$lc_override')"
if grep -q 'GOATOS_CI_FAIL_FAST=' tools/ci/land-main.sh; then bad "land-main.sh sets GOATOS_CI_FAIL_FAST (must keep fail-fast default)"; else ok "land-main.sh leaves fail-fast default"; fi
( unset GOATOS_CI_FAIL_FAST; ci_fail_fast_enabled ) && ok "dispatcher default (land-main) is fail-fast" || bad "dispatcher default is not fail-fast"
( GOATOS_CI_FAIL_FAST=0; ci_fail_fast_enabled ) && bad "FAIL_FAST=0 still fail-fast" || ok "FAIL_FAST=0 disables fail-fast"
grep -q 'GOATOS_CI_RERUN_FILE' tools/ci/run-local-ci.sh && ok "RED summary lists a re-run line per failing step" || bad "no per-step re-run list"

[ "$fails" -eq 0 ] && echo "node22 test: PASS" || { echo "node22 test: $fails failure(s)"; exit 1; }
