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
# THE NO-DOCKER EXCEPTION: GOATOS_PGTEST_ADMIN_DSN
#   The rule above exists because the pgtest harness USED TO mean "spin a throwaway container", and
#   a container costs ~1 GB of the maintainer's RAM. It has a sanctioned second path:
#   backend/internal/platform/pgtest honours GOATOS_PGTEST_ADMIN_DSN, and when that is set
#   SkipIfNoDocker returns early -- no Colima, no Docker, no container, and nothing is looked up on
#   15432. Refusing that invocation blocks a LEGITIMATE landing: `make land-main` cannot otherwise
#   execute the Postgres-gated migration/view proofs on this laptop while the tunnel is up, so a
#   new migration lands unproven. The CONDITION was over-broad, not the rationale.
#
#   So GOATOS_RUN_POSTGRES_TESTS=1 is ALLOWED when an admin DSN is supplied -- with one narrowing,
#   which is the whole reason a check remains here at all: the DSN must NOT name a protected
#   database. 127.0.0.1:15432 is the OCI tunnel (a maintained staging-like clone, delta-only by
#   AGENTS.md), 5433 the legacy local stack, 15433 its sibling, 15544 the phone-QA throwaway that
#   another session may be mid-run on. pgtest CREATEs and DROPs databases on whatever it is pointed
#   at, so aiming it at one of those is a destructive act wearing the no-Docker path's clothes. It
#   is refused loudly, and GOATOS_ALLOW_LOCAL_DOCKER_DB does NOT unlock it -- that hatch is about
#   RAM. Stand up your own throwaway cluster on your own port instead (homebrew initdb + pg_ctl).
#
# ESCAPE HATCH
#   Set GOATOS_ALLOW_LOCAL_DOCKER_DB=1 for an explicitly-requested local Docker DB, a
#   Docker-specific test, or a disposable mutation database. That is a MAINTAINER decision, not an
#   agent convenience. It does NOT unlock a protected-database admin DSN.
#
# USAGE
#   As a PreToolUse hook, fed the candidate command on stdin or as $*:
#     tools/agent-hooks/block-local-docker-db.sh "colima start"        -> exit 1
#     tools/agent-hooks/block-local-docker-db.sh "go test ./..."       -> exit 0
#   Or standalone to check the current environment:
#     tools/agent-hooks/block-local-docker-db.sh --check-env
#   Adversarial self-test (asserts the exception is narrow, not a hole):
#     tools/agent-hooks/block-local-docker-db.sh --self-test
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

# The admin DSN as THIS invocation would see it: the hook's own environment, or the candidate
# command text -- a land-main line carries it as an inline prefix, so the env is not set yet.
admin_dsn_for() {
  local cmd="$1"
  if [[ -n "${GOATOS_PGTEST_ADMIN_DSN:-}" ]]; then
    printf '%s' "$GOATOS_PGTEST_ADMIN_DSN"
    return 0
  fi
  # GOATOS_PGTEST_ADMIN_DSN=<value>, quoted or bare, up to the next whitespace or quote.
  printf '%s' "$cmd" | sed -n 's/.*GOATOS_PGTEST_ADMIN_DSN=["'"'"']\{0,1\}\([^"'"'"' ]*\).*/\1/p' | head -n1
}

# A DSN that would have pgtest CREATE/DROP databases on one we must never touch. Matched on the
# PORT, because that is what identifies these four on this laptop.
dsn_targets_protected_db() {
  grep -Eq '(127\.0\.0\.1|localhost|0\.0\.0\.0|\[::1\]):(15432|15433|5433|15544)([/?]|$)' <<<"$1"
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
  echo >&2
  echo "For the Postgres-gated tests specifically, the no-Docker path is already sanctioned:" >&2
  echo "  stand up your own throwaway cluster (initdb + pg_ctl on YOUR OWN port), then" >&2
  echo "  GOATOS_RUN_POSTGRES_TESTS=1 \\" >&2
  echo "  GOATOS_PGTEST_ADMIN_DSN=postgres://postgres@127.0.0.1:<port>/postgres?sslmode=disable ..." >&2
  echo "That starts no container and never touches 15432. Never aim it at 15432/15433/5433/15544." >&2
  exit 1
}

refuse_protected_dsn() {
  echo "BLOCKED: GOATOS_PGTEST_ADMIN_DSN points at a protected database" >&2
  echo >&2
  echo "The pgtest harness CREATEs and DROPs databases on whatever it is pointed at. 15432 is the" >&2
  echo "OCI staging-like clone (delta-only, never reset), 5433/15433 the legacy local stack, and" >&2
  echo "15544 the phone-QA throwaway another session may be mid-run on." >&2
  echo >&2
  echo "Use your OWN throwaway cluster on your OWN port:" >&2
  echo "  initdb -D <scratch>/pgdata -U postgres --auth=trust" >&2
  echo "  pg_ctl -D <scratch>/pgdata -o '-p <port> -c listen_addresses=127.0.0.1' -l <scratch>/pg.log start" >&2
  echo >&2
  echo "GOATOS_ALLOW_LOCAL_DOCKER_DB does NOT unlock this: that hatch is about RAM; this is about" >&2
  echo "not destroying the maintainer's data." >&2
  exit 1
}

if [[ "$candidate" == "--check-env" ]]; then
  if [[ "${GOATOS_RUN_POSTGRES_TESTS:-0}" == "1" ]]; then
    env_dsn="$(admin_dsn_for "")"
    if [[ -n "$env_dsn" ]]; then
      dsn_targets_protected_db "$env_dsn" && refuse_protected_dsn
    elif oci_tunnel_up; then
      refuse "GOATOS_RUN_POSTGRES_TESTS=1 is set while the OCI tunnel is up"
    fi
  fi
  exit 0
fi

if [[ "$candidate" == "--self-test" ]]; then
  self_test_fail=0
  # expect_exit <want> <label> <env-assignments...> -- <command text>
  expect_exit() {
    local want="$1" label="$2"; shift 2
    local envs=() ; while [[ "$1" != "--" ]]; do envs+=("$1"); shift; done; shift
    local got=0
    # `export` failing on a malformed assignment must FAIL the case, never pass it silently --
    # that false-green is the exact defect this self-test exists to catch elsewhere.
    ( export "${envs[@]}" || exit 99; "$0" "$1" >/dev/null 2>&1 ) || got=$?
    if [[ "$got" != "$want" ]]; then
      echo "SELF-TEST FAIL: $label -- want exit $want, got $got" >&2
      self_test_fail=1
    fi
  }
  DSN_OK='GOATOS_PGTEST_ADMIN_DSN=postgres://postgres@127.0.0.1:15751/postgres?sslmode=disable'
  DSN_OCI='GOATOS_PGTEST_ADMIN_DSN=postgres://postgres@127.0.0.1:15432/goatos?sslmode=disable'
  DSN_LEGACY='GOATOS_PGTEST_ADMIN_DSN=postgres://postgres@127.0.0.1:5433/goatos?sslmode=disable'
  DSN_PHONE='GOATOS_PGTEST_ADMIN_DSN=postgres://postgres@127.0.0.1:15544/goatos?sslmode=disable'
  # The exception: a throwaway port on its own is allowed, from the command text or the env.
  expect_exit 0 "own-port DSN in the command text" IGNORE=1 -- "cd backend && GOATOS_RUN_POSTGRES_TESTS=1 $DSN_OK go test ./internal/ceoai/..."
  expect_exit 0 "own-port DSN inherited from the env" "$DSN_OK" -- "GOATOS_RUN_POSTGRES_TESTS=1 go test ./internal/ceoai/..."
  expect_exit 0 "own-port DSN via --check-env" "$DSN_OK" GOATOS_RUN_POSTGRES_TESTS=1 -- "--check-env"
  expect_exit 1 "OCI DSN via --check-env" "$DSN_OCI" GOATOS_RUN_POSTGRES_TESTS=1 -- "--check-env"
  expect_exit 1 "OCI DSN inherited from the env" "$DSN_OCI" -- "GOATOS_RUN_POSTGRES_TESTS=1 go test ./..."
  # ...and it is NOT a hole: a protected target is refused whichever way it arrives.
  expect_exit 1 "OCI tunnel DSN in the command text" IGNORE=1 -- "GOATOS_RUN_POSTGRES_TESTS=1 $DSN_OCI go test ./..."
  expect_exit 1 "legacy 5433 DSN in the command text" IGNORE=1 -- "GOATOS_RUN_POSTGRES_TESTS=1 $DSN_LEGACY go test ./..."
  expect_exit 1 "phone-QA 15544 DSN in the command text" IGNORE=1 -- "GOATOS_RUN_POSTGRES_TESTS=1 $DSN_PHONE go test ./..."
  # The container bans the file was written for are untouched.
  expect_exit 1 "colima start is still refused" IGNORE=1 -- "colima start"
  expect_exit 1 "goatos-local-current is still refused" IGNORE=1 -- "docker start goatos-local-current"
  expect_exit 0 "an ordinary command still passes" IGNORE=1 -- "go test ./..."
  # The RAM hatch must not unlock a protected DSN.
  expect_exit 1 "ALLOW_LOCAL_DOCKER_DB does not unlock the OCI DSN" GOATOS_ALLOW_LOCAL_DOCKER_DB=0 -- "GOATOS_RUN_POSTGRES_TESTS=1 $DSN_OCI go test ./..."
  if [[ "$self_test_fail" == "0" ]]; then
    echo "block-local-docker-db self-test: OK"
    exit 0
  fi
  exit 1
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

# Postgres integration tests spin a throwaway CONTAINER through the pgtest harness -- unless an
# admin DSN is supplied, which is the sanctioned no-Docker path (see the header). The DSN itself is
# then the thing worth checking, and it is checked whether or not the tunnel happens to be up.
if grep -Eq 'GOATOS_RUN_POSTGRES_TESTS=1' <<<"$candidate"; then
  candidate_dsn="$(admin_dsn_for "$candidate")"
  if [[ -n "$candidate_dsn" ]]; then
    dsn_targets_protected_db "$candidate_dsn" && refuse_protected_dsn
  elif oci_tunnel_up; then
    refuse "GOATOS_RUN_POSTGRES_TESTS=1 while the OCI tunnel is up"
  fi
fi

exit 0
