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
  `token`, `reset`, `watch`, `final{answer, chart, conversation_id, message_id}`, `error`), `/events`
  (`stop_pressed` / `watch_stop` for a `request_id`; checked against the user who started that request,
  so Stop works before the chat row exists), `/metrics`, `/metrics/users`, `/metrics/recent`, `/healthz`
  (`{ok, provider}`).

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
   plus the MCP tools `run_sql`, `mcp__mesha__watch_tags` (live tag watch, below) and `mcp__mesha__describe_table` (fixed catalog read of up to 6 tables per
   call — `table` comma list and/or `tables` array — returning columns, FK join targets and, for base tables
   <= ~2M rows, top values of up to 6 category/status-like text columns; every name must match strict
   `schema.table` identifiers before it is interpolated, and all reads go through `run_sql`'s same read-only
   path). A `run_sql` "column/relation does not exist" error comes back with the real column lists of up to
   4 tables the query referenced (same validated describe path). No Bash, Edit, Write, NotebookEdit, Web*, Task, Agent. Read paths are limited
   to the repo and upload dirs (no `/proc`, no `.pgenv`).
2. **run_sql:** no query rules — any SQL over any table/schema, no tenant filter (single tenant). Runs in
   `BEGIN READ ONLY` with `default_transaction_read_only=on`, 60 s `statement_timeout` (psql process
   killed at 75 s), 500 rows / 100,000 characters of output. Refusals are only
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
- **Copied app queries + drift manifest.** Metrics the map copies from backend logic live as run-as-is SQL in
  `references/`: `adg-by-park.sql` (Weighing > Growth, `growth.go` + `identity_scope.go`; matches the API exactly),
  `cost-per-kg-gain.sql` (FCR tab, `fcr.go`; joins pens by shed id + partition like the app and lists weighed
  pens with no feed rows in `unmatched_pens`), `feed-stock-days-left.sql` (Feed Analytics > Stock).
  `references/derived-queries.json` lists, per query, the source files it was derived from + a sha256 of each.
  `gen-data-map.mjs --check` (so `make mesha-data-map-guard`) fails when any source changes or goes missing.
  To clear it: diff the source since it was hashed, re-derive the query, re-check its numbers against the app,
  update the SKILL.md example numbers, then `node tools/ask-mesha-agent/gen-data-map.mjs --rehash-derived`.
  Adding a copied query = add its entry to the manifest, then rehash.
- Known gap: sex/origin cuts of ADG (need `sex_scope.go`) are not in the SQL. Feed `fed_kg` is always 0.

- **Live table index:** at startup and hourly, `server.mjs` lists every readable table (with approximate row
  counts) from the database catalog into the system prompt. New tables show up without editing the map; the
  map only adds meanings and traps. The agent must search this list before saying "not recorded".

## Performance (response time is the benchmark)

- Every answer records a timing event (first progress/tool/token, total, tool + DB-query counts, tokens,
  cost) to the metrics store; `GET /metrics` returns p50/p90. `node tools/ask-mesha-agent/bench.mjs`
  drives the real `/ask` path (local only: the token's `/ask` login bypass is off on Cloud Run; there it only unlocks `/metrics*`).
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
  `ask_stopped.reason` is `stop_pressed` (panel Stop), `chat_deleted` or `client_closed`; `cost_estimated=true`
  means no SDK result arrived and the answer's cap was charged.
  Signals: `ask_started` (model, effort, deep, question_preview = first 80 chars), `ask_first_token`,
  `ask_tool` (tool, plain label, duration_ms tool_use→tool_result, ok, error), `budget_warning` (>= 80 %,
  once per month per instance), `budget_blocked`, `auth_denied` (path), `attachment_saved`, `chat_busy`
  (409), `provider_fallback` (from, to, reason), `watch_started` / `watch_ended`.
- Where to look: `GET /metrics/users` (per email, today (IST)/7d/30d: asks, success, failed by class, stopped,
  success rate, p50/p90 total and first-token, avg tools, cost, watches, busy) and `GET /metrics/recent?email=` (last 50
  asks: status, duration, question preview). Same auth as `/metrics`: open on a 127.0.0.1 bind; on Cloud Run
  (`HOST=0.0.0.0`) only with `Authorization: Bearer $ASK_MESHA_BENCH_TOKEN` (the token's `/ask` login bypass is
  still off there because `K_SERVICE` is set). Cloud Logging:
  `jsonPayload.component="ask-mesha-agent"`. Grafana: `tools/ask-mesha-agent/deploy/grafana/` (import steps).
- Stop vs. navigation away: the panel POSTs `/ceo-ai/events {kind:"stop_pressed"}` before aborting; the server
  waits up to 1.5 s for it, else the stop is recorded as `client_closed`.
- Tests: `node --test tools/ask-mesha-agent/events.test.mjs`. `bench.mjs` accepts
  `ASK_MESHA_BENCH_ABORT_MS=N` to simulate Stop.

## Live tag watch (watch_tags)

"Watch Castro 1 tags for 10 minutes", "is A0002A moving? keep watching", "tell me when Yashoda goats stop
moving" and one-shot "which goats are slower than their pen / own pace right now" go to the read-only MCP tool
`watch_tags` (`tools/ask-mesha-agent/watch.mjs`, registered in `server.mjs` next to `run_sql`).

- **Server polls, not the model.** Every `interval_s` (5-30, default 10) for `minutes` (default 5, max 30; 0 = one
  snapshot) the agent server reads
  `herd_signal_tag_latest` (+ the live table's tag -> goat -> pen join) through the same READ ONLY `runSql` path as
  `run_sql` and streams SSE `{type:"watch", phase:start|tick|end|error, rows, changes}` frames. No model call per
  tick; the model gets one compact summary at the end and writes a 2-4 sentence answer. Cost ~ one normal answer.
- **Same semantics as the Herd Signals Live Monitor** (`backend/internal/herdsignals`): movement_state thresholds
  (>=100 moving, 10-99 low, 1-9 quiet, 0 no movement), read-time stale/missing after 30 min without a packet, weak
  signal <= -75 dBm, low battery < 2800 mV, status precedence missing > weak > low battery. `compare`:
  `self` = `applyRiskSignals` own-baseline % (p75 of the tag's 24h 300s windows since `animal_monitoring_since`,
  scaled to the 15-min window; <= -70% far below, >= +150% spike), `peers` = % vs the pen median motion_delta
  (<= -70% lower than pen), `both`. `live_state` (moving_now / active_1m) is shown when the DB has the realtime
  columns from origin/main (probed per watch).
- **Stops**: time up (default 5 min), `stop_when` met, "Stop watching" (`/ceo-ai/events` kind `watch_stop`: ends the
  watch only, the answer still arrives), Stop (aborts the answer), client disconnect (request close aborts polling
  immediately), 30-min hard cap, 3 failed polls in a row (`data_error`). `stop_when` "stopped moving" = no motion for
  `still_minutes` (1-30, default 5). No background continuation; one active watch per chat, at most 4 per server
  instance (`MAX_WATCHES`); the table shows at most 60 tags.
- **Request timeouts bound the real watch length.** The answer SSE passes through `goatos-admin-web-stg`, whose
  Cloud Run request timeout was 300 s (checked 2026-09-24; `goatos-ask-mesha-stg` is 3600 s). The admin-web wiring
  step in `deploy/deploy-stg.sh` sets it to 2100 s; if Terraform (`cloud_run_services.tf`) manages admin-web it
  must say >= 2100 s too, or the next apply drops it back and watches longer than ~5 min end as
  `client_disconnected`. The agent's `MCP_TOOL_TIMEOUT` is 2100 s for the same reason.
- **Tenant:** the watch query filters `tenant_id` only when `X-GoatOS-Tenant-ID` is a UUID; see
  "Multi-tenant isolation" — like `run_sql`, it is not isolation until the DB enforces it.
- **Events**: `watch_started` / `watch_ended` (reason, duration_ms, polls, tags) per user; `/metrics/users` shows
  `watches` per window.
- **Panel**: `features/ceo-ai/ceo-ai-watch.tsx` live card (table, change feed, countdown, Stop watching); frames
  parsed in `lib/ceo-ai-stream.ts` (`onWatch`); keeps updating while the panel is minimized.

## Cost controls

- `ASK_MESHA_MONTHLY_BUDGET_USD` (default **100**): once this month's summed answer cost reaches it,
  `/ask` replies "Ask Mesha is paused for this month. Please contact the Mesha team." without calling Claude.
  Fails closed if spend can't be read ("unavailable for a moment").
- Per-answer caps (SDK `maxBudgetUsd`, counted inside the monthly cap): `ASK_MESHA_PER_ANSWER_BUDGET_USD`
  (default 1) for lookups, `ASK_MESHA_DEEP_ANSWER_BUDGET_USD` (default 5) for investigations.
  Each running answer reserves its cap: a new ask is refused when spent + in-flight caps >= the monthly
  cap, and its own cap is clipped to what is left.
- One run per chat (lease, 60 s TTL refreshed by the 10 s SSE heartbeat): a second ask on a busy chat gets HTTP 409
  `chat_busy` (logged as a `chat_busy` event).
- Input limits: question <= 20,000 characters (413 `question_too_long`); at most 5 attachments, ~10 MB together
  (request body cap 15 MB incl. base64, 413 `too_large`).
- No SDK result (tab closed, crash): the answer's cap is charged (`cost_estimated`). A Vertex attempt that fails
  over to the key adds its own cost (its cap when it produced no result) to the answer, on every exit path.
- Cost = SDK `total_cost_usd` per answer (tokens × list price incl. cache reads/writes), stored with the
  metric; monthly spend = sum since the 1st (UTC). It is an estimate; the GCP bill is authoritative.
- GCP budgets only alert; RUNBOOK §3c adds a $100 Vertex budget alert as a backstop (plus an Anthropic Console limit for the key).

## Claude access

- `ASK_MESHA_CLAUDE_AUTH=auto` (deploy default): the service carries both the Vertex env and the
  Anthropic API key. `provider.mjs` probes Vertex (tiny `rawPredict`, metadata-server token; locally
  `gcloud auth print-access-token` if installed) at startup and every 15 min while on the key, hourly once
  on Vertex. Each question uses Vertex iff the last probe passed, else the key; a Vertex 429/403/404 before
  any token is shown and before any tool call (so no query or live watch runs twice) marks Vertex down and
  reruns that question once on the key (the failed attempt's Vertex spend — or its cap when it returned no
  result — is added to that answer's cost; its session and session cost are rolled back). So STG runs on the key
  now and moves to Vertex by itself when quota is approved — no redeploy. `provider` (`vertex` |
  `anthropic`) is on every metric row and event, `/healthz` shows it, logs say `[provider] switched to …`.
  The monthly cap is one cap across both. Force a provider with
  `gcloud run services update … --update-env-vars=ASK_MESHA_CLAUDE_AUTH=vertex|api-key` (config change,
  no build); after Vertex is live, switch to `vertex` and disable the key's secret version (RUNBOOK §3b).
- `ASK_MESHA_CLAUDE_AUTH=vertex`: Claude Sonnet 5 / Opus 5.5 via Vertex AI with the
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

## MCP (hosted connector, `ask_goatos`)

- The hosted MCP (`backend/cmd/mcp`, `goatos-mcp-stg`, https://mcp.mesha.sg/mcp) answers `ask_goatos`
  via this agent when `MESHA_MCP_AGENT_URL` is set (`MESHA_MCP_AGENT_AUDIENCE` defaults to the URL,
  `MESHA_MCP_AGENT_TIMEOUT` defaults to 240s). Unset => the legacy API `/ceo-ai/ask` path, unchanged.
  Typed `get_*` tools never go through the agent.
- Call shape: `POST ${MESHA_MCP_AGENT_URL}/ceo-ai/ask` with `{question, conversation_id, stream:false}`;
  Google ID token from the metadata server in `X-Serverless-Authorization`; the CEO's own bearer in
  `Authorization` (the agent validates it against the STG API like admin-web); `X-Mesha-Client: mcp`.
- `stream:false` on the agent runs the same pipeline (auth, chat privacy, per-chat busy lock, per-answer
  and monthly caps, events) and returns one JSON `{answer, chart, conversation_id, message_id, timing,
  request_id}` or `{error, message}` with the HTTP status (409 busy, 404 unknown chat, 413 too long, 502
  failed run). Events carry `source:"mcp"`. With no live panel, `watch_tags` is snapshot-only
  (`minutes` forced to 0) and the model is told so.
- MCP-started chats are normal chats owned by the same email, so they show up in the admin-web panel.
- Wiring: `_ASK_MESHA_WIRE_MCP=true` (default `"false"`) in `cloudbuild.stg.yaml` → `deploy-stg.sh`
  grants the MCP runtime SA `roles/run.invoker` on the agent and sets the env vars + `--timeout=300`
  on `goatos-mcp-stg`. Runbook: `tools/ask-mesha-agent/deploy/RUNBOOK.md` §6b.

## Local testing (Ravi's laptop)

- `cloud-sql-proxy --port 55432 goatos-stg:asia-south1:goatos-stg-core-db`; `.pgenv` from secret
  `mesha-ceo-readonly-db-url`. If the proxy logs `invalid_rapt`, run `gcloud auth application-default login`.
- Agent: `node tools/ask-mesha-agent/server.mjs` (port 8787). admin-web against live STG API with the flag:
  `tools/ask-mesha-agent/start-admin-web.sh prod` (port 3300; `prod` avoids 20–35 s dev compiles).
- Visual check every UI change at 390×844 and 1440×900 (normal + maximized) before handing back.

## Playbook: move Ask Mesha from the Anthropic key to Vertex (for Claude/Codex)

Use when Ravi says "Vertex is approved, switch Ask Mesha". Project `goatos-stg`, service
`goatos-ask-mesha-stg`, region `asia-south1`, secret `goatos-stg-ask-mesha-anthropic-api-key`.

1. **Check Vertex really works** (expect HTTP 200, not 429/404):
   ```bash
   T=$(gcloud auth print-access-token); curl -s -o /dev/null -w "%{http_code}\n" -X POST -H "Authorization: Bearer $T" -H "x-goog-user-project: goatos-stg" -H "Content-Type: application/json" "https://aiplatform.googleapis.com/v1/projects/goatos-stg/locations/global/publishers/anthropic/models/claude-sonnet-5:rawPredict" -d '{"anthropic_version":"vertex-2023-10-16","max_tokens":5,"messages":[{"role":"user","content":"hi"}]}'
   ```
   Repeat with `claude-opus-5-5`. Both must be 200.
2. **Auto mode usually switches by itself** within 15 min. Confirm: `curl <service-url>/healthz` (with an
   ID token) shows `"provider":"vertex"`, or Cloud Logging has `[provider] switched to vertex`.
3. **Pin it to Vertex** (config change, no build, ~30 s):
   ```bash
   gcloud run services update goatos-ask-mesha-stg --project=goatos-stg --region=asia-south1 --update-env-vars=ASK_MESHA_CLAUDE_AUTH=vertex
   ```
4. **Make future builds use Vertex:** trigger Cloud Build with `_ASK_MESHA_CLAUDE_AUTH=vertex` (or change the
   default in `cloudbuild.stg.yaml` via a PR landed with `make land-main`). Do this BEFORE step 5, or the
   next deploy's preflight fails on a disabled key.
5. **Retire the key:** `gcloud secrets versions disable 1 --secret=goatos-stg-ask-mesha-anthropic-api-key --project=goatos-stg`,
   then ask Ravi to revoke the key in console.anthropic.com (never do Console actions yourself).
6. **Verify:** ask one question in the panel; the metric/event row shows `provider: vertex`; spend keeps
   counting under the same $100 cap.

Rollback (Vertex failing): `--update-env-vars=ASK_MESHA_CLAUDE_AUTH=auto` (needs an enabled key version) or `=api-key`.
Never store the key in the repo, env files or logs; it lives only in Secret Manager.

## Code snapshot in the image (what the chat can read)

- The deploy copies the repo at the exact deployed main SHA into the image at `/repo`, read-only
  (`chmod a-w`). No `.git`, no git/GitHub credentials, no `node_modules`, no build output, no secrets
  (`.env*`, keys, service-account JSON, `.pgenv`, `.npmrc`, tfstate). See
  `tools/ask-mesha-agent/Dockerfile.dockerignore`.
- Also excluded (size, and raw data the chat must not treat as truth): `docs/runbooks/evidence`,
  `docs/prototypes/config-sop-studio/research`, any `artifacts/` folder, `fixtures/`, the Android
  screenshot gallery, `tools/dashboard-automation/commit-classification`, and binary/media files
  (docx, pdf, png, jpg, webp, gif, mp4, apk, aab, ipa). Snapshot ~35 MB (was ~77 MB); image ~450–550 MB.
- **Automatic secret scrub (second net):** the Dockerfile runs `tools/ask-mesha-agent/deploy/scrub-snapshot.mjs /repo`
  on every build, whichever route builds it (Cloud Build STG, `deploy-stg.sh`, a manual `docker build` by
  Claude/Codex). It DELETES (never fails the build) files whose name looks like a credential file
  (`.env*`, keys, service-account/google-services JSON, `*secret*/*credential*` data files, tfstate) or whose
  content holds a real-looking secret (private keys, Anthropic/OpenAI/AWS/Google/GitHub/Slack tokens,
  Postgres URLs with a real password), plus symlinks that escape `/repo`. Paths removed are logged, never
  contents. To cover a new secret kind, add a pattern + a case in `test/scrub.test.mjs`; preview with
  `node tools/ask-mesha-agent/deploy/scrub-snapshot.mjs . --dry-run`.
- Checked 2026-09-24: every file the agent read in local testing was under `backend/internal/*` or
  `backend/migrations` — nothing in the excluded set. If you exclude more, re-check the same way
  (tally Read/Grep paths from the agent's session transcripts) so answers don't lose sources.
- Why this design: each deploy = one commit, rollback also rolls back what the chat knows, no
  credentials on the server. The chat only knows deployed code, never unmerged branches.

## Future: if goatos splits into microservices / Kubernetes

1. Each service's CI uploads a source snapshot (same exclusions, no git) at its deployed SHA to a bucket,
   e.g. `gs://<bucket>/code-snapshots/<service>/<sha>.tar.gz`.
2. A manifest of live versions (`service -> sha`), updated by each deploy or read from image tags.
3. The agent stops baking code into its image: at startup and on manifest change it downloads the
   matching snapshots read-only into `/repo/<service>/` and searches them as one tree.
4. Data: one read-only login per service DB, or (preferred) one read-only reporting replica/warehouse.
5. Unchanged: read-only everywhere, no git credentials, deployed-code only, $100 cap, per-user events,
   chat privacy. Estimate 1–2 days of work at split time.
