#!/usr/bin/env bash
set -euo pipefail

DOCKER_BIN="${DOCKER_CLEANUP_GOATOS_DOCKER_BIN:-docker}"

execute=false
delete_volumes=false
classify_only=false

usage() {
  cat <<'USAGE'
Usage:
  tools/dev/docker-cleanup-goatos.sh [--delete-volumes] [--execute]
  tools/dev/docker-cleanup-goatos.sh --classify-only [volume ...]

Default mode is dry-run. The script deletes nothing unless --execute is passed.
Volume deletion also requires --delete-volumes.

Only volumes with these temp prefixes are deletable:
  goatos_tmp_
  goatos_test_
  goatos_bench_tmp_

Protected:
  goatos_dev_pg_data

Unclassified volumes are skipped. This script never runs docker volume prune and
never deletes images, build cache, or arbitrary named volumes.
USAGE
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --execute)
      execute=true
      shift
      ;;
    --delete-volumes)
      delete_volumes=true
      shift
      ;;
    --classify-only)
      classify_only=true
      shift
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    --*)
      echo "Unknown flag: $1" >&2
      usage >&2
      exit 2
      ;;
    *)
      break
      ;;
  esac
done

classify_volume() {
  case "$1" in
    goatos_dev_pg_data)
      echo "protect"
      ;;
    goatos_tmp_*|goatos_test_*|goatos_bench_tmp_*)
      echo "delete"
      ;;
    *)
      echo "skip"
      ;;
  esac
}

print_classification() {
  while IFS= read -r volume; do
    [ -n "$volume" ] || continue
    printf '%s\t%s\n' "$(classify_volume "$volume")" "$volume"
  done
}

run_classify_only() {
  if [ "$#" -gt 0 ]; then
    printf '%s\n' "$@" | print_classification
    return
  fi
  if [ -t 0 ]; then
    echo "No volume names provided for --classify-only." >&2
    exit 2
  fi
  print_classification
}

docker_available() {
  if ! command -v "$DOCKER_BIN" >/dev/null 2>&1; then
    echo "Docker command not found: $DOCKER_BIN"
    echo "No Docker resources were deleted."
    exit 0
  fi
  if ! "$DOCKER_BIN" info >/dev/null 2>&1; then
    echo "Docker is not running or is not reachable."
    echo "Start Docker Desktop and rerun this cleanup if needed."
    echo "No Docker resources were deleted."
    exit 0
  fi
}

if [ "$classify_only" = true ]; then
  run_classify_only "$@"
  exit 0
fi

if [ "$#" -gt 0 ]; then
  echo "Unexpected positional arguments: $*" >&2
  usage >&2
  exit 2
fi

docker_available

deletable_volumes=""
protected_volumes=""
skipped_volumes=""

while IFS= read -r volume; do
  [ -n "$volume" ] || continue
  case "$(classify_volume "$volume")" in
    delete)
      deletable_volumes="${deletable_volumes}${volume}"$'\n'
      ;;
    protect)
      protected_volumes="${protected_volumes}${volume}"$'\n'
      ;;
    skip)
      skipped_volumes="${skipped_volumes}${volume}"$'\n'
      ;;
  esac
done < <("$DOCKER_BIN" volume ls --format '{{.Name}}')

echo "Goat OS Docker cleanup"
if [ "$execute" = true ]; then
  echo "Mode: execute"
else
  echo "Mode: dry-run"
fi

echo
echo "Protected permanent volumes:"
if [ -n "$protected_volumes" ]; then
  printf '%s' "$protected_volumes"
else
  echo "(none found)"
fi

echo
echo "Unclassified volumes skipped:"
if [ -n "$skipped_volumes" ]; then
  printf '%s' "$skipped_volumes"
else
  echo "(none found)"
fi

echo
echo "Safe Goat OS temp volumes eligible for deletion:"
if [ -n "$deletable_volumes" ]; then
  printf '%s' "$deletable_volumes"
else
  echo "(none found)"
fi

echo
echo "Images, build cache, containers, anonymous volumes, and arbitrary named volumes are not deleted by this script."

if [ "$delete_volumes" != true ]; then
  echo
  echo "No volumes will be deleted because --delete-volumes was not provided."
  exit 0
fi

if [ "$execute" != true ]; then
  echo
  echo "Dry-run only. Re-run with --execute --delete-volumes to delete the eligible temp volumes above."
  exit 0
fi

if [ -z "$deletable_volumes" ]; then
  echo
  echo "No classified Goat OS temp volumes to delete."
  exit 0
fi

echo
echo "Deleting classified Goat OS temp volumes:"
while IFS= read -r volume; do
  [ -n "$volume" ] || continue
  echo "docker volume rm $volume"
  "$DOCKER_BIN" volume rm "$volume"
done <<< "$deletable_volumes"
