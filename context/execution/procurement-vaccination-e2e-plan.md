# Procurement Source Entry -> Vaccination E2E Plan

Date: 2026-06-24

Purpose: define the seeded end-to-end proof before adding more CRUD or calling
procurement/source-entry plus vaccination complete.

This plan is the bridge between:

```text
context/execution/procurement-source-entry-backend-handoff.md
context/frontend/procurement-source-entry-frontend-handoff.md
context/execution/vaccination-process-integrity-backend-handoff.md
context/frontend/vaccination-process-integrity-frontend-handoff.md
```

## Source Evidence Checked

Procurement is a separate source-entry vertical, not a sub-tab inside PHC
Vaccination.

Evidence checked through Graphify and source exports:

```text
/Users/ravi/mesha/wiki/graphify-out/converted/Procurement DB [Goats]_dda03a25.md
  purchase/load, vendor/source, moved-to-farm, tag status, transport, transit,
  unloaded, selection, problems, and historical vaccination evidence.

/Users/ravi/mesha/wiki/graphify-out/converted/RFID source of truth_9d32525c.md
  RFID, old ID, farm, shed, tag, breed, age, and partition identity evidence.
```

The resulting product boundary is:

```text
Procurement/source-entry:
  purchase/source/holding warmup, source health/tagging, pre-dispatch decision,
  truck loading, transit, arrival gate, discrepancy review, accepted intake.

PHC Vaccination:
  consumes accepted-intake truth only: goat identity, park/shed, entry/intake
  date, defer signal, and trusted historical vaccination evidence.
  
Vaccination execution context (inside /vaccination):
  shows physical execution context (park/shed/stage/defer/blocker/owner) only
  after accepted intake places the goat into park/shed context. NOT a separate
  Parks module or route.
```

## Non-Negotiable Invariant

These goats must never appear as active PHC vaccination or vaccination execution
work:

```text
source-only/candidate
rejected before purchase/load
rejected before truck
pre-dispatch deferred
blocked missing proof
blocked owner missing
identity conflict
not loaded
missing from load
extra unknown goat
arrival rejected
arrival health/weight blocked
dead / sold / lost
ownership unresolved
```

Only accepted-intake goats may trigger post-arrival PHC vaccination work.

## E2E Comes Before CRUD

Do not add broad goat/shed CRUD first. Seed and assert the full flow first.
Then add only the CRUD/buttons that the flow proves are necessary.

## Local Architecture Gate Before Visual E2E

Do not start seeded screenshot/Playwright E2E until the local laptop stack proves
the same business chain that production will run. API + frontend + seeded rows
is not enough, because that only proves the UI can read rows. An outbox relay
that only logs is also not enough, because logging is not delivery.

For this flow, Postgres is the source of truth. Do not add Redis unless a real
cache/lock use case is implemented and documented; Redis is not required for
the current vaccination/source-entry chain.

Before visual E2E, prove the local equivalent of production:

```text
local Postgres container/migrations/seed
  -> API runs from current source
  -> accepted source goat writes goat.created / intake event into the outbox
  -> local outbox relay processes the event
  -> local consumer/event dispatcher delivers it to the registered handler
  -> handler generates vaccination obligations
  -> sweeper runner creates shed drive / obligation batch / SOP task
  -> SOP proof + verification/completion updates obligation/completion status
  -> CT / AC / PA / WF / Vaccination read the updated Postgres state
```

Required local architecture checks:

```text
1. One command or documented sequence starts local Postgres, API, admin-web,
   local outbox relay, local consumer/event dispatcher, and sweeper runner.
2. The local outbox relay must actually deliver to the event handler path. A
   logging-only publisher is not acceptable for E2E readiness.
3. goat.created / accepted-intake generation handlers must be registered in the
   runtime used by local E2E, not only present in package tests.
4. The sweeper must have a runnable local command or supervised loop. A service
   type without cmd/sweeper wiring is not enough.
5. The proof path must use the backend proof/SOP APIs or an explicitly
   documented local storage equivalent, not React fixtures or direct DB writes.
6. The smoke must assert downstream reads, not just writes:
   Control Tower, Action Center, Protocol Adherence, Workflows, and
   /vaccination must all reflect the same accepted-intake goat and exclude
   rejected/unresolved source goats.
```

Only after this local architecture gate is green should the visual E2E section
below run. Seeded screenshots are the final presentation proof, not the first
proof of correctness.

Allowed initial seed mechanisms:

```text
preferred:
  real backend APIs and generated clients

allowed for bootstrap only:
  deterministic DB fixture/seed for tenant, grants, locations, shed, vaccine
  stock, protocol config, SOP version, and test users when no admin API exists

not allowed:
  frontend mock rows
  local fixtures inside React components
  direct browser writes to DB/GCS/local media
```

## Minimal Seed Shape

Use one deterministic scenario, small enough to debug and broad enough to prove
the business rules.

```text
tenant:
  e2e-proc-vaccination

users/grants:
  admin/operator/verifier grants for procurement, PHC vaccination, Parks,
  SOP/proof, and process-integrity routes.

locations:
  source/holding farm: SRC-HF-001
  destination park: CBE
  destination shed: CBE-K1

source load:
  E2E-SRC-001
  expected goats: 4
  warmup window: 45-70 days supported

vaccination foundation:
  source-backed vaccination protocol version
  vaccination SOP version with proof policy
  vaccine stock/batch if protocol requires it
```

### Seed Goats

Use four goats so every critical branch is visible.

| Fixture | Procurement path | Expected downstream |
| --- | --- | --- |
| `SRC-A-CLEAN` | source warmup -> source health pass -> pre-dispatch accepted -> dispatched with proof -> arrived -> accepted intake | Appears in PHC vaccination, Action Center, vaccination execution context, SOP/proof/verification/completion |
| `SRC-B-REJECT-BEFORE-TRUCK` | source health fail or pre-dispatch rejected | Procurement history/gap only; never PHC vaccination or vaccination execution work |
| `SRC-C-OWNER-MISSING` | source health pass but ownership unresolved or owner missing | Procurement Action Center/Control Tower only; no PHC vaccination or vaccination execution work |
| `SRC-D-EXTRA-UNKNOWN` | appears during arrival as extra unknown goat | Arrival Gate/identity review only; no accepted intake until resolved |

Optional fifth goat for a broader pass:

| Fixture | Procurement path | Expected downstream |
| --- | --- | --- |
| `SRC-E-DEFERRED-HEALTH` | pre-dispatch or arrival health defer | Procurement deferred row; PHC may only see a defer signal after accepted intake, not an active dose obligation |

## Backend E2E Assertions

Run these as API or integration assertions before relying on frontend clicks.

```text
1. Create source-entry load.
2. Add source goats.
3. Record source health:
   - clean goat passes
   - reject goat fails or is rejected
   - owner-missing goat remains blocked
4. Record pre-dispatch decisions:
   - clean goat accepted_for_truck
   - reject goat rejected_before_truck
   - owner-missing goat blocked_owner_missing
5. Dispatch load:
   - succeeds only with eligible accepted goats
   - fails if proof exists but zero eligible accepted goats
6. Arrival review:
   - clean goat can be arrival_accepted only after loaded/transit/resolved
   - extra unknown goat remains unresolved
7. Accept intake:
   - creates PHC handoff for clean goat only
   - does not auto-convert pending health, identity, or ownership truth
8. Generate/publish vaccination obligations:
   - clean goat becomes eligible
   - rejected, owner-missing, and extra-unknown goats stay excluded
9. Execute vaccination SOP/proof/verification:
   - proof uses backend proof API
   - verifier approve creates vaccination_completion
   - verifier reject/request rework stays actionable
10. Confirm projections:
   - top-level command screens with procurement selected show source-entry gaps
   - vaccination Action Center/PA/CT/Workflow show only accepted-intake work
   - vaccination execution context shows only accepted park/shed execution context
```

## Frontend E2E Click Plan

Run this only after the backend seed exists and generated contracts are current.

Open and click through:

```text
/procurement/source-entry
  Source Entry Board with load rows, warmup age, state chips, owner, proof, next
  action.

/procurement/source-entry/loads/{load_id}
  Load Detail with purchase -> holding -> source SOP -> pre-dispatch -> truck
  loading -> transit -> arrival -> accepted intake timeline.

/procurement/source-entry/arrival-gate/{load_id}
  Expected/loaded/arrived/matched/missing/extra counts, row-level review
  actions, accepted intake action.

/ 
  Control Tower top-level command screen; procurement data appears only when
  selected as a domain/filter/lens, not as a nested procurement route.

/action-center?domain=procurement
  Procurement work queue only, using backend read model.

/protocol-adherence?domain=procurement
  Expected vs actual table for source health, tagging, dispatch proof, transit
  proof, arrival review, accepted intake.

/workflows?domain=procurement
  Load-to-intake chain map.

/workflows/{row_id}?domain=procurement
  Procurement workflow drilldown through the top-level Workflow route.

/action-center
/protocol-adherence
/workflows
/vaccination
/sops
  Vaccination-only current slice must show only the accepted-intake goat and
  must not show rejected/unresolved source goats.
```

Every clickable control must do one of these:

```text
call a real generated backend API
navigate to a real route
submit a real server action
be visibly disabled with an honest reason
```

## Where CRUD Buttons Belong

Add CRUD only after seeded E2E confirms the required operator action. Put the
buttons in the operational surface where the action naturally happens.

| Action | Correct UI placement | Notes |
| --- | --- | --- |
| Create procurement load | `/procurement/source-entry` header as `New load` | Not PHC Vaccination, not Goat Passport |
| Add source/candidate goat | Load Detail as `Add source goat` | Creates procurement load-goat/source identity, not clean herd truth |
| Record source health | Load Detail row action or SOP task action | Uses source health SOP/proof path |
| Accept/reject/defer/block pre-dispatch | Pre-Dispatch section row actions | Rejection before truck stays procurement history |
| Submit truck/loading proof | Load Detail dispatch section | Uses backend proof/media API |
| Record arrival review | Arrival Gate row actions | Handles missing/extra/health/weight flags |
| Accept intake | Arrival Gate load-level action | Only after all selected goats are eligible |
| Assign park/shed | Arrival Gate/accepted intake picker | Do not build generic shed CRUD unless separately required |
| Create shed for test data | Seed/fixture or future Admin/Parks config | Do not block E2E on generic shed CRUD |
| Create vaccination work | Not a manual button | Generated from published protocol + accepted intake |

Global `Create Goat` is not part of this flow. A source goat is created through
procurement source-entry, and a PHC/Parks goat appears only after accepted
intake.

## Scope Chrome Rule

Keep park/date/source scope in the top bar or behind Filters. Do not repeat
`Scope`, `All parks`, source, or date chips inside page bodies.

Examples:

```text
good:
  top bar says All parks / as of date
  page body has work-state filters and command controls

good:
  page body has a Filters button that opens source/load/park/date criteria

bad:
  top bar says All parks and page body repeats All parks/date/source chips
```

## Visual QA Rules

Use the mock as the visual source of truth:

```text
mock/goatos-dashboard-mock.html
```

The implementation must match the mock's:

```text
left nav hierarchy
top scope bar
dense command-center spacing
segmented controls
chip rows
KPI cards
table density
workflow chain map
modal spacing
desktop and narrow behavior
```

`check:mock-fidelity` is only a guardrail. It is not visual proof. Capture and
inspect screenshots against the mock before handoff.

## Required Gates

Backend / seed closure:

```bash
go test ./internal/procurement/... ./internal/processintegrity/... ./internal/vaccination/... ./internal/sop/... ./internal/proof/... ./internal/permissions/...
npm --prefix tools/contract-validation run validate
make api-client-generate
make validate-migrations
make validate-sqlc-plans
git diff --check
if [ -n "$(git ls-files --others --exclude-standard)" ]; then git ls-files --others --exclude-standard -z | xargs -0 git add -N --; fi
git diff --check
```

Frontend / click closure:

```bash
npm --prefix apps/admin-web run typecheck
npm --prefix apps/admin-web run lint
npm --prefix apps/admin-web run check:mock-fidelity
npm --prefix apps/admin-web run build
npm --prefix apps/admin-web run smoke:visual:live
git diff --check
if [ -n "$(git ls-files --others --exclude-standard)" ]; then git ls-files --others --exclude-standard -z | xargs -0 git add -N --; fi
git diff --check
```

Runtime discipline:

```text
restart backend/admin-web from current source
do not trust an already-ready stale :8080 binary
route_not_registered means stale route registry/runtime, not DB grants
hard-refresh browser before visual review
```

## Done Means

This E2E is done only when:

```text
source-entry seed creates load and four branch goats
accepted-intake goat reaches PHC vaccination and vaccination execution context
rejected/unresolved/extra goats never appear in PHC vaccination or vaccination execution work
top-level Control Tower/Action Center/PA/Workflow show procurement gaps only
when procurement is selected
vaccination Action Center/PA/CT/Workflow show vaccination gaps only
SOP/proof/verification/completion loop is exercised
all UI controls are real API/navigation/actions or honestly disabled
screenshots prove mock structure/density on desktop and narrow
```

After this passes, build the missing CRUD/buttons from the placement table. Do
not add broad generic CRUD before this seeded proof.

## Minimal Backend Session Prompt

```text
Work in /Users/ravi/mesha/goatos. Backend/seed/E2E only.

Read AGENTS.md, context/README.md, and
context/execution/procurement-vaccination-e2e-plan.md.

Create the deterministic procurement-source-entry -> accepted-intake ->
vaccination E2E seed/check path from the plan. Use real backend APIs/generated
clients where possible, DB fixture only for bootstrap gaps. Prove accepted goats
reach PHC vaccination/Parks and rejected/unresolved/extra goats do not.

No frontend UI. Regenerate contracts/clients if changed, then run the required
backend gates from this plan. Leave generated clients in a clean, intentional
diff so frontend can consume them. Include the untracked-file whitespace check
before claiming gates are green.
```

## Minimal Frontend Session Prompt

```text
Work in /Users/ravi/mesha/goatos. Frontend/click QA only.

Read AGENTS.md, context/README.md,
context/frontend/procurement-source-entry-frontend-handoff.md, and
context/execution/procurement-vaccination-e2e-plan.md.

Using only generated backend types/APIs, audit and finish the existing
procurement scaffold under app/(admin)/procurement/source-entry/**,
features/procurement/**, and lib/api/procurement*. Do not rebuild a second UI.
Do not create or keep nested procurement Action Center, Protocol Adherence,
Control Tower, or Workflows page routes; use the existing top-level command
screens with `?domain=procurement`.

Click through the seeded E2E with real rows. Match the mock structure/density,
keep park/date/source scope in the top bar or Filters, and do not add global
goat/shed CRUD. Wire only the CRUD/buttons proven by the E2E placement table;
honestly disable anything deferred. Run the required frontend gates and attach
desktop+narrow visual notes. Include the untracked-file whitespace check before
claiming gates are green.
```
