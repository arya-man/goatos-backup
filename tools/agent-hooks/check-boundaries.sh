#!/usr/bin/env bash
set -euo pipefail

fail=0
admin_web_data_pattern="from ['\"]@google-cloud/bigquery|new BigQuery\\(|googleapis|sheets\\.spreadsheets|spreadsheets\\.values|script\\.google\\.com|script\\.googleusercontent\\.com|docs\\.google\\.com/spreadsheets|/spreadsheets/d/|export\\?format=csv|output=csv|gviz/tq|from ['\"](xlsx|exceljs)['\"]|require\\(['\"](xlsx|exceljs)['\"]\\)|XLSX\\."

if [ "${1:-}" = "--self-test" ]; then
  if ! command -v rg >/dev/null 2>&1; then
    echo "rg is required for boundary self-test."
    exit 1
  fi
  tmpdir="$(mktemp -d "${TMPDIR:-/tmp}/goatos-boundary-self-test.XXXXXX")"
  trap 'rm -rf "$tmpdir"' EXIT
  mkdir -p "$tmpdir/apps/admin-web/lib"
  cat >"$tmpdir/apps/admin-web/lib/direct-sheet.ts" <<'EOF'
import { google } from "googleapis";
import * as XLSX from "xlsx";

export const url = "https://docs.google.com/spreadsheets/d/example/export?format=csv";
void google.sheets;
void XLSX;
EOF
  if rg -n "$admin_web_data_pattern" "$tmpdir/apps/admin-web/lib" >/dev/null 2>&1; then
    echo "Boundary self-test passed: synthetic admin-web direct data access was detected."
    exit 0
  fi
  echo "Boundary self-test failed: synthetic admin-web direct data access was not detected."
  exit 1
fi

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
    --glob '!apps/investor-web-shadow/lib/bigquery.ts' >/tmp/goatos-boundary-warnings 2>/dev/null; then
    cat /tmp/goatos-boundary-warnings
    echo "Direct BigQuery access outside legacy adapter allowlist."
    fail=1
  fi

  admin_web_code=(
    "apps/admin-web/app"
    "apps/admin-web/components"
    "apps/admin-web/lib"
  )
  if [ -d "apps/admin-web" ]; then
    if rg -n "$admin_web_data_pattern" "${admin_web_code[@]}" \
      --glob '!node_modules/**' >/tmp/goatos-admin-web-data-boundary-warnings 2>/dev/null; then
      cat /tmp/goatos-admin-web-data-boundary-warnings
      echo "Admin web must use Goat OS backend APIs only; direct Sheets/App Script/BigQuery/XLSX data access is forbidden."
      fail=1
    fi
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
