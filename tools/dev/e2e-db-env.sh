#!/usr/bin/env bash
# Shared DB resolver for local E2E/proof/load scripts.
#
# E2E must never silently touch the normal laptop app database. The caller must
# pass an explicit DB URL. Read-only checks may target the normal local DB, but
# mutating proof/load scripts must use an isolated throwaway DB.

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
Read-only checks may target the normal local DB by passing:
  DATABASE_URL=postgres://postgres:goatos@127.0.0.1:5433/goatos?sslmode=disable
Mutating E2E/proof/load scripts must use an isolated DB. For the local
GCP-kernel stack, start it first, then pass:
  GOATOS_E2E_DATABASE_URL=postgres://postgres:goatos@127.0.0.1:55432/goatos?sslmode=disable
MSG
  return 2
}

resolved_database_url="$(goatos_resolve_e2e_database_url)" || exit $?
export DATABASE_URL="$resolved_database_url"
unset resolved_database_url

goatos_classify_e2e_database_url() {
  local dsn="${1:-${DATABASE_URL:-}}"
  if ! command -v python3 >/dev/null 2>&1; then
    printf 'invalid\n'
    return
  fi
  printf '%s' "$dsn" | python3 -c '
import ipaddress
import shlex
import sys
from urllib.parse import parse_qs, unquote, urlsplit

raw = sys.stdin.read().strip()

def invalid():
    print("invalid")
    raise SystemExit(0)

try:
    if not raw:
        invalid()
    if raw.lower().startswith(("postgres://", "postgresql://")):
        parsed = urlsplit(raw)
        query = parse_qs(parsed.query, keep_blank_values=True)
        host_value = query.get("hostaddr", query.get("host", [parsed.hostname or ""]))[-1]
        parsed_port = parsed.port
        port_value = query.get("port", [str(parsed_port) if parsed_port is not None else ""])[-1]
        dbname = query.get("dbname", [unquote(parsed.path.lstrip("/"))])[-1]
    else:
        values = {}
        for token in shlex.split(raw, posix=True):
            if "=" not in token:
                invalid()
            key, value = token.split("=", 1)
            values[key.strip().lower()] = value
        host_value = values.get("hostaddr", values.get("host", ""))
        port_value = values.get("port", "")
        dbname = values.get("dbname", "")

    hosts = [unquote(item).strip().strip("[]").rstrip(".").lower() for item in host_value.split(",")]
    port_parts = [item.strip() for item in str(port_value).split(",")]
    if not hosts or not all(hosts) or not dbname or not port_parts or not all(port_parts):
        invalid()
    ports = [int(item) for item in port_parts]
    if len(ports) == 1 and len(hosts) > 1:
        ports *= len(hosts)
    if len(ports) != len(hosts) or any(port < 1 or port > 65535 for port in ports):
        invalid()

    def is_local(host):
        if host.startswith("/"):
            return True
        if host in {"localhost", "localhost.localdomain", "host.docker.internal", "0.0.0.0", "::"} or host.endswith(".localhost"):
            return True
        try:
            address = ipaddress.ip_address(host)
            return address.is_loopback or address.is_unspecified
        except ValueError:
            return False

    normal = dbname.lower() == "goatos" and any(is_local(host) and port == 5433 for host, port in zip(hosts, ports))
    print("normal_app" if normal else "other")
except (TypeError, ValueError, IndexError, KeyError):
    invalid()
'
}

goatos_e2e_require_isolated_database() {
  local label="${1:-E2E/proof/load script}"
  local classification
  classification="$(goatos_classify_e2e_database_url "$DATABASE_URL")"
  if [ "$classification" = "invalid" ]; then
    cat >&2 <<MSG
$label cannot safely classify DATABASE_URL.
Use a PostgreSQL URL or keyword DSN with explicit host, port, and dbname.
Mutating E2E/proof/load scripts fail closed for unrecognized connection strings.
MSG
    exit 2
  fi
  if [ "$classification" = "normal_app" ]; then
    cat >&2 <<MSG
$label refuses to mutate the normal local app DB on 5433.
There is no override for this. Use an isolated DB for destructive/proof/load
runs, for example:
  make dev-local-kernel-up
  GOATOS_E2E_DATABASE_URL=postgres://postgres:goatos@127.0.0.1:55432/goatos?sslmode=disable $label
MSG
    exit 2
  fi
}
