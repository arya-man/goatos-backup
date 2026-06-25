#!/usr/bin/env bash
# PostToolUse hook: auto-rebuild goatos-docs Graphify graph when tracked .md files change.
# Runs claude -p in background (debounced: at most once per 60s).

HOOK_INPUT_FILE="$(mktemp "${TMPDIR:-/tmp}/goatos-docs-graph-hook.XXXXXX")"
trap 'rm -f "$HOOK_INPUT_FILE"' EXIT
cat >"$HOOK_INPUT_FILE" 2>/dev/null || true

CHANGED_MD_PATHS=$(HOOK_INPUT_FILE="$HOOK_INPUT_FILE" python3 - <<'PY' 2>/dev/null || true
import json
import os
import re
from pathlib import Path


def normalize(path):
    path = (path or "").strip()
    if not path:
        return ""
    cwd = os.getcwd()
    if os.path.isabs(path):
        try:
            path = os.path.relpath(path, cwd)
        except ValueError:
            return ""
    return path.removeprefix("./")


_PRUNE = {"node_modules", ".git", ".next", "dist", "build", "vendor",
          "graphify-out", ".codex-goatos-render"}


def add_path(paths, path):
    # Any goatos .md counts, except generated/vendored/build noise. Mirrors
    # tools/agent-hooks/goatos-docs-corpus.sh (the rebuild's corpus definition).
    path = normalize(path)
    if not path.endswith(".md"):
        return
    if _PRUNE & set(path.split("/")):
        return
    paths.add(path)


paths = set()

# Claude Code exposes tool input in an environment variable.
try:
    claude_input = json.loads(os.environ.get("CLAUDE_TOOL_INPUT", "{}"))
except Exception:
    claude_input = {}
if isinstance(claude_input, dict):
    add_path(paths, claude_input.get("file_path"))

# Codex sends one hook JSON object on stdin. For apply_patch, file paths are in
# the patch command headers.
try:
    stdin_payload = Path(os.environ["HOOK_INPUT_FILE"]).read_text()
    codex_input = json.loads(stdin_payload) if stdin_payload.strip() else {}
except Exception:
    codex_input = {}
if isinstance(codex_input, dict):
    tool_input = codex_input.get("tool_input")
    if isinstance(tool_input, dict):
        add_path(paths, tool_input.get("file_path"))
        command = tool_input.get("command")
        if isinstance(command, str):
            for line in command.splitlines():
                match = re.match(r"\*\*\* (?:Add|Update|Delete) File: (.+)$", line)
                if match:
                    add_path(paths, match.group(1))

for path in sorted(paths):
    print(path)
PY
)

if [ -z "$CHANGED_MD_PATHS" ]; then
    exit 0
fi

REPO="/Users/ravi/mesha/goatos"
LOG="/tmp/graphify-goatos-update.log"
MARKER="$REPO/graphify-out/.needs_docs_graph_update"
TS="$(date '+%Y-%m-%d %H:%M:%S')"

# Always record that doc-graph-relevant files changed. Observable (so a missed
# update is never silent) and durable (the marker survives until a successful
# rebuild clears it).
{
  echo "[$TS] doc graph stale - changed:"
  echo "$CHANGED_MD_PATHS" | sed 's/^/    /'
} >> "$LOG" 2>&1

mkdir -p "$REPO/graphify-out"
{ [ -f "$MARKER" ] && cat "$MARKER"; echo "$CHANGED_MD_PATHS"; } 2>/dev/null \
  | sort -u > "$MARKER.tmp" && mv "$MARKER.tmp" "$MARKER"

# Auto-rebuild is ON by default (set GOATOS_DOCS_GRAPH_AUTOREBUILD=0 to disable
# and stay flag-only). Unlike CRG's deterministic AST `update`, doc extraction
# needs an LLM, so the rebuild runs rebuild-docs-graph.sh: it re-extracts ONLY
# the changed files, merges, re-clusters, and SELF-VERIFIES - on any failure or
# regression it restores the previous graph and keeps this marker, so a flaky
# headless run can never silently corrupt the graph (the old failure mode).
if [ "${GOATOS_DOCS_GRAPH_AUTOREBUILD:-1}" = "0" ]; then
    echo "[$TS] flag-only (auto-rebuild disabled). Run rebuild-docs-graph.sh or the interactive /graphify update to clear $MARKER" >> "$LOG"
    exit 0
fi

# Debounce: at most one background rebuild per 90s (a wave of edits coalesces
# into one rebuild that picks up every changed file via the manifest hash diff).
STAMP="/tmp/graphify-goatos-last-update"
NOW=$(date +%s)
if [ -f "$STAMP" ]; then
    LAST=$(cat "$STAMP" 2>/dev/null || echo 0)
    if [ "$((NOW - LAST))" -lt 90 ]; then
        echo "[$TS] debounced (<90s); marker holds changed files for the next rebuild" >> "$LOG"
        exit 0
    fi
fi
echo "$NOW" > "$STAMP"

HOOK_DIR="$(cd "$(dirname "$0")" && pwd)"
nohup bash "$HOOK_DIR/rebuild-docs-graph.sh" >/dev/null 2>&1 &

exit 0
