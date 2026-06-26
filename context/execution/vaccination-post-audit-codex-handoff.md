# Vaccination Post-Audit Codex Handoff

Date: 2026-06-25

Purpose: hand off the next Codex session after the active Claude Audit Log finish
run completes. This is a review-and-gate pass, not a parallel edit pass.

## Start Condition

Start only after the user provides Claude's final output or confirms Claude has
stopped.

During Claude's active run, Claude owns the Audit Log files:

- `apps/admin-web/features/operations-audit`
- `apps/admin-web/app/(admin)/operations/audit`
- `backend/internal/operationsaudit`
- `/operations/audit` OpenAPI and generated-client artifacts

Do not edit those files concurrently. The next Codex session should first review
Claude's output and current diff.

## Read First

- `AGENTS.md`
- `SKILLS.md`
- `context/README.md`
- `docs/phases/README.md`
- `apps/admin-web/AGENTS.md`
- `context/frontend/current-admin-web-scope.md`
- `context/execution/vaccination-trigger-closure-parallel-handoff.md`
- `context/execution/vaccination-pre-e2e-readiness-audit.md`
- `context/execution/vaccination-ui-proof-upload-current-handoff.md`
- `context/frontend/supplier-warmup-vaccination-gaps.md`
- latest `mock/goatos-dashboard-mock.html`

## Review Target

Verify the Claude Audit Log finish against the documented contract:

- no visible Operations sidebar vertical
- Audit Log lives under Admin / Data Ops while the implementation route may
  remain `/operations/audit`
- Audit `Viewing as` uses the same shared role-lens model as the top-bar preview
  and does not re-declare a second role list
- raw UUID/domain/module/category debug filters are not the primary CEO/admin UX
- mock business controls exist where backed: KPI cards, operation-family chips,
  role/span preview, search/status/operator/anomaly controls, activity trail,
  cursor pagination, entity/history links, empty/error states, and disabled
  export unless backed
- visible audit events stay limited to current built surfaces; no fake rows,
  fake totals, fixture projections, or unbuilt operation families
- accepted architecture is preserved: generated clients only, no raw backend
  URLs, no local route-handler business mutations, additive backend contracts
  only, and no idempotency/audit/outbox transaction-boundary changes
- this remains a mock-fidelity finish, not a broad SRP/refactor pass; only the
  role-lens extraction needed for sharing is allowed

## Required Proof

Do not accept green lint/typecheck/build alone.

Required proof before calling Audit Log finish complete:

- updated UI fidelity ledger with screenshot paths
- `smoke:visual:live` against populated local audit rows
- screenshots showing Admin / Data Ops IA, role-lens/`Viewing as`, operation
  chips, activity trail, empty/error behavior, and disabled export
- `git diff --check`
- documented admin-web gates from `apps/admin-web/AGENTS.md`
- backend verification only if Claude changed backend or contract files

## E2E Decision

After reviewing Claude's output, compare the result to:

- `context/execution/vaccination-pre-e2e-readiness-audit.md#e2e-start-gate`
- `context/execution/vaccination-trigger-closure-parallel-handoff.md#e2e-start-gate`

If any gate is missing, report the blocker and stop before E2E. If every gate is
green, the next step is to prepare or run the documented E2E plan in
`context/execution/procurement-vaccination-e2e-plan.md`.

## Next Codex Prompt

```text
Work in /Users/ravi/mesha/goatos. Read AGENTS.md, SKILLS.md,
context/README.md, docs/phases/README.md,
context/execution/vaccination-post-audit-codex-handoff.md,
context/execution/vaccination-ui-proof-upload-current-handoff.md,
context/execution/vaccination-trigger-closure-parallel-handoff.md,
context/execution/vaccination-pre-e2e-readiness-audit.md,
context/frontend/current-admin-web-scope.md, apps/admin-web/AGENTS.md, and latest
mock/goatos-dashboard-mock.html.

Review Claude's completed Audit Log finish. Do not edit concurrently owned Audit
files unless Claude has stopped and a concrete fix is required. First read the
current diff and Claude's summary, then verify: no Operations sidebar vertical,
Audit under Admin / Data Ops, shared top-bar role-lens used by Audit `Viewing as`,
raw debug filters removed from primary UX, mock business controls present,
visible events limited to current built surfaces, generated clients only, no fake
rows/totals/fixtures, and no architecture-boundary violations.

Validate the real proof gate: UI fidelity ledger screenshots plus
`smoke:visual:live` with populated local audit rows. Green lint/typecheck/build
alone is not enough. Run or verify the documented admin-web/backend gates only
where relevant, then decide whether the E2E Start Gate is green. Stop before
running E2E unless every documented gate is satisfied and the user explicitly
asks to proceed.
```
