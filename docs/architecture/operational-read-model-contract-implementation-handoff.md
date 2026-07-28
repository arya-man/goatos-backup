# Operational Read Model Contract Implementation Handoff

Status: current as of `origin/main` commit `66772e207701`
Context: follow-up to `docs/architecture/operational-read-model-contract.md`  
Goal: keep the PA / CT / Calendar / mobile contract work honest before adding
more verticals.

## Why This Exists

The operational read model contract is documented and wired into
discoverability/registration guards. Most of the original PA, Control Tower,
admin-web, and Android drift has now landed. This handoff records what is
already fixed and what still needs implementation so a fresh agent session does
not reopen resolved defects.

## Landed

- Protocol Adherence keeps endpoint pagination: rows remain paged, `NextCursor`
  is preserved, and the summary comes from repository-owned whole-result rows.
- Protocol Adherence selected-drive scoping is repository-owned and uses the
  stronger selected-drive discriminators instead of service-side `RuleID +
  DueAt` filtering.
- Protocol Adherence service calls defensively force
  `domain.CategoryVaccination`, matching the public endpoint invariant.
- Control Tower/admin-web render backend-owned structured fields directly. The
  drawer no longer derives proof/scope semantics by splitting display text.
- The global shared admin table fallback path no longer masks missing backend
  table contracts.
- Android Control Tower, Calendar, and Adherence DTO drift from the previous
  round has been addressed.
- Android vaccination execution DTOs now model the OpenAPI row shape for
  scheduling: `VaccinationExecutionRow` exposes `dueDate`; Android-only
  assignment/schedule fields must not be consumed unless backend/OpenAPI add
  them first.
- The contract docs, guard, Makefile references, and registration references
  are tracked.

## Still Pending

### Golden Fixtures

Add a single vaccination golden fixture that proves the same seed data agrees
across Calendar, Action Center, Protocol Adherence, Control Tower, Workflows,
Admin Web, Android, reporting, and generated clients.

Acceptance:

- Fixture covers shared counts, scope labels, state buckets, evidence/proof
  fields, and pagination-invariant summaries.
- OpenAPI/generated TypeScript and Android DTO decode paths both consume the
  same fixture shape.
- Admin Web and Android assertions do not recompute whole-result truth from
  page-local rows.

### Semantic Drift Guard

`make operational-read-model-contract-guard` is still a discoverability/static
text guard. It proves the contract doc is present and referenced; it does not
yet enforce Go/OpenAPI/TypeScript/Kotlin/frontend semantic drift.

Recommended follow-up guards:

- Go response structs vs OpenAPI field drift.
- OpenAPI vs generated TypeScript drift.
- OpenAPI vs Android DTO consumed-field drift.
- Frontend string parsing and fallback drift for command lenses/detail drawers.
- Backend tests that prove whole-result summaries remain independent from row
  pagination.

### Vertical Onboarding Gate

Write the operational read model checklist into the engineering gate for every
new vertical. The checklist must require the canonical write owner, work-item
identity, scope grain, time grain, state machine, evidence model, shared
summaries, Calendar representation, Action Center representation, Control Tower
representation, Workflows representation, admin-web contract, Android contract,
OpenAPI/generated clients, and golden fixture coverage.

### Android Sheds Whole-Result Summary

Android Sheds still has a fallback path that can load all execution rows to
compute day/adherence totals. The next backend slice should expose a
whole-result Sheds day/adherence summary so Android can render backend-owned
summary truth without draining paginated rows.

## Verification Boundary

Passing `make operational-read-model-contract-guard` and
`make guardrail-registration-guard` only proves discoverability and registration.
Do not describe those guards as semantic enforcement until the drift checks
above are implemented.
