# Goat OS Skills

Use this file as the human-readable index for Codex, Claude, and future coding
agents. It follows the same pattern as the Heva skill index: one entry point,
then focused reference files by topic.

## Primary Skill

Use the Goat OS build skill for all product, architecture, phase, contract,
backend, frontend, mobile, analytics, infra, and review work:

```text
.agents/skills/goatos-build/SKILL.md
```

Claude discovers the same skill through a symlink:

```text
.claude/skills/goatos-build -> ../../.agents/skills/goatos-build
```

Do not hand-maintain two copies. `.agents/skills/goatos-build/` is the source.

## Code Review Skill

Use the Goat OS code-review skill to **review or audit** a change (diff, branch,
PR, or path) — kernel correctness, 1-5M-animal scale safety, hexagonal
boundaries, backend + frontend architecture, and vaccination/obligation
business-rule fidelity. It orchestrates CRG, Graphify, RTK, and repowise. Use
`goatos-build` to build; use this to review and gate before push.

```text
.agents/skills/goatos-code-review/SKILL.md
```

Claude discovers the same skill through a symlink:

```text
.claude/skills/goatos-code-review -> ../../.agents/skills/goatos-code-review
```

Reference docs inside (progressive disclosure — load only what the change touches):

```text
.agents/skills/goatos-code-review/references/toolchain.md         # CRG/Graphify/RTK/repowise operator manual
.agents/skills/goatos-code-review/references/kernel-and-scale.md  # operational kernel + 1-5M scale + idempotency
.agents/skills/goatos-code-review/references/backend.md           # Go hexagonal layering, pgx/sqlc, observability
.agents/skills/goatos-code-review/references/aggregates-and-projections.md # membership, group keys, join grain, paging
.agents/skills/goatos-code-review/references/frontend.md          # admin-web contract fidelity, mock, IA guardrails
.agents/skills/goatos-code-review/references/business-rules.md    # vaccination/obligation/feed/org rule fidelity
```

Operational read model contract:

```text
docs/architecture/operational-read-model-contract.md
docs/decisions/operational-location-display-contract.md
```

Load this contract whenever a change touches Calendar, Control Tower, Action
Center, Protocol Adherence, Workflows, admin-web detail pages, Android
execution/proof screens, OpenAPI/generated clients, or any new vertical/module
such as shifting, counts, breeding, weighing, feed, or procurement. It is the
default pattern for pluggable verticals: canonical facts -> grain-explicit
operational read model -> shared surface adapters.

**MANDATORY: Operational Location rule** — when a shed partition exists (a
`Castro 1` alongside `Castro 2`), every user-facing surface must render the
partition label (`"Castro 2"`, never just `"Castro"`), group/key by `shed_id` +
park (never by name), and never collapse partitions into the parent unless
explicitly the aggregate. Full contract and shared primitives:
`docs/decisions/operational-location-display-contract.md`.

Discoverability/static-text guard:
`make operational-read-model-contract-guard`.

Critical animal action guardrails:

```text
docs/features/critical-animal-action-guardrails.md
```

Load this contract whenever a change touches quarantine, ICU, death,
contagious-disease isolation, high-risk movement, vaccination defer/reopen,
weighing availability, or sale/allocation blockers. Interim availability guard:
`make critical-animal-action-availability-guard`.

Android row-action scope guardrail:

```text
docs/decisions/mobile-data-fetch-anti-patterns.md
```

Load the "Android row-action scope" section whenever a change touches repeated
mobile cards with per-row actions, especially weighing free-flow animal cards.
An individual animal row Save/Update/Retry must use row/animal-scoped in-flight
state, never a screen-wide `actionInFlight`/`busy` gate. Machine guard:
`make android-row-action-scope-guard` (also included in `make mobile-guard` and
local CI).

## Leadership Assistant Coverage Skill

Use the leadership-assistant skill whenever a change adds or modifies a
**leadership-relevant** table, read API, OpenAPI contract, admin-web route,
mobile workflow, reporting view, domain event, or official KPI. It is the
canonical HOW-TO for keeping the Mesha leadership assistant (CEO/CXO read-only
chatbot) read path in sync — Cube-first routing, `ceo_ai.*` views, MCP Toolbox
tools, read-API mappings, GenAI query-classes, evals — or writing a documented
exclusion.

```text
.agents/skills/goatos-leadership-assistant/SKILL.md
```

Claude discovers the same skill through a symlink:

```text
.claude/skills/goatos-leadership-assistant -> ../../.agents/skills/goatos-leadership-assistant
```

Reference docs inside (progressive disclosure):

```text
.agents/skills/goatos-leadership-assistant/references/coverage-howto.md  # step-by-step + copy-paste per tier
.agents/skills/goatos-leadership-assistant/references/architecture.md    # agentic loop, ports, safety, persistence, streaming, eval
.agents/skills/goatos-leadership-assistant/references/exclusions.md      # what is legitimately NOT leadership-relevant
```

Machine gate: `make leadership-assistant-coverage-guard`
(`tools/agent-hooks/check-leadership-assistant-coverage.mjs`), registered in
`tools/ci/guardrail-manifest.json`, wired into `make guardrails` /
`tools/ci/run-local-ci.sh`, and nudged on PostToolUse for Claude
(`.claude/settings.json`) and Codex (`.codex/hooks.json`). Scaffold:
`node tools/ceo-ai/scaffold-coverage.mjs <module>`. Backfill baseline:
`docs/ceo-ai/coverage-matrix.md`.

## Agent tool routing (human)

Before starting work, read **`docs/ai/agent-tool-routing.md`**:

- **Cursor** — admin-web UI, mock fidelity, small single-file fixes.
- **Claude Code** (`claude` in repo root) — OpenAPI, backend engine, migrations,
  sqlc, protocol/scheduling, multi-module refactors.

## Android UI Preview And Screenshots

For Android Compose UI work, load the visual workflow before coding:

```text
apps/goatos-android/docs/ui-preview-and-screenshots.md
```

Use Showkase debug fixtures for fast mock-state review on device/emulator and
Paparazzi snapshots for committed PR regression evidence. Add fixtures for edge
cases before handoff: empty/loading/offline, duplicate/unknown/wrong-shed scans,
two RFID tags, multiple vaccines, proof missing/uploading/failed/synced, and
role-specific navigation. Production mobile copy must never leak internal terms
such as Room, outbox, idempotency, API, local, localhost, debug, or fixture.

When adding a new vertical that reuses Vaccination patterns, treat the latest
`origin/main` / staging Firebase Vaccination app as the regression baseline.
Do not change Vaccination day-strip rollover, shed cards, scan feed/list/bottom
sheets, proof capture/submit behavior, or backend read-model semantics as a
side effect of the new vertical. If a shared utility must change, prove
Vaccination parity with focused tests and screenshots before touching the new
vertical UI.

For physical-phone role and RFID scan testing, load this runbook before changing
Android scan handling, role gates, or local seed data:

```text
docs/runbooks/phone-qa-throwaway-rbac.md
```

That runbook is the source for the disposable `goatos-phone-qa` database,
the two-parks/two-sheds phone scenario, the five physical RFID mapping, and the
rule that Vaccination may transform input only in the local dev build while
Weighing remains raw free-flow.

**Weighing vocabulary (permanent rule):** weighing has exactly two verbs — CLOSE
a bucket, or REOPEN it if it is already closed. No third verb, and no force,
override or skip variant of close. The close gate is UNCONDITIONAL: a bucket
cannot close while verification is pending, and if it will not close the answer
is to resolve the verification, never to add a path around the gate. See
`AGENTS.md` → "Weighing vocabulary" and
`context/repo-audits/weighing-implementation-do-not-reopen-ledger.md` → D-5.

## STG Deploy Routing

When the user says "deploy STG", "push to STG", "promote STG", "ship to
staging", or similar:

- load `docs/runbooks/stg-deploy.md` (canonical contract) and
  `context/deploy-contract.json`
- follow `docs/runbooks/cloud-deploy-staging.md` for full Cloud Deploy mechanics
- deploy is **manual Google Cloud Deploy** from latest approved `origin/main`
- do NOT use generic GitHub/CI assumptions
- do NOT offer GitHub Actions or PR-driven deploy options
- do NOT force-push a `stg` branch
- verify `ravi@mesha.sg` / `vgoats.com` / `goatos-stg` before any cloud command
- every completed release must have a GitHub release tag from `make release-tag`
  with separate Backend, Frontend/Admin Web, Mobile Android, Infra/Deploy,
  Docs/Seed/Data, and Other sections
- Firebase App Distribution releases must first run
  `make restore-stg-android-release-env`, then record the Firebase release URL
  and Android version/code in the GitHub tag

## Context Files

Always start with:

```text
AGENTS.md
context/README.md
.agents/skills/goatos-build/SKILL.md
```

For any request to fix, continue, or close whole-project audit findings, load
the current queue before code changes:

```text
context/repo-audits/current-whole-project-remediation-ledger.md
```

That file is the current whole-project implementation queue and contains its
own closure gate. The historical `last-35-commits-consolidated-bug-ledger.md`
and `consolidated-ledger-defect-closure-program.md` remain authoritative only
for work that explicitly names one of their older, separate IDs.

The current ledger records its reviewed SHA. Before implementation, fetch fresh
`origin/main`, re-adjudicate the selected IDs and migration tail, and treat the
recorded SHA as evidence provenance rather than live status.

For generic task hierarchy, ownership, Today/My Tasks, or escalation work, also
load:

```text
context/execution/operational-task-kernel-remediation-plan.md
```

Then load only the needed reference:

```text
.agents/skills/goatos-build/references/repo-structure.md
.agents/skills/goatos-build/references/architecture.md
.agents/skills/goatos-build/references/forms-sop.md
.agents/skills/goatos-build/references/contracts-events.md
.agents/skills/goatos-build/references/frontend-mobile.md
.agents/skills/goatos-build/references/analytics-infra.md
.agents/skills/goatos-build/references/execution-plan.md
.agents/skills/goatos-build/references/phase-prd-trd.md
.agents/skills/goatos-build/references/existing-repos.md
.agents/skills/goatos-build/references/security-ops.md
```

## Phase Workflow

Before implementing any phase:

```text
1. Read docs/phases/README.md.
2. Read the active PRD/TRD listed there.
3. Load .agents/skills/goatos-build/SKILL.md.
4. Load the reference files relevant to the changed area.
5. If the phase introduces a new permanent rule, module, tool, API pattern, or
   workflow, update the relevant skill reference before coding.
6. Keep deep product truth in context/ and docs/phases/.
7. Keep skill references short routing/playbook files, not duplicate specs.
```

Current override:

```text
For Protocol Engine Phase 0 (Preventive Care (PC) vaccination, feed direction, protocol config,
obligations, inventory ledger), load .agents/skills/goatos-build/SKILL.md and
then docs/protocol-engine/* and docs/preventive-care-vaccination/*, including
docs/protocol-engine/high-scale-kernel-validation-plan.md for retry,
idempotency, outbox/Pub/Sub, sweeper, bounded-worker, and 1M-scale validation
work.
```

After implementing any phase:

```text
1. Compare actual code, migrations, contracts, tests, adapters, and workflows
   against the phase PRD/TRD.
2. Update PRD/TRD if implementation intentionally changed scope or behavior.
3. Update context/ if the change affects long-lived architecture/product truth.
4. Update .agents/skills/goatos-build/references/ if agents need new routing or
   rules for future work.
5. Update AGENTS.md only for new always-on repo rules.
6. Keep CLAUDE.md and CODEX.md as shims unless the agent tool itself requires
   a new shim.
7. Run guardrails and leave docs/code in sync before marking the phase done.
```

## Rules

- One skill source, no duplicate copies.
- `CLAUDE.md` and `CODEX.md` are shims to `AGENTS.md`.
- Skill references point back to `context/` and active phase docs.
- Admin-web/operator UI copy/options/navigation/table/filter/chip/drawer truth is
  backend-contract owned; see `AGENTS.md`, `apps/admin-web/AGENTS.md`, and
  `context/frontend/admin-web-backend-ui-contract.md` before frontend work.
- Mobile/frontend user-facing copy must never leak implementation/debug/test/
  roadmap language. CEO/director/operator UI should use business language only;
  keep `V1`, `Room`, `outbox`, `idempotency`, `backend`, `API`, `mock`,
  `localhost`, and similar technical words in docs/tests/logs, not screens. Load
  `.agents/skills/goatos-build/references/frontend-mobile.md` for the full copy
  firewall before touching Compose/React UI.
- Hooks call shared scripts in `tools/agent-hooks/`.
- CI is the hard gate; hooks are fast feedback.
- Do not create many skills up front. Add a new skill only when the trigger is
  truly independent from `goatos-build`.
- TELEMETRY GUARDRAIL: a new/changed Android screen/viewmodel or admin-web
  route must wire Analytics + Crashlytics + funnel step, checked by `make
  telemetry-guard` (`tools/telemetry-guard/`). See `AGENTS.md` and
  `docs/observability/TELEMETRY_GUARDRAILS.md`.

## Lens / anti-pattern skills (invokable by Claude + Codex)

These are **thin entrypoints** — a table of contents that routes to the canonical
detail. The detailed rules live in `.agents/skills/goatos-code-review/references/`
(the review chapters) and `docs/decisions/` (the decision records); the lens skills
link there and do NOT duplicate them. `goatos-code-review/SKILL.md` →
"Standalone lens skills — when each applies" is the full routing table. Invoke a
lens when its trigger matches:

| Lens skill | Invoke when | Canonical detail it fronts |
|---|---|---|
| `scale-anti-patterns` | writing/reviewing `backend/internal/**` query, worker, repo, SQL, hot-path/dashboard read, or any measured API/SSR hot load at/above 500ms | `references/kernel-and-scale.md`; `docs/decisions/scale-anti-patterns.md`, `operational-kernel-5k-50k-scale-envelope.md`, `high-scale-dashboard-projections.md` |
| `db-migration-safety` | a Postgres migration, hot-path query, read-model, or any mutating write path | `references/backend.md`, `references/aggregates-and-projections.md`; `docs/decisions/scale-anti-patterns.md`, `room-migration-safety.md`, `stale-binary-migration-drift-guard.md` |
| `kernel-scale-lens` | a trigger/obligation/reminder/sweeper/projection/notification/Calendar/AC/PA/process-integrity path | `context/architecture/operational-kernel.md`; `references/kernel-and-scale.md`; `docs/decisions/operational-kernel-5k-50k-scale-envelope.md`, `high-scale-dashboard-projections.md` |
| `frontend-anti-patterns` | an `apps/admin-web/**` page, SSR read, nav, label, dashboard, or slow page load/API contract selection | `references/frontend.md`, `references/mobile.md`; `docs/decisions/calendar-ownership.md`, `high-scale-dashboard-projections.md`, `mobile-data-fetch-anti-patterns.md` |
| `mobile-anti-patterns` | `apps/goatos-android/**` screen/route, list fetch, Room, offline, memory, lifecycle, phone-scale UI (windowing/chips/spinners) | `references/mobile.md`; `docs/decisions/mobile-data-fetch-anti-patterns.md`, `android-offline-first.md`, `room-migration-safety.md`, `android-navigation-stack.md`, `apps/goatos-android/docs/phone-scale-ui.md` |
| `nav-composition` | nav rendering, role/module gating, sidebar/bottom-bar composition | `references/frontend.md`; `docs/decisions/role-module-nav-composition.md` |
| `domain-event-architecture` | any backend/frontend/mobile CRUD/import/offline write, domain event, outbox producer/consumer, shifting, dead birth, feed direction, vaccination mutation, or future operational module | `context/architecture/domain-event-integration-contract.md`; `context/architecture/domain-event-registry.json`; `references/contracts-events.md`, `references/kernel-and-scale.md`, `references/frontend.md`, `references/mobile.md` |

Machine gates each lens names (`make scale-guard`, `validate-hot-index-migrations`,
`mobile-guard`, `admin-web-request-reads-guard`, `nav-composition-guard`,
`domain-event-architecture-guard`, `operational-read-model-contract-guard`, …) are
registered in `tools/ci/guardrail-manifest.json` and wired into `make guardrails`
/ `tools/ci/run-local-ci.sh`.
