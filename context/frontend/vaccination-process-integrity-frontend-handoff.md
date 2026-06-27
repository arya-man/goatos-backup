# Vaccination Process Integrity Frontend Handoff

Date: 2026-06-24

Purpose: frontend source of truth for reshaping the admin-web screens so they
match the mock's command-center/product shape while showing **vaccination only**.

This is not a generic Control Tower, not a generic Action Center, and not a
generic all-domain Workflow/SOP rollout.

The backend can be generic. The visible admin-web review surface for this build
must remain the PHC / Vaccination lens only.

## Platform Model (READ FIRST — this is NOT a vaccination app)

Goat OS is a **generic, multi-vertical OS**: a module-agnostic engine (protocol
rules, obligations, SOP tasks, proof, verification, process-integrity) →
**verticals** (e.g. PHC) → **modules** (Vaccination today; Feed Direction,
deworming, others next, on the **same engine**). **Vaccination is one module under
PHC — the current visible slice, not the app's identity.** "Vaccination-only" /
"vaccination lens" below means the current visible *content slice*; the engine,
shell, Config, and SOP layers stay generic. Never treat vaccination as an app-wide
scope (no global "Vaccination" badge/label); the active module is shown by the
sidebar nav + page crumb. Do not bake vaccination into generic layers; do not show
unbuilt modules as live.

## Read First

```text
AGENTS.md
SKILLS.md
context/README.md
context/frontend/current-admin-web-scope.md
context/execution/vaccination-process-integrity-backend-handoff.md
context/execution/sop-vaccination-backend-handoff.md
context/forms/final-forms-sop-engine.md
.agents/skills/goatos-build/references/frontend-mobile.md
.agents/skills/goatos-build/references/forms-sop.md
docs/protocol-engine/obligation-engine.md
docs/protocol-engine/state-machines.md
docs/phc-vaccination/PRD.md
docs/phc-vaccination/TRD.md
mock/goatos-dashboard-mock.html
```

## Non-Negotiable Visual Rule

`mock/goatos-dashboard-mock.html` is the UI/UX source of truth. Port the mock's:

```text
layout
screen hierarchy
sidebar/topbar density
card/table shape
chips/tags
icons
spacing
font scale
empty states
responsive behavior
```

Do not use old admin-web primitives or old dashboard layouts. Do not make a
second visual system.

## Non-Negotiable IA Rule

**Vaccination-only is data scope, not UI hierarchy. Mock hierarchy wins.**

Control Tower, Action Center, Protocol Adherence, and Workflows are **top-level
command-room screens**, placed exactly where the mock places them:

```text
/                    Control Tower
/action-center       Action Center
/protocol-adherence  Protocol Adherence
/workflows           Workflows  (+ /workflows/{row_id} drilldown)
/vaccination         PHC Vaccination module surface only
```

They are NOT tabs inside PHC / Vaccination. Filtering content to vaccination
data does not move these screens under a vertical. Do not rebuild an Action
Center / Adherence / Verification tab strip inside `/vaccination`. Do not keep
compatibility redirects such as `/vaccination/adherence` or
`/vaccination/workflows/{row}`; old nested command paths must disappear. Parks is
NOT a vaccination product route. There is no `/parks/vaccination` exception:
Vaccination execution is owned by PHC/Vaccination and must use `/vaccination`
plus `/vaccination/execution/sheds/[shedId]` for shed drilldown.

## Non-Negotiable Scope Rule

The content is vaccination-only:

```text
show:
  PHC / Vaccination process integrity
  Admin / Data Ops Config for vaccination rules
  Admin / Data Ops SOP Library for vaccination SOP policy
  Vaccination execution context scoped by park/shed

do not show as live product:
  Inventory
  Procurement
  Team
  Counts
  Breeding
  generic Health
  Feed
  Farmer Network
  HR
  generic Parks
```

The mock may contain all domains; this slice does not.

Current repo reality (IA reset landed 2026-06-24):

```text
top-level command screens (mock hierarchy):
  /                    Control Tower      — /control-tower/vaccination
  /action-center       Action Center      — /vaccination/action-center (+ verify queue)
  /protocol-adherence  Protocol Adherence — /vaccination/adherence backend API
  /workflows           Workflows          — list from /vaccination/action-center;
                       /workflows/{row_id} chain from /vaccination/workflows/{row_id}
                       backend API

verticals below:
  /vaccination         PHC Vaccination module surface: mock-faithful matrix,
                       cohort detail, drive/shed-event execution, proof +
                       verification backlog, rejected/rework — links out, not the
                       Action Center, no tab strip.
  /vaccination/execution/sheds/[shedId]
                       physical shed execution drilldown when needed.
  /config, /sops       Admin / Data Ops authoring + vaccination-only SOP Library.

still pending:
  execution UI for start SOP, proof upload, submit, verify/reject/rework when
  backend contracts are complete. Action/Adherence/Control Tower/Workflows all
  read real process-integrity contracts; rows populate once protocols publish.
```

## Current Frontend Review Notes

Latest review state on 2026-06-24:

```text
mostly fixed:
  Control Tower, Action Center, Protocol Adherence, and Workflows are top-level.
  /vaccination is PHC operations only.
  Action Center no longer owns a PHC tab strip.
  route_not_registered copy no longer blames DB grants.
  most repeated body scope bars were removed.

still not acceptable if regressed:
  /vaccination must not render an inline duplicate "All parks" park chip row in
  the body. Park/date scope must live in the top bar or Filters, not page body.
  Action Center's domain "All" chip must not silently clear the selected park
  scope. Domain/work-state/severity chips should preserve top-bar scope unless
  the user explicitly changes scope through Filters/topbar.

watch during E2E:
  the shell top bar currently derives park label from the ?park query value.
  Backend filters use park UUIDs, so the top bar must not display a raw UUID to
  operators. Either map UUID -> park display name/code or pass a safe
  display-only label from a trusted API response.
```

Hard scope chrome rule:

```text
Keep park/date scope in the top bar.
Remove repeated park/date/scope chips from page bodies.
If a page needs park filtering, put it behind Filters or update the top-bar
scope. Do not duplicate "Scope", "All parks", selected park, or date inline.
```

## Mental Model

The dashboard exists to prove whether the configured process is followed.

```text
Config = what should happen
SOP Library = how it must be done/proven
Workflows = the chain from config to completion
Protocol Adherence = expected vs actual
Action Center = what must be acted on now
Control Tower = is the process intact, where broken, owner, next action
Parks = physical park/shed/stage/drive context
```

All screens should read from the backend process-integrity model. The frontend
must not infer state by stitching together partial API responses unless the
backend contract explicitly says so.

## Control Tower vs Protocol Adherence

These are related but different lenses:

```text
Protocol Adherence:
  expected vs actual ledger.
  shows how rules + SOP proof performed against reality.
  can include on-track totals, deferred/explained rows, evidence, and gap math.
  user question: "Was the configured vaccination process followed?"

Control Tower:
  exception summary for leadership/operations.
  shows only broken or at-risk vaccination process.
  highlights severity, location, owner, and next action.
  user question: "Is the vaccination process intact right now, and what must be fixed?"
```

Action Center sits below both:

```text
Action Center:
  exact work queue.
  user question: "What does someone need to do now?"
```

## Screens To Shape

### `/` Control Tower

Use the mock Control Tower structure, but only vaccination alerts.

Must show:

```text
top summary:
  process intact / not intact
  critical gaps
  warning/at-risk gaps
  verification backlog
  owner missing

critical alert band:
  vaccination process gaps only
  severity tag
  owner
  next action
  park/shed/drive detail

open gaps table/list:
  gap
  severity
  detail
  owner
  next action
  link to Action Center / Protocol Adherence / Parks drilldown
```

Allowed examples:

```text
PPR drive overdue - CBE / K2 - owner missing - assign operator
Proof rejected - Yashoda 10 - verifier requested rework
Cold-chain proof missing - Gandhi 2 - request proof
No source-backed protocol published - Config action required
Vaccination SOP missing/published mismatch - SOP action required
```

Not allowed:

```text
Inventory 2-week floor breached
Procurement below buffer
Team SLA misses
generic Parks FCR drift
raw herd counts/census KPIs
```

### `/action-center` Action Center (top-level)

Use the mock Action Center board style, but columns/chips are vaccination work
states. This is a top-level command screen, not a PHC tab.

Required states:

```text
scheduled
due
overdue
in_progress
proof_pending
verification_pending
rejected / rework
deferred
blocked
owner_missing
completed
```

Each card/row must show:

```text
vaccine / dose / drive
park
shed
goat or cohort/count
due time/window
work state
severity
owner chain
blocker/rejection reason
proof status
verification status
next action
links to goat passport / parks drilldown / workflow detail
```

Actions must be real or honestly disabled with a reason. Do not fake success.

### `/protocol-adherence` Protocol Adherence (top-level)

This screen is not useless. It is the proof that config + SOP are being followed.
It is a top-level command screen, not a PHC tab.

Use the mock Protocol Adherence shape:

```text
header:
  "Is the agreed process being followed?"

summary:
  overall adherence
  open process gaps
  critical gaps
  on-track count

table:
  Expected
  Actual
  Gap
  Severity
  Owner
  Next action
  Evidence
```

Vaccination-only examples:

```text
Expected: Enterotox booster K1-CBE - 17 due
Actual: 0 done - untouched
Gap: overdue - skipped-silent
Severity: critical
Owner: Health Mgr / Park Head
Next: escalate / execute
Evidence: none
```

Deferred/explained rows must be visible, not hidden.

### `/workflows` Workflows (top-level)

Do not build a broad all-domain Workflow product. `/workflows` is a top-level
command screen that stays vaccination-only: a canonical chain template plus a
list of live vaccination workflow instances (from Action Center data). Each row
drills into `/workflows/{row_id}`, the chain-reaction map for one
obligation/drive. Control Tower, Action Center, and Adherence rows also link
into `/workflows/{row_id}`.

The workflow chain must show:

```text
config published
obligation generated
drive/batch opened
SOP task started
proof uploaded
verification accepted/rejected
vaccination completion posted
booster/next-dose generated when applicable
```

Use the mock's chain-reaction map style, but only for a selected vaccination
drive/obligation/task.

### `/vaccination` PHC Vaccination module surface

This is the PHC vertical's own Vaccination module surface — NOT the Action
Center. It must match the mock shape:

```text
header actions: SOP · Import sheet · New drive
Target -> Group -> Route -> Execute chain
Vaccination status matrix
Per-cohort vaccination detail
Drive/shed-event execution table
proof pending / verification pending / rejected-rework states as sections or rows
links out to Action Center, Protocol Adherence, and Workflows only where needed
```

It must not embed the Action Center board, the Adherence ledger, a Verification
tab, or any tab strip. Empty data keeps the mock sections visible with proper
empty states; it must not collapse into a generic KPI dashboard.

### Vaccination execution context

Park/shed execution context renders inside `/vaccination`, scoped by the top-bar
park dropdown. The read-model endpoint must be Vaccination-owned:
`/vaccination/execution`. Shed detail uses UI route
`/vaccination/execution/sheds/[shedId]` and backend API
`/vaccination/execution/sheds/{shed_id}`. Do not keep `/parks/vaccination`
routes, redirects, feature folders, or API contracts.

Physical context shown:

```text
park (from top bar scope)
shed
animal stage
defer/blocker state
owner chain
drive status
SOP status
proof status
verification status
next action
```

It is not generic Parks.

### `/sops`

Keep SOP Library vaccination-only:

```text
show vaccination.drive / vaccination.*
hide non-vaccination SOP inventory
domain chips may appear only as inactive/zero visual affordances
default new SOP = vaccination session/drive
```

Completed guardrails to preserve:

```text
proof_policy uses canonical subject_scope
SOP Library authoring exposes photo/video proof policies only
do not reintroduce attachment proof policy unless backend adds attachment_proof DSL support
```

## Data Wiring Rules

Use generated OpenAPI types and server-side fetchers in:

```text
apps/admin-web/lib/api/server.ts
```

No client-side mock rows. Empty states are allowed only when the real backend
returns no data. **Backend API failures must surface as visible error states**
(alert band, error card), never be swallowed into empty arrays that read as "no data".
Empty-but-OK keeps sections visible with zero-count badges + empty copy.

Empty states should still match mock density and explain the actual blocker:

```text
no source-backed protocol published
no vaccination SOP published
no obligations generated yet
no proof submitted yet
no verification queue items yet
```

Do not hand-write DTOs unless unavoidable; if generated types are missing,
report backend/API contract gaps.

## Backend Dependencies To Ask For

Frontend should expect or request these backend contracts:

```text
VaccinationProcessIntegrityRow
VaccinationActionCenterResponse
VaccinationAdherenceResponse
VaccinationControlTowerResponse
VaccinationWorkflowDrilldownResponse
```

Each should include:

```text
park/shed/goat-or-cohort context
protocol/rule/SOP identifiers
due window
work_state
gap_type
severity
owner chain
blocker/rejection reason
proof status
verification status
next_action
evidence refs
counts by state where needed
cursor/limit metadata
```

## Implementation Order

```text
1. Preserve SOP Library guardrails:
   - keep proof_policy on canonical subject_scope
   - keep authorable proof policies limited to photo/video

2. Keep Action Center at top-level `/action-center` (mock board, vaccination
   rows). Do not nest it under `/vaccination`.

3. Keep Protocol Adherence at top-level `/protocol-adherence` (mock
   Expected/Actual/Gap table). Do not nest it under `/vaccination`.

4. Keep `/` Control Tower as the mock-style vaccination alert summary, linking
   out to the command screens.

5. Keep Workflows at top-level `/workflows` (chain template + live list) with the
   `/workflows/{row_id}` drilldown; Control Tower / Action Center / Adherence rows
   link into it.

6. Keep `/vaccination` as the PHC Vaccination module surface only (no tab strip),
   matching the mock's vaccination screen structure.

7. Keep park/shed execution as Vaccination-owned physical context inside
   `/vaccination`; use `/vaccination/execution/sheds/[shedId]` for deep detail.

8. Wire execution actions only when backend endpoints are real:
   start SOP, submit proof, verify/reject/rework.
```

## Validation

Required gates:

```bash
npm --prefix apps/admin-web run typecheck
npm --prefix apps/admin-web run lint
npm --prefix apps/admin-web run check:mock-fidelity
npm --prefix apps/admin-web run build
git diff --check
```

Visual verification:

```text
Open mock:
  mock/goatos-dashboard-mock.html

Compare:
  Control Tower
  Action Center
  Protocol Adherence
  Workflows
  SOP Library

Open implemented:
  /
  /action-center
  /protocol-adherence
  /workflows
  /vaccination
  /sops
  /vaccination

Check desktop and narrow/mobile:
  no clipping
  no horizontal overflow
  buttons/cards text fits
  mock density preserved
  vaccination-only content
```

## Full Frontend E2E / Click QA Checklist

The next frontend closure session must test every reachable vaccination surface
with a fresh backend process and real seeded data. Do not call the frontend done
from empty states alone.

For procured goats, include the accepted-intake boundary plan:

```text
context/execution/procurement-vaccination-e2e-plan.md
```

This confirms PHC/Parks screens show accepted-intake goats only, while
rejected-before-truck, owner-missing, and extra-unknown source goats remain in
procurement surfaces.

Preflight:

```text
hard-restart the local backend/admin stack from current source
confirm backend route smoke returns 200/expected auth for:
  /control-tower/vaccination
  /vaccination/action-center
  /protocol-adherence
  /workflows
  /workflows/{row_id}
  /vaccination/execution
confirm admin-web is not serving a cached/stale build
```

Click/visual coverage:

```text
/                         Control Tower
/action-center            Status board, SOP queues, filters, work-state cards
/protocol-adherence       KPI row, severity filters, Expected/Actual/Gap table
/workflows                KPI tiles, domain chips, catalog, chain map
/workflows/{row_id}       workflow nodes, links to goat/passport/action
/vaccination              mock-faithful Vaccination module screen
/vaccination/execution/sheds/[shedId] shed drilldown
/config?category=vaccination vaccination protocol config/publish path
/sops                     vaccination-only SOP Library
/sops New SOP modal       actual builder, defaults, proof policy, save/dry-run
/goats/{goat_id}          contextual passport from a work row
```

For every clickable control, verify one of these is true:

```text
calls a real generated backend API
navigates to a real route
submits a real server action
is visibly disabled with an honest reason
```

Required UI checks:

```text
no repeated park/date/scope chips in page bodies
top bar shows the selected park name/code, not a raw UUID
desktop and narrow screenshots match mock structure/density
empty states are honest and compact, not giant placeholder pages
no fake all-domain cards or old admin UI are visible
```

## Done Means

Frontend slice is ready for E2E only when:

```text
Control Tower is mock-shaped and vaccination-only
Action Center is mock-shaped and vaccination-only
Protocol Adherence proves expected vs actual for vaccination
Workflow drilldown explains config -> obligation -> SOP -> proof -> verify -> completion
Vaccination execution context links to the same work rows
SOP Library stays vaccination-only
All data comes from backend APIs/generated types
No fake all-domain cards or old admin UI are visible
```
