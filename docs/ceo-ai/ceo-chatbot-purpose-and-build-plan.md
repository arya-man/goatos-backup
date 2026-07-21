# Mesha CEO Chatbot Purpose And Build Plan

Status: source of truth for why the CEO chatbot exists and how Mesha builds it.

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

1. Existing Mesha read APIs and read models when they can answer the question.
2. MCP Toolbox business tools over curated `ceo_ai.*` reporting views.
3. Governed read-only SQL fallback when no API/tool exists yet.

The browser never talks directly to Gemini, MCP Toolbox, or Postgres. The Mesha
assistant API owns auth, tenant scope, tool choice, execution, validation,
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

## API, MCP, And SQL Split

Existing Mesha APIs stay as APIs. Do not wrap every REST endpoint as an MCP
tool. That creates duplicate contracts and noisy model choices.

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

Use SQL fallback only when no existing API or MCP business tool can answer the
question. SQL fallback must remain read-only, tenant-scoped, bounded, and
audited.

## Database Access Model

Mesha backend uses Go with `pgx` / `pgxpool` and typed SQL/sqlc-style adapters.
It does not use GORM.

The CEO chatbot does not need an ORM. It needs a controlled read layer:

- a dedicated read-only database role
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

## Required Reporting Views

The long-term stable interface for the CEO bot is `ceo_ai.*`, not raw normalized
tables. Initial view set:

- `ceo_ai.animal_current_scope`
- `ceo_ai.vaccination_shed_status`
- `ceo_ai.feed_direction_current`
- `ceo_ai.counts_movement_daily`
- `ceo_ai.procurement_pipeline`
- `ceo_ai.ops_exception_queue`
- `ceo_ai.sop_execution_status`
- `ceo_ai.inventory_stock_position`

These views should expose business-language columns such as `park_label`,
`shed_label`, `animal_count`, `due`, `done`, `blocked_reason`, `status`, and
`owner_label`.

## Future Developer Rule

Any new Mesha feature that creates leadership-relevant operational data must
update the CEO chatbot context in the same pull request.

The required update is one of:

- add or update a read API mapping
- add or update a `ceo_ai.*` reporting view
- add or update an MCP Toolbox tool
- document why the feature should not be visible to the CEO chatbot

This applies to all developers and all coding agents, including Claude and
Codex. If a feature affects counts, vaccination, feed, shifting, procurement,
inventory, SOP execution, verification, workforce, action center, or audit
visibility, the CEO chatbot context must be reviewed.

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
- stale-context check when new read APIs or reporting tables are added

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

