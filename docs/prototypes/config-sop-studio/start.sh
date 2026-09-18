#!/bin/sh
# Static, loopback-only CODEX prototype. Ctrl+C stops the server.
set -eu
prototype_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
prototype_port=${1:-4322}
case "$prototype_port" in ''|*[!0-9]*) echo 'Usage: sh start.sh [port]' >&2; exit 2;; esac
if [ "$prototype_port" -lt 1024 ] || [ "$prototype_port" -gt 65535 ]; then
  echo 'Choose a port from 1024 through 65535.' >&2
  exit 2
fi
command -v python3 >/dev/null 2>&1 || { echo 'Python 3 is required.' >&2; exit 1; }
printf 'CODEX prototype: http://127.0.0.1:%s/#/configuration/items\nStop with Ctrl+C. Browser drafts stay in local storage.\n' "$prototype_port"
exec python3 -m http.server "$prototype_port" --bind 127.0.0.1 --directory "$prototype_dir"
