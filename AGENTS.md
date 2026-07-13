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
(in chat, WhatsApp screenshots, Preventive Care sign-off, or ad-hoc instructions) that may
**contradict or supersede** existing docs, seeded config, implemented kernel
behavior, or a prior decision in the same thread:

1. **Stop and surface the conflict first** — quote the old rule/source and the new
   instruction side by side. Do **not** silently pick one, blend them, or change
   code/docs on assumption.
2. **Ask explicitly** which rule wins, whether the old rule is retired, or
   whether both apply in different scopes (species, stage, procurement path, etc.).
3. **Implement only after confirmation** — then update the canonical source in the
   same change as the code (`docs/preventive-care-vaccination/vaccination-rules.md`,
   published `rule_dsl`, TRD/ADR, or this file when appropriate).

Ambiguity is not approval. Informal agreement in a screenshot or chat applies to
**that** scenario until it is written into the source contract.

Confirmed Preventive Care (PC) vaccination override: never ask about, model, seed,
import, expose, or schedule from mother-not-vaccinated / unknown-mother status.
The private source/wiki may contain that branch, but GoatOS ignores it. Mothers
are kept vaccinated operationally, and every kid uses the approved standard
schedule in `docs/preventive-care-vaccination/vaccination-rules.md`.

## Consolidated Defect-Ledger Closure (Mandatory)

When asked to fix/continue/close the consolidated audit ledger or its bugs, read
both of these before editing:

- `context/repo-audits/last-35-commits-consolidated-bug-ledger.md`
- `context/repo-audits/consolidated-ledger-defect-closure-program.md`

Select one highest-priority unblocked root defect (or an inseparable cluster),
reconstruct the live count from the file, and follow the closure program across
every affected backend, SQL, API, admin-web, Android, architecture, performance,
memory, retry, pagination, security, E2E, observability, and CI/CD layer. Do not
mark a row fixed until its current-SHA proof packet and independent Claude/Codex
counter-review pass. Merge duplicate-root evidence instead of inflating counts.
`CLAUDE.md` and `CODEX.md` remain thin shims to this shared rule.

Read first:

- `context/README.md`
- `SKILLS.md`
- `.agents/skills/goatos-build/SKILL.md`
- `context/architecture/final-architecture.md`
- `context/frontend/final-frontend-mobile-backend-architecture.md`
- `docs/mobile/README.md` (Goat OS Android app — one common role-aware app for field operator + leadership; native Kotlin + Compose, `apps/goatos-android/`, app id `sg.mesha.goatos`; read before any mobile work)
- `context/forms/final-forms-sop-engine.md`
- `context/analytics/final-analytics-infra.md`
- `context/agents/ai-agent-context-and-protocols.md`

Android verification is not allowed to stop at a missing inherited `JAVA_HOME`
or unavailable USB phone. Run `make android-doctor`; repo tooling resolves the
pinned JDK/SDK itself. Run `make android-dev-run`; it prefers an authorized
physical phone and otherwise starts/waits for the configured emulator. See
`docs/runbooks/android-dev-device.md`. JDK 21 runs Gradle/AGP; app bytecode
compatibility remains Java/Kotlin 17.

Historical planning/archive docs were removed from the active tree. If a human
explicitly asks for archaeology, use git history or source material rather than
normal build docs.

Purpose:

- Goat OS is the operating system for mixed-species herd-animal identity, health, vaccination, genetics, breeding, workforce, SOP tasks, media proof, verification, devices, commerce interfaces, and analytics.
- Canonical backend/data model/app APIs are built fresh.
- `Goats and Parks.docx` is the base source for herd-animal and park semantics
  across every slice. Any feature touching herd-animal identity, species/breed
  labels, park/shed scope, shed tags, lifecycle/stage, pregnancy/lactation/
  warm-up/fattening, feed safety, weighing, handling, medicine administration,
  park roles, or feed sessions must start from
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
    Entry, or future Parks modules. Parks is a scope/context dimension for
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
- Setup is per-machine and agent-enforced on fresh clones: if `make ai-setup`
  has never run on this clone, the committed `ai-setup-guard` hook blocks the
  first real tool call with bootstrap instructions — run `make ai-setup` first,
  then resume the task (see `docs/ai/README.md`). The graph DB (`.code-review-graph/`) and
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
- If any Google auth surface expires or cannot refresh non-interactively
  (`gcloud`, ADC, Cloud SQL Auth Proxy, Secret Manager, Google Drive/Docs/
  Sheets, or a Google browser session), do not stop at "token refresh failed"
  when the task requires Google access. Use browser-based reauthentication
  immediately: `gcloud auth login ravi@mesha.sg` for CLI user credentials,
  `gcloud auth application-default login` for ADC, or the relevant browser/
  connector sign-in for Drive/Docs/Sheets. After reauth, re-verify the active
  account, organization, project, and target before any write/deploy/config
  mutation. For Goat OS, the expected account is `ravi@mesha.sg` and the
  expected Google Cloud organization is `vgoats.com`.
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
- Treat every test or script labeled E2E as a production-path proof, never a
  seeded readback. E2E fixtures may insert only external/input facts required to
  start the scenario (for example tenant, herd animal, location, workforce,
  inventory, or authored configuration). Obligations, batches, completions,
  verification outcomes, SOP tasks/submissions, notifications/escalations,
  cancellations, and Calendar/process-integrity projections must be produced by
  the same service, API, durable event consumer, sweeper, or projector used in
  production. A narrower test that intentionally seeds derived state must live
  with the owning package as an integration/read-model test and must not appear
  in an E2E report. `tools/agent-hooks/check-e2e-kernel-integrity.sh` enforces
  this rule for both Claude and Codex and in CI.
- Couple migrations to initial seed setup. If a migration changes tenant/goat/
  RFID/location, HRMS/ownership, founder grants, protocol/SOP/capacity,
  obligation/completion/proof, notification/verification, or app-visible
  projection/read-model tables, update the matching seed command,
  seed/projection test, or seed runbook in the same patch. The
  `seed-migration-guard` target is part of `make guardrails` and blocks
  schema/read-model drift where source rows seed correctly but the live app reads
  empty or missing projection tables. See
  `docs/runbooks/initial-seed-migration-coupling.md`.
- Do not make seed scripts hand-fill every new table. Classify setup tables as
  source/canonical, derived/read-model, static catalog/config, or
  operational/audit/event. Derived app-visible tables must be rebuilt from
  canonical data through `make seed-closeout` / `tools/dev/seed-closeout.sh`.
  New projection tables also need access-pattern indexes, freshness/version
  state, and an explicit partitioning decision.
- Do not start local, staging, or production app code against a database that is
  behind that build's migrations. Apply migrations first, seed only canonical
  source truth second, run deterministic closeout/projectors third, then start
  API/admin/workers or mark the environment green.
- Use `.agents/skills/goatos-build/SKILL.md` as the active agent reference map.
- Use ports/adapters for replaceable vendors and tools.
- Use OpenAPI REST/JSON for web/mobile app APIs.
- Use JSON Schema for form DSL and event payload contracts.
- Use protobuf/gRPC only behind the app API boundary when a real internal workload needs it.
- Keep frontend/mobile data access behind generated clients and app APIs.
- Golden frontend rule for Codex, Claude, and every developer using this repo:
  admin-web/goatos-android (mobile) are renderers, not product-truth owners. Backend
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
- Treat million-animal scale as a hard requirement on every design, prompt, and
  code change. Before accepting any new query, worker, import path, reporting
  path, or UI data flow, check the scale shape: tenant/run scoped, indexed,
  chunked or paginated, bounded in memory/goroutines, idempotent for retries,
  and covered by query-plan validation when it touches large tables.
- NEVER write these scale anti-patterns in `backend/internal/**` (request paths,
  app services, worker repo methods). They are fast at ~1k rows and fatal at 1M.
  Each is machine-blocked by `make scale-guard` (CI `guardrails` job); named,
  explained, and given its approved alternative in
  `docs/decisions/scale-anti-patterns.md`. The rule underneath all of them:
  **compute-on-write (projections), never compute-on-read.**
  - **compute-on-read / god-CTE** — reconstructing derived state from raw
    event/instance tables per request via a big multi-CTE query. Use a
    materialized read model updated on write; the request does an indexed lookup.
  - **capped read-time rollup presented as truth** — fetching a larger raw page,
    grouping in app/service/frontend state, then clearing pagination/cursor and
    showing the collapsed card/count as business truth. This is banned for
    Calendar, Action Center, and other operator projections. Put the grouped row
    in the projector/read model and prove it with seed/projector E2E.
  - **full (stop-the-world) MV refresh** — `DELETE FROM <projection> WHERE
    tenant_id` + full reinsert. Use incremental (outbox-delta) maintenance, or a
    version-swap; never whole-tenant delete+reinsert.
  - **N+1 query** — a `.Query/.QueryRow/.Exec/.SendBatch` inside a `for`/`range`.
    Use one set-based statement (`UNNEST`, `INSERT ... SELECT`, `CASE` bulk update).
  - **N+1 fan-out (the nested "N+2" case)** — a ctx-taking call to an injected I/O
    dependency (repo/reader/port/client/roster/ownership) inside a `for`/`range`,
    where the real `.Query/.Exec` sits one adapter layer down — invisible to the
    raw-driver **N+1 query** check above. "Small data, still slow": one round trip
    per row, so a 25-row page becomes 51 serial reads. Machine-blocked as the
    `n-plus-one-fanout` rule (`make scale-guard`, distinct from `n-plus-one`;
    baselined debt in `tools/scale-guard/baseline.txt`). Fix by batching to a
    single `*ByIDs` / `= ANY($1)` read (as `ShedSummary` now does with
    `ShedOwnerships`), not by looping a per-item service/port call.
  - **OFFSET pagination** — `LIMIT/OFFSET` with a growable offset. Use keyset/cursor.
  - **non-SARGable predicate** — `lower(col) LIKE '%x%'` / function on an indexed
    column. Use a normalized column, expression index, or `pg_trgm` GIN.
  - **polling full scan / unbounded worker tick** — copy the keyset-chunked
    `FOR UPDATE SKIP LOCKED` claim used by the obligation/idempotency sweepers.
  - **non-terminating pagination loop** — a read-page loop with no cursor advance.
    Guarantee forward progress (monotonic cursor or exclude processed rows).
  If a case is genuinely bounded, annotate it `// scale-guard:ignore: <reason>`;
  do not disable the guard. A green latency gate today means "correct shape", not
  "1M-proven" (gates run at ~1k rows — see the ADR's runtime-gap section).
  The admin-web twin lives in `apps/admin-web/**`: a Next.js SSR helper that drains
  a paginated endpoint cursor-by-cursor into one array to compute a KPI (the
  `searchAllGoats` full-herd walk, removed in `810bc1b3`) is machine-blocked by
  `make admin-web-request-reads-guard`
  (`tools/agent-hooks/check-admin-web-request-reads.mjs`); read a projection/summary
  endpoint instead. Rule + rationale in `docs/decisions/scale-anti-patterns.md`.
- Treat every aggregate/projection/card/summary as a grain-and-identity proof,
  not an arithmetic exercise. Before writing or approving a query that combines
  `JOIN` with `COUNT`/`SUM`/`GROUP BY`, identify the canonical membership source,
  use the same stable group key on producer and consumer, prove every join is
  1:1 or deduplicate/pre-aggregate the many side, map farm/park/shed/cohort with
  an explicit scope matrix, and keep totals/reminders independent of UI page
  size. Tests must adversarially cover one-to-many fan-out, shifted
  due-vs-execution dates, every supported scope depth, page boundaries, and the
  live DB status matrix when status buckets exist. Add the nearby
  `projection-review:` evidence marker defined in
  `.agents/skills/goatos-code-review/references/aggregates-and-projections.md`
  and run `make aggregate-projection-guard`; the required CI guard includes
  committed, staged, unstaged, and untracked changes.
- E2E publishing rule for Codex, Claude, and every feature agent: any generated
  E2E result for a feature, fix, audit, or scale gate must be committed inside
  this repo and surfaced on the GitHub Pages CI report site before handoff. The
  report must appear as a card/list item on the main CI reports index
  (`https://vgoats.github.io/goatos/`), not only as a standalone deep link.
  If the E2E belongs to an existing category (for example a vaccination kernel
  story belongs inside `/e2e-report/`), add it inside that category's report;
  do not create another root card. Create a new root card only for a genuinely
  new report category, then wire that category in `.github/workflows/pages.yml`
  and document it in `docs/runbooks/github-workflows.md`. The detail page must
  follow the existing E2E report visual contract: self-contained HTML with
  title/subtitle, summary tiles, pass/fail/pending badges, report sections, and
  readable code/evidence blocks. It must explain the test in enough detail for
  a reviewer who did not write the code: what behavior is under test, why it
  matters, setup/data, action/trigger, assertions, evidence source, and any
  certification boundary. One-line headings or test names are not enough. Do
  not publish raw markdown, a bare `<pre>`, screenshots-only evidence, or a
  hidden artifact as the final report.
  Do not leave E2E reports only in `/tmp`, scratchpads, attachments, local
  `.codex/` or `.claude/` folders, or chat. State clearly when a report is local
  E2E only and not staging or production certification. Before handoff, verify
  the live root index and the report URL with `curl`; if GitHub Pages caching is
  in play, include a `?v=<commit-sha>` cache-busting URL plus the workflow run.
- Treat the operational kernel as the golden rule for every feature. Read
  `context/architecture/operational-kernel.md` before designing or implementing
  triggers, obligations, reminders, notifications, deadlines, escalations,
  dashboards, Calendar, Action Center, Protocol Adherence, Workflow, or
  process-integrity views. Every feature must answer: what process was expected,
  was it followed, where did it break, who owns next action, what is due by
  when, what evidence proves it, and what alert/escalation fires when a deadline
  is crossed.
- For any projection-backed serving read behind a freshness/coverage gate
  (Vaccination execution/operations/shed, CT/AC/PA, Calendar), follow the
  Serving-Read Freshness Contract in
  `docs/decisions/high-scale-dashboard-projections.md` (also in the
  `kernel-scale-lens` skill), Claude AND Codex: (1) freshness TTL must exceed the
  projector refresh schedule (jitter headroom) and is AGE-based — never widen the
  TTL to mask a date-coverage bug; (2) date coverage is inclusive-query vs
  exclusive projection `date_to` — project one day beyond the max query range
  (45d ⇒ 46d), and fixed-date tests seed the window around their fixed dates, not
  `now±N`; (3) a rebuild keeps serving last-known-good and a failed rebuild never
  clobbers it; (4) reads served entirely from a bounded canonical index
  (completed/accepted history) are NOT gated on the hot projection; (5) a prune of
  non-serving versions re-derives `serving_projection_version` inside the DELETE,
  never a version captured before the txn/advisory-lock released.
- Treat every Android READ screen as offline-first with Room as the single source
  of truth for the UI (hard rule — Claude, Codex, and humans). Backend owns the data;
  on-device, the screen renders from Room and the network refresh runs in the
  background (stale-while-revalidate): persist every backend read response to Room,
  have the repository expose a `Flow` the ViewModel observes, refresh-on-open to upsert
  Room (which re-emits), and show a sync/stale indicator — NEVER a blank/loading wall
  on re-entry when cached data exists. A network-only read repository (a thin
  `api.xxx()` pass-through with no Room persistence) is BANNED for screen-facing reads;
  new read models ship with their Room entity + DAO + Flow from day one. Do not call
  the app "offline-first" until the read models are cached (bootstrap + the write
  outbox already are; Calendar/Control-Tower/Execution/Adherence/Insights must be
  migrated). Full rule + the NetworkBoundResource pattern:
  `docs/decisions/android-offline-first.md`; refs the Android data-layer + offline-first
  architecture guides.
- Treat every Android Room schema change as an installed-APK upgrade contract, never just a
  fresh-install schema (hard rule — Claude, Codex, humans). Room builds a DB two ways: a fresh
  install runs `createAllTables` (every @Entity), but an in-place upgrade runs ONLY the registered
  `Migration` objects and then validates against the @Entity set — so an @Entity added to a
  @Database with no migration to CREATE its table compiles, works on fresh installs, and CRASHES
  every upgrade on open (`Migration didn't properly handle <table>`). This actually shipped
  (roster_timetable_cache / roster_coverage_cache, MOB-007) and a plain in-memory Room test is
  blind to it. Required: `exportSchema = true` + committed `schemas/<db>/<version>.json`; every
  version bump ships its `Migration(N-1, N)` that creates exactly the new tables/columns/indices;
  additive + non-destructive (no `fallbackToDestructiveMigration` — the outbox holds unsynced
  operator writes, the cache is the offline SSOT); and BOTH a schema-equivalence `*MigrationTest`
  AND an upgrade-crash `*UpgradeCrashTest` (seed an old-version file via a test-only old @Database,
  reopen with current schema + real migrations, assert no crash + data preserved). Machine-blocked
  by `make room-migration-guard` (`tools/agent-hooks/check-room-migration-safety.mjs`, diff-scoped,
  in the CI `guardrails` job). Full rule: `docs/decisions/room-migration-safety.md`.
- NEVER fetch more than one screen-page of rows on mobile/web (hard rule — Claude,
  Codex, humans). A phone viewport holds ~7-10 items; pulling 50/200/1000 rows to
  render is the mobile twin of compute-on-read. Machine-blocked by `make mobile-guard`
  (`tools/agent-hooks/check-mobile-list-fetch.mjs`, diff-scoped in CI so a commit with
  no mobile code passes instantly); rule + rationale in
  `docs/decisions/mobile-data-fetch-anti-patterns.md`. The rules:
  - **Calendar week/month overview = DOTS ONLY** — one per-day marker (a drive exists;
    optional tone) from a backend day-marker set (`includeDateMarkers` /
    `CalendarDateMarkerDto`). Never fetch or parse a day's events to draw the grid.
  - **Every drill level paginates** — L1 day list, L2 sheds, L3 vaccine-capture
    (done/pending/skipped animals) are each a keyset page of **~20** with infinite
    scroll (prefetch next at item ~17-18). Never request > ~20 rows in one page.
  - **A vaccination drive is a mix of SHEDS, never grouped by vaccine** — a shed may
    bundle same/different vaccines, but the drive is shed-scoped. Coverage-by-vaccine is
    a metric, not the drive grouping.
  - Parse/transform each field ONCE (never re-parse inside `.find`/`.filter` → O(n^2)),
    off the Main thread (`Dispatchers.Default`, ideally in the repo via `flowOn`).
  - **Room is the single source of truth, so pagination binds BOTH layers** — the network
    fetch AND the Room read the UI observes use the same keyset + ~20 page size. NEVER
    `SELECT *` / `observeAll()` / an ever-growing accumulated blob; the observed read is a
    bounded keyset window (Room `PagingSource`; Paging 3 + `RemoteMediator` for large lists).
    Otherwise the over-fetch just moves from network to DB.
  If a case is genuinely bounded (e.g. a fixed 7-cell week loop) annotate the line
  `// mobile-guard:ignore: <reason>`; do not disable the guard.
  The retention twin is memory, not fetch size: an in-heap cache/accumulator that
  grows with no cap/TTL/eviction, or a DAO reading a whole table into memory
  (`observeAll` `SELECT *`), OOMs the phone at scale (fixed in `7058fff2` +
  `d58acac2`). Machine-blocked by `make android-bounded-memory-guard`
  (`tools/agent-hooks/check-android-bounded-memory.mjs`, diff-scoped) — use an
  `LruCache` or a Room `JsonBlobCacheDao` with `readCachedJson` (TTL) +
  `enforceCacheBounds` (row/byte cap), or filter the DAO read to active rows
  (`WHERE status IN (...)`) / a `LIMIT` window.
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
- Treat a state transition and the sync of any derived read model it OWNS as ONE
  atomic transaction. A record must never reach its published/committed state
  while a read model it is the sole writer of failed to save. Do the derived
  upsert AND any post-write parity/verification check INSIDE the same DB
  transaction as the state change, so a sync failure rolls the whole transition
  back — no status flip, no outbox event, no audit row, no partially-written read
  model. A post-commit "best-effort" sync is allowed ONLY as a fallback for an
  already-committed replay or an adapter without transactional support, never as
  the first-commit path. Canonical case: publishing a vaccination protocol version
  upserts + parity-checks `rule_dsl.capacity` into `vaccination_capacity_config`
  inside the publish transaction (`PublishVersionWithCapacity` /
  `PublishVersionWithDerivedRules`), and a parity mismatch
  (`ports.ErrCapacityParityMismatch`) rolls the publish back. Every such flow needs
  a rollback regression test — failed sync ⇒ source stays in its prior state with
  zero side effects; see `TestPublishVersionWithCapacityRollsBackOnSyncFailure`.
- Treat authored config/business values as validate-or-reject, never
  silently-default. A field that is PRESENT but out of range (e.g.
  `rule_dsl.capacity.max_per_day < 1`, `max_buffer_days < 0`) must FAIL the
  publish/save with a clear error, not be rewritten to a default business value
  the author never entered; defaults apply ONLY to genuinely-absent fields.
  Frontends must keep a cleared field distinct from an explicit `0` (a blank input
  publishes the declared default; an explicit out-of-range value is sent verbatim
  so the backend rejects it) — never coerce blank to `0` or to an invented value,
  and never let a React default become authored business truth.
- Treat the clinical defer set as a mandatory medical safety block, never an
  authored subset (C35-010). The four clinical states `sick`, `under_treatment`,
  `quarantine`, `icu` are non-optional postponement rules per
  `docs/preventive-care-vaccination/vaccination-rules.md`: an animal in any of them
  must have its open vaccination work DEFERRED (held for recovery), never cancelled
  or left scheduled — a wrong medical action is P0 regardless of how cleanly it
  compiles. A published rule's `eligibility.defer_states` may only ADD states; it
  may never drop one of the four. Enforce on BOTH layers: publish/validation must
  REJECT a present, non-empty `defer_states` that omits any mandatory clinical
  state (an empty/absent list maps to the safe full default), and the generator
  must union the mandatory set in regardless of the authored list so an
  already-published partial rule is still safe at runtime. The single source of
  truth is `backend/internal/protocol/domain.MandatoryClinicalDeferStates`
  (`EffectiveClinicalDeferStates` / `MissingMandatoryClinicalDeferStates`) — do not
  re-hardcode the set elsewhere; the SQL siblings
  (`vaccination_eligibility_rollups` usable flag,
  `ListRecoverableDeferredVaccinationGoatIDs`) must stay in sync with it. Mechanical
  backstop: `make clinical-defer-guard` (required in CI).
- CI availability is never a closure blocker (Claude AND Codex). A GitHub Actions
  billing/spending/platform failure — the synthetic `BuildFailed` /
  `(Unknown event)` / zero-job `startup_failure` runs — must NOT be recorded as
  an external blocker or used to defer a fix. When remote GitHub Actions cannot
  execute, run the SAME required CI gates LOCALLY via `make ci-local` (which
  mirrors `.github/workflows/ci.yml` job-for-job: agent guardrails, scale-guard +
  self-test, clinical-defer-guard, mobile-guard, `go test ./...`, sqlc/migration
  validation, admin-web lint/typecheck/mock-fidelity, and the Android
  compile/unit gate with mandatory JDK/SDK; no USB device is required) and treat a green `make
  ci-local` on the exact pushed SHA as the authoritative gate. Record the
  `make ci-local` SHA + result as the current-SHA proof. Restoring org Actions
  billing stays a separate maintainer task, tracked but never blocking closure.
- Treat Goat OS time semantics as India-business-calendar semantics. Physical
  storage may use `timestamptz`/absolute instants, but every business meaning
  derived from those instants — scheduling, due/missed buckets, reminder keys,
  reporting groups, audit-log display, and UI labels — must convert to
  `Asia/Kolkata` first. UTC must never define a Goat OS business day.
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
- Always-on local stack rule (Codex and Claude): when a task needs any local
  frontend, backend, worker, importer, proxy, emulator, database container, or
  other Goat OS service, first check whether it is already running and do not
  stop it just because the immediate command is done. Prefer the persistent
  service wrapper (`make dev-local-service-start`, `make dev-local-service-status`,
  `make dev-local-service-logs`) over foreground one-off terminals for long-lived
  stack work. Leave required services running at the end of the turn/session
  unless the user explicitly asks to stop them or stopping is required to prevent
  machine damage/data loss. If code/env changes require a restart, restart on the
  same ports and health-check before reporting done. Do not finish with a needed
  app stack stopped, and do not leave required servers as active Codex terminal
  sessions that block the final response; use the service wrapper/supervisor and
  logs. Status/final updates must name what is running plus the URL/port. If a
  service cannot be kept running, state the blocker and the exact restore command.
- Founder/builder visibility invariant: `ravi@mesha.sg`, `manohark@mesha.sg`,
  `manju@mesha.sg`, `abhishek@mesha.sg`, and `aryaman@mesha.sg` are the
  platform-owner leadership cohort. In local, staging, and production seed/
  provisioning paths they must be granted `role='ceo_internal'`,
  tenant scope, and the RBAC grants needed for every built visible module.
  New features or visible route changes are incomplete until leadership
  seed commands, bootstrap/nav tests, and docs include the module. RBAC-based
  route visibility applies to non-founder operators, not to these five builder
  accounts.
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
  is enough. The founder/builder visibility invariant above is the narrow
  exception because those exact accounts are provisioning seed truth.

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

<!-- BEGIN TELEMETRY GUARDRAIL (generated by telemetry-guard lane; do not hand-edit inline, extend docs/observability/TELEMETRY_GUARDRAILS.md instead) -->
## TELEMETRY GUARDRAIL (mandatory)

Whenever you add or modify a user-facing surface — an Android screen,
viewmodel, or flow in `apps/goatos-android`; an admin-web route in
`apps/admin-web`; or a new product-meaningful event emitted anywhere — you
MUST wire:

1. **Firebase Analytics event(s)** — `AnalyticsPort.track(...)` with an
   `AnalyticsEvents` constant (never an inline string) on Android; a
   Faro event (`faro`/`trackEvent`/`pushEvent`) or route error-boundary
   coverage on admin-web.
2. **Crashlytics fatal + non-fatal logging** on error paths that surface can
   hit (Android; wiring itself is tracked as TODO — see the doc below).
3. **The relevant funnel/journey step**, when the surface sits on a tracked
   journey (`login → bootstrap → drive-open → scan → vaccination-capture →
   submit`, or a future documented funnel).

Run `make telemetry-guard` (or `python3 tools/telemetry-guard/telemetry-guard.py`)
before committing — it is part of `make guardrails` / `make ci-local
JOB=guardrails` / the `ci` guardrails job, diff-scoped against `origin/main` so
unrelated commits pass instantly.

Use `// telemetry:exempt <reason>` only with a real justification (internal
debug-only screen, pure presentational component, route fully covered by a
parent error boundary) — it is a reviewer-facing escape hatch, not a rubber
stamp.

Full rule, rationale, required symbol names (including what is wired today vs
TODO), compliant/non-compliant examples, and how the guard works:
`docs/observability/TELEMETRY_GUARDRAILS.md`.
<!-- END TELEMETRY GUARDRAIL -->
