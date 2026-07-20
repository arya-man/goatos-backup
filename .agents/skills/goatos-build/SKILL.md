---
name: goatos-build
description: Build, review, or modify Goat OS across backend, forms, mobile, dashboards, analytics, infra, contracts, and AI-agent context. Use when working on Goat OS product code, contracts, architecture, migration, or execution planning.
version: 0.1.0
user-invocable: true
argument-hint: "[area: backend|forms|mobile|dashboard|analytics|infra|contracts|migration|review]"
---

# Goat OS Build Skill

Use this skill for Goat OS engineering work. This file is the single entry point
for references. Do not route from memory alone.

## Required First Step — 4-Layer Lookup

Work layers in order. Stop when the question is answered. Never jump to files first.

**Layer 1 — CRG** (code structure: callers, imports, blast radius)
```
repo_root: <absolute path of your goatos checkout>   # git rev-parse --show-toplevel
Cold/review/diff task      -> get_minimal_context_tool, then one targeted graph query
Known-symbol traversal     -> query_graph_tool callers_of/callees_of/imports_of/tests_for
Keyword/domain lookup      -> semantic_search_nodes_tool, then query_graph_tool
Changing code              -> detect_changes_tool + get_impact_radius_tool
Single file/function read  -> read the file; use graph only if impact is unclear
```

**Layer 2 — Graphify** (business context + technical docs). Query both in parallel:

**mesha_docs_graph** — wiki SOPs, farm workflows, vaccination protocols, org model (maintainer-local only):
```
MCP: mesha_docs_graph or mesha_visual_graph
CLI: uvx --from 'graphifyy[mcp]==0.8.44' graphify query "QUESTION" \
       --graph /Users/ravi/mesha/graphify-out/graph.json
```

**goatos-docs graph** — TRDs, ADRs, protocol engine, obligation engine, frontend scope, skill refs (locally generated; run `make ai-rebuild-docs` if missing):
```
CLI: uvx --from 'graphifyy[mcp]==0.8.44' graphify query "QUESTION" \
       --graph ./graphify-out/graph.json
```
Skip mesha_docs_graph if not configured. If the local goatos-docs graph is
missing, run `make ai-rebuild-docs` or fall back to the relevant source docs.

**Layer 3 — Skill references** (architecture decisions, TRDs, contracts)
Load `context/README.md`, then only the ONE reference doc from the table below
that CRG + Graphify point toward. Do not load all references blindly.

**Layer 4 — Grep/Read** (CRG blind spots only)
HTTP route strings, middleware wiring, config values, SQL strings, uncommitted code,
any `callers_of = 0` that seems wrong.

## Current Active Build Path

For protocol/config-driven operations work, the active implementation path is
**Protocol Engine Phase 0**. Use these as the source of truth for Preventive Care (PC)
vaccination, feed direction, protocol rules, obligations, inventory ledger,
Action Center, Protocol Adherence, and Phase 0 implementation:

```text
docs/protocol-engine/PHASE-0-CHECKLIST.md
docs/protocol-engine/IMPLEMENTATION-PLAN.md
docs/protocol-engine/obligation-engine.md
docs/protocol-engine/state-machines.md
docs/protocol-engine/high-scale-kernel-validation-plan.md
docs/protocol-engine/migration-and-cutover.md
context/architecture/operational-kernel.md
docs/preventive-care-vaccination/TRD.md
docs/feed-direction/TRD.md
docs/decisions/calendar-ownership.md
context/execution/calendar-vaccination-slice-parallel-handoff.md
context/execution/admin-web-e2e-checklist.md
mock/goatos-dashboard-mock.html
```

Use older `docs/phases/*` docs for existing built repo patterns and historical
status only. They do not override the protocol-engine Phase 0 contract above.

Permanent scale and guard-authoring rules:

- Keep indexed predicate columns bare. Never cast an indexed UUID/text column in
  a predicate (for example `id::text = ANY($1::text[])`); bind a typed array on
  the parameter side and prove the natural planner choice on a realistic row
  count.
- Configuration guards must parse/bound the resource and validate related
  fields inside the same block. Every guard needs an adversarial sibling-block
  self-test, and both its self-test and real check belong in `ci-local`.
- Vaccination seed/config changes must keep the committed fixture contract in
  sync. Matrix schedule rows require `route_site=subcutaneous` as protocol
  metadata only; do not add route/site back to vaccination SOP/operator forms.
- A green unit test or checker is not recurrence protection until the failing
  fixture is run by the local/hosted CI entrypoint and the shared anti-pattern
  is recorded in `AGENTS.md` and the relevant reference doc.
- `requiredInCI` means a guard runs from a standard `make ci-local` component
  job. A guard reachable only through the legacy `JOB=guardrails` compatibility
  helper is unwired and must fail the registration meta-guard.
- Shed shifting is profile-driven. Resolve the active destination
  `shed_profiles -> animal_stage_lookup` row and lock its version; never infer a
  destination stage from resident goats. Verified completion atomically changes
  shed + operational stage and hands both events to Vaccination's rescope and
  recheck consumers. Require one production-path E2E for that whole chain.

Operational kernel golden rule: every feature must plug into the shared
trigger -> obligation -> sweeper/reminder -> notification/escalation -> proof ->
verification -> read-model chain from
`context/architecture/operational-kernel.md`. The end goal is always process
integrity: was the expected process followed, where broken, who owns next action,
what evidence proves it, and what alert/escalation fired when a deadline crossed.
For any backend/frontend/mobile CRUD, import, mobile offline write, domain event,
or future module such as shifting, dead birth, or feed direction, also load
`context/architecture/domain-event-integration-contract.md` and keep
`context/architecture/domain-event-registry.json` plus
`make domain-event-architecture-guard` green.

Calendar is currently reopened only as the Preventive Care (PC) Vaccination due-work command lens
at `/calendar`. It must follow the Calendar ownership ADR and vaccination slice
handoff: generic `CalendarEvent` contract, vaccination-specific detail blocks,
bounded date windows, protected route registration, durable nudge/snooze writes,
and seeded Postgres E2E proof. Do not treat the full mock Calendar taxonomy as
built product.

## Before you code: operational-invariant discipline

Before touching code, identify WHICH operational invariant(s) the change touches:

- **pagination forward-progress:** cursor is monotonic, next-page fetch cannot regress
- **effective-state on partial updates:** a partial edit re-validates the WHOLE effective
  locked record (not just the changed field), so date/status/rule/version mutations don't
  create impossible states
- **retry-attempt accounting:** claiming/leasing work is NOT an attempt; attempts charged
  only when delivery is actually attempted; cancellation releases untouched work without
  consuming a retry
- **durable recorders:** notifications, reminders, escalations, manual-review queues are
  durable rows, never logs or in-memory state; fail-closed on write failure
- **worker boundedness & resumability:** every sweeper/worker is keyset-chunked with
  forward progress, `FOR UPDATE SKIP LOCKED` claim, lease/cursor, and idempotency —
  never restart permanently at offset zero
- **migration lock-safety:** hot-table indexes are CONCURRENT in separate migrations,
  DDL phases (add-nullable → backfill → constrain) are separated, resumable/idempotent
- **India business-date correctness:** date-only values in business rules (due/missed/
  recovery windows, eligibility checks) use India/local operational timezone, never UTC

Every bug fix REQUIRES a failing-before regression test added to an existing suite. Run
the test BEFORE the fix to confirm it fails; after the fix it passes. A fix with no
failing-before test is unproven.

Before pushing to `main`, a FULL `make ci-local` must pass on the exact commit being
pushed. Partial `JOB=...` runs are fine while developing; only a full local CI gates
main push. GitHub Actions availability is irrelevant to this gate; use
`make land-main` for the fetch/rebase/full-local-CI/race-check/push sequence.

## Reference Guide

| Reference | Load when |
| --- | --- |
| `references/repo-structure.md` | Creating/scaffolding `goatos/`, moving docs, setting up `.claude`, `.agents`, `AGENTS.md`, `CLAUDE.md`, or deciding where files live |
| `references/architecture.md` | Reviewing or changing contexts, modules, boundaries, ports/adapters, backend shape, or AI authority rules |
| `references/backend-impl.md` | Extending/reviewing built Go backend code, current API wiring, identity passport/identifier surfaces, protocol/obligation/vaccination modules, backend package patterns, request middleware, tenant-scoped queries, sqlc follow-up, observability and logging, panic recovery, or log sinks |
| `references/forms-sop.md` | Working on SOP forms, form DSL, builder/editor, native runner, task engine, verification flow, or Slack-form replacement |
| `references/contracts-events.md` | Adding/changing OpenAPI, JSON Schema, protobuf, event envelope, decision records, generated clients, or contract drift checks |
| `references/frontend-mobile.md` | Reusing existing dashboard/mobile UI, changing admin-web/operator-mobile, role-aware dashboard, RBAC UI visibility, or app data adapters |
| `references/analytics-infra.md` | Analytics, BI, AI analyst, BigQuery, Tinybird, Cube, dbt, Metabase, telemetry, cost guardrails, or dashboard metric source |
| `references/execution-plan.md` | Splitting work across two developers/agents, Protocol Engine Phase 0, delivery phases, dev/stg/prod setup, load testing, SLOs, or migration spike |
| `references/phase-prd-trd.md` | Starting or reviewing a phase PRD/TRD, Protocol Engine Phase 0, checking phase scope, or ensuring phase docs update agent references before code |
| `references/source-findings.md` | Using facts from General/Slack docs, customer promise safety findings, legacy source docs, or checking whether source facts reached canonical docs |
| `references/existing-repos.md` | Inspecting or migrating from `dashboard`, `vgoats-dashboard`, `procurement_app`, `slack-automation-scripts`, or `website` reference repos |
| `references/security-ops.md` | Dashboard gating, Slack token rotation, secrets, IAM tiers, Google Cloud context, Cloud SQL data pulls, prod read-only agent access, or auth/RBAC concerns |

## Reference Doc Convention

Use one skill with many references. The committed source skill lives here:

```text
goatos/.agents/skills/goatos-build/
  SKILL.md
  references/<topic>.md
```

Claude discovers the same skill through a symlink or generated copy:

```text
goatos/.claude/skills/goatos-build -> ../../.agents/skills/goatos-build
```

Do not hand-maintain two skill copies.

Do not create separate skills like `goatos-analytics`, `goatos-forms`,
`goatos-mobile` unless the trigger surface becomes truly independent. Goat OS is
one product; this skill is the navigation layer.

## Must

- Read wide, write narrow.
- **Promote staging only through GitHub's PR merge.** Never push any local ref,
  `HEAD`, `main`, local `stg`, agent branch, or refspec directly to remote
  `stg`. Open the same-repository `vgoats/goatos main -> stg` PR, wait for
  `stg-pr-gate`, and merge it in GitHub. Manual workflow dispatch is only a
  rerun of the exact current `stg` SHA already produced by such a merge. Never
  use `--no-verify` to bypass the installed pre-push guard.
- **Land main through `make land-main` (Codex and Claude).** Do not issue a
  direct `git push` / `git mesha-push` to `main`, and do not run CI before
  refreshing main during a landing. The target requires a clean worktree,
  fetches and rebases onto fresh `origin/main`, runs complete affected-component
  `make ci-local`, refetches main, retries rebase + CI if main moved, then pushes
  and verifies the exact green SHA. Use a clean isolated worktree when the
  development checkout is dirty or shared; never auto-rebase unrelated local
  changes merely because an agent session started.
- For any consolidated-ledger fix, read
  `context/repo-audits/last-35-commits-consolidated-bug-ledger.md` and obey
  `context/repo-audits/consolidated-ledger-defect-closure-program.md`. Work one
  root batch at a time; do not claim closure without its current-SHA proof
  packet, required CI gates, and independent counter-review.
- Lock to the user-approved slice. Shared/generic infrastructure may be built
  only to serve that slice, and visible UI/API handoffs must not present future
  verticals as live product.
- **No fake business truth (always on, all layers).** Never invent ownership,
  assignments, mappings, counts, statuses, owners, or any business fact in
  runtime code, seeds, importers, migrations, or generated data. A missing source
  of truth becomes exactly one of: (a) a reviewed mapping, (b) a clearly-labeled
  provisional seed fixture that a preflight can reject, or (c) an explicit
  blocker. Round-robin / even-spread / "pick a plausible default" are acceptable
  ONLY as a one-time data-fill strategy that emits a reviewable artifact tagged
  provisional + needs_review — never inside an apply/runtime path. Preflights and
  imports must FAIL (non-zero) until every required row has an explicit, reviewed
  source. Reference pattern: `backend/cmd/seed-shed-positions` (`-generate-provisional`
  writes the artifact; `-mapping` applies it; `-strict` blocks gaps AND unreviewed
  provisional rows).
- **Vaccination source seed is ownership-dependent.** For local/dev/staging
  vaccination rehearsals, run `make seed-vaccination-source-full`; do not treat
  `backend/cmd/seed-vaccination-real` as a standalone whole-setup command. The
  vaccination seed must fail if HRMS roster/position prerequisites are missing,
  because creating due work before owners/operators exist produces broken
  Action Center owner/operator assignment states. A source reseed is not green
  until vaccination generation and the drive-batching sweeper have also run
  through the visible schedule horizon and the closeout check proves zero
  in-window `scheduled`/`due` vaccination obligations remain unbatched.
- **Any new vaccination/HRMS sheet is validated before DB access.** Normalize it
  to the canonical six-file bundle and run `make vaccination-hrms-source-audit
  SOURCE=/absolute/path AS_OF=YYYY-MM-DD`. Read every failure category, apply
  only reviewed deterministic fixture repairs, sanitize PII, regenerate the
  committed fixture/manifest/correction ledger, and run
  `make vaccination-hrms-seed-fixture-guard`. Never invoke an individual seed
  binary to bypass a red preflight. A new source column, migration, config/SOP
  rule, owner role, or importer branch must update the source audit, strict
  validator, adversarial self-test, transform, fixture hashes,
  `docs/runbooks/source-seed-data-validation.md`, source-date contract, and
  anti-pattern docs in the same patch; the coupling guard must fail otherwise.
- Use `context/` as architecture truth.
- Use generated contracts instead of hand-copying DTOs.
- Before coding a phase, read its PRD/TRD and update skill references if the
  phase adds a permanent rule/module/tool/workflow.
- After coding a phase, run the PRD/TRD/context/skill closeout sync so docs
  describe what was actually built.
- When a migration changes an initial-seed-owned setup table or app-visible
  projection/read-model table, update the seed command, seed/projection test, or
  seed runbook in the same change. `make seed-migration-guard` enforces this
  coupling; see `docs/runbooks/initial-seed-migration-coupling.md`.
- New setup tables must declare their class: source/canonical, derived/read
  model, static catalog/config, or operational/audit/event. Derived app-visible
  tables are filled by deterministic projectors registered in
  `tools/dev/seed-closeout.sh`, not by hand-written seed rows.
- Projection closeout is output-specific. If a projector command has more than
  one visible output, call it with explicit flags for each read model. A
  default-false `-project-*` flag must be present as `-project-...=true` in
  `tools/dev/seed-closeout.sh` on the owning command invocation. Guard evidence
  must come from `tools/dev/seed-closeout.sh --dry-run`, not raw shell source; a
  generic command, commented example, disabled branch, or uncalled helper is not
  enough.
- Projection-backed operator pages must follow the last-known-good serving
  contract in `docs/decisions/high-scale-dashboard-projections.md`: no first
  projection/no serving rows or uncovered date/window may fail closed, but stale
  yellow/rebuilding/failed/over-TTL projections with serving rows covering the
  request must return those rows plus freshness metadata instead of taking the
  page down.
- Source-backed vaccination seed means the whole executable setup: founder
  grants, HRMS roster, attendance/leave, timetable-backed positions, strict
  shed-manager/backup mapping, position duties, published
  `vaccination.matrix` config, trusted vaccination history, generated future
  obligations, generated drive batches, and deterministic closeout. A goats-only
  seed, or a generation-only seed that has not run the sweeper, is not a usable
  Goat OS seed.
- After destructive seeds, bulk imports, fixture resets, or large canonical
  backfills, refresh Postgres planner statistics for touched canonical tables
  before running read-model projectors or latency gates. The source seed must
  `ANALYZE` freshly loaded location, HRMS, goat, protocol, obligation, event,
  and vaccination-completion tables after commit and before projection closeout.
- Local/staging/prod app code must not start against a database behind that
  build's migrations. The setup order is always: migrate schema, seed only
  canonical source truth, run deterministic closeout/projectors, then start or
  certify API/admin/workers.
- Normal local laptop runtime must use one Goat OS app database for API,
  admin-web, and mobile. Resolve the single `goatos-local-current` Docker DB or
  the `127.0.0.1:5433/goatos` fallback; if multiple Goat OS app Postgres
  containers are visible, fail instead of guessing. E2E/proof/load scripts must
  fail closed unless `GOATOS_E2E_DATABASE_URL` or `DATABASE_URL` is explicitly
  passed. Read-only E2E checks may target the normal `5433` app DB, but
  mutating proof/load scripts that create goats/proofs, replay outbox, insert
  history, or run migrations must always refuse `5433`. There is no override
  for mutating the normal app DB from E2E. Destructive/load tests must use an
  isolated DB with its own seed/cleanup, such as the explicit local GCP-kernel
  stack on `55432`; that stack must not become the default local runtime DB.
- Staging deployment authority is Cloud Deploy. GitHub Actions, Cloud Build, or
  local operators may build images and create releases, but `goatos-stg` Cloud
  Run services/jobs must be updated by `tools/deploy/stg-clouddeploy-task.sh`
  through `deploy/clouddeploy/stg/clouddeploy.yaml`. Direct Cloud Run mutation
  is break-glass only; read `docs/runbooks/cloud-deploy-staging.md`.
- Keep AI as proposer/triage, never canonical authority.
- Preserve module boundaries in the Go modular monolith.
- Keep frontend/mobile behind app APIs and generated clients.
- Publish every feature/fix/audit/scale E2E result into the GitHub Pages CI
  reports site before handoff. It must be a visible card/list item on the root
  report index (`https://vgoats.github.io/goatos/`) and a styled report page
  matching the existing E2E report shell (summary tiles, badges, sections, code
  blocks). If the E2E belongs to an existing category, update that category's
  report instead of creating a new root card; create a new card only for a
  genuinely new report category. A standalone deep link, raw markdown dump,
  attachment, scratchpad, or chat paste is not an accepted E2E report. Each E2E
  report must explain the scenario, setup/data, action/trigger, assertions,
  evidence source, and certification boundary; a one-line test name is not a
  report.
- An E2E label is a certification claim: setup may seed source/input fixtures,
  but the asserted business outcome must flow through the production
  service/API/event/worker path. Never insert or update derived obligations,
  batches, completions, SOP execution, notifications, escalations, Calendar
  rows, or process-integrity rows in an E2E scenario. Put intentionally
  derived-state-seeded coverage in the owning package as an integration or
  read-model test instead. Run
  `tools/agent-hooks/check-e2e-kernel-integrity.sh` before publishing.
- Treat backend-driven UI contracts as a golden rule. Admin-web/operator-mobile
  must render backend-owned OpenAPI/app contracts for navigation, route labels,
  page/section titles, table columns, filter/sort/page-size semantics, chips,
  tabs, row-click params, drawer/action copy, disabled reasons, and
  summary/detail field sets. Frontend may own only layout, CSS, responsive
  density, icon-token mapping, focus/hover behavior, and local open/closed or
  selected-row state. When a page still has hardcoded product text or control
  semantics, migrate it into the backend contract or record the temporary gap in
  `context/frontend/` before handoff. Stable UI text that needs runtime
  governance is stored in tenant-scoped `admin_ui_config_entries` and compiled
  into `/admin-web/bootstrap`; live/domain values stay in canonical module DB
  tables and are compiled as DB families. UI config entries must not relabel
  live/module-owned option groups or override semantic option metadata such as
  source-system publishability.
- Keep official analytics metrics behind Cube.
- Keep Slack/Sheets/App Script as legacy reference/migration only.
- When source artifacts add lasting facts, sync the relevant `context/` doc and
  this skill's reference map in the same closeout.
- For protocol/config work, keep workflow status, activation state, version
  audit, and source evidence separate. Source docs are engineering evidence for
  preset values, not runtime source/review UI fields. Vaccination config uses a
  scoped active ruleset model: one active company `vaccination.matrix` version,
  plus at most one active park override per park.
- For vaccination seed/reseed work, source vaccination dates are trusted history
  anchors, not open due work. Preserve past source dates as accepted history,
  suppress seed-created open work on or before the backend business date, and
  let the vaccination kernel schedule only future obligations from that base.
  Do not copy or hand-roll kid/adult path logic in seeders; call the live
  vaccination schedule-path helper/config. Checklist:
  `docs/runbooks/vaccination-seed-source-date-contract.md`.
- Local vaccination proof after a seed/reseed/import/change of goat shed, goat
  health state, goat lifecycle, source history, protocol rules, or HRMS
  ownership must run the same closeout chain: generation, sweeper drive
  batching, calendar/full-schedule canonical reads, and the zero-unbatched
  check. Do not inspect Calendar/Full Schedule between generation and sweeper
  and call the micro-drive output meaningful; that is a partial environment.
- Vaccination seed/import must leave no accepted live goat without a real active
  shed. Missing source placement is completed deterministically into the
  seed-intake shed during this build phase; do not skip the animal and do not
  invent vaccination history. Goat vaccination obligations are shed-scoped only.
  Run `make goat-shed-scope-guard`; seed closeout runs the DB proof.
- For Preventive Care (PC) vaccination, never ask about, model, seed, import,
  expose, or schedule from mother-not-vaccinated / unknown-mother status. The
  source/wiki branch is ignored in GoatOS; mothers are kept vaccinated
  operationally and every kid uses the approved standard schedule.
- For Preventive Care (PC) vaccination drive planning, individual due dates are
  per-animal obligation truth, NOT drive boundaries. The sweeper/drive planner
  must maximize compatible distinct animals per park visit inside the authored
  medical window and one-time batching hold (`max_batching_hold_days`,
  default 7; `max_batching_hold_count`, default 1). Shed count is never a
  batching constraint; it is display/proof detail. Exact-due-date grouping that
  creates 1-2 animal micro-drives while nearby compatible animals are still
  inside the same animal group's due/ready-to-safe-until window is a core
  algorithm bug. Obligation/vaccine row count is only a tie-breaker after
  distinct animal count. Micro-drives are valid only when no compatible
  same-park work can be safely clubbed before the selected animals' binding
  safe date. Normal per-drive animal caps are soft on the last safe day, but the
  per-animal shot cap remains hard. Wide sweeps/backfills must pass separate
  `asOf` and
  `dueBefore` values: `asOf` drives hold/backdating/planned-date math, while
  `dueBefore` only selects eligible obligations. Batched execution/read-model
  rows must display/sort/status by `obligation_batches.planned_date`, falling
  back to animal `due_at` only for unbatched work. Guard:
  `make vaccination-drive-clubbing-guard`. Post-reseed/local proof also requires
  `make vaccination-drive-clubbing-db-proof` after the sweeper.

## Must Not

- Do not duplicate architecture facts inside `AGENTS.md`, `CLAUDE.md`, or skill refs.
- Do not add direct BigQuery queries to React pages.
- Do not add direct Firestore/GCS writes to the operator app.
- Do not let AI-created decisions become canonical without deterministic validation and evidence.
- Do not copy old repos blindly into `goatos/`; dashboard frontends are the
  intentional exception and should be snapshot/cloned into `goatos/apps/` for
  safe rewiring while live repos stay untouched.
- Do not treat the public website as Goat OS core.
- Do not use demo/sandbox as protocol rule concepts. Draft or inactive versions
  generate no new obligations. Activation requires the category/scope policy,
  CEO/COO/superadmin authority, JSON-schema validation, SOP binding where
  needed, impact preview, and non-overlapping active windows.
- Do not start or hand off local API/admin-web from `/tmp`, `/private/tmp`, or
  `/var/folders` worktrees unless the command explicitly sets
  `GOATOS_ALLOW_TEMP_WORKTREE_LOCAL_STACK=1` for a throwaway experiment. The
  visible local stack must serve a non-temporary checkout that can be reconciled
  to the pushed branch/main, otherwise fixes can look landed while the browser is
  exercising a disposable tree.
