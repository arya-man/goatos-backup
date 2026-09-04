#!/usr/bin/env bash
# Read-only full-table parity check for the maintainer OCI Postgres clone versus
# Goat OS staging. It proves semantic schema equality and fingerprints every
# non-system base table before OCI is treated as staging-equivalent. It is read-only:
# no writes, migrations, grants, or repair steps are attempted here.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
OUT_DIR="${GOATOS_DB_PARITY_OUT_DIR:-${REPO_ROOT}/tools/ceo-ai/eval/out/db-parity}"
OCI_DSN="${GOATOS_OCI_DATABASE_URL:-${DATABASE_URL:-}}"
STG_DSN="${GOATOS_STG_DATABASE_URL:-}"
started_proxy_pid=""
fingerprint_tmp_dir=""
CHECK_GRANTS="${GOATOS_DB_PARITY_CHECK_GRANTS:-0}"

if [[ -z "$OCI_DSN" ]]; then
  env_file="${GOATOS_OCI_DB_ENV:-$HOME/mesha/local-data/goatos-stg-to-oci/oci-goatos-db.env}"
  if [[ -f "$env_file" ]]; then
    # shellcheck disable=SC1090
    source "$env_file"
    OCI_DSN="${GOATOS_OCI_DATABASE_URL:-${DATABASE_URL:-}}"
  fi
fi

if [[ -z "$STG_DSN" ]]; then
  STG_DSN="$(gcloud secrets versions access latest --secret=goatos-stg-database-url --project="${GOATOS_STG_PROJECT:-goatos-stg}" 2>/dev/null || true)"
fi

die() { echo "db-parity: $*" >&2; exit 2; }
cleanup() {
  if [[ -n "$started_proxy_pid" ]]; then
    kill "$started_proxy_pid" >/dev/null 2>&1 || true
  fi
  if [[ -n "$fingerprint_tmp_dir" && -d "$fingerprint_tmp_dir" ]]; then
    rm -rf "$fingerprint_tmp_dir"
  fi
}
trap cleanup EXIT

rewrite_stg_socket_dsn_for_proxy() {
  local dsn="$1" port="$2"
  STG_SOCKET_DSN="$dsn" STG_PROXY_PORT="$port" python3 - <<'PY'
import os
import shlex
from urllib.parse import parse_qsl, urlencode, urlsplit, urlunsplit

raw = os.environ["STG_SOCKET_DSN"].strip()
port = os.environ["STG_PROXY_PORT"]
if raw.lower().startswith(("postgres://", "postgresql://")):
    parsed = urlsplit(raw)
    query = dict(parse_qsl(parsed.query, keep_blank_values=True))
    query.pop("host", None)
    query.pop("hostaddr", None)
    query["sslmode"] = query.get("sslmode", "disable")
    netloc = parsed.netloc
    if "@" in netloc:
        auth = netloc.rsplit("@", 1)[0]
        netloc = f"{auth}@127.0.0.1:{port}"
    else:
        netloc = f"127.0.0.1:{port}"
    print(urlunsplit((parsed.scheme, netloc, parsed.path, urlencode(query), parsed.fragment)))
else:
    parts = {}
    for token in shlex.split(raw, posix=True):
        if "=" not in token:
            raise SystemExit("unsupported staging DB DSN")
        key, value = token.split("=", 1)
        parts[key] = value
    parts["host"] = "127.0.0.1"
    parts["port"] = port
    parts.setdefault("sslmode", "disable")
    print(" ".join(f"{key}={shlex.quote(value)}" for key, value in parts.items()))
PY
}

maybe_start_stg_proxy() {
  [[ "$STG_DSN" == *"/cloudsql/"* ]] || return 0
  command -v cloud-sql-proxy >/dev/null 2>&1 || die "staging DB URL uses /cloudsql socket but cloud-sql-proxy is not installed"
  command -v python3 >/dev/null 2>&1 || die "python3 is required to rewrite staging socket DSN for cloud-sql-proxy"
  local instance="${GOATOS_STG_CLOUD_SQL_INSTANCE:-goatos-stg:asia-south1:goatos-stg-core-db}"
  local port="${GOATOS_STG_DB_PROXY_PORT:-15433}"
  if ! lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; then
    echo "db-parity: starting temporary Cloud SQL proxy for staging on 127.0.0.1:${port}"
    cloud-sql-proxy --address 127.0.0.1 --port "$port" "$instance" >/tmp/goatos-stg-cloud-sql-proxy.log 2>&1 &
    started_proxy_pid="$!"
    for _ in 1 2 3 4 5 6 7 8 9 10; do
      if lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; then
        break
      fi
      sleep 0.5
    done
  fi
  lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1 || die "Cloud SQL proxy did not listen on 127.0.0.1:${port}; see /tmp/goatos-stg-cloud-sql-proxy.log"
  STG_DSN="$(rewrite_stg_socket_dsn_for_proxy "$STG_DSN" "$port")"
}

[[ -n "$OCI_DSN" ]] || die "GOATOS_OCI_DATABASE_URL or DATABASE_URL is required for the OCI tunnel database"
[[ -n "$STG_DSN" ]] || die "GOATOS_STG_DATABASE_URL is required, or gcloud must read secret goatos-stg-database-url"
maybe_start_stg_proxy

case "$OCI_DSN" in
  postgres://*@127.0.0.1:15432/*|postgresql://*@127.0.0.1:15432/*) ;;
  *) die "refusing OCI DSN that is not the approved tunnel 127.0.0.1:15432" ;;
esac

mkdir -p "$OUT_DIR"
command -v shasum >/dev/null 2>&1 || die "shasum is required for all-table content fingerprints"
fingerprint_tmp_dir="$(mktemp -d "$OUT_DIR/.fingerprint-tmp.XXXXXX")"

schema_dump() {
  local dsn="$1" out="$2"
  pg_dump "$dsn" --schema-only --no-owner --no-privileges \
    --exclude-schema='pg_*' --exclude-schema='information_schema' > "$out"
}

inventory_db() {
  local dsn="$1" out="$2"
  psql "$dsn" -X -v ON_ERROR_STOP=1 -At -F $'\t' <<'SQL' > "$out"
SELECT 'extension', extname, extversion
FROM pg_extension
UNION ALL
SELECT 'relation', n.nspname || '.' || c.relname, c.relkind::text
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname NOT IN ('pg_catalog', 'information_schema')
  AND n.nspname NOT LIKE 'pg_toast%'
  AND c.relkind IN ('r', 'p', 'v', 'm', 'S', 'f')
UNION ALL
SELECT 'column',
       table_schema || '.' || table_name || '.' || column_name,
       data_type || ':' || COALESCE(udt_name, '') || ':' || is_nullable || ':' || COALESCE(column_default, '') || ':' || COALESCE(generation_expression, '')
FROM information_schema.columns
WHERE table_schema NOT IN ('pg_catalog', 'information_schema')
UNION ALL
SELECT 'constraint',
       n.nspname || '.' || c.relname || '.' || con.conname,
       con.contype::text || ':' || pg_get_constraintdef(con.oid, true)
FROM pg_constraint con
JOIN pg_class c ON c.oid = con.conrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname NOT IN ('pg_catalog', 'information_schema')
UNION ALL
SELECT 'index',
       schemaname || '.' || tablename || '.' || indexname,
       indexdef
FROM pg_indexes
WHERE schemaname NOT IN ('pg_catalog', 'information_schema')
ORDER BY 1, 2, 3;
SQL
  if [[ "$CHECK_GRANTS" == "1" ]]; then
    psql "$dsn" -X -v ON_ERROR_STOP=1 -At -F $'\t' <<'SQL' >> "$out"
SELECT 'role-grant', grantee || ':' || table_schema || '.' || table_name, privilege_type
FROM information_schema.role_table_grants
WHERE table_schema NOT IN ('pg_catalog', 'information_schema')
ORDER BY 1, 2, 3;
SQL
    sort -o "$out" "$out"
  fi
}

fingerprint_db() {
  local dsn="$1" out="$2"
  local relation_list schema_name table_name relation_name raw_hashes row_count row_hash
  relation_list="$fingerprint_tmp_dir/relations.tsv"
  psql "$dsn" -X -v ON_ERROR_STOP=1 -At -F $'\t' <<'SQL' > "$relation_list"
SELECT n.nspname, c.relname
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname NOT IN ('pg_catalog', 'information_schema')
  AND n.nspname NOT LIKE 'pg_toast%'
  AND c.relkind IN ('r', 'p')
  -- A partitioned parent query includes every child row, so hashing its children
  -- again would duplicate the same data and make the daily all-data check slow.
  AND NOT c.relispartition
ORDER BY n.nspname, c.relname;
SQL

  : > "$out"
  while IFS=$'\t' read -r schema_name table_name; do
    relation_name="$(psql "$dsn" -X -v ON_ERROR_STOP=1 -At -v schema_name="$schema_name" -v table_name="$table_name" <<'SQL'
SELECT format('%I.%I', :'schema_name', :'table_name');
SQL
)"
    raw_hashes="$(mktemp "$fingerprint_tmp_dir/row-hashes.XXXXXX")"
    # Stream one fixed-width digest per row, then sort those digests locally.
    # This preserves multiplicity and is independent of row storage order while
    # avoiding a huge string_agg/sort inside Cloud SQL for history tables.
    psql "$dsn" -X -v ON_ERROR_STOP=1 -At -c "SELECT md5(to_jsonb(t)::text) FROM ${relation_name} AS t;" > "$raw_hashes"
    row_count="$(wc -l < "$raw_hashes" | tr -d ' ')"
    row_hash="$(LC_ALL=C sort "$raw_hashes" | shasum -a 256 | awk '{print $1}')"
    printf '%s\t%s\t%s\n' "$relation_name" "$row_count" "$row_hash" >> "$out"
    rm -f "$raw_hashes"
  done < "$relation_list"
  sort -o "$out" "$out"
}

echo "db-parity: dumping schema"
schema_dump "$OCI_DSN" "$OUT_DIR/oci.schema.sql"
schema_dump "$STG_DSN" "$OUT_DIR/stg.schema.sql"

if ! diff -u "$OUT_DIR/stg.schema.sql" "$OUT_DIR/oci.schema.sql" > "$OUT_DIR/schema.diff"; then
  echo "db-parity: raw pg_dump differs; semantic inventory decides pass/fail, see $OUT_DIR/schema.diff"
fi

echo "db-parity: diffing extension/relation/column/constraint/index/grant inventory"
inventory_db "$OCI_DSN" "$OUT_DIR/oci.inventory.tsv"
inventory_db "$STG_DSN" "$OUT_DIR/stg.inventory.tsv"

if ! diff -u "$OUT_DIR/stg.inventory.tsv" "$OUT_DIR/oci.inventory.tsv" > "$OUT_DIR/inventory.diff"; then
  echo "db-parity: semantic schema inventory mismatch; see $OUT_DIR/inventory.diff" >&2
  exit 1
fi

echo "db-parity: fingerprinting every user base table"
fingerprint_db "$OCI_DSN" "$OUT_DIR/oci.fingerprint.tsv"
fingerprint_db "$STG_DSN" "$OUT_DIR/stg.fingerprint.tsv"

if ! diff -u "$OUT_DIR/stg.fingerprint.tsv" "$OUT_DIR/oci.fingerprint.tsv" > "$OUT_DIR/fingerprint.diff"; then
  echo "db-parity: data fingerprint mismatch; see $OUT_DIR/fingerprint.diff" >&2
  exit 1
fi

echo "db-parity: PASS (schema + inventory + every-table row-count/content fingerprints match)"
echo "db-parity: artifacts in $OUT_DIR"
