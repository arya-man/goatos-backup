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

RBAC vocabulary rule: do not introduce a separate admin person/business role.
CEO/CXO full access is represented by the `ceo_internal` grant role and `cxo`
workforce hint. Product route/package names such as `/admin/*` or `admin-web`
are not grant roles.

CPT operator-drive rehearsal seed packet: when seeding the supplied CPT source,
use `fixtures/vaccination-cpt-operator-drive-2026-07-23/` and business date
`2026-07-23`. The packet is CPT/Channapatna only; do not synthesize CBE/
Coimbatore. The only field operators are Amit Kumar, Darshan Talwar, and Sagar
Mahoor, all equal vaccination operators at `200` unique animals/day/operator.
Chandrakant is director-only monitoring scope. The five founder/CXO emails in
`docs/runbooks/auth.md` must receive tenant-scoped `ceo_internal` grants. The
same roster may include verifier-only Firebase email-password grants; Jyothi
`jyothipvg12345@gmail.com` is a verifier for CPT seed review and must not gain a
vaccination operator seat or animal capacity.
`Adult` filename is only source naming; kid/adult/booster/clinical/combo/buffer
rules still come from the backend vaccination rule engine. Adult vaccination
generation must not use `entry_date` / `post_arrival` as a due-date anchor:
accepted same-vaccine history drives adult booster/repeat timing, and adult
blank-history animals automatically join the normal adult drive for that vaccine;
they do not require a separate manual-campaign trigger or approval. Repeat versus
initial/catch-up is an animal-level dose instruction inside one logical drive, not
a reason to split the roster into separate drives. When repeat-history readiness
dates differ but their safe windows overlap, use the latest readiness date inside
the shared window for both repeat-history and blank-history obligations; do not
create an earlier partial drive. If repeat history arrives after a stable
blank-history obligation was already generated, reschedule that same open row
onto the shared cohort date; never leave the old split date or mint a duplicate.
Pack work by whole physical
shed; partition is only a fallback when the physical shed itself exceeds the full
per-operator cap. Kid/young DOB and age-window timing remains strict.
Run it with `make seed-vaccination-cpt-operator-drive` — that target materializes
the packet's documented `raw/` layout into the normalized bundle both seed
commands require and then runs the documented chain against it, so the documented
command and the executable shape agree. Do not hand-run the individual binaries.
The materialized CPT bundle must include reviewed shed-manager rows for every
active Channapatna physical shed. Darshan Talwar is the manager/default drive
owner and Sagar Mahoor is the reviewed backup; `seed-shed-positions` must run
even when `cpt-operator-roster.json` is present. A completed accepted-history
shed, such as Gandhi ET+TT, still resolves to Darshan in shed summaries rather
than showing `Operators unassigned`.
The director and the CEO/CXO grants come from the committed
`cpt-operator-roster.json` (`directors[]`, `leadership_full_access`), which
`backend/cmd/seed-roster-real` now genuinely consumes; a director is seeded with
no `workforce_positions` row, so he carries zero field execution capacity, and
that is asserted rather than assumed.
After that DB seed, provision Android login for the three field operators from
the same roster: Amit, Darshan, and Sagar each need their own Firebase/Auth
email-password identity using their `operators[].email_hint`. Never use one
common operator account, one shared password, or a CEO/CXO account for field
Android execution; use unique temporary passwords or individual reset flows, do
not commit plaintext passwords, and smoke-test Android bootstrap/login for each
operator before calling the seed complete.

Git identity rule: Goat OS commits must use a Mesha identity. Before committing
or landing, verify `git config user.email` ends in `@mesha.sg`; never commit or
push with Heva, Slice, gmail, or personal identities. `make git-identity-guard`
and `make ci-local` enforce this.

Leadership assistant coverage rule: every current or future
leadership-relevant backend module, migration, OpenAPI contract, admin-web
route, mobile workflow, reporting table, or domain event must update the
leadership assistant read path in the same change. Update a Mesha read API
mapping, MCP Toolbox tool, `ceo_ai.*` reporting view, assistant context/doc, or
document an explicit exclusion. `make leadership-assistant-coverage-guard`
enforces this in local CI.

Operational read model rule: shared command surfaces and mobile/admin/reporting
reads must follow `docs/architecture/operational-read-model-contract.md`.
**Partition display rule (MANDATORY):** when a partition exists (`Castro 1`
alongside `Castro 2`), every surface must render the partition label, group by
`shed_id` + park, never collapse unless explicitly the aggregate. Full convention,
worked wrong-examples, schema requirements, and guards: `docs/decisions/operational-location-convention.md`.
Whenever developing or debugging Calendar, Control Tower, Action Center,
Protocol Adherence, Workflows, admin-web detail pages, Android execution/proof
screens, OpenAPI/generated clients, or a new vertical/module, first identify the
canonical write owner, row/summary grain, bucket disjointness, stable scope
identity, whole-result summary behavior, and every consuming surface. Run
`make operational-read-model-contract-guard` + `make operational-location-guard`.

Critical animal actions (quarantine, ICU, death, contagious disease isolation,
high-risk movement, and sale/allocation blockers) must follow `docs/features/critical-animal-action-guardrails.md`.
Run `make critical-animal-action-availability-guard` when touching those paths.

CEO AI reporting views: backend/migrations/postgres/000024-000027 introduce
`ceo_ai.*` reporting views (vaccination_shed_status, vaccination_dose_pickup,
action_center, vaccination_operator_status) that read canonical vaccination/
procurement/obligation/workforce tables but do NOT modify the vaccination seed/
config/SOP schema. These migrations support the leadership assistant's
operational read path and are not part of the vaccination protocol contract.

Defect-prevention and kernel non-deviation rule: every bug fix, audit batch,
migration, feature, and operational module must load and follow
`context/execution/defect-prevention-execution-contract.md`. Coordinators for
the whole-ledger/kernel program also resume from
`context/execution/operational-kernel-program-state.md`. Operational work is
one event-driven, interlinked task/ticketing waterfall: canonical event and
transaction, real owner and pinned clock, bounded hierarchy,
acknowledgement-gated contacts, proof, separate verification/sign-off task,
close/reopen rollup, and shared reads. Module domain facts stay local; private
task authorities, schedulers, owner fallbacks, overdue logic, escalation
ladders, verification queues, and screen-only follow-up pipelines are banned.

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
docs/architecture/operational-read-model-contract.md
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
- ET+TT adult booster is mandatory schedule work: adult ET+TT dose 2 is due 21
  days after adult ET+TT dose 1. Do not treat `ET+TT Booster` as kid-only,
  optional, or as the 182-day repeat; the repeat starts only after ET+TT dose 2
  / course completion.
  Post-seed guard: accepted `et_tt_adult_w1` without same-goat
  `et_tt_adult_w2` obligation/completion is a broken DB and must block handoff.
- Vaccination drive capacity is per available operator per business date and
  counts unique animals, not doses or vaccine obligation rows. Persist generated
  operator/shed/partition assignments set-wise; if the safe buffer would be
  breached, mark the drive over-cap required and finish instead of silently
  pushing animals beyond the latest-safe date.
- A physical shed at or below one operator's full configured cap is indivisible,
  even when it does not fit the current day's residual slots. Carry the complete
  shed to the next operator-day; do not peel off partitions to fill a remainder.
  Enforce this in park pre-batching as well as operator planning and group
  compatible blank-history catch-up and history-backed repeat rule rows by the
  same physical shed before applying the cap. For CPT, preserve the canonical
  route `Gandhi`, `Godel 1`, `Godel 2`, `Mandela 2`, `Old Yashoda`, yielding
  `193 + 131 = 324` with one 200-animal operator for the full adult cohort.
- Operator submission time is the medical `administered_at` anchor. Delayed
  verifier/director approval may set `verified_at`/`closed_at`, but must never
  replace the administration date used for booster and repeat scheduling.
- Batch readiness and closure count only active `recorded`/`accepted`
  vaccination completions. Retained `rejected`/`reversed` attempts are audit
  history and must not block a later successful retry.
- STG operator grants are park-scoped, never tenant-scoped. Leadership/director
  visibility may get tenant scope, but field execution accounts (`operator`,
  weighing operators) must declare a park and materialize `user_scope_grants`
  with `scope_type='park'`. Run `make stg-operator-scope-guard` for any STG
  login, Firebase, workforce, or operator grant change.
- Operator drive assignments are generated metadata, not obligation membership.
  Review SQL joins at exact assignment grain so multiple operators, planned
  dates, or partitions cannot multiply counts or expose another operator's
  partition work.
- `vaccination_drive_assignments` is the canonical operator-day source for
  scheduled drive execution once rows exist. Full Schedule, Calendar L1/L2/L3,
  Action Center, Protocol Adherence, Control Tower, Workflows, execution pages,
  and leadership/operator reads must prefer assignment `planned_date` before
  batch `planned_date` or obligation `due_at`. A date move or vaccine override is
  not complete until every command lens reads the same assignment/effective-date
  grain and local CI's `vaccination-schedule-canonical-guard` would fail if any
  consumer falls back to stale dates.
- Shed partition labels are not canonical sheds. `Gandhi 1`, `Gandhi 2`, and
  `Godel 1 - Part 3` must normalize to physical-shed owner/count rows plus
  partition metadata. CPT-only rehearsal seeds must not pull CBE/Coimbatore
  into HRMS, parks, APIs, or UI just because the full fixture has both centers.
  Strict source-backed shed ownership applies to active sheds that contain live
  source goats; empty baseline catalog sheds are not vaccination drive truth.
  In CPT operator-drive rehearsal seeds, Amit, Darshan, and Sagar are all
  vaccination operators; do not treat Amit as park-head-only or
  Sagar as backup/support-only for drive assignment. Preserve the source
  week-offs: Amit Friday, Darshan Sunday, Sagar Saturday.
- Vaccination operator availability/capacity is a sweep-session fact. Load it
  once per `(tenant, park, business_date, cap_per_operator)` and pass/cache it
  through date scoring, `ConductedBy` selection, effective cap calculation, and
  assignment splitting. Never call `AvailableVaccinationOperatorsForDrive` from
  multiple helpers or from park/date loops; that is the same N+1 fan-out bug
  wearing a different shirt.
- The local vaccination trigger fixture has one reviewed synthetic primary RFID
  (`CBE-RFID-0001`) for emulator scan E2E. Keep it synthetic and synchronized
  with the source fixture validator/runbooks when changed.
- A green unit test or checker is not recurrence protection until the failing
  fixture is run by the local/hosted CI entrypoint and the shared anti-pattern
  is recorded in `AGENTS.md` and the relevant reference doc.
- `requiredInCI` means a guard must run from a standard `make ci-local`
  component job. The current registration meta-guard proves declarations and
  textual reachability only; semantic recipe execution, affected-job routing,
  file uniqueness/existence, and spoof resistance remain mandatory F0 work in
  `context/repo-audits/current-whole-project-remediation-ledger.md`. Until F0,
  reviewers must verify those properties directly rather than treating the
  meta-guard as complete proof.
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
For vaccination FCM, do not stop at "an alert exists": each rule must name its
trigger, audience source, cadence/SLA, message summary, and tap route. Routine
day-start/afternoon nudges are field-scoped; the 20:30 IST due-today checkpoint
adds PC director/CEO leadership only when scheduled sheds are still not
submitted. Proof submission sends role-specific rows: verifier devices deep-link
to video review, leadership devices deep-link to Vaccination overview. Run
`make fcm-recipient-routing-guard` before landing notification changes.
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

Before editing a bug fix, audit row, migration, new feature, or kernel milestone,
fill the batch record and prevention matrix in
`context/execution/defect-prevention-execution-contract.md`. Name the exact
failing production path, sibling sites, canonical invariant, regression,
persistent safety control, structural guard or stronger-control rationale,
adversarial self-test when a structural guard applies (otherwise the applicable
production-path proof for the stronger control), ordinary local-CI job,
recovery/observability, skill/doc updates, and internal dependency/commit order
in the single integration PR.
For operational features also name the event, task
source identity, owner, clock, parent/work-unit, proof, sign-off leaf, contact
policy, acknowledgement, rollup, shared reads, and reconciliation. A missing
answer is a design gap, not work to defer silently.

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

Every bug fix REQUIRES a failing-before regression test added to an existing
suite. Run the test BEFORE the fix to confirm it fails; after the fix it passes.
A net-new feature proves its acceptance behavior is absent or failing on the
base. A docs/policy-only change uses structural validation and diff proof rather
than inventing a runtime failure. A behavioral fix without failing-before proof
is unproven.

The fix also requires recurrence prevention in the same batch. Prefer a DB,
transaction, type, schema, or production-path control over a weak static grep.
When the rule is mechanically detectable, ship a structural guard with
adversarial fixtures, manifest registration, self-test, Make target, and normal
`run_common` or component-job wiring. Update the closest canonical anti-pattern,
the relevant skill/reference, and operational recovery. If any applicable leg
is absent, report `source-fixed, closure-pending`; do not mark the work done.

Before pushing to `main`, a FULL `make ci-local` must pass on the exact commit
being pushed. Partial `JOB=...` runs are fine while developing; only a full
local CI gates main push. This is the ordinary deterministic CI gate, not a
substitute for applicable PostgreSQL, migration, device, browser, deploy, or
live-state certification lanes. GitHub Actions availability is irrelevant.
Ordinary work and this documentation foundation use `make land-main`; the
approved whole-ledger/task-kernel program uses
`make land-integration-pr PR=<number>` after F0 implements and proves it.

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
- Treat the current public/operator-facing Goat OS path as production-facing
  even though it reuses the existing `goatos-stg` Google/Firebase project
  internally. Public app config, release labels, dashboard URLs, API URLs, and
  operator instructions should use `sg.mesha.goatos`,
  `https://dashboard.mesha.sg`, and `https://api.goatos.mesha.sg/` unless the
  user explicitly asks about historical staging. Firebase Auth issuer/audience
  may still be `goatos-stg` while that existing Firebase project is reused.
- **Promote staging only through manual Cloud Deploy.** Never create or wait for
  a `main -> stg` PR or GitHub Actions deployment. Never push any local ref to
  remote `stg`. Deploy only from a clean, approved `origin/main` SHA through
  `docs/runbooks/stg-deploy.md` and the repo-owned Cloud Deploy helpers. Never
  use `--no-verify` to bypass the installed pre-push guard.
- **Recover STG billing/Cloud Run 429 through the dedicated runbook.** If
  `stg.dashboard.mesha.sg` or `stg-api.dashboard.mesha.sg` returns Google
  Frontend `429 Rate exceeded` after a paid/restored bill, read
  `docs/runbooks/stg-cloud-run-billing-recovery.md` before changing code. Verify
  `billingEnabled: true`, Cloud Run readiness in `asia-south1`, and the
  maintainer baseline `goatos-api-stg min-instances=2 max-instances=2`; finish
  with terminal curls and live Chrome verification.
- **Land ordinary work and this documentation foundation through
  `make land-main` (Codex and Claude).** Do not issue a
  direct `git push` / `git mesha-push` to `main`, and do not run CI before
  refreshing main during a landing. The target requires a clean worktree,
  fetches and rebases onto fresh `origin/main`, runs complete affected-component
  `make ci-local`, refetches main, retries rebase + CI if main moved, then pushes
  and verifies the exact green SHA. Use a clean isolated worktree when the
  development checkout is dirty or shared; never auto-rebase unrelated local
  changes merely because an agent session started.
- **Kernel/remediation program exception.** The approved whole-ledger and
  operational-kernel program uses one external integration PR, not direct-main
  landing. F0 must introduce the repo-owned exact-head program-PR landing gate
  defined in `context/execution/defect-prevention-execution-contract.md` before
  any implementation batch can close or the program PR can merge. Until that
  gate exists, `make land-main` remains the default for unrelated ordinary
  changes and for landing the documentation foundation only.
- For any whole-project audit fix, read
  `context/repo-audits/current-whole-project-remediation-ledger.md` and its
  current closure gate. For work explicitly naming an older last-35 ID, read
  its historical ledger and closure program instead. Work one root batch at a
  time; do not claim closure without its current-SHA proof packet, required CI
  gates, and independent counter-review.
  Before implementation, fetch fresh `origin/main`, re-adjudicate the selected
  IDs and migration tail, and treat the ledger's recorded SHA as evidence
  provenance rather than live status.
- For every bug fix, feature, migration, and kernel milestone, use
  `context/execution/defect-prevention-execution-contract.md` as the mandatory
  batch-entry, recurrence-prevention, PR-topology, and closure checklist. A
  behavior-only fix is not complete.
- For generic task hierarchy, owner/duty clocks, Today/My Tasks, sign-off, or
  escalation work, also obey
  `context/execution/operational-task-kernel-remediation-plan.md`.
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
- **A reseed proof requires a clean, origin/main-identical checkout, and it is
  gated.** `make seed-checkout-staleness-gate` runs read-only as the first step
  of every seed target, before any DB write, and fails closed on an unreachable
  origin, a `HEAD` that differs from `origin/main`, or a dirty tree.
  `GOATOS_ALLOW_STALE_SEED_CHECKOUT=1` bypasses it loudly and voids the proof.
- **A documented DB comparison must be executed, not just described.**
  `tools/dev/seed-closeout.sh` runs the drive packet's
  `check-expected-drive-schedules.mjs` (self-test, then the real row comparison)
  whenever `GOATOS_EXPECTED_DRIVE_SCHEDULES` names a packet expectation file; a
  missing file or a packet without the checker fails the closeout. Without this
  the reseed can report "clean" while breaching a per-operator animal cap,
  fanning out past `active_operators_per_day`, assigning a non-contract
  operator, or presenting superseded/zero-obligation shell batches.
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
  A source-audit range must be identical to the DB `CHECK` and the domain
  validator for the same field, or the preflight does not predict the seed:
  operator `shift_start_minute`/`shift_end_minute` are minutes-of-day `0..1439`
  on both bounds, in all three places.
  Committed fixture/contract loaders must fail loud on unknown keys.
  `backend/cmd/seed-roster-real` decodes `cpt-operator-roster.json` with
  `Decoder.DisallowUnknownFields()`, so a declared block with no consuming struct
  field is a hard named error instead of an `encoding/json` silent drop. Adding a
  block to that contract requires adding its consumer in the same change.
  For vaccination `health_status` source data, keep case-log vocabulary separate
  from clinical state: `Open -> sick`, `Extended -> under_treatment`,
  `Closed -> healthy`, `Fine -> healthy`. Never seed `Closed` or `Fine` as
  `recovering` or as a defer.
  Accepted one-time vaccination completion history is canonical: if generation
  recreates an active obligation for the same goat/rule during seed, the seed
  must supersede the active duplicate and preserve the accepted completion as
  goat history.
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
- Role-catalog and `workforce_members.primary_role_hint` changes are seed-owned.
  The live Growth Director role key is `growth_director` and it is Weighing-only;
  do not spell it `director_growth`, do not merge it with `pc_director`, and do
  not let HRMS roster seed create vaccination capacity from it.
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
- The browser-visible shared local stack is one atomic exact-main appliance:
  clean **exact origin/main** admin-web on `127.0.0.1:3300`, API on
  `127.0.0.1:8080`, and database `goatos` from `goatos-local-current`. Use the
  persistent service wrapper, never ambient `DATABASE_URL`, and verify both
  listener CWDs, `/readyz`, and an authenticated Calendar data-plane read before
  handoff. Shared local API startup pins `GOATOS_PG_QUERY_TIMEOUT=15s`: the
  production-oriented 3-second DB deadline must not turn valid Calendar rows
  into a false empty/error screen while the developer machine is compiling or
  running CI. The supervisor must fast-forward only a clean ancestor checkout,
  re-exec before DB preparation, and restart FE and BE together when either
  fails or `origin/main` advances. Admin-web dev startup must always go through
  `apps/admin-web/scripts/run-local-next.mjs`, including isolated/custom ports
  such as `--port 3318`. Plain `next dev` bypasses the local bearer environment
  and causes `/admin-web/bootstrap` to fail with `invalid_bearer_token`. An
  isolated E2E stack is unrelated: it must have non-shared FE/BE ports and an
  explicit throwaway DB; never stop, sync, seed, migrate, reuse, or delete it
  while fixing the shared stack. Run `make
  local-stack-service-guard`; it is a registered, required standard local-CI
  guard. Canonical operating details:
  `docs/runbooks/local-full-stack-rehearsal.md`.
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
- Vaccination proof grain is SOP-owned and must flow through backend config/API
  into Android. Current source fixture contract is shed-level video proof:
  one-to-five shed videos (camera or gallery) plus per-goat scan timestamps.
  Do not hardcode per-goat video proof in seeders, Android, verifier bridge, or
  assistant copy. Any change to this proof grain must update the migration,
  committed fixture manifest, validator, runbook, Android UI, and verifier path
  in the same patch.
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
- Vaccination drive date override is a write-path/kernel operation, not a
  frontend/read-model illusion. When a vaccine is moved out of a mixed
  operator-cap drive, raw `vaccination_drive_assignments` must be split so only
  the moved vaccine leaves the original date. Selecting the original date is the
  supported revert path: cancel the active override and restore the raw
  assignment membership. Proof must cover move and revert with raw DB assertions
  plus existing clinical-rule outcomes and operator animal caps.
- Vaccination operator animal capacity is HRMS-owned. The live source is
  `workforce_positions.vaccination_daily_animal_cap` per operator position;
  `vaccination_capacity_config.max_per_day` is only a fallback when HRMS has no
  explicit cap. Operator shift configuration (shift_label, shift_start_minute,
  shift_end_minute) seeds into `vaccination_operator_shift_config` via the
  operator-roster overlay; operator assignment config (active_operators_per_day,
  default_operator_code) seeds into `vaccination_operator_assignment_config`
  (scheduler-consumed operator assignment config). HRMS edits,
  operator-roster seed overlays, planner assignment splitting, timetable
  capacity, admin-web display, and leadership reads must all use the same
  per-position cap field. Any change to cap source, shift fields, or assignment
  config must update the migration/seed fixture companions, admin UI, scheduler
  tests, and capacity/date-move E2E in one patch. Clearing
  `vaccination_daily_animal_cap` to null is a valid HRMS edit that restores
  tenant-default capacity; never coerce it to zero or silently leave the
  previous custom cap. Operator-cap SQL proof must be a real Postgres test for
  custom/null-default/week-off rows; a source-string guard is only a lint.
- Date-scoped operator capacity overrides are explicit exceptions, not a new
  normal cap source. CPT operator-drive seed uses one override for Darshan on
  2026-07-25 so ET+TT can schedule 210 after only 114 completions on
  2026-07-24; every other CPT operator/date remains governed by the standing
  HRMS cap of 200 unless a future fixture explicitly declares another
  `seed_catchup_overrides` row.
- Adult non-repeating physical-partition campaign obligations must be
  idempotent at campaign grain across seed replays. A corrected seed as-of may
  realign an unbatched open obligation to the reviewed campaign day, but must
  not create another open obligation for the same goat+dose.

## Must Not

- Do not duplicate architecture facts inside `AGENTS.md`, `CLAUDE.md`, or skill refs.
- Do not add direct BigQuery queries to React pages.
- Do not add direct Firestore/GCS writes to the operator app.
- Do not let AI-created decisions become canonical without deterministic validation and evidence.
  When an agent claims a test passes, the test must have actually executed (not "[no test files]"/"no tests to run"/skipped). Paste the real test RUN line, its pass/fail output, and commit SHA. A reviewer or orchestrator MUST re-run the claimed test independently to verify the agent's result. Any code review claiming a fix is complete must have pasted test evidence on the real production path, not only a unit test in isolation — the production caller, the retry path, the race condition, or the edge case must reproduce the exact failure first, then pass after the fix.
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

<!-- Coupling review 2026-07-29: seed-roster-real adds feed_direction to the preventive_care department module grant. This changes runtime module/navigation authorization only; it does not change HRMS roster rows, vaccination history, source dates, fixture bytes, hashes, or counts. -->
<!-- Coupling review 2026-08-21: seed-roster-real adds pc_care (deworming / ticks removal / hoof trimming / hair trimming) to the preventive_care department module grant; migration 000181_pc_care_module_grants.sql applies the same grant to already-seeded databases. This changes runtime module/navigation authorization only; it does not change HRMS roster rows, vaccination history, source dates, fixture bytes, hashes, or counts. -->
<!-- Coupling review 2026-07-20: the counts (approval, department_module_grants) and feed_direction migrations 000009-000015 plus the seed-roster-real department-module-grants write were reviewed against the vaccination HRMS seed source. They are orthogonal to it (counts/feed tables, not the vaccination roster source), so no fixture/source-data change is required. Recorded in fixtures/vaccination-hrms-source-full/manifest.json -> seed_contract_coupling_reviews. -->
<!-- Coupling review 2026-07-22: adult ET+TT dose-2 post-seed invariant and shed partition name-pattern normalization do not change raw fixture bytes. They change transform/generation validation: partition-bearing shed labels normalize to physical shed + partition metadata, and accepted et_tt_adult_w1 must have same-goat et_tt_adult_w2 work before handoff. -->

<!-- Coupling review 2026-07-23: seed-roster-real gained an operator-roster overlay. When a source dir ships cpt-operator-roster.json it is the authoritative field capacity: the park's resolved seats are recast into equal per-person vaccination_operator_<name> positions (manager tier, not backup) with contract week-offs, the strict PC-manager/backup/park-head requirement is waived for that park, and seed-vaccination-source-full skips shed-manager seeding for the operator-roster park. The committed jun-26 fixture ships no such file, so its behavior is unchanged. -->
<!-- Coupling review 2026-07-23: workforce_positions.vaccination_daily_animal_cap is now the HRMS source of truth for per-operator vaccination animal capacity. Operator-roster rehearsal sources may set animal_cap_per_day; seed-roster-real validates it and writes it to HRMS positions. Runtime scheduling must read that HRMS position cap before tenant/default capacity, so changing an operator's cap changes future drive assignment splitting without changing raw vaccination dates or fixture bytes. -->

<!-- 2026-07-23 operator-config auto-cascade: migration 000036 adds obligation_operator_config_replan_watermarks, an operational idempotency-watermark table (no seed data / no HRMS-source rows; consumer-only). No fixture bytes change. -->
<!-- Coupling review 2026-07-24: for CPT operator-drive reseeds, keep the canonical source/history vaccination matrix intact but run the Makefile target with GOATOS_CPT_EXCLUDE_PPR_2026=1 so the packet publishes no PPR obligations in 2026. -->
<!-- Coupling review 2026-07-24: CPT adult campaign generation ignores entry_date/post_arrival as a strict splitter; adults group by vaccine/rule plus physical shed/partition under the configured cap, using last vaccination date when present and otherwise the partition campaign start. Kid/young schedules keep strict age/entry timing. The one-time 2026-07-25 ET+TT 210-animal allowance is represented only as an explicit seed_catchup_overrides row; normal operator cap semantics remain 200. -->
<!-- Coupling review 2026-07-24: CPT operator-drive clean reseed may start from a freshly migrated local DB. seed-roster-real resolves only centers present in the selected source bundle and can create that required park row before HRMS import; generation history uses the full as-of business day so same-day accepted completions suppress duplicate open work before the 2026-07-25 ET+TT 210 catch-up proof runs. -->
<!-- Coupling review 2026-07-25: Editable vaccination caps (migration 000045 + PUT /vaccination/capacity-config): the operator daily animal cap and a new nullable per-animal shot-cap override are edited on the People/vaccination-operators screen and written to vaccination_capacity_config, cascading vaccination.capacity.changed per active park to re-plan future drives. Seed leaves the override NULL (planner falls back to rule_dsl/default), so no seed fixture, roster, or SOP contract changes. The apply-leave path additionally enforces a min-1-operator-per-day coverage guard (min_operator_coverage 409). -->
<!-- Coupling review 2026-07-25: selected_operator_ids on vaccination_operator_assignment_config is an admin-selected parallel roster preference. Seed leaves it empty; saving config may reassign current/future open planned drive rows, but source fixture bytes, HRMS roster import, SOP definitions, and completed proofs remain unchanged. -->
<!-- Coupling review 2026-07-25: migration 000002 restores selected_operator_ids on already-migrated DBs after the collapsed baseline gained the column. It is a runtime schema repair only; backfill from default_operator_id keeps previous scheduling behavior and does not alter fixture/source contracts. -->
<!-- Coupling review 2026-08-06: pc.vaccination duty derivation. seed-closeout requires an active seat holding BOTH execute and manage for pc.vaccination, because kernelstages/reminder_cadence.go resolves both duty types for the reminder ladder. seed-position-duties previously excluded the WHOLE module from manage, so the requirement was structurally unsatisfiable and every fresh local stack looped "Local database preparation failed". The exclusion is now narrowed to the two prefixes it was always meant to cover -- vaccination_operator_* (inflated HR tier, still executing) and backup_manager (covers the absent manager's tasks, not their authority) -- so preventive_care_manager / park_head / shed_manager earn manage as the cadence file already documented. -->
<!-- Coupling review 2026-08-01: a module that enqueues a verification item must declare BOTH ends -- an entry in notificationbridge.pendingModuleProfiles (its own recipients, wording and tap route; there is deliberately no fallback profile) AND at least one active verify-duty holder in position_module_duties. Missing either means the pending-proof push reaches nobody while every test stays green, which is exactly how the path looked wired for months while notification_requests stayed empty. Both are asserted by tests, not comments. -->

## WEIGHING IS SCAN-AND-SUBMIT (do not re-derive rules)

Assign sheds → individual: scan RFID + weight + video per animal; lump-sum: total
weight + count + video(s) per shed → submit. **The only business rule is: no double
scan of the same animal in a bucket before submit.**

NO shed↔RFID validation · NO roster/expected count/denominator/percentage · NO herd
or goat or clinical lookup · NO vaccine/protocol/obligation rules · NO "shed is empty"
concept (free-flow cannot know what is in a shed).

If a finding assumes any of those exist, it is invalid — close it and cite ban B-5 in
`context/repo-audits/weighing-implementation-do-not-reopen-ledger.md`. Real weighing
findings are about PLUMBING: writes landing, evidence being reviewable, failures being
visible, screens showing honest numbers. Full statement:
`docs/features/weighing/TRD.md` → "What weighing IS".

Weights dashboard reporting rule: when a user selects a date range, whole-shed
daily gain and load gain use the first and latest accepted weighs inside that
same selected range. Do not borrow a 28-day/four-week baseline from outside the
visible period. The table and daily-gain chart show only sheds that have accepted
weigh data in the selected range. Breed+sex chips on shed rows are allowed only
through the recorded `weight_demographics.go` read-only exception; they are row
labels, not scan validation, and mixed whole-shed averages must not be split by
breed or sex.

Isolation does not exempt Weighing from shared operational coordination.
Weighing emits its domain/audit/idempotency/proof/outbox facts atomically; a
shared-kernel consumer outside the Weighing package consumes those events
outward-only into owner/clock, hierarchy, contact-waterfall, proof, and sign-off
task state. The consumer must be receipt-backed, idempotent, version-fenced,
bounded, observable, replayable, and reconciled. Never add an inbound
`task_nodes`, SOP, obligation, roster, herd, or lifecycle dependency to
Weighing, and never let generic task state gate scan-and-submit execution.

<!-- Coupling review 2026-08-04: vaccination drive safe-date override metadata is runtime scheduling state, not source seed data. Requested/applied override dates and conflict metadata do not change raw vaccination/HRMS source files, SOP contracts, seed closeout, fixture hashes, or approved source-date validation. Approved combo helper sharing is code reuse for clinical scheduling only. -->
<!-- Coupling review 2026-08-04: seed-roster-real adds aas_health + milk + feed_direction + vaccination to the health department module grant and milk to preventive_care, extending the same department-grant mechanism recorded on 2026-07-29 for feed_direction. Runtime module/navigation authorization only; no HRMS roster row, vaccination history, source date, fixture byte, hash or count changes. -->
<!-- Coupling review 2026-08-05: CBE/CPT controlled seed-port support is runtime-only: preserve double-tag aliases, constrain sweeps by dose/target, seed verifier grants from existing auth-pending rows, add weighing duties, and use the active position partial-unique key. HRMS fixture/source bytes and validation contracts stay unchanged. Do not generate Blue Tongue or PPR open obligations for the current port; schedule them later only after stock/source confirmation. -->
<!-- Coupling review 2026-08-05 follow-up: when validating CBE/CPT seed-port work, confirm optional `rfid2` values are imported as secondary aliases and that the port is run with `GOATOS_SEED_EXCLUDE_VACCINES=blue_tongue,ppr` until stock/manual scheduling is ready. -->

### Rework is not a terminal state (2026-08-05)

When touching vaccination submit/verify, remember an accepted SOP task can be
reopened by a rejection (`ReopenTaskForRework`) and a weighing bucket refuses to
complete while any animal in it sits in `rework`. Both exist because a rejected
animal's re-submission used to be silently dropped: the server replayed the old
response, the app reported success, and the operator's redone work vanished.

## Kid/adult is a cohort property (do not re-derive it from age)

An animal's `goats.age_band` follows its `management_stage` through
`animal_stage_lookup.age_band` (`kid` / `adult` / NULL). A shifting stamps the destination
cohort's band in the same transaction that moves the animal
(`identity/adapters/postgres.RelocateGoatsToShedInTx`); migration `000109` owns the
classification and the live-herd backfill.

It is deliberately NOT age-derived. In the live CBE/CPT herd, F2 fattening cohorts are kid at up
to 67 weeks and K2 to 55 weeks — "kid" means *not yet in a breeding cohort*, an operational
classification made by placement. A `>20 weeks ⇒ adult` rule would flip 261 animals against the
farm's own record.

Consequences for anyone touching this:

- Classify a new cohort by editing `animal_stage_lookup`, never by adding a rule in Go.
- ICU / Quarantine carry NULL band on purpose: a clinical placement must not reclassify an animal.
- `Warmup` is kid by maintainer decision, deliberately against the source sheet.
- The vaccination **schedule path** is separately age-derived and is expected to disagree; do not
  reconcile them.
- Pinned by `migrations/postgres.TestStageAgeBandClassification` and the
  `story_shifting_kid_to_adult` kernel story.

<!-- Coupling review 2026-08-05 (preventive_care module grants): seed-roster-real drops "milk" from preventive_care defaultDepartmentModules and migration 000110 deactivates the existing preventive_care milk + aas_health department_module_grants rows. No vaccination/HRMS source impact: department_module_grants decides which modules a bottom bar OFFERS and is not a seed source input. No HRMS row, fixture byte/hash/count, goat/DOB/species field, protocol_rules row or vaccination matrix changes. Vaccination operator capacity is unaffected -- it derives from the operator role grant plus shed assignment, never from a department module grant, so the four PC operators keep their drives and their caps. Migration 000110 is DML on department_module_grants only, no canonical-table DDL. -->
<!-- Coupling review 2026-08-14: migrations 000160/000161 put capacity and cohort config on shed_partitions for pen-grain Counts/Sheds editing. Treat them as pen-catalog configuration, not vaccination HRMS source, source-date, SOP proof, protocol, goat_shed_partitions placement, or operator-capacity inputs. The committed vaccination fixture remains byte-stable. -->
<!-- Coupling review 2026-08-15: seed import and runtime generation now share SchedulePathForGoat for kid/adult schedule-path selection. This changes derived scheduling behavior only; raw HRMS fixture bytes, SOP contracts, proof grain, source vaccination dates and roster rows remain unchanged. -->
<!-- Coupling review 2026-08-16: migration 000171 and seed-vaccination-real add Flushing to the writable stage vocabulary with NULL age_band. Treat this as animal_stage_lookup catalog configuration only: no HRMS fixture byte/hash/count, source-date, SOP proof, protocol, goat_shed_partitions placement, or operator-capacity input changes. -->
<!-- Coupling review 2026-08-22: migration 000187 adds NULLABLE workforce_members.first_name/last_name/email for the People/HRMS directory and the in-app Add Person onboarding (POST /admin/workforce/people). No vaccination/HRMS source impact: seed commands never populate the three columns, identity stays display_name/display_code, the unique (tenant_id, lower(email)) partial index ignores NULLs, and login emails are written only by the runtime create-person flow. Migration 000188 creates auth_allowed_emails, a runtime-only auth admission table no seed writes. Fixture bytes, hashes, row counts, SOP contracts, and source vaccination dates are unchanged. -->
<!-- Coupling review 2026-08-26: protocol_rule_dimensions.procurement_purpose is compiled execution-index metadata with DEFAULT 'all'. It is produced from existing rule DSL at publish time to keep procurement purpose filtering in SQL; it is not an HRMS/vaccination source field and does not affect fixture bytes, hashes, source dates, SOP proof grain, roster capacity, or seed closeout. -->
<!-- Coupling review 2026-08-29: manual vaccination anchors are treated as the operational start date for that vaccine family. When building or reviewing vaccination changes, confirm same-family DOB/arrival/calendar/manual_campaign seed rows before that anchor are suppressed while boosters/revacs continue from the manual anchor. No HRMS fixture bytes, hashes, source rows, SOP proof grain, or validation inputs change. -->
