#!/usr/bin/env bash
# PreToolUse guard (Claude AND Codex): fresh-clone AI-stack bootstrap gate.
#
# Purpose: on a clone that has NEVER run `make ai-setup`, block the first piece
# of real agent work with a clear instruction to run setup, so the agent runs
# `make ai-setup` first and THEN resumes the user's actual prompt. Once the
# stack is installed (or setup was attempted once on this clone) the guard is
# silent forever.
#
# Portability: repo-relative only — no absolute maintainer paths (ai-doctor
# enforces this). Escape hatches:
#   GOATOS_AI_SETUP_GUARD=0            disable entirely
#   touch <git-dir>/goatos-ai-setup-attempted   suppress on this clone
set -u
[ "${GOATOS_AI_SETUP_GUARD:-1}" = "0" ] && exit 0

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$HERE/../.." && pwd)"

# Already set up? Core = CRG binary installed AND this clone's graph DB dir
# exists (created by `code-review-graph install/build` during ai-setup).
if command -v code-review-graph >/dev/null 2>&1 && [ -d "$REPO/.code-review-graph" ]; then
    exit 0
fi

# Marker lives in the git dir (works for worktrees, never committed).
GIT_DIR="$(git -C "$REPO" rev-parse --absolute-git-dir 2>/dev/null || printf '%s/.git' "$REPO")"
MARKER="$GIT_DIR/goatos-ai-setup-attempted"

INPUT="$(cat 2>/dev/null || true)"

# Let the setup work itself through, and record that setup was attempted so a
# failed install (offline machine, no brew) cannot deadlock the session.
case "$INPUT" in
    *ai-setup*|*ai-doctor*|*ai-rebuild*|*code-review-graph*|*graphify*|*"brew install rtk"*|*"uv tool install"*|*"pipx install"*|*"pip install"*)
        : > "$MARKER" 2>/dev/null || true
        exit 0
        ;;
esac

# Setup was already attempted on this clone — do not nag again.
[ -f "$MARKER" ] && exit 0

cat >&2 <<'MSG'
AI-SETUP GUARD: this clone has not run `make ai-setup` yet, so the committed
token-saving stack (code-review-graph, Graphify docs graph, RTK) is inactive.

Do this FIRST, then resume the user's original task:
  1. make ai-setup          # installs CRG/Graphify/RTK, builds the code graph
  2. make ai-rebuild-docs   # optional now; needs an AI backend key
  3. Re-run the tool call that was just blocked and continue the task.

If this machine cannot install tools (offline/CI), suppress this guard with:
  touch "$(git rev-parse --absolute-git-dir)/goatos-ai-setup-attempted"
or set GOATOS_AI_SETUP_GUARD=0. This guard blocks until setup is run or
suppressed; setup commands themselves always pass.
MSG
exit 2
