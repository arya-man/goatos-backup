#!/usr/bin/env bash
# Single source of truth for the goatos-docs Graphify corpus.
#
# Corpus = every *.md on origin/main, plus the one schema .html god-node, MINUS
# generated/vendored/build noise. It is read from a clean, detached worktree
# pinned to origin/main (dg_ensure_main_worktree) -- NOT from the live working
# tree of whatever branch/worktree invokes this. That guarantees the corpus is
# main-aware and untracked-file-free: an in-progress feature branch, another
# worktree's scratch .md, or a stale 311-commit-behind checkout can no longer
# change what the graph indexes.
#
# Both the drift detector and the rebuild source this so "the docs on main" means
# the same set everywhere.
#
# Usage:
#   goatos-docs-corpus.sh                     # print absolute paths in the main worktree
#   goatos-docs-corpus.sh --root              # print the main-worktree root only
#   goatos-docs-corpus.sh --matches REL_PATH  # exit 0 if REL_PATH (main-relative) is in corpus
set -uo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=/dev/null
. "$HERE/docs-graph-lib.sh"

# Directories never indexed (generated output, deps, build artifacts, VCS).
PRUNE=(node_modules .git .next dist build vendor graphify-out .codex-goatos-render)

# A caller mid-rebuild pins the already-prepared worktree (DOCS_GRAPH_PINNED_WT)
# so we do NOT re-reset it: if origin/main advances between the rebuild's ensure
# and this call, a second reset would jump the tree ahead of the SHA the rebuild
# captured, silently indexing newer content than the basis stamp records.
if [ -n "${DOCS_GRAPH_PINNED_WT:-}" ] && [ -d "${DOCS_GRAPH_PINNED_WT}/.git" -o -f "${DOCS_GRAPH_PINNED_WT}/.git" ]; then
    MAIN_WT="$DOCS_GRAPH_PINNED_WT"
else
    MAIN_WT="$(dg_ensure_main_worktree)" || {
        echo "docs-corpus: could not resolve clean origin/main worktree" >&2
        exit 2
    }
fi

if [ "${1:-}" = "--root" ]; then
    printf '%s\n' "$MAIN_WT"
    exit 0
fi

_corpus() {
    local prune_expr=() first=1 d
    for d in "${PRUNE[@]}"; do
        if [ $first -eq 1 ]; then first=0; else prune_expr+=( -o ); fi
        prune_expr+=( -path "*/$d/*" )
    done
    find "$MAIN_WT" -type f -name '*.md' -not \( "${prune_expr[@]}" \)
    [ -f "$MAIN_WT/docs/schema-and-system-design.html" ] && echo "$MAIN_WT/docs/schema-and-system-design.html"
}

if [ "${1:-}" = "--matches" ]; then
    # $2 is main-relative (or absolute in the main worktree); normalize to absolute.
    target="$2"
    case "$target" in
        /*) : ;;
        *) target="$MAIN_WT/$target" ;;
    esac
    # Materialize first: piping into `grep -q` closes the pipe early and, under
    # `set -o pipefail`, find's SIGPIPE (141) would masquerade as "no match".
    grep -qxF "$target" <<<"$(_corpus)"
    exit $?
fi

_corpus
