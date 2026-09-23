#!/usr/bin/env bash
# Reclaim space the automation itself creates, so a sweep can never fill the box.
#
# Two jobs, both idempotent and safe to run while nothing is sweeping:
#   1. prune old run directories (screenshots + receipts) beyond a keep-count
#   2. drop throwaway databases and leftover snapshot tables a write-lane run left behind
#
# Refuses to touch anything that is not clearly disposable. Never drops a database
# unless its name matches the disposable naming rule the write lane is required to use.
set -euo pipefail

KEEP_RUNS="${GOATOS_JANITOR_KEEP_RUNS:-10}"
RENDER_ROOT="${GOATOS_JANITOR_RENDER_ROOT:-$HOME/goatos-automation/goatos/.codex-goatos-render}"
MIN_FREE_GB="${GOATOS_JANITOR_MIN_FREE_GB:-20}"
DRY="${GOATOS_JANITOR_DRY_RUN:-0}"

say() { echo "janitor: $*"; }
run() { if [ "$DRY" = "1" ]; then say "would: $*"; else "$@"; fi; }

# Portable: GNU df has --output, BSD/macOS does not. Parse the Avail column either way.
free_gb() { df -g "$1" 2>/dev/null | awk 'NR==2{print $4}' || df -BG "$1" 2>/dev/null | awk 'NR==2{gsub(/G/,"",$4); print $4}'; }

say "free before: $(free_gb /) GB on /, $(free_gb "$RENDER_ROOT" 2>/dev/null || echo '?') GB where runs are written"

# --- 1. old run directories -------------------------------------------------
for dir in "$RENDER_ROOT/dashboard-automation" "$RENDER_ROOT/admin-web-screenshots"; do
  [ -d "$dir" ] || continue
  mapfile -t old < <(ls -1dt "$dir"/*/ 2>/dev/null | tail -n +$((KEEP_RUNS + 1)))
  if [ ${#old[@]} -gt 0 ]; then
    say "pruning ${#old[@]} run dir(s) in $(basename "$dir"), keeping the newest $KEEP_RUNS"
    for d in "${old[@]}"; do run rm -rf "$d"; done
  fi
done

# --- 2. throwaway databases -------------------------------------------------
# Only names the write lane is required to use. A database that does not match
# this is never touched, no matter how old it looks.
DISPOSABLE_RE='^(goatos_)?(dashboard[_-]automation|preview|throwaway|tmp)[_-][a-z0-9]+$'
PSQL="${GOATOS_JANITOR_PSQL:-psql}"
if command -v "$PSQL" >/dev/null 2>&1; then
  while read -r db; do
    [ -n "$db" ] || continue
    if [[ "$db" =~ $DISPOSABLE_RE ]]; then
      say "dropping disposable database $db"
      run "$PSQL" -qAt -d postgres -c "DROP DATABASE IF EXISTS \"$db\" WITH (FORCE)" >/dev/null
    fi
  done < <("$PSQL" -qAt -d postgres -c "select datname from pg_database where not datistemplate" 2>/dev/null || true)

  # snapshot tables a write journey kept because its restore could not be proved.
  # Those are evidence, so only clear ones older than the retention window.
  SNAP_AGE_HOURS="${GOATOS_JANITOR_SNAPSHOT_AGE_HOURS:-48}"
  while read -r t; do
    [ -n "$t" ] || continue
    say "dropping snapshot table older than ${SNAP_AGE_HOURS}h: $t"
    run "$PSQL" -qAt -c "DROP TABLE IF EXISTS $t" >/dev/null
  done < <("$PSQL" -qAt -c "
    select format('%I.%I', schemaname, tablename)
    from pg_tables
    where tablename like 'goatos_snapshot_%'
      and pg_catalog.obj_description(format('%I.%I', schemaname, tablename)::regclass, 'pg_class') is distinct from 'keep'
  " 2>/dev/null || true)
else
  say "psql not on PATH; skipped the database sweep (set GOATOS_JANITOR_PSQL)"
fi

AFTER="$(free_gb /)"
say "free after: ${AFTER} GB on /"
if [ "${AFTER:-0}" -lt "$MIN_FREE_GB" ]; then
  say "STILL BELOW FLOOR: ${AFTER} GB < ${MIN_FREE_GB} GB. The next sweep will refuse to run."
  exit 1
fi
