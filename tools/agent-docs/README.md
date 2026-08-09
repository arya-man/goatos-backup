# Agent Docs Bootstrap

Goat OS can use Context7 for current framework/library documentation. Context7
and Gemini keys are stored in Google Secret Manager, not in git.

For this workspace, agent-doc secrets must live in `goatos-stg`. Do not use
`goatos-dev` for Context7, Gemini/Graphify, Claude/Codex bootstrap, local agent
docs, or developer setup unless the maintainer explicitly asks for it.

## Developer Setup

Authenticate with a Mesha Google account that has access to the secret:

```bash
gcloud auth login <your-mesha-email>
cd /path/to/goatos
tools/agent-docs/bootstrap-context7-secret.sh
source ~/.config/goatos/context7.env
```

Optional MCP registration:

```bash
tools/agent-docs/bootstrap-context7-secret.sh --configure-codex --configure-claude
source ~/.config/goatos/context7.env
```

Normal agent setup does this automatically:

```bash
make ai-setup
make ai-doctor
```

Those targets run `tools/agent-docs/ensure-context7.sh`. If the local key is
missing and `gcloud` is already logged in to a Mesha account with access, it
fetches the Context7 and Gemini keys from `goatos-stg` and registers Context7
MCP for Codex/Claude when those CLIs are installed. If Google auth is not
available, it skips without breaking unrelated repo checks.

To refresh the local framework docs cache:

```bash
tools/agent-docs/sync-context7-docs.sh
```

The script fetches only the narrow topics listed in
`tools/agent-docs/context7-docs.config.tsv`. It mirrors docs into a non-hidden
local path and copies them to `~/.cache/goatos/context7-docs/current` for
Graphify because hidden dot-directories and gitignored files are not a reliable
scan corpus for Graphify.

For a one-time high-quality semantic graph build, use Claude with an Anthropic
API key:

```bash
export ANTHROPIC_API_KEY="<anthropic-api-key>"
GOATOS_CONTEXT7_GRAPH_BACKEND=claude \
GOATOS_CONTEXT7_GRAPH_MODEL="REPLACE_WITH_FABLE_MODEL_ID" \
tools/agent-docs/sync-context7-docs.sh
```

For cheaper refreshes, keep the same backend and switch
`GOATOS_CONTEXT7_GRAPH_MODEL` to a Sonnet or Haiku model. If no provider key is
available, the docs cache still refreshes and agents can search/read the local
docs directly.

The default Gemini graph build uses `GOOGLE_API_KEY` / `GEMINI_API_KEY`, which
`bootstrap-context7-secret.sh` writes from the `goatos-stg` Secret Manager
secret `graphify-gemini-api-key`.

The script reads:

```text
project: goatos-stg
secrets: context7-api-key, graphify-gemini-api-key
```

It writes only a local file:

```text
~/.config/goatos/context7.env
```

Do not commit downloaded Context7 docs or local env files. Generated framework
docs and Graphify outputs belong under `.agent-docs/context7/` and are ignored.
