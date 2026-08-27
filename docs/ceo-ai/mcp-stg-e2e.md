# MCP Staging E2E and OCI Parity

This runbook is the release gate for treating a local MCP run as staging-equivalent.
It is read-only against staging and against the maintainer OCI clone.

## 1. Prove OCI DB parity with staging

Start the approved OCI tunnel from the main workspace:

```bash
$HOME/mesha/tools/local/oci-goatos-a1-dev.sh tunnel
```

In another shell, load the OCI database URL and staging URL:

```bash
source "${GOATOS_OCI_DB_ENV:-$HOME/mesha/local-data/goatos-stg-to-oci/oci-goatos-db.env}"
export GOATOS_OCI_DATABASE_URL="$DATABASE_URL"
export GOATOS_STG_DATABASE_URL="$(gcloud secrets versions access latest --secret=goatos-stg-database-url --project=goatos-stg)"
make oci-stg-db-parity
```

The parity script refuses any OCI DSN except the approved local tunnel
`127.0.0.1:15432`. It writes schema, inventory, and critical dashboard table
fingerprints to `tools/ceo-ai/eval/out/db-parity/` and fails on any diff.

## 2. Run external MCP JSON-RPC against staging APIs

Set a real staging bearer token and tenant id:

```bash
export MESHA_MCP_E2E_URL=https://mcp.mesha.sg/mcp
export MESHA_STG_API_BASE_URL=https://stg.api.mesha.sg
export MESHA_EVAL_BEARER="<staging bearer token>"
export GOATOS_EVAL_TENANT_ID="<tenant uuid>"
make e2e-mcp-smoke
```

The smoke calls MCP `tools/list`, then every read-only typed tool with baseline
and filter cases:

- Action Center, verification, feed, procurement, sales, counts, health, milk
  feeding, workforce, vaccination, and weighing.
- Sales filters: all farms, `CBE`, `CPT`, deals pagination.
- Weighing filters: all parks, selected park, `male`, `female`, shed weights,
  progress, process state, demographics.
- Composite tools: vaccination today and health today are compared against the
  exact staging API calls they aggregate.

For each typed call, the harness compares MCP `structuredContent.data` with the
canonical staging API JSON for the same tenant and arguments.

The same run also judges messy natural-language intent through MCP, including
typos and shorthand such as “sales revnue”, “weighng dashbord”, “procuremnet”,
and write/tenant-injection attempts. For dashboard reads, the harness resolves
the expected MCP tool, calls it, derives expected facts from the staging APIs, and
checks the returned MCP text/structured payload contains those facts. Safety
prompts still go through `ask_goatos` to verify refusal behavior. The report is
written to
`tools/ceo-ai/eval/out/mcp-stg-e2e-report.json`.

## 3. Run answer-quality eval

Use the normal live assistant eval after the MCP smoke:

```bash
export MESHA_ASSISTANT_URL="https://<staging-api>/ceo-ai/ask"
export GOATOS_EVAL_DATABASE_URL="$GOATOS_OCI_DATABASE_URL"
export GOATOS_EVAL_TENANT_ID="<tenant uuid>"
export MESHA_EVAL_BEARER="<staging bearer token>"
make ceo-ai-eval
```

Provider selection remains environment-driven. Vertex/GPT-compatible providers
should be wired through the existing CEO-AI config; local Ollama can only be used
on machines where `ollama` is installed and the backend provider supports that
endpoint. Do not bypass Cube/API/Toolbox routing to let a model query raw
Postgres.

## One command gate

After the env above is set, run the full gate:

```bash
make mcp-full-e2e
```

This chains OCI parity and judged MCP E2E. The broader DB-oracle
`make ceo-ai-eval` remains a separate assistant-quality gate for `/ceo-ai/ask`.
