# Vaccination Process Integrity Backend Handoff

Date: 2026-06-24

Purpose: backend source of truth for the current walking slice:

```text
Config defines what should happen
  -> SOP defines how it must happen
  -> obligations / SOP tasks / proof / verification record what actually happened
  -> process-integrity projection computes where the process is intact or broken
  -> Control Tower, Action Center, Protocol Adherence, and Workflow drilldowns read that same truth
```

This is **vaccination only** for the visible product slice. Shared engines may be
generic, but APIs, fixtures, docs, screenshots, and handoff language must not
present other verticals as built product.

## Architecture Rule: Generic Core, Vaccination Lens

Do not hard-code the whole platform around vaccination. The visible slice is
vaccination, but the backend foundation should remain reusable for later
modules.

```text
generic/reusable:
  protocol definitions, versions, rules, publish gates
  obligation instances, batches, status events
  SOP definitions, versions, tasks, submissions
  proof/media records and storage adapters
  verification/rework semantics
  process-integrity row shape: expected vs actual, gap, severity, owner, next action

vaccination-specific:
  vaccination rule/dose semantics
  vaccination_completion and booster/next-dose rules
  vaccination Action Center / Protocol Adherence / Control Tower APIs
  vaccination execution context projections
    (park/shed/stage/defer/blocker/owner context)
```

Future modules should add module-specific adapters/completions/projections on
top of the same engines. They should not require another SOP engine, proof
engine, obligation engine, or dashboard truth model.

## Forward Architecture Guardrails

The current process-integrity routes may still be vaccination-named while the
active slice is vaccination. That is acceptable only as the current module lens,
not as a reason to fork another command engine later.

```text
current slice:
  /action-center, /protocol-adherence, /workflows, and Control Tower read
  vaccination process-integrity rows by default.

future procurement lens:
  use the same process-integrity contracts/services with domain-aware filters
  such as ?domain=procurement.
  Do not build /procurement/source-entry/action-center, a second procurement
  Action Center engine, or a parallel procurement Protocol Adherence engine.
```

SOLID boundary rule:

```text
module-specific adapters produce rows/events
  -> shared process-integrity app/service computes command-lens read models
  -> top-level command screens select domain/scope/date through one contract
```

SOP/proof/verification must stay one platform engine. Large in-progress
SOP/proof changes must be audited as extensions of `backend/internal/sop`,
`backend/internal/proof`, and the existing obligation/vaccination bridges. Do
not create a second SOP runtime, second proof media path, or command-lens-only
submission model because procurement needs its own business rows.

Procurement handoff to vaccination must be proven before it is called wired:

```text
accepted intake
  -> durable procurement handoff row/event
  -> outbox/consumer or synchronous app service path reaches the vaccination
     obligation generator
  -> generated Preventive Care (PC) vaccination obligations/SOP tasks
  -> CT / AC / PA / WF / Vaccination read models reflect the same state
```

`procurement_pc_handoffs.event_status` is useful tracking, but it is not proof
by itself. A log line or queued row is not enough; tests must prove the handler
ran and the downstream vaccination read models changed.

## Read First

```text
AGENTS.md
SKILLS.md
context/README.md
context/frontend/current-admin-web-scope.md
context/execution/sop-vaccination-backend-handoff.md
context/forms/final-forms-sop-engine.md
.agents/skills/goatos-build/references/backend-impl.md
.agents/skills/goatos-build/references/forms-sop.md
.agents/skills/goatos-build/references/contracts-events.md
docs/protocol-engine/IMPLEMENTATION-PLAN.md
docs/protocol-engine/obligation-engine.md
docs/protocol-engine/state-machines.md
docs/preventive-care-vaccination/PRD.md
docs/preventive-care-vaccination/TRD.md
docs/preventive-care-vaccination/V1-FOUNDATION-SPEC.md
mock/goatos-dashboard-mock.html
```

Inspect current code/migrations before creating anything new. The repo already
has protocol, obligation, vaccination, parks, SOP, proof, inventory, workforce,
and location foundations in progress. Extend existing modules/contracts; do not
fork a parallel engine.

## Three Review Passes Already Done

### 1. Active GoatOS Docs And Code

Relevant active docs say the core chain is:

```text
published protocol_version/rule
  -> obligation_instances
  -> obligation_batches grouped by shed/cohort
  -> one sop_task pinned to sop_version_id
  -> sop_submission + proof_refs
  -> verification approve/reject/rework
  -> vaccination_completions
  -> obligation_status_events + outbox
  -> projections for Action Center / Control Tower / Passport / Calendar
```

Key docs:

```text
docs/protocol-engine/obligation-engine.md
docs/protocol-engine/state-machines.md
docs/preventive-care-vaccination/TRD.md
context/execution/sop-vaccination-backend-handoff.md
```

Current repo reality observed on 2026-06-24:

```text
built / in progress:
  protocol, obligation, vaccination, parks, SOP, proof, inventory, workforce,
  locations modules exist.
  /action-center/obligations exists as a bounded compatibility obligation API.
  execution read APIs are owned by Preventive Care (PC) / Vaccination at /vaccination/execution and
  /vaccination/execution/sheds/{shed_id}. The old Parks-owned execution paths are
  removed (now route_not_registered); do not reintroduce them.
  execution WorkState already includes due, overdue, scheduled, in_progress,
  proof_pending, verification_pending, rejected, deferred, blocked,
  owner_missing, completed.
  /admin/sops and SOP version/dry-run/publish/retire contracts exist.
  proof APIs exist with local backend-owned storage and GCS adapter selection.
  vaccination verification queue and accept/reject completion endpoints exist.
  SOP submission hooks bridge vaccination submission and verification fanout.

still pending:
  one coherent process-integrity read model that powers Action Center,
  Protocol Adherence, Control Tower, and Workflow drilldown.
  server-side Action Center filters for park/shed/severity/owner/work_state
  without frontend filtering after a capped page.
  Protocol Adherence API with expected vs actual rows.
  Control Tower API with only broken/at-risk vaccination gaps.
  workflow drilldown API from config -> obligation -> SOP -> proof -> verify -> completion.
  full E2E proof upload -> SOP submission -> verification -> completion -> booster basis.
```

Current backend/frontend integration note:

```text
By the later 2026-06-24 backend slice, the process-integrity APIs and generated
types exist. Do not rely on old "pending contracts" wording without checking the
current tree. The remaining closeout risk is live seeded E2E: real protocol/SOP
publish, obligation generation, SOP/proof execution, verification/rework, and
process-integrity projections all proving the same state.
```

### 2. Mock UI / Product Intent

`mock/goatos-dashboard-mock.html` has the correct mental model:

```text
Protocol Adherence:
  "Is the agreed process being followed?"
  Expected -> Actual -> Gap -> Severity -> Owner -> Next -> Evidence

Workflows:
  chain-reaction map from event/config to tasks, proof, verification, closure

Action Center:
  the work queue: due, overdue, verification, rework, owner/action

Control Tower:
  the rollup: process intact/not intact, critical gaps, owner, next action
```

Control Tower and Protocol Adherence are not the same screen:

```text
Protocol Adherence:
  detailed ledger of expected vs actual.
  includes on-track counts, visible deferred/explained work, evidence, and gap math.
  answers: "Did the rule/SOP happen as configured?"

Control Tower:
  exception-only command summary.
  hides normal/on-track work and shows broken or at-risk process with owner/next action.
  answers: "Is the process intact right now, and where must leadership intervene?"
```

Do **not** copy the mock as all-domain live product. Use its structure and
density, but content for this slice is vaccination process integrity only.

### 3. Wiki / Legacy / Source Evidence

Graphify/source checks found recurring source concepts, not new runtime scope:

```text
Mesha director/handbook graph:
  Director -> Manager -> Park Head ownership chain.

Visual graph:
  Video Verification Team, executive strategy dashboard, action/status UI.

Procurement/source graph:
  Procurement SOP Selection DB, Selection Decision, Video Verification Team.
  Useful as proof/verification semantics only. Do not build procurement now.
```

Legacy code reviewed for salvage only:

```text
/Users/ravi/mesha/procurement_app/src/constants/sop.ts
/Users/ravi/mesha/procurement_app/src/components/SOPOverlay.tsx
/Users/ravi/mesha/procurement_app/src/screens/SOPEditorScreen.tsx
  useful: step/sub-check structure, timed overlay, validation, dirty-state UX.

/Users/ravi/mesha/procurement_app/src/services/uploadQueue.ts
/Users/ravi/mesha/procurement_app/src/store/uploadQueueStore.ts
  useful: local queue, upload progress, retry/backoff, stuck-upload recovery.

/Users/ravi/mesha/slack-automation-scripts/unified_automation.js
  useful: verified/rejected/rework semantics and "all actions terminal +
  all verifiable actions reviewed" gating.

/Users/ravi/mesha/dashboard/app/api/vaccination/route.ts
  useful: legacy display columns only. Do not copy BigQuery or old UI.
```

Canonical replacements:

```text
Firestore/Firebase direct writes -> GoatOS backend APIs + Postgres + proof port
Slack/Sheets verification       -> proof records + verification workflow
BigQuery vaccination dashboard  -> Postgres process-integrity projection
old dashboard UI                -> mock-matching admin-web surfaces
```

## Non-Negotiable Product Invariant

The dashboard exists to prove that the configured process is being followed.

```text
Config answers: what is expected?
SOP answers: how must work be executed/proven?
Obligations/tasks/proofs answer: what happened?
Process integrity answers: where did expected != actual?
```

Frontend must not compute truth by guessing. Backend must expose one coherent
process-integrity model and derived summaries.

## Backend Target: Process Integrity Projection

Create or extend a backend read model that can power all four lenses. Name can
follow repo convention. Prefer a reusable conceptual row shape, with a
vaccination category/API lens for this slice:

```text
process_integrity
  category = vaccination
```

It may be an SQL view, materialized projection table, repository query, or
module read model. Pick the lowest-risk shape that fits current code. It must be
tenant-scoped, indexed/paginated, and testable.

If the lowest-risk first implementation is a vaccination-specific repository
projection, keep the field names and derivation rules generic enough that
future modules can reuse the same contract shape.

Required row fields:

```text
identity:
  tenant_id
  process_key / row_id
  obligation_id
  batch_id
  sop_task_id
  sop_submission_id
  completion_id

scope:
  park_id / park_name
  shed_id / shed_name
  cohort_id when relevant
  goat_id when per-goat
  animal_stage

configured expectation:
  protocol_id
  protocol_version_id
  rule_id
  dose_code / drive_name
  sop_version_id
  proof_policy
  due_at
  window_start
  window_end
  expected_count

actual execution:
  obligation_status
  batch_status
  sop_task_state
  submission_state
  proof_state
  verification_state
  completion_state
  completed_count
  proof_count
  rejected_count
  deferred_count

derived process state:
  work_state
  gap_type
  severity
  blocker_reason
  owner_state
  next_action
  process_intact boolean

ownership:
  operator_id / operator_name
  park_head_id / park_head_name
  verifier_id / verifier_name
  escalation_owner_id / escalation_owner_name

evidence:
  proof_ids
  evidence_count
  latest_evidence_at
  latest_rejection_reason
  audit_ref / trace_ref if available
```

Required work states:

```text
scheduled
due
overdue
in_progress
proof_pending
verification_pending
rejected
deferred
blocked
owner_missing
completed
```

Recommended derivation order:

```text
completed:
  accepted verification + vaccination_completion exists.

rejected:
  latest verification/review rejected or rework requested.

verification_pending:
  proof/submission exists and verify_before_apply is true, but no accepted/rejected review.

proof_pending:
  SOP task/submission exists or obligation is in progress, but required proof is missing/incomplete.

owner_missing:
  execution owner cannot be resolved from task assignment, shed owner, park owner, or capability scope.

blocked:
  stock-out, unpublished/missing SOP, missing proof policy, ICU/quarantine/sick defer state,
  expired lot, no verifier, or other explicit blocker.

deferred:
  obligation is intentionally deferred/waived with visible reason; never silently hidden.

in_progress:
  task/batch started and not terminal.

overdue:
  due window closed or due_at < now and no terminal completion/defer/cancel.

due:
  due now / due window open and no task terminal state.

scheduled:
  future due row exists.
```

Do not count `canceled`, `superseded`, death/sale exits, or unrelated
non-vaccination SOPs as actionable vaccination work.

## Backend APIs To Build Or Align

Inspect existing routes first. Extend existing endpoints if they already exist;
do not create duplicate products with different names.

### Action Center API

Purpose: exact work/gaps someone can act on now.

Required behavior:

```text
server-side filters:
  park_id
  shed_id
  work_state
  severity
  owner_id
  due_window
  protocol_version_id
  limit
  cursor/keyset

response:
  grouped rows by park/shed/drive/goat-or-cohort
  counts by work_state from the filtered query
  row fields from the process-integrity projection
```

No frontend-side filtering after a capped unfiltered page.

### Protocol Adherence API

Purpose: expected vs actual.

Required behavior:

```text
rows:
  expected
  actual
  gap
  severity
  owner
  next_action
  evidence

examples:
  "Enterotox booster K1-CBE: 17 due, 0 done, overdue"
  "PPR Yashoda 10: 88 due, scheduled, not started"
  "Enterotox ICU cohort: deferred with visible reason"
```

It must include deferred/explained obligations so "not skipped" is visible.

### Control Tower API

Purpose: only broken or at-risk vaccination process.

Required behavior:

```text
summary:
  process_intact boolean
  critical_count
  warning_count
  open_gap_count
  verification_backlog
  owner_missing_count
  config_or_sop_blockers

alerts:
  severity
  title
  detail
  park/shed/drive
  owner
  next_action
  evidence/state link
```

Do not return generic Inventory/Procurement/Team/Parks KPI rows in this slice.

### Workflow Drilldown API

Purpose: explain the chain for one drive/obligation/task.

Required behavior:

```text
nodes:
  config published
  obligation generated
  batch/drive opened
  SOP task started
  proof uploaded
  verification accepted/rejected
  completion posted
  booster/next-dose generated if applicable

each node:
  state
  timestamp
  actor/owner
  evidence/ref
  blocker/rejection reason when broken
```

This can initially be a detail endpoint behind Action Center rows rather than a
standalone product.

## SOP Execution / Proof / Completion Requirements

Before true E2E can pass:

```text
Start SOP task
Submit vaccination SOP fields
Attach backend-issued proof refs
Verify / reject / request rework
Approve -> create vaccination_completion
Reject -> keep actionable rework state
Completion -> update obligation status and booster/next-dose basis
```

Proof/media rules are in `context/execution/sop-vaccination-backend-handoff.md`:

```text
same backend proof API for local and prod
local adapter: backend-owned filesystem storage
prod adapter: GCS signed upload
frontend/mobile never writes directly to arbitrary disk/GCS
tests use temp dirs or fake storage, not real GCS
```

## Scale Rules

Every query must be:

```text
tenant-scoped
indexed
bounded by due window / state / scope
paginated with limit + cursor/keyset
safe for million-goat operations
covered by exact SQL/query-plan tests when hot-path or cross-table projection
```

Hot-path validations to add/update:

```text
process-integrity by work_state/due window
process-integrity by park/shed
Control Tower broken/at-risk query
Action Center counts by state
Protocol Adherence expected-vs-actual query
Workflow drilldown by obligation/batch/task
```

## Implementation Order

```text
0. Completed guardrail to keep enforced:
   - SOP proof_policy uses canonical subject_scope, not legacy scope.
   - SOP proof_policy authoring is photo/video only; do not reintroduce
     attachment as a policy type unless an executable attachment_proof DSL field
     exists.

1. Define generated API schemas for vaccination process-integrity rows,
   Action Center, Protocol Adherence, Control Tower summary, and workflow
   drilldown. Regenerate clients.

2. Build process-integrity repository/service over current tables.

3. Wire Action Center API with server-side filters and counts.

4. Wire Protocol Adherence API using the same read model.

5. Wire Control Tower summary API using only broken/at-risk vaccination gaps.

6. Close SOP execution/proof/media/verification/completion loop.

7. Add focused backend tests and query-plan coverage.

8. Run frontend/backend E2E against a seeded dev-real vaccination protocol/SOP.
```

## Seeded E2E Closure Scenario

Before vaccination can be called done, run a seeded local/dev scenario with real
backend APIs and real admin-web clicks. It must include enough data to exercise
every work state and screen, not only empty states.

For procured goats, also run the accepted-intake boundary plan:

```text
context/execution/procurement-vaccination-e2e-plan.md
```

That plan proves rejected/unresolved source goats stay out of Preventive Care (PC) / Parks while
accepted-intake goats become eligible for post-arrival vaccination.

Minimum seed shape:

```text
2 parks
several sheds
20-50 goats assigned across park/shed/stage
published vaccination protocol version with at least one dose and booster basis
published vaccination SOP version with photo/video proof policy
vaccine/batch/stock where the protocol requires it
obligations grouped into shed/cohort drives
SOP tasks linked to obligations/drives/goats
proof records through backend media/proof API
verification queue items
```

Branches to prove:

```text
due
overdue
scheduled
in_progress
proof_pending
verification_pending
rejected / rework
deferred
blocked
owner_missing
completed
```

End-to-end checks:

```text
publish config -> obligations generate
drive/session appears in Action Center and vaccination execution context
workflow drilldown shows config -> obligation -> SOP -> proof -> verify -> completion
proof upload uses backend proof API, not direct disk/GCS writes
verify approve creates vaccination_completion
reject/request rework keeps row actionable and outranks proof_pending
completion updates adherence and Control Tower
booster/next due uses accepted completion time
failed SOP submission fanout rows are visible through backend visibility endpoint
```

Local runtime discipline:

```text
restart the backend from current source before review
do not trust an already-ready stale :8080 binary
route_not_registered means stale route registry/runtime, not DB grants
```

## Done Means

Do not call vaccination done until this passes:

```text
published source-backed vaccination protocol exists
published vaccination SOP version exists
goat entry/eligibility creates obligation
obligation groups into shed/cohort drive
SOP task starts
proof record created through backend media/proof API
submission references proof
verifier approves/rejects
approved verification creates vaccination_completion
booster/next due uses accepted completion time
Action Center shows current work state
Protocol Adherence shows expected vs actual
Control Tower shows process intact or broken with owner and next action
Parks drilldown shows park/shed/stage/drive context
```
