#!/usr/bin/env bash
# Cursor preToolUse (Write) -> shared write-scope guard when .agent/scope.json exists.
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO"
bash tools/agent-hooks/check-write-scope.sh
