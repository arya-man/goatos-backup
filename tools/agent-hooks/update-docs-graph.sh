#!/usr/bin/env bash
# PostToolUse hook: keep the goatos-docs Graphify graph in sync with origin/main.
#
# The graph is a projection of the docs on origin/main, so the meaningful "it went
# stale" signal is origin/main advancing -- not a .md being edited in the local
# working tree (which is not on main yet, and previously also pulled in untracked
# scratch files). This hook therefore just runs the main-aware drift detector:
# after a session lands docs to main (push/merge), the next Write/Edit tool call
# notices the new origin/main SHA and (unless disabled) kicks a debounced rebuild.
#
# Consumes and ignores any hook payload on stdin (Claude passes tool input via env,
# Codex on stdin); it needs neither because it compares SHAs, not file paths.
cat >/dev/null 2>&1 || true
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec bash "$HERE/docs-graph-drift-detect.sh"
