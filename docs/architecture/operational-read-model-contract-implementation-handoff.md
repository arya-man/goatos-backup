# Operational Read Model Contract Implementation Handoff

Status: ready for implementation  
Context: follow-up to `docs/architecture/operational-read-model-contract.md`  
Goal: close the current PA / CT / Calendar / mobile contract drift before adding more verticals

## Why This Exists

The operational read model contract is now documented and wired into agent/CI
discoverability. The current worktree still has implementation gaps that violate
that contract. Fix these before treating the architecture as enforced.

This handoff is intentionally concrete so another Codex/Claude session can pick
one defect at a time and implement with tests.

## Counter-Review Disposition

This section records the Claude counter-feedback review. Do not implement every
claim blindly; use the disposition below.

Accepted as valid:

- Protocol Adherence selected-drive scoping is unsafe because it keys on
  `RuleID + DueAt` and can merge different park/shed/partition/drive grains.
- The operational read model guard is currently discoverability-only. Its
  self-test is too weak and it must not be described as semantic enforcement.
- Android Calendar DTOs contain date fields that the Calendar backend/OpenAPI
  `DriveSummary` and `CalendarEvent` contracts do not emit.
- Android `AdherenceRowDto` is missing consumed OpenAPI fields, including
  `shed_name`, `partition_label`, and drive capacity detail fields.
- Android Control Tower DTO is missing `partition_label`.
- Android shed/day UI totals are computed from fetched execution rows and can be
  page-local when more execution pages exist.
- Control Tower drawer parses `detail` text instead of rendering backend-owned
  structured scope/proof fields.
- Per-shed proof/submit state must not be borrowed from a shared parent task
  state for per-goat proof mode.
- There is no single cross-surface golden fixture proving Calendar, Action
  Center, Protocol Adherence, Control Tower, Workflows, Admin Web, Android, and
  generated clients agree on the same seed data.

Qualified, not accepted as stated:

- "CT/AC/PA vaccination contracts are absent from admin-api" is not a blanket
  defect. Vaccination Process Integrity schemas and admin-web types are owned by
  `contracts/openapi/app-api.yaml` and generated app-api types in
  `apps/admin-web/lib/api/server.ts`. Procurement process-integrity schemas live
  in `contracts/openapi/admin-api.yaml`. Before changing schemas, verify the
  intended contract owner for the surface/domain.
- "Protocol Adherence summary is fixed because the service drains all pages" is
  not compliant. Draining pages makes summaries whole-result by deleting
  endpoint pagination and creating an unbounded service read. Keep this as a
  defect.

## Current Implementation Review

These findings were re-reviewed after the contract doc was added. They are
still open code defects unless a later implementation commit changes the cited
files.

Latest review snapshot:

- P1 `backend/internal/processintegrity/app/service.go`: `ProtocolAdherence`
  still drains all repository pages into `allRows` and still returns
  `NextCursor: nil`. This removes endpoint pagination instead of returning
  paged rows plus a whole-result summary.
- P1 `backend/internal/processintegrity/app/service.go`: selected-drive scope
  still uses only `RuleID + DueAt`, so repeated rules, multi-shed/partition
  drives, batches, and protocol versions can bleed together or drop rows.
- P1 `apps/admin-web/features/control-tower/control-tower-local-drawer.tsx`:
  Control Tower still derives evidence/scope copy from `work_state` and
  `detail.split(":")`, so the frontend is inventing proof/scope semantics.
- P1 `apps/admin-web/lib/admin-ui-contract.ts`: `SHARED_TABLE_FALLBACKS` still
  exists and can still hide missing backend table contracts.
- P2
  `apps/goatos-android/core/core-network/src/main/kotlin/sg/mesha/goatos/core/network/dto/ControlTowerDto.kt`:
  `ControlTowerAlertDto` still lacks `partition_label` while backend/OpenAPI/TS
  include it.
- P2 `tools/agent-hooks/check-operational-read-model-contract.mjs`: the guard
  still checks only doc tokens and references; it does not enforce
  Go/OpenAPI/TS/Kotlin/admin-web drift.
- P1 these files are still untracked and must be committed with the Makefile/CI
  references:
  `docs/architecture/operational-read-model-contract.md`,
  `docs/architecture/operational-read-model-contract-implementation-handoff.md`,
  and `tools/agent-hooks/check-operational-read-model-contract.mjs`.

Verification note: `make operational-read-model-contract-guard` and
`make guardrail-registration-guard` passing only proves discoverability and
registration. It does not close any finding in this section.

### P1: Protocol Adherence Still Removes Pagination

File:

```text
backend/internal/processintegrity/app/service.go
```

Review finding:

`ProtocolAdherence` still drains every repository page into `allRows`, then
returns `NextCursor: nil`. That removes API pagination instead of keeping
paginated rows plus a whole-result summary. The endpoint can become unbounded
and clients lose cursor behavior.

Implementation rule:

Do not accept a fix that merely increases the page size or drains pages in a
different layer. Rows must stay paginated; summary and total count must be
computed independently at the full selected scope.

### P1: Protocol Adherence Selected-Drive Scope Is Still Unstable

File:

```text
backend/internal/processintegrity/app/service.go
```

Review finding:

The selected-drive helper still scopes by `RuleID + DueAt`, then filters only
that pair. That is not a stable drive identity. Recurring, multi-shed,
partitioned, multi-batch, or protocol-versioned cases can bleed together or
drop valid rows.

Implementation rule:

Selected-drive identity must use a true drive/batch/assignment identity where
available, or a documented tuple containing every discriminating dimension the
producer distinguishes: tenant, park, shed, partition, batch/drive/assignment,
business/planned date, protocol version, and rule/lane.

### P1: Control Tower Drawer Still Interprets Semantics In React

File:

```text
apps/admin-web/features/control-tower/control-tower-local-drawer.tsx
```

Review finding:

The drawer still derives evidence/scope semantics from `work_state` and
`detail.split(":")`. This violates the "Control Tower is a surface, not a
source" contract. React must render backend-owned typed fields; it must not
infer proof/scope meaning from display copy.

Implementation rule:

Backend/OpenAPI/generated clients must own fields such as `scope_label`,
`evidence_summary`, `proof_summary`, `proof_state`, `verification_state`, or
equivalent. Admin Web renders those fields and includes `partition_label` when
present.

### P1: Admin UI Table Fallbacks Still Mask Missing Backend Contracts

File:

```text
apps/admin-web/lib/admin-ui-contract.ts
```

Review finding:

Shared table fallbacks still provide frontend-owned visible table titles,
columns, and data sources, and the table lookup can use them when a backend page
contract is missing. That can false-green stale or absent backend UI contracts.

Implementation rule:

Backend-owned command lenses and passport/detail tables must fail closed when a
required page table contract is missing. Any compatibility fallback must be
explicitly owned, narrowly scoped, and have an expiry/removal path.

### P2: Android Control Tower DTO Is Still Contract-Stale

File:

```text
apps/goatos-android/core/core-network/src/main/kotlin/sg/mesha/goatos/core/network/dto/ControlTowerDto.kt
```

Review finding:

`ControlTowerAlertDto` still omits `partition_label` while OpenAPI and generated
TypeScript include it. Android remains stale against the shared contract.

Implementation rule:

Add `partitionLabel` with `@SerialName("partition_label")`, then add a decode
test proving both present and absent optional values are handled.

### P2: Operational Read Model Guard Still Does Not Enforce Drift

File:

```text
tools/agent-hooks/check-operational-read-model-contract.mjs
```

Review finding:

The guard still checks doc tokens and reference links only. It does not catch
Go/OpenAPI/TS/Kotlin drift, frontend fallbacks, Android DTO omissions, or
page-independent summary violations.

Implementation rule:

Either keep the guard honestly described as discoverability-only, or add real
structural checks. Passing this guard must never be reported as proof that the
operational read model contract is semantically enforced.

### P1: Contract Files Are Still Untracked

Files:

```text
docs/architecture/operational-read-model-contract.md
docs/architecture/operational-read-model-contract-implementation-handoff.md
tools/agent-hooks/check-operational-read-model-contract.mjs
```

Review finding:

These files are referenced by Makefile/CI but are still untracked in the current
worktree. A fresh checkout will fail if the Makefile/CI references land without
these files.

Implementation rule:

Stage and commit all three files with the Makefile/CI references. Re-run
`make operational-read-model-contract-guard` and
`make guardrail-registration-guard` after staging.

## Findings To Fix

### 1. Protocol Adherence Deletes Pagination

File:

```text
backend/internal/processintegrity/app/service.go
```

Current problem:

`ProtocolAdherence` drains every repository page into memory, filters/scopes the
full slice, returns all rows, and sets `NextCursor: nil`. This fixes page-local
summary drift by removing endpoint pagination, which violates the contract:
summary aggregation and row pagination must stay separate.

Required fix:

- Keep row pagination for `ProtocolAdherenceResponse.Rows`.
- Compute `Summary` from a whole-result aggregate at the selected-drive scope.
- Return `TotalCount` for the selected drive, not only the returned page.
- Preserve `NextCursor` for remaining selected-drive rows.
- Do not fetch unbounded rows in service memory.

Acceptance tests:

- A drive with more than one page returns page 1 rows plus non-nil `NextCursor`.
- The summary counts all selected-drive rows, not only page 1.
- Page 2 returns the next selected-drive rows.
- Memory behavior is bounded by page size plus aggregate query, not all rows.

### 2. Protocol Adherence Drive Scope Is Not Stable Enough

File:

```text
backend/internal/processintegrity/app/service.go
```

Current problem:

Selected-drive scoping uses `RuleID + DueAt`. That is better than `RuleID` alone,
but still not stable enough for recurring or multi-drive work across park, shed,
partition, protocol version, batch, operator assignment, or planned-date
boundaries.

Required fix:

- Define a stable selected-drive identity for Protocol Adherence.
- Scope selected-drive rows by enough fields to prevent cross-drive bleed.
- Prefer a true drive/batch/assignment identity where available.
- If a tuple is required, include the dimensions documented in
  `operational-read-model-contract.md`: tenant, park/shed/partition, business
  date or assignment planned date, batch/drive, protocol version, and rule.
- Avoid string parsing or label matching.

Acceptance tests:

- Same rule, same due date, different shed/partition: no mixing.
- Same rule, different dates: no mixing.
- Same rule, same date, different batch/drive/assignment: no mixing.
- Same selected drive across multiple pages: pagination remains stable.

### 3. Control Tower Evidence/Scope Is Interpreted In React

File:

```text
apps/admin-web/features/control-tower/control-tower-local-drawer.tsx
```

Current problem:

`evidenceSummary` derives proof/scope meaning from `alert.work_state` and
`alert.detail.split(":")[0]`. This makes Control Tower a private interpreter of
proof/scope semantics and ignores structured fields such as `partition_label`.

Required fix:

- Move evidence/scope summary semantics to backend-owned structured fields.
- Add explicit fields to `ControlTowerAlert` / OpenAPI / generated clients as
  needed, for example:
  - `scope_label`
  - `evidence_summary`
  - `proof_summary`
  - `proof_state`
  - `verification_state`
- Admin Web should render those fields, not parse `detail`.
- Include `partition_label` in displayed scope where relevant.

Acceptance tests:

- Frontend source no longer splits `alert.detail` to derive scope.
- Control Tower drawer renders backend-provided evidence/scope copy.
- Partitioned shed alert displays partition scope.
- Missing backend field fails closed or uses an explicitly optional fallback
  approved by contract.

### 4. Admin UI Table Fallbacks Mask Missing Backend Contracts

File:

```text
apps/admin-web/lib/admin-ui-contract.ts
```

Current problem:

Shared table fallbacks provide visible table titles, columns, and data sources
when the backend page contract is missing a table. This can hide backend contract
drift instead of failing closed.

Required fix:

- Remove global visible shared table fallbacks for backend-owned tables.
- Keep only explicitly approved compatibility fallbacks, if any, and document
  their owner/expiry.
- Missing page table contracts should throw/fail closed.
- Tests should prove missing table contracts do not silently render invented
  columns.

Acceptance tests:

- Missing backend table contract throws.
- Existing backend-provided table contract renders normally.
- No shared fallback provides visible table title/columns/data source for
  command-lens or passport tables unless explicitly exempted.

### 5. Android Control Tower DTO Is Stale

File:

```text
apps/goatos-android/core/core-network/src/main/kotlin/sg/mesha/goatos/core/network/dto/ControlTowerDto.kt
```

Current problem:

OpenAPI and generated TypeScript now include `ControlTowerAlert.partition_label`,
but Android `ControlTowerAlertDto` does not.

Required fix:

- Add `@SerialName("partition_label") val partitionLabel: String? = null`.
- Update any mobile mapping/display code that should use partition scope.
- Add decode test for Control Tower alert with `partition_label`.

Acceptance tests:

- Kotlin serialization decodes `partition_label`.
- Existing responses without `partition_label` still decode.
- Any rendered scope that includes partition uses the DTO field instead of
  parsing labels.

### 6. Operational Read Model Guard Is Only Discoverability

File:

```text
tools/agent-hooks/check-operational-read-model-contract.mjs
```

Current problem:

The guard is useful, but its name and CI placement can imply semantic
enforcement. It only checks that the doc exists and is referenced. Its self-test
does not exercise `validate`.

Required fix:

- Either rename/scope the guard clearly as discoverability-only, or extend it.
- At minimum, update messages/docs to say it is a discoverability guard.
- Make `--self-test` exercise `validate` with fake/missing docs and references.
- Do not claim this guard checks Go/OpenAPI/TS/Kotlin drift, frontend fallbacks,
  or page-independent summaries until it actually does.

Recommended follow-up guards:

- Go response struct vs OpenAPI field drift.
- OpenAPI vs generated TypeScript drift.
- OpenAPI vs Android DTO consumed-field drift.
- Command-lens frontend string parsing/fallback guard.
- PA summary pagination guard or targeted backend test.

Acceptance tests:

- Guard self-test fails if `validate` stops detecting a missing reference.
- Guard output clearly says it checks discoverability, not semantic contract
  compliance.

### 7. New Contract Files Must Be Tracked

Files:

```text
docs/architecture/operational-read-model-contract.md
docs/architecture/operational-read-model-contract-implementation-handoff.md
tools/agent-hooks/check-operational-read-model-contract.mjs
```

Current problem:

These files are referenced by Makefile/CI but are currently untracked in this
worktree. A commit that forgets to add them will break everyone else.

Required fix:

- Add all three files to git in the implementation commit.
- Keep `make operational-read-model-contract-guard` and
  `make guardrail-registration-guard` green after staging.

### 8. Android Calendar DTO Invents Date Fields

File:

```text
apps/goatos-android/core/core-network/src/main/kotlin/sg/mesha/goatos/core/network/dto/CalendarDto.kt
```

Current problem:

`DriveSummaryDto` and `CalendarEventDto` include
`effective_schedule_date`, `current_assignment_date`,
`assignment_planned_date`, `planned_date`, and `scheduled_date`. The Calendar
backend domain type, OpenAPI `DriveSummary`, and generated TypeScript Calendar
schemas do not emit those fields. This lets mobile fall back to non-existent
schedule fields and collapse back to legacy `due_at` behavior.

Required fix:

- Remove phantom fields from Calendar DTOs unless backend/OpenAPI first owns
  them.
- If mobile needs an effective schedule date, add one backend-owned field with a
  clear grain and source, then update Go, OpenAPI, generated TS, and Kotlin in
  the same change.
- Update tests that currently assert preference for phantom fields.

Acceptance tests:

- Kotlin DTO tests decode current OpenAPI-compatible Calendar responses.
- Search shows Android does not reference the removed phantom fields.
- Calendar date rendering uses backend-owned fields only.

### 9. Android Adherence DTO Omits Required/Consumed Fields

File:

```text
apps/goatos-android/core/core-network/src/main/kotlin/sg/mesha/goatos/core/network/dto/AdherenceDto.kt
```

Current problem:

OpenAPI `AdherenceRow` requires `shed_name` and includes `partition_label`,
`drive_capacity_state`, `drive_operator_cap`,
`drive_available_operators`, `drive_latest_safe_date`, and
`drive_medical_defer_reason`. Android `AdherenceRowDto` omits those fields.

Required fix:

- Add the missing fields with correct nullability/defaults.
- Update mobile mapping/rendering only where the screen actually consumes them.
- Add serialization tests covering required field decode and optional field
  absence.

Acceptance tests:

- Android decodes an `AdherenceRow` fixture containing all OpenAPI fields.
- Android decodes a mixed-version fixture where optional fields are absent.
- No mobile UI derives partition/shed/capacity meaning from labels if a typed
  field exists.

### 10. Android Sheds Totals Are Page-Local

File:

```text
apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/ShedsViewModel.kt
```

Current problem:

`toShedsUiState` computes selected-day totals and adherence-window totals from
`VaccinationExecutionResponseDto.rows`. The response is paged with
`next_cursor`, so those totals undercount when more pages exist.

Required fix:

- Prefer backend-provided whole-result summary fields for day/adherence totals.
- If no summary exists, add one to the execution endpoint rather than summing
  paged rows in the ViewModel.
- Keep paged rows for the list/card body only.

Acceptance tests:

- Page size 1 and page size wide render the same selected-day totals.
- A response with non-nil `next_cursor` does not present page-local numbers as
  whole-day or adherence totals.
- List pagination still appends rows normally.

### 11. Shed Submit State Must Be Shed-Grain

File:

```text
backend/internal/vaccination/adapters/postgres/repository.go
```

Current problem:

For `shed_level_video`, submit state is derived from shed submission facts. For
other proof modes, `ShedCompletionSummary` falls back to
`ShedCompletionSubmitState(state, pendingVerify)` where `state` is the parent
`sop_tasks.state`. That can present a parent task state as if it were the shed's
own submit/proof state.

Required fix:

- Define the intended submit/proof state grain for per-goat proof mode.
- If a shed-grain state exists, read it from shed/obligation/proof/verification
  facts, not from the parent task.
- If per-goat mode intentionally has no shed submit state, expose an explicit
  enum such as `not_applicable` instead of borrowing parent state.

Acceptance tests:

- Two sheds under one parent task can show different per-shed proof/submit
  states.
- Parent task state changes do not mutate a shed card's submit state unless the
  shed facts changed.
- `shed_level_video` behavior remains unchanged.

### 12. Contract Documentation Corrections

Files:

```text
docs/architecture/operational-read-model-contract.md
AGENTS.md
SKILLS.md
docs/runbooks/local-ci.md
```

Current problem:

The contract language must stay precise enough that agents and developers do
not over-apply it.

Required fix:

- Treat the operational read model as a response/DTO/read contract, not a
  blanket requirement to introduce materialized tables.
- Require ADR/scale review before adding new projection tables or read stores.
- Include Action Center and Workflows anywhere the shared command-surface family
  is listed.
- Keep `ceo_ai` as a one-way reporting consumer, not a peer runtime dependency.
- Do not reference nonexistent `make contract-check`; list current gates and
  explicitly mark missing semantic guard halves.
- Use "every landing/change" wording instead of process-specific "every PR".
- Include projection/freshness metadata in read-shape expectations.
- Include the shed-grain vs shared-parent state rule.

## Suggested Implementation Order

1. Track the new doc and guard files.
2. Clarify/fix the discoverability guard and self-test.
3. Fix Android DTO drift: Control Tower `partition_label`, Adherence missing
   fields, and Calendar phantom date fields.
4. Fix Android Sheds page-local totals by adding/using backend whole-result
   summaries.
5. Fix Control Tower frontend parsing by adding/rendering backend-owned fields.
6. Fix shed-grain submit/proof state for per-goat proof mode.
7. Remove unsafe table fallbacks.
8. Fix Protocol Adherence selected-drive scope.
9. Fix Protocol Adherence pagination plus whole-result summary.
10. Add or extend cross-surface golden fixture coverage.

## Required Final Verification

Run at minimum:

```bash
make operational-read-model-contract-guard
make guardrail-registration-guard
make aggregate-projection-guard
make api-client-check
npm --prefix apps/admin-web run typecheck
npm --prefix apps/admin-web run test
```

For backend PA fixes, also run targeted Go tests for process integrity. If the
fix touches OpenAPI or Android DTOs, run the relevant generated-client and
Android unit tests.
