# Ask Mesha coding agent (leadership chat)

Read this before touching the admin-web **Ask Mesha** panel, `apps/admin-web/app/api/ceo-ai/*`,
`tools/ask-mesha-agent/`, the `mesha-data-map` skill, or the `_ASK_MESHA_*` Cloud Build steps.

## What it is

- The 4 CEOs use the existing Ask Mesha panel in admin-web. With the flag set, it is answered by
  a **Claude Agent SDK agent** that reads the goatos code (read-only) and queries goatos-stg
  **read-only**, instead of the legacy Go/Vertex `ceo-ai` backend.
- **Kill switch / flag:** `CEO_AI_AGENT_URL` on admin-web (`apps/admin-web/app/api/ceo-ai/_forward.ts`).
  Unset => legacy backend `ceo-ai`, unchanged. Never modify the legacy backend to make the agent work.
- Same HTTP contract as the backend: `/ceo-ai/starters`, `/conversations` (list/create/get/patch/soft
  delete), `/conversations/:id/messages`, `/conversations/:id/files/:fileId`, `/ask` (SSE: `progress`,
  `token`, `final{answer, chart, conversation_id, message_id}`, `error`), `/metrics`, `/healthz`.

## Audience rules (non-negotiable)

- Answers are for CEOs: business language, direct answer first, one compact table, <= 3 bullets.
  HARD RULE: never mention or offer code, files, SQL/queries, databases/views/columns, tools,
  sessions, budgets or the agent's limits ("I checked the code", "I can trace it in the code",
  "I'm low on budget" are bugs). Speak as Mesha's analyst ("the dashboard calculates it by…").
  Activity-step labels are business wording too ("Checking weighing records"). Code/SQL only if asked.
- Never invent a metric proxy when an exact definition exists; if a metric can't be reproduced
  from read-only data, say so (see the data map's "Metric definitions" and "Known gaps").
- UI: no citation pill / "coding-agent · Live data" footer on agent answers; no greeting message;
  copy is a small icon under the answer (not a text button); keep Mesha brand tokens (tints, not new colours).

## Read-only guarantees (keep all four layers)

1. **Tools:** `ASK_MESHA_READONLY=1` (default) gives the agent only `Read/Grep/Glob/Skill/TodoWrite`
   plus the MCP tools `run_sql` and `mcp__mesha__describe_table` (fixed catalog read of up to 6 tables per
   call — `table` comma list and/or `tables` array — returning columns, FK join targets and, for base tables
   <= ~2M rows, top values of up to 6 category/status-like text columns; every name must match strict
   `schema.table` identifiers before it is interpolated, and all reads go through `run_sql`'s same read-only
   path). A `run_sql` "column/relation does not exist" error comes back with the real column lists of up to
   4 tables the query referenced (same validated describe path). No Bash, Edit, Write, NotebookEdit, Web*, Task. Read paths are limited
   to the repo and upload dirs (no `/proc`, no `.pgenv`).
2. **run_sql:** no query rules — any SQL over any table/schema, no tenant filter (single tenant). Runs in
   `BEGIN READ ONLY` with `default_transaction_read_only=on`, 60 s timeout, 500 rows. Refusals are only
   about execution shape, not data access: psql backslash commands (they run programs on the host, e.g.
   `\!`), more than one statement (`;` inside the query), and statements starting with
   commit/rollback/end/abort/set/reset/begin/start (so the model can't step out of `BEGIN READ ONLY`).
   The READ ONLY transaction is still a guard rail, **not** a guarantee; only layer 4 (the role) is.
3. **Chat privacy:** `mesha_ceo_readonly` has NO access to assistant chat tables (`ceo_ai_conversations`, `ceo_ai_messages`, `ceo_ai_assistant_audit`, `ceo_ai_response_cache`, `ceo_ai_rate_limit`, and never the `ask_mesha` schema); each CEO sees only their own chats (service-enforced ownership).
4. **Platform (the real guarantee):** DB role `mesha_ceo_readonly` has SELECT on **every table** in public/analytics/audit/ceo_ai/forensic_repair (+ default privileges for new tables) and **no write privilege anywhere** (granted 2026-09-24 via audit.begin_change; revoke `dblink` + `public` CREATE —
   RUNBOOK §3d); container runs non-root with the repo baked **read-only** at `/repo`; no git/GitHub/cloud
   credentials; agent env is an allow-list (`agentEnv()` in `server.mjs`).
- Proof to re-run after changes: ask "edit AGENTS.md" and "git push --force" — both must be refused and
  the checkout unchanged; a data question must still answer with 1 query.

## Multi-tenant isolation — REQUIRED before onboarding a 2nd tenant

Today goatos-stg has exactly **one tenant (Mesha)**, so the CEO agent reads every table with a single
read-only login and no tenant filter (maintainer decision 2026-09-24). **This is only safe while there is
one tenant.** Before any second tenant's data is loaded, do all of the following in one PR (land via
`make land-main`; follow the db-migration-safety skill):

1. **Tie the chat to the asking user's tenant (session level).** The agent already authenticates the
   caller (Firebase bearer → STG API) and knows `user.tenantId` from `X-GoatOS-Tenant-ID`; chats are
   already owned by email + tenant. The server — never the model — must pick the DB login for that tenant.
2. **Enforce it in the database (row level).** Prompt rules ("always filter tenant_id") are NOT isolation.
   - One read-only login per tenant (e.g. `ceo_ro_<tenant>`), all members of a `ceo_readers` group role,
     with **no write privilege** anywhere; mapping table `ceo.reader_tenants(role name, tenant_id uuid)`.
   - `ENABLE ROW LEVEL SECURITY` + a `FOR SELECT TO ceo_readers USING (tenant_id = ceo.current_reader_tenant())`
     policy on **every table with a `tenant_id` column** (322 of 343 base tables on 2026-09-24; generate the
     policies in a loop in the migration). `goatos_app` is unaffected (policies target `ceo_readers` only).
   - The ~21 tables without `tenant_id` are shared reference/lookup data; review each — anything that is
     actually tenant data must gain `tenant_id` first.
   - `run_sql` connects as the tenant's login. The model cannot switch tenants because it cannot change
     the connected role (no `SET ROLE` membership).
3. **Guard:** CI check that fails when a table with `tenant_id` lacks the `ceo_readers` policy, plus an
   adversarial self-test (a fake second tenant must see **zero** rows through the agent).
4. **Proof before enabling:** Mesha login sees Mesha rows; a second-tenant login sees only its own rows
   for the same free-form SQL (`SELECT * FROM public.feed_purchases`), and agent answers are unchanged.

Not the answer: per-tenant copies of tables/schemas (344× duplication and migration pain) or indexes
(speed only, no protection). `tenant_id` indexes should still exist on large tables for RLS performance.

## Data map (how it stays fast and correct)

- `.agents/skills/mesha-data-map/` (linked from `.claude/skills/`): routing table, **metric definitions
  that match the dashboard** (tested SQL), known gaps, and `references/views.generated.md` (generated —
  never hand-edit). Always-loaded short form: `tools/ask-mesha-agent/data-map-core.md`.
- Regenerate: `node tools/ask-mesha-agent/gen-data-map.mjs`; guard: `make mesha-data-map-guard`
  (full check with DB env, object-set check without).
- Daily refresh job: `tools/ask-mesha-agent/refresh-data-map.sh` (isolated worktree of origin/main,
  read-only SQL via `ro-sql.sh`, opens a PR, **never merges**). Schedules in `tools/ask-mesha-agent/schedule/`.
- Known gap: per-animal ADG (dashboard 162 g) is not reproducible from `ceo_ai.*`; needs a
  `ceo_ai.weighing_observations` view (sketch in SKILL.md "Known gaps"). Feed `fed_kg` is always 0.

- **Live table index:** at startup and hourly, `server.mjs` lists every readable table (with approximate row
  counts) from the database catalog into the system prompt. New tables show up without editing the map; the
  map only adds meanings and traps. The agent must search this list before saying "not recorded".

## Performance (response time is the benchmark)

- Every answer records a timing event (first progress/tool/token, total, tool + DB-query counts, tokens,
  cost) to the metrics store; `GET /metrics` returns p50/p90. `node tools/ask-mesha-agent/bench.mjs`
  drives the real `/ask` path (local only; `ASK_MESHA_BENCH_TOKEN` is ignored on Cloud Run).
- Why it's fast: `CLAUDE.md`/`AGENTS.md` are injected into the **system prompt** (cached, 1 h TTL via
  `ENABLE_PROMPT_CACHING_1H=1`, `CLAUDE_CODE_DISABLE_CLAUDE_MDS=1`) and all chats share one checkout,
  so the large prefix is read from cache instead of re-written per chat. Per-chat worktrees break the
  cache; keep `ASK_MESHA_WORKTREE_PER_CHAT` off. Quick lookups: `claude-sonnet-5`, effort `low`.
  Investigations (attachment, `deep:` prefix, or verify/check/why/bug/wrong/explain…) automatically use
  `claude-opus-5-5`, effort `high`, and must explain with a worked example (readings, arithmetic, verdict).
  Baseline: 84 s / $0.64 → lookups ~15–30 s / ~$0.10; investigations ~1–2 min / ~$1.
- Streaming: server sends `reset` before a tool call (pre-tool narration is cleared); the panel
  typewriter reveals tokens per frame; a Codex-style activity trail shows plain-English steps and
  "Worked for Xm · N steps".
- Repo hook `ai-setup-guard` blocks tools in fresh clones; the agent sets the documented
  `GOATOS_AI_SETUP_GUARD=0`.

## Observability (per user)

- `tools/ask-mesha-agent/events.mjs` emits one structured JSON line per event on stdout
  (`{severity, message, event_name, component:"ask-mesha-agent", ts, request_id, chat_id, email, tenant_id, ...}`,
  same shape as admin-web's `admin_backend_api_fetch`) and persists it: Postgres `ask_mesha.events`
  (`sql/002_events.sql`, idempotent, applied with `ASK_MESHA_DB_MIGRATE=1`) or `$STATE/events.jsonl`.
- Exactly one terminal event per ask: `ask_completed` (total_ms, first_token_ms, tool_calls, db_queries,
  tool_errors, sql_errors, turns, model, effort, deep, cost_usd, input/output tokens, answer_chars, chart),
  `ask_failed` (`error_class`: auth | budget_blocked | per_answer_cap | sdk_error | db_error | timeout |
  unknown, + `error`), `ask_stopped` (client closed the stream; `error_class=client_aborted`).
  Signals: `ask_started` (model, effort, deep, question_preview = first 80 chars), `ask_first_token`,
  `ask_tool` (tool, plain label, duration_ms tool_use→tool_result, ok, error), `budget_warning` (>= 80 %,
  once per month per instance), `budget_blocked`, `auth_denied` (path), `attachment_saved`.
- Where to look: `GET /metrics/users` (per email, today/7d/30d: asks, success, failed by class, stopped,
  success rate, p50/p90 total and first-token, avg tools, cost) and `GET /metrics/recent?email=` (last 50
  asks: status, duration, question preview). Same auth as `/metrics`. Cloud Logging:
  `jsonPayload.component="ask-mesha-agent"`. Grafana: `tools/ask-mesha-agent/deploy/grafana/` (import steps).
- Stop vs. navigation away both appear as `ask_stopped` until the panel reports which one it was.
- Tests: `node --test tools/ask-mesha-agent/events.test.mjs`. `bench.mjs` accepts
  `ASK_MESHA_BENCH_ABORT_MS=N` to simulate Stop.

## Cost controls

- `ASK_MESHA_MONTHLY_BUDGET_USD` (default **100**): once this month's summed answer cost reaches it,
  `/ask` replies "budget reached" without calling Claude. Fails closed if spend can't be read.
- Per-answer caps (SDK `maxBudgetUsd`, counted inside the monthly cap): `ASK_MESHA_PER_ANSWER_BUDGET_USD`
  (default 1) for lookups, `ASK_MESHA_DEEP_ANSWER_BUDGET_USD` (default 5) for investigations.
  Each running answer reserves its cap: a new ask is refused when spent + in-flight caps >= the monthly
  cap, and its own cap is clipped to what is left.
- One run per chat (lease): a second ask on a busy chat gets HTTP 409 `chat_busy` (logged as a `chat_busy` event).
- Cost = SDK `total_cost_usd` per answer (tokens × list price incl. cache reads/writes), stored with the
  metric; monthly spend = sum since the 1st (UTC). It is an estimate; the GCP bill is authoritative.
- GCP budgets only alert; RUNBOOK §3c adds a $100 Vertex budget alert as a backstop.

## Claude access

- `ASK_MESHA_CLAUDE_AUTH=vertex` (default on GCP): Claude Sonnet 5 / Opus 5.5 via Vertex AI with the
  runtime service account (`roles/aiplatform.user`), billed to GCP, no key. Model ids `claude-sonnet-5` /
  `claude-opus-5-5` verified GA on Vertex 2026-09-24 **only on the `global` endpoint** (not asia-south1/us-east5;
  `global` is not India-pinned). goatos-stg needs a Claude quota increase first (it returned 429) — RUNBOOK §3b.
- `api-key`: Anthropic Console key in Secret Manager. `oauth`: `claude setup-token` — a personal
  Pro/Max plan is for its owner's own use; do not power the shared service with it.
- Local laptop testing may use the developer's own Claude login.

## Storage and sessions

- `ASK_MESHA_DATABASE_URL` => Postgres schema `ask_mesha` (chats, messages, metrics, SDK
  `session_entries`) with a **separate writable app user** — never `mesha_ceo_readonly`.
  DDL `tools/ask-mesha-agent/sql/001_init.sql` + `002_events.sql` (idempotent, applied when `ASK_MESHA_DB_MIGRATE=1`).
  Chats are owned by **email + tenant**; delete is soft (`deleted_at`).
- `ASK_MESHA_UPLOADS_BUCKET` => GCS `uploads/<chat>/<fileId>-<name>`; files re-display after reload via
  the owner-checked file route; only images/PDF are served inline (others download, `nosniff`).
- Unset => JSON/local files under `ASK_MESHA_STATE_DIR` (local dev only).
- Sessions resume across Cloud Run instances via the SDK `sessionStore` (Postgres); if a transcript is
  missing the last 20 messages are replayed as context.

## Deploy (STG) — same authority as everything else

- The image is built by the **existing** `cloudbuild.stg.yaml` path at the exact deployed main SHA; the
  repo snapshot is copied read-only into the image. Nobody checks out code on the server; no git
  credentials exist there. Steps are gated: `_ASK_MESHA_DEPLOY` and `_ASK_MESHA_WIRE_ADMIN_WEB`
  (default `"false"`). `deploy/deploy-stg.sh` verifies infra exists and never creates it.
- One-time setup (service account, bucket, secrets, app DB user, invoker grant, Vertex, budget alert,
  dblink revoke): `tools/ask-mesha-agent/deploy/RUNBOOK.md`. Rollout: deploy flag first, then admin-web
  wiring. Rollback: unset `CEO_AI_AGENT_URL`/`CEO_AI_AGENT_AUDIENCE` on admin-web.
- admin-web → agent auth: Google ID token in `X-Serverless-Authorization` (Cloud Run IAM); the user's
  Firebase bearer stays in `Authorization` and is validated by the agent against the STG API.
- Landing still requires `make land-main` (exact-SHA receipt). Never `gh pr merge`.

## Local testing (Ravi's laptop)

- `cloud-sql-proxy --port 55432 goatos-stg:asia-south1:goatos-stg-core-db`; `.pgenv` from secret
  `mesha-ceo-readonly-db-url`. If the proxy logs `invalid_rapt`, run `gcloud auth application-default login`.
- Agent: `node tools/ask-mesha-agent/server.mjs` (port 8787). admin-web against live STG API with the flag:
  `tools/ask-mesha-agent/start-admin-web.sh prod` (port 3300; `prod` avoids 20–35 s dev compiles).
- Visual check every UI change at 390×844 and 1440×900 (normal + maximized) before handing back.
