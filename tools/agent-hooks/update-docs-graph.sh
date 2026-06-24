#!/usr/bin/env bash
# PostToolUse hook: auto-rebuild goatos-docs Graphify graph when tracked .md files change.
# Runs claude -p in background (debounced — at most once per 60s).

FILE_PATH=$(python3 -c "
import os, json
try:
    print(json.loads(os.environ.get('CLAUDE_TOOL_INPUT', '{}')).get('file_path', ''))
except Exception:
    print('')
" 2>/dev/null || echo "")

# Only fire for .md files in tracked directories
if [ -z "$FILE_PATH" ]; then
    exit 0
fi
if ! echo "$FILE_PATH" | grep -qE '^(docs|context|\.agents/skills)/.*\.md$'; then
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
