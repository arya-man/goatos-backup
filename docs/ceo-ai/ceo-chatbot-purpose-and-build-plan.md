# Mesha CEO Chatbot Purpose And Build Plan

Status: source of truth for why the leadership assistant exists and how Mesha
will build it. This is not an implementation-complete document.

Implemented now:

- dashboard bubble/chat shell
- server-side leadership gate
- safe read-only API demo routing
- assistant coverage docs/skills/hooks/local-CI guardrails

Not implemented yet:

- Google ADK agent runtime
- Vertex/Gemini production planner in the Mesha backend
- Cube Core metric service
- persisted memory/session store
- MCP Toolbox runtime service
- complete `ceo_ai.*` reporting schema
- safe SQL fallback executor
- production retry/fallback orchestration
- assistant audit/metrics persistence

## Purpose

The Mesha CEO chatbot is a leadership-only operating assistant inside the Mesha
dashboard. It exists so CEO/CXO users can ask plain-English questions about live
operations and get short, correct, sourced answers without opening every module
manually.

The bot is not a general employee chatbot. It is not a write surface. It is not a
replacement for domain workflows. It is a read-only leadership lens over Mesha
data.

Example questions it must support:

- How many animals are in Castro 1?
- How many goats versus sheep do we have today?
- Which sheds are overdue for vaccination?
- What vaccination work is due today?
- Which feed directions are blocked?
- Which shifts are pending execution?
- What procurement loads are open?
- What are the highest-risk operational exceptions right now?

Important vocabulary:

- `animal` means all species Mesha tracks, including goats and sheep.
- `goat` means only `species = 'goat'`.
- `sheep` means only `species = 'sheep'`.

## User Experience

The assistant appears as a fixed floating bubble in the dashboard for CEO/CXO
users only. Clicking the bubble opens the chat panel. The panel must remain
inside the browser window and must not rely on dragging.

Answers should be concise by default. For count questions, return aggregate
totals first. Do not list individual animals unless the CEO explicitly asks for
animal records and an approved detail tool exists.

Every answer should include enough source metadata for trust: source surface,
freshness/date, and whether Gemini planned the tool call.

## Runtime Architecture

```text
CEO/CXO dashboard bubble
  -> Mesha assistant API
  -> leadership + tenant verification
  -> Gemini on Vertex AI plans the request
  -> Mesha chooses the safest available read path
  -> answer composer returns a short sourced answer
```

Read paths are used in this order:

1. Cube governed metrics when the question asks for an official KPI.
2. Existing Mesha read APIs and read models when they can answer the question.
3. MCP Toolbox business tools over curated `ceo_ai.*` reporting views.
4. Governed read-only SQL fallback when no metric/API/tool exists yet.

The browser never talks directly to Gemini, Cube, MCP Toolbox, or Postgres. The
Mesha assistant API owns auth, tenant scope, tool choice, execution, validation,
formatting, and audit.

## Vertex AI Role

Vertex AI is the Google Cloud runtime used to call Gemini.

Gemini's job is planning and extraction:

- classify the question domain
- choose an allowed tool
- extract parameters such as shed, park, date, status, species, or limit
- when allowed, draft a read-only SQL query over approved reporting schema

Gemini must not hold database credentials. Gemini must not execute SQL. Gemini
must not decide permissions.

## Cube Role

Cube is the governed metric service. It is not Gen AI and it is not Vertex AI.
It is the official formula layer for business numbers such as active animals,
vaccination overdue, mortality rate, feed cost, procurement cost, and operator
completion rate.

Cube still queries data. In local and early staging it should query Postgres
through a read-only database user. As BigQuery/dbt marts become available, Cube
can point official historical metrics to those marts. The important rule is that
official KPI definitions live in Cube, not in dashboard components, prompts, or
one-off SQL.

Use Cube when the question is asking for a leadership KPI, trend, comparison, or
slice that has an approved metric:

```text
User asks: "How many vaccinations are overdue by park?"
Gemini plans: metric = vaccination_overdue, dimension = park
Mesha assistant API calls Cube
Cube runs the approved SQL against Postgres or BigQuery
Mesha assistant API reviews, formats, and returns the answer
```

Cube does not replace APIs, MCP Toolbox, Postgres, BigQuery, or Vertex AI:

- Vertex/Gemini understands the English question and chooses the route.
- Cube calculates official metrics.
- Mesha APIs return app/workflow-shaped operational reads.
- MCP Toolbox exposes controlled database tools and curated reporting reads.
- Read-only SQL is fallback when no governed metric or read tool exists.

## API, MCP, And SQL Split

Existing Mesha APIs stay as APIs. Do not wrap every REST endpoint as an MCP
tool. That creates duplicate contracts and noisy model choices.

Use Cube for official governed metrics:

- active animal count
- vaccination due and overdue
- vaccination compliance
- mortality and deaths by period
- procurement cost and load metrics
- feed cost and consumption metrics
- operator completion and backlog metrics

Use APIs for app-shaped reads that already exist:

- counts breakdown
- herd summary
- vaccination execution
- vaccination action center
- feed direction preview
- feed packing worklist
- procurement load lists
- action center queues
- audit summaries

Use MCP Toolbox for business analytics tools and direct database reads:

- count by scope
- vaccination due summary
- feed direction summary
- operations exceptions
- safe read-only SQL over `ceo_ai.*`

Use SQL fallback only when no Cube metric, existing API, or MCP business tool can
answer the question. SQL fallback must remain read-only, tenant-scoped, bounded,
and audited.

## Local And Staging Cube Setup

Local development should prove the full path before staging:

```text
local dashboard on :3300
  -> local Mesha assistant API
  -> local or mocked Vertex/Gemini planner
  -> local Cube Core on a configured local port, for example 127.0.0.1:4000
  -> local Postgres read-only user
```

Local requirements:

- Cube runs as a separate service, not inside the browser. The local port is
  just configuration through `MESHA_CUBE_URL`; `127.0.0.1:4000` is only the
  recommended default for laptop testing.
- Cube connects to the developer's local Postgres through a read-only user.
- The assistant API calls Cube server-side after leadership/tenant checks.
- If Cube has no metric for a question, the assistant tries Mesha read APIs,
  then MCP/reporting tools, then validated read-only SQL fallback.

Staging should mirror production shape:

```text
stg dashboard
  -> stg Mesha assistant API
  -> Vertex AI / Gemini in the stg GCP project
  -> mesha-cube-stg on Cloud Run
  -> stg Cloud SQL Postgres read-only user
  -> BigQuery/dbt marts later for historical governed metrics
```

Recommended staging services:

```text
mesha-assistant-stg:
  Mesha backend route that owns auth, tenant scope, planning, orchestration,
  memory, retries, fallback, answer review, and audit.

mesha-cube-stg:
  Cube Core Cloud Run service for governed metrics.

mesha-mcp-toolbox-stg:
  MCP Toolbox Cloud Run service for curated database tools and fallback views.

stg Cloud SQL Postgres:
  live operational data, accessed by Cube and Toolbox through dedicated
  read-only users only.

Secret Manager:
  Cube DB password, Cube API secret/JWT secret, Toolbox config, assistant
  service credentials, and Vertex configuration.
```

Only the Mesha backend should call Cube in staging. The browser should call only
the Mesha assistant endpoint.

Initial environment variables:

```text
MESHA_CUBE_URL=http://127.0.0.1:4000
MESHA_CUBE_API_SECRET=<local secret>
MESHA_CUBE_DB_USER=mesha_cube_readonly
MESHA_CUBE_DB_PASSWORD=<local/stg secret>
MESHA_CUBE_DB_NAME=<database name>
MESHA_CUBE_DB_HOST=<local or Cloud SQL host>
MESHA_CUBE_DB_PORT=5432
```

For staging, store secrets in Secret Manager and run Cube on Cloud Run with
authenticated internal service-to-service access.

## Database Access Model

Mesha backend uses Go with `pgx` / `pgxpool` and typed SQL/sqlc-style adapters.
It does not use GORM.

The CEO chatbot does not need an ORM. It needs a controlled read layer:

- a dedicated read-only database role
- Cube metrics for official KPIs
- stable `ceo_ai.*` reporting views
- MCP Toolbox tools over those views
- server-side SQL validation for fallback queries
- audit logs for every answer

The read-only database role must not inherit application write permissions. It
must not have `INSERT`, `UPDATE`, `DELETE`, `TRUNCATE`, `CREATE`, `DROP`,
`ALTER`, `GRANT`, `REVOKE`, or sequence privileges.

## SQL Safety Rules

Generated SQL must obey all rules:

- exactly one `SELECT`
- no semicolon-separated multiple statements
- no comments
- no DML, DDL, transaction, extension, admin, or function-execution commands
- only approved tables/views
- tenant filter is mandatory for tenant-scoped data
- row-returning queries must have `LIMIT`
- maximum fallback result size is 100 rows
- aggregate answers are preferred over raw rows

Unsafe SQL must be rejected by the server even if Gemini produced it.

## Required Reporting Coverage

The long-term stable interface for the leadership assistant is complete Mesha
coverage through read APIs, MCP business tools, and `ceo_ai.*` reporting views.
This is not a starter-only list. The assistant must cover every current and
future leadership-relevant module.

Required coverage includes:

- `ceo_ai.animal_current_scope`
- `ceo_ai.shed_capacity_current`
- `ceo_ai.vaccination_shed_status`
- `ceo_ai.vaccination_dose_pickup`
- `ceo_ai.feed_direction_current`
- `ceo_ai.counts_movement_daily`
- `ceo_ai.procurement_pipeline`
- `ceo_ai.source_entry_health_status`
- `ceo_ai.ops_exception_queue`
- `ceo_ai.sop_execution_status`
- `ceo_ai.verification_queue_status`
- `ceo_ai.inventory_stock_position`
- `ceo_ai.workforce_coverage_status`
- `ceo_ai.action_center_current`
- `ceo_ai.audit_activity_summary`

These views should expose business-language columns such as `park_label`,
`shed_label`, `animal_count`, `due`, `done`, `blocked_reason`, `status`, and
`owner_label`.

## Future Developer Rule

Any new Mesha feature that creates leadership-relevant operational data must
update the leadership assistant context in the same pull request.

The required update is one of:

- add or update a read API mapping
- add or update a `ceo_ai.*` reporting view
- add or update an MCP Toolbox tool
- document why the feature should not be visible to the CEO chatbot

This applies to all developers and all coding agents, including Claude and
Codex. If a feature affects counts, vaccination, dose pickup, feed, shifting,
procurement, source entry, inventory, SOP execution, verification, workforce,
action center, audit visibility, health/exception state, or any future operating
module, the leadership assistant context must be reviewed.

The PR must either add the assistant read path or explicitly document why the
feature is excluded from leadership visibility. Silent gaps are not allowed.

Canonical files:

- `docs/ceo-ai/ceo-chatbot-purpose-and-build-plan.md`
- `docs/ceo-ai/mcp-toolbox-plan.md`
- `docs/ceo-ai/mcp-toolbox-tools.yaml`
- `context/agents/ceo-bot-analytics-context.md`

## Rollout Phases

Phase 1: local/staging assistant bubble

- CEO/CXO-only visibility
- fixed bubble and chat panel
- Vertex/Gemini planner
- existing Mesha read APIs for counts, vaccination, and feed
- concise answer formatting

Phase 2: MCP Toolbox and reporting schema

- create read-only database role
- create `ceo_ai.*` views
- deploy MCP Toolbox on Cloud Run
- connect Mesha assistant API to Toolbox server-side
- add tool audit logs

Phase 3: SQL fallback

- allow Gemini to draft SQL only over approved schema
- validate SQL server-side
- execute using read-only role
- format aggregate answers
- add guardrail tests

Phase 4: automatic feature coverage

- CI check for leadership-relevant feature changes
- agent instructions for Claude/Codex
- PR checklist requiring CEO chatbot context updates
- stale-context check when new read APIs, routes, modules, reporting tables, or
  operating workflows are added

## Definition Of Done

A CEO chatbot capability is done only when:

- the answer uses live Mesha data
- the route is CEO/CXO gated server-side
- tenant scope is enforced server-side
- the bot can explain its source
- raw lists are avoided unless explicitly requested
- SQL, if used, is read-only and validated
- the relevant docs/context are updated
- local typecheck and guardrail checks pass
