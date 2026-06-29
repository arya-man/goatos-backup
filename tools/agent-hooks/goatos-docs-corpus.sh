#!/usr/bin/env bash
# Single source of truth for the goatos-docs Graphify corpus.
#
# Prints the absolute path of every file that belongs in the graph, one per
# line. Both the change-trigger (update-docs-graph.sh) and the rebuild
# (rebuild-docs-graph.sh) source this so "any goatos .md" means the same set in
# both places.
#
# Corpus = every *.md in the repo, plus the one schema .html that is already a
# graph god-node, MINUS generated/vendored/build noise.
#
# Usage:
#   goatos-docs-corpus.sh            # print all corpus files
#   goatos-docs-corpus.sh --matches REL_PATH   # exit 0 if REL_PATH is in corpus, else 1

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

# Directories never indexed (generated output, deps, build artifacts, VCS).
PRUNE=(node_modules .git .next dist build vendor graphify-out .codex-goatos-render)

_corpus() {
    local prune_expr=()
    local first=1
    for d in "${PRUNE[@]}"; do
        if [ $first -eq 1 ]; then first=0; else prune_expr+=( -o ); fi
        prune_expr+=( -path "*/$d/*" )
    done
    # all .md, minus pruned dirs
    find "$REPO" -type f -name '*.md' -not \( "${prune_expr[@]}" \)
    # the one schema html that is an existing graph node
    [ -f "$REPO/docs/schema-and-system-design.html" ] && echo "$REPO/docs/schema-and-system-design.html"
}

if [ "${1:-}" = "--matches" ]; then
    # $2 may be absolute or repo-relative; normalize to absolute.
    target="$2"
    case "$target" in
        /*) : ;;
        *) target="$REPO/$target" ;;
    esac
    _corpus | grep -qxF "$target"
    exit $?
fi

_corpus
