# MCP Toolbox — Local Runbook (Mesha Leadership Assistant)

This runbook explains how to run the Google **genai MCP Toolbox**
(`github.com/googleapis/genai-toolbox`) locally for the Mesha leadership
("CEO AI") assistant, how the tool catalog is structured, and how the Mesha
backend talks to it through the Go client in
`backend/internal/ceoai/toolboxclient`.

Owner surfaces:

- `docs/ceo-ai/mcp-toolbox-tools.yaml` — the Toolbox tool/source/toolset config.
- `tools/dev/run-mcp-toolbox-local.sh` — local launcher (download + serve + health).
- `backend/internal/ceoai/toolboxclient/**` — the Go HTTP client the backend uses.
- `docs/runbooks/mcp-toolbox-local.md` — this file.

## What Toolbox is (and is NOT) here

Toolbox is the **database tool server**. It gives the Vertex/Gemini planner a
controlled list of curated business tools over the `ceo_ai.*` reporting schema
instead of a raw database password or free-form SQL. It runs SQL against
Postgres through the **read-only** `mesha_ceo_readonly` role.

- The **browser never calls Toolbox.** Only the Mesha backend calls it,
  server-side, after it has verified the leadership role and bound `tenant_id`
  from the session.
- `tenant_id` is **always** the first parameter of every tool and is injected
  by the backend from the session — never from user or model text.
- Toolbox is **not** Vertex (that is the planner) and **not** Cube (that is the
  governed-metric service). Official KPIs route to Cube first; Toolbox serves
  operational/cross-module reads that no Cube metric or Mesha read API covers.

### Why the read-only SQL fallback is NOT a Toolbox tool

The plan's tier-4 read-only SQL fallback (`mesha_readonly_sql`) is **deliberately
excluded** from `mcp-toolbox-tools.yaml`. The Go backend `sqlguard`
(`backend/internal/ceoai/sqlguard`) validates and executes fallback `SELECT`s
against the same `mesha_ceo_readonly` role. Keeping the fallback in Go means one
validator/audit path owns the dynamic-SQL risk; Toolbox only ever serves the
curated, parameterized `ceo_ai.*` tools. Do **not** add a generic SQL tool to
the Toolbox config.

## Tool catalog

`mcp-toolbox-tools.yaml` defines one env-driven `postgres` source and **15**
curated tools, all in the `mesha_ceo_toolset` toolset. Each tool reads exactly
one business-language `ceo_ai.*` view, requires `tenant_id`, and caps rows with
`LIMIT LEAST($n, cap)`:

| Tool | Backing `ceo_ai.*` view | Leadership question |
| --- | --- | --- |
| `mesha_count_by_scope` | `animal_current_scope` | census by park/shed/species/stage/sex/status |
| `mesha_capacity_summary` | `shed_capacity_current` | over/under-capacity sheds |
| `mesha_vaccination_due_summary` | `vaccination_shed_status` | due/overdue/done by shed |
| `mesha_vaccination_dose_pickup` | `vaccination_dose_pickup` | which vaccines/doses to pick |
| `mesha_feed_direction_summary` | `feed_direction_current` | feed needed / blocked cells |
| `mesha_shifting_summary` | `counts_movement_daily` | births/deaths/shifts/approvals |
| `mesha_procurement_summary` | `procurement_pipeline` | open procurement loads |
| `mesha_source_entry_health` | `source_entry_health_status` | expected vs received/rejected |
| `mesha_ops_exceptions` | `ops_exception_queue` | highest-risk exceptions |
| `mesha_sop_execution` | `sop_execution_status` | SOP/shift task status |
| `mesha_verification_queue` | `verification_queue_status` | proof/verification backlog |
| `mesha_inventory_stock` | `inventory_stock_position` | stock on hand / reorder |
| `mesha_workforce_coverage` | `workforce_coverage_status` | ownership + backup coverage |
| `mesha_action_center` | `action_center_current` | cross-module action queue |
| `mesha_audit_summary` | `audit_activity_summary` | what happened today |

Every future leadership-relevant module must add a curated tool over its
`ceo_ai.*` view here (or a documented exclusion) in the same PR, per the
coverage contract in `docs/ceo-ai/mcp-toolbox-plan.md`.

## The read-only DB role (`mesha_ceo_readonly`)

Toolbox connects as `mesha_ceo_readonly`. This role is **provisioned
externally** (not by an app migration). Migration
`backend/migrations/postgres/000020_ceo_ai_reporting_schema.sql` creates the
`ceo_ai` schema and views and **grants** `SELECT`/`USAGE` to the role; it does
not create the login or its password.

Provision it once per environment (values from Secret Manager — never commit a
password):

```sql
CREATE ROLE mesha_ceo_readonly LOGIN PASSWORD '<from Secret Manager>';
ALTER ROLE mesha_ceo_readonly SET statement_timeout = '8000ms';
ALTER ROLE mesha_ceo_readonly SET idle_in_transaction_session_timeout = '5000ms';
ALTER ROLE mesha_ceo_readonly SET default_transaction_read_only = on;
```

Then apply migration 000020 so the schema/views exist and the grants land. The
role must have **no** write/DDL/GRANT ability and **no** access to `public`
tables — only `ceo_ai.*`.

## Secrets & config (Secret Manager → `.env.ceo-ai.local`)

All secrets/config live in **Google Secret Manager** (project `goatos-stg`, org
`vgoats.com`) for runtime and **GitHub Actions secrets** (repo `vgoats/goatos`)
for CI. Local dev pulls them into a **gitignored** `.env.ceo-ai.local`. Never
commit a secret value — only names and retrieval steps.

Before any `gcloud` read, confirm context: account `ravi@mesha.sg`, project
`goatos-stg`, org `vgoats.com`.

Create the local env file (names only; fill values from Secret Manager):

```bash
# .env.ceo-ai.local  (gitignored — DO NOT COMMIT)
MESHA_MCP_DB_HOST=127.0.0.1
MESHA_MCP_DB_PORT=5433
MESHA_MCP_DB_NAME=goatos
MESHA_MCP_DB_USER=mesha_ceo_readonly
MESHA_MCP_DB_PASSWORD=__from_secret_manager__

# The backend also reads these to reach Toolbox:
MESHA_MCP_TOOLBOX_URL=http://127.0.0.1:5001
MESHA_MCP_TOOLSET=mesha_ceo_toolset
```

The local launcher also accepts a full `MESHA_MCP_DB_DSN` and derives the split
`MESHA_MCP_DB_*` fields from it. `tools/dev/fetch-ceo-ai-secrets.sh` writes both
`MESHA_MCP_DB_DSN` and `MESHA_CEO_READONLY_DATABASE_URL` so Toolbox and the Go
SQL guard use the same read-only database target.

Fetch the read-only DB password from Secret Manager (example — adjust the
secret name to the one provisioned for your environment):

```bash
gcloud config set account ravi@mesha.sg
gcloud config set project goatos-stg
gcloud secrets versions access latest --secret=mesha-ceo-readonly-db-password \
  --project=goatos-stg
```

Put the value into `MESHA_MCP_DB_PASSWORD` in `.env.ceo-ai.local`.

## Run it locally

```bash
# start (or no-op if already healthy on :5001)
tools/dev/run-mcp-toolbox-local.sh

# replace a running instance
tools/dev/run-mcp-toolbox-local.sh --restart

# attached, Ctrl-C to stop
tools/dev/run-mcp-toolbox-local.sh --foreground

# stop
tools/dev/run-mcp-toolbox-local.sh --stop
```

The launcher:

1. sources `.env.ceo-ai.local` (falling back to already-exported
   `MESHA_MCP_DB_*`, with laptop defaults for host/port/name/user);
2. ensures the pinned `toolbox` binary (`TOOLBOX_VERSION`, default `0.32.0`)
   exists under `.toolbox-bin/` (downloads it if missing — the dir is
   gitignored);
3. starts Toolbox on `127.0.0.1:5001` serving `mcp-toolbox-tools.yaml` with the
   native `/api` endpoint enabled and `--allowed-hosts`/`--allowed-origins`
   restricted to localhost + the admin-web dev origin;
4. health-checks `GET /` and exits non-zero if it does not come up.

Verify the toolset manifest:

```bash
curl -s http://127.0.0.1:5001/api/toolset/mesha_ceo_toolset | jq '.tools | keys'
```

Invoke a tool directly (parameters as JSON; `tenant_id` required):

```bash
curl -s -X POST http://127.0.0.1:5001/api/tool/mesha_ops_exceptions/invoke \
  -H 'Content-Type: application/json' \
  -d '{"tenant_id":"<tenant-uuid>","limit":5}'
# -> {"result":"[{...rows...}]"}   (result is a JSON-encoded string)
```

## Backend HTTP client (`toolboxclient`)

`backend/internal/ceoai/toolboxclient` is a self-contained, stdlib-only client
(no parent-package import) over the Toolbox `/api` contract:

- `New(Config{BaseURL, Toolset, Timeout, MaxRetries})` — validates config.
- `LoadToolset(ctx)` — `GET /api/toolset/{toolset}` → decoded `Toolset` (tools +
  parameters) for planner discovery.
- `Invoke(ctx, tool, params)` — `POST /api/tool/{tool}/invoke` → rows as
  `json.RawMessage`. The backend places the session-bound `tenant_id` into
  `params`; the client passes params through verbatim.

Contract details:

- **Per-call deadline 8s** (`DefaultTimeout`), matching the assistant tool budget.
- **Retries** (default 2) on transport errors and HTTP 5xx/429 with bounded
  backoff; **no** retry on 4xx or tool-level errors (deterministic SQL).
- **Typed errors:** `*HTTPError` (status/transport), `*ToolError` (Toolbox
  returns HTTP 200 with an embedded `{"error":...}` result on SQL failure),
  and the sentinels `ErrConfig` / `ErrToolNotFound` / `ErrInvalidResponse`
  (match with `errors.Is` / `errors.As`).

Run the unit tests (httptest, no DB):

```bash
cd backend && go test ./internal/ceoai/toolboxclient/...
```

## Staging / production

Deploy Toolbox as its own Cloud Run service
(`mesha-mcp-toolbox-stg` / `-prod`, region `asia-south1`, internal/authenticated
ingress only) with its own service account, the `mesha_ceo_readonly` Cloud SQL
credential, and this same YAML mounted from Secret Manager. Recommended args:
`--config=/app/tools.yaml --address=0.0.0.0 --port=8080 --enable-api
--allowed-hosts=<toolbox-host> --allowed-origins=<none/internal> --telemetry-gcp`.
The backend reaches it via `MESHA_MCP_TOOLBOX_URL` / `MESHA_MCP_TOOLSET`. See
`docs/ceo-ai/mcp-toolbox-plan.md` for the full cloud resource contract.

## Troubleshooting

- **`relation "ceo_ai.<view>" does not exist`** on invoke → migration 000020 is
  not applied to the target DB, or Toolbox is pointed at the wrong database.
- **`password authentication failed for user "mesha_ceo_readonly"`** →
  `MESHA_MCP_DB_PASSWORD` does not match the provisioned role password.
- **`cannot execute ... in a read-only transaction`** → expected; the role is
  read-only by design. Never grant it write/DDL.
- **Port 5001 occupied by a non-healthy process** → the launcher refuses to
  fight it; free the port or set `MESHA_MCP_TOOLBOX_PORT`.
- **Binary version drift** → the launcher re-downloads when
  `.toolbox-bin/toolbox --version` != `TOOLBOX_VERSION`.
