#!/usr/bin/env bash
set -euo pipefail

fail=0
admin_web_data_pattern="from ['\"]@google-cloud/bigquery|new BigQuery\\(|googleapis|sheets\\.spreadsheets|spreadsheets\\.values|script\\.google\\.com|script\\.googleusercontent\\.com|docs\\.google\\.com/spreadsheets|/spreadsheets/d/|export\\?format=csv|output=csv|gviz/tq|from ['\"](xlsx|exceljs)['\"]|require\\(['\"](xlsx|exceljs)['\"]\\)|XLSX\\."
admin_web_feature_deep_import_pattern="(from|import\\() ['\"](@/features/[^'\"]+/[^'\"]+|(\\.\\.?/)+features/[^'\"]+/[^'\"]+)['\"]"
admin_web_visible_branding_pattern="goat[[:space:]-]*os|vgoat"
admin_web_debug_footer_pattern="Trace [A-Za-z0-9{]|Rendered [A-Za-z0-9{]"

check_admin_feature_relative_imports() {
  local root="${1:-apps/admin-web/features}"
  local relative_pattern="(from|import\\() ['\"](\\.\\./)+[^/'\"]+/[^'\"]+['\"]"
  [ -d "$root" ] || return 0

  while IFS= read -r file; do
    local current_feature
    current_feature="${file#"$root"/}"
    current_feature="${current_feature%%/*}"
    while IFS= read -r line; do
      local target_feature
      target_feature="$(printf '%s\n' "$line" | sed -E "s/.*(from|import\\() ['\"](\\.\\.\\/)+([^/'\"]+)(\\/[^'\"]+)?['\"].*/\\3/")"
      if [ -n "$target_feature" ] \
        && [ "$target_feature" != "$current_feature" ] \
        && [ -d "$root/$target_feature" ]; then
        echo "$file:$line"
        fail=1
      fi
    done < <(grep -nE "$relative_pattern" "$file" 2>/dev/null || true)
  done < <(find "$root" \( -name '*.ts' -o -name '*.tsx' -o -name '*.js' -o -name '*.jsx' \) 2>/dev/null)
}

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
  else
    echo "Boundary self-test failed: synthetic admin-web direct data access was not detected."
    exit 1
  fi

  # Adversarial tests for the Firebase-auth exemption. The exemption is BY URL,
  # not by file: only the `securetoken.googleapis.com` host is stripped, and the
  # auth file is STILL scanned for every other prohibited data-access pattern.
  # Regression guards: (a) a securetoken-only line is exempt, (b) a forbidden
  # Sheets URL added INSIDE the auth file is still caught (the old whole-file
  # path allowlist false green), (c) a line MIXING the auth host with a real
  # Sheets URL is still caught (the older per-line host-filter false green).
  mkdir -p "$tmpdir/apps/admin-web/lib/auth"
  auth_refresh_scan() {
    # Mirror production: scan the auth file, strip only the exempt securetoken
    # host, then re-test the remaining content against the data pattern.
    rg -n "$admin_web_data_pattern" "$1" 2>/dev/null \
      | sed -E 's#securetoken\.googleapis\.com##g' \
      | rg "$admin_web_data_pattern" 2>/dev/null || true
  }
  auth_refresh_file="$tmpdir/apps/admin-web/lib/auth/firebase-refresh.ts"
  cat >"$auth_refresh_file" <<'EOF'
const SECURE_TOKEN_URL = "https://securetoken.googleapis.com/v1/token";
void SECURE_TOKEN_URL;
EOF
  if [ -z "$(auth_refresh_scan "$auth_refresh_file")" ]; then
    echo "Firebase-auth exemption self-test passed: exact securetoken endpoint is exempt."
  else
    echo "Firebase-auth exemption self-test failed: securetoken-only line was flagged."
    exit 1
  fi
  cat >"$auth_refresh_file" <<'EOF'
const SECURE_TOKEN_URL = "https://securetoken.googleapis.com/v1/token";
export const leak = "https://docs.google.com/spreadsheets/d/leak/export?format=csv";
void SECURE_TOKEN_URL;
EOF
  if auth_refresh_scan "$auth_refresh_file" | rg -q 'spreadsheets'; then
    echo "Firebase-auth exemption self-test passed: forbidden Sheets URL inside auth file still detected."
  else
    echo "Firebase-auth exemption self-test failed: forbidden Sheets URL inside auth file NOT detected (false green)."
    exit 1
  fi
  cat >"$auth_refresh_file" <<'EOF'
const leak = "https://securetoken.googleapis.com" + "https://docs.google.com/spreadsheets/d/leak/export?format=csv";
void leak;
EOF
  if auth_refresh_scan "$auth_refresh_file" | rg -q 'spreadsheets'; then
    echo "Firebase-auth exemption self-test passed: mixed securetoken+Sheets line still detected."
  else
    echo "Firebase-auth exemption self-test failed: mixed securetoken+Sheets line NOT detected (false green)."
    exit 1
  fi
  rm -f "$auth_refresh_file"
  mkdir -p "$tmpdir/apps/admin-web/app/bad-brand"
  cat >"$tmpdir/apps/admin-web/app/bad-brand/page.tsx" <<'EOF'
export default function BadBrandPage() {
  return <div>goat os and vgoat visible shell text</div>;
}
EOF
  if rg -ni "$admin_web_visible_branding_pattern" "$tmpdir/apps/admin-web/app" >/dev/null 2>&1; then
    echo "Admin-web visible branding guard self-test passed: synthetic violation detected."
  else
    echo "Admin-web visible branding guard self-test failed: synthetic violation NOT detected."
    exit 1
  fi
  mkdir -p "$tmpdir/apps/admin-web/app/debug-footer"
  cat >"$tmpdir/apps/admin-web/app/debug-footer/page.tsx" <<'EOF'
export default function DebugFooterPage() {
  return <div>Trace abc123. Rendered 13 Jun 2026, 08:24.</div>;
}
EOF
  if rg -n "$admin_web_debug_footer_pattern" "$tmpdir/apps/admin-web/app" >/dev/null 2>&1; then
    echo "Admin-web debug footer guard self-test passed: synthetic violation detected."
  else
    echo "Admin-web debug footer guard self-test failed: synthetic violation NOT detected."
    exit 1
  fi
  mkdir -p "$tmpdir/apps/admin-web/features/overview"
  mkdir -p "$tmpdir/apps/admin-web/features/goat-passport"
  cat >"$tmpdir/apps/admin-web/features/overview/bad-import.ts" <<'EOF'
import { thing } from "@/features/herd-search/internal";

void thing;
EOF
  if rg -n "$admin_web_feature_deep_import_pattern" "$tmpdir/apps/admin-web/features" >/dev/null 2>&1; then
    echo "Feature deep-import guard self-test passed: synthetic violation detected."
  else
    echo "Feature deep-import guard self-test failed: synthetic violation NOT detected."
    exit 1
  fi
  cat >"$tmpdir/apps/admin-web/features/overview/bad-relative-import.ts" <<'EOF'
import { thing } from "../goat-passport/internal";

void thing;
EOF
  fail=0
  check_admin_feature_relative_imports "$tmpdir/apps/admin-web/features" >/tmp/goatos-feature-relative-self-test
  if grep -q "bad-relative-import.ts" /tmp/goatos-feature-relative-self-test; then
    echo "Feature relative deep-import guard self-test passed: synthetic violation detected."
  else
    cat /tmp/goatos-feature-relative-self-test
    echo "Feature relative deep-import guard self-test failed: synthetic violation NOT detected."
    exit 1
  fi
  fail=0

  # Self-test for slog.New guard: create a synthetic violation outside the observability package.
  mkdir -p "$tmpdir/backend/internal/somepackage"
  cat >"$tmpdir/backend/internal/somepackage/fake.go" <<'EOF'
package somepackage

import (
  "log/slog"
  "os"
)

func bad() *slog.Logger {
  return slog.New(slog.NewJSONHandler(os.Stdout, nil))
}

func badPackageLevel() {
  slog.Error("bad")
}

func bad5xxLog() {
  slog.Default().Error("http_5xx")
}
EOF
  if rg -n "slog\.New\(" "$tmpdir/backend/internal/somepackage" \
      --glob '!*_test.go' >/dev/null 2>&1; then
    echo "slog.New guard self-test passed: synthetic violation detected."
  else
    echo "slog.New guard self-test failed: synthetic violation NOT detected."
    exit 1
  fi
  if rg -n "slog\.(Debug|Info|Warn|Error)(Context)?\(" "$tmpdir/backend/internal/somepackage" \
      --glob '!*_test.go' >/dev/null 2>&1; then
    echo "package-level slog guard self-test passed: synthetic violation detected."
  else
    echo "package-level slog guard self-test failed: synthetic violation NOT detected."
    exit 1
  fi
  if rg -n '"http_5xx"' "$tmpdir/backend/internal/somepackage" \
      --glob '!*_test.go' >/dev/null 2>&1; then
    echo "http_5xx guard self-test passed: synthetic violation detected."
  else
    echo "http_5xx guard self-test failed: synthetic violation NOT detected."
    exit 1
  fi

  # Self-test for recover() guard: create a synthetic silent panic swallow.
  mkdir -p "$tmpdir/backend/internal/pkg"
  cat >"$tmpdir/backend/internal/pkg/noLog.go" <<'EOF'
package pkg

func silentPanic() {
  defer func() {
    if p := recover(); p != nil {
      _ = p // silent swallow — no log call
    }
  }()
}
EOF
  if rg -n "recover\(\)" "$tmpdir/backend/internal/pkg/noLog.go" \
      --glob '!*_test.go' >/dev/null 2>&1; then
    echo "recover() guard self-test passed: recover() without log detected."
  else
    echo "recover() guard self-test failed: synthetic recover() not detected."
    exit 1
  fi

  echo "All boundary self-tests passed."
  exit 0
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
    "apps/admin-web/features"
    "apps/admin-web/lib"
  )
  if [ -d "apps/admin-web" ]; then
    # The Firebase Auth token-refresh endpoint (securetoken.googleapis.com) is
    # AUTH, not Sheets/BigQuery/App-Script DATA access, and matches the data
    # pattern only via its bare `googleapis` host. Exempt ONLY that exact host —
    # never the whole file. A whole-file PATH allowlist was a false green: a real
    # Sheets/BigQuery/XLSX line added inside firebase-refresh.ts would pass
    # unscanned. A per-line `grep -v <host>` was also wrong: a line MIXING the
    # auth host with a real Sheets URL got dropped. So the auth file IS scanned;
    # then only the `securetoken.googleapis.com` host token is stripped and the
    # remaining content re-tested — a mixed or unrelated data-access line is
    # still caught. See --self-test.
    admin_web_auth_refresh_file="apps/admin-web/lib/auth/firebase-refresh.ts"
    : >/tmp/goatos-admin-web-data-boundary-warnings
    rg -n "$admin_web_data_pattern" "${admin_web_code[@]}" \
      --glob '!node_modules/**' \
      --glob "!${admin_web_auth_refresh_file}" \
      >>/tmp/goatos-admin-web-data-boundary-warnings 2>/dev/null || true
    if [ -f "$admin_web_auth_refresh_file" ]; then
      rg -n "$admin_web_data_pattern" "$admin_web_auth_refresh_file" 2>/dev/null \
        | sed -E 's#securetoken\.googleapis\.com##g' \
        | rg "$admin_web_data_pattern" \
        >>/tmp/goatos-admin-web-data-boundary-warnings 2>/dev/null || true
    fi
    if [ -s /tmp/goatos-admin-web-data-boundary-warnings ]; then
      cat /tmp/goatos-admin-web-data-boundary-warnings
      echo "Admin web must use Goat OS backend APIs only; direct Sheets/App Script/BigQuery/XLSX data access is forbidden."
      fail=1
    fi
    if rg -ni "$admin_web_visible_branding_pattern" \
      apps/admin-web/app apps/admin-web/components apps/admin-web/features apps/admin-web/lib/api/server.ts \
      --glob '!node_modules/**' \
      --glob '*.{ts,tsx}' >/tmp/goatos-admin-web-branding-boundary-warnings 2>/dev/null; then
      if grep -viE 'GOATOS_|GoatOSApiError|X-GoatOS-|@goatos|goatos-build|goatos\.sop-form\.v1' /tmp/goatos-admin-web-branding-boundary-warnings \
        >/tmp/goatos-admin-web-branding-boundary-filtered; then
        cat /tmp/goatos-admin-web-branding-boundary-filtered
        echo "Admin web rendered/user-facing strings must use Mesha branding; Goat OS and VGoat are internal or legacy labels only."
        fail=1
      fi
    fi
    if rg -n "$admin_web_debug_footer_pattern" \
      apps/admin-web/app apps/admin-web/components apps/admin-web/features \
      --glob '!node_modules/**' \
      --glob '*.{ts,tsx}' >/tmp/goatos-admin-web-debug-footer-boundary-warnings 2>/dev/null; then
      cat /tmp/goatos-admin-web-debug-footer-boundary-warnings
      echo "Admin web must not render debug trace/render footers in normal UI. Keep trace IDs inside explicit error diagnostics only."
      fail=1
    fi
    if rg -n "$admin_web_feature_deep_import_pattern" "${admin_web_code[@]}" \
      --glob '!node_modules/**' >/tmp/goatos-admin-web-feature-boundary-warnings 2>/dev/null; then
      cat /tmp/goatos-admin-web-feature-boundary-warnings
      echo "Admin web feature modules must import other features through public entrypoints only."
      fail=1
    fi
    check_admin_feature_relative_imports "apps/admin-web/features"
  fi

  if rg -n "goat_identity_counters|GoatIdentityCounter" backend/internal/identity \
    --glob '!**/adapters/postgres/sqlc/schema.sql' \
    --glob '!**/adapters/postgres/sqlc/models.go' >/tmp/goatos-reporting-boundary-warnings 2>/dev/null; then
    cat /tmp/goatos-reporting-boundary-warnings
    echo "Identity package must not own reporting counter projection queries."
    fail=1
  fi

  # Guard: slog.New must only appear in platform/observability and test files.
  # Package-level slog logging must not appear in product code either; loggers
  # should be constructed with observability.New and used through an instance.
  if [ -d "backend" ]; then
    if rg -n "slog\.New\(" backend \
        --glob '*.go' \
        --glob '!*_test.go' \
        --glob '!backend/internal/platform/observability/**' \
        2>/dev/null | grep -v '//'; then
      echo "slog.New() used outside platform/observability in non-test backend Go code."
      echo "Use observability.New(observability.Config{...}) instead."
      fail=1
    fi
    if rg -n "slog\.(Debug|Info|Warn|Error)(Context)?\(" backend \
        --glob '*.go' \
        --glob '!*_test.go' \
        --glob '!backend/internal/platform/observability/**' \
        2>/dev/null | grep -v '//'; then
      echo "Package-level slog logging used outside platform/observability in non-test backend Go code."
      echo "Use a logger instance from observability.New(observability.Config{...}) instead."
      fail=1
    fi
    if rg -n '"http_5xx"' backend \
        --glob '*.go' \
        --glob '!*_test.go' \
        --glob '!backend/internal/platform/httpresponse/**' \
        2>/dev/null | grep -v '//'; then
      echo "http_5xx logging must be centralized in platform/httpresponse."
      echo "Use httpresponse.WriteError(...) instead of hand-rolled 5xx logging."
      fail=1
    fi
  fi

  # Guard: recover() in non-test backend Go code must either log the panic or
  # re-panic so an outer boundary can log it. A recover() that does neither
  # silently swallows panics. This is a conservative file-level check.
  if [ -d "backend" ]; then
    while IFS= read -r gofile; do
      # Skip test files.
      case "$gofile" in *_test.go) continue ;; esac
      if grep -qE '\brecover\(\)' "$gofile"; then
        if ! grep -qE '\blog\.(Error|ErrorContext|Warn|WarnContext)\b|\bslog\.(Error|ErrorContext|Warn|WarnContext)\b|\bpanic\(' "$gofile"; then
          echo "$gofile: recover() block found with no log.Error/ErrorContext call or re-panic in file."
          echo "  recover() blocks must log the panic value before suppressing/converting it, or re-panic to an outer logger."
          fail=1
        fi
      fi
    done < <(find backend -name '*.go' -not -name '*_test.go' 2>/dev/null)
  fi
else
  # rg is not available: fall back to grep for the guards that support it.
  # Secret/token scan and admin-web/BigQuery guards are rg-only; they are skipped
  # with a warning, then fail closed so the run is not a vacuous pass.
  echo "ERROR: rg (ripgrep) not found. Secret and BigQuery guards cannot run; slog.New/recover guards use grep fallback." >&2
  echo "  Install ripgrep (brew install ripgrep) for full boundary enforcement." >&2
  fail=1

  # slog.New and package-level slog guards — grep fallback (excludes
  # observability package and test files).
  if [ -d "backend" ]; then
    while IFS= read -r gofile; do
      case "$gofile" in
        *_test.go) continue ;;
        */platform/observability/*) continue ;;
      esac
      if grep -n 'slog\.New(' "$gofile" 2>/dev/null | grep -v '//' >/dev/null 2>&1; then
        echo "$gofile: slog.New() used outside platform/observability."
        echo "Use observability.New(observability.Config{...}) instead."
        fail=1
      fi
      if grep -nE 'slog\.(Debug|Info|Warn|Error)(Context)?\(' "$gofile" 2>/dev/null | grep -v '//' >/dev/null 2>&1; then
        echo "$gofile: package-level slog logging used outside platform/observability."
        echo "Use a logger instance from observability.New(observability.Config{...}) instead."
        fail=1
      fi
      case "$gofile" in
        */platform/httpresponse/*) ;;
        *)
          if grep -n '"http_5xx"' "$gofile" 2>/dev/null | grep -v '//' >/dev/null 2>&1; then
            echo "$gofile: http_5xx logging must be centralized in platform/httpresponse."
            echo "Use httpresponse.WriteError(...) instead of hand-rolled 5xx logging."
            fail=1
          fi
          ;;
      esac
    done < <(find backend -name '*.go' -not -name '*_test.go' 2>/dev/null)
  fi

  # recover() guard — grep-based (same as the rg branch above).
  if [ -d "backend" ]; then
    while IFS= read -r gofile; do
      case "$gofile" in *_test.go) continue ;; esac
      if grep -qE '\brecover\(\)' "$gofile"; then
        if ! grep -qE '\blog\.(Error|ErrorContext|Warn|WarnContext)\b|\bslog\.(Error|ErrorContext|Warn|WarnContext)\b|\bpanic\(' "$gofile"; then
          echo "$gofile: recover() block found with no log.Error/ErrorContext call or re-panic in file."
          echo "  recover() blocks must log the panic value before suppressing/converting it, or re-panic to an outer logger."
          fail=1
        fi
      fi
    done < <(find backend -name '*.go' -not -name '*_test.go' 2>/dev/null)
  fi
fi

exit "$fail"
