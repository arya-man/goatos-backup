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

## The review pass (drive the tools in this order)

Do not open files first. Query the graphs, view the diff through RTK, then read
only what the graphs point at. Full operator manual: `references/toolchain.md`.

1. **Scope the change (CRG).** `get_minimal_context_tool` then
   `detect_changes_tool` — what changed, affected flows, test-coverage gaps.
   `repo_root` = your goatos checkout (`git rev-parse --show-toplevel`).
2. **View the diff (RTK).** `git diff main...HEAD` — the `pre-rtk-git-diff.sh`
   hook auto-routes large diffs through `rtk` so raw diff bytes never flood
   context. (`GOATOS_RTK=0` to bypass; gate `GOATOS_RTK_MIN_BYTES`, default 50000.)
3. **Blast radius (CRG).** `get_impact_radius_tool` plus targeted
   `query_graph_tool` calls — who calls the changed symbols, which kernel flows
   are touched.
   `query_graph_tool tests_for` — is the change tested?
4. **Health & risk (repowise).** `repowise risk <range>` (defect risk of the
   change), `repowise health`, `repowise dead-code`; or `repowise serve` →
   http://localhost:3000 for the health/risk/graph/coverage dashboard.
5. **Business cross-check (Graphify).** Query the docs graph for the rules the
   change touches (vaccination timing, obligation states, feed/calendar). Read
   the authoritative doc it names before judging domain logic.
6. **Read the suspects (Grep/Read).** Only now open the files the graphs flagged,
   for the blind spots graphs can't see: SQL strings, route strings, constants,
   migrations, uncommitted code.

Then apply the reference checklist(s) for the changed layer and report findings
by severity (CRITICAL blocks; HIGH should fix; MEDIUM/LOW note).

## Reference routing

Load only the reference(s) the change touches — progressive disclosure.

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
  `docs/protocol-engine/obligation-engine.md`, `docs/decisions/calendar-ownership.md`
- Org/species base: `context/source-findings/goats-and-parks-source-findings.md`
- Repo AGENTS rules: `AGENTS.md`, `backend/AGENTS.md`, `apps/admin-web/AGENTS.md`

## Maintainer-rule lock

If the change encodes a **new** business/medical rule, timing, or workflow that
contradicts existing docs/config/kernel behavior, do NOT silently accept it.
Surface the conflict (old source vs new change side by side) and require an
explicit maintainer decision before approving — per `AGENTS.md` "Business and
medical rule changes." Confirmed override: never accept mother-vaccination-status
as a scheduling input.

## After the review — commit & push

Reviews that end in an accepted change push to `main` via the Mesha/VGoats token
(never a `gh` account — this workspace also has Heva/Slice accounts that must not
touch this repo):

Before any push, state and verify the authority tuple: branch, remote org/repo,
git identity, and that `MESHA_GITHUB_PAT` is present. For Goat OS the target must
be Mesha/VGoats (`vgoats/goatos`) on `main`; stop if the remote or identity points
at Heva, Slice, or any non-Mesha organization.

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

These skill files are committed and linted by `make ai-doctor`. Keep every path
**repo-relative** (`context/...`, `backend/internal/...`, `./graphify-out/graph.json`).
The only allowed absolute path is the maintainer-local Mesha wiki graph
(`/Users/ravi/mesha/graphify-out/graph.json`), which lives outside this repo — do
not write the repo-root path in any committed doc.
