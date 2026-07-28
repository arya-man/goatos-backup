---
name: goatos-code-review
description: Review or audit a Goat OS change (diff, branch, PR, or path) for kernel correctness, aggregate/projection grain and key correctness, release-scale safety (current 5k-50k envelope; 1-5M future certification), hexagonal boundaries, backend + frontend + Android-mobile architecture (Room SSOT / offline / pagination / memory), DB-schema/migration lock-safety, and vaccination/obligation business-rule fidelity — applying root-cause-vs-band-aid, anti-pattern, and blast-radius lenses and returning a bug list (or approval). Orchestrates CRG, Graphify, RTK, and repowise. Use when reviewing code, auditing a diff, or gating a change before push.
version: 0.1.0
user-invocable: true
argument-hint: "[target: diff | branch | PR | path — what to review]"
---

# Goat OS Code Review Skill

Use this skill to **review** Goat OS code — a diff, a branch, a PR, or a path —
not to build it. For building/navigating, use `goatos-build`. This skill is the
review gate: it knows where every layer lives, drives the four review tools
(CRG, Graphify, RTK, repowise), and holds the checklists that turn "the build is
green" into "this is safe to merge at the current 5,000-50,000-animal release
envelope (with 1-5M retained as the future certification gate)."

This file is the single entry point. Route to references below; do not review
from memory alone. Every path here is repo-relative to the goatos checkout root.

## Reviewer principle — patterns over memorized facts

**Verify every volatile specific against its committed source at review time.
This skill gives you the checks, not the current values.**

Anything that drifts between commits — the exact obligation status set, which
transitions are legal, the allowed same-day drive combinations, numeric
gaps/thresholds/TTLs, table and column names, and which `cmd/*` binaries or crons
exist — is NOT authoritative in this document. It is authoritative in the
committed migration `CHECK` constraint, the seeded config / rule DSL, the rules
doc, the Makefile, or `package.json`. Any concrete value written below is
**illustrative and may drift**; when a check names one, treat the named source as
truth and the value as a hint. Never approve or flag on a memorized value — open
the source the check points to and read the live value there.

## When to use

- "Review this diff / branch / PR" · "audit these changes" · "is this safe to merge"
- Before any `git mesha-push main` of non-trivial backend or frontend code
- After `goatos-build` produces a change and you need an independent pass
- Assessing blast radius, scale risk, or business-rule regressions of a change

## Golden context (read first, always)

Goat OS is a Go modular monolith (hexagonal ports & adapters) + an SSR-first
Next.js admin-web + a native Kotlin/Compose Android app (`apps/goatos-android/`,
Room-backed offline-first), built on one **operational kernel**:

> business event → canonical transaction → audit + outbox (same txn) → trigger →
> obligation → sweeper/scheduler → reminder/escalation → notification → proof →
> verification → read-model/projection → leadership answer.

Every feature must plug into that chain and hold at the current release scale
envelope — **5,000-50,000 animals** — with query plans proven at the upper-bound
~500k obligation rows (50k animals × retained obligations) under one kernel
worker's cadence/backlog validation. The **1-5 million-animal** bar is retained as
the FUTURE certification gate, not the present release requirement — see
`docs/decisions/operational-kernel-5k-50k-scale-envelope.md`. At this envelope the
read-model step of the chain is served by **canonical indexed SQL by default**
(list = keyset ~20; summary = indexed aggregate): none of the five named screen
projections run in the active runtime, while `vaccination_eligibility_rollups` and
counts summaries survive — the kernel chain itself is unchanged, only its read
shape. The kernel is the core of the system; review it first. Its law lives in
`context/architecture/operational-kernel.md` (golden rule) and
`context/architecture/operational-kernel-system-design.md` (system design).

## Scope detection (do this first, before the review pass)

Map the changed paths to which reference(s) to load. **A change that touches
multiple layers loads MULTIPLE references** — do not stop at the first match.
The four tools (CRG, Graphify, RTK, repowise) apply on **every** review
regardless of which layer changed.

| Changed path pattern | Load reference(s) |
|---|---|
| `apps/admin-web/**`, `packages/ui`, `packages/rbac`, `packages/forms-dsl`, `packages/api-client` | `references/frontend.md` |
| `apps/goatos-android/**` (Kotlin/Compose app) | `references/mobile.md` |
| `backend/internal/**`, `backend/cmd/**`, `backend/migrations/**` | `references/backend.md` **+** `references/kernel-and-scale.md` |
| Projection/read model/card/summary/calendar/reminder code, or a query combining `JOIN` with aggregation/pagination | `references/aggregates-and-projections.md` **+ producer and consumer lenses** |
| `contracts/openapi`, event-payload / JSON-schema contracts | `references/backend.md` **+** `references/business-rules.md` **+ every consumer lens the contract reaches** (see consumer auto-pull below) |
| Calendar, Control Tower, Action Center, Protocol Adherence, Workflows, admin/mobile execution/proof screens, or new vertical/module onboarding | `docs/architecture/operational-read-model-contract.md` **+** `references/aggregates-and-projections.md` **+ consumer lenses** |
| `docs/**`, `rule_dsl` / protocol config, vaccination/feed rules | `references/business-rules.md` |
| Any change (toolchain / tool-driving) | `references/toolchain.md` (always) |
| **Every review, before flagging anything** | `references/review-lens-ledger.md` (always) — closed decisions + banned patterns; do NOT re-flag a CLOSED/LOCKED item or propose a BANNED one |

Multi-layer rule: if a change touches kernel + backend + frontend together (e.g.
a new obligation type wired from migration → engine → contract → admin-web page),
load `references/kernel-and-scale.md` + `references/backend.md` +
`references/frontend.md` **together** and apply all their checklists. Under-scoping
the load is how a scale or contract regression slips through.

### Proportionality & blast radius (depth = size × reach, NOT size alone)

Review depth scales to **change size AND blast radius AND layers crossed** — never
line count alone. Use the CRG impact-radius / callers query to get reach before
deciding depth. A 3-line diff with wide reach is NOT a small review.

- **Small + isolated** (no cross-layer reach, no consumer, no kernel/scale/security
  surface): related lens(es) only + one fast tool pass. Don't run the kernel/scale
  certification gate for a change that touches nothing kernel.
- **Small diff, wide reach** (e.g. a contract/DTO change consumed by mobile or
  admin-web): pull the **consumer lens** even though no consumer file is in the
  diff. This is where back-compat and mobile over-fetch anti-patterns hide.

**Consumer auto-pull (contract/DTO/list-endpoint changes).** Any change to
`contracts/openapi`, an event/JSON-schema payload, a shared DTO, or a
list/paginated endpoint: run CRG `callers_of` / impact-radius to find who consumes
it, then load the consumer lens for each reached surface —
`references/mobile.md` if an Android client consumes it, `references/frontend.md`
if admin-web does. Two reasons the consumer lens is mandatory, not optional:
(1) an anti-pattern can originate at the **contract shape** — a list endpoint
shipped without a keyset `next_cursor` + `total` *forces* the mobile client to
over-fetch, so it is a contract-level bug flagged at the source PR before the
consumer is even written; (2) `make mobile-guard` is **diff-scoped** in CI, so a
backend-only diff shows zero mobile files and the guard passes green — it is blind
to a backend-induced mobile anti-pattern, and the skill review is the only catch.
The same holds for backend/kernel/DB reach: a small migration or shared-query
change with wide impact still runs the kernel/scale checks of the tables it reaches.

## Fix-quality / regression audit — the primary question for any "fix"

When the change is a **fix** (a commit/PR/diff that claims to resolve a bug,
regression, or audit-ledger row), root-cause quality is the first lens, applied to
**every** layer below — a band-aid that passes tests and compiles is still a
finding. For each fix, answer:

- [ ] **Root cause or symptom?** Does it remove the cause, or only mask the
      observable symptom (a raised timeout on unchanged serial N+1 code, a UI
      state cleared without fixing the data flow, a default that hides an
      out-of-range value)? A symptom patch that will regress is a finding.
- [ ] **Regression test that fails BEFORE the fix.** Is there a test that
      reproduces the bug and would fail on the pre-fix code? "Tests pass" on a fix
      with no failing-before test is unproven — the test may assert the buggy
      behavior or never exercise the path.
- [ ] **One call site fixed while siblings remain.** Was the same root pattern
      fixed everywhere it occurs, or only at the reported call site? Grep/CRG
      `callers_of` the pattern — a fix at one adapter while a sibling adapter keeps
      the bug is a partial fix (illustrative: a "bulk" API that still loops singular
      writes in one path).
- [ ] **Bug moved to another layer.** Did the fix push the defect elsewhere —
      backend permissiveness masking a missing client contract, a frontend guard
      hiding a backend gap, a read-time compute replacing a write-time projection?
- [ ] **False-green confidence.** Does the change create or rely on a guardrail /
      report / gate that reads green without proving the fix — a baseline-
      grandfathered scale guard, a diff-scoped mobile guard on a backend change, a
      prose scale report not tied to the current SHA, a test asserting the wrong
      response key, a skipped/never-started CI job? For static Terraform/HCL
      guards, require block-bounded matching and an adversarial sibling-block
      fixture; a file-wide regex is not proof.
- [ ] **Closure-ledger fixes** additionally run the
      `consolidated-ledger-defect-closure-program.md` proof-packet gate (see
      "Consolidated-ledger closure gate" below) and require independent
      counter-review before the row is marked fixed.

## Review priority order

Review in this order; a failure high on the list blocks merge regardless of how
clean the rest is:

1. **Kernel integrity** — does the change plug into the kernel chain, or does it
   fork a private scheduler / proof / notification / status engine? (`references/kernel-and-scale.md`)
2. **Scale & idempotency (current envelope 5k-50k; 1-5M future certification)** —
   bounded sweepers, tenant/date-filtered indexed queries, keyset pagination,
   bounded goroutines, idempotency key + DB unique constraint, atomic
   state+audit+outbox. (`references/kernel-and-scale.md`) The current release gate
   proves query plans at the upper bound — up to ~500k obligation rows (50k animals
   × retained obligations) — under single-worker cadence/backlog validation; the
   1-5M bar stays as the FUTURE certification gate, retained not deleted. At this
   envelope screens read **canonical indexed SQL by default** (list = keyset ~20;
   summary = indexed aggregate): none of the five named screen projections
   (`calendar_event_projections`, `process_integrity_projection_rows`,
   `vaccination_shed`/`execution`/`operations_projection_rows`) run in the active
   runtime, while `vaccination_eligibility_rollups` and counts summaries survive.
   Those five canonical screen reads are the ONLY sanctioned exemption to the
   compute-on-read ban — each carries a scoped
   `// scale-guard:ignore: 5k-50k-envelope; see operational-kernel-5k-50k-scale-envelope.md`
   annotation plus query-plan tests (list AND aggregate shapes); the guard is NOT
   globally disabled and every other path in `backend/internal/**` stays under full
   enforcement of the seven scale anti-patterns. An exempted read with no plan test
   is a defect. Any aggregate/projection also proves canonical membership, stable
   group key, join cardinality, hierarchy mapping, and page-independent totals using
   `references/aggregates-and-projections.md`. See
   `docs/decisions/operational-kernel-5k-50k-scale-envelope.md`.
3. **Operational read-model contract** — shared command surfaces and
   mobile/admin/reporting reads must follow
   `docs/architecture/operational-read-model-contract.md`: grain-explicit
   counts, declared disjoint/overlapping buckets, page-independent summaries,
   stable selected scope identity, backend/OpenAPI/TS/Kotlin contract sync, and
   cross-surface golden fixtures for new verticals. A screen-local fix that
   merely hides mismatched Calendar/Control Tower/Protocol Adherence/mobile
   numbers is a finding.
4. **Security / privacy / tenant isolation** — every scoped query filters
   `tenant_id`; no secrets/tokens/service-account JSON in logs; input validated
   at boundaries. (Goat identifiers are livestock data, NOT PII — log them.)
5. **Architecture boundaries** — domain/app/ports/adapters layering; no
   cross-module table writes; vendor SDKs confined to adapters. (`references/backend.md`)
6. **Business-rule fidelity** — vaccination schedule/gaps, obligation state
   machine, defer/re-scope, org/species model. Wrong medical rules are worse than
   wrong code. (`references/business-rules.md`)
7. **Observability & resilience** — kernel-boundary logging via `platform/observability`,
   metrics on new APIs/workers, DLQ + retry bounds, durable notifications.
8. **UI contract & mock fidelity** — admin-web renders backend-owned contracts;
   ports the mock; passes `check:mock-fidelity`. (`references/frontend.md`)
9. **Maintainability** — small focused files, explicit errors, tests.

## Consolidated-ledger closure gate

When a change claims to fix any row in
`context/repo-audits/last-35-commits-consolidated-bug-ledger.md`, load and apply
`context/repo-audits/consolidated-ledger-defect-closure-program.md` in addition
to every layer reference selected above. Review the current-SHA proof packet,
not only the diff. Reject the closure claim if any applicable real-Postgres,
retry/idempotency, pagination, contract/API, admin-web, Android Room/offline,
logout, performance/memory, authorization, architecture, observability, guard
self-test, ordinary-PR CI, or independent-counter axis is missing. Confirm that
duplicate-root evidence was merged and every ledger count/status summary was
reconciled mechanically. A compile, typecheck, screenshot, mock-only test,
missing/skipped workflow, or prose report is not closure proof.

## Volatile anchors — verify, don't trust the list below

These are the checks whose values move. For each, the review action is "open the
named committed source and read the live value," not "compare to a number here."

- **Obligation status set + legal transitions.** Verify the allowed status values
  against the latest `CHECK` constraint in `backend/migrations/postgres/` (the
  most recent migration that alters `obligation_instances` status wins — grep the
  full migration set, do not assume an early one is current). Verify legal
  transitions against `docs/protocol-engine/state-machines.md` and
  `docs/protocol-engine/obligation-engine.md`. *Illustrative only, may drift:* the
  set has included `scheduled` (default), `due`, `in_progress`, `deferred`,
  `completed`, `missed`, `waived`, `canceled`, `superseded`; terminal states are
  the completed/missed/waived/canceled/superseded family. `pending`/`assigned` are
  NOT obligation statuses — `pending` appears only as a computed view label in the
  HTTP work-state mapping, so a diff that writes `pending`/`assigned` to
  `obligation_instances.status` is a bug to flag.
- **Idempotency tables + conflict targets.** Verify the write path reserves a key
  and persists it in the same txn, with a DB unique constraint backing the
  `ON CONFLICT ... DO NOTHING`. *Illustrative only, may drift:* outbox uses
  `outbox_messages`, processed-events `domain_event_processed_events` (composite
  PK dedupe), DLQ `outbox_dlq_actions` (unique on `(tenant_id, idempotency_key)`),
  obligations `obligation_instances` (unique on `(tenant_id, idempotency_key)`)
  and `obligation_batches`, and the reservation table `idempotency_keys` (PK
  `idempotency_key`). Confirm the actual table/column/constraint in the touched
  migration and the sqlc/`commands.sql` insert, not this list.
- **Vaccination schedule / gaps / same-day combos.** Verify against
  `docs/preventive-care-vaccination/vaccination-rules.md` and the seeded `rule_dsl`
  / config — never against a memorized week number or combo set. The allowed
  same-day bundles and inter-dose gaps are rule-doc + seeded-config truth.
- **Vaccination drive clubbing.** Per-animal `due_at` is not a drive boundary.
  Exact-date batching that creates 1-2 animal micro-drives while compatible
  nearby animals are still inside buffer is a business-rule defect. Verify
  `make vaccination-drive-clubbing-guard` for sweeper/planner/calendar changes.
- **`cmd/*` binaries and crons.** Verify a referenced sweeper/worker actually
  exists under `backend/cmd/` before treating "the X cron does Y" as real. Real
  binaries include `obligation-sweeper`, `outbox-relay`, `outbox-dlq`,
  `notification-dispatcher`, `domain-event-consumer`,
  `domain-event-processed-sweeper`, `idempotency-key-sweeper`,
  `partition-maintainer`, `generate-vaccination-obligations`, and the
  `calendar-*` sweepers/projectors — *illustrative, grep `backend/cmd/` for the
  current set.* Do NOT assume `in-progress-timeout` or `drive-membership` binaries
  exist; they do not. Stub dirs carry only a `.gitkeep`.
- **Timezone.** Goat OS medical/business calendar days are resolved in the
  operational location timezone (currently `Asia/Kolkata` by default via
  `locations.timezone`). Business dates must use `platform/biztime` or an
  explicit location timezone. Raw UTC may appear only for non-calendar instants
  such as audit/event storage and deterministic event-key normalization, never
  for due/missed/recovery/drive calendar-day decisions. Flag raw `UTC().Date()`
  / `time.Now().UTC()` day bucketing in those decisions.
- **Make targets / npm scripts / thresholds.** Only cite a target/script the
  Makefile or `package.json` actually defines; verify before asserting one exists.

## The review pass (drive the tools in this order)

Do not open files first. Query the graphs, view the diff through RTK, then read
only what the graphs point at. Full operator manual: `references/toolchain.md`.

1. **Scope the change (CRG).** Cold-review entry tool then the change-detection
   tool — what changed, affected flows, test-coverage gaps. `repo_root` = your
   goatos checkout (`git rev-parse --show-toplevel`).
2. **View the diff (RTK).** `git diff main...HEAD` — the `pre-rtk-git-diff.sh`
   hook auto-routes large diffs through `rtk` so raw diff bytes never flood
   context. (`GOATOS_RTK=0` to bypass; gate `GOATOS_RTK_MIN_BYTES`, default 50000.)
3. **Blast radius (CRG).** The impact-radius tool plus targeted graph-query calls
   — who calls the changed symbols, which kernel flows are touched. Run the
   tests-for query — is the change tested?
4. **Health & risk (repowise).** `repowise risk <range>` (defect risk of the
   change), `repowise health`, `repowise dead-code`; or `repowise serve` →
   http://localhost:3000 for the health/risk/graph/coverage dashboard.
5. **Business cross-check (Graphify).** Query the docs graph for the rules the
   change touches (vaccination timing, obligation states, feed/calendar). Read
   the authoritative doc it names before judging domain logic.
6. **Read the suspects (Grep/Read).** Only now open the files the graphs flagged,
   for the blind spots graphs can't see: SQL strings, route strings, constants,
   migrations, uncommitted code — and the volatile anchors above (status
   constraints, rule DSL, `cmd/` set).

Then apply the reference checklist(s) for the changed layer and the fix-quality
audit above, and produce the result in the **Output contract** shape below —
default is a bug list only, or an approval when clean.

### CRG tool namespace (harness-dependent)

CRG tools are described by **role** in this skill, not by exact name, because the
MCP namespace differs per harness. When loading them via ToolSearch:

- **Claude harness:** `mcp__code-review-graph__<tool>` (hyphens) — e.g.
  `select:mcp__code-review-graph__detect_changes_tool,mcp__code-review-graph__get_impact_radius_tool`.
- **Codex harness:** `mcp__code_review_graph__<tool>` (underscores).

Role → tool mapping (verify the tool is present in your harness before relying on
it): cold-review entry = `get_minimal_context_tool`; change detection =
`detect_changes_tool`; blast radius = `get_impact_radius_tool`; graph traversal
(callers/callees/imports/tests) = `query_graph_tool`; affected execution flows =
`get_affected_flows_tool` **only if ToolSearch exposes it**. If a tool is absent
in the active harness, derive the same review context from `detect_changes_tool`,
`get_impact_radius_tool`, targeted `query_graph_tool`, and Grep for graph blind
spots rather than assuming the optional tool exists.

## Output contract (what `/code-review` returns)

The review output is a **bug list, or an approval — nothing else.** No praise, no
narration of what you read, no restating the diff, no per-lens walkthrough when it
found nothing. The reader wants the bugs or the green light.

### Default (first review of a target)

If any bugs are found, output **only the ranked bug list**, most severe first.
Nothing before it except one line: `Reviewed <target> · <N> findings (Px…Py)`.
Each finding is compact but actionable — never a bare title:

```
<ID> · <P0|P1|P2|P3> · <one-line title>
  where:   <file:line> (+ sibling sites if the pattern repeats)
  bug:     <failure scenario — concrete input/state → wrong output/crash/leak>
  fix:     <one-line fix direction>
  guard:   <missing test/guardrail that would have caught it>   # omit if none
```

Severity = the priority rule (P0 data loss / wrong medical action / tenant-security
break / outage; P1 scale / broken core rule / offline leak / false-green gate;
P2 bounded correctness / weak guard / missing test; P3 maintainability). Use the
full FINDING FORMAT (Origin, Verdict CONFIRMED/PLAUSIBLE, Root-cause-or-band-aid,
E2E/guardrail status, etc.) only when the caller asks for the audit-grade ledger
or the target is a full audit — otherwise keep the compact 4-line shape above.

If **zero bugs**: output the approval, nothing else —
`APPROVED · <target> · <lenses applied> · <gates run/NA>`. Approval requires the
scope-selected lenses actually applied and any mandatory gate for the layer run or
explicitly marked N/A (mock-fidelity for frontend, sqlc-plan/hot-index for
hot-path DB, mobile-guard for mobile, `make ai-doctor` before push). Never approve
on "build is green" alone.

### Re-review (a prior review exists for this target)

When re-reviewing after fixes (iterative rounds on the same PR/branch), do NOT
re-emit the whole list. Reconcile against the prior findings and output:

1. **Fixed** — a short summary of which prior findings are now resolved, each with
   its current-SHA proof (the file:line that changed + the failing-before test now
   passing). A finding is "fixed" only with proof; "marked done" is not fixed.
2. **Still open / regressed / newly found** — the remaining bug list in the same
   compact shape.
3. **Verdict** — `APPROVED` **only when every raised bug is fixed-with-proof** (and
   for closure-ledger rows, independent counter-review passed). Until then the
   output stays a bug list; state `NOT APPROVED · <n> open` at the top.

Track findings by stable ID across rounds so "all raised bugs fixed" is mechanical,
not vibes. For consolidated-ledger work, the canonical list lives in
`context/repo-audits/last-35-commits-consolidated-bug-ledger.md`; reconcile counts
there rather than inventing a competing list.

## Reference routing

Load only the reference(s) the scope-detection step selected — progressive
disclosure. (Multi-layer changes load multiple; see Scope detection above.)

| Change touches | Load |
|---|---|
| Kernel chain, sweepers, scale, idempotency, generic engine | `references/kernel-and-scale.md` |
| Go backend: modules, layering, pgx/sqlc, migrations, observability, tests | `references/backend.md` |
| admin-web / Next.js: contracts, mock fidelity, IA, data access | `references/frontend.md` |
| Goat OS Android (Kotlin/Compose): Room SSOT, pagination, offline, memory, lifecycle | `references/mobile.md` |
| Aggregate/projection/read-model/card/calendar/reminder summary or paged rail | `references/aggregates-and-projections.md` plus every reached producer/consumer lens |
| A contract/DTO/list-endpoint consumed by a mobile or admin-web client | consumer lens (`references/mobile.md` / `references/frontend.md`) — see Proportionality & blast radius |
| Vaccination / obligation / feed / calendar / SOP / org / species rules | `references/business-rules.md` |
| Which tool to run, how to run it, in what order | `references/toolchain.md` |

Deeper source-of-truth docs (not duplicated here — read the doc):

- Kernel: `context/architecture/operational-kernel.md`, `operational-kernel-system-design.md`
- Release-scale envelope (authority): `docs/decisions/operational-kernel-5k-50k-scale-envelope.md`
- Scale (future 1-5M certification): `docs/protocol-engine/high-scale-kernel-validation-plan.md`
- Backend stack: `docs/decisions/go-backend-stack.md`, `docs/decisions/observability.md`
- Dashboards at scale: `docs/decisions/high-scale-dashboard-projections.md`
- Frontend: `context/frontend/final-frontend-mobile-backend-architecture.md`,
  `current-admin-web-scope.md`, `admin-web-backend-ui-contract.md`
- Business rules: `docs/preventive-care-vaccination/vaccination-rules.md`,
  `docs/protocol-engine/obligation-engine.md`, `docs/protocol-engine/state-machines.md`,
  `docs/decisions/calendar-ownership.md`
- Org/species base: `context/source-findings/goats-and-parks-source-findings.md`
- Repo AGENTS rules: `AGENTS.md`, `backend/AGENTS.md`, `apps/admin-web/AGENTS.md`

## Standalone lens skills — when each applies

These invokable lens skills are **thin entrypoints** (table-of-contents pointers),
not a second copy of the rules. The detailed rules stay in the review `references/`
chapters above and the canonical decision docs; each lens links to them. Invoke a
lens when its trigger matches — it routes you to the same canonical detail this
skill uses, so Claude and Codex land on one source of truth.

| Lens skill | Invoke when the change touches | Fronts (canonical detail) |
|---|---|---|
| `scale-anti-patterns` | `backend/internal/**` query / worker / repo / SQL; hot-path read; dashboard slice | `references/kernel-and-scale.md` + `docs/decisions/scale-anti-patterns.md`, `operational-kernel-5k-50k-scale-envelope.md`, `high-scale-dashboard-projections.md` |
| `db-migration-safety` | a Postgres migration, hot-path query, read-model, or any mutating write path | `references/backend.md` + `references/aggregates-and-projections.md` + `docs/decisions/scale-anti-patterns.md`, `room-migration-safety.md`, `stale-binary-migration-drift-guard.md` |
| `kernel-scale-lens` | a trigger / obligation / reminder / sweeper / projection / notification / Calendar / AC / PA / process-integrity path | `context/architecture/operational-kernel.md` + `references/kernel-and-scale.md` + `docs/decisions/operational-kernel-5k-50k-scale-envelope.md`, `high-scale-dashboard-projections.md` |
| `frontend-anti-patterns` | `apps/admin-web/**` page / SSR read / nav / label / dashboard | `references/frontend.md` (+ `references/mobile.md`) + `docs/decisions/calendar-ownership.md`, `high-scale-dashboard-projections.md`, `mobile-data-fetch-anti-patterns.md` |
| `mobile-anti-patterns` | `apps/goatos-android/**` list fetch / Room / offline / memory / lifecycle | `references/mobile.md` + `docs/decisions/mobile-data-fetch-anti-patterns.md`, `android-offline-first.md`, `room-migration-safety.md` |
| `nav-composition` | nav rendering, role/module gating, sidebar/bottom-bar composition | `references/frontend.md` + `docs/decisions/role-module-nav-composition.md` |

A multi-layer change invokes multiple lenses — same rule as the reference-routing
table. Every machine gate a lens names is registered in
`tools/ci/guardrail-manifest.json` and wired into `make guardrails` /
`tools/ci/run-local-ci.sh`.

## Kernel / scale changes — run the scale gate

If the change touches the operational kernel (triggers, obligations, sweepers,
outbox, notifications, projections) or any hot-path query on large tables, do not
approve on unit tests alone. The **current release gate is the 5k-50k envelope**:
canonical indexed reads, one kernel worker, and query plans proven at the
upper-bound ~500k obligation rows (50k animals × retained obligations) under
single-worker cadence/backlog validation. The **1-5M full certification remains
the FUTURE gate** (`make high-scale-kernel-e2e-certification`), retained not
deleted per `docs/decisions/operational-kernel-5k-50k-scale-envelope.md`. Confirm
the scale gates:

```bash
# from the goatos checkout root
make validate-sqlc-plans              # indexed access path for hot-table queries
make validate-hot-index-migrations    # hot-row index migrations are present
make validate-migrations              # migration set is well-formed
make high-scale-kernel-e2e-data       # data-plane kernel e2e (no browser)
make high-scale-kernel-e2e-certification   # full certification gate (browser)
```

An exempted canonical screen read (one of the five named projections retired for
this envelope) must ship query-plan tests for BOTH the keyset list AND the indexed
aggregate shape, and run the aggregate path against the upper-bound row count — a
green plan at 5k is not proof for the 50k aggregate, and an exempted read with no
plan test is a defect. Mark in the review whether the kernel/scale change ran (or
must run) the scale gate before push. A new hot-path query without `make
validate-sqlc-plans` coverage is a HIGH finding.

## Maintainer-rule lock

If the change encodes a **new** business/medical rule, timing, or workflow that
contradicts existing docs/config/kernel behavior, do NOT silently accept it.
Surface the conflict (old source vs new change side by side) and require an
explicit maintainer decision before approving — per `AGENTS.md` "Business and
medical rule changes." Confirmed override: never accept mother-vaccination-status
as a scheduling input.

## Mandatory review checklist

Every review MUST verify ALL of the following before approval. This is the bind to
operational invariants that turn "the build is green" into "this is safe to merge":

- [ ] **Forward-progress pagination:** cursor is monotonic; next page cannot regress;
      page size never silently changes business completeness of a projection read
- [ ] **Effective-state validation on partial updates:** a partial edit re-validates
      the WHOLE effective locked record (not just the changed field); date/status/
      rule/version mutations are atomic with validation
- [ ] **Retry-attempt accounting:** claiming/leasing work is NOT an attempt; attempts
      charged only when delivery is actually attempted; cancellation releases untouched
      work without consuming a retry
- [ ] **Durable recorders present & fail-closed:** notifications, reminders, escalations,
      manual-review queues are persisted rows, not logs; write failure fails closed
- [ ] **Operator-visible manual-review queues:** durable, paginated, visible in Control
      Tower / Action Center / Protocol Adherence, resolvable (approved/rejected/waived)
- [ ] **Guard-to-CI wiring:** any new guardrail is registered in the guardrail manifest
      AND wired into `make guardrails` / full local CI (not left as diff-only or disabled)
- [ ] **Guards match deployed configuration:** a guard that reads config must read the
      real deployed values or require explicit configuration in the rule/test (not default
      silently to safe-at-code-review, unsafe-at-runtime)

Do not approve if any leg of this checklist is incomplete. A green build without this
proof is a false-green confidence gate.

## After the review — commit & push

### Pre-push authority gate (verify before any push)

This workspace also has Heva and Slice GitHub/Cloud accounts that must never
touch this repo. Before pushing, state and verify the authority tuple:

- **Branch** — you are on the intended branch, not `main` directly if a branch
  was expected.
- **Remote URL / org / repo** — `git remote -v` resolves to `vgoats/goatos`
  (Mesha/VGoats). Stop if it points at Heva, Slice, `hevaplatform`, or any
  non-Mesha org.
- **Push path** — the push uses the Mesha PAT path `git mesha-push main` (backed
  by `MESHA_GITHUB_PAT`, user `ravimesha`, org `vgoats`). **Never** push via a
  `gh` account — the active `gh` account may be Heva or Slice, which is the wrong
  org for Goat OS.

If any leg of the tuple is wrong, correct context before proceeding — do not push.

### Push

Reviews that end in an accepted change push to `main` via the Mesha/VGoats token:

```bash
# from the goatos checkout root
make ai-doctor                       # portability gate — must pass before push
npm --prefix apps/admin-web run check:mock-fidelity   # if frontend changed
git add <reviewed paths>             # never git add -A — leave in-flight work alone
git commit -m "<type>: <what changed>"
git mesha-push main                  # uses $MESHA_GITHUB_PAT (user ravimesha, org vgoats)
```

CI (`.github/workflows/ci.yml`) re-runs `make ai-doctor` + boundary/contract-drift
guards on push. Generated graphs (`graphify-out/`, `.code-review-graph/`,
`.repowise/`) are gitignored and machine-local — never commit them.

## Portability rule for this skill

These skill files are committed. `make ai-doctor` lints them: it **bans the
repo-root path token** (the `<user>/mesha/goatos` absolute prefix) in committed
active docs/skills, and runs a resolve-smoke proving repo-relative paths resolve
from any cwd. So keep every path **repo-relative** (`context/...`,
`backend/internal/...`, `./graphify-out/graph.json`). The one allowed absolute
path is the maintainer-local Mesha wiki graph
(`/Users/ravi/mesha/graphify-out/graph.json`), which lives outside this repo and
cannot be made repo-relative — do not write the repo-root path in any committed
doc.
