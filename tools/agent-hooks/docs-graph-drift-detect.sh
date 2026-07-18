#!/usr/bin/env bash
# Main/worktree-aware drift detector for the goatos-docs Graphify graph.
#
# The graph is a projection of the docs on origin/main. It goes stale not only
# when someone edits a .md in THIS checkout (the only thing the old PostToolUse
# trigger noticed), but whenever origin/main advances -- via `git fetch`, a merge,
# or work landed from any other worktree. None of those touch a file in the local
# checkout, so the old trigger never fired and the graph silently rotted (it sat
# 311 commits / ~2 weeks behind, missing the 5k-50k envelope ADR while still
# carrying retired 1M-scale nodes).
#
# This detector compares the origin/main SHA the graph was built from (the basis
# stamp) against the current origin/main SHA. It is READ-ONLY with respect to the
# graph: it prints an advisory, records a pending marker, and -- unless disabled
# -- kicks a background rebuild. It never mutates graph.json itself.
#
# Wire it on SessionStart (fires once per session, in every checkout/worktree) so
# any session that opens after main moved sees the drift. GOATOS_DOCS_GRAPH_AUTOREBUILD=0
# makes it advise-only (no background rebuild).
set -uo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=/dev/null
. "$HERE/docs-graph-lib.sh"

OUT="$(dg_out 2>/dev/null)" || exit 0        # not inside the goatos repo -> silent no-op
LOG="/tmp/graphify-goatos-update.log"
TS="$(date '+%Y-%m-%d %H:%M:%S')"

CUR="$(dg_origin_main_sha 2>/dev/null || true)"
[ -n "$CUR" ] || exit 0                       # can't resolve origin/main -> nothing to compare
BASIS="$(dg_basis_sha 2>/dev/null || true)"

if [ "$BASIS" = "$CUR" ]; then
    exit 0                                     # graph reflects current main
fi

# Drift. Record it durably + visibly.
mkdir -p "$OUT"
printf '%s\n' "$CUR" > "$OUT/.needs_docs_graph_update"
{
  echo "[$TS] docs-graph DRIFT: basis=${BASIS:-<none>} origin/main=$CUR"
} >> "$LOG" 2>&1

if [ -z "$BASIS" ]; then
    echo "docs-graph: never built by the main-aware path (no basis). origin/main=$(echo "$CUR" | cut -c1-12). Run: tools/agent-hooks/rebuild-docs-graph.sh --full"
else
    N="$(git -C "$(dg_canonical_checkout)" rev-list --count "${BASIS}..${CUR}" 2>/dev/null || echo '?')"
    echo "docs-graph DRIFT: graph built from ${BASIS:0:12}, origin/main is now ${CUR:0:12} (${N} commits ahead). Rebuild with tools/agent-hooks/rebuild-docs-graph.sh"
fi

# Auto-rebuild on drift (default ON). The rebuild is locked, debounced, basis-
# stamped and self-verifying, so at most one rebuild runs per main advance and a
# failure leaves the previous good graph in place.
if [ "${GOATOS_DOCS_GRAPH_AUTOREBUILD:-1}" = "0" ]; then
    echo "docs-graph: auto-rebuild disabled (GOATOS_DOCS_GRAPH_AUTOREBUILD=0); marker holds the target SHA."
    exit 0
fi

STAMP="/tmp/graphify-goatos-last-update"
NOW=$(date +%s)
if [ -f "$STAMP" ]; then
    LAST=$(cat "$STAMP" 2>/dev/null || echo 0)
    if [ "$((NOW - LAST))" -lt 90 ]; then
        echo "[$TS] drift rebuild debounced (<90s)" >> "$LOG"
        exit 0
    fi
fi
echo "$NOW" > "$STAMP"
nohup bash "$HERE/rebuild-docs-graph.sh" >/dev/null 2>&1 &
exit 0
