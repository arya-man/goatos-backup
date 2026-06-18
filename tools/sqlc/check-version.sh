#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
pinned_version="$(tr -d '[:space:]' < "$repo_root/tools/sqlc/sqlc.version")"
sqlc_bin="${SQLC:-sqlc}"

if ! command -v "$sqlc_bin" >/dev/null 2>&1; then
  echo "sqlc is not installed. Install $pinned_version and ensure it is on PATH or set SQLC=/path/to/sqlc." >&2
  exit 1
fi

installed_version="$("$sqlc_bin" version | tr -d '[:space:]')"
if [ "$installed_version" != "$pinned_version" ]; then
  echo "sqlc version mismatch: installed $installed_version, pinned $pinned_version" >&2
  exit 1
fi
