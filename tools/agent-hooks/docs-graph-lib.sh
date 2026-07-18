#!/usr/bin/env bash
# Shared helpers for the goatos-docs Graphify graph tooling.
#
# The docs graph is a projection of the docs *as they exist on origin/main*, not
# of whatever branch/worktree happens to hold the checkout. Historically the
# corpus was `find $checkout -name '*.md'` over the live (possibly stale, possibly
# dirty, untracked-polluted) working tree, and the only refresh trigger was an
# in-checkout .md edit. That made the graph blind to `git fetch` / main advancing
# / edits made in other worktrees, and let a 311-commit-stale feature branch drive
# the corpus. This library centralizes the main-aware, worktree-independent
# resolution so corpus.sh, the rebuild, and the drift detector all agree.
#
# source-only: `source "$(dirname "$0")/docs-graph-lib.sh"`
set -uo pipefail

# Directory of this library, captured at source time. Everything resolves relative
# to the SCRIPT location, never the caller's cwd -- ai-doctor invokes corpus.sh
# from /tmp, and the hooks fire from arbitrary worktrees, so cwd is not a reliable
# anchor for "which repo".
DG_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# The canonical checkout that owns the committed graph outputs. The scripts live in
# <checkout>/tools/agent-hooks/, so <lib>/../.. is the checkout they belong to; from
# there git-common-dir points at the main repo's .git whose parent is the main
# checkout (/Users/ravi/mesha/goatos). This is cwd-independent by construction.
dg_canonical_checkout() {
    local here common
    here="$(cd "$DG_LIB_DIR/../.." && pwd)" || return 1
    common="$(git -C "$here" rev-parse --path-format=absolute --git-common-dir 2>/dev/null)" \
        || { printf '%s\n' "$here"; return 0; }
    dirname "$common"
}

# graphify-out lives in the canonical checkout, never in a linked worktree, so
# every session reads/writes the same graph regardless of where it runs.
dg_out() {
    local canon
    canon="$(dg_canonical_checkout)" || return 1
    printf '%s/graphify-out\n' "$canon"
}

# The origin/main commit the graph should reflect. Best-effort fetch first (no
# creds -> stays on the last-known ref, which is still main-aware, just not
# freshly fetched).
dg_origin_main_sha() {
    local canon
    canon="$(dg_canonical_checkout)" || return 1
    git -C "$canon" fetch --quiet origin main 2>/dev/null || true
    git -C "$canon" rev-parse origin/main 2>/dev/null
}

# The origin/main SHA the current graph was actually built from (stamp written by
# the assembler). Empty if never built by the main-aware path.
dg_basis_sha() {
    local out
    out="$(dg_out)" || return 1
    cat "$out/.docs_graph_basis" 2>/dev/null | tr -d '[:space:]'
}

# A clean, detached worktree pinned to origin/main, used as the corpus basis. This
# is "the clean current main checkout" the graph is built from; it never carries
# untracked files, feature-branch drift, or another agent's work-in-progress.
# Prints the worktree path on stdout.
dg_ensure_main_worktree() {
    local canon wt sha
    canon="$(dg_canonical_checkout)" || return 1
    wt="${GOATOS_DOCS_GRAPH_MAIN_TREE:-$HOME/.cache/goatos-docs-graph/main-tree}"
    sha="$(git -C "$canon" rev-parse origin/main 2>/dev/null)" || return 1
    if [ ! -e "$wt/.git" ]; then
        mkdir -p "$(dirname "$wt")"
        git -C "$canon" worktree prune 2>/dev/null || true
        git -C "$canon" worktree add --force --detach "$wt" "$sha" >/dev/null 2>&1 || return 1
    else
        # Reset the managed tree hard to the target; discard anything local.
        git -C "$wt" reset --hard "$sha" >/dev/null 2>&1 || return 1
        git -C "$wt" clean -fdq >/dev/null 2>&1 || true
    fi
    printf '%s\n' "$wt"
}

# Resolve a python interpreter that can `import graphify`.
dg_graphify_python() {
    local out py
    out="$(dg_out 2>/dev/null || true)"
    py="$(cat "$out/.graphify_python" 2>/dev/null || true)"
    if [ -n "$py" ] && [ -x "$py" ] && "$py" -c "import graphify" >/dev/null 2>&1; then
        printf '%s\n' "$py"; return 0
    fi
    if command -v uv >/dev/null 2>&1; then
        py="$(uv tool run graphifyy python -c "import sys; print(sys.executable)" 2>/dev/null || true)"
        if [ -n "$py" ] && [ -x "$py" ] && "$py" -c "import graphify" >/dev/null 2>&1; then
            [ -n "$out" ] && printf '%s' "$py" > "$out/.graphify_python"
            printf '%s\n' "$py"; return 0
        fi
    fi
    if command -v graphify >/dev/null 2>&1; then
        py="$(head -1 "$(command -v graphify)" | sed 's/^#!//')"
        if [ -n "$py" ] && [ -x "$py" ] && "$py" -c "import graphify" >/dev/null 2>&1; then
            [ -n "$out" ] && printf '%s' "$py" > "$out/.graphify_python"
            printf '%s\n' "$py"; return 0
        fi
    fi
    if python3 -c "import graphify" >/dev/null 2>&1; then
        printf 'python3\n'; return 0
    fi
    return 1
}
