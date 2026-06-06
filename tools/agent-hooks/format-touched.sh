#!/usr/bin/env bash
set -euo pipefail

if git rev-parse --is-inside-work-tree >/dev/null 2>&1 && command -v gofmt >/dev/null 2>&1; then
  files="$(git diff --name-only -- '*.go' && git diff --cached --name-only -- '*.go')"
  if [ -n "$files" ]; then
    echo "$files" | sort -u | xargs gofmt -w
  fi
fi
