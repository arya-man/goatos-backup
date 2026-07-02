#!/usr/bin/env bash
# Cursor preToolUse (Shell) -> shared RTK noisy-command hook.
set -u

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$HERE/../.." && pwd)"

INPUT="$(cat 2>/dev/null || true)"
NORMALIZED="$(printf '%s' "$INPUT" | python3 "$HERE/normalize-input.py")"
printf '%s' "$NORMALIZED" | bash "$REPO/tools/agent-hooks/pre-rtk-noisy-commands.sh"
exit $?
