#!/usr/bin/env bash
set -euo pipefail

DOCKER_BIN="${DOCKER_STORAGE_REPORT_DOCKER_BIN:-docker}"

section() {
  printf '\n== %s ==\n' "$1"
}

docker_available() {
  if ! command -v "$DOCKER_BIN" >/dev/null 2>&1; then
    echo "Docker command not found: $DOCKER_BIN"
    echo "No Docker resources were inspected or modified."
    exit 0
  fi
  if ! "$DOCKER_BIN" info >/dev/null 2>&1; then
    echo "Docker is not running or is not reachable."
    echo "Start Docker Desktop and rerun this report if you need current usage."
    echo "No Docker resources were inspected or modified."
    exit 0
  fi
}

list_volumes() {
  "$DOCKER_BIN" volume ls --format '{{.Name}}'
}

docker_available

section "Docker System DF"
"$DOCKER_BIN" system df || true

section "Docker Detailed Usage"
echo "Includes image, container, local volume, and build cache usage as Docker reports it."
"$DOCKER_BIN" system df -v || true

section "Images Usage"
"$DOCKER_BIN" image ls --format 'table {{.Repository}}\t{{.Tag}}\t{{.ID}}\t{{.Size}}\t{{.CreatedSince}}' || true

section "Containers Usage"
"$DOCKER_BIN" container ls -a --size --format 'table {{.Names}}\t{{.ID}}\t{{.Status}}\t{{.Size}}\t{{.Image}}' || true

section "Volumes Usage"
echo "Docker does not expose per-volume size in all versions; see Docker Detailed Usage above when available."
"$DOCKER_BIN" volume ls --format 'table {{.Name}}\t{{.Driver}}\t{{.Scope}}' || true

section "Build Cache Usage"
if "$DOCKER_BIN" builder du >/dev/null 2>&1; then
  "$DOCKER_BIN" builder du || true
else
  echo "docker builder du is not available; see Docker System DF/Detailed Usage."
fi

section "Running Containers"
"$DOCKER_BIN" container ls --format 'table {{.Names}}\t{{.ID}}\t{{.Status}}\t{{.Image}}' || true

section "Stopped Containers"
"$DOCKER_BIN" container ls -a \
  --filter status=created \
  --filter status=exited \
  --filter status=dead \
  --size \
  --format 'table {{.Names}}\t{{.ID}}\t{{.Status}}\t{{.Size}}\t{{.Image}}' || true

section "Goat OS-Looking Volumes"
goatos_volumes="$(list_volumes | awk '/^goatos_/ { print }')"
if [ -n "$goatos_volumes" ]; then
  printf '%s\n' "$goatos_volumes"
else
  echo "No volumes with goatos_ prefix found."
fi

section "Anonymous Volumes"
anonymous_volumes="$(list_volumes | awk '/^[a-f0-9]{64}$/ { print }')"
anonymous_count="$(printf '%s\n' "$anonymous_volumes" | awk 'NF { count++ } END { print count + 0 }')"
echo "Anonymous volume count: $anonymous_count"
if [ "$anonymous_count" -gt 0 ]; then
  printf '%s\n' "$anonymous_volumes"
fi

section "Cleanup Honesty"
cat <<'NOTE'
This report is read-only and deletes nothing.

Docker build cache and dangling images are machine-wide, not Goat-OS-scoped.
Do not label global build-cache/image pruning as Goat OS-only cleanup.

On Docker Desktop for Mac, deleting Docker resources frees space inside the
Linux VM first. The host-side Docker.raw/VM disk image may still need Docker
Desktop disk reclaim/reset workflow before macOS Finder/System Settings shows
the space as available.
NOTE
