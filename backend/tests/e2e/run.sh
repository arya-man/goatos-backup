#!/usr/bin/env bash
# Runs the vaccination "kernel story" E2E suite (backend/tests/e2e) and prints the HTML report path.
#
# This script cd's to the repo root itself, so it can be invoked from anywhere:
#   backend/tests/e2e/run.sh
#
# Equivalent manual command (run from the backend Go module):
#   go test ./tests/e2e/... -run TestKernelStor -v
#
# Requires Docker: each story boots its own throwaway Postgres container via
# backend/internal/platform/pgtest.StartPostgres (all committed goose migrations applied). If
# Docker is unavailable, the tests skip cleanly via pgtest.SkipIfNoDocker rather than failing.
#
# On success this prints the path to the generated, dependency-free HTML report at
# backend/tests/e2e/report/index.html (open directly in a browser, or host as-is on GitHub Pages).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
cd "$REPO_ROOT/backend"

go test ./tests/e2e/... -run TestKernelStor -v

REPORT="$REPO_ROOT/backend/tests/e2e/report/index.html"
if [ -f "$REPORT" ]; then
  echo ""
  echo "Kernel story report: file://$REPORT"
else
  echo "WARNING: report not found at $REPORT" >&2
  exit 1
fi
