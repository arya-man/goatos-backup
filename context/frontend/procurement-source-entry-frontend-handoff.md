# Procurement Source Entry Frontend Handoff

Date: 2026-06-24

Purpose: define the frontend slice for source-side goat entry, holding farm
warmup, pre-dispatch rejection, truck/transit, and arrival intake.

This is **not part of the current PHC Vaccination UI repair**. Procurement is a
separate source-entry vertical. Do not add procurement cards to the active
vaccination Control Tower/Action Center unless the view is explicitly using a
procurement lens such as `?domain=procurement`.

## Business Case: Goat Journey Starts at Purchase/Source

The goat journey can start before park arrival:

```text
purchase/source
  -> supplier / holding farm warmup (45-70 days, not a fixed 8-week cap)
  -> source-side tagging and warmup may happen before final accepted intake
  -> tagging + health SOP
  -> accept/reject before truck loading
  -> truck/transit
  -> arrival gate at main park
  -> accepted herd intake
```

Frontend must not teach users that "goat entry" starts only at park arrival.
Park arrival is only one gate in the full procurement/source-entry journey.

## Implementation Gate

Before building, verify backend OpenAPI contracts and generated client types
exist for procurement/source-entry. If those contracts are missing in the local
tree, stop after documenting the missing backend contract names. Do not create
fake rows, local fixtures, temporary route handlers, or UI-only data adapters to
make the screens appear complete.

As of this handoff, the backend slice is expected to expose these API
contracts. The read-model contracts are backend/API endpoints only; do not
mirror them as admin-web page routes:

```text
/procurement/source-entry/loads
/procurement/source-entry/loads/{load_id}
/procurement/source-entry/loads/{load_id}/goats
/procurement/source-entry/goats/{goat_id}/source-health
/procurement/source-entry/goats/{goat_id}/pre-dispatch-decision
/procurement/source-entry/loads/{load_id}/dispatch
/procurement/source-entry/loads/{load_id}/arrival-review
/procurement/source-entry/loads/{load_id}/accept-intake

FORBIDDEN nested command-room paths (use top-level filters instead):
/procurement/source-entry/action-center        → use /action-center?domain=procurement
/procurement/source-entry/protocol-adherence   → use /protocol-adherence?domain=procurement
/procurement/source-entry/control-tower        → use /?domain=procurement
/procurement/source-entry/workflows/{row_id}   → use /workflows/{row_id}?domain=procurement
```

When building this slice, keep the build order strict:

```text
generated client types
  -> typed API helpers
  -> screen data mapping
  -> render states from backend truth
  -> visual QA against mock density/structure
```

The frontend may route to procurement only after the backend can prove the same
rows through API responses.

The next milestone is seeded E2E/click QA from:

```text
context/execution/procurement-vaccination-e2e-plan.md
```

Do not build broad goat/shed CRUD before that seeded path proves which actions
are needed.

Sequence:

```text
1. Backend seed/API E2E proves the source-entry -> accepted-intake ->
   vaccination boundary.
2. Frontend builds/finishes procurement screens from real generated APIs.
3. Frontend click QA runs against the seeded data.
4. Only then add missing CRUD/buttons from the E2E placement table.
```

## Read First

```text
AGENTS.md
SKILLS.md
context/README.md
context/frontend/current-admin-web-scope.md
context/execution/procurement-source-entry-backend-handoff.md
context/execution/procurement-vaccination-e2e-plan.md
context/product/glossary.md
context/source-findings/drive-docs-findings.md
context/product/goat-os-feature-phases.md
mock/goatos-dashboard-mock.html
```

## Product Truth

The goat journey can start before park arrival:

```text
purchase/source
  -> supplier / holding farm warmup, sometimes 45-70 days
  -> source-side tagging/warmup may happen before final accepted intake
  -> tagging + health SOP
  -> accept/reject before truck loading
  -> truck/transit
  -> arrival gate at main park
  -> accepted herd intake
```

Frontend must not teach users that "goat entry" starts only at park arrival.
Park arrival is only one gate in the full procurement/source-entry journey.

## Current Built State

Do not misrepresent current product:

```text
built / visible today in the vaccination review slice:
  PHC Vaccination process-integrity screens
  Vaccination execution context
  Admin Config
  vaccination-only SOP Library

backend contracts now exist for procurement/source-entry:
  source-entry loads and load detail
  add source/candidate goat
  source health
  pre-dispatch decision
  dispatch proof
  arrival review
  accepted intake
  procurement Action Center / Protocol Adherence / Control Tower / Workflow
  read models

scaffolded in admin-web and must be audited/finished, not rebuilt:
  app/(admin)/procurement/source-entry/**
  features/procurement/**
  lib/api/procurement.ts
  lib/api/procurement-server.ts

still pending:
  seeded source-entry -> accepted-intake -> vaccination E2E with real rows
  populated-data visual QA on Source Entry, Load Detail, AC, PA, CT, Workflow
  CRUD/buttons proven by E2E and wired to real POST contracts
  pagination/cursor completion where backend exposes next_cursor
  disabled/honest states for any intentionally deferred write action
```

If the user is reviewing the current vaccination slice, keep procurement/source
entry out of the visible product except as explanatory copy where needed.

Rejected-before-truck, arrival-rejected, dead/sold/lost, ownership-blocked,
identity-conflict, source-only/candidate, rejected-before-purchase/load, and
unknown/extra-unresolved goats must remain procurement history/work. They must
not be rendered as PHC vaccination work or vaccination execution rows.

## UI/UX Source Of Truth

For admin-web, the visual source remains:

```text
mock/goatos-dashboard-mock.html
```

Port the mock's structure and density. Do not use old admin primitives or old
generic dashboard layouts. If the mock lacks an exact
procurement page, reuse its command-center patterns:

```text
Control Tower:
  exception summary only

Action Center:
  work cards grouped by state

Protocol Adherence:
  expected vs actual table

Workflows:
  chain map from source entry to intake

	SOP Library:
	  source health / dispatch / arrival SOP policies are future procurement SOP
	  scope and are OUT of the current E2E unless explicitly reopened
```

Do not invent a separate visual system.

## Scope Chrome Rule

Keep park/date scope in the top bar. Remove repeated park/date/scope chips from
page bodies. If a procurement page needs source, holding farm, load, park, or
date filtering, put it behind Filters or update the top-bar/shell scope instead
of repeating the same "Scope", "All parks", source, or date chips inline.

## IA

After verifying backend contracts and generated types in the local tree, preserve
procurement as its own vertical surface and finish the existing source-entry
routes. Do not bury it under PHC Vaccination and do not add a duplicate
procurement nav tree.

Expected admin-web routes:

```text
/procurement/source-entry
/procurement/source-entry/loads/{load_id}
/procurement/source-entry/arrival-gate/{load_id}
```

Forbidden admin-web routes:

```text
/procurement/source-entry/action-center
/procurement/source-entry/protocol-adherence
/procurement/source-entry/adherence
/procurement/source-entry/control-tower
/procurement/source-entry/workflows
/procurement/source-entry/workflows/{row_id}
```

The current top-level command screens can later filter to procurement data:

```text
/                         Control Tower top-level, with procurement filter/lens only
/action-center?domain=procurement
/protocol-adherence?domain=procurement
/workflows?domain=procurement
/workflows/{row_id}?domain=procurement
```

Current default Action Center / Protocol Adherence / Workflows stay
vaccination-only unless a procurement lens/filter is explicitly selected.

## Procurement Operational Screens To Finish

Every procurement operational screen below must preserve the mock's
command-center density while showing procurement/source-entry data only. The
current task is to audit and finish the existing procurement scaffold from real
generated contracts; do not create a second UI tree or mock-only duplicate. If
backend contracts are missing, show an honest blocked/empty state instead of
mock rows.

## Operational Screen Coverage Matrix

| Screen | Must cover | Must not do |
| --- | --- | --- |
| Source Entry Board | Loads by source/holding farm; warmup age; health/tag SOP state; pre-dispatch pending/accepted/rejected/deferred; owner; proof state; next action | Do not show accepted park vaccination work here |
| Candidate/source intake | Source-only tagged goats; warmup age; purchase/ownership pending; selection state; reject/defer/block reason | Do not create PHC/Parks rows before accepted intake |
| Load Detail | Full journey timeline: purchase -> holding -> source SOP -> pre-dispatch -> truck loading -> transit -> arrival gate -> accepted intake | Do not flatten the whole journey into one status pill |
| Pre-Dispatch Decision | Accept for truck, reject before truck, defer/block, reason, proof, audited actor/time | Do not treat rejection as a vaccination rejection |
| Arrival Gate | Expected vs loaded vs arrived counts; matched/missing/extra goats; identity/health/weight flags; media proof; accept-intake action | Do not let goats become clean herd rows before reconciliation |
| SOP Library procurement slice | Future source health, pre-dispatch, truck loading, transit handoff, arrival gate SOPs | Out of current E2E unless explicitly reopened; do not expose all-domain SOP catalog |
| PHC Vaccination screens | Read only accepted intake outputs: origin, entry/intake date, historical vaccination evidence, defer signal | Do not own source warmup, pre-dispatch rejection, or arrival discrepancy review |
| Parks screens | Show only goats accepted into park/shed context | Do not show rejected-before-truck goats as park work |

## Top-Level Command Lens Behavior

These are not procurement pages. They are behavior on the existing top-level
command screens when a procurement domain/filter/lens is selected. Do not create
nested procurement routes or nav entries for them.

| Top-level screen | Procurement lens behavior | Must not do |
| --- | --- | --- |
| Control Tower `/` | Exception-only procurement gaps: aged warmup, high rejection, missing proof, unresolved arrival mismatch, owner missing | Do not create `/procurement/source-entry/control-tower`; do not show normal loads or procurement inventory metrics as alerts |
| Action Center `/action-center?domain=procurement` | Due/overdue source SOP, missing proof, identity conflict, aged warmup, deferred, owner missing, arrival mismatch | Do not create `/procurement/source-entry/action-center`; do not mix generic all-domain cards into the current vaccination Action Center |
| Protocol Adherence `/protocol-adherence?domain=procurement` | Expected vs actual for source health, tagging, dispatch proof, transit proof, arrival review, accepted intake | Do not create `/procurement/source-entry/adherence` or `/procurement/source-entry/protocol-adherence`; do not show vague KPI cards without row-level evidence |
| Workflows `/workflows?domain=procurement` and `/workflows/{row_id}?domain=procurement` | Chain map from load creation to intake with blocked/current/completed steps | Do not create `/procurement/source-entry/workflows` or `/procurement/source-entry/workflows/{row_id}`; do not replace the mock workflow chain with a simple table |

### Source Entry Board

Shows loads/goats before main park intake:

```text
load/vendor/source
holding farm
warmup age/days
tagging status
health SOP status
pre-dispatch decision
blocked/rejected/deferred counts
next action
owner
proof status
```

Required states/chips:

```text
source warmup
source candidate
health pending
proof missing
identity conflict
rejected before purchase/load
pre-dispatch pending
accepted for truck
rejected before truck
deferred / blocked
in transit
arrival review
accepted intake
```

### Load Detail

Shows the full journey:

```text
purchase/load
source holding
source health/tagging SOP
pre-dispatch decision
truck loading
transit/handoff proof
arrival review
accepted intake
links to created canonical goats and downstream PHC obligations
```

Must include per-goat rows, not only load totals:

```text
goat/source tag
canonical goat match state
warmup days
health status
pre-dispatch decision
loaded?
arrived?
arrival review state
accepted intake?
downstream PHC/vaccination status link when accepted
```

### Pre-Dispatch Decision

Must support:

```text
accept for truck
reject before truck
defer
block pending proof/health/identity
reason
proof attachment/video/photo
audited actor/time
```

Rejected-before-truck goats must stay visible in procurement history but must
not look like park goats needing PHC vaccination work.

Decision outcomes:

```text
accepted_for_truck
rejected_before_truck
deferred_health
deferred_identity
blocked_missing_proof
blocked_owner_missing
```

### Arrival Gate

Shows:

```text
expected count
arrived count
matched goats
missing goats
extra/unknown goats
health flags
weight flags
media proof
discrepancy review
accepted intake action
```

Arrival outcomes:

```text
accepted_intake
accepted_with_estimate
blocked_identity
blocked_health
blocked_weight
missing_from_load
extra_unknown_goat
arrival_rejected
quarantine_or_defer
```

### Command Screens

When procurement/source-entry is opened, the existing top-level command screens
should gain a procurement lens, but only from backend process-integrity
contracts:

```text
Control Tower:
  only broken/at-risk procurement source-entry process.

Action Center:
  exact due/overdue/blocked/rejected/deferred/owner-missing work.

Protocol Adherence:
  expected vs actual evidence table for source health, dispatch, transit,
  arrival review, and accepted intake.

Workflows:
  load/goat timeline with proof and verification nodes.
```

If backend contracts are missing in a local session, these screens must not show
fake procurement rows.

Required backend contract readiness before building these lenses:

```text
Source Entry Board:
  list loads/rows by state, owner, warmup age, proof, next action.

Load Detail:
  timeline, per-goat rows, proof/review state, downstream handoff links.

Pre-Dispatch Decision:
  accept/reject/defer/block actions with reason, proof, and row_version.

Arrival Gate:
  expected/loaded/arrived/matched/missing/extra counts plus review actions.

Procurement command lenses:
  Action Center, Protocol Adherence, Control Tower, and Workflows must all read
  backend process-integrity/procurement read models, not frontend filters over
  a capped list.
```

## CRUD Placement

Build create/actions only where the E2E plan places them:

| Action | Correct UI placement |
| --- | --- |
| Create procurement load | `/procurement/source-entry` header as `New load` |
| Add source/candidate goat | Load Detail as `Add source goat` |
| Record source health | Load Detail row action or SOP task action |
| Accept/reject/defer/block pre-dispatch | Pre-Dispatch section row actions |
| Submit truck/loading proof | Load Detail dispatch section |
| Record arrival review | Arrival Gate row actions |
| Accept intake | Arrival Gate load-level action |
| Assign park/shed | Arrival Gate/accepted intake picker |

Do not add a global `Create Goat` shortcut for this flow. Do not build generic
shed CRUD unless a separate Admin/Parks contract and product need is opened.

## Empty/Error States

Use honest empty states:

```text
No procurement source-entry contracts are available yet.
No source-entry loads for this scope.
Backend route not registered: restart/use current backend, not DB grants.
```

Do not show fake all-domain procurement cards just to make the screen look full.

## Relationship To Vaccination UI

Vaccination screens may show:

```text
origin: procured
entry date / accepted intake date
post-arrival vaccination due
intake health defer signal
historical vaccination-at-procurement evidence
```

Vaccination screens must not own:

```text
source warmup timeline
pre-dispatch rejection
truck/transit proof
arrival discrepancy review
vendor/source economics
```

Parks screens must not show a procured goat until accepted intake has assigned
it into a park/shed context. Rejected-before-truck goats remain visible only
through procurement history/detail surfaces.

## Acceptance

Do not call frontend done until:

```text
visible IA makes clear goat journey starts at purchase/source
45-70 day source warmup does not look anomalous or invalid
pre-dispatch rejected goats are visible as procurement history, not PHC work
arrival gate is a distinct checkpoint before accepted herd intake
all data comes from generated backend contracts
mock visual density/spacing/tables/cards are ported
desktop and narrow screenshots are visually inspected against the mock
seeded E2E proves accepted goats reach PHC/Parks and rejected/unresolved goats
stay out
typecheck, lint, check:mock-fidelity, build, git diff --check, and untracked-file
whitespace checks pass
```
