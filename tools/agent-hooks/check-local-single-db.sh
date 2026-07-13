#!/usr/bin/env bash
set -euo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
fail=0

normal_runtime_files=(
  "tools/dev/run-local-stack.sh"
  "tools/dev/run-local-stack-supervised.sh"
  "apps/admin-web/scripts/run-local-next.mjs"
)

e2e_runtime_files=(
  "tools/dev/high-scale-kernel-e2e-all.sh"
  "tools/dev/admin-web-e2e-smoke.sh"
  "tools/dev/procurement-vaccination-e2e-matrix.sh"
  "tools/dev/vaccination-chain-proof.sh"
  "tools/dev/vaccination-rework-proof.sh"
  "tools/dev/vaccination-trusted-history-proof.sh"
)

mark_fail() {
  fail=1
  printf 'local-single-db-guard: FAIL: %s\n' "$*" >&2
}

require_pattern() {
  local file="$1"
  local pattern="$2"
  local description="$3"
  if ! grep -Fq "$pattern" "$repo/$file"; then
    mark_fail "$file missing ${description}"
  fi
}

reject_pattern() {
  local file="$1"
  local pattern="$2"
  local description="$3"
  if grep -Fq "$pattern" "$repo/$file"; then
    mark_fail "$file still contains ${description}"
  fi
}

for file in "${normal_runtime_files[@]}"; do
  if [ ! -f "$repo/$file" ]; then
    mark_fail "$file is missing"
    continue
  fi
  require_pattern "$file" "goatos-local-current" "canonical local DB detection"
  require_pattern "$file" "Multiple Goat OS local app Postgres containers are running; refusing to guess DATABASE_URL." "multiple-DB refusal"
  reject_pattern "$file" "127.0.0.1:5432/goatos" "old host-Postgres fallback on :5432"
done

require_pattern "tools/dev/lib/db-mutation-guard.sh" "127.0.0.1:5433/goatos" "single normal local DB fallback on :5433"
require_pattern "apps/admin-web/scripts/run-local-next.mjs" "127.0.0.1:5433/goatos" "single normal local DB fallback on :5433"
require_pattern "tools/dev/run-local-stack.sh" "lib/db-mutation-guard.sh" "shared shell DB mutation guard"
require_pattern "tools/dev/run-local-stack-supervised.sh" "lib/db-mutation-guard.sh" "shared shell DB mutation guard"

for file in "${e2e_runtime_files[@]}"; do
  if [ ! -f "$repo/$file" ]; then
    mark_fail "$file is missing"
    continue
  fi
  require_pattern "$file" "tools/dev/e2e-db-env.sh" "shared E2E DB resolver"
  require_pattern "$file" "goatos_e2e_require_isolated_database" "mutating E2E isolated-DB guard"
  reject_pattern "$file" "127.0.0.1:55432/goatos" "silent old E2E DB default on :55432"
  reject_pattern "$file" "127.0.0.1:5433/goatos" "silent normal app DB default in E2E/proof scripts"
done

require_pattern "tools/dev/e2e-db-env.sh" "GOATOS_E2E_DATABASE_URL" "explicit E2E DB override"
require_pattern "tools/dev/e2e-db-env.sh" "GOATOS_E2E_ALLOW_APP_DB_MUTATION" "explicit normal-app-DB mutation override"
require_pattern "tools/dev/e2e-db-env.sh" "E2E/proof/load scripts require an explicit" "fail-closed E2E DB resolver"
reject_pattern "tools/dev/e2e-db-env.sh" "printf 'postgres://postgres:goatos@127.0.0.1:5433/goatos" "silent normal app DB default in E2E resolver"

if command -v docker >/dev/null 2>&1 && docker ps >/dev/null 2>&1; then
  candidates="$(
    docker ps --format '{{.Names}} {{.Ports}}' 2>/dev/null \
      | awk '
          $1 == "goatos-local-current" ||
          $1 == "goatos-postgres" ||
          $1 ~ /^goatos-postgres-/ {
            if (match($0, /127\.0\.0\.1:[0-9]+->5432\/tcp/)) {
              print $0
            }
          }
        '
  )"
  count="$(printf '%s\n' "$candidates" | sed '/^$/d' | wc -l | tr -d ' ')"
  if [ "$count" -gt 1 ]; then
    mark_fail "multiple Goat OS local app Postgres containers are running"
    printf '%s\n' "$candidates" >&2
  fi
fi

if [ "$fail" -ne 0 ]; then
  cat >&2 <<'MSG'

Normal local laptop runtime must resolve exactly one Goat OS app database.
Separate E2E/load-test databases are allowed only when isolated by their test
scripts and not used by dev-local/admin-web/mobile runtime.
MSG
  exit 1
fi

echo "local-single-db-guard: PASS"
