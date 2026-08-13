#!/usr/bin/env bash
# Runs the weighing free-flow/isolation guard THE MOMENT a weighing file is edited.
#
# The guard already existed and was wired into `make ci-local`. That is hours downstream of the
# mistake: an identity join into goat_identifiers reached a maintainer review before any check
# ran, because nobody runs the full CI matrix mid-task. Enforcement has to sit where the decision
# is made, not at the end of the run.
#
# Cheap by design: only fires when a weighing file actually changed, and only runs one node
# script. Never blocks on unrelated edits.
set -uo pipefail
repo="${CLAUDE_PROJECT_DIR:-${CODEX_PROJECT_DIR:-}}"
[ -n "$repo" ] || repo="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
cd "$repo" 2>/dev/null || exit 0

guard="tools/agent-hooks/check-weighing-free-flow-guard.mjs"
[ -f "$guard" ] || exit 0

# Did anything under the weighing surface change (staged, unstaged, or untracked)?
changed="$(git status --porcelain -- \
  backend/internal/weighing \
  backend/migrations/postgres \
  apps/goatos-android 2>/dev/null | head -1)"
[ -n "$changed" ] || exit 0

out="$(node "$guard" 2>&1)" || {
  printf '%s\n' "$out"
  printf '\nweighing isolation guard FAILED — weighing may read ONLY weighing-owned tables (plus proof/idempotency/audit/outbox and the explicitly listed org tables). Herd/goat/vaccination tables are banned on EVERY path, reads included.\n'
  exit 2
}
exit 0
