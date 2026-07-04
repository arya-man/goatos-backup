#!/usr/bin/env bash
# repowise-coverage.sh — generate Go test coverage and ingest it into the local
# repowise index, so the dashboard Coverage tab (risk x coverage map, untested-
# hotspot warnings, per-module breakdown) is populated. Free, no LLM.
#
# Why it is a SEPARATE opt-in target (not part of `make ai-setup`): it runs the
# backend test suite, which is slow and — for the DB-backed integration tests —
# needs the local dev database up (`make dev-local` / proxy on :55432). A fresh
# clone should bootstrap fast, so coverage is a follow-up you run once the test
# env is available. Partial coverage still ingests: packages whose tests fail or
# skip simply contribute nothing, the rest are counted.
#
# Portability: repo-relative only, no maintainer paths (ai-doctor Rule 1).
# Escape hatch: REPOWISE_COVERAGE=0 skips entirely.
set -uo pipefail

[ "${REPOWISE_COVERAGE:-1}" = "0" ] && { echo "repowise-coverage: skipped (REPOWISE_COVERAGE=0)"; exit 0; }

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$HERE/../.." && pwd)"
cd "$REPO" || exit 1

command -v repowise >/dev/null 2>&1 || { echo "repowise-coverage: repowise not installed — run 'make ai-setup' first. Skipping."; exit 0; }
[ -d "$REPO/.repowise" ] || { echo "repowise-coverage: no .repowise index — run 'make ai-setup' first. Skipping."; exit 0; }
command -v go >/dev/null 2>&1 || { echo "repowise-coverage: go not installed — skipping."; exit 0; }
[ -f "$REPO/backend/go.mod" ] || { echo "repowise-coverage: backend/go.mod not found — skipping."; exit 0; }

# Coverage artifacts live under .repowise/ (already gitignored, never committed).
OUT_DIR="$REPO/.repowise/coverage"
mkdir -p "$OUT_DIR"
PROFILE="$OUT_DIR/cover.out"
LCOV="$OUT_DIR/cover.lcov"

# Ensure the Go cover.out -> lcov converter is available (repowise ingests
# lcov / cobertura / clover, not Go's native profile format).
GCOV="$(go env GOPATH)/bin/gcov2lcov"
if [ ! -x "$GCOV" ]; then
    echo "repowise-coverage: installing gcov2lcov..."
    go install github.com/jandelgado/gcov2lcov@latest || { echo "repowise-coverage: gcov2lcov install failed — skipping."; exit 0; }
fi
[ -x "$GCOV" ] || { echo "repowise-coverage: gcov2lcov unavailable — skipping."; exit 0; }

echo "repowise-coverage: running backend test suite with coverage (best-effort; DB-backed tests need :55432)..."
( cd "$REPO/backend" && go test ./... -coverprofile="$PROFILE" -covermode=atomic -timeout "${REPOWISE_COVERAGE_TIMEOUT:-300s}" ) \
    || echo "repowise-coverage: some tests failed/skipped (expected without full dev DB/seed) — ingesting partial coverage."

if [ ! -s "$PROFILE" ]; then
    echo "repowise-coverage: no coverage profile produced — skipping ingest."
    exit 0
fi

# gcov2lcov resolves package->file paths via go modules, so run it FROM the
# module root (backend/). It emits repo-root-relative paths (backend/internal/...)
# which already match repowise's file-node paths — no path rewrite needed.
( cd "$REPO/backend" && "$GCOV" -infile "$PROFILE" -outfile "$LCOV" 2>/dev/null ) \
    || { echo "repowise-coverage: lcov conversion failed — skipping."; exit 0; }

echo "repowise-coverage: ingesting $(grep -c '^SF:' "$LCOV" 2>/dev/null || echo '?') files into repowise..."
repowise health "$REPO" --coverage "$LCOV" --coverage-format lcov >/dev/null 2>&1 \
    && echo "repowise-coverage: done — Coverage tab populated (repowise serve -> Code Health -> Coverage)." \
    || echo "repowise-coverage: ingest returned nonzero — check 'repowise health --coverage $LCOV'."
