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


def add_path(paths, path):
    path = normalize(path)
    if re.match(r"^(docs|context|\.agents/skills)/.*\.md$", path):
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

# Debounce: skip if last update ran within 60 seconds
STAMP="/tmp/graphify-goatos-last-update"
NOW=$(date +%s)
if [ -f "$STAMP" ]; then
    LAST=$(cat "$STAMP" 2>/dev/null || echo 0)
    DIFF=$((NOW - LAST))
    if [ "$DIFF" -lt 60 ]; then
        exit 0
    fi
fi
echo "$NOW" > "$STAMP"

# Run graphify skill update in a new background Claude session
nohup bash -c "
  cd /Users/ravi/mesha/goatos
  claude -p '/graphify docs context .agents/skills/goatos-build/references --update' \
    >> /tmp/graphify-goatos-update.log 2>&1
" &

exit 0
