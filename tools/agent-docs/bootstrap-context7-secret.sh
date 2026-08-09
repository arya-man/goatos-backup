#!/usr/bin/env bash
set -euo pipefail

PROJECT="${GOATOS_CONTEXT7_SECRET_PROJECT:-goatos-stg}"
CONTEXT7_SECRET="${GOATOS_CONTEXT7_SECRET_NAME:-context7-api-key}"
GEMINI_SECRET="${GOATOS_GEMINI_SECRET_NAME:-graphify-gemini-api-key}"
EXPECTED_ACCOUNT_DOMAIN="${GOATOS_EXPECTED_ACCOUNT_DOMAIN:-mesha.sg}"
ENV_DIR="${GOATOS_LOCAL_CONFIG_DIR:-$HOME/.config/goatos}"
ENV_FILE="$ENV_DIR/context7.env"

usage() {
  cat <<'USAGE'
Usage: tools/agent-docs/bootstrap-context7-secret.sh [--configure-codex] [--configure-claude]

Fetches Goat OS agent-doc API keys from Google Secret Manager into a local
gitignored env file:

  ~/.config/goatos/context7.env

The caller must have Secret Manager accessor permission for:

  project: goatos-stg
  secrets: context7-api-key, graphify-gemini-api-key

Options:
  --configure-codex   Register Context7 MCP with Codex using the local env var.
  --configure-claude  Register Context7 MCP with Claude Code using the key.
  -h, --help          Show this help.
USAGE
}

CONFIGURE_CODEX=0
CONFIGURE_CLAUDE=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --configure-codex)
      CONFIGURE_CODEX=1
      ;;
    --configure-claude)
      CONFIGURE_CLAUDE=1
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

if ! command -v gcloud >/dev/null 2>&1; then
  echo "gcloud is required. Install Google Cloud SDK, then rerun this script." >&2
  exit 1
fi

if [[ "$PROJECT" != "goatos-stg" && "${GOATOS_ALLOW_NON_STG_AGENT_DOCS_PROJECT:-}" != "1" ]]; then
  echo "Refusing to use project $PROJECT for Goat OS agent-doc secrets; expected goatos-stg." >&2
  echo "Set GOATOS_ALLOW_NON_STG_AGENT_DOCS_PROJECT=1 only for an explicit user-requested non-stg operation." >&2
  exit 1
fi

ACCOUNT="$(gcloud auth list --filter=status:ACTIVE --format='value(account)' | head -n 1)"
if [[ -z "$ACCOUNT" ]]; then
  echo "No active gcloud account. Run: gcloud auth login <your-mesha-email>" >&2
  exit 1
fi

case "$ACCOUNT" in
  *@"$EXPECTED_ACCOUNT_DOMAIN") ;;
  *)
    echo "Active gcloud account is $ACCOUNT, expected *@$EXPECTED_ACCOUNT_DOMAIN." >&2
    echo "Run: gcloud auth login <your-mesha-email>" >&2
    exit 1
    ;;
esac

if ! gcloud projects describe "$PROJECT" >/dev/null 2>&1; then
  echo "Cannot access project $PROJECT with account $ACCOUNT." >&2
  exit 1
fi

mkdir -p "$ENV_DIR"
chmod 700 "$ENV_DIR"

CONTEXT7_KEY="$(gcloud secrets versions access latest --project "$PROJECT" --secret "$CONTEXT7_SECRET")"
if [[ -z "$CONTEXT7_KEY" ]]; then
  echo "Secret $CONTEXT7_SECRET in $PROJECT returned an empty value." >&2
  exit 1
fi

GEMINI_KEY="$(gcloud secrets versions access latest --project "$PROJECT" --secret "$GEMINI_SECRET")"
if [[ -z "$GEMINI_KEY" ]]; then
  echo "Secret $GEMINI_SECRET in $PROJECT returned an empty value." >&2
  exit 1
fi

umask 077
cat > "$ENV_FILE" <<EOF
export CONTEXT7_API_KEY="$CONTEXT7_KEY"
export GOOGLE_API_KEY="$GEMINI_KEY"
export GEMINI_API_KEY="$GEMINI_KEY"
EOF
chmod 600 "$ENV_FILE"

echo "Wrote Goat OS agent-doc env file: $ENV_FILE"
echo "To load it in this shell: source \"$ENV_FILE\""

if [[ "$CONFIGURE_CODEX" -eq 1 ]]; then
  if ! command -v codex >/dev/null 2>&1; then
    echo "codex command not found; skipping Codex MCP registration." >&2
  else
    codex mcp add context7 -- zsh -lc 'source "$HOME/.config/goatos/context7.env"; exec npx -y @upstash/context7-mcp'
    echo "Registered Context7 MCP for Codex."
  fi
fi

if [[ "$CONFIGURE_CLAUDE" -eq 1 ]]; then
  if ! command -v claude >/dev/null 2>&1; then
    echo "claude command not found; skipping Claude MCP registration." >&2
  else
    claude mcp add --scope user context7 -- zsh -lc 'source "$HOME/.config/goatos/context7.env"; exec npx -y @upstash/context7-mcp --api-key "$CONTEXT7_API_KEY"'
    echo "Registered Context7 MCP for Claude Code."
  fi
fi
