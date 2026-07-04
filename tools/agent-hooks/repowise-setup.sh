#!/usr/bin/env bash
# repowise-setup.sh — install + index repowise for THIS goatos checkout, org-safe.
#
# repowise is the code-QUALITY layer of the local AI stack: code-health,
# defect-risk, refactoring plans, dead-code, and architectural decisions. It
# COMPLEMENTS — does not replace — the rest of the stack. Route queries by
# strength: navigation/callers/impact -> CRG (code-review-graph); docs/"why" ->
# Graphify; diff/review display -> RTK; quality/risk/refactor/dead-code -> repowise.
#
# ORG SAFETY (critical, see AGENTS.md "Organization Boundary Rule"):
#   `repowise init` mutates GLOBAL config by default — ~/.claude/settings.json,
#   the Claude Desktop config, and VS Code global settings. On a machine that
#   also has another org's repo checked out (e.g. a Heva repo pointed at its own
#   repowise index) that global write would CLOBBER the other org's registration
#   and silently mix the two. This wrapper snapshots and RESTORES those global
#   files, so setup leaves ONLY repo-relative artifacts:
#       <repo>/.repowise   index/caches (gitignored)
#       <repo>/.mcp.json   project-scoped repowise MCP (gitignored)
#       <git-dir>/hooks/post-commit   repo-local auto-update on commit
#   Nothing global or cross-org is touched.
#
# Portability: repo-relative only — no maintainer paths (ai-doctor Rule 1).
# Escape hatches:
#   REPOWISE_SETUP=0            skip repowise entirely (CRG/Graphify/RTK still run)
#   REPOWISE_GLOBAL_REGISTER=1  keep repowise's global registration (opt-in; only
#                               safe on a single-org machine)
set -uo pipefail

[ "${REPOWISE_SETUP:-1}" = "0" ] && { echo "repowise-setup: skipped (REPOWISE_SETUP=0)"; exit 0; }

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$HERE/../.." && pwd)"
cd "$REPO" || exit 1

# 1. Install repowise if missing (same fallback ladder as CRG/Graphify).
if ! command -v repowise >/dev/null 2>&1; then
    echo "repowise-setup: installing repowise..."
    if command -v uv >/dev/null 2>&1; then uv tool install repowise
    elif command -v pipx >/dev/null 2>&1; then pipx install repowise
    else python3 -m pip install --user repowise; fi
fi
if ! command -v repowise >/dev/null 2>&1; then
    echo "repowise-setup: repowise unavailable (offline / no installer) — skipping, non-fatal."
    exit 0
fi

# 2. Snapshot global config so init's global writes can be reverted (org-safe).
CLAUDE_GLOBAL="$HOME/.claude/settings.json"
DESKTOP_CFG="$HOME/Library/Application Support/Claude/claude_desktop_config.json"
SNAP="$(mktemp -d)"
snapshot_one() { # $1=src file  $2=snap basename
    if [ -f "$1" ]; then cp "$1" "$SNAP/$2.json"; else : > "$SNAP/$2.absent"; fi
}
restore_one() { # $1=dst file  $2=snap basename
    if [ -f "$SNAP/$2.json" ]; then cp "$SNAP/$2.json" "$1" 2>/dev/null || true
    elif [ -e "$SNAP/$2.absent" ]; then rm -f "$1" 2>/dev/null || true
    fi
}
restore_global() {
    if [ "${REPOWISE_GLOBAL_REGISTER:-0}" != "1" ]; then
        restore_one "$CLAUDE_GLOBAL" claude
        restore_one "$DESKTOP_CFG" desktop
        echo "repowise-setup: global config restored (repo-relative registration only)."
    fi
    rm -rf "$SNAP" 2>/dev/null || true
}
trap restore_global EXIT
if [ "${REPOWISE_GLOBAL_REGISTER:-0}" != "1" ]; then
    snapshot_one "$CLAUDE_GLOBAL" claude
    snapshot_one "$DESKTOP_CFG" desktop
fi

# 3. Build the index (deterministic layers only — no LLM, no network, no codex/
#    agents/claude-md side-writes). Wiki docs are opt-in later via `repowise init`
#    with an LLM key; the dashboard + all query tools work without them.
echo "repowise-setup: building index (graph/git/health/dead-code/decisions, no LLM)..."
repowise init --index-only --no-codex --no-agents --no-claude-md -y \
    || echo "repowise-setup: init returned nonzero (continuing; check 'repowise init' manually)."

# 4. Install a repo-local post-commit auto-update hook (append-safe, marker-guarded).
GIT_DIR="$(git rev-parse --git-dir 2>/dev/null || printf '.git')"
HOOK="$GIT_DIR/hooks/post-commit"
MARK="repowise-auto-update"
mkdir -p "$GIT_DIR/hooks"
if [ ! -f "$HOOK" ]; then
    cat > "$HOOK" <<'EOF'
#!/usr/bin/env sh
# repowise-auto-update: refresh the local repowise index in the background on commit.
command -v repowise >/dev/null 2>&1 || exit 0
( cd "$(git rev-parse --show-toplevel 2>/dev/null || pwd)" && nohup repowise update >/dev/null 2>&1 & )
exit 0
EOF
    chmod +x "$HOOK"
    echo "repowise-setup: installed post-commit auto-update hook."
elif ! grep -q "$MARK" "$HOOK" 2>/dev/null; then
    {
        printf '\n# %s: refresh local repowise index in background on commit.\n' "$MARK"
        printf 'command -v repowise >/dev/null 2>&1 && ( cd "$(git rev-parse --show-toplevel 2>/dev/null || pwd)" && nohup repowise update >/dev/null 2>&1 & )\n'
    } >> "$HOOK"
    chmod +x "$HOOK"
    echo "repowise-setup: appended repowise auto-update to existing post-commit hook."
fi

echo "repowise-setup: done."
echo "  index         : .repowise/ (gitignored, local-only)"
echo "  auto-update   : on commit (post-commit hook) + on edit (agent PostToolUse)"
echo "  dashboard     : repowise serve   ->  http://localhost:3000"
echo "  query tools   : repowise health | risk | dead-code | decisions"
