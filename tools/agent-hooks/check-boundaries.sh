#!/usr/bin/env bash
set -euo pipefail

fail=0

if command -v rg >/dev/null 2>&1; then
  if rg -n "xox[baprs]-[0-9A-Za-z-]{20,}|firebase-debug\\.log|BEGIN PRIVATE KEY|PRIVATE KEY-----" . \
    --glob '!node_modules/**' \
    --glob '!.git/**' \
    --glob '!docs/archive/**' \
    --glob '!context/**' \
    --glob '!tools/agent-hooks/check-boundaries.sh'; then
    echo "Secret/log pattern found. Rotate/remove before commit."
    fail=1
  fi

  if rg -n "from ['\"]@google-cloud/bigquery|new BigQuery\\(" apps packages \
    --glob '!node_modules/**' \
    --glob '!apps/admin-web/lib/bigquery.ts' \
    --glob '!apps/investor-web-shadow/lib/bigquery.ts' >/tmp/goatos-boundary-warnings 2>/dev/null; then
    cat /tmp/goatos-boundary-warnings
    echo "Direct BigQuery access outside legacy adapter allowlist."
    fail=1
  fi

  if rg -n "goat_identity_counters|GoatIdentityCounter" backend/internal/identity \
    --glob '!**/adapters/postgres/sqlc/schema.sql' \
    --glob '!**/adapters/postgres/sqlc/models.go' >/tmp/goatos-reporting-boundary-warnings 2>/dev/null; then
    cat /tmp/goatos-reporting-boundary-warnings
    echo "Identity package must not own reporting counter projection queries."
    fail=1
  fi
fi

exit "$fail"
