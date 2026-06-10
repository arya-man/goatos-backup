#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cleanup_script="$repo_root/tools/dev/docker-cleanup-goatos.sh"
report_script="$repo_root/tools/dev/docker-storage-report.sh"

assert_contains() {
  local haystack="$1"
  local needle="$2"
  if [[ "$haystack" != *"$needle"* ]]; then
    echo "expected output to contain: $needle" >&2
    echo "actual output:" >&2
    printf '%s\n' "$haystack" >&2
    exit 1
  fi
}

assert_not_contains() {
  local haystack="$1"
  local needle="$2"
  if [[ "$haystack" == *"$needle"* ]]; then
    echo "expected output not to contain: $needle" >&2
    echo "actual output:" >&2
    printf '%s\n' "$haystack" >&2
    exit 1
  fi
}

bash -n "$report_script"
bash -n "$cleanup_script"

classification="$("$cleanup_script" --classify-only \
  goatos_tmp_alpha \
  goatos_test_beta \
  goatos_bench_tmp_gamma \
  goatos_dev_pg_data \
  goatos_current_work \
  postgres_data)"

assert_contains "$classification" $'delete\tgoatos_tmp_alpha'
assert_contains "$classification" $'delete\tgoatos_test_beta'
assert_contains "$classification" $'delete\tgoatos_bench_tmp_gamma'
assert_contains "$classification" $'protect\tgoatos_dev_pg_data'
assert_contains "$classification" $'skip\tgoatos_current_work'
assert_contains "$classification" $'skip\tpostgres_data'

tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/goatos-docker-cleanup-test.XXXXXX")"
trap 'rm -rf "$tmp_dir"' EXIT

fake_docker="$tmp_dir/docker"
fake_log="$tmp_dir/docker.log"
cat > "$fake_docker" <<'FAKE'
#!/usr/bin/env bash
set -euo pipefail

echo "$*" >> "${FAKE_DOCKER_LOG:?}"

case "${1:-}" in
  info)
    exit 0
    ;;
  volume)
    case "${2:-}" in
      ls)
        printf '%s\n' \
          goatos_tmp_alpha \
          goatos_test_beta \
          goatos_bench_tmp_gamma \
          goatos_dev_pg_data \
          goatos_current_work \
          other_volume
        exit 0
        ;;
      rm)
        exit 0
        ;;
    esac
    ;;
esac

echo "unexpected fake docker invocation: $*" >&2
exit 2
FAKE
chmod +x "$fake_docker"

run_cleanup_with_fake_docker() {
  : > "$fake_log"
  FAKE_DOCKER_LOG="$fake_log" DOCKER_CLEANUP_GOATOS_DOCKER_BIN="$fake_docker" "$cleanup_script" "$@"
}

dry_run_output="$(run_cleanup_with_fake_docker --delete-volumes)"
assert_contains "$dry_run_output" "Mode: dry-run"
assert_contains "$dry_run_output" "Dry-run only"
assert_not_contains "$(cat "$fake_log")" "volume rm"

execute_without_volume_flag="$(run_cleanup_with_fake_docker --execute)"
assert_contains "$execute_without_volume_flag" "No volumes will be deleted because --delete-volumes was not provided."
assert_not_contains "$(cat "$fake_log")" "volume rm"

execute_output="$(run_cleanup_with_fake_docker --execute --delete-volumes)"
assert_contains "$execute_output" "docker volume rm goatos_tmp_alpha"
assert_contains "$execute_output" "docker volume rm goatos_test_beta"
assert_contains "$execute_output" "docker volume rm goatos_bench_tmp_gamma"
assert_contains "$(cat "$fake_log")" "volume rm goatos_tmp_alpha"
assert_contains "$(cat "$fake_log")" "volume rm goatos_test_beta"
assert_contains "$(cat "$fake_log")" "volume rm goatos_bench_tmp_gamma"
assert_not_contains "$(cat "$fake_log")" "volume rm goatos_dev_pg_data"
assert_not_contains "$(cat "$fake_log")" "volume rm goatos_current_work"
assert_not_contains "$(cat "$fake_log")" "volume rm other_volume"

echo "Docker storage script tests passed."
