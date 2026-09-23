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

## Read-only guarantees (keep all three layers)

1. **Tools:** `ASK_MESHA_READONLY=1` (default) gives the agent only `Read/Grep/Glob/Skill/TodoWrite`
   plus the MCP tool `run_sql`. No Bash, Edit, Write, NotebookEdit, Web*, Task. Read paths are limited
   to the repo and upload dirs (no `/proc`, no `.pgenv`).
2. **run_sql:** one statement, **any backslash refused** (psql runs meta-commands mid-line: `\g |cmd`,
   `\o`, `\copy`), `BEGIN READ ONLY`, `default_transaction_read_only=on`, 30 s timeout, 500 rows.
3. **Platform:** DB role `mesha_ceo_readonly` (sees `ceo_ai.*` only; revoke `dblink` + `public` CREATE —
   RUNBOOK §3d); container runs non-root with the repo baked **read-only** at `/repo`; no git/GitHub/cloud
   credentials; agent env is an allow-list (`agentEnv()` in `server.mjs`).
- Proof to re-run after changes: ask "edit AGENTS.md" and "git push --force" — both must be refused and
  the checkout unchanged; a data question must still answer with 1 query.

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

## Performance (response time is the benchmark)

- Every answer records a timing event (first progress/tool/token, total, tool + DB-query counts, tokens,
  cost) to the metrics store; `GET /metrics` returns p50/p90. `node tools/ask-mesha-agent/bench.mjs`
  drives the real `/ask` path (local only; `ASK_MESHA_BENCH_TOKEN` is ignored on Cloud Run).
- Why it's fast: `CLAUDE.md`/`AGENTS.md` are injected into the **system prompt** (cached, 1 h TTL via
  `ENABLE_PROMPT_CACHING_1H=1`, `CLAUDE_CODE_DISABLE_CLAUDE_MDS=1`) and all chats share one checkout,
  so the large prefix is read from cache instead of re-written per chat. Per-chat worktrees break the
  cache; keep `ASK_MESHA_WORKTREE_PER_CHAT` off. Defaults: `claude-sonnet-5`, effort `low`; `deep:`
  prefix => `claude-opus-5-5`. Baseline: 84 s / $0.64 → ~15–30 s / ~$0.10 per question.
- Repo hook `ai-setup-guard` blocks tools in fresh clones; the agent sets the documented
  `GOATOS_AI_SETUP_GUARD=0`.

## Cost controls

- `ASK_MESHA_MONTHLY_BUDGET_USD` (default **100**): once this month's summed answer cost reaches it,
  `/ask` replies "budget reached" without calling Claude. Fails closed if spend can't be read.
- `ASK_MESHA_PER_ANSWER_BUDGET_USD` (default 1): SDK `maxBudgetUsd` per answer.
- GCP budgets only alert; RUNBOOK §3c adds a $100 Vertex budget alert as a backstop.

## Claude access

- `ASK_MESHA_CLAUDE_AUTH=vertex` (default on GCP): Claude Sonnet 5 / Opus 5.5 via Vertex AI with the
  runtime service account (`roles/aiplatform.user`), billed to GCP, no key. Confirm model ids/region
  in Model Garden (`ASK_MESHA_MODEL`, `ASK_MESHA_DEEP_MODEL`, `ASK_MESHA_VERTEX_REGION`).
- `api-key`: Anthropic Console key in Secret Manager. `oauth`: `claude setup-token` — a personal
  Pro/Max plan is for its owner's own use; do not power the shared service with it.
- Local laptop testing may use the developer's own Claude login.

## Storage and sessions

- `ASK_MESHA_DATABASE_URL` => Postgres schema `ask_mesha` (chats, messages, metrics, SDK
  `session_entries`) with a **separate writable app user** — never `mesha_ceo_readonly`.
  DDL `tools/ask-mesha-agent/sql/001_init.sql` (idempotent, applied when `ASK_MESHA_DB_MIGRATE=1`).
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
