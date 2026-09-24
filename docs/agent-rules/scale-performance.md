# Scale Safety, Hot-Path Latency and Kernel Rules

> Moved verbatim from `AGENTS.md` (split 2026-09-24 to keep session start small).
> These rules are as binding as `AGENTS.md` itself. Only file location changed.

> **Active program (2026-09-24):** API p95 budget is 50-100ms target, 200-300ms acceptable, hard max 500ms. Findings, per-endpoint audits and the fix queue live in `docs/perf/2026-09-24-stg-latency/README.md` - read it before any perf work.

- Treat scale-safe design as a hard requirement on every design, prompt, and
  code change, sized to the current release scale target. Per
  `docs/decisions/operational-kernel-5k-50k-scale-envelope.md`, the present
  release envelope is 5,000-50,000 animals, with query-plan proof required at
  the envelope's upper bound (up to ~500k obligation rows); one-million to
  1-5M-animal deployment is the future certification bar, not a present
  release requirement. Regardless of that target, before accepting any new query,
  worker, import path, reporting path, or UI data flow, check the scale shape:
  tenant/run scoped, indexed, chunked or paginated, bounded in memory/
  goroutines, idempotent for retries, and covered by query-plan validation when
  it touches large tables.
- Treat hot API/SSR latency as part of scale safety, not polish. Every
  operator-facing API read, admin-web SSR page bootstrap, dashboard, schedule,
  calendar, worklist, and drawer/list load has a hard sub-500ms budget under the
  API latency policy (`tools/perf/api-latency-policy.mjs`: p90 <= 300ms,
  p95/p99 <= 500ms). A seconds-class load is a bug even when `make ci-local`
  passes; `ci-local` is not latency evidence unless the live latency gate ran
  and recorded samples. Do not fix this with bigger limits, longer timeouts,
  skeletons, prefetch, or frontend caches. Fix the serving shape: narrow endpoint
  for the screen grain/window, indexed/keyset query, batched read, or accepted
  projection/read model.
- NEVER write these scale anti-patterns in `backend/internal/**` (request paths,
  app services, worker repo methods). They are fast at ~1k rows and fatal at 1M.
  Each is machine-blocked by `make scale-guard` (CI `guardrails` job); named,
  explained, and given its approved alternative in
  `docs/decisions/scale-anti-patterns.md`. The rule underneath all of them:
  **compute-on-write (projections), never compute-on-read** — with one scoped
  exemption: per
  `docs/decisions/operational-kernel-5k-50k-scale-envelope.md`, the five named
  Calendar/process-integrity/vaccination-shed/execution/operations screen reads
  carry a scoped `// scale-guard:ignore: 5k-50k-envelope` annotation and serve
  canonical indexed SQL directly at the current 5k-50k release envelope. The
  guard is NOT globally disabled: compute-on-read stays banned for every other
  path in `backend/internal/**`, and the exemption is removed from a screen the
  moment it earns its own projection under that ADR's scale-out ladder.
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
    Vaccination operator availability is explicitly in this class: a sweep or
    preflight may probe many dates, but it must share one session cache at
    `(tenant, park, business_date, cap_per_operator)` grain across capacity
    scoring, `ConductedBy`, effective-cap, and assignment-split helpers. Do not
    call `AvailableVaccinationOperatorsForDrive` from those helpers independently
    or inside park/date loops.
  - **OFFSET pagination** — `LIMIT/OFFSET` with a growable offset. Use keyset/cursor.
  - **non-SARGable predicate** — `lower(col) LIKE '%x%'` / function on an indexed
    column. Use a normalized column, expression index, or `pg_trgm` GIN.
  - **column-side type cast in a predicate** — `indexed_uuid::text =
    ANY($1::text[])` can disable the ordinary index on the stored column. Keep
    the column bare and cast the typed bind array (`indexed_uuid =
    ANY($1::uuid[])`). Add a natural planner proof at a realistic row count;
    forcing `enable_seqscan=off` is not sufficient.
  - **polling full scan / unbounded worker tick** — copy the keyset-chunked
    `FOR UPDATE SKIP LOCKED` claim used by the obligation/idempotency sweepers.
  - **non-terminating pagination loop** — a read-page loop with no cursor advance.
    Guarantee forward progress (monotonic cursor or exclude processed rows).
  If a case is genuinely bounded, annotate it `// scale-guard:ignore: <reason>`;
  do not disable the guard. A green latency gate today means "correct shape", not
  "1M-proven" (gates run at ~1k rows — see the ADR's runtime-gap section).
  - **guard false-green across configuration blocks** — a static Terraform/HCL
    guard must parse/bound the resource and require related `name`/`value` (or
    equivalent) fields in the same block. Every guard self-test must include an
    adversarial sibling-block fixture, and `ci-local` must run both the
    self-test and the real check.
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
- Cross-surface count parity (Claude AND Codex): the SAME business fact must show
  the SAME number on every surface that renders it — admin-web, the mobile app,
  and the API. If two surfaces disagree (e.g. a drive shows 200 doses on the web
  operator schedule but 400 on the mobile calendar), one backend read model is
  wrong even if each query is internally consistent — the frontend/mobile is
  usually faithfully rendering a wrong backend number, so "web fine, mobile
  broken" is really "two backend read models of the same fact disagree". Pick ONE
  authoritative source+grain per business count and reuse it across surfaces
  (for "animals in a drive/day" that is `count(DISTINCT target_id)` over the real
  `vaccination_drive_assignments`, the grain the operator schedule uses). NEVER
  render an estimate/rollup column (`estimated_targets`, `estimated_*`,
  `*_quantity`, cached counters) as a user-facing count while a sibling surface
  reads the actuals. When you add or change a count shown on more than one
  surface, prove parity in the same change next to the `projection-review:`
  marker and add a test asserting the surfaces resolve to the same source/grain.
  Full rule + the 200-vs-400 incident:
  `docs/decisions/scale-anti-patterns.md` -> "Cross-surface count parity".
- Mobile Vaccination Overview current-drive count lock (maintainer decision
  2026-07-29): the mobile Vaccination Overview top summary is NOT a CEO
  adherence KPI, NOT an obligation-history rollup, NOT a shift/carry-forward
  number, and NOT a dose-administration count. It must answer only: for the
  current visible vaccination drive, how many distinct animals are in the drive,
  how many have been vaccinated/submitted, and how many are left. The denominator
  must be the real current drive animal membership
  (`count(DISTINCT target_id)` over the drive assignment/membership grain; for
  the current STG ET+TT example this is 324 animals, never 1296). Do not join
  protocol dimensions/rule rows/dose rows in a way that fans out animals. Do not
  include completed history from older drive dates unless those animals are part
  of the current visible drive membership. Any mobile change touching this card
  must include a regression test for a multi-dimension ET+TT rule where the
  display remains 324 total animals and shows vaccinated vs left from the same
  drive grain.
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
  (45d ⇒ 46d), calendar/day-based projectors must align default windows to
  business-day boundaries and cover the UI's supported week/month query windows
  (for example Monday-start weeks and previous/current/next first-to-last-day
  month picker requests)
  rather than `now±N` clock instants, and fixed-date tests seed the window
  around their fixed dates; (3) stale last-known-good rows serve with freshness metadata while
  no first projection or an uncovered date/window fails closed; (4) a rebuild
  keeps serving last-known-good and a failed rebuild never clobbers it; (5) reads
  served entirely from a bounded canonical index (completed/accepted history)
  are NOT gated on the hot projection; (6) a prune of
  non-serving versions re-derives `serving_projection_version` inside the DELETE,
  never a version captured before the txn/advisory-lock released.
