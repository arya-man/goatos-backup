#!/usr/bin/env bash
# Cursor afterFileEdit -> shared post-edit format/boundary/contract/graph hooks.
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO"

bash tools/agent-hooks/format-touched.sh
bash tools/agent-hooks/check-boundaries.sh
bash tools/agent-hooks/check-contract-drift.sh

if command -v code-review-graph >/dev/null 2>&1; then
  (code-review-graph update --skip-flows --repo "$REPO" >/dev/null 2>&1 &)
fi

bash tools/agent-hooks/update-docs-graph.sh
