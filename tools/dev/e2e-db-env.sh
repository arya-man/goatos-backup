#!/usr/bin/env bash
# Shared DB resolver for local E2E/proof/load scripts.
#
# E2E must never silently touch the normal laptop app database. The caller must
# pass an explicit DB URL. That URL may intentionally target the normal local DB
# for a read/proof run, or an isolated throwaway DB for destructive/load runs,
# but it must be visible in the command/env.

goatos_resolve_e2e_database_url() {
  if [ -n "${GOATOS_E2E_DATABASE_URL:-}" ]; then
    printf '%s\n' "$GOATOS_E2E_DATABASE_URL"
    return
  fi
  if [ -n "${DATABASE_URL:-}" ]; then
    printf '%s\n' "$DATABASE_URL"
    return
  fi
  cat >&2 <<'MSG'
E2E/proof/load scripts require an explicit GOATOS_E2E_DATABASE_URL or DATABASE_URL.
This prevents test harnesses from silently touching the normal local app DB.
If you intentionally want the normal local DB, pass:
  DATABASE_URL=postgres://postgres:goatos@127.0.0.1:5433/goatos?sslmode=disable
For the isolated local GCP-kernel stack, start it first, then pass:
  GOATOS_E2E_DATABASE_URL=postgres://postgres:goatos@127.0.0.1:55432/goatos?sslmode=disable
MSG
  return 2
}

resolved_database_url="$(goatos_resolve_e2e_database_url)" || exit $?
export DATABASE_URL="$resolved_database_url"
unset resolved_database_url

goatos_e2e_database_url_is_normal_app_db() {
  local url
  url="$(printf '%s' "${1:-$DATABASE_URL}" | tr '[:upper:]' '[:lower:]')"
  case "$url" in
    *@127.0.0.1:5433/goatos* | *@localhost:5433/goatos* | *//127.0.0.1:5433/goatos* | *//localhost:5433/goatos*)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

goatos_e2e_require_isolated_database() {
  local label="${1:-E2E/proof/load script}"
  if ! goatos_e2e_database_url_is_normal_app_db "$DATABASE_URL"; then
    return 0
  fi
  if [ "${GOATOS_E2E_ALLOW_APP_DB_MUTATION:-0}" = "1" ]; then
    return 0
  fi
  cat >&2 <<MSG
$label refuses to mutate the normal local app DB on 5433.
Use an isolated DB for destructive/proof/load runs, for example:
  make dev-local-kernel-up
  GOATOS_E2E_DATABASE_URL=postgres://postgres:goatos@127.0.0.1:55432/goatos?sslmode=disable $label
If you intentionally want this script to write into the normal seeded local DB,
make the risk explicit:
  GOATOS_E2E_ALLOW_APP_DB_MUTATION=1 DATABASE_URL=postgres://postgres:goatos@127.0.0.1:5433/goatos?sslmode=disable $label
MSG
  exit 2
}
