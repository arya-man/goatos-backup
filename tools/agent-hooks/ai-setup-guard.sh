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
#   REPOWISE_SETUP=0                   drop the repowise requirement (CRG/Graphify/RTK only)
#   touch <git-dir>/goatos-ai-setup-attempted-v2   suppress on this clone
set -u
[ "${GOATOS_AI_SETUP_GUARD:-1}" = "0" ] && exit 0

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$HERE/../.." && pwd)"

# Already set up? Two cores must both be present:
#   CRG      — binary installed AND this clone's graph DB dir exists.
#   repowise — binary installed AND this clone's index dir exists (opt out with
#              REPOWISE_SETUP=0, e.g. a machine that only wants CRG/Graphify/RTK).
crg_ok=0; repowise_ok=0
if command -v code-review-graph >/dev/null 2>&1 && [ -d "$REPO/.code-review-graph" ]; then crg_ok=1; fi
if [ "${REPOWISE_SETUP:-1}" = "0" ] || { command -v repowise >/dev/null 2>&1 && [ -d "$REPO/.repowise" ]; }; then repowise_ok=1; fi
if [ "$crg_ok" = 1 ] && [ "$repowise_ok" = 1 ]; then
    exit 0
fi

# Marker lives in the git dir (works for worktrees, never committed). The -v2
# suffix re-prompts already-set-up clones once so they pick up the repowise
# layer added to the stack; after one `make ai-setup` the guard is silent again.
GIT_DIR="$(git -C "$REPO" rev-parse --absolute-git-dir 2>/dev/null || printf '%s/.git' "$REPO")"
MARKER="$GIT_DIR/goatos-ai-setup-attempted-v2"

INPUT="$(cat 2>/dev/null || true)"

# Let the setup work itself through, and record that setup was attempted so a
# failed install (offline machine, no brew) cannot deadlock the session.
case "$INPUT" in
    *ai-setup*|*ai-doctor*|*ai-rebuild*|*code-review-graph*|*graphify*|*repowise*|*"brew install rtk"*|*"uv tool install"*|*"pipx install"*|*"pip install"*)
        : > "$MARKER" 2>/dev/null || true
        exit 0
        ;;
esac

# Setup was already attempted on this clone — do not nag again.
[ -f "$MARKER" ] && exit 0

cat >&2 <<'MSG'
AI-SETUP GUARD: this clone has not run `make ai-setup` (with the repowise layer)
yet, so the committed token-saving stack is inactive or incomplete:
  code-review-graph (structure/nav) · Graphify (docs) · RTK (diffs) · repowise
  (code-health / defect-risk / refactor / dead-code — quality layer).

Do this FIRST, then resume the user's original task:
  1. make ai-setup          # installs CRG/Graphify/RTK + repowise, builds indexes
  2. make ai-rebuild-docs   # optional now; needs an AI backend key
  3. Re-run the tool call that was just blocked and continue the task.

repowise stays repo-scoped (index in .repowise/, project-only registration) — it
never writes global config, so it cannot mix with another org's checkout.

If this machine cannot install tools (offline/CI), suppress this guard with:
  touch "$(git rev-parse --absolute-git-dir)/goatos-ai-setup-attempted-v2"
or set GOATOS_AI_SETUP_GUARD=0 (or REPOWISE_SETUP=0 to drop only repowise). This
guard blocks until setup is run or suppressed; setup commands themselves pass.
MSG
exit 2
