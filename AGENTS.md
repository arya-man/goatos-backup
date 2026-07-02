# Goat OS Workspace Agent Context

## MANDATORY: 4-Layer Lookup on Every Code Question

Work through layers in order. Stop at the layer that answers the question. Do NOT jump to files/grep first.

### Layer 1 — CRG (code structure)
For callers, callees, imports, blast radius, architecture, dead code, test coverage:
```
repo_root: <absolute path of your goatos checkout>   # git rev-parse --show-toplevel

Cold/review/diff task      -> get_minimal_context_tool, then one targeted graph query
Known-symbol traversal     -> query_graph_tool callers_of/callees_of/imports_of/tests_for
Keyword/domain lookup      -> semantic_search_nodes_tool, then query_graph_tool
Changing code              -> detect_changes_tool + get_impact_radius_tool
Single file/function read  -> read the file; use graph only if impact is unclear
```

### Layer 2 — Graphify (business/product context + technical docs)
Two graphs. Query both in parallel:

**mesha_docs_graph** — wiki SOPs, farm workflows, vaccination protocols, org model:
```
MCP: mesha_docs_graph
CLI: uvx --from 'graphifyy[mcp]==0.8.44' graphify query "QUESTION" \
       --graph /Users/ravi/mesha/graphify-out/graph.json
```

**goatos-docs graph** — TRDs, ADRs, phase docs, obligation engine, skill references (locally generated; run `make ai-rebuild-docs` if missing):
```
CLI: uvx --from 'graphifyy[mcp]==0.8.44' graphify query "QUESTION" \
       --graph ./graphify-out/graph.json
```
When built, it covers protocol engine, Preventive Care (PC) vaccination, feed direction,
frontend scope, analytics infra, execution plans, observability, auth, SOP
cutover, and skill references.

### Layer 3 — Skill references (architecture decisions, TRDs, phase contracts)
When CRG + Graphify don't cover it — deep implementation rules, phase PRDs/TRDs,
OpenAPI contracts, form DSL, analytics infra, security/ops rules:
```
Load .agents/skills/goatos-build/SKILL.md → pick only the relevant reference doc
Do NOT load all reference docs — let CRG + Graphify narrow which one applies
```

### Layer 4 — Grep/Read (CRG blind spots)
Only for what the graph cannot see:
- HTTP route strings (`r.GET("/api/v1/...")`)
- Middleware wired via reflection or string keys
- Config/env values and constants
- SQL query strings
- Uncommitted/unstaged code
- Any `callers_of = 0` result that seems wrong — verify with grep

## Business and medical rule changes (maintainer lock)

When the maintainer states a **new working rule, condition, timing, or workflow**
(in chat, WhatsApp screenshots, PHC sign-off, or ad-hoc instructions) that may
**contradict or supersede** existing docs, seeded config, implemented kernel
behavior, or a prior decision in the same thread:

1. **Stop and surface the conflict first** — quote the old rule/source and the new
   instruction side by side. Do **not** silently pick one, blend them, or change
   code/docs on assumption.
2. **Ask explicitly** which rule wins, whether the old rule is retired, or
   whether both apply in different scopes (species, stage, procurement path, etc.).
3. **Implement only after confirmation** — then update the canonical source in the
   same change as the code (`docs/phc-vaccination/source-nuances-rules.md`,
   published `rule_dsl`, TRD/ADR, or this file when appropriate).

Ambiguity is not approval. Informal agreement in a screenshot or chat applies to
**that** scenario until it is written into the source contract.

Read first:

- `context/README.md`
- `SKILLS.md`
- `.agents/skills/goatos-build/SKILL.md`
- `context/architecture/final-architecture.md`
- `context/frontend/final-frontend-mobile-backend-architecture.md`
- `context/forms/final-forms-sop-engine.md`
- `context/analytics/final-analytics-infra.md`
- `context/agents/ai-agent-context-and-protocols.md`

Historical planning/archive docs were removed from the active tree. If a human
explicitly asks for archaeology, use git history or source material rather than
normal build docs.

Purpose:

- Goat OS is the operating system for goat identity, health, vaccination, genetics, breeding, workforce, SOP tasks, media proof, verification, devices, commerce interfaces, and analytics.
- Canonical backend/data model/app APIs are built fresh.
- `Goats and Parks.docx` is the base source for goat and park semantics across
  every slice. Any feature touching goat identity, park/shed scope, shed tags,
  lifecycle/stage, breed labels, pregnancy/lactation/warm-up/fattening, feed
  safety, weighing, handling, medicine administration, park roles, or feed
  sessions must start from
  `context/source-findings/goats-and-parks-source-findings.md` and must not
  invent conflicting semantics. Feature-specific docs may add stricter
  source-backed rules, but conflicts require an explicit source/owner decision.
- Scope lock: build exactly the user-approved slice, not adjacent product areas
  that the shared platform could theoretically support. Generic foundations are
  allowed only when they serve the approved slice; visible routes, nav, seeded
  cards, mock data, screenshots, and handoff language must not imply another
  vertical is built. For the current admin-web review, the visible slice is Preventive Care (PC)
  Vaccination plus Admin/Data Ops config and vaccination SOP policy.
- Current admin-web frontend scope supersedes the old dashboard/admin product
  surface. For admin-web UI work, read
  `context/frontend/current-admin-web-scope.md`: build the connected Admin
  Config + Preventive Care (PC) Vaccination + vaccination execution context slice (rendered inside
  /vaccination, with shed detail under /vaccination/execution/sheds/{shed_id}),
  with Control Tower summarizing only process
  gaps. Old Operations/Legacy/SOP/counts/import routes are removed from active
  admin-web and must not be rebuilt unless scope is explicitly reopened. Parks is
  NOT a separate vaccination product route or sidebar entry.
- **NON-NEGOTIABLE — the ONLY admin-web UI/UX source of truth is the mock**
  `mock/goatos-dashboard-mock.html`. PORT its layout, structure, table shapes,
  empty states, icon system, spacing, and density. It is **not a color theme**.
  **Never reuse/adapt/recolor old admin UI** (`admin-primitives.tsx`, old
  cyan/slate palette, emoji icons, collapse-to-KPI layouts) — the old admin UI
  is gone; rebuild from scratch to the mock. MANDATORY before any frontend
  `git mesha-push`: `npm --prefix apps/admin-web run check:mock-fidelity` must
  pass + visual compare to the mock.
- Frontend product taxonomy is non-negotiable:
  - **Vertical** = business operating domain/department, such as Preventive Care (PC), Parks,
    Procurement, Admin/Data Ops, Counts, Breeding, Inventory, HR/People, Farmer
    Network. A vertical owns operational context.
  - **Module** = a concrete workflow/product inside a vertical, such as
    Preventive Care (PC) -> Vaccination, Preventive Care (PC) -> future Treatment/Deworming, Procurement -> Source
    Entry, or future Parks-owned modules. Parks is a scope/context dimension for
    vaccination execution, not the owner of a vaccination module.
  - **Command lens** = top-level cross-module screen, not a vertical or module:
    Control Tower, Action Center, Calendar, Protocol Adherence, and Workflows.
  Preventive Care (PC) is a vertical and must not use the syringe/injection icon; the syringe/
  injection icon belongs to the Vaccination module. Counts is a separate
  vertical, so Control Tower must not show raw goat census totals as its own
  KPI. Control Tower is for gaps, adherence, exceptions, escalations, and next
  actions.
- Frontend command-room/authority guardrail: Control Tower, Action Center,
  Calendar, Protocol Adherence, and Workflows are top-level screens only. Config
  and SOP Library are top-level Admin / Data Ops authority screens only. Do not
  duplicate them under procurement/source-entry, Preventive Care (PC), Parks, or any future
  vertical as routes, redirects, tabs, or nav items. A vertical can feed those
  top-level screens through a selected domain/filter/lens such as
  `?domain=procurement` or `?category=vaccination`, but it must not create
  nested routes like
  `/vaccination/adherence`, `/vaccination/config`,
  `/procurement/source-entry/action-center`, `/procurement/source-entry/control-tower`,
  or any `/parks/vaccination` nested command paths. Vaccination execution
  renders INSIDE /vaccination, never as a separate Parks route.
- Config / Protocol Rules is a generic Admin / Data Ops authority screen
  (`/config`) for CEO/COO/superadmin users. It is not owned by Preventive Care (PC) / Vaccination.
  Preventive Care (PC) / Vaccination may link to `/config?category=vaccination`, but the Config UI
  must stay category/schema-driven: changing category changes the form fields and
  `rule_dsl`; do not show vaccination fields for `feed_direction`.

Code navigation (graph-first):

- For code-structure questions (callers, callees, dependencies,
  blast-radius/impact, diff review, architecture, hub/dead-code), query the
  `code-review-graph` MCP tools before broad file scans. Read files for what the
  graph cannot see: constants, config values, HTTP route strings, error text,
  and uncommitted code.
- Route by task shape, not ritual:
  - cold/review/diff: `get_minimal_context_tool` first, then one targeted graph
    query;
  - known symbol: go straight to `query_graph_tool`;
  - keyword/domain lookup: `semantic_search_nodes_tool`, then targeted graph;
  - single file/function read: read the file, then graph only for impact.
- Graph is the fast first pass for traversal; native Grep/Read is the fallback
  for graph blind spots. One graph query replaces many grep/read cycles when the
  question is graph-shaped.
- Setup is per-machine and optional. The graph DB (`.code-review-graph/`) and
  Graphify outputs (`graphify-out/graph.json`, reports, cost files, cache) are
  gitignored and regenerated locally. To enable the portable setup, run
  `make ai-setup`; to rebuild local graphs, run `make ai-rebuild`; to verify the
  clone is wired without committed graph artifacts, run `make ai-doctor`.
- Maintainer-local only: the Graphify Mesha wiki/doc/visual graphs
  (`mesha_docs_graph`, `mesha_visual_graph`) are built from sources outside this
  repo and cannot be reproduced here. Use them if already configured; otherwise
  skip and use Grep/Read.
- **Agent tool choice (human)**: before a non-trivial task, read
  `docs/ai/agent-tool-routing.md` — Cursor for admin-web UI and small fixes;
  Claude Code (terminal `claude` in repo root) for contracts, backend engine,
  migrations, and multi-module work. No second IDE required.

Current repos:

- `dashboard/` - current live CEO-style Next.js dashboard; do not edit for Goat OS rewiring. Snapshot/copy into `goatos/apps/admin-web/`.
- `vgoats-dashboard/` - current live investor/reduced dashboard; do not edit for Goat OS rewiring. Snapshot/copy into `goatos/apps/investor-web-shadow/`.
- `procurement_app/` - reusable React Native camera/upload/team ideas; currently procurement/Firebase coupled.
- `website/` - public MESHA site; business-context reference only, out of Goat OS core.
- `slack-automation-scripts/` - legacy Slack/Sheets/App Script automation.

Organization boundaries:

- Mesha/VGoats, Heva, and Slice are separate businesses and must never be
  mixed in GitHub or Google Cloud operations.
- Goat OS belongs to Mesha/VGoats. Google Cloud work for Goat OS targets the
  `vgoats.com` organization and future `goatos-dev`, `goatos-stg`, and
  `goatos-prod` projects.
- Do not use Heva projects/orgs, Slice projects/orgs, or `hevaplatform` for
  Goat OS work.
- Do not modify or replace the legacy `goatos-sheets` project while creating
  Goat OS projects.
- Before any cloud/GitHub command that creates, updates, deletes, grants IAM,
  links billing, deploys, or changes configuration, verify and state the active
  account, organization, folder, project, and target repo. If the target is not
  Mesha/VGoats for Goat OS work, stop and correct context first.
- For read-only Google-backed data pulls, Cloud SQL queries, dashboard issue
  CSVs, or any request phrased as "use gcloud/browser login", follow
  `docs/runbooks/google-cloud-environments.md` -> `goatos-dev Read-Only Cloud
  SQL Access` before touching Chrome or dashboard UI. The default source is
  gcloud + Secret Manager + Cloud SQL Auth Proxy + Postgres, not dashboard DOM
  scraping.
- For GitHub operations in this repo, use the Mesha/VGoats repository token
  path: `git mesha-push main` for pushes and the `MESHA_GITHUB_PAT`-backed
  remote URL for direct remote/CI verification. Do not rely on whatever `gh`
  account is active; this workspace may also have Heva and Slice GitHub
  accounts configured, and those must not be used for Goat OS repo authority.
- Create Goat OS cloud resources under `vgoats.com`, preferably in a `goat-os`
  folder, or directly under the org if folder creation is not available. Do not
  create Goat OS resources inside `system-gsuite` or `apps-script`.

Do:

- Keep architecture facts in `context/`.
- Use `.agents/skills/goatos-build/SKILL.md` as the active agent reference map.
- Use ports/adapters for replaceable vendors and tools.
- Use OpenAPI REST/JSON for web/mobile app APIs.
- Use JSON Schema for form DSL and event payload contracts.
- Use protobuf/gRPC only behind the app API boundary when a real internal workload needs it.
- Keep frontend/mobile data access behind generated clients and app APIs.
- Golden frontend rule for Codex, Claude, and every developer using this repo:
  admin-web/operator-mobile are renderers, not product-truth owners. Backend
  OpenAPI/app contracts must own visible navigation, route availability, page
  titles, section/table labels, filter/sort/page-size semantics, chips/tabs,
  row-click params, drawer/action labels, empty/error copy, disabled reasons,
  and summary-vs-detail field sets. Frontend may own layout, CSS, responsive
  density, icon-token rendering, focus/hover state, and local open/closed or
  selected-row state only. If a visible label/control/action is hardcoded in a
  frontend page, either move it into a backend contract plus OpenAPI/generated
  client, or document the temporary exception in `context/frontend/` before
  shipping.
  Backend-owned does not mean backend-code hardcoded live data: tenant/location/
  person/goat/shed/vendor/operator IDs, park codes/names, capacities, role/actor
  scope, permissions, and business-managed dropdown vocabularies must come from
  Postgres/source-backed config and be compiled into the contract by backend.
  Stable UI text that rarely changes (nav/page titles, table/filter labels,
  chips/tabs, empty/error copy, disabled reasons) belongs in the backend
  bootstrap contract; when it needs runtime governance, store it as tenant-scoped
  `admin_ui_config_entries` and compile it into `/admin-web/bootstrap`.
  These entries may not relabel live/module-DB-owned options such as parks,
  sheds, breeds, SOP labels, feed items, or role/grant scopes, and may not
  override semantic option metadata such as source-system publishability.
  Frontend must not ship local defaults that later get replaced by async config.
  Backend code may hold only product contract shape, compile mapping, and
  intentional default skeletons for missing optional UI config rows; live/domain
  values stay in canonical module tables.
- For frontend code changes, perform rendered visual QA before pushing. Open the
  changed local page, capture and inspect screenshots, and compare with the
  authoritative UI/UX source of truth, the mock `mock/goatos-dashboard-mock.html`
  (port its structure, not just its colors). Old dashboard/admin pages are NOT
  the visual target and must not be reused/recolored. Before push, run the
  mandatory gate `npm --prefix apps/admin-web run check:mock-fidelity`.
  Check pixel-level UI quality: sidebar/nav alignment, tab/title spacing,
  typography, color, card padding, chart sizing, labels, icons, empty space,
  overflow, clipping, and desktop/narrow responsive states. Do not accept
  typecheck/build or a `missing_config` page as frontend visual proof. For
  admin-web, run `npm --prefix apps/admin-web run smoke:visual:live` when the
  local backend/admin-web can be started; it captures desktop/narrow
  screenshots and runs layout/a11y/token-leak checks. Open the resulting images
  under `.codex-goatos-render/admin-web-screenshots/` and include the screenshot
  review result in the handoff before pushing. Build passing means only that the
  code compiles; it does not mean the UI ships.
- Read wide, write narrow: agents may inspect the whole tree, but edits must stay within declared task scope.
- Treat million-goat scale as a hard requirement on every design, prompt, and
  code change. Before accepting any new query, worker, import path, reporting
  path, or UI data flow, check the scale shape: tenant/run scoped, indexed,
  chunked or paginated, bounded in memory/goroutines, idempotent for retries,
  and covered by query-plan validation when it touches large tables.
- Treat the operational kernel as the golden rule for every feature. Read
  `context/architecture/operational-kernel.md` before designing or implementing
  triggers, obligations, reminders, notifications, deadlines, escalations,
  dashboards, Calendar, Action Center, Protocol Adherence, Workflow, or
  process-integrity views. Every feature must answer: what process was expected,
  was it followed, where did it break, who owns next action, what is due by
  when, what evidence proves it, and what alert/escalation fires when a deadline
  is crossed.
- Treat idempotency as a mandatory write-path contract for every mutating API,
  worker, importer, webhook, state transition, outbox producer/consumer, server
  action, and UI-triggered write. Each write path must accept or derive a stable
  idempotency key or operation identity, persist that key and a semantic request
  fingerprint in the same transaction as the side effects, return the original
  result for an exact replay without rerunning side effects or outbox work, and
  reject a same-key different-payload replay or return the original result with
  no new side effects. The SQL pattern `ON CONFLICT DO UPDATE` with only
  `idempotency_key = EXCLUDED.idempotency_key` is not sufficient when later code
  can still mutate state. Tests must cover first call, exact replay, same-key
  different-payload replay, and downstream duplicate prevention.
- For dashboards or reports that slice data by month, date, breed, farm, shed,
  load, category, status, gender, operator, source, or similar dimensions, use
  the canonical rule in `docs/decisions/high-scale-dashboard-projections.md`
  before coding.
- Add observability for new APIs/workers: latency, errors, DB pressure, queue lag, DLQ, and media failures.
- Goat identifiers (RFID, old tag, breed, farm, shed, partition) are operational
  livestock business data, NOT PII. Log them in diagnostics so a failure is
  traceable to the exact goat/row. The only logging redaction rule is secrets:
  never log credentials, tokens, or service-account JSON. Repo hygiene is
  separate and still applies: do not commit raw private source files or row
  dumps to git.
- Construct backend loggers via `backend/internal/platform/observability`
  (env sink `GOATOS_OBS_SINK`: `stdout_json`/`otlp`/`gcm`); do not hand-roll
  `slog.New` in new code. Log once at boundaries with trace/request/tenant/
  import_run_id context, and recover-and-log panics at goroutine edges. See
  `docs/decisions/observability.md`.
- Keep committed project docs role-based rather than person-based. Use labels
  such as data owner, reviewer, operator, CEO/internal admin, or vendor instead
  of individual names unless a legal/contract artifact explicitly requires a
  named person.

Do not:

- Local dev servers (`:3300` admin-web, `:8080` backend): the workspace owner has
  granted agents (Codex and Claude) STANDING authority to stop, restart, re-port,
  or `next build` over them WITHOUT asking — just do it when the work needs it
  (clean build, or an expired local token making routes redirect to `/login`;
  restart with `npm --prefix apps/admin-web run dev:local` to re-mint a fresh
  token). Do not pause to ask permission for a restart/rebuild. The only
  discipline: restore the server on the SAME port, never silently change ports,
  don't run `next build` concurrently with a live `next dev` on the same `.next`
  (stop it first), and if you break it, restore it. See
  `apps/admin-web/AGENTS.md` → "Local Dev Server Safety" for the full rule. This
  applies to every agent (Codex and Claude).
- Do not reintroduce old staging labels as architecture.
- Do not commit generated Graphify/CRG graphs. `graphify-out/graph.json`,
  `manifest.json`, `GRAPH_REPORT.md`, `graph.html`, `cost.json` and the
  `.code-review-graph/` DB are gitignored and machine-regenerated locally. Commit
  ONLY the setup docs, rules, hooks, and generation scripts — never the graph
  artifacts themselves. Run `make ai-doctor` before pushing AI-tooling changes.
- Do not let frontend/mobile read BigQuery, Sheets, Firestore, GCS, or operational databases directly.
- Do not spread vendor SDK calls through product code.
- Do not modify current live dashboard repos while building Goat OS copies.
- Do not add unbounded goroutines, full-table/full-herd API scans, direct media proxying through APIs, or dashboard raw BigQuery scans.
- Do not use direct gRPC for browser/React Native product clients without a new written ADR.
- Do not duplicate architecture decisions across random docs.
- Do not put individual staff/founder/vendor names into PRDs, TRDs, runbooks,
  prompts committed as docs, status files, or skill references when a role label
  is enough.

Validation expectation:

- Run the narrowest relevant typecheck/build/test command for changed code.
- For DB query or migration changes on large tables, verify the indexed access
  path and add/update `make validate-sqlc-plans` coverage when the query is on a
  hot path or can touch import/goat/event/counter rows at scale.
- At phase closeout, compare code/contracts/migrations/tests against PRD/TRD and
  update context/skills/agent references if implementation changed the truth.
- For docs-only edits, run greps for stale terms when the user has explicitly banned wording.

Morning README update expectation:

- For Goat OS work sessions that start in the morning, check whether `README.md`
  reflects the latest pushed project status before moving deep into new
  implementation work.
- If phase progress, shipped backend/frontend pieces, deploy gates, real-data
  import status, or next-step priorities changed, update `README.md` with
  executive status wording and push it.
- Be precise: do not call Phase 1 shippable until auth/RBAC, frontend screens,
  production event egress, and real data-run gaps are actually closed.

Workflow documentation expectation:

- If GitHub Actions workflows or CI guardrail scripts change, update
  `docs/runbooks/github-workflows.md` with clear project-facing wording in the
  same change.
- The runbook must explain what each workflow does, when it runs, what temporary
  services it starts, and what common failures mean.

Mock auto-push expectation (Codex AND Claude):

- Standing order (2026-06-25): whenever you edit the ops-console mock
  `mock/goatos-dashboard-mock.html`, commit and push it IMMEDIATELY — do not wait
  for confirmation, so the pushed copy is never behind local edits.
- Run `tools/agent-hooks/push-mock.sh` after editing the mock (Claude also wires
  it to a Stop hook). The script commits ONLY the mock file and pushes `main` via
  `git mesha-push` — it never `git add -A`, so unrelated in-flight work is left
  untouched. It no-ops when the mock is clean.
- This applies only to the mock. Other code/doc changes follow the normal
  review-and-push flow.
