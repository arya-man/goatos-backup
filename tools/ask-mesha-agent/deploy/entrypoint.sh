#!/bin/sh
# Materialise $ASK_MESHA_STATE_DIR/.pgenv (read by server.mjs / psql for run_sql)
# from the read-only DB URL secret, without ever printing it.
#   ASK_MESHA_READONLY_DB_URL  e.g. postgresql://mesha_ceo_readonly:***@/goatos?host=/cloudsql/goatos-stg:asia-south1:goatos-stg-core-db
# If the URL is unset but PGHOST/PGUSER/... are already in the env, those are used.
set -eu
STATE_DIR="${ASK_MESHA_STATE_DIR:-$HOME/.ask-mesha-agent}"
umask 077
mkdir -p "$STATE_DIR"
PGENV_FILE="$STATE_DIR/.pgenv"

node --input-type=module - "$PGENV_FILE" <<'JS'
import fs from "node:fs";
const out = process.argv[2];
const raw = (process.env.ASK_MESHA_READONLY_DB_URL || "").trim();
const env = {};
const MAP = { host: "PGHOST", port: "PGPORT", user: "PGUSER", password: "PGPASSWORD", dbname: "PGDATABASE", sslmode: "PGSSLMODE" };
if (raw && !raw.includes("://")) {
  // libpq keyword DSN: host=/cloudsql/... user=... password=... dbname=...
  for (const m of raw.matchAll(/(\w+)\s*=\s*('(?:[^'\\]|\\.)*'|\S+)/g)) {
    const key = MAP[m[1]];
    if (key) env[key] = m[2].startsWith("'") ? m[2].slice(1, -1).replace(/\\(.)/g, "$1") : m[2];
  }
  if (env.PGHOST && !env.PGSSLMODE) env.PGSSLMODE = env.PGHOST.startsWith("/") ? "disable" : "require";
} else if (raw) {
  let u;
  // URL() rejects an empty host (postgresql://u:p@/db?host=/cloudsql/...).
  try { u = new URL(raw.replace(/@\//, "@unix-socket/").replace(/^(postgres(?:ql)?:\/\/)\//, "$1unix-socket/")); } catch { console.error("ask-mesha: ASK_MESHA_READONLY_DB_URL is not a valid URL"); process.exit(1); }
  const q = u.searchParams;
  env.PGHOST = q.get("host") || (u.hostname === "unix-socket" ? "" : decodeURIComponent(u.hostname));
  if (u.port) env.PGPORT = u.port; else if (q.get("port")) env.PGPORT = q.get("port");
  env.PGUSER = decodeURIComponent(u.username || q.get("user") || "");
  env.PGPASSWORD = decodeURIComponent(u.password || q.get("password") || "");
  env.PGDATABASE = decodeURIComponent(u.pathname.replace(/^\//, "") || q.get("dbname") || "");
  env.PGSSLMODE = q.get("sslmode") || (env.PGHOST.startsWith("/") ? "disable" : "require");
} else {
  for (const k of ["PGHOST", "PGPORT", "PGUSER", "PGPASSWORD", "PGDATABASE", "PGSSLMODE"]) {
    if (process.env[k]) env[k] = process.env[k];
  }
}
for (const k of ["PGHOST", "PGUSER", "PGDATABASE"]) {
  if (!env[k]) { console.error(`ask-mesha: read-only DB config missing ${k}; run_sql will fail`); process.exit(1); }
}
for (const [k, v] of Object.entries(env)) {
  if (/[\n\r]/.test(v)) { console.error(`ask-mesha: ${k} contains a newline`); process.exit(1); }
}
const body = Object.entries(env).filter(([, v]) => v !== "").map(([k, v]) => `${k}=${v}`).join("\n") + "\n";
fs.writeFileSync(out, body, { mode: 0o600 });
console.error(`ask-mesha: wrote ${Object.keys(env).length} PG settings for user ${env.PGUSER} (secret values not logged)`);
JS

# Secrets stay in the process env only as long as needed.
unset ASK_MESHA_READONLY_DB_URL PGPASSWORD 2>/dev/null || true
exec "$@"
