#!/usr/bin/env bash
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ENV_FILE="${GOATOS_CONTEXT7_ENV_FILE:-$HOME/.config/goatos/context7.env}"
DOCTOR=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --doctor)
      DOCTOR=1
      ;;
    -h|--help)
      cat <<'USAGE'
Usage: tools/agent-docs/ensure-context7.sh [--doctor]

Ensures the local Context7 and Gemini keys are available for Goat OS agent
tooling. If the local env file is missing and gcloud is authenticated to a
Mesha account with Secret Manager access, this fetches the keys via
bootstrap-context7-secret.sh.

In CI, or when local Google auth is not available, this prints a short status
and exits successfully so ordinary repo checks remain portable.
USAGE
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      exit 2
      ;;
  esac
  shift
done

log() {
  printf '[context7] %s\n' "$*"
}

context7_api_ok() {
  (
    set +u
    [ -f "$ENV_FILE" ] && . "$ENV_FILE"
    [ -n "${CONTEXT7_API_KEY:-}" ] || exit 1
    code="$(curl -sS -o /dev/null -w '%{http_code}' \
      -H "Authorization: Bearer $CONTEXT7_API_KEY" \
      'https://context7.com/api/v2/libs/search?libraryName=react&query=hooks' 2>/dev/null || true)"
    [ "$code" = "200" ]
  )
}

gemini_api_ok() {
  (
    set +u
    [ -f "$ENV_FILE" ] && . "$ENV_FILE"
    key="${GEMINI_API_KEY:-${GOOGLE_API_KEY:-}}"
    [ -n "$key" ] || exit 1
    code="$(curl -sS -o /dev/null -w '%{http_code}' \
      -H "x-goog-api-key: $key" \
      'https://generativelanguage.googleapis.com/v1beta/models' 2>/dev/null || true)"
    [ "$code" = "200" ]
  )
}

codex_has_context7() {
  command -v codex >/dev/null 2>&1 || return 0
  codex mcp list 2>/dev/null | grep -q '^context7[[:space:]]'
}

claude_has_context7() {
  command -v claude >/dev/null 2>&1 || return 0
  claude mcp list 2>/dev/null | grep -qi 'context7'
}

maybe_configure_mcp() {
  if command -v codex >/dev/null 2>&1 && ! codex_has_context7; then
    log "Codex Context7 MCP missing; registering"
    codex mcp add context7 -- zsh -lc 'source "$HOME/.config/goatos/context7.env"; exec npx -y @upstash/context7-mcp' >/dev/null
  fi

  if command -v claude >/dev/null 2>&1 && ! claude_has_context7; then
    log "Claude Context7 MCP missing; registering"
    claude mcp add --scope user context7 -- zsh -lc 'source "$HOME/.config/goatos/context7.env"; exec npx -y @upstash/context7-mcp --api-key "$CONTEXT7_API_KEY"' >/dev/null
  fi
}

if [[ "${CI:-}" = "true" || "${GITHUB_ACTIONS:-}" = "true" ]]; then
  log "CI detected; skipping local Context7 bootstrap"
  exit 0
fi

if context7_api_ok && gemini_api_ok; then
  log "Context7 and Gemini API keys OK"
  maybe_configure_mcp || log "MCP auto-registration skipped or failed; env key is still OK"
  exit 0
fi

if ! command -v gcloud >/dev/null 2>&1; then
  log "gcloud not found; skipping Context7 bootstrap"
  exit 0
fi

ACCOUNT="$(gcloud auth list --filter=status:ACTIVE --format='value(account)' 2>/dev/null | head -n 1 || true)"
case "$ACCOUNT" in
  *@mesha.sg) ;;
  "")
    log "no active gcloud account; skipping Context7 bootstrap"
    exit 0
    ;;
  *)
    log "active gcloud account is $ACCOUNT, not a Mesha account; skipping Context7 bootstrap"
    exit 0
    ;;
esac

log "agent-doc keys missing; fetching from Google Secret Manager"
if "$REPO/tools/agent-docs/bootstrap-context7-secret.sh"; then
  if context7_api_ok && gemini_api_ok; then
    log "Context7/Gemini bootstrap OK"
    maybe_configure_mcp || log "MCP auto-registration skipped or failed; env key is still OK"
    exit 0
  fi
fi

if [[ "$DOCTOR" -eq 1 ]]; then
  log "agent-doc keys not ready; continuing doctor because this may be an unauthenticated local clone"
  exit 0
fi

log "agent-doc keys not ready; continuing without framework-doc cache"
exit 0
