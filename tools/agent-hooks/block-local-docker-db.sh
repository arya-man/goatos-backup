#!/usr/bin/env bash
# block-local-docker-db — refuse to start Colima / local Docker Postgres on this laptop.
#
# WHY THIS EXISTS
# ---------------
# 2026-08-22: an agent session (mine) ran `colima start` and `GOATOS_RUN_POSTGRES_TESTS=1 go test`
# on the maintainer's laptop while the OCI Postgres tunnel was already available on
# 127.0.0.1:15432. Colima came back up and held ~987 MB of RAM until the maintainer found the
# child processes and killed them by hand. The tests it was starting the VM for could have run
# against OCI, which was already up and already held the real 46k-packet dataset.
#
# The rule is recorded in AGENTS.md and CLAUDE.md at every level (workspace root, canonical
# goatos, and this worktree). Documentation alone did not stop it — the session read those files
# and started Colima anyway — so this is the executable half.
#
# THE RULE
#   Do NOT start Colima, Docker Desktop, `goatos-local-current`, or any local Docker Postgres, and
#   do NOT run Postgres integration tests with GOATOS_RUN_POSTGRES_TESTS=1, while the OCI tunnel on
#   127.0.0.1:15432 is reachable. Use the OCI dev database.
#
# ESCAPE HATCH
#   Set GOATOS_ALLOW_LOCAL_DOCKER_DB=1 for an explicitly-requested local Docker DB, a
#   Docker-specific test, or a disposable mutation database. That is a MAINTAINER decision, not an
#   agent convenience.
#
# USAGE
#   As a PreToolUse hook, fed the candidate command on stdin or as $*:
#     tools/agent-hooks/block-local-docker-db.sh "colima start"        -> exit 1
#     tools/agent-hooks/block-local-docker-db.sh "go test ./..."       -> exit 0
#   Or standalone to check the current environment:
#     tools/agent-hooks/block-local-docker-db.sh --check-env
set -uo pipefail

if [[ "${GOATOS_ALLOW_LOCAL_DOCKER_DB:-0}" == "1" ]]; then
  exit 0
fi

candidate="${*:-}"
if [[ -z "$candidate" && ! -t 0 ]]; then
  candidate="$(cat)"
fi

oci_tunnel_up() {
  nc -z -G 2 127.0.0.1 15432 >/dev/null 2>&1
}

refuse() {
  local what="$1"
  echo "BLOCKED: $what" >&2
  echo >&2
  echo "The OCI Postgres dev database is the test target on this machine. Starting Colima or a" >&2
  echo "local Docker Postgres costs ~1 GB of RAM on the maintainer's laptop for a database that" >&2
  echo "is already running, already migrated, and already holds the real captured dataset." >&2
  echo >&2
  echo "Use OCI instead:" >&2
  echo "  tunnel:  \$HOME/mesha/tools/local/oci-goatos-a1-dev.sh tunnel   (127.0.0.1:15432)" >&2
  echo "  creds:   source \"\${GOATOS_OCI_DB_ENV:-\$HOME/mesha/local-data/goatos-stg-to-oci/oci-goatos-db.env}\"" >&2
  echo >&2
  echo "If a local Docker DB is genuinely required (Docker-specific behaviour, or a disposable" >&2
  echo "mutation database), ask the maintainer and re-run with GOATOS_ALLOW_LOCAL_DOCKER_DB=1." >&2
  exit 1
}

if [[ "$candidate" == "--check-env" ]]; then
  if [[ "${GOATOS_RUN_POSTGRES_TESTS:-0}" == "1" ]] && oci_tunnel_up; then
    refuse "GOATOS_RUN_POSTGRES_TESTS=1 is set while the OCI tunnel is up"
  fi
  exit 0
fi

# Commands that start a local container runtime or a local Postgres container.
if grep -Eq '(^|[^[:alnum:]_])colima[[:space:]]+start' <<<"$candidate"; then
  refuse "'colima start' — a local Docker VM (~1 GB RAM)"
fi
if grep -Eq 'limactl[[:space:]]+start' <<<"$candidate"; then
  refuse "'limactl start' — a local Lima/Colima VM"
fi
if grep -Eq 'docker[[:space:]]+(start|run|compose[[:space:]]+up).*goatos-local-current' <<<"$candidate"; then
  refuse "starting the goatos-local-current Postgres container"
fi
if grep -Eq 'open[[:space:]]+-a[[:space:]]+["]?Docker' <<<"$candidate"; then
  refuse "launching Docker Desktop"
fi

# Postgres integration tests spin a throwaway container through the pgtest harness.
if grep -Eq 'GOATOS_RUN_POSTGRES_TESTS=1' <<<"$candidate" && oci_tunnel_up; then
  refuse "GOATOS_RUN_POSTGRES_TESTS=1 while the OCI tunnel is up"
fi

exit 0
