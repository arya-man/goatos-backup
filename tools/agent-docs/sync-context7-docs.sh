#!/usr/bin/env bash
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CONFIG="${GOATOS_CONTEXT7_DOCS_CONFIG:-$REPO/tools/agent-docs/context7-docs.config.tsv}"
PRIMARY_DIR="${GOATOS_CONTEXT7_PRIMARY_DIR:-$REPO/.agent-docs/context7/current}"
GRAPH_DIR="${GOATOS_CONTEXT7_GRAPH_DIR:-$REPO/agent-docs/context7/current}"
GRAPHIFY_WORK_DIR="${GOATOS_CONTEXT7_GRAPHIFY_WORK_DIR:-$HOME/.cache/goatos/context7-docs}"
LOG_DIR="${GOATOS_CONTEXT7_LOG_DIR:-$REPO/.agent-docs/context7/logs}"
ENV_FILE="${GOATOS_CONTEXT7_ENV_FILE:-$HOME/.config/goatos/context7.env}"
GRAPH_BACKEND="${GOATOS_CONTEXT7_GRAPH_BACKEND:-gemini}"
GRAPH_MODEL="${GOATOS_CONTEXT7_GRAPH_MODEL:-${GRAPHIFY_GEMINI_MODEL:-gemini-3.1-pro-preview}}"
BUILD_GRAPH=1

usage() {
  cat <<'USAGE'
Usage: tools/agent-docs/sync-context7-docs.sh [--no-graph]

Fetch narrow, version-aware Context7 docs for the framework/library surfaces
Goat OS actually uses. Generated docs are local-only and gitignored.

Outputs:
  .agent-docs/context7/current/     primary local cache
  agent-docs/context7/current/      non-hidden mirror for Graphify scanning
  ~/.cache/goatos/context7-docs/    Graphify working copy and graph output
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --no-graph)
      BUILD_GRAPH=0
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
  shift
done

if [[ ! -f "$ENV_FILE" ]]; then
  "$REPO/tools/agent-docs/ensure-context7.sh" || true
fi

if [[ ! -f "$ENV_FILE" ]]; then
  echo "[context7-docs] missing $ENV_FILE; cannot fetch docs" >&2
  exit 0
fi

# shellcheck source=/dev/null
. "$ENV_FILE"
if [[ -z "${CONTEXT7_API_KEY:-}" ]]; then
  echo "[context7-docs] CONTEXT7_API_KEY is empty; cannot fetch docs" >&2
  exit 0
fi

mkdir -p "$PRIMARY_DIR" "$GRAPH_DIR" "$LOG_DIR"
FETCH_LOG="$LOG_DIR/sync.log"
: > "$FETCH_LOG"

while IFS=$'\t' read -r slug library query; do
  [[ -z "${slug:-}" || "$slug" == \#* ]] && continue
  out="$PRIMARY_DIR/${slug}.md"
  response="$PRIMARY_DIR/${slug}.response"
  printf '[context7-docs] fetch %s %s\n' "$slug" "$library" | tee -a "$FETCH_LOG"
  code="$(curl -sS -G -w '%{http_code}' -o "$response" \
    -H "Authorization: Bearer $CONTEXT7_API_KEY" \
    --data-urlencode "libraryId=$library" \
    --data-urlencode "query=$query" \
    --data-urlencode "type=txt" \
    'https://context7.com/api/v2/context' || true)"
  bytes="$(wc -c < "$response" | tr -d ' ')"
  if [[ "$code" == "200" && "$bytes" -gt 200 ]]; then
    {
      printf '# %s\n\n' "$slug"
      printf 'Context7 library: `%s`\n\n' "$library"
      printf 'Query: `%s`\n\n' "$query"
      printf 'Fetched: `%s`\n\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
      cat "$response"
    } > "$out"
    cp "$out" "$GRAPH_DIR/${slug}.md"
    printf '  ok bytes=%s\n' "$bytes" | tee -a "$FETCH_LOG"
  else
    printf '  fail http=%s bytes=%s\n' "$code" "$bytes" | tee -a "$FETCH_LOG"
  fi
  sleep 0.15
done < "$CONFIG"

count="$(find "$PRIMARY_DIR" -maxdepth 1 -name '*.md' -print | wc -l | tr -d ' ')"
bytes="$(find "$PRIMARY_DIR" -maxdepth 1 -name '*.md' -print0 | xargs -0 wc -c 2>/dev/null | tail -1 | awk '{print $1}')"
echo "[context7-docs] cached ${count} docs, ${bytes:-0} bytes"

if [[ "$BUILD_GRAPH" -eq 0 ]]; then
  exit 0
fi

case "$GRAPH_BACKEND" in
  gemini)
    if [[ -z "${GEMINI_API_KEY:-${GOOGLE_API_KEY:-}}" ]]; then
      echo "[context7-docs] skipping Graphify build: GEMINI_API_KEY/GOOGLE_API_KEY not set"
      exit 0
    fi
    ;;
  claude)
    if [[ -z "${ANTHROPIC_API_KEY:-}" ]]; then
      echo "[context7-docs] skipping Graphify build: ANTHROPIC_API_KEY not set"
      exit 0
    fi
    ;;
  openai)
    if [[ -z "${OPENAI_API_KEY:-}" ]]; then
      echo "[context7-docs] skipping Graphify build: OPENAI_API_KEY not set"
      exit 0
    fi
    ;;
esac

mkdir -p "$GRAPHIFY_WORK_DIR/current"
cp "$PRIMARY_DIR"/*.md "$GRAPHIFY_WORK_DIR/current/"
cp "$CONFIG" "$GRAPHIFY_WORK_DIR/current/topics.tsv"

if command -v uvx >/dev/null 2>&1; then
  if ! (
    cd "$GRAPHIFY_WORK_DIR"
    uvx --from 'graphifyy[gemini]==0.8.49' graphify extract current \
      --backend "$GRAPH_BACKEND" \
      --model "$GRAPH_MODEL" \
      --out "$GRAPHIFY_WORK_DIR/current"
  ); then
    echo "[context7-docs] Graphify build skipped/failed; docs cache is still available"
    exit 0
  fi
else
  if ! (
    cd "$GRAPHIFY_WORK_DIR"
    graphify extract current \
      --backend "$GRAPH_BACKEND" \
      --model "$GRAPH_MODEL" \
      --out "$GRAPHIFY_WORK_DIR/current"
  ); then
    echo "[context7-docs] Graphify build skipped/failed; docs cache is still available"
    exit 0
  fi
fi
