#!/usr/bin/env bash
# Auto-commit + push the Goat OS ops-console mock whenever it has changed.
#
# Standing order (Ravi, 2026-06-25): "whenever I make changes to mock always
# push, don't wait for anything." Applies to BOTH Codex and Claude.
#  - Claude runs this from a Stop hook (see ~/.claude or workspace .claude
#    settings.json).
#  - Codex / any agent: call this script after editing the mock (see AGENTS.md
#    "Mock auto-push expectation").
#
# Mock-only: it commits ONLY mock/goatos-dashboard-mock.html so it never sweeps
# unrelated in-flight work. No-ops (and no network) when the mock is clean.
set -uo pipefail

REPO="/Users/ravi/mesha/goatos"
F="mock/goatos-dashboard-mock.html"

cd "$REPO" 2>/dev/null || exit 0

# Nothing changed in the mock (unstaged + staged both clean) -> done, no network.
if git diff --quiet -- "$F" && git diff --cached --quiet -- "$F"; then
  exit 0
fi

# HARD GATE: never push a mock whose clicks are broken. A bad inline script
# kills every handler, so verify syntax + headless click-smoke first. Block on
# failure (do NOT commit/push) and print loudly so the agent fixes it.
if ! "$REPO/tools/agent-hooks/check-mock.sh"; then
  echo "[push-mock] ABORT — mock failed click validation; not committing/pushing." >&2
  exit 1
fi

git add "$F" || exit 0
git commit -q -m "chore: update ops-console mock (auto)" -- "$F" || exit 0

# Push via the profile-sourced Mesha/VGoats remote (MESHA_GITHUB_PAT). Never gh.
zsh -ic 'git mesha-push main' >/dev/null 2>&1 || true
exit 0
