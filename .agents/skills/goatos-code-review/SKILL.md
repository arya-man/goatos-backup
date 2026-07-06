---
name: goatos-code-review
description: Review or audit a Goat OS change (diff, branch, PR, or path) for kernel correctness, 1-5M-animal scale safety, hexagonal boundaries, backend + frontend architecture, and vaccination/obligation business-rule fidelity. Orchestrates CRG, Graphify, RTK, and repowise. Use when reviewing code, auditing a diff, or gating a change before push.
version: 0.1.0
user-invocable: true
argument-hint: "[target: diff | branch | PR | path — what to review]"
---

# Goat OS Code Review Skill

Use this skill to **review** Goat OS code — a diff, a branch, a PR, or a path —
not to build it. For building/navigating, use `goatos-build`. This skill is the
review gate: it knows where every layer lives, drives the four review tools
(CRG, Graphify, RTK, repowise), and holds the checklists that turn "the build is
green" into "this is safe to merge at 1-5M animals."

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
Next.js admin-web, built on one **operational kernel**:

> business event → canonical transaction → audit + outbox (same txn) → trigger →
> obligation → sweeper/scheduler → reminder/escalation → notification → proof →
> verification → read-model/projection → leadership answer.

Every feature must plug into that chain and hold at **1-5 million animals**. The
kernel is the core of the system; review it first. Its law lives in
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
| `backend/internal/**`, `backend/cmd/**`, `backend/migrations/**` | `references/backend.md` **+** `references/kernel-and-scale.md` |
| `contracts/openapi`, event-payload / JSON-schema contracts | `references/backend.md` **+** `references/business-rules.md` |
| `docs/**`, `rule_dsl` / protocol config, vaccination/feed rules | `references/business-rules.md` |
| Any change (toolchain / tool-driving) | `references/toolchain.md` (always) |

Multi-layer rule: if a change touches kernel + backend + frontend together (e.g.
a new obligation type wired from migration → engine → contract → admin-web page),
load `references/kernel-and-scale.md` + `references/backend.md` +
`references/frontend.md` **together** and apply all their checklists. Under-scoping
the load is how a scale or contract regression slips through.

## Review priority order

Review in this order; a failure high on the list blocks merge regardless of how
clean the rest is:

1. **Kernel integrity** — does the change plug into the kernel chain, or does it
   fork a private scheduler / proof / notification / status engine? (`references/kernel-and-scale.md`)
2. **Scale & idempotency (1-5M)** — bounded sweepers, tenant/date-filtered
   indexed queries, keyset pagination, bounded goroutines, idempotency key + DB
   unique constraint, atomic state+audit+outbox. (`references/kernel-and-scale.md`)
3. **Security / privacy / tenant isolation** — every scoped query filters
   `tenant_id`; no secrets/tokens/service-account JSON in logs; input validated
   at boundaries. (Goat identifiers are livestock data, NOT PII — log them.)
4. **Architecture boundaries** — domain/app/ports/adapters layering; no
   cross-module table writes; vendor SDKs confined to adapters. (`references/backend.md`)
5. **Business-rule fidelity** — vaccination schedule/gaps, obligation state
   machine, defer/re-scope, org/species model. Wrong medical rules are worse than
   wrong code. (`references/business-rules.md`)
6. **Observability & resilience** — kernel-boundary logging via `platform/observability`,
   metrics on new APIs/workers, DLQ + retry bounds, durable notifications.
7. **UI contract & mock fidelity** — admin-web renders backend-owned contracts;
   ports the mock; passes `check:mock-fidelity`. (`references/frontend.md`)
8. **Maintainability** — small focused files, explicit errors, tests.

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

Then apply the reference checklist(s) for the changed layer and report findings
by severity (CRITICAL blocks; HIGH should fix; MEDIUM/LOW note).

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

## Reference routing

Load only the reference(s) the scope-detection step selected — progressive
disclosure. (Multi-layer changes load multiple; see Scope detection above.)

| Change touches | Load |
|---|---|
| Kernel chain, sweepers, scale, idempotency, generic engine | `references/kernel-and-scale.md` |
| Go backend: modules, layering, pgx/sqlc, migrations, observability, tests | `references/backend.md` |
| admin-web / Next.js: contracts, mock fidelity, IA, data access | `references/frontend.md` |
| Vaccination / obligation / feed / calendar / SOP / org / species rules | `references/business-rules.md` |
| Which tool to run, how to run it, in what order | `references/toolchain.md` |

Deeper source-of-truth docs (not duplicated here — read the doc):

- Kernel: `context/architecture/operational-kernel.md`, `operational-kernel-system-design.md`
- Scale: `docs/protocol-engine/high-scale-kernel-validation-plan.md`
- Backend stack: `docs/decisions/go-backend-stack.md`, `docs/decisions/observability.md`
- Dashboards at scale: `docs/decisions/high-scale-dashboard-projections.md`
- Frontend: `context/frontend/final-frontend-mobile-backend-architecture.md`,
  `current-admin-web-scope.md`, `admin-web-backend-ui-contract.md`
- Business rules: `docs/preventive-care-vaccination/vaccination-rules.md`,
  `docs/protocol-engine/obligation-engine.md`, `docs/protocol-engine/state-machines.md`,
  `docs/decisions/calendar-ownership.md`
- Org/species base: `context/source-findings/goats-and-parks-source-findings.md`
- Repo AGENTS rules: `AGENTS.md`, `backend/AGENTS.md`, `apps/admin-web/AGENTS.md`

## Kernel / scale changes — run the certification gate

If the change touches the operational kernel (triggers, obligations, sweepers,
outbox, notifications, projections) or any hot-path query on large tables, do not
approve on unit tests alone. Confirm the scale gates:

```bash
# from the goatos checkout root
make validate-sqlc-plans              # indexed access path for hot-table queries
make validate-hot-index-migrations    # hot-row index migrations are present
make validate-migrations              # migration set is well-formed
make high-scale-kernel-e2e-data       # data-plane kernel e2e (no browser)
make high-scale-kernel-e2e-certification   # full certification gate (browser)
```

Mark in the review whether the kernel/scale change ran (or must run) the
certification gate before push. A new hot-path query without `make
validate-sqlc-plans` coverage is a HIGH finding.

## Maintainer-rule lock

If the change encodes a **new** business/medical rule, timing, or workflow that
contradicts existing docs/config/kernel behavior, do NOT silently accept it.
Surface the conflict (old source vs new change side by side) and require an
explicit maintainer decision before approving — per `AGENTS.md` "Business and
medical rule changes." Confirmed override: never accept mother-vaccination-status
as a scheduling input.

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
