#!/usr/bin/env bash
# DRV-R3 local-DB mutation guard — shared by run-local-stack.sh and run-local-stack-supervised.sh.
#
# Pure, side-effect-free functions (safe to `source` from a test). They decide whether the local dev
# DATABASE_URL is a TRUSTED disposable local database that may be migrated/seeded without asking, or an
# UNTRUSTED target that requires an explicit `GOATOS_ALLOW_DB_MUTATION=1` opt-in.
#
# Trust rules:
#   - DATABASE_URL inherited from the environment  -> UNTRUSTED (could be a Cloud SQL Auth Proxy, which
#     also listens on 127.0.0.1, or an unrelated Postgres).
#   - A recognized Goat OS local docker container was detected  -> TRUSTED.
#   - No recognized docker container: the hardcoded 127.0.0.1:5433 fallback  -> UNTRUSTED (DRV-R3a: it
#     could be anything on that port).

# resolve_database_url <detect_fn_name>
#   Sets and exports DATABASE_URL and sets db_url_inherited (0 = trusted, 1 = untrusted).
#   <detect_fn_name> is the name of a function that echoes a detected docker DATABASE_URL or nothing;
#   passing it in keeps this testable with an injected fake detector.
resolve_database_url() {
  local detect_fn="${1:-detect_docker_database_url}"
  if [ -n "${DATABASE_URL:-}" ]; then
    db_url_inherited=1
    export DATABASE_URL
    return 0
  fi
  local detected
  detected="$("$detect_fn")"
  if [ -n "$detected" ]; then
    db_url_inherited=0
    export DATABASE_URL="$detected"
  else
    db_url_inherited=1
    export DATABASE_URL="postgres://postgres:goatos@127.0.0.1:5433/goatos?sslmode=disable"
  fi
}

# assert_mutable_local_db
#   Refuses (exit 1) to mutate an UNTRUSTED database unless GOATOS_ALLOW_DB_MUTATION=1|true is set.
assert_mutable_local_db() {
  if [ "${db_url_inherited:-1}" = "1" ] \
    && [ "${GOATOS_ALLOW_DB_MUTATION:-}" != "1" ] \
    && [ "${GOATOS_ALLOW_DB_MUTATION:-}" != "true" ]; then
    echo "Refusing to migrate/seed: DATABASE_URL is not a verified disposable LOCAL database" >&2
    echo "(inherited from the environment, or the no-docker 127.0.0.1:5433 fallback — a Cloud SQL Auth" >&2
    echo "Proxy also listens there). Set GOATOS_ALLOW_DB_MUTATION=1 to opt in, or unset DATABASE_URL to" >&2
    echo "use an auto-detected local docker database." >&2
    exit 1
  fi
}
