#!/usr/bin/env bash
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
STATE_DIR="${GOATOS_CONTEXT7_STATE_DIR:-$REPO/.agent-docs/context7}"
STAMP="$STATE_DIR/deps.sha256"
LOCK="$STATE_DIR/refresh.lock"

if [[ "${GOATOS_CONTEXT7_AUTO_REFRESH:-1}" = "0" ]]; then
  exit 0
fi

mkdir -p "$STATE_DIR"

hash_inputs() {
  (
    cd "$REPO"
    {
      printf 'context7-docs.config.tsv\n'
      [[ -f tools/agent-docs/context7-docs.config.tsv ]] && sha256sum tools/agent-docs/context7-docs.config.tsv
      find . \
        \( -path './.git' -o -path './.agent-docs' -o -path './agent-docs' -o -path './node_modules' -o -path './.claude/worktrees' \) -prune -o \
        \( -name 'package.json' -o -name 'package-lock.json' -o -name 'pnpm-lock.yaml' -o -name 'yarn.lock' -o -name 'go.mod' -o -name 'go.sum' -o -name 'build.gradle.kts' -o -name 'settings.gradle.kts' -o -name 'gradle.properties' -o -name 'libs.versions.toml' \) \
        -type f -print0 \
        | sort -z \
        | xargs -0 sha256sum 2>/dev/null || true
    } | sha256sum | awk '{print $1}'
  )
}

new_hash="$(hash_inputs)"
old_hash="$(cat "$STAMP" 2>/dev/null || true)"
graph="$HOME/.cache/goatos/context7-docs/current/graphify-out/graph.json"

if [[ "$new_hash" = "$old_hash" && -s "$graph" ]]; then
  exit 0
fi

if ! mkdir "$LOCK" 2>/dev/null; then
  exit 0
fi
trap 'rmdir "$LOCK" 2>/dev/null || true' EXIT

if "$REPO/tools/agent-docs/ensure-context7.sh" >/dev/null 2>&1 &&
   "$REPO/tools/agent-docs/sync-context7-docs.sh" >/dev/null 2>&1; then
  printf '%s\n' "$new_hash" > "$STAMP"
fi
