#!/usr/bin/env bash
# Self-test for tools/ci/workspace-guard.sh.
set -euo pipefail
guard="$(cd "$(dirname "$0")" && pwd)/workspace-guard.sh"
tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/ws/inside" "$tmp/outside"
git -C "$tmp/ws/inside" init -q; git -C "$tmp/outside" init -q
( cd "$tmp/ws/inside" && GOATOS_WORKSPACE_ROOT="$tmp/ws" bash "$guard" ) || { echo "inside clone refused" >&2; exit 1; }
if ( cd "$tmp/outside" && GOATOS_WORKSPACE_ROOT="$tmp/ws" bash "$guard" ) 2>"$tmp/err"; then
  echo "outside clone should be refused" >&2; exit 1; fi
grep -q "Goat OS work belongs under ~/mesha; this clone is at" "$tmp/err"
# prefix sibling (ws2) must not count as inside ws
mkdir -p "$tmp/ws2/x"; git -C "$tmp/ws2/x" init -q
if ( cd "$tmp/ws2/x" && GOATOS_WORKSPACE_ROOT="$tmp/ws" bash "$guard" ) 2>/dev/null; then
  echo "sibling prefix dir should be refused" >&2; exit 1; fi
( cd "$tmp/outside" && GOATOS_WORKSPACE_ROOT="$tmp/ws" GOATOS_ALLOW_OUTSIDE_WORKSPACE=1 bash "$guard" ) || { echo "override ignored" >&2; exit 1; }
echo "workspace-guard self-test: ok"
