#!/usr/bin/env bash

postgres_ci_log_container() {
  local container_name="$1"

  echo "Docker Postgres logs for $container_name:" >&2
  docker logs "$container_name" >&2 || true
}

postgres_ci_is_transient_connection_error() {
  grep -Eiq \
    '(the database system is starting up|the database system is shutting down|database system is in recovery mode|could not connect to server|connection refused|server closed the connection unexpectedly|terminating connection due to administrator command)'
}

postgres_ci_wait_ready() {
  local container_name="$1"
  local db_user="$2"
  local db_name="$3"
  local attempts="${4:-90}"

  for _ in $(seq 1 "$attempts"); do
    if [ "$(docker inspect -f '{{.State.Running}}' "$container_name" 2>/dev/null || true)" != "true" ]; then
      postgres_ci_log_container "$container_name"
      echo "Docker Postgres container exited before becoming ready." >&2
      return 1
    fi

    if docker exec "$container_name" pg_isready -U "$db_user" -d "$db_name" >/dev/null 2>&1 &&
      docker exec "$container_name" psql -v ON_ERROR_STOP=1 -U "$db_user" -d "$db_name" -Atqc 'SELECT 1' >/dev/null 2>&1; then
      return 0
    fi

    sleep 1
  done

  postgres_ci_log_container "$container_name"
  echo "Docker Postgres did not become ready after $attempts seconds." >&2
  return 1
}

postgres_ci_psql() {
  local container_name="$1"
  local db_user="$2"
  local db_name="$3"
  shift 3

  local input_file
  input_file="$(mktemp "${TMPDIR:-/tmp}/goatos-psql.XXXXXX")"
  cat >"$input_file"

  local output
  local status
  for attempt in $(seq 1 5); do
    if output="$(docker exec -i "$container_name" psql -v ON_ERROR_STOP=1 -U "$db_user" -d "$db_name" "$@" <"$input_file" 2>&1)"; then
      printf '%s\n' "$output"
      rm -f "$input_file"
      return 0
    else
      status=$?
    fi

    if [ "$attempt" -lt 5 ] && printf '%s\n' "$output" | postgres_ci_is_transient_connection_error; then
      sleep "$attempt"
      continue
    fi

    printf '%s\n' "$output" >&2
    postgres_ci_log_container "$container_name"
    rm -f "$input_file"
    return "$status"
  done

  rm -f "$input_file"
}

postgres_ci_pg_dump() {
  local container_name="$1"
  shift

  local output
  local status
  for attempt in $(seq 1 5); do
    if output="$(docker exec "$container_name" pg_dump "$@" 2>&1)"; then
      printf '%s\n' "$output"
      return 0
    else
      status=$?
    fi

    if [ "$attempt" -lt 5 ] && printf '%s\n' "$output" | postgres_ci_is_transient_connection_error; then
      sleep "$attempt"
      continue
    fi

    printf '%s\n' "$output" >&2
    postgres_ci_log_container "$container_name"
    return "$status"
  done
}
