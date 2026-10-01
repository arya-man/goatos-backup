# Ask Mesha coding agent (leadership chat)

Read this before touching the admin-web **Ask Mesha** panel, `apps/admin-web/app/api/ceo-ai/*`,
`tools/ask-mesha-agent/`, the `mesha-data-map` skill, or the `_ASK_MESHA_*` Cloud Build steps.

## What it is

- The 4 CEOs use the existing Ask Mesha panel in admin-web. With the flag set, it is answered by
  a **Gemini agent on Vertex AI** (`tools/ask-mesha-agent/gemini.mjs`, newest Gemini Pro) that reads the
  goatos code (read-only) and queries goatos-stg **read-only**, instead of the legacy Go/Vertex `ceo-ai`
  backend. No Anthropic/Claude credentials exist in the runtime path.
- **One instruction pack, model-neutral:** `tools/ask-mesha-agent/instructions.mjs` builds the system
  instruction from `CLAUDE.md` (+ `@AGENTS.md`), the CEO answer rules, `data-map-core.md` and the live
  table index. Edit the rules there, not in `server.mjs`. Its CEO-rule text is byte-identical to the
  pre-Gemini prompt (hash-tested in `test/gemini.test.mjs`).
- **Kill switch / flag:** `CEO_AI_AGENT_URL` on admin-web (`apps/admin-web/app/api/ceo-ai/_forward.ts`).
  Unset => legacy backend `ceo-ai`, unchanged. Never modify the legacy backend to make the agent work.
- Same HTTP contract as the backend: `/ceo-ai/starters`, `/conversations` (list/create/get/patch/soft
  delete), `/conversations/:id/messages`, `/conversations/:id/files/:fileId`, `/ask` (SSE: `progress`,
  `token`, `reset`, `watch`, `final{answer, chart, conversation_id, message_id}`, `error`), `/events`
  (`stop_pressed` / `watch_stop` for a `request_id`; checked against the user who started that request,
  so Stop works before the chat row exists), `/metrics`, `/metrics/users`, `/metrics/recent`, `/healthz`
  (`{ok, provider:"gemini", model}`).

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

1. **Tools (always read-only, there is no other mode):** the agent gets only the code tools
   `read_file` (line ranges; images/PDFs come back inline), `grep` (ripgrep), `glob`, `list_dir`, `get_skill`
   (`code-tools.mjs`: realpath sandbox to the repo + this chat's upload dirs, `.git` and credential-shaped
   files refused) plus the tools of the in-process **`mesha` MCP server** (`McpServer` in `server.mjs`, reached
   by Gemini through an MCP client over an in-memory transport, so the handlers are the same code): `run_sql`, `mcp__mesha__run_reference` (runs an allow-listed `.agents/skills/mesha-data-map/references/*.sql`
   file by name as `SELECT * FROM (<file>) q [WHERE] [ORDER BY] [LIMIT]` through `run_sql`'s same read-only path; typed
   `params` fill only `/*param:x*/…/*end*/` spans declared by `-- param: x date|uuid|int|number`, so the model never retypes ~5KB SQL), `mcp__mesha__watch_tags` (live tag watch, below) and `mcp__mesha__describe_table` (fixed catalog read of up to 6 tables per
   call — `table` comma list and/or `tables` array — returning columns, FK join targets and, for base tables
   <= ~2M rows, top values of up to 6 category/status-like text columns; every name must match strict
   `schema.table` identifiers before it is interpolated, and all reads go through `run_sql`'s same read-only
   path). A `run_sql` "column/relation does not exist" error comes back with the real column lists of up to
   4 tables the query referenced (same validated describe path). No shell, edit, write, web or sub-agent tool exists. Read paths are limited
   to the repo and upload dirs (no `/proc`, no `.pgenv`).
2. **run_sql:** no query rules — any SQL over any table/schema, no tenant filter (single tenant). Runs in
   `BEGIN READ ONLY` with `default_transaction_read_only=on`, 60 s `statement_timeout` (psql process
   killed at 75 s), 500 rows / 100,000 characters of output. Refusals are only
   about execution shape, not data access: psql backslash commands outside plain `'...'` string literals (they run programs on the host, e.g.
   `\!`; regex backslashes inside `'...'` are allowed, `E'...'`/`U&'...'` with any backslash are refused, and psql runs
   with `standard_conforming_strings=on` so the literal scan matches psql's lexer), more than one statement (`;` inside the query), and statements starting with
   commit/rollback/end/abort/set/reset/begin/start (so the model can't step out of `BEGIN READ ONLY`).
   The READ ONLY transaction is still a guard rail, **not** a guarantee; only layer 4 (the role) is.
3. **Chat privacy:** `mesha_ceo_readonly` has NO access to assistant chat tables (`ceo_ai_conversations`, `ceo_ai_messages`, `ceo_ai_assistant_audit`, `ceo_ai_response_cache`, `ceo_ai_rate_limit`, and never the `ask_mesha` schema); each CEO sees only their own chats (service-enforced ownership).
4. **Platform (the real guarantee):** DB role `mesha_ceo_readonly` has SELECT on **every table** in public/analytics/audit/ceo_ai/forensic_repair (+ default privileges for new tables) and **no write privilege anywhere** (granted 2026-09-24 via audit.begin_change; revoke `dblink` + `public` CREATE —
   RUNBOOK §3d); container runs non-root with the repo baked **read-only** at `/repo`; no git/GitHub/cloud
   credentials; there is no model subprocess or shell, and the model credential is the runtime SA (ADC), never a key.
- Proof to re-run after changes: ask "edit AGENTS.md" and "git push --force" — both must be refused and
  the checkout unchanged; a data question must still answer with 1 query. Sandbox/secret refusals are unit-tested
  in `test/gemini.test.mjs` ("code tools: sandbox ...").

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
- The guard is wired into local CI: `run_common` (so `make ci-local` / `make land-main`) runs `make mesha-data-map-guard`
  for every non-docs change, with PG env stripped (hash + object-set check; column check needs `MESHA_DATA_MAP_LIVE=1`).
  Registered as `mesha-data-map` in `tools/ci/guardrail-manifest.json`; self-test `tools/ask-mesha-agent/test/derived-queries.test.mjs`.
  Changing a backend/migration file listed in `derived-queries.json` (growthdirector, weighing, feed, sales, pens...) fails
  CI with "App logic behind an Ask Mesha saved query changed: <query> derived from <file>" until re-derived + rehashed.
- Known gap: sex/origin cuts of ADG (need `sex_scope.go`) are not in the SQL. Feed `fed_kg` is always 0.

### Pens (model-agnostic)

Words: **pen** = what the farm works and paints on the building: `Godel 1 Part 3` (G1P3), `Castro 1` (C1).
**Group** = `Godel 1` / `Castro` — a grouping only, never a physical shed; the chat never calls it a shed.

Two data models, and the team has not decided which one wins:

1. **Label model (today):** animals sit on the group row (`goats.shed_id` = "Godel 1") plus
   `goat_shed_partitions.partition_label` ('Part 3' or '3'); `shed_partitions` is the catalog of parts and its
   `alias_location_id` points at the legacy per-pen `locations` row ("Godel 1 - Part 3", "Castro 1"). Legacy pen rows
   hold 0 animals but weighing buckets / verification items still point at them. Finishing this model = switching those
   legacy rows off (inactive/retired).
2. **Pen-row model:** every pen is its own `locations` row and animals move onto it; the group becomes grouping only
   (name prefix or `parent_location_id`).

The chat must answer correctly in both without edits, so every pen answer goes through ONE resolver,
`.agents/skills/mesha-data-map/references/pens.sql`: any `(location_id, partition_label)` -> `(park, group_name,
pen_label, pen_key, "Godel 1 Part 3 (G1P3)")`, via (in order) catalog group+label, catalog alias row, group-name prefix
split of the row name (same rule as `fcr.go` / `shed_partition_resolve.go`), else the row itself (undivided shed or a
pen row). Inactive/retired rows and parts still resolve (history), `pen_key` starts with the park code (same names in
both parks), and a record on a group row with no part is flagged `grp_only` (answer "part not recorded").
The resolver is one `LATERAL ... ORDER BY k.lbl = '', k.o LIMIT 1` join; the LIMIT is what stops a record matching both
its group+label key and a blank key (a hand-written join without it double-counted Y3 in testing).
`pen-weighing-latest.sql` builds on it (latest individual + whole-pen weigh per pen, one query).

**Test queries** (`references/pens-selftest.sql`; every row must be `ok=t`):

```bash
cd .agents/skills/mesha-data-map/references
{ sed -n '/^WITH pl AS/,/^  WHERE g.merged_into_goat_id IS NULL)$/p' pens.sql; cat pens-selftest.sql; } | psql -X -A
```

Checks: every alive animal resolves to a real pen; animals per pen equal the direct partition counts; every weighing
bucket of the last 30 days resolves to exactly one pen (never a bare group); no duplicate `(location, label)` keys;
every catalog pen row and its group+label give the same `pen_key` (model-2 readiness); verification items, pc care
tasks and feed rows resolve; retired legacy pen rows with no catalog entry still split into group + part.
24/09/2026: 1,562 alive animals in 100 occupied pens (117 listed incl. empty active pens), 173 weighing buckets, 0 failures;
CBE G1P3 last weighing 22/09/2026, 4 animals individually, 23.13 kg.

**When the team migrates** (either way), `make mesha-data-map-guard` fails ("re-validate pens.sql for the new pen
model") because `derived-queries.json` hashes `oploc.go`, `fcr.go`, `shed_partition_resolve.go` and every migration
that touches `goat_shed_partitions` / `shed_partitions` / `locations` (a directory entry with a `match` regex,
so a brand-new migration trips it too). Then:
1. run the self-test on STG after the migration; fix `pens.sql` only if a row fails;
2. model 2: check pen rows under a group (`parent_location_id` or name prefix) and animals with a blank label resolve
   to the same `pen_key` as before (compare the `pens.sql` demo output before/after);
3. model 1: check retired legacy rows still resolve old weighing buckets (history);
4. re-ask: pens per Godel 1 (both parks), G1P3 CBE last weighing, which pen has most animals, Castro 2 CBE headcount;
5. `node tools/ask-mesha-agent/gen-data-map.mjs --rehash-derived`.

- **Live table index:** at startup and hourly, `server.mjs` lists every readable table (with approximate row
  counts) from the database catalog into the system prompt. New tables show up without editing the map; the
  map only adds meanings and traps. The agent must search this list before saying "not recorded".

## Performance (response time is the benchmark)

- Every answer records a timing event (first progress/tool/token, total, tool + DB-query counts, tokens,
  cost) to the metrics store; `GET /metrics` returns p50/p90. `node tools/ask-mesha-agent/bench.mjs`
  drives the real `/ask` path (local only: the token's `/ask` login bypass is off on Cloud Run; there it only unlocks `/metrics*`).
- Why it's fast: the instruction pack (`CLAUDE.md`/`AGENTS.md`, rules, data map, table index) is one
  byte-stable system instruction and all chats share one checkout, so Vertex implicit context caching
  serves the prefix (`cache_read_tokens` on each metric). Per-chat worktrees break the cache; keep
  `ASK_MESHA_WORKTREE_PER_CHAT` off. Quick lookups: `ASK_MESHA_MODEL` with thinking level `low`.
  Investigations (attachment, `deep:` prefix, or verify/check/why/bug/wrong/explain…) use
  `ASK_MESHA_DEEP_MODEL` with thinking level `high`, and must explain with a worked example.
  Each model turn is ~8-12 s on `gemini-3.1-pro-preview`: lookups ~30-60 s, investigations 2-8 min (2026-10-01 local run).
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

## Accuracy regression (golden questions, `tools/ask-mesha-agent/eval/`)

Correctness is proven by a suite, not by hand. `eval/golden.json` holds ~40 CEO questions (headcount, pens,
weighing, ADG, cost/kg gain, feed stock/money, vendor + buyer dues incl. the double-count flag, load-wise,
deaths/births/sick, vaccination done/due, preventive care, shifts, leadership tasks, staff hours, verification
backlog, toxin, wastage, plus refusal/privacy/injection/false-premise traps). Each item has `truth` SQL (or a
`references/*.sql` file) that is run LIVE and read-only at eval time: numbers are never hard-coded. `extract`
rules say which truth numbers must appear in the answer (exact or `tol` / `tol_pct`), `must_mention` /
`must_not_mention` regexes (SQL/table words are always banned), and `max_seconds`.

- Run on demand (local agent on :8787, bench token):
  `ASK_MESHA_STATE_DIR=~/mesha/ask-mesha-local ASK_MESHA_BENCH_TOKEN=... node tools/ask-mesha-agent/eval/run.mjs`
  Options: `--subset <n|tag|id,...>` (e.g. `core`, `trap`, `adg-by-park`), `--concurrency 2`, `--budget-usd 6`
  (stops asking once measured spend passes it; skipped items don't fail), `--truth-only` (runs every truth
  query + UI read, no agent calls, $0: use it to check a new golden), `--no-ui`. Prints a table, writes
  `$ASK_MESHA_STATE_DIR/evals/<ts>.json`, exits 1 if any item fails. Each run emits an `eval_run` event
  (pass/fail/error/skipped, accuracy, cost, failed ids, UI mismatches); `GET /metrics` returns `accuracy`
  (last 20 runs) so accuracy is visible over time.
- When to run: after any data map / saved query (`references/*.sql`) / agent prompt or app-logic change that a
  metric is derived from; before landing an Ask Mesha change (`--subset core` at minimum, full when a metric
  changed); weekly on the timer.
- Cost: ~$0.15 per question, counted in the $100 monthly cap (bench asks are real asks). Weekly full run
  (42 q) ~= $6.3/run ~= $25-27/month. Schedule = `schedule/mesha-ask-eval.{service,timer}` (systemd) or
  `schedule/sg.mesha.ask-eval.plist` (launchd), both calling `eval/scheduled.sh`: WEEKLY full by default,
  nothing daily. Configure with `ASK_MESHA_EVAL_SUBSET` / `ASK_MESHA_EVAL_BUDGET_USD` /
  `ASK_MESHA_EVAL_CONCURRENCY`; secrets (bench token) in `$ASK_MESHA_STATE_DIR/.eval.env` (chmod 600).
- 3-way check (chat vs truth SQL vs admin-web screen): items with `ui_api {path, query, extract}` also call the
  SAME backend read the dashboard uses (e.g. `/weighing/leadership/growth`, `/growth-director/fcr`,
  `/feed-analytics/stock`, `/procurement/loadwise-sales`, `/counts/mortality`, `/herd-register/summary`) on
  `GOATOS_STG_API`. Verdicts: `UI differs from SQL (possible UI bug)` vs `chat differs (chat bug)`. It runs only
  when `ASK_MESHA_EVAL_BEARER` (a leadership Firebase ID token) and `ASK_MESHA_EVAL_TENANT`
  (`00000000-0000-4000-8000-000000000001` on stg) are set; otherwise the report says it was skipped. Getting a
  token: sign in to admin-web as a leadership user, DevTools > Application > Cookies > copy
  `goatos_firebase_id_token`, then `export ASK_MESHA_EVAL_BEARER='<paste>'` in that shell only. It expires in
  ~1 h; never store it in a file, env file, report or log (the runner only sends it as a header).
- A wrong answer found by a CEO ALWAYS becomes a golden (every miss is a permanent test): add an item with the
  CEO's wording, truth SQL that computes the correct number live (copy it from the data map / SKILL.md, read-only,
  `{{month_start}}`-style date macros instead of fixed "this month" dates), the numbers + tolerance that must
  appear, and any `must_mention` caveat the miss lacked; add `ui_api` when a dashboard shows the number.
  Check it with `--truth-only --subset <id>`, then fix the data map / prompt until `--subset <id>` passes.
  Only edit a golden when the golden/truth itself is wrong, never to make a wrong agent answer pass.
- Tests: `node --test tools/ask-mesha-agent/test/eval-grade.test.mjs` (grading rules, golden shape, macros).

## Cost controls

- `ASK_MESHA_MONTHLY_BUDGET_USD` (default **100**): once this month's summed answer cost reaches it,
  `/ask` replies "Ask Mesha is paused for this month. Please contact the Mesha team." without calling the model.
  Fails closed if spend can't be read ("unavailable for a moment").
- Per-answer caps (the agent loop stops before running more tools once the answer's token cost passes its cap; counted inside the monthly cap): `ASK_MESHA_PER_ANSWER_BUDGET_USD`
  (default 1) for lookups, `ASK_MESHA_DEEP_ANSWER_BUDGET_USD` (default 5) for investigations.
  Each running answer reserves its cap: a new ask is refused when spent + in-flight caps >= the monthly
  cap, and its own cap is clipped to what is left.
- One run per chat (lease, 60 s TTL refreshed by the 10 s SSE heartbeat): a second ask on a busy chat gets HTTP 409
  `chat_busy` (logged as a `chat_busy` event).
- Input limits: question <= 20,000 characters (413 `question_too_long`); at most 5 attachments, ~10 MB together
  (request body cap 15 MB incl. base64, 413 `too_large`).
- No result (tab closed, crash): the answer's cap is charged (`cost_estimated`).
- Cost = Gemini `usageMetadata` per model call × Vertex list price (`priceFor()` in `gemini.mjs`; thinking tokens
  bill as output, cached input at 10%, long-context rate over 200k prompt tokens; override with
  `ASK_MESHA_PRICE_IN_PER_M` / `ASK_MESHA_PRICE_OUT_PER_M`), stored with the metric; monthly spend = sum since the
  1st (UTC). It is an estimate; the GCP bill is authoritative.
- GCP budgets only alert; RUNBOOK §3c adds a $100 Vertex budget alert as a backstop.

## Model access (Gemini on Vertex AI)

- The service calls Gemini on Vertex AI (`@google/genai`, `vertexai: true`) with the Cloud Run runtime service
  account (`roles/aiplatform.user`, ADC from the metadata server). No API key, no model secret.
  `deploy-stg.sh` refuses to deploy when the SA lacks the role.
- Models (verified with a live `generateContent` 200 on goatos-stg, location `global`, 2026-10-01):
  `ASK_MESHA_MODEL` default **`gemini-3.1-pro-preview`** (newest Pro), `ASK_MESHA_FAST_MODEL` /
  `ASK_MESHA_CHECK_MODEL` default **`gemini-3.8-flash`** (newest Flash, used by the answer checker),
  `ASK_MESHA_DEEP_MODEL` defaults to `ASK_MESHA_MODEL`. Endpoint: `ASK_MESHA_GEMINI_PROJECT` (goatos-stg),
  `ASK_MESHA_GEMINI_LOCATION` (`global`). Do not pin a model Google has announced for retirement.
- Agent loop (`runAgent`): stream a turn, run all its function calls in parallel, return results (errors as
  data so the model fixes its call), repeat; max `ASK_MESHA_MAX_STEPS` (40) turns, then one tool-less turn
  to answer (`error_max_turns`); one nudge if a turn comes back empty; 429/5xx retried twice before any text.
  Gemini 3 thought signatures are echoed back verbatim. SSE events are unchanged (`progress`/`reset`/`token`/
  `replace`/`final`), so admin-web needs no change.
- Skills: exposed as the `get_skill` tool (list, then load `SKILL.md` by name) like the SDK's on-demand Skill
  tool, so the cached system instruction stays small; `data-map-core.md` is always inlined.
- Screenshots: image/PDF attachments go to the model as inline parts in the user turn and stay readable with
  `read_file`; text/CSV attachments are read with `read_file`.
- `provider` (`gemini`) and `model` are on every metric row, event and saved answer; `/healthz` shows them.
- Local laptop: ADC (`gcloud auth application-default login`), or `ASK_MESHA_GEMINI_AUTH=gcloud` to use the
  signed-in `gcloud` user token when ADC needs a browser re-auth (ignored on Cloud Run).

## Storage and sessions

- `ASK_MESHA_DATABASE_URL` => Postgres schema `ask_mesha` (chats, messages, metrics; the legacy SDK
  `session_entries` table is no longer written) with a **separate writable app user** — never `mesha_ceo_readonly`.
  DDL `tools/ask-mesha-agent/sql/001_init.sql` + `002_events.sql` (idempotent, applied when `ASK_MESHA_DB_MIGRATE=1`).
  Chats are owned by **email + tenant**; delete is soft (`deleted_at`).
- `ASK_MESHA_UPLOADS_BUCKET` => GCS `uploads/<chat>/<fileId>-<name>`; files re-display after reload via
  the owner-checked file route; only images/PDF are served inline (others download, `nosniff`).
- Unset => JSON/local files under `ASK_MESHA_STATE_DIR` (local dev only).
- Turns are stateless: every question replays the chat's last 20 stored messages as user/model turns, so any
  instance can answer any chat (older chats that started on Claude continue the same way).

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
  Typed `get_*` tools never go through the agent, and in agent mode `toolList()` does not offer them (only `ask_goatos` plus the docs tools).
- Scoping: leaders add this connector to their everyday Claude, so the agent-mode `ask_goatos`
  description says to call it ONLY for Mesha farm-data questions, and `initialize` returns server
  `instructions` telling clients to answer everything else without Goat OS tools
  (`askGoatOSDescription` / `initializeResult` in `backend/cmd/mcp/main.go`, guarded by `ask_scope_test.go`).
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
- Agent: `node tools/ask-mesha-agent/server.mjs` (port 8787; `ASK_MESHA_GEMINI_AUTH=gcloud` if ADC is stale;
  `GOATOS_REPO` = a clean `origin/main` worktree under `~/mesha`). admin-web against live STG API with the flag:
  `tools/ask-mesha-agent/start-admin-web.sh prod` (port 3300; `prod` avoids 20–35 s dev compiles).
- Visual check every UI change at 390×844 and 1440×900 (normal + maximized) before handing back.

## Playbook: change the Gemini model (for Claude/Codex)

Use when Ravi says "move Ask Mesha to <model>" or Google announces a retirement. Project `goatos-stg`,
service `goatos-ask-mesha-stg`, region `asia-south1`.

1. **Find the id and prove it serves** (expect HTTP 200 and `modelVersion` = the id; in zsh write `${M}`):
   ```bash
   T=$(gcloud auth print-access-token); M=gemini-3.1-pro-preview
   curl -s -H "Authorization: Bearer $T" -H "x-goog-user-project: goatos-stg" "https://aiplatform.googleapis.com/v1beta1/publishers/google/models?pageSize=300" | grep -o '"name": "publishers/google/models/gemini[^"]*"'
   curl -s -w "\n%{http_code}\n" -H "Authorization: Bearer $T" -H "Content-Type: application/json" "https://aiplatform.googleapis.com/v1/projects/goatos-stg/locations/global/publishers/google/models/${M}:generateContent" -d '{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}'
   ```
2. **Run the golden eval locally on the new id** (`ASK_MESHA_MODEL=<id>` on the local server, then
   `node tools/ask-mesha-agent/eval/run.mjs --url http://127.0.0.1:<port>`); it must not score below the current model.
3. **Switch the live service** (config change, no build, ~30 s):
   `gcloud run services update goatos-ask-mesha-stg --project=goatos-stg --region=asia-south1 --update-env-vars=ASK_MESHA_MODEL=<id>`
4. **Make builds keep it:** change the default in `gemini.mjs` via a PR landed with `make land-main`
   (or trigger Cloud Build with `_ASK_MESHA_MODEL=<id>`).
5. **Verify:** `/healthz` shows the id; ask one question in the panel; its metric row shows the model.

Rollback: the same `--update-env-vars=ASK_MESHA_MODEL=<previous id>`.

## Code snapshot in the image (what the chat can read)

- The deploy copies the repo at the exact deployed main SHA into the image at `/repo`, read-only
  (`chmod a-w`). No `.git`, no git/GitHub credentials, no `node_modules`, no build output, no secrets
  (`.env*`, keys, service-account JSON, `.pgenv`, `.npmrc`, tfstate). See
  `tools/ask-mesha-agent/Dockerfile.dockerignore`.
- Also excluded (size, and raw data the chat must not treat as truth): `docs/runbooks/evidence`,
  `docs/prototypes/config-sop-studio/research`, any `artifacts/` folder, `fixtures/`, the Android
  screenshot gallery, and binary/media files
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

## Single instance (required)

The service runs as ONE Cloud Run instance (`--max-instances=1`, `--concurrency=12`, set in
`deploy/deploy-stg.sh`). Stop / Stop watching / chat delete (`activeRuns`), running watches and the
monthly-cap in-flight reservations live in that process's memory, so a second instance would let a
Stop or delete land on the wrong instance (silently ignored) and let parallel asks on two instances
each pass the monthly cap. 12 concurrent requests covers 4 CEOs asking, watching and pressing Stop at
once. Before raising `--max-instances`, move `activeRuns`, watch cancellation and cap reservations to
Postgres (same store as the chat lock) and add cross-instance tests. (Found by Codex review of PR 388.)
