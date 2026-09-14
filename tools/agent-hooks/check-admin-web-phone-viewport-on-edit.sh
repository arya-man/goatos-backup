#!/usr/bin/env bash
# Runs the admin-web phone-viewport guard THE MOMENT an admin-web CSS/TSX file is edited
# (maintainer rule 2026-09-14: the dashboard is opened on phones; nothing may clip or break at
# 390px). The guard is also in `make ci-local`, but that is hours downstream of the edit that
# introduced a 640px box -- enforcement sits where the decision is made.
#
# Cheap by design: only fires when an admin-web .css/.tsx changed (staged, unstaged, untracked),
# and runs one node script. Never blocks on unrelated edits.
set -uo pipefail
repo="${CLAUDE_PROJECT_DIR:-${CODEX_PROJECT_DIR:-}}"
[ -n "$repo" ] || repo="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
cd "$repo" 2>/dev/null || exit 0

guard="tools/agent-hooks/check-admin-web-phone-viewport.mjs"
[ -f "$guard" ] || exit 0

changed="$(git status --porcelain -- apps/admin-web 2>/dev/null | grep -E '\.(css|tsx)$' | head -1)"
[ -n "$changed" ] || exit 0

out="$(node "$guard" 2>&1)" || {
  printf '%s\n' "$out"
  printf '\nadmin-web phone-viewport guard FAILED -- the dashboard is opened on phones. Make the box fluid (minmax(0,1fr), a max-width phone media query, or an overflow-x:auto wrapper on the table/chart); do not grow the baseline. See docs/decisions/admin-web-phone-viewport.md.\n'
  exit 2
}
exit 0
