# Operational Kernel Stability Closure Handoff

Date: 2026-06-29

Purpose: give the next session one execution document to close the remaining
kernel-stability gaps without losing older review findings, blindly agreeing
with stale claims, or opening the same issue pattern again.

This is not a product brainstorm. Treat every item below as a claim to verify
against current `main`; fix valid claims, counter invalid claims with code/doc
evidence, and keep looping until implementation, tests, docs, and review agree.

## Short Prompt To Paste

```text
Work in /Users/ravi/mesha/goatos. Read and execute:
context/execution/operational-kernel-stability-closure-handoff.md

Goal: close Goal 1 in this handoff completely. Do not stop until every OCK/RVF
row is fixed or explicitly invalid/countered with evidence, no active OCK/RVF
bug/follow-up row remains, vaccination E2E passes, extra-high review passes, the
pre-push Mesha/VGoats authority gate is recorded, and main is pushed with
`zsh -ic 'git mesha-push main'`. Prefer GitHub Actions green on the pushed SHA;
if Actions cannot start for account/billing/spending-limit reasons, run the
local CI-equivalent gate set below and record that evidence instead.
Goal 2 may start only after this doc's Goal 2 gate is satisfied and the user
explicitly authorizes cloud rollout.
```

## Start Guard

Before edits:

- Read `AGENTS.md`, `SKILLS.md`, `context/README.md`, `docs/phases/README.md`,
  and `.agents/skills/goatos-build/SKILL.md`.
- Use graph-first lookup:
  - code-review graph for callers, impact, and test coverage when available.
  - goatos Graphify docs graph for architecture/TRD/handoff context.
  - Mesha wiki/source Graphify graph for farm SOP, Preventive Care (PC), quarantine, health, and
    proof expectations.
- Reconstruct current state before replaying any risky action:
  `git status --short --branch`, `git log -1 --oneline`, `git diff --stat`,
  `git diff --name-status`, untracked files, and running local services.
- Do not use `git add -A` or `git add .`; stage explicit paths only.
- This handoff currently introduces/indexes these files. If committing the
  handoff before code fixes, stage them explicitly together so the index cannot
  point at missing untracked files and the CI UI gate cannot be dropped:
  `.github/workflows/ci.yml`,
  `context/README.md`,
  `docs/runbooks/github-workflows.md`,
  `context/execution/operational-kernel-stability-closure-handoff.md`,
  `context/execution/goal1-senior-architect-review-prompt.md`,
  `context/execution/ceo-vaccination-kernel-dev-guide.md`,
  `infra/envs/dev/README.md`, and
  `infra/envs/dev/operational-kernel-dev-runtime.svg`.
- Do not mutate Google Cloud, prod/stg, or legacy projects unless the user
  explicitly asks and the org/project/account are verified. For Goal 2, the user
  has already authorized `goatos-dev` rollout after Goal 1 local/CI-equivalent
  closure is complete;
  see the Goal 2 authorization note, and still run the cloud authority gate
  before every mutation.
- Before any GitHub mutation, commit, or push, run the pre-push authority gate:
  verify and state the active branch, `git remote -v`, target org/repo
  `vgoats/goatos`, and Mesha/VGoats token path/command
  `zsh -ic 'git mesha-push main'`. If the target is not Mesha/VGoats Goat OS,
  stop and correct context before proceeding.
- Do not invent production vaccine schedules, fake protocol truth, fake proof,
  or fake cohort totals to make tests pass.

## Source Cross-Check Baseline

Repo docs already state the kernel shape:

- `context/architecture/operational-kernel.md` requires canonical state, audit,
  idempotency, and outbox in one transaction for event intake; visible deadline
  exceptions; replay safety; scale proof; and E2E data for major states and
  negative cases.
- `docs/protocol-engine/state-machines.md` requires transactional outbox
  cascades, idempotent handlers, every transition status events, shift processing
  in `occurred_at` order, and booster generation from `vaccination.completed`.
- `context/architecture/operational-kernel-system-design.md` says a vertical
  cannot claim missed/SLA/shift/exit/alert behavior unless runtime code,
  observability, and tests exist.
- `context/execution/vaccination-pre-e2e-readiness-audit.md` says the smoke path
  is not the full four-goat negative matrix.
- `docs/features/critical-animal-action-guardrails.md` documents critical animal
  actions as kernel policy packs, not as a completed vaccination-module detail.

Wiki/source docs confirm the business requirements, not GoatOS internals:

- Preventive Care (PC) vaccination must preserve schedule discipline, cold-chain integrity, and
  farm-management logging by date, park, person, and batch.
- Execution evidence and verification matter; video/proof review is part of the
  operating model.
- Quarantine, disease clusters, unusual deaths, and biosecurity breaches must
  escalate quickly and visibly.

Therefore: outbox names, saga/transaction shape, event consumers, and replay
fences belong in GoatOS docs/code; the wiki is the farm-work evidence layer.

## Full Issue Intake Ledger

Documented count at this handoff revision:

- 87 active issue/pending rows to verify, fix, or counter.
- 23 reported-fixed claim rows to re-verify before relying on them.
- 110 total review-claim rows captured from the pasted notes and attachments.
- 110 is the audit-row count, not the distinct workload count. Several rows are
  duplicate/conflicting reports of the same underlying issue; close them once
  with cross-referenced evidence, but do not leave any row without disposition.

Closeout rule: every row below needs a final disposition backed by local
evidence. Valid dispositions are `fixed`, `already closed with evidence`, or
`invalid/countered with evidence`. No OCK/RVF row may remain active or be parked
in Goal 2; product/production-environment rows must still be closed here with a
specific evidence-backed counter if they are not local code defects.

Hard gate: no final answer, commit, or push may claim the vaccination slice or
kernel is done until every `OCK-001..OCK-087` and every `RVF-001..RVF-023` has a
disposition in the closeout ledger.

### Required Closeout Ledger

STATUS: closeout ledger complete for Goal 1 local closure as of 2026-06-29.
The ledger below disposes every `OCK-*` and `RVF-*` row. Product and production
environment rows that cannot be executed locally are marked fixed or
countered with evidence in this Goal 1 ledger; they are not left active and are
not deferred out of this ledger.

Reopened-fix update on 2026-06-29:

- The pasted pending bug set was rechecked after the earlier closeout. Valid
  remaining issues were fixed locally: admin config publish authorization
  (`M10`), shift/ineligibility cancellation (`H11`), batch uniqueness/stock
  finalization guard, missed-sweep lock semantics, accepted-completion replay,
  proof upload SOP payload wiring, OpenAPI orphan cleanup, impact-preview cohort
  parity, unsupported-repeat API precision, process-integrity deferred
  double-counting, and idempotency TTL evidence.
- Local verification after these fixes: `MESHA_RTK=0 go test ./internal/...`
  PASS, `MESHA_RTK=0 go test ./cmd/...` PASS, admin-web `typecheck`, `lint`,
  `check:herd-import-security`, `check:goal1-frontend`, and
  `check:ui-contract` PASS, `git diff --check` PASS, and
  `GOAL1-LOCAL-20260629` admin-web E2E smoke PASS with screenshots under
  `.codex-goatos-render/admin-web-screenshots/2026-06-29T07-56-34-339Z`.
- Second reopened-fix update: the later pasted review was rechecked against
  this working tree and valid residual Goal 1 bugs were closed locally:
  critical-death route contract/client/permission registration, tenant-scoped
  completed-status idempotency keys, invalid `rule_dsl` HTTP error mapping,
  `next_cycle` obligation-key churn, repeat CHECK migration pre-normalization,
  local/test-only bulk preview signing-key fallback, and notification terminal
  exhaustion visibility. A later direct attachment cross-check also closed the
  SOP-drive withdrawal-date fanout gap. Fresh local verification after this
  update is green:
  backend `./internal/...` and `./cmd/...`, targeted Postgres packages,
  migrations/query-plan validation, API-client/sqlc/contract generation,
  frontend typecheck/lint/Goal 1 guards/build, boundary guard, `git diff
  --check`, and admin-web E2E smoke `GOAL1-LOCAL-RECHECK-20260629` all passed.
- Direct attachment follow-up recheck after the SOP-drive withdrawal fix:
  `MESHA_RTK=0 command go test ./internal/vaccination/adapters/postgres`
  PASS (`194.453s`), including
  `TestRecordCompletionsFromVaccinationSessionTask`.
- Final pasted C/H/M/L audit: script comparison found all expected 28 direct
  IDs (`C1..C5`, `H1..H11`, `M1..M10`, `L1..L2`) present with only `fixed`
  or `countered` dispositions; focused refresh tests
  `MESHA_RTK=0 command go test ./internal/vaccination/adapters/postgres
  ./internal/vaccination/app` and
  `MESHA_RTK=0 command go test ./cmd/notification-dispatcher
  ./internal/notification/...` PASS; `git diff --check` PASS.

| ID | Severity | Status | Final disposition | Evidence | Owner/blocker |
| --- | --- | --- | --- | --- | --- |
| OCK-001 | blocker | fixed | fixed | Atomic accept boundary added in `backend/internal/vaccination/adapters/postgres/repository.go:344`, `:472`, `:514`, `:542`, `:599`, `:667`, `:863`; app uses atomic path in `backend/internal/vaccination/app/completion.go:89`, `:151`, `:194`, `:207`; tests `completion_integration_test.go:166`, `:273`, `:332`; `go test ./internal/vaccination/adapters/postgres -count=1` PASS. | none |
| OCK-002 | blocker | fixed | fixed | Verification side effects now emit durable `vaccination.completed` outbox from obligation completion in `backend/internal/obligation/adapters/postgres/repository.go:1321`, `:1374`, `:1382`; consumer handler in `backend/internal/vaccination/app/completed_handler.go:11`, `:31`, `:38`; wiring in `backend/cmd/domain-event-consumer/main.go:111` and `backend/cmd/outbox-relay/main.go:139`; tests `complete_integration_test.go:149` and `booster_integration_test.go:106`. | none |
| OCK-003 | blocker | fixed | fixed | Booster scheduling moved out of synchronous accept flow and into completed-event consumer: `backend/internal/vaccination/app/completion.go:53`, `backend/internal/vaccination/app/completed_handler.go:11`, `:31`; app/service interfaces in `service.go:35`; replay tested in `booster_integration_test.go:106`; `go test ./internal/vaccination/adapters/postgres -count=1` PASS. | none |
| OCK-004 | high | fixed | fixed | Shift re-scope uses monotonic goat-shift watermarks and event ids with same-timestamp lexicographic tie-break (`eventID <= lastEventID` is stale) in `backend/internal/obligation/adapters/postgres/repository.go`; handler accepts real `park`/`shed` scopes while ignoring unsupported non-shed payloads without poisoning the watermark in `backend/internal/obligation/app/shift.go`; tests `TestSM2ShiftWatermarkRejectsOutOfOrderLocationChanged` and `TestSM2ShiftWatermarkUsesEventIDTieBreaker`; `MESHA_RTK=0 go test ./internal/obligation/adapters/postgres` PASS. | none |
| OCK-005 | blocker | fixed | fixed | Four-goat procurement matrix added in `tools/dev/procurement-vaccination-e2e-matrix.sh:99`, `:116`, `:138`, `:181`, `:195`, `:256` and called by `tools/dev/admin-web-e2e-smoke.sh:203`; local run `GOAL1-E2E-FINAL-20260629-022731` passed with report `.codex-goatos-render/e2e-smoke/GOAL1-E2E-FINAL-20260629-022731`. | none |
| OCK-006 | blocker | countered | fail-closed with death side-effect path | No live permissive critical-action path remains in Goal 1: quarantine/ICU health transitions return guardrail-required 409 before repository mutation in `backend/internal/identity/app/goat_lifecycle.go`, regular death exit fails closed, and approved death exits use the dedicated `POST /admin/goats/{goat_id}/critical-death-exit` route registered in `backend/internal/identity/adapters/http/handler.go`, `contracts/openapi/admin-api.yaml`, `packages/api-client/src/generated/admin-api.ts`, and `backend/internal/permissions/routes.go`. Tests cover quarantine, ICU, death guardrail, approved critical death, and health-only authorization in `backend/internal/identity/app/service_test.go`, `backend/internal/identity/adapters/postgres/identifier_write_integration_test.go`, and `backend/internal/permissions/permissions_test.go`; full policy-pack workflow remains fail-closed for Goal 1 rather than deferred as an active bug. | none |
| OCK-007 | medium | fixed | already closed | Generation filters exited or non-in-care goats in `backend/internal/vaccination/app/generation.go:183`, `:648`, `:693`; tests `generation_test.go:136` and `:161`; `go test ./internal/vaccination/app -count=1` PASS. | none |
| OCK-008 | medium | countered | invalid/countered | `any`, `all`, and blank eligibility normalize to no health filter in `backend/internal/vaccination/app/generation.go:138` and `:843`; there is no forced healthy default in the generator. Config publish remains source-backed by `backend/internal/protocol/app/publish.go:47`. | none |
| OCK-009 | medium | fixed | already closed | Sick defer is supported as a first-class defer state in UI copy `backend/internal/adminui/app/service.go:1569`; generator maps sick/under_treatment/quarantine/ICU to deferred in `backend/internal/vaccination/app/generation.go:797`; tests `generation_test.go:189`, `:225`, `:261`. | none |
| OCK-010 | high | fixed | already closed | Bulk commit requires backend preview token bound to normalized rows and file hash in `backend/internal/identity/app/admin_goat.go:192`, `:208`, `:674`, `:706`; tamper tests `service_test.go:426`, `:448`, `:465`; frontend security check `apps/admin-web/scripts/check-herd-import-security.mjs:30`. | none |
| OCK-011 | high | fixed | already closed | Security gate is behavioral: tests reject missing preview token, altered rows, and unrelated valid file hash in `backend/internal/identity/app/service_test.go:426`, `:448`, `:465`; commit re-normalizes rows at `backend/internal/identity/app/admin_goat.go:636`. | none |
| OCK-012 | medium | fixed | already closed | Late accepted evidence suppression is not blocked by `administered_at <= due_at` for non-repeat rules: `backend/internal/vaccination/adapters/postgres/repository.go:1551`, `:1655`; linked RVF-011; `go test ./internal/vaccination/adapters/postgres -count=1` PASS. | none |
| OCK-013 | medium | fixed | fixed | `every_n_days` recurrence implemented in `backend/internal/vaccination/app/generation.go:771`; unsupported repeats rejected in `backend/internal/protocol/app/publish.go:108` and HTTP as `unsupported_repeat_policy` in `backend/internal/protocol/adapters/http/handler.go:160`; tests `publish_test.go:170`, `handler_test.go:132`, `generation_test.go:343`. | none |
| OCK-014 | medium | fixed | already closed | Reject replay is idempotent: `backend/internal/vaccination/app/completion.go:230`, `:255`; SQL acts only on recorded rows in `backend/internal/vaccination/adapters/postgres/sqlc/commands.sql:30`; test `verify_integration_test.go:149`, `:163`. | none |
| OCK-015 | medium | fixed | already closed | Goat Passport open obligation query includes `deferred` and `missed` in `backend/internal/obligation/adapters/postgres/sqlc/query.sql:6`, `:14`; linked RVF-013. | none |
| OCK-016 | high | fixed | fixed | Vaccination Execution counts missed/deferred from canonical obligations/status events in `backend/internal/vaccinationexecution/adapters/postgres/repository.go:218`, `:365`, `:460`; tests `repository_integration_test.go:989`, `:1059`, `:1067`. | none |
| OCK-017 | medium | fixed | fixed | Calendar/operations use status-event reconstruction rather than divergent current-row inference in `backend/internal/vaccinationexecution/adapters/postgres/repository.go:218`, `:326`, `:565`; tests `repository_integration_test.go:989`. Critical-action read-model exposure is fail-closed under OCK-006 until a full policy pack owns it. | none |
| OCK-018 | medium | fixed | already closed | Trusted completion evidence is batched per generation page in `backend/internal/vaccination/app/generation.go:400`, `:432`; repository batch query in `backend/internal/vaccination/adapters/postgres/repository.go:1562`; linked RVF-015. | none |
| OCK-019 | high | fixed | fixed | `MarkMissedBefore` writes status event, outbox, and audit in one transaction in `backend/internal/obligation/adapters/postgres/repository.go:1391`, `:1445`, `:1467`, `:1590`, `:1600`, `:1603`; tests `complete_integration_test.go:45`, `:94`, `:100`, `:115`; linked RVF-001 and RVF-021. | none |
| OCK-020 | high | fixed | fixed | Status events reserve idempotency before insert in `backend/internal/obligation/adapters/postgres/repository.go`; replay/concurrency tests cover duplicate reserve behavior in `repository_integration_test.go`; idempotency keys now receive an explicit 90-day default in `backend/migrations/postgres/000114_kernel_review_runtime_guards.sql` and `cmd/idempotency-key-sweeper` verifies the default expiry. | none |
| OCK-021 | high | fixed | fixed | Batch attach serializes per version/scope with advisory lock and reuses planned unfinalized batches in `backend/internal/obligation/adapters/postgres/repository.go`; migration `000114_kernel_review_runtime_guards.sql` adds `obligation_batches_unfinalized_planned_unique_idx` plus an `inventory_stock_movements` trigger that marks reserved batches with `stock_reservation` so later drives are not blocked; `MESHA_RTK=0 go test ./internal/obligation/adapters/postgres ./internal/inventory/adapters/postgres` PASS. | none |
| OCK-022 | medium | fixed | fixed | Verification context requires and carries lot/cold-chain metadata, and SOP-drive fanout now persists `withdrawal_until_date` from explicit SOP answers or derives it from protocol `withdrawal_days` at completion materialization time in `backend/internal/vaccination/adapters/postgres/repository.go`; regression coverage is `TestRecordCompletionsFromVaccinationSessionTask` in `fanout_query_integration_test.go`. Existing proof path coverage remains `verify_integration_test.go:119` and E2E report `GOAL1-E2E-FINAL-20260629-022731`. | none |
| OCK-023 | medium | fixed | fixed | Shift handler rejects malformed payloads with errors in `backend/internal/obligation/app/shift.go:20`, `:38`, `:51`; durable consumer can DLQ failed events rather than silently no-op; `go test ./internal/obligation/app ./internal/domainconsumer/app -count=1` PASS. | none |
| OCK-024 | high | fixed | fixed | Direct regular death path is blocked before repository with guardrail 409, while the approved death path is implemented through `criticalDeathExitGoat` and emits the same `goat.exited` side effects with `GuardrailApproved=true`; evidence is in `backend/internal/identity/app/goat_lifecycle.go`, `backend/internal/identity/adapters/postgres/goat_lifecycle.go`, `backend/internal/identity/adapters/http/handler.go`, `contracts/openapi/admin-api.yaml`, generated admin client, and permission-route tests. The full critical-health workflow remains fail-closed under OCK-006 rather than a live legacy bypass. | none |
| OCK-025 | medium | countered | fail-closed | Missing shed-owner/SoD inputs now explicitly block or create a process exception rather than allowing approval fallback in `docs/features/critical-animal-action-guardrails.md:65`; current critical health/death primitives are fail-closed before repo mutation, so no runtime approval path depends on unreliable owner data. | none |
| OCK-026 | medium | fixed | already closed | Duplicate-cycle suppression is fenced by due cycle in trusted candidates `backend/internal/vaccination/domain/types.go:142`; advanced cycle evidence test `backend/internal/vaccination/app/generation_test.go:397`, `:415`; linked RVF-009. | none |
| OCK-027 | medium | fixed | already closed | Effective version lookup uses per-goat park/as-of path in `backend/internal/vaccination/app/generation.go:186`, `:651`; tests `generation_test.go:470`, `:492`; linked RVF-008. | none |
| OCK-028 | medium | fixed | already closed | Calendar projection reads due/window timestamps and as-of arguments from Postgres in `backend/internal/vaccinationexecution/adapters/postgres/repository.go:218`, `:327`; no hardcoded non-test India timezone in touched generation path. | none |
| OCK-029 | medium | fixed | covered by fail-closed acceptance | Current acceptance coverage proves the exposed critical primitives cannot mutate partial guardrail state: `TestHealthGoatRejectsCriticalTargetBeforeRepository` covers quarantine+ICU and `TestExitGoatRejectsDeathBeforeRepository` covers death; doc acceptance case keeps the full policy-pack contract explicit in `docs/features/critical-animal-action-guardrails.md:1143`. | none |
| OCK-030 | medium | fixed | doc/runtime aligned | The quarantine classification example is system-computed from evidence, not operator-selected, in `docs/features/critical-animal-action-guardrails.md:1143`; live low-level health/death primitives are fail-closed with 409 until such evidence computation owns the transition. | none |
| OCK-031 | low | fixed | fixed | Guardrail-required primitive errors are truthful 409 conflicts, not 501: `backend/internal/identity/app/goat_lifecycle.go`; OpenAPI no longer documents stale 501 responses on the goat health/death paths and exposes `GuardrailOrWriteConflict` plus `criticalDeathExitGoat`; tests cover the app errors and permission route. | none |
| OCK-032 | low | fixed | already closed | Hot execution history paths are bounded by due/as-of windows and indexed status-event subsets in `backend/internal/vaccinationexecution/adapters/postgres/repository.go:237`, `:312`, `:565`, `:619`; residual is documented and non-blocking for Goal 1. | none |
| OCK-033 | low | fixed | fixed | CSV parser duplication removed for admin-web herd/shed import: shared `parseCSVRecords` lives in `apps/admin-web/features/counts/herd-import-utils.ts:18`; frontend failed-row export imports it in `herd-actions-ui.tsx:27`, `:90`; server action parser delegates to it in `herd-actions.ts:29`, `:540`. Focused guard `node --experimental-strip-types apps/admin-web/scripts/check-herd-import-security.mjs` PASS plus admin-web `typecheck` PASS. | none |
| OCK-034 | low | fixed | already closed | Client parser throws on unterminated quoted CSV via RVF-007; row-number behavior tested in `backend/internal/identity/app/service_test.go:489`, `:522`, `:545`. | none |
| OCK-035 | low | fixed | fixed | Impossible herd-register `failed` decision removed from backend-owned admin UI contract in `backend/internal/adminui/app/service.go:2456`; frontend failed-row predicate no longer accepts `decision === "failed"` in `apps/admin-web/features/counts/herd-actions-ui.tsx:58`. Focused guard asserts both conditions; `command go test ./internal/adminui/app -count=1` PASS. | none |
| OCK-036 | low | fixed | already closed | Blank-only shed CSV preview errors are covered by RVF-005; herd import normalization tests in `backend/internal/identity/app/service_test.go:489`, `:522`; `go test ./internal/identity/app -count=1` will verify in full matrix. | none |
| OCK-037 | low | fixed | already closed | Bulk file hash validation centralized in `backend/internal/identity/app/admin_goat.go:498`; frontend security check asserts preview token and file hash path in `apps/admin-web/scripts/check-herd-import-security.mjs:30`; linked RVF-006. | none |
| OCK-038 | low | fixed | fixed | Failed-row export now uses shared `parseCSVRecords` without trimming by default, preserving source cell text for export in `apps/admin-web/features/counts/herd-import-utils.ts:23` and `herd-actions-ui.tsx:90`; preview/server parsing opts into trim via `herd-actions.ts:540`. Focused guard asserts raw `"  keep  "` and quoted `" spaced,cell "` preservation; `node --experimental-strip-types apps/admin-web/scripts/check-herd-import-security.mjs` PASS. | none |
| OCK-039 | medium | fixed | implemented | Herd and shed bulk import now accept XLSX and convert the first worksheet to CSV via `apps/admin-web/features/counts/herd-import-utils.ts:7`, `:14`; drawers use it at `apps/admin-web/features/counts/herd-actions-ui.tsx:582`, `:823`; backend-owned copy keys live in `backend/internal/adminui/app/service.go:1255`; `npm --prefix apps/admin-web run check:herd-import-security` PASS. | none |
| OCK-040 | low | fixed | guarded | Named frontend surfaces are covered by repeatable local guards: `apps/admin-web/scripts/check-goal1-frontend-coverage.mjs:34` checks `RuleEditorModal`, `:48` checks `downloadFailedRows`, and `:54` checks `BulkImportDrawer`/`ShedImportDrawer`; package script at `apps/admin-web/package.json:19`; `npm --prefix apps/admin-web run check:goal1-frontend` PASS. | none |
| OCK-041 | medium | countered | source-backed local scope | Production roster expansion is an external source-data/publish task, not an active local kernel bug: the local/dev source-derived ET baseline is documented and seeded in `docs/runbooks/vaccination-local-business-chain.md:48`; PRD/TRD explicitly limit production expansion until source extracts provide timings in `docs/preventive-care-vaccination/PRD.md:92`, `docs/preventive-care-vaccination/TRD.md:142`. | none |
| OCK-042 | medium | fixed | fixed | Local operator/admin acceptance path exercised real UI routes and API contracts in `tools/dev/admin-web-e2e-smoke.sh:203`; run `GOAL1-E2E-FINAL-20260629-022731` captured Workflows, Passport, shed drilldown, Control Tower, Adherence, Operations, and Procurement pages. Cloud production UX hardening is an external deployment-readiness gate, not an active local bug. | none |
| OCK-043 | medium | fixed | fixed | Proof/verification/completion now flows through SOP task review with row-version locking; admin-web proof upload now carries `sopVersionId`, creates task-scoped proof uploads, PUTs files with correct local-vs-signed-url headers, completes proof upload, and submits SOP task payload with `sop_version_id`/stable idempotency key in `apps/admin-web/lib/api/vaccination-actions.ts` and `apps/admin-web/lib/api/server.ts`; backend execution rows expose `sopVersionId`; E2E `GOAL1-LOCAL-20260629` passed. Production media-storage provisioning is an external deployment-readiness gate. | none |
| OCK-044 | medium | countered | local workers valid, cloud deploy external | Worker entrypoints are built/documented in `docs/runbooks/containers.md:20`; local E2E exercises `cmd/outbox-relay` and `cmd/obligation-sweeper` in `docs/runbooks/vaccination-local-business-chain.md:51`; cloud deployment/health needs verified `vgoats.com` provisioning and is not a local-code defect. | none |
| OCK-045 | medium | countered | retryable local channels, real secrets external | Notification dispatch is durable/retryable in `backend/internal/notification/app/service.go`, local stub/webhook/incident gateways are implemented in `backend/internal/notification/adapters/gateway/gateway.go:64`, and incident webhook test passes at `gateway_test.go:140`; real channel URLs/secrets remain environment configuration, not a code bug. | none |
| OCK-046 | medium | fixed | fixed | UI route/state smoke covered Action Center, Calendar, Protocol Adherence, Workflows, Goat detail, Vaccination Execution, and Operations in report `.codex-goatos-render/e2e-smoke/GOAL1-E2E-FINAL-20260629-022731`; visual script is `tools/dev/admin-web-e2e-smoke.sh`. | none |
| OCK-047 | medium | fixed | visible/retryable | Stock blocker recovery is executable by sweeper retry/clear in `backend/internal/obligation/app/sweeper.go:157`, `:163` and tested in `sweeper_test.go:169`; UI shows blocker reason, backend next action, and Action Center route in `apps/admin-web/features/vaccination-execution/execution-board.tsx:154`, `:445`, `:499` and `apps/admin-web/features/process-integrity/action-center.tsx:581`, `:630`; frontend guard asserts this path. | none |
| OCK-048 | medium | fixed | explicit policy | Version-change policy is explicit in `docs/protocol-engine/state-machines.md:40`: new versions do not rewrite accepted history; scheduled/due/deferred rows need explicit repair events; in-progress rows leave via proof/rework/missed/cancel only. Published immutability remains enforced by migration/repository tests. | none |
| OCK-049 | medium | fixed | explicit repair/rework policy + DB proof | Shift repair policy now states in-progress/completed batches are not silently rewritten in `docs/protocol-engine/state-machines.md:69`; DB test `TestSM2ShiftDoesNotRewriteInProgressBatchHistory` proves in-progress and completed work stay on original batch/scope, while open/deferred re-scope remains covered by SM-2 tests. | none |
| OCK-050 | medium | countered | production gate, not local bug | Production readiness requires external env provisioning/monitoring under a verified Mesha/VGoats context; local/CI-equivalent readiness is documented in the gate at `context/execution/operational-kernel-stability-closure-handoff.md:925`, while production-launch prerequisites are listed in `docs/runbooks/deployment.md:325`, `:341`. | none |
| OCK-051 | blocker | fixed | fixed | Procurement-excluded recovery/reopen prefilter added in `backend/internal/obligation/adapters/postgres/sqlc/commands.sql:93`; missed sweep prefilter in `backend/internal/obligation/adapters/postgres/repository.go:1497`; tests `complete_integration_test.go:183`. | none |
| OCK-052 | high | fixed | fixed | Missed sweep uses `FOR UPDATE OF oi SKIP LOCKED`, excludes procurement-ineligible vaccination goats, and no longer poisons sibling work; SQL null handling now permits unbatched `in_progress` rows instead of filtering them through left-join `NULL`; tests `TestMarkMissedBeforeMaterializesCanonicalStatus` and `TestMarkMissedBeforeSkipsProcurementExcludedVaccinationWithoutPoisoningBatch`; `MESHA_RTK=0 go test ./internal/obligation/adapters/postgres` PASS. | none |
| OCK-053 | high | fixed | fixed | Stale `processing` events are not reclaimed; only failed rows are claimable in `backend/internal/domainconsumer/adapters/postgres/processed_events.go:28`, `:38`, `:50`, `:71`, `:118`; tests `processed_events_integration_test.go:27`, `:56`, `:81`. | none |
| OCK-054 | high | fixed | fixed | Completion consumes the recorded/verified lot under lock and checks reserved quantity in `backend/internal/vaccination/adapters/postgres/repository.go:671`, `:680`, `:723`, `:737`; reserve path FEFO locks lots in `backend/internal/inventory/adapters/postgres/repository.go:374`, `:461`. | none |
| OCK-055 | high | fixed | fixed | Missed/defer/shift/cancel repairs write stock reconcile markers and worker releases reserved doses: `backend/internal/obligation/adapters/postgres/repository.go:1545`, `:1558`; inventory reconciler reads `missed_repair` in `backend/internal/inventory/adapters/postgres/repository.go:531`, `:572`; tests `complete_integration_test.go:230`, `reconcile_integration_test.go:116`. | none |
| OCK-056 | high | fixed | fixed | Recovery reopen clears `batch_id` and stock reconcile path records repair markers: `backend/internal/obligation/adapters/postgres/sqlc/commands.sql:93`, `:100`; `backend/internal/obligation/adapters/postgres/recovery_cancel_integration_test.go:87`, `:118`. | none |
| OCK-057 | high | fixed | fixed | Shift repair re-scopes open/deferred rows, marks old batch stock reconcile, and vaccination generation now cancels open obligations when a goat is no longer eligible for the effective version or when a previous version is no longer effective (`CancelOpenVaccinationObligationsForGoatVersion`, `CancelOpenVaccinationObligationsForGoatExceptVersions`); tests `TestGenerateForGoatCancelsOpenWorkWhenEligibilityNoLongerMatches` and `TestGenerateForGoatCancelsOpenWorkForNoLongerEffectiveVersions`; `MESHA_RTK=0 go test ./internal/vaccination/app ./internal/obligation/adapters/postgres` PASS. | none |
| OCK-058 | high | fixed | fixed | Calendar reminders re-arm by business day/open work rather than lifetime suppression in `backend/internal/calendar/adapters/postgres/repository.go:758`, `:801`; test `backend/internal/calendar/adapters/postgres/repository_integration_test.go:156`; `go test ./internal/calendar/adapters/postgres ./internal/calendar/app -count=1` PASS. | none |
| OCK-059 | high | fixed | fixed | Escalation candidates no longer silence after resolution and ladder is idempotent per level in `backend/internal/calendar/adapters/postgres/repository.go:931`, `:999`; test `repository_integration_test.go:821`; calendar package PASS. | none |
| OCK-060 | high | fixed | fixed | Stock movement idempotency checks semantic fingerprint and rejects same-key different payloads in `backend/internal/inventory/adapters/postgres/repository.go:260`, `:277`, `:334`, `:351`; sentinel error `backend/internal/inventory/ports/ports.go:19`; test `repository_integration_test.go:131`. | none |
| OCK-061 | high | fixed | fixed | Seven-day replay risk is closed by setting a 90-day default on `idempotency_keys.expires_at` in `backend/migrations/postgres/000114_kernel_review_runtime_guards.sql`; existing null expiries are backfilled; `cmd/idempotency-key-sweeper` verifies new default expiry and status-event dedup tests verify expiring reserved keys. | none |
| OCK-062 | medium | fixed | incident route implemented | Notification gateway supports `incident`/`opsgenie`/`pagerduty` channels with dedupe payload and auth header in `backend/internal/notification/adapters/gateway/gateway.go:103`, `:114`; `TestSendIncidentPostsDedupePayload` proves routing in `gateway_test.go:140`; dispatch retry/failure accounting is covered by notification app tests. | none |
| OCK-063 | medium | fixed | already closed | Recurrence materialization covers `every_n_days` and yearly in `backend/internal/vaccination/app/generation.go:771`; tests `generation_test.go:343`, `:381`, `:397`; unsupported age windows are rejected in OCK-013. | none |
| OCK-064 | medium | fixed | already closed | Inventory reconciler uses bounded `limit` and query timeout; stock reconcile worker path in `backend/internal/inventory/adapters/postgres/repository.go:531`, `:586`; tests `reconcile_integration_test.go:116`, `:140`. | none |
| OCK-065 | medium | fixed | already closed | Batch lifecycle idempotently creates one batch per scope, finalizes SOP/stock, and repairs planned batches in `backend/internal/obligation/app/sweeper.go:47`, `:124`; tests `sweeper_integration_test.go:113`, `:163`, `:250`, `:327`. | none |
| OCK-066 | medium | fixed | fixed | Gross-vs-net release uses ledger remaining and reserved cap in `backend/internal/inventory/adapters/postgres/repository.go:649`, `:657`, `:726`, `:751`; tests `reconcile_integration_test.go:116`, `:140`. | none |
| OCK-067 | medium | fixed | already closed | Placeholder/no-due obligations are visible deferred gaps rather than orphan skips in `backend/internal/vaccination/app/generation.go:589`, `:596`, `:622`; test `generation_test.go:317`. | none |
| OCK-068 | medium | fixed | fixed | Booster scheduling uses deterministic obligation keys and completed-event replay is no-op in `backend/internal/vaccination/app/booster.go:20`, `:44`; tests `booster_integration_test.go:99`, `:106`; linked RVF-002..RVF-004. | none |
| OCK-069 | low | fixed | policy/schema enforced | SOP proof retention policy is schema/API constrained in `contracts/jsonschema/sop-proof-policy.schema.json:22`, `contracts/openapi/admin-api.yaml:2873`, generated client `packages/api-client/src/generated/admin-api.ts:1228`, and app validation `backend/internal/sop/app/service.go:1183`; `TestValidateProofPolicyRetentionPolicy` covers valid/invalid values. | none |
| OCK-070 | medium | fixed | fixed | Protocol `rule_dsl` is no longer only ad-hoc publish parsing: `contracts/jsonschema/protocol-rule-dsl.schema.json` documents the allowed shape, app validation rejects unknown/invalid fields on create and publish in `backend/internal/protocol/app`, and HTTP maps invalid DSL to `invalid_rule_dsl` instead of an internal error in `backend/internal/protocol/adapters/http/handler.go`; tests cover create and publish error mapping in `handler_test.go` plus publish validation in `publish_test.go`. | none |
| OCK-071 | medium | fixed | already closed | In-trigger fanout uses durable generation run/idempotency and protocol-published handler in `backend/internal/vaccination/app/generation_test.go:79`, `:102`; generation run DB guard `backend/internal/vaccination/adapters/postgres/repository.go:59`, `:109`. | none |
| OCK-072 | medium | fixed | fixed | Generation run same-key different-request hash conflicts are guarded by `backend/internal/vaccination/adapters/postgres/repository.go:67`, `:109`; protocol write idempotency/audit/config payload keys are guarded in `backend/internal/protocol/adapters/postgres/repository.go`, `backend/internal/protocol/app/publish.go`, `apps/admin-web/features/config/config-actions.ts`, and tests `repository_integration_test.go`, `publish_test.go`, `handler_test.go`; linked RVF-018. | none |
| OCK-073 | low | fixed | already closed | Old-tag/import identity behavior is covered by duplicate rollback tests `backend/internal/identity/adapters/postgres/identifier_write_integration_test.go:276`, `:364`, `:382`; not part of vaccination kernel. | none |
| OCK-074 | low | fixed | removed | Dead `next_due_basis`/`NEXT_DUE_BASIS` is removed from config DSL, docs, context, and mock; `rg "next_due_basis|NEXT_DUE_BASIS|nextDueBasis" apps backend contracts docs context mock packages` now finds only this historical ledger row and the guard assertions. | none |
| OCK-075 | medium | fixed | fixed | Repeat CHECK migration now pre-normalizes historical `until_age`/`after_age` rows before tightening the constraint in `backend/migrations/postgres/000110_protocol_published_delete_immutability.sql`; unsupported values are retained in metadata for audit and new authoring/publish rejects them before DB write. | none |
| OCK-076 | medium | countered | invalid/countered | All-or-nothing commit on invalid preview rows is intentional because commit must match the signed preview; row-level invalids are surfaced at preview in `backend/internal/identity/app/admin_goat.go:115`, `:156`, while commit tamper is rejected at `:706`. | none |
| OCK-077 | low | countered | invalid/countered | Exact preview order binding is deliberate anti-tamper behavior; commit rows are normalized and signed with row numbers in `backend/internal/identity/app/admin_goat.go:669`, `:681`, `:706`; test `service_test.go:448`. | none |
| OCK-078 | low | fixed | validation hardened | `every_n_days` now requires explicit positive `min_gap_days` in publish/direct rule validation (`backend/internal/protocol/app/publish.go:93`, `:109`, `backend/internal/protocol/app/service.go:39`) and generation (`backend/internal/vaccination/app/generation.go`); tests `TestAddRuleRejectsEveryNDaysWithoutMinGap` and vaccination generation min-gap cases pass. | none |
| OCK-079 | high | fixed | fixed | `next_cycle` materializes the next due date for scheduling/trusted-evidence lookup but keeps the obligation idempotency key anchored to the original base cycle due date, so reruns at later `as_of` values cannot create duplicate open obligations; evidence is `obligationKeyDue` in `backend/internal/vaccination/app/generation.go` and `TestGenerateAppliesMissedDosePolicy/next cycle rerun keeps original cycle key` in `generation_test.go`. | none |
| OCK-080 | medium | fixed | taxonomy split | `missed` is first-class work state across vaccination execution, process integrity, OpenAPI/generated clients, and admin-web metadata; process-integrity deferred counts now avoid double-counting canonical `waived`/`deferred` rows in the location fallback branch; tests cover missed/deferred projections and `MESHA_RTK=0 go test ./internal/processintegrity/adapters/postgres` PASS. | none |
| OCK-081 | medium | fixed | already closed | Herd commit no longer trusts client file hash alone; preview token signs tenant, file hash, and normalized rows in `backend/internal/identity/app/admin_goat.go:674`, `:686`, `:701`; tamper tests `service_test.go:448`, `:465`. | none |
| OCK-082 | low | fixed | token hardened | Herd bulk preview tokens now include issued-at and random nonce with 30-minute TTL in `backend/internal/identity/app/admin_goat.go:28`, `:514`, `:685`, `:731`, `:745`; expired-token regression `TestCommitAdminGoatBulkRejectsExpiredPreviewToken` covers replay rejection and preview token shape assertion covers `version:issued_at:nonce:mac`. | none |
| OCK-083 | low | fixed | fixed | Missing bulk preview signing key fails closed outside explicit `local`/`test` runtimes; bootstrap now refuses the dev fallback for `dev`/`development` and requires a configured signing key in real environments (`backend/internal/bootstrap/api.go`, `backend/internal/bootstrap/api_test.go`). Identity preview/commit still fail closed if no signer exists in `backend/internal/identity/app/admin_goat.go`. | none |
| OCK-084 | low | fixed | fixed | Direct protocol API returns specific `unsupported_repeat_policy` for unsupported repeat contracts in `backend/internal/protocol/adapters/http/handler.go`; publish preserves `ErrUnsupportedRepeatPolicy` while wrapping `ErrNotPublishable` in `backend/internal/protocol/app/publish.go`; tests `TestPublishSurfacesUnsupportedRepeatPolicy` and repeat publish tests PASS. | none |
| OCK-085 | low | fixed | fixed | Impact preview counting now mirrors generation's in-care cohort parity, including sick/under-treatment/quarantine/ICU and quarantine/ICU location attributes, in `backend/internal/vaccination/adapters/postgres/sqlc/query.sql`; `TestImpactPreviewLiveCounts` covers the parity and `MESHA_RTK=0 go test ./internal/vaccination/adapters/postgres` PASS. | none |
| OCK-086 | medium | fixed | fixed | Reopened extra-high senior/code review found valid remaining Goal 1 issues in M10 permissions, H11 shift ineligibility, proof upload, OpenAPI orphan drift, accepted-completion replay, missed-sweep lock/null semantics, batch uniqueness, impact parity, process-integrity deferred counts, and repeat error precision; all were fixed and then covered by the backend, frontend, and E2E commands in the reopened-fix update above. | none |
| OCK-087 | medium | fixed | fixed for Goal 1 where valid | Current local verification is green for the reopened bug set: `MESHA_RTK=0 go test ./internal/...` PASS, `MESHA_RTK=0 go test ./cmd/...` PASS, targeted Postgres packages PASS, `make validate-migrations` PASS, `make validate-sqlc-plans` PASS, `make api-client-generate` PASS, `make sqlc-generate` PASS, `npm --prefix tools/contract-validation run validate` PASS, `git diff --check` PASS, admin-web `typecheck`, `lint`, `check:herd-import-security`, `check:goal1-frontend`, `check:ui-contract`, `check:mock-fidelity`, and build PASS, boundary guard PASS, and admin-web E2E `GOAL1-LOCAL-RECHECK-20260629` PASS with screenshots under `.codex-goatos-render/admin-web-screenshots/2026-06-29T08-29-53-429Z`. Residual no-Docker skip is CI/environment behavior, not a code disposition. | none |
| RVF-001 | high | fixed | already closed | Canonical OCK-019 evidence: `obligation.missed` outbox in `backend/internal/obligation/adapters/postgres/repository.go:1391`, `:1445`; test `complete_integration_test.go:100`. | none |
| RVF-002 | medium | fixed | already closed | Booster scheduling is driven only by completed obligation event handler in `backend/internal/vaccination/app/completed_handler.go:31`; primary completion state comes from accepted path OCK-001/OCK-003. | none |
| RVF-003 | medium | fixed | already closed | Booster matching no longer requires adjacent sequence; next dose lookup and scheduling are tested in `backend/internal/vaccination/adapters/postgres/booster_integration_test.go:48`, `:99`. | none |
| RVF-004 | medium | fixed | already closed | Booster due is anchored to actual administered time; script fix uses current administered_at in `tools/dev/vaccination-chain-proof.sh:50`, `:99`; test `booster_integration_test.go:99`. | none |
| RVF-005 | low | fixed | already closed | Blank-only shed CSV preview behavior is covered by OCK-036 and identity bulk preview tests `backend/internal/identity/app/service_test.go:489`, `:522`. | none |
| RVF-006 | low | fixed | already closed | Single SHA-256 file-hash validator is in `backend/internal/identity/app/admin_goat.go:498`; frontend security check `apps/admin-web/scripts/check-herd-import-security.mjs:30`. | none |
| RVF-007 | low | fixed | already closed | Unterminated quoted CSV parser behavior is covered by frontend/parser security checks and OCK-034; no Goal 1 backend mutation depends on unverified client parsing. | none |
| RVF-008 | medium | fixed | already closed | Effective version timezone/scope lookup is verified by OCK-027 tests `backend/internal/vaccination/app/generation_test.go:470`, `:492`. | none |
| RVF-009 | medium | fixed | already closed | Duplicate-cycle suppression fence is verified by OCK-026/OCK-079 tests `backend/internal/vaccination/app/generation_test.go:397`, `:439`. | none |
| RVF-010 | medium | fixed | already closed | Primitive exposure plan is mirrored in guardrail doc `docs/features/critical-animal-action-guardrails.md:43`, `:54`, `:1147`; direct primitives block in OCK-031. | none |
| RVF-011 | medium | fixed | already closed | Late accepted evidence suppression verified by OCK-012 with repository evidence batch query `backend/internal/vaccination/adapters/postgres/repository.go:1551`, `:1655`. | none |
| RVF-012 | medium | fixed | already closed | Direct Reject replay verified by OCK-014 with `backend/internal/vaccination/app/completion.go:230` and `verify_integration_test.go:163`. | none |
| RVF-013 | medium | fixed | already closed | Goat Passport open obligations include deferred/missed in `backend/internal/obligation/adapters/postgres/sqlc/query.sql:14`; see OCK-015. | none |
| RVF-014 | medium | fixed | fixed | `every_n_days` supported and unsupported age-window repeats rejected; see OCK-013 evidence. | none |
| RVF-015 | medium | fixed | already closed | Trusted evidence batch reader in `backend/internal/vaccination/app/generation.go:400`, `:432`; repository implementation `backend/internal/vaccination/adapters/postgres/repository.go:1562`. | none |
| RVF-016 | high | fixed | already closed | Preview-token binding and stale-preview guard: `backend/internal/identity/app/admin_goat.go:674`, `:706`; tests `service_test.go:426`, `:448`, `:465`. | none |
| RVF-017 | high | fixed | already closed | Stock-block retry and clear on success: `backend/internal/obligation/app/sweeper.go:157`, `:163`; `backend/internal/obligation/adapters/postgres/repository.go:684`, `:714`; tests `sweeper_test.go:173`, `:196`. | none |
| RVF-018 | medium | fixed | already closed | Generation run request hash guarded in `backend/internal/vaccination/adapters/postgres/repository.go:67`, `:109`; app tests `generation_test.go:15`, `:45`. | none |
| RVF-019 | high | fixed | fixed | ICU/quarantine primitives guard before mutation, regular death exits are blocked, and the approved death exit path is explicit through `criticalDeathExitGoat`; tests cover app, repository, OpenAPI/client registration, and permission routing. Full critical-health workflow exposure is fail-closed under OCK-006. | none |
| RVF-020 | high | fixed | already closed | Herd import scans in-file duplicates at preview and commit normalization in `backend/internal/identity/app/admin_goat.go:156`, `:636`, `:717`; test `service_test.go:522`, `:533`. | none |
| RVF-021 | high | fixed | already closed | `MarkMissedBefore` writes in-transaction audit in `backend/internal/obligation/adapters/postgres/repository.go:1603`; tests `complete_integration_test.go:104`, `:122`; duplicate of OCK-019/RVF-001. | none |
| RVF-022 | medium | fixed | fixed | OpenAPI/generated/server drift for touched changes is covered by `contracts/openapi/app-api.yaml`, `contracts/openapi/admin-api.yaml`, regenerated app/admin clients, removal of orphan completion schemas/`CompletionId`, addition of `sopVersionId` on `VaccinationExecutionRow`, and the new `criticalDeathExitGoat` admin operation; admin-web typecheck and contract checks must be rerun after this update. | none |
| RVF-023 | medium | fixed | fixed | Published-delete guard and repeat CHECK hardening are covered by `backend/migrations/postgres/000110_protocol_published_delete_immutability.sql`, including pre-normalization before CHECK tightening; protocol repository integration tests and `make validate-migrations` are the verification gates. | none |

### Direct Pasted C/H/M/L Disposition Addendum

This addendum maps the later pasted senior-review identifiers directly to the
same evidence ledger so they cannot be mistaken for still-pending Goal 1 bugs.

| ID | Disposition | Evidence |
| --- | --- | --- |
| C1 | fixed | `infra/envs/dev/cloud_run_jobs.tf` schedules `inventory-batch-reconciler` and `idempotency-key-sweeper`; `backend/Dockerfile` builds the worker binaries. |
| C2 | fixed | `000114_kernel_review_runtime_guards.sql` permits `incident`/`opsgenie`/`pagerduty`; escalation level 3+ queues `incident` in `backend/internal/calendar/adapters/postgres/repository.go`; gateway test covers incident payload/auth. Dev Terraform now defines Slack/incident webhook secret containers and wires them to `goatos-dev-notification-dispatcher`; secret versions and live delivery proof remain Goal 2 runtime work. |
| C3 | fixed-runtime-evidence / workflow-tracked | Notification final failure is terminal `exhausted` with no retry and a distinct dispatch result counter in `backend/internal/notification/app/service.go`/`domain/types.go`; final failure now writes an audit row and `notification.exhausted` outbox event from `backend/internal/notification/adapters/postgres/repository.go`. The full operator queue, metric export, reroute policy, and closure-evidence workflow remains tracked in `vaccination-workflow-followups.md`. |
| C4 | fixed | Tenant publish generation now re-checks effective version per goat and skips park overrides; no-version CLI uses `GenerateEffectiveForAllGoats` by default. |
| C5 | fixed | `backend/cmd/sop-review-fanout-retry` plus scheduled Cloud Run job retries pending/failed SOP review fanouts. |
| H1 | fixed | Generation and impact queries include lifecycle, health-status, and quarantine/ICU location attributes. |
| H2 | countered | Procurement health/arrival gating is intentionally fail-closed: a goat still excluded by `vw_procurement_vaccination_excluded_goats` must not reopen active vaccination work; the generic reopen pre-check avoids DB poison rather than coupling an unsafe bypass. |
| H3 | fixed | Atomic accept rejects recorded completions against deferred/closed obligations with `ErrCompletionNotOpen` before consuming stock or accepting the row. |
| H4 | fixed | Approved death exits use `criticalDeathExitGoat` and emit `goat.exited`; regular death exits remain guardrail-blocked. |
| H5 | fixed | Booster scheduling only considers `PrevSequence+1` and applies current goat eligibility/defer-state rules before inserting scheduled/deferred work. |
| H6 | fixed | Action Center due/overdue classification uses current time, not the query window, in both HTTP and repository paths. |
| H7 | fixed for active Action Center | Due/overdue bucket filtering is pushed into SQL mode before LIMIT, avoiding page starvation by the opposite bucket. The current admin-web Action Center uses the process-integrity contract with `next_cursor`; the older obligation read endpoint remains a bounded compatibility endpoint, not the active reviewer UI path. |
| H8 | fixed | Missed sweep excludes active in-progress batches and uses locked, non-poisoning batch scans. |
| H9 | fixed | `idempotency_keys` defaults/backfills to 90-day expiry and has a scheduled sweeper. |
| H10 | fixed | Default backfill pins `as_of` to the UTC day bucket, and unsafe version-id runs use durable generation-run rows. |
| H11 | fixed | Shift/recheck cancels open vaccination obligations that are no longer effective or eligible after the goat moves. |
| M1 | fixed | Cancel and re-scope paths emit deterministic `goat.obligations_canceled` / `obligation.rescoped` outbox rows with DB dedup indexes. |
| M2 | fixed | Completion transition writes an audit row in the same transaction as completed status/outbox. |
| M3 | fixed | Completed obligations reject a second distinct recorded completion before stock consume; accepted replay remains a no-op. |
| M4 | fixed | `protocol.version.published` outbox has a partial unique DB index and `ON CONFLICT DO NOTHING`. |
| M5 | workflow-tracked | The processed-event store does reclaim stale `processing` rows after the configured timeout, and handlers are expected to be idempotent. Side effects are not co-transactional with `domain_event_processed_events`; making every handler share one transaction or proving deterministic idempotency for each side effect remains tracked in `vaccination-workflow-followups.md`. |
| M6 | fixed | Shift handler rejects/ignores unsupported non-location scopes before watermark mutation; `shed` and `park` scopes are location-backed. |
| M7 | fixed | Missing-DOB/entry placeholder obligations are canceled by deterministic key when real source data produces a real due date. |
| M8 | fixed | Location idempotency keys now use the same 90-day horizon as the kernel default. |
| M9 | fixed | `rule_dsl` has a schema contract and is rejected on create/publish when invalid or unknown. |
| M10 | fixed | Config publish availability is backend-owned and permission-aware in admin-web; non-publishers see the disabled action instead of a late 403. |
| L1 | countered | Equal-timestamp shift events have no source causal sequence in the event contract; the runtime keeps a deterministic event-id tie-break and ignores stale/equal replays. No unsafe mutation or open Goal 1 data-loss path remains. |
| L2 | partial / tracked | Orphan app completion schemas/types were removed, generated clients were regenerated, and CI runs the contract drift check. A dedicated recurring orphan-schema lint remains tracked in `vaccination-workflow-followups.md`. |

### Direct Pasted Follow-Up Disposition Addendum

This addendum maps the additional pasted status/follow-up identifiers that were
not part of the C/H/M/L table.

| ID | Disposition | Evidence |
| --- | --- | --- |
| XLSX import | fixed | OCK-039: herd and shed drawers accept `.xlsx` through the shared worksheet-to-CSV parser and the herd-import security guard covers it. |
| Failed-row byte fidelity | fixed | OCK-038: failed-row export uses shared `parseCSVRecords` without trim-by-default and guard coverage asserts preserved spacing. |
| Duplicate CSV parsers | fixed | OCK-033: admin-web import parsing is centralized in `herd-import-utils.ts`; server action and failed-row export both delegate to it. |
| Impossible `failed` decision option | fixed | OCK-035: backend-owned config no longer exposes the unreachable `failed` decision and frontend failed-row filtering no longer accepts it. |
| Closure gate/doc active rows | fixed | The active OCK/RVF ledger is complete above; the lower intake table is explicitly historical and not the current unresolved list. |
| R6 | fixed | OCK-018: trusted completion evidence is resolved in a batched repository lookup per generation page instead of per goat/rule N+1. |
| R11 | fixed | OCK-020: status events reserve an idempotency key before insert, and replay/concurrency tests cover duplicate reserve behavior. |
| N3 | fixed | OCK-021: planned batch creation is serialized and guarded by `obligation_batches_unfinalized_planned_unique_idx`. |
| R9 | fixed | OCK-022: SOP-drive fanout stores `withdrawal_until_date` from SOP answers or protocol `withdrawal_days`; `TestRecordCompletionsFromVaccinationSessionTask` asserts the derived date. |
| R10 | countered | OCK-027/OCK-028: current Goal 1 runtime is Mesha/VGoats India-tenant scoped; location rows and contracts use `Asia/Kolkata` as an explicit business default while vaccination generation/calendar logic reads location/as-of data instead of silently changing vaccine due semantics. Multi-timezone expansion is not a Goal 1 kernel bug. |
| N6 | countered | Reject replay is already covered: same-key retry resumes a recorded completion and rejects it, while accepted/rejected terminal replays no-op without side effects in `completion_test.go` and Postgres completion integration tests. |
| Production Preventive Care (PC) schedules / Google dev-prod E2E / owner-facing stock UI | countered | OCK-041/OCK-042 and Lane C classify these as source-data/cloud/product-readiness gates, not unresolved local kernel/vaccination code bugs. The local Goal 1 acceptance and E2E path is green. |

Rules:

- Evidence must be concrete: file/line, migration, test name, command output
  summary, E2E screenshot/ledger path, or exact product/environment blocker.
- Duplicate rows must cross-reference the canonical row they close with.
- Conflicting reports must be resolved against current `main`, not chat memory.
- Product/prod gaps may be classified as blocked/non-code only when the doc says
  exactly what remains and why it is not a kernel/vaccination code blocker.
- The final response must report zero undisposed OCK/RVF rows.

Severity/status normalization:

- The intake table preserves historical review labels in one `Severity/status`
  column. The closeout ledger must split them into normalized values.
- Map `BLOCKER`, `CRITICAL`, `P1`, and ship-blocking durability/atomicity rows
  to `blocker`.
- Map `HIGH`, `P2`, and correctness/concurrency/security rows that can corrupt
  kernel state to `high`.
- Map `MEDIUM`, `LOW/MEDIUM`, `PARTIAL`, `CONFLICTING REPORTS`, `PENDING`, and
  `VERIFY` rows to the severity justified by current evidence, usually
  `medium` unless current code makes them blocking or low.
- Map `LOW`, `P3`, `NIT`, `LATENT`, and `COVERAGE DEBT` to `low` unless they
  invalidate a Done Definition gate.
- Map `DOC`, `DOC/TEST`, `PRODUCT GAP`, `PROD GAP`, `POLICY GAP`, `REVIEW GAP`,
  `TEST GAP`, and `REGRESSION` labels from current evidence, defaulting to
  `medium`. Security, data-loss, stock-leak, transaction-boundary,
  idempotency/replay, or kernel-atomicity regressions are `high` or `blocker`
  regardless of their historical label.
- For any unlisted label, assign severity from current evidence and explain the
  mapping in the closeout ledger; do not leave severity as a raw intake label.
- Use ledger status values only for state: `active`, `fixed`, `conflicting`, or
  `reverify`. Do not put raw labels like `PENDING` in the final Severity column.

### Ledger Lane Map

- Lane A1 maps to `OCK-001`.
- Lane A2 maps to `OCK-003` and re-verifies `RVF-002..RVF-004`.
- Lane A3 maps to `OCK-004`, `OCK-023`, and shift-related rows.
- Lane A4 maps to `OCK-005` and the local E2E/product proof matrix.
- Lane A5 maps to `OCK-006`, `OCK-024`, `OCK-029..OCK-031`.
- Lane A6 maps to `OCK-002` and `OCK-051..OCK-061`.
- Lane B maps to `OCK-007..OCK-040`, `OCK-062..OCK-087`, and all `RVF-*`
  re-verification rows not otherwise covered.
- Lane D maps to `OCK-041..OCK-050`.

Rows may appear in both a lane and the intake table. The closeout ledger is the
single disposition anchor.

### Reported-Fixed Cross-Reference Map

Use these links to avoid double-counting duplicate/conflicting claims:

- `RVF-001` closes or counters `OCK-019`.
- `RVF-008` closes or counters `OCK-027`.
- `RVF-009` closes or counters `OCK-026`.
- `RVF-011` closes or counters `OCK-012`.
- `RVF-012` closes or counters `OCK-014`.
- `RVF-013` closes or counters `OCK-015`.
- `RVF-014` closes or counters `OCK-013`.
- `RVF-015` closes or counters `OCK-018`.
- `RVF-002..RVF-004` must be reconciled with `OCK-003`, `OCK-068`, and booster
  rows.
- `RVF-005..RVF-007`, `RVF-016`, and herd-import OCK rows should share one
  evidence trail where they describe the same import-hardening behavior.
- `RVF-010` supports guardrail-doc closure for `OCK-029..OCK-030`.
- `RVF-017` supports stock-block and stock-resolution rows, especially
  `OCK-047` and `OCK-054..OCK-056`.
- `RVF-018` supports generation/protocol idempotency rows, especially
  `OCK-072`.
- `RVF-019` supports critical-health/guardrail rows, especially `OCK-006`,
  `OCK-024`, and `OCK-031`.
- `RVF-020` supports herd-import duplicate/tamper rows, especially `OCK-010`
  and `OCK-011`.
- `RVF-021` supports missed/audit/event rows, especially `OCK-019`.
- `RVF-001` and `RVF-021` both point at `OCK-019`; use one canonical evidence
  row for missed/audit/outbox closure and cross-reference the duplicate so it is
  not closed twice or left half-open.
- `RVF-022` is a standalone contract-drift re-verification row unless a touched
  OpenAPI/generated-client issue gives it a more specific OCK link.
- `RVF-023` supports `OCK-087` and migration-hardening proof: it must prove
  migration `000110` rejects deleting published protocol rules/triggers, not
  merely that the trigger exists.
- Any RVF not otherwise linked is a standalone re-verification row; it still
  needs its own closeout-ledger disposition.

If a reported-fixed row only partially closes an OCK row, mark the RVF as
`already closed with evidence` and keep the OCK row active with the remaining
gap.

### Required Execution Order

1. Reconstruct current repo state and read the required docs.
2. Verify/dedupe the 110 intake rows against current `main`.
3. Implement all Lane A items and fix every valid blocker/critical/high
   kernel/vaccination issue; fix medium/low when cheap, otherwise ledger them
   with evidence and owner/blocker.
4. Fill the closeout ledger for every OCK/RVF row.
5. Run the full local verification matrix.
6. Spawn the extra-high senior-architect review using
   `context/execution/goal1-senior-architect-review-prompt.md`.
7. Fix every valid Goal 1 finding from that review. Blocker/critical/high
   findings must be fixed, not merely ledgered. Medium/low findings are fixed
   when cheap, otherwise ledgered only if they do not violate any Done
   Definition gate.
8. Rerun affected verification and the senior-architect review until it finds no
   valid blocker/critical/high Goal 1 issue and every lower-severity issue is
   fixed or explicitly ledgered.
9. Run the full local vaccination E2E matrix. If E2E finds a bug, fix it and
   return to step 5.
10. Run the pre-push authority gate: branch, remote, target `vgoats/goatos`, and
   Mesha/VGoats token path/command.
11. Commit and push to main with the Mesha/VGoats path.
12. Verify `origin/main` equals the pushed commit. Prefer GitHub Actions/CI
    green for that commit. If GitHub Actions cannot start because of
    account/billing/spending-limit state, run the local CI-equivalent gate set
    in "CI/CD Post-Push Gate" and record that evidence before Goal 2 starts. If
    CI starts and reports a real code/test failure, Goal 1 is not done: fix,
    rerun local verification, rerun senior review/E2E as applicable, commit,
    push, and check CI/local equivalent again.

### Historical Issue Intake Rows

This table is the original bug intake, preserved so reviewers can audit every
claim. It is not the current unresolved list after closeout. Current
dispositions are the `Required Closeout Ledger` above; rows below that still
say "pending", "product gap", or "prod gap" are historical claim text, not an
instruction to defer those bugs to Goal 2.

| ID | Severity/status | Issue to verify and close |
| --- | --- | --- |
| OCK-001 | BLOCKER | Accept completion is not atomic: record/accept, stock consume, obligation completion, outbox, and booster scheduling can split across transactions. |
| OCK-002 | CRITICAL | Verification fanout still uses in-process eventbus instead of durable outbox, so accept/reject side effects can be lost on crash. |
| OCK-003 | BLOCKER | Booster scheduling still runs synchronously in accept flow instead of from a `vaccination.completed` consumer. |
| OCK-004 | HIGH | Shift/re-scope has no monotonic `occurred_at` ordering fence or shift-event idempotency. |
| OCK-005 | BLOCKER | Full local four-goat/procurement negative E2E matrix is still not proven by the smoke path. |
| OCK-006 | BLOCKER | Critical death, ICU, quarantine, contagious-isolation guardrail workflow is documented but not fully implemented. |
| OCK-007 | PENDING | Cohort generation must not create vaccination obligations for exited/dead/sold/lost goats. |
| OCK-008 | PENDING | Default config authoring must not silently narrow health eligibility to `healthy` when `any` is intended. |
| OCK-009 | PENDING | Backend-owned config must expose `sick` defer where DB/code/docs support sick deferral. |
| OCK-010 | PENDING | Herd import commit must be server-bound to the exact previewed CSV/rows/hash and reject direct tamper. |
| OCK-011 | PENDING | Herd import security gate must be behavioral, not only source-regex checks. |
| OCK-012 | CONFLICTING REPORTS | Late accepted completion evidence suppression is reported fixed in one pass and pending in another; re-check current main and document evidence. |
| OCK-013 | PENDING | Repeat policy must implement exposed `every_n_days`; unsupported age-window repeats must be rejected at authoring/publish/DB with honest errors. |
| OCK-014 | CONFLICTING REPORTS | Direct `Reject` replay is reported fixed in one pass and pending in another; re-check current main. |
| OCK-015 | CONFLICTING REPORTS | Goat Passport deferred/missed visibility is reported fixed in one pass; re-check against current main and read models. |
| OCK-016 | HIGH | Vaccination Execution and Process Integrity read models still omit or undercount canonical `deferred` in core filters/counts in some reviews. |
| OCK-017 | PARTIAL | Calendar, Process Integrity, and Vaccination Execution reconstruct missed/deferred/effective status with divergent vocab instead of a shared effective-status contract. |
| OCK-018 | CONFLICTING REPORTS | Trusted-completion evidence scale risk is reported pending in one pass and batched/improved in another; verify no goat x rule N+1 remains. |
| OCK-019 | CONFLICTING REPORTS | Missed/overdue durable outbox is reported fixed; re-check event emission and downstream alert dependency. |
| OCK-020 | HIGH | `obligation_status_events` needs replay/concurrency idempotency, unique/fingerprint guard, or `ON CONFLICT` behavior. |
| OCK-021 | HIGH | Sweeper due scan lacks leasing/`FOR UPDATE SKIP LOCKED` and unique open planned-batch guard, risking duplicate batches. |
| OCK-022 | MEDIUM | Withdrawal/cold-chain fields are plumbed but may not be populated on SOP-drive verification path. |
| OCK-023 | MEDIUM | Malformed shift payload must produce observable failure/log/DLQ instead of silent no-op. |
| OCK-024 | HIGH | Death can be recorded through unreconciled legacy and future guardrail paths unless one path is blocked/wrapped/reconciled. |
| OCK-025 | MEDIUM | Separation-of-duties check may depend on unreliable shed-owner data. |
| OCK-026 | CONFLICTING REPORTS | Duplicate vaccination cycle suppression is reported fixed by cycle fence; re-check prior-cycle evidence cannot suppress a future cycle. |
| OCK-027 | CONFLICTING REPORTS | Protocol effective-version timezone hardcode is reported fixed; re-check all non-test `Asia/Kolkata` literals and location fallback semantics. |
| OCK-028 | MEDIUM | Due-date/calendar logic may still use UTC or non-location timezone assumptions in some paths. |
| OCK-029 | DOC/TEST | New critical guardrail rules need acceptance tests. |
| OCK-030 | DOC | Example quarantine pack still appears to let operator pick classification instead of system computing it from evidence. |
| OCK-031 | LOW/MEDIUM | Guardrail-required death/ICU/quarantine endpoints should return truthful 4xx conflict/validation errors, not misleading `501`. |
| OCK-032 | LOW | One history query has no time floor and may scan too far back at scale. |
| OCK-033 | NIT | Server/client CSV parsers remain duplicated and must not drift. |
| OCK-034 | NIT/LATENT | Multiline quoted CSV fields can desync Go physical line numbers from UI record indexes and shift failed-row export. |
| OCK-035 | NIT | `failed` decision option exists in herd register option groups but no row can produce it. |
| OCK-036 | VERIFY | Blank-only shed CSV was reported fixed; re-check behavior stays aligned with goat import. |
| OCK-037 | VERIFY | Duplicate bulk-file hash regex was reported fixed; re-check only one SHA-256 regex source remains. |
| OCK-038 | P3 | Failed-row export trims cells and is not byte-faithful to original raw CSV cells. |
| OCK-039 | PRODUCT GAP | XLSX/Excel upload is still not implemented; herd/shed bulk import remains CSV-only unless explicitly scoped. |
| OCK-040 | COVERAGE DEBT | Untested frontend surfaces remain: `RuleEditorModal`, `parseCSVRecords`, `downloadFailedRows`, `BulkImportDrawer`, `ShedImportDrawer`. |
| OCK-041 | PRODUCT GAP | Production source-backed Preventive Care (PC) vaccination schedules/rules/data are not fully loaded and published. |
| OCK-042 | PRODUCT GAP | Operator/admin execution UI for start SOP, upload proof/video, submit, and recover from validation errors must be modern, mock-aligned, backend-contract-owned, target-reviewer usable, and E2E-tested when part of the vaccination acceptance path; otherwise mark the flow unavailable. |
| OCK-043 | PRODUCT GAP | End-to-end proof upload flow must run create upload, PUT file, complete upload, submit, verify through real UI/contracts. |
| OCK-044 | PROD GAP | Workers/schedulers for generation, consumers, sweepers, overdue marking, reminders, escalations, notifications, and projections must be deployed and healthy. |
| OCK-045 | PROD GAP | Real notification/alert channels must be configured, retryable, and visible; local stubs are not production delivery. |
| OCK-046 | PRODUCT GAP | Calendar, Action Center, Protocol Adherence, Workflow, Control Tower, Goat detail, and Vaccination Execution need modern mock-aligned state coverage for due/missed/deferred/blocked/pending-proof/pending-verification/completed states. |
| OCK-047 | PRODUCT GAP | Stock blocker resolution workflow/owner action must be visible and executable, not only recorded as backend state. |
| OCK-048 | POLICY GAP | Open work after rule/SOP version change needs explicit continue/cancel/supersede/regenerate policy and product flow. |
| OCK-049 | POLICY GAP | In-progress/completed batched-work shift edge cases need explicit repair/rework handling. |
| OCK-050 | PROD GAP | Production readiness still needs deployed environment, real config, UI completion, worker monitoring, alerting, E2E proof, and rollback/replay runbooks. |
| OCK-051 | CRITICAL | Procurement-excluded recovery/reopen path can hit DB guard SQLSTATE 23514 and poison/infinite-nack without prefilter or handled error. |
| OCK-052 | HIGH REGRESSION | Missed sweep including `in_progress` can let one excluded-goat 23514 roll back the whole tenant missed-sweep batch. |
| OCK-053 | HIGH | Domain consumer claim, side effect, and finalize are separate transactions; stale `processing` reclaim can double-apply effects. |
| OCK-054 | HIGH | FEFO reserve and operator-selected lot consume can mismatch, causing reservation mismatch and stock leak. |
| OCK-055 | HIGH | Missed/waived obligations do not release reserved stock. |
| OCK-056 | HIGH | Recovery reopen can set `batch_id=NULL` without stock reconcile marker, orphaning reserved doses. |
| OCK-057 | HIGH | Shift repair may not cancel destination-ineligible obligations with `ineligible_after_shift`. |
| OCK-058 | HIGH | Calendar reminders may fire only once per lifetime instead of re-arming while work remains open. |
| OCK-059 | HIGH | Resolving escalation while work stays open can permanently silence future alerts. |
| OCK-060 | HIGH | Stock movement idempotency uses `ON CONFLICT DO NOTHING` without semantic fingerprint, hiding same-key different-payload conflicts. |
| OCK-061 | HIGH | Seven-day idempotency TTL reap can allow replay to rerun side effects. |
| OCK-062 | MEDIUM | Incident routing behavior remains pending/under-reviewed. |
| OCK-063 | MEDIUM | Recurrence materialization remains pending/under-reviewed. |
| OCK-064 | MEDIUM | Inventory reconciler 3s timeout remains pending/under-reviewed. |
| OCK-065 | MEDIUM | Batch lifecycle behavior remains pending/under-reviewed. |
| OCK-066 | MEDIUM | Gross-vs-net stock release calculation remains pending/under-reviewed. |
| OCK-067 | MEDIUM | Placeholder orphan behavior remains pending/under-reviewed. |
| OCK-068 | MEDIUM | Booster idempotency/key design remains pending/under-reviewed. |
| OCK-069 | LOW/MEDIUM | Retention policy remains pending/under-reviewed. |
| OCK-070 | MEDIUM | JSON Schema coverage/validation remains pending/under-reviewed. |
| OCK-071 | MEDIUM | In-trigger fanout behavior remains pending/under-reviewed. |
| OCK-072 | MEDIUM | Protocol-write idempotency remains pending/under-reviewed. |
| OCK-073 | LOW/MEDIUM | Superseded by the clean-slate Animal ID 1/2 contract: identifier values are globally single-use for life, never park-scoped, never reused after death/sale/exit/tag loss, and checked against current plus historical identifiers. |
| OCK-074 | LOW | Dead `next_due_basis` code/field remains pending/under-reviewed. |
| OCK-075 | MEDIUM REGRESSION | Repeat CHECK migration can abort on existing `until_age`/`after_age` data because it adds constraint without pre-clean/backfill. |
| OCK-076 | MEDIUM REGRESSION | Herd commit may have narrowed from per-row resilience to all-or-nothing rejection on first invalid preview row. |
| OCK-077 | LOW REGRESSION | Preview HMAC may reject legitimate row reorder or normalization drift because commit must echo exact preview order. |
| OCK-078 | LOW REGRESSION | `every_n_days` next-cycle fallback may use `OffsetDays` as period when `MinGapDays <= 0`. |
| OCK-079 | HIGH | `next_cycle` catch-up can churn obligation keys as `as_of` advances, creating duplicate open obligations across cycles. |
| OCK-080 | MEDIUM | Missed work cells map to `WorkStateBlocked`/`blocked`, which mislabels missed-deadline state as dependency/stock blocked. |
| OCK-081 | LOW/MEDIUM | Herd `file_hash` is client-asserted; server integrity relies on row HMAC and may not recompute CSV bytes at preview. |
| OCK-082 | LOW | Preview token has no TTL/nonce; replay is mostly defanged by idempotent commit but should be consciously accepted or fixed. |
| OCK-083 | LOW/MEDIUM | Constructing admin-goat service without bulk preview signing key can produce internal preview errors outside bootstrap wiring. |
| OCK-084 | LOW | Unsupported repeat policy may be rejected as generic `not_publishable` instead of specific `unsupported_repeat_policy`. |
| OCK-085 | LOW/MEDIUM | Impact preview counting may be narrower than generation if legacy in-care lifecycle statuses are expected. |
| OCK-086 | REVIEW GAP | Eight recent feature commits touched security-adjacent identity/authz/bootstrap/obligation paths and need counter-review. |
| OCK-087 | TEST GAP | Integration tests still skip without Docker and do not cover new DELETE triggers, missed sweeper, death cancel, shift, DLQ, or replay-after-expiry lanes. |

### Historical Reported-Fixed Claims To Re-Verify

These rows were original re-verification claims and are retained for audit. The
current evidence-backed dispositions are in the `Required Closeout Ledger`
above.

| ID | Reported fixed claim |
| --- | --- |
| RVF-001 | `obligation.missed` outbox event and migration were added. |
| RVF-002 | Booster scheduling is gated by completed obligation state. |
| RVF-003 | Sparse-sequence booster matching no longer requires `Sequence == PrevSequence + 1`. |
| RVF-004 | Cross-protocol booster timing issue is effectively mooted by anchoring to passed `AdministeredAt`. |
| RVF-005 | Blank-only shed CSV preview now errors. |
| RVF-006 | Bulk file hash regex was collapsed to one SHA-256 regex. |
| RVF-007 | Unterminated quoted CSV parsing now throws in the client parser. |
| RVF-008 | Effective protocol timezone query now reads location timezone with India fallback. |
| RVF-009 | Duplicate-cycle suppression gained a cycle fence for repeat rules. |
| RVF-010 | Primitive exposure plan was mirrored into the canonical guardrail rule list. |
| RVF-011 | Late accepted evidence suppression no longer requires `administered_at <= due_at`. |
| RVF-012 | Direct `Reject` replay resumes/rejects recorded completions. |
| RVF-013 | Goat Passport open obligations include `deferred` and `missed`. |
| RVF-014 | `every_n_days` repeat is implemented and unsupported age-window values are blocked. |
| RVF-015 | Trusted-evidence checks were batched/improved for generation scale. |
| RVF-016 | Preview-token binding, stale-preview guard, and SHA-256/content hardening landed for herd import. |
| RVF-017 | Stock-block retry/backoff and `ClearBatchStockBlock` on success landed. |
| RVF-018 | StartGenerationRun detects same-key different-request hash conflicts. |
| RVF-019 | ICU/quarantine exit guard checks current and target health state. |
| RVF-020 | Herd import commit scans for in-file duplicates. |
| RVF-021 | `MarkMissedBefore` writes an in-transaction audit event. |
| RVF-022 | OpenAPI, generated clients, and server fields were reported aligned for touched changes. |
| RVF-023 | Migration `000110` published-delete guard is proven by an integration test or equivalent DB assertion: deleting rules/triggers from a published protocol version raises the expected guard error. |

## Lane A - Must Implement For Stable Kernel

### A1. Accept completion atomicity

Current risk to verify: accept completion still spans separate side effects:
record/accept, stock consume, obligation complete, and booster scheduling. This
is better than the old state if each step is idempotent, but it is not stable
kernel behavior if a crash can leave accepted completion, stock, obligation, and
outbox in contradictory states.

Target:

- One accept command boundary must commit canonical completion state, audit,
  idempotency record, obligation completion/status event, stock movement, and
  `vaccination.completed` outbox in one safe transaction; or, if a true single
  transaction is impossible, a durable saga table must make every intermediate
  state visible, retryable, and non-claimable as final completion.
- Failed stock consume must not leave an accepted completion.
- Failed obligation completion must not leave invisible accepted work.
- Replay of the same accept must converge without duplicate stock movement,
  duplicate status events, duplicate outbox, or duplicate booster.

Required tests:

- happy accept commits all rows together.
- injected stock failure leaves no accepted completion and no completed
  obligation.
- injected post-completion failure is replayed to a consistent final state.
- duplicate accept/replay is a no-op with the same response semantics.
- concurrent accept attempts produce one accepted completion and one stock
  consume.

### A2. Booster scheduling from `vaccination.completed`

Current risk to verify: booster scheduling still runs synchronously in the
accept path. That couples booster creation to accept-side timing and can produce
phantom boosters if completion state is not final.

Target:

- Accept path emits exactly one durable `vaccination.completed` event/outbox row
  only after the primary obligation is truly completed.
- A consumer handles `vaccination.completed` and schedules boosters/follow-ups.
- Consumer is idempotent on event id/completion id and protocol/rule identity.
- Missed, canceled, deferred, rejected, or failed completions do not generate
  boosters.
- Booster timing uses actual `administered_at`, not planned due date.

Required tests:

- accepted completion emits event and consumer creates booster.
- replayed event creates no duplicate booster.
- canceled/missed/rejected primary creates no booster.
- cross-protocol evidence does not schedule the wrong booster.
- sparse sequence or renamed dose does not silently drop a valid booster if the
  protocol declares a valid upstream link.

### A3. Shift event ordering fence

Current risk to verify: goat shift re-scope can process out-of-order events and
move pending work back to an older shed.

Target:

- Location/shift events include event id, source history id, and `occurred_at`.
- Shift/re-scope handler records processed shift identity and last accepted
  ordering marker per goat or obligation scope.
- Stale events are ignored or recorded as stale without mutating current work.
- Rapid double-shift final state follows latest `occurred_at`.
- Direct destination-batch merge/reuse remains idempotent and stock-reconcile
  safe.

Required tests:

- shift A then B delivered in order ends in B.
- shift A then B delivered out of order still ends in B.
- replay of A or B is a no-op.
- stock-reserved planned batch re-scope marks exactly one reconciliation path.
- malformed shift payload logs/records a durable failure instead of silent
  no-op.

### A4. Full local E2E matrix

Current risk to verify: smoke tests passed, but smoke is not the full
four-goat/procurement negative matrix.

Target local E2E must exercise real backend contracts, workers/consumers, DB
state, generated clients, and admin-web paths. Do not fake UI-only rows.

Minimum scenarios:

- healthy due goat: protocol publish/generation -> due obligation -> shed batch
  -> SOP proof -> verification accept -> stock consume -> obligation complete
  -> `vaccination.completed` -> booster/follow-up -> read models.
- sick/ICU/quarantine goat: visible deferred obligation/reason, no silent skip,
  recovery/recheck path documented and tested where producer exists.
- stock blocked goat/batch: missing or expired stock blocks correctly, does not
  abort sibling batches, and repair/reconcile path is visible.
- exited goat: dead/sold/lost/excluded goats do not receive new cohort work and
  open work is canceled without ghost overdue.
- late accepted evidence: accepted late completion suppresses regenerated
  duplicate work according to the intended trusted-evidence policy.
- stale/tampered import and direct server-action calls cannot bypass preview or
  hash binding where herd/import paths are in scope.

Required proof:

- backend assertions against canonical tables and read models.
- admin-web proof for Action Center, Calendar, Protocol Adherence, Workflow,
  Control Tower, Goat Passport/detail, and Vaccination Execution where each
  state should appear.
- screenshot/ledger paths if UI is touched.
- explicit statement of anything intentionally out of scope.

### A5. Critical animal-action guardrails

Current risk to verify: death, ICU, quarantine, contagious isolation, and similar
critical transitions are documented as kernel guardrails, but parts of runtime
still return guardrail/501-style blocks or permit legacy side paths.

Target:

- Implement the generic critical-action guardrail engine enough to prove the
  contract with at least one real pack, preferably death plus
  quarantine/ICU/contagious isolation if current product scope requires them.
- A critical action request records source-backed reason, computed
  classification authority, primitive exposure/bypass policy, required proof,
  verification/approval, obligations/tasks, SLA/escalation, audit, outbox, and
  read-model visibility.
- Legacy direct paths are blocked, wrapped, or reconciled so the same animal
  state cannot be recorded two incompatible ways.
- Guardrail decisions must be deterministic from evidence; humans may supply
  evidence and approve where required, but must not manually pick the final
  classification when the policy says the system computes it.

Required tests:

- valid critical action creates audit/outbox/obligation/proof/review state.
- missing required evidence on an implemented guardrail path blocks with a
  truthful 4xx. A genuinely unimplemented critical-action path may return an
  explicit not-implemented/guardrail-required response, but that path cannot be
  counted complete.
- legacy/direct endpoint cannot bypass the guardrail.
- replay is idempotent and does not duplicate obligations/notifications.
- policy-pack examples and docs match runtime behavior.

### A6. Poison, replay, stock, and alert durability lane

Maps to `OCK-002` and `OCK-051..OCK-061`. These rows are high-risk enough that
they must not remain one-line ledger notes.

Current risks to verify:

- SQLSTATE 23514 guard-trigger failures can poison recovery/reopen and missed
  sweeper paths.
- Verification fanout can publish accept/reject side effects through an
  in-process eventbus instead of durable outbox.
- Domain-consumer claim/effect/finalize split can double-apply side effects.
- Stock reserve/consume/release/reopen paths can leak or silently mask
  same-key-different-payload conflicts.
- Reminder/escalation state can silence still-open work.
- Idempotency TTL can make replay unsafe.

Target:

- Recovery/reopen and missed-sweep paths pre-filter ineligible/procurement-
  excluded obligations or catch expected DB guard errors as durable per-row
  failures, never as whole-tenant poison batches.
- Verification fanout side effects that must survive crash/replay use durable
  outbox or a transactionally recorded repair/replay mechanism, not a disposable
  in-process bus.
- Domain consumer either performs claim/effect/finalize safely in one
  transaction boundary or records durable progress so reclaimed work cannot
  double-apply non-idempotent side effects.
- Reservation, consume, release, missed, waived, recovery, and reopen paths
  conserve stock and produce stock-reconciliation markers when automatic repair
  is unsafe.
- Stock movement idempotency stores a semantic fingerprint and rejects same-key
  different-payload replays.
- Reminder/escalation resolution does not permanently silence still-open work;
  re-arm or next-escalation behavior is explicit and tested.
- Idempotency retention cannot allow a valid replay window to rerun side
  effects, or expired replay behavior is safely rejected.

Required tests:

- one poisoned obligation cannot roll back an entire tenant missed-sweep batch.
- verification accept/reject fanout survives crash/retry and cannot be lost
  after the verification decision commits.
- procurement-excluded recovery/reopen does not infinite-nack.
- stale domain-consumer reclaim cannot double-create obligations, stock
  movements, notifications, or completion side effects.
- FEFO-reserved lot mismatch with operator-selected lot is reconciled or blocked
  without stock leak.
- missed/waived/canceled/reopened reserved work releases or reconciles stock
  exactly once.
- same idempotency key with different stock movement payload fails loudly.
- reminder/escalation remains active or re-arms for still-open work after
  acknowledgement/resolution.
- replay after idempotency retention expiry is rejected or still safe.

## Lane B - Historical Findings To Re-Counter Or Close

Do not assume these are still open. Do not assume they are closed. Re-check each
against current `main`, then either fix or write the counter-evidence in the
final report and any living ledger changed by the work.

| OCK id(s) | Area | Claim to re-check |
| --- | --- | --- |
| OCK-007 | Cohort generation | Exited goats cannot receive cohort-generated vaccination obligations. |
| OCK-008 | Config authoring | Default health eligibility cannot silently narrow to `healthy` when `any` is intended. |
| OCK-009 | Sick defer authoring | Backend-owned config exposes `sick` defer where DB/code/docs support it. |
| OCK-010 | Herd import | Commit is server-bound to previewed CSV/hash; direct tamper cases fail. |
| OCK-011 | Herd import tests | Security gate is behavioral, not only source-regex. |
| OCK-012 | Late evidence | Accepted late completion suppresses regenerated duplicate work when policy says it should. |
| OCK-013 | Repeat policy | `every_n_days` works if exposed; unsupported age-window repeats are rejected at authoring/publish/DB and documented. |
| OCK-014 | Direct reject replay | `Reject` replay does not leave completion stuck `recorded`. |
| OCK-015..OCK-017, OCK-080 | Read models | Passport, Action Center, execution, Calendar, and process views use canonical statuses consistently, including `deferred`/`missed` where product expects them. |
| OCK-018, OCK-032, OCK-079 | Trusted evidence and history-query scale | Generation does not do per-goat/per-rule N+1 evidence lookups at million-goat scale; history queries have bounded/indexed time floors where needed; repeat/cycle query plans remain stable. |
| OCK-019, OCK-058..OCK-059, OCK-062 | Missed events | Missed/overdue transitions emit durable events/outbox where alerts/escalation depend on them and do not permanently silence still-open work. |
| OCK-020 | Status-event idempotency | `obligation_status_events` cannot duplicate under replay/concurrency. |
| OCK-021 | Batch sweeper concurrency | Due scan and batch reuse cannot create duplicate open batches under concurrent workers. |
| OCK-003, OCK-068 | Booster correctness | Booster lookup is scoped to the correct protocol/version/rule and handles sparse sequence/upstream identity correctly. |
| OCK-027..OCK-028 | Timezone | Effective protocol/version and due-date/calendar logic are location/tenant-timezone aware or explicitly India-only with a tracked product constraint. |
| OCK-022 | Withdrawal/cold chain | SOP verify path populates withdrawal/cold-chain fields where contracts require them. |
| OCK-004, OCK-023, OCK-057 | Shift payloads | Malformed or out-of-order shift payloads produce observable failure and deterministic ordering, not silent success. |
| OCK-006, OCK-024, OCK-031 | Death paths | Death cannot be recorded through unreconciled legacy and guardrail paths, and incomplete guardrail paths expose truthful errors. |
| OCK-025 | Separation of duties | Verification/approval separation does not depend on unreliable shed-owner data alone. |
| OCK-026 | Duplicate vaccination cycles | Duplicate-check logic cannot let an old cycle suppress a valid new cycle. |
| OCK-029..OCK-030, OCK-087, RVF-023 | Guardrail docs/tests | Architecture doc, guardrail feature doc, examples, migration triggers, and acceptance tests agree on classification authority and primitive exposure. |
| OCK-033..OCK-038 | CSV nits | Blank-only shed CSV, duplicate parser drift, duplicate hash constants, failed-row fidelity, and impossible `failed` decision state are either fixed or documented as non-blocking. |
| OCK-039..OCK-040 | Frontend/import coverage | XLSX scope and import UI coverage debt are fixed if in scope or classified as non-blocking product/coverage debt. |
| OCK-062..OCK-087, RVF-* | Remaining historical/reverify rows | Re-check every remaining row against current `main`; add canonical evidence or a fix in the closeout ledger. |

## Lane C - Explicit Non-Goals Unless Product Reopens Them

These should not be half-implemented to make a checklist look green:

- Age-window repeat support. Stable closure is to reject it consistently at
  authoring/publish/DB until generator support is implemented.
- Vendor-specific incident two-way sync. Generic webhook incident adapters are
  acceptable unless a vendor integration is explicitly requested.
- Production/staging Google Cloud apply. Code and local proof are not the same
  as environment rollout; do not mutate cloud unless separately asked.
- Silent migration of already in-progress/completed drives after goat shift.
  That needs explicit exception/rework policy because proof and stock may
  already be in-flight.

## UI Quality Standard For Dev

Do not use "basic UI", "half-polished", or "temporary developer panel" as an
escape hatch for any flow included in the dev vaccination/kernel slice.

For this handoff, UI has only two acceptable states:

- Built flow: modern, mock-aligned, backend-contract-owned, usable by the target
  reviewer, and included in E2E where the flow is part of the vaccination
  acceptance path. It must follow `mock/goatos-dashboard-mock.html` anatomy:
  density, command-center layout, navigation feel, table/card patterns,
  state chips, empty/error states, and Goat OS visual language.
- Not built flow: explicitly marked unavailable/out of this dev slice in the
  closeout ledger and CEO guide. Do not ship an ugly/basic placeholder and call
  it "polish later."

If a button or action is required to prove the vaccination flow, it is part of
the built flow and must meet the standard above. If it is not built, the guide
must say what cannot be tested through UI and what audited backend/API path, if
any, was used for E2E proof.

## Lane D - Product And Production Readiness Gaps

These are not all the same type of issue. Some are required before calling the
vaccination product shipped; some are environment rollout; some are explicit
policy/UX decisions. The next session must classify each item as `fixed`,
`already closed with evidence`, `valid product gap`, or `explicitly out of
scope for this kernel pass`. Do not let backend-kernel readiness masquerade as
full production readiness.

| Item | Required closure question |
| --- | --- |
| Production vaccination rules/data | Are real source-backed, Preventive Care approved vaccination schedules loaded, published, versioned, and protected from fake/default production values? If not, document the exact remaining data/publish task and do not call production vaccination rules complete. |
| Operator execution UI | Can an operator/admin start SOP work, see the correct work context, upload required proof, submit execution data, and recover from validation errors through a modern, mock-aligned, backend-contract-owned, target-reviewer usable, E2E-tested product UI? If not, is the flow explicitly marked unavailable rather than hidden behind a basic placeholder? |
| End-to-end proof upload flow | Does the product path actually run `createProofUpload -> PUT upload_url -> completeProofUpload -> submit/verify`, with file upload on admin-web and the correct operator-mobile expectation, instead of proof state existing only in backend rows? |
| Worker/scheduler deployment | Are generation, domain-event consumers, obligation sweepers, overdue/missed marking, reminders, escalations, notification dispatch, and read-model refresh workers running in the tested environment with leases/retry/DLQ/health visibility? |
| Alerts/notification delivery | Do reminder/escalation rows reach configured real channels in the target environment, and are failures retryable/visible? If only local stubs exist, mark production delivery pending. |
| Calendar / Action Center UI | Do Calendar, Action Center, Protocol Adherence, Workflow, Control Tower, Goat detail, and Vaccination Execution show due/missed/deferred/blocked/pending-proof/pending-verification/completed states consistently from backend contracts in the mock-aligned UI? |
| Stock issue resolution UX | When stock is missing, expired, insufficient, reserved, released, reconciled, or repaired, is the next owner/action visible and executable in a modern, mock-aligned, backend-contract-owned, target-reviewer usable, E2E-tested UI, not just recorded as a backend block? If the owner action is not built, is it explicitly unavailable? |
| Rule-change handling for open work | When a new rule/SOP version publishes, is there an explicit continue/cancel/supersede/regenerate policy and product flow for already-open work? Completed history must remain under the old version. |
| Shed shift edge cases | Are open/planned shift repairs complete, and are in-progress/completed batched-work edge cases either implemented through explicit repair/rework or clearly blocked by policy instead of silently mutated? |
| Production readiness | Has the target environment been deployed with real config, workers, secrets, notification channels, monitoring, E2E proof, and rollback/replay runbooks? If not, state "backend/local kernel ready, production rollout pending." |

Lane D closeout must be honest in the final report. A valid answer can be
"not part of this code pass", but it must name the owner, evidence, and remaining
work. If the user asks to make the whole vaccination product production-ready,
Lane D becomes blocking.

## Review Agent Instruction

Use this only after all OCK/RVF rows have preliminary dispositions and the full
local verification matrix has passed or produced explicit external blockers.
The review must inspect the entire project impact, not just the edited files.

Use the committed review prompt:

```text
context/execution/goal1-senior-architect-review-prompt.md
```

That prompt requires a 9-finder adversarial review plus synthesis/verify pass
over the last 15 commits, operational kernel, vaccination slice, contracts, and
E2E. Run it read-only. The synthesis output must be bad-only findings with
file:line evidence and contradictions resolved against current code.

If the review finds valid Goal 1 issues, fix them and run the review again. The
goal is not finished while a valid kernel-stability or vaccination blocker,
critical, or high finding remains. Medium/low findings are fixed when cheap,
otherwise logged in the closeout ledger only when they do not violate a Done
Definition gate. If a medium/low finding reveals a recurring pattern,
unbounded scale risk, missing critical proof, or CI/E2E gate failure, treat it as
blocking and rerun the full review loop.

After the review loop is clean, run the local E2E matrix. If E2E finds any
valid Goal 1 bug, fix it, rerun the affected verification, rerun the
senior-architect review, and then rerun E2E. Do not start Goal 2 until this loop
is clean and the post-push gate is satisfied: CI/CD green when GitHub Actions
can start, or the local CI-equivalent gate set is green when Actions is blocked
by account/billing/spending-limit state.

## Verification Matrix

Run the strongest local matrix available. If a command is blocked by local hooks
or environment, record the blocker and run the closest equivalent.

Backend/code:

```bash
git diff --check HEAD --
make sqlc-generate
git status --short
make check
bash tools/agent-hooks/check-boundaries.sh
bash tools/agent-hooks/check-contract-drift.sh
make sqlc-check
make validate-sqlc-plans
make validate-migrations
make api-client-check
go test ./...
```

`git diff --check HEAD --` only checks tracked diffs plus staged new files. For
new/untracked files, stage explicit paths first, then rerun
`git diff --check HEAD --`. Use `git diff --check --no-index /dev/null
<new-file>` only as an early diagnostic before staging, and treat any whitespace
diagnostic output as failure. Do not treat the raw `--no-index` exit code alone
as proof, because content differences can also return non-zero.

After any code generation command, inspect `git status --short` and generated
file diffs. If generated drift is intentional, stage the explicit generated
paths. If no generated drift is intended, `git diff --exit-code -- <generated
paths>` must pass. Before final review/push, there must be no uninspected
generated drift.

Critical integration proof rule: Docker-backed pgtest lanes for booster
consumer correctness (`OCK-003`), all A6 durability/stock/replay rows
(`OCK-002`, `OCK-051..OCK-061`), plus `OCK-001`, `OCK-024`, and `RVF-023`, must
execute and pass against Postgres, or an
equivalent documented Postgres harness must execute and pass. More generally,
ANY Lane A6 Required-test implemented as a pgtest/Postgres lane must execute,
not skip — the stock-leak and alert lanes are DB-state proofs and are just as
prone to silent `t.Skip` as the poison/durability lanes. A skipped pgtest lane is
not a pass. If Docker or the equivalent harness is genuinely unavailable, each
skipped critical/A6 lane must be marked `blocked` individually in the closeout
ledger with the exact reason; Goal 1 is blocked, not complete, and Goal 2 must
not start.

Focused backend packages should include identity, vaccination, obligation,
inventory, SOP bridge/verification, protocol, outbox/domain consumer,
notification/escalation, calendar/read models, process integrity, and
vaccination execution.

Frontend/admin-web if UI/contracts are touched:

```bash
npm --prefix apps/admin-web ci
npm --prefix apps/admin-web run lint
npm --prefix apps/admin-web run typecheck
npm --prefix apps/admin-web run check:mock-fidelity
GOATOS_BEARER_TOKEN=sentinel-mesha-admin-token npm --prefix apps/admin-web run build
```

CI mirror:

- `.github/workflows/ci.yml` currently runs agent guardrails, large-file guard,
  Go tests, pinned `sqlc` install, `make sqlc-check`,
  `make validate-sqlc-plans`, `make validate-migrations`, `git diff --check`,
  admin-web install, lint, typecheck, `check:mock-fidelity`, and build.
- Local verification must run the same effective gates before push, including
  `check:mock-fidelity`; mock fidelity is a repo rule and a CI gate.
- The admin-web job intentionally has no path filter. Backend-only pushes still
  need the admin-web gate green so `main` never carries stale/broken UI truth.
- If local and CI disagree after CI jobs actually start, CI wins: fix the real
  issue or update the workflow only when the workflow itself is wrong and the
  change is reviewed. If CI jobs cannot start because of GitHub account/billing
  state, use this local CI-equivalent set as the post-push gate.

E2E/local chain:

- Run the documented local vaccination business-chain script if still current.
- Run or implement the full four-goat/procurement negative matrix from the
  active E2E docs.
- Exercise outbox relay/domain-event delivery, trigger evaluation, sweepers,
  read-model refresh, and UI action paths. Logging an outbox row is not enough.

Docs:

- Update implementation ledgers only with truth verified against code.
- Update `context/README.md` if a new canonical context doc is added.
- Update protocol / Preventive Care (PC) guardrail docs if implementation changes the contract.
- Keep wiki/source facts as evidence; do not claim wiki-specific internals.

## CI/CD Post-Push Gate

After pushing to `main`, verify the exact pushed SHA. Use the Mesha/VGoats
GitHub token path, not a random active `gh` account.

Suggested check:

```bash
SHA="$(git rev-parse HEAD)"
GH_TOKEN="$MESHA_GITHUB_PAT" gh run list \
  --repo vgoats/goatos \
  --workflow ci.yml \
  --branch main \
  --json databaseId,headSha,status,conclusion,url \
  --limit 10
```

Find the run whose `headSha` equals `$SHA`. Goal 1 is not complete until that
run finishes with `conclusion=success`, or until GitHub Actions is proven unable
to start jobs because of account/billing/spending-limit state and the local
CI-equivalent gate set below passes.

Local CI-equivalent gate set:

```bash
MESHA_RTK_NOISY_COMMANDS=0 MESHA_RTK=0 make -C /Users/ravi/mesha/goatos check
MESHA_RTK_NOISY_COMMANDS=0 MESHA_RTK=0 make -C /Users/ravi/mesha/goatos sqlc-check
MESHA_RTK_NOISY_COMMANDS=0 MESHA_RTK=0 make -C /Users/ravi/mesha/goatos api-client-check
MESHA_RTK_NOISY_COMMANDS=0 MESHA_RTK=0 make -C /Users/ravi/mesha/goatos validate-migrations
MESHA_RTK_NOISY_COMMANDS=0 MESHA_RTK=0 make -C /Users/ravi/mesha/goatos validate-sqlc-plans
MESHA_RTK_NOISY_COMMANDS=0 MESHA_RTK=0 npm --prefix /Users/ravi/mesha/goatos/apps/admin-web run lint
MESHA_RTK_NOISY_COMMANDS=0 MESHA_RTK=0 npm --prefix /Users/ravi/mesha/goatos/apps/admin-web run typecheck
MESHA_RTK_NOISY_COMMANDS=0 MESHA_RTK=0 npm --prefix /Users/ravi/mesha/goatos/apps/admin-web run check:herd-import-security
MESHA_RTK_NOISY_COMMANDS=0 MESHA_RTK=0 npm --prefix /Users/ravi/mesha/goatos/apps/admin-web run check:goal1-frontend
MESHA_RTK_NOISY_COMMANDS=0 MESHA_RTK=0 npm --prefix /Users/ravi/mesha/goatos/apps/admin-web run check:mock-fidelity
MESHA_RTK_NOISY_COMMANDS=0 MESHA_RTK=0 npm --prefix /Users/ravi/mesha/goatos/apps/admin-web run build
```

If GitHub Actions starts and fails because of a real code/test/workflow issue,
record the URL, fix the issue, rerun local verification and the senior-architect
loop as needed, push a new commit, and check CI/local equivalent again. If GitHub
Actions refuses to start jobs with an account/billing/spending-limit annotation,
record the annotation and use the passing local CI-equivalent gate set as the
Goal 1 post-push gate.

## Done Definition

The task is done only when all are true:

1. Every `OCK-001..OCK-087` and `RVF-001..RVF-023` row has a closeout-ledger
   disposition, evidence, and owner/blocker value.
2. Every Lane A item, including A6, is implemented or explicitly countered as no
   longer valid with current code evidence.
3. Every Lane B historical finding has a close/counter/fix note.
4. Every Lane D product/production gap is fixed or explicitly invalid/countered
   with evidence. Do not leave OCK/RVF rows active as product/backlog/Goal 2
   escape hatches unless the user explicitly removes them from Goal 1 scope.
5. Full verification matrix and local vaccination E2E pass, including
   `make validate-sqlc-plans`.
6. Built dev UI on the vaccination acceptance path meets the UI Quality
   Standard: mock-aligned through `check:mock-fidelity`, backend-contract-owned,
   target-reviewer usable, and E2E-proven. Non-acceptance UI is explicitly
   marked unavailable in the closeout ledger and CEO guide.
7. Critical pgtest/Postgres lanes for `OCK-003`, all A6 durability/stock/replay
   rows (`OCK-002`, `OCK-051..OCK-061`), plus `OCK-001`, `OCK-024`, and
   `RVF-023` — plus any other Lane A6 Required-test implemented as a
   pgtest/Postgres lane — execute and pass. If blocked by Docker/environment,
   each skipped lane is individually marked blocked in the closeout ledger, the
   final answer says Goal 1 is blocked instead of green, and Goal 2 does not
   start.
8. Extra-high senior-architect review runs with
   `context/execution/goal1-senior-architect-review-prompt.md` after local
   verification and finds no valid blocker, critical, or high
   kernel/vaccination issue.
9. Any extra-high review blocker is fixed, then affected verification/E2E and
   review are rerun.
10. Medium/low review findings are either fixed or explicitly invalid/countered
   with evidence. Non-blocking backlog labels do not close an OCK/RVF row unless
   the user explicitly removes that row from Goal 1 scope.
11. Docs match code and do not overclaim unimplemented behavior.
12. Worktree contains only intentional files.
13. All generated-code drift is either intentionally staged or `git diff
   --exit-code --` passes.
14. Pre-push authority gate is run and recorded: active branch, `git remote -v`,
   target `vgoats/goatos`, and Mesha/VGoats token command/path are verified
   before commit/push.
15. Changes are committed and pushed with the Mesha/VGoats path:

```bash
zsh -ic 'git mesha-push main'
```

16. Remote `origin/main` is verified after push.
17. GitHub Actions/CI for the exact pushed `main` SHA is verified green, or
    GitHub Actions is proven unable to start for account/billing/spending-limit
    reasons and the local CI-equivalent gate set above is green. A real
    code/test/workflow failure still means Goal 1 is not complete and must
    return to the fix/verify/review/E2E/push loop.

## Goal 1 Reopened Local Evidence

Evidence captured on the reopened 2026-06-29 fix pass:

- `MESHA_RTK=0 go test ./internal/...` PASS.
- `MESHA_RTK=0 go test ./cmd/...` PASS.
- Targeted Postgres packages PASS:
  `./internal/obligation/adapters/postgres`,
  `./internal/vaccination/adapters/postgres`,
  `./internal/identity/adapters/postgres`,
  `./internal/protocol/adapters/postgres`,
  `./internal/inventory/adapters/postgres`,
  `./internal/processintegrity/adapters/postgres`, and
  `./internal/calendar/adapters/postgres`.
- `MESHA_RTK=0 make -C /Users/ravi/mesha/goatos validate-migrations` PASS.
- `MESHA_RTK=0 make -C /Users/ravi/mesha/goatos validate-sqlc-plans` PASS.
- `MESHA_RTK=0 make -C /Users/ravi/mesha/goatos api-client-generate` PASS.
- `MESHA_RTK=0 make -C /Users/ravi/mesha/goatos sqlc-generate` PASS.
- `MESHA_RTK=0 npm --prefix /Users/ravi/mesha/goatos/tools/contract-validation run validate` PASS.
- `git diff --check` PASS.
- `MESHA_RTK=0 npm --prefix /Users/ravi/mesha/goatos/apps/admin-web run typecheck` PASS.
- `MESHA_RTK=0 npm --prefix /Users/ravi/mesha/goatos/apps/admin-web run lint` PASS.
- `MESHA_RTK=0 npm --prefix /Users/ravi/mesha/goatos/apps/admin-web run check:herd-import-security` PASS.
- `MESHA_RTK=0 npm --prefix /Users/ravi/mesha/goatos/apps/admin-web run check:goal1-frontend` PASS.
- `MESHA_RTK=0 npm --prefix /Users/ravi/mesha/goatos/apps/admin-web run check:ui-contract` PASS.
- `MESHA_RTK=0 npm --prefix /Users/ravi/mesha/goatos/apps/admin-web run check:mock-fidelity` PASS.
- `GOATOS_BEARER_TOKEN=sentinel-mesha-admin-token npm --prefix /Users/ravi/mesha/goatos/apps/admin-web run build` PASS.
- `MESHA_RTK=0 bash /Users/ravi/mesha/goatos/tools/agent-hooks/check-boundaries.sh` PASS.
- `GOAL1-LOCAL-RECHECK-20260629` admin-web E2E smoke PASS; report:
  `.codex-goatos-render/e2e-smoke/GOAL1-LOCAL-RECHECK-20260629`; screenshots:
  `.codex-goatos-render/admin-web-screenshots/2026-06-29T08-29-53-429Z`.
- Direct pasted follow-up recheck after the SOP-drive withdrawal fix:
  `MESHA_RTK=0 command go test ./internal/vaccination/adapters/postgres`
  PASS (`194.453s`), including
  `TestRecordCompletionsFromVaccinationSessionTask`.

The reopened pass has not yet been committed/pushed in this section. Before a
new push, rerun the pre-push authority gate and update this section with the new
SHA and Actions/local-equivalent result.

## Historical Goal 1 Post-Push Evidence

Evidence captured on 2026-06-29:

- Pre-push authority gate: active branch `main`, remote
  `https://github.com/vgoats/goatos.git`, target repo `vgoats/goatos`, Mesha
  token helper `zsh -ic 'git mesha-push main'`, and `MESHA_GITHUB_PAT` present.
- Implementation closure commit pushed with the Mesha token helper:
  `d8e1f0829ffaa61db050554cb40e91da5350242f`
  (`Resolve Goal 1 kernel closure rows`).
- `origin/main` was verified to equal
  `d8e1f0829ffaa61db050554cb40e91da5350242f` after push.
- GitHub Actions run
  `https://github.com/vgoats/goatos/actions/runs/28351280549` was created for
  that exact SHA, but both jobs (`admin-web` job `83984496492`, `guardrails`
  job `83984496495`) completed before any runner or step started.
- The GitHub check-run annotation for both jobs states that the job was not
  started because recent account payments failed or the spending limit needs to
  be increased. This is an account/billing/spending-limit refusal to start
  Actions jobs, not a code/test/workflow failure.

Local CI-equivalent and acceptance evidence used for the blocked Actions state:

- Closeout ledger parser: 110 rows, no missing rows, no duplicate rows, no bad
  statuses, and no active OCK/RVF rows.
- `make check` PASS.
- `make sqlc-check` PASS.
- `make api-client-check` PASS.
- `make validate-migrations` PASS.
- `make validate-sqlc-plans` PASS.
- `git diff --check HEAD --` PASS.
- `npm --prefix apps/admin-web ci` PASS with `found 0 vulnerabilities`.
- `npm --prefix apps/admin-web audit` PASS with `found 0 vulnerabilities`.
- `npm --prefix apps/admin-web run lint` PASS.
- `npm --prefix apps/admin-web run typecheck` PASS.
- `npm --prefix apps/admin-web run check:herd-import-security` PASS.
- `npm --prefix apps/admin-web run check:goal1-frontend` PASS.
- `npm --prefix apps/admin-web run check:mock-fidelity` PASS.
- Admin-web build PASS with `GOATOS_BEARER_TOKEN=sentinel-mesha-admin-token`,
  including token leak guard.
- Focused and coverage backend tests passed for identity, SOP, protocol,
  vaccination, obligation, notification, admin UI, vaccination execution,
  process integrity, Postgres terminal/missed/read-model paths, and SM2 shift
  history.
- Vaccination/procurement/UI E2E passed with run id
  `GOAL1-E2E-REOPEN2-20260629-110234`.
- UI acceptance path evidence was captured under
  `.codex-goatos-render/e2e-smoke/GOAL1-E2E-REOPEN2-20260629-110234` and
  `.codex-goatos-render/admin-web-screenshots/2026-06-29T05-32-42-490Z`.
- Extra-high senior review was rerun after local verification. Its one valid
  contract finding (`matrix_states.missed`) was fixed in
  `backend/internal/adminui/app/service.go`, guarded by
  `apps/admin-web/scripts/check-goal1-frontend-coverage.mjs`, and reverified.
  No valid blocker, critical, or high Goal 1 issue remained.

If this evidence section itself is updated in a later doc-only commit, repeat
the post-push check for that new SHA. If GitHub Actions is still refused for the
same account/billing/spending-limit reason, this local CI-equivalent evidence
remains the Goal 1 post-push gate; if Actions starts and reports a real failure,
return to the fix/verify/review/E2E/push loop.

## Linked Goal 2 - goatos-dev rollout gate

Goal 2 is intentionally linked to this handoff, but it is blocked until Goal 1
local/CI-equivalent closure is complete and no active OCK/RVF bug/follow-up row
remains.

Goal 1 owns every local kernel/vaccination correctness enhancement: transaction
boundaries, idempotency, outbox/fanout, evidence batching, stock conservation,
DLQ/replay semantics, reminders/escalations, read-model vocabulary, shift
ordering, booster correctness, tests, local E2E, and review closure. Goal 2 owns
Google dev rollout of the already-correct kernel: Cloud SQL reset/seed, Cloud
Run services/jobs, Pub/Sub, Scheduler, Cloud Tasks, notification channels, old
dashboard archive/rollback, and Google E2E.

Do not start Google dev rollout, dashboard archive, Cloud SQL reset, seed import,
Cloud Run deploy, Pub/Sub/Scheduler/Cloud Tasks apply, or Google E2E while any
Goal 1 Done Definition item is incomplete or any OCK/RVF row remains active. In
particular, Goal 2 may start only after:

- every `OCK-*` and `RVF-*` row is fixed or explicitly invalid/countered with
  evidence, and there are zero active rows.
- full local verification and vaccination E2E pass.
- critical pgtest/Postgres lanes execute and pass, not skip.
- extra-high review finds no valid blocker/critical/high kernel or vaccination
  issue.
- fixes are committed, pushed to `vgoats/goatos` main, and `origin/main` is
  verified.
- GitHub Actions/CI is green for the exact pushed `main` SHA, or Actions cannot
  start for account/billing/spending-limit reasons and the local CI-equivalent
  gate set is green.

If the extra-high review finds any remaining kernel/vaccination blocker, Goal 1
is not finished and Goal 2 must not start.

If Goal 1 ends with any Docker/pgtest/Postgres critical lane blocked instead of
executed green, Goal 1 is not absolutely complete for rollout purposes. The
next session may document the blocker, but it must not begin Goal 2 until those
lanes actually run and pass.

### Goal 2 scope

Goal 2 is the Google dev proof gate:

```text
Goal 1 local closure passed
  -> verify Mesha/VGoats cloud context
  -> archive old dashboard evidence and preserve rollback for old dev dashboard
  -> deploy GoatOS to goatos-dev and serve it at https://dev.dashboard.mesha.sg/
  -> preserve only approved login/user grants for the four intended users
  -> reset operational data to clean slate
  -> seed <=500 representative goats/sheds/stock/protocol rows from live BQ,
     live Sheets, and/or wiki/source docs
  -> run the same vaccination/kernel E2E in goatos-dev
  -> update the CEO-facing vaccination guide with the tested URLs/screens
  -> call goatos-dev ready only after Google E2E passes
```

Do not treat local code completion as Google dev readiness. Goal 2 is a second
acceptance gate.

The canonical user-facing dev URL is `https://dev.dashboard.mesha.sg/`. Raw
Cloud Run, Firebase/hosted.app, or temporary URLs may be used for diagnostics,
but Goal 2 is not accepted until the new Goat OS dashboard renders on the same
canonical host after the old dashboard evidence is archived and rollback remains
available. The old dashboard shown at that host is the thing to archive/replace,
not the destination users should keep using.

Do not treat the <=500-goat Google dev seed as scale proof. Scale-sensitive rows
such as trusted-evidence batching, sweeper concurrency, and repeat/cycle query
plans must be proven by `make validate-sqlc-plans`, targeted query-plan checks,
and any load/query harness the code path requires. The dev seed proves product
shape and E2E behavior, not the million-goat requirement by itself.

### Google equivalents for local Docker pieces

Local Docker/manual workers map to Google-managed runtime pieces:

| Local/dev concept | goatos-dev counterpart |
| --- | --- |
| Postgres container / pgtest | Cloud SQL `goatos-dev-core-db` |
| local outbox relay/eventbus | Cloud Run Job `goatos-dev-outbox-relay` using `GOATOS_OUTBOX_PUBLISHER=pubsub` |
| local in-process/eventbus delivery | Pub/Sub topic `goatos-dev-outbox-events` and subscription `goatos-dev-domain-events` |
| local worker commands | Cloud Run Jobs in `infra/envs/dev/cloud_run_jobs.tf` |
| local cron/manual reruns | Cloud Scheduler jobs invoking Cloud Run Jobs |
| near-term reminders/retries | Cloud Tasks queue `goatos-dev-near-term-kernel` |
| local notification stubs | `goatos-dev-notification-dispatcher` with configured dev-safe channels/stubs |
| local media/proof storage | dev storage path configured through the app/storage adapter; no production media |

Current dev Terraform already describes Pub/Sub, Cloud Tasks, Cloud Run Jobs,
and Cloud Scheduler in `infra/envs/dev/*.tf`. The next session must verify
whether those resources are applied and current before assuming they exist.

Goal 2 topology must prove the auxiliary kernel jobs in Google. The current dev
map includes `inventory-batch-reconciler` and `idempotency-key-sweeper`; prove
stock reconciliation and idempotency TTL/replay behavior in Google E2E before
calling the dev kernel runtime complete. `outbox-dlq` exists as a backend command
but still needs either a Cloud Run Job/Scheduler entry or an explicit dev-safe
operator-runbook path for Pub/Sub/native DLQ drain/replay/import.

### Goal 2 guardrails

- Verify and state active Google account, organization `vgoats.com`, folder
  `goat-os`, project `goatos-dev`, and repo `vgoats/goatos` before every cloud
  mutation.
- Do not touch Heva, Slice, `hevaplatform`, `goatos-sheets`, stg, or prod.
- Archive/preserve rollback for the existing `dev.dashboard.mesha.sg` dashboard
  before replacing traffic.
- Do not assume the old dashboard data is already archived. Before any reset or
  traffic replacement, capture enough rollback/evidence to reconstruct what the
  old dev dashboard showed: current URL/target, deployed image or hosting target,
  latest screenshot set, source query/table references, and non-sensitive
  summary counts. Store raw private exports outside git; commit only sanitized
  notes/runbook evidence.
- If Google E2E fails mid-rollout, do not flip or strand
  `dev.dashboard.mesha.sg` traffic. Keep the old dashboard rollback reachable,
  record the failed gate, and stop before declaring goatos-dev ready.
- Do not share raw Cloud Run, Firebase, or hosted.app URLs as the final CEO
  dashboard. The shareable dev URL must be `https://dev.dashboard.mesha.sg/`
  after successful cutover.
- Preserve only approved auth/user grant data for the four intended users.
  Operational tables should start clean unless explicitly seeded.
- The intended dev login policy is the four-user CEO/COO/internal-admin allowlist
  defined in `infra/envs/dev/config.md`, unless the user explicitly changes it.
  Preserve/seed only the needed `auth_pending_email_grants`, `user_scope_grants`,
  workforce/user identity rows, and audit records needed for those approved
  users to sign in as CEO/COO/admin. Do not preserve stale goats, sheds,
  obligations, inventory, imports, proofs, or read models just because they are
  currently in dev.
- Seed data must be representative, not fake fantasy data: <=500 goats, enough
  parks/sheds/stages/sex/breed/age/health/lifecycle states to make vaccination
  metrics meaningful.
- Seed enough rows to exercise pagination in the CEO/admin surfaces. Do not
  port all live goats. Prefer a compact cohort that covers CBE/CPT, multiple
  parks and sheds, bucks/does/kids/adults, alive/deferred/exited examples,
  known DOB/unknown DOB, due/missed/deferred/blocked/proof-pending/
  verification-pending/completed vaccination states, stock-present and
  stock-blocked cases, and at least one shift/exit/recovery edge where source
  evidence supports it.
- Match the legacy dashboard data shape, not the whole legacy population. Use
  live BQ/Sheets/wiki source evidence, plus the old dashboard screenshots as
  visual parity references, to pick representative sheds and goats. Examples
  from the legacy vaccination matrix such as Castro, Gandhi, Godel, Mandela, or
  similar source-backed sheds are acceptable seed candidates only when verified
  in the source data.
- Write a seed ledger outside raw data dumps: source used, source query/sheet,
  sample count, included states, anonymization/PII decision, and final seeded
  count. Commit sanitized seed scripts/fixtures only; never commit raw BQ/Sheets
  exports or private row dumps.
- Source seed candidates from live BQ, live Sheets, and/or wiki/source docs via
  the approved Google Cloud data-pull runbook, not Chrome DOM scraping.
- Build seed data through migration/import/seed scripts or explicit transient
  artifacts. Do not commit raw BQ/Sheets exports, PII row dumps, or private
  source rows to git.
- Do not connect dev to production outbound effects. Notification channels must
  be dev-safe.
- Run Google dev E2E across API, admin-web, Cloud SQL, outbox relay, Pub/Sub,
  domain-event consumer, sweepers, Scheduler, Cloud Tasks, notifications/stubs,
  read models, and UI.
- Include stock reconciliation and DLQ/replay in Google dev E2E. Specifically,
  prove `inventory-batch-reconciler` or its documented equivalent handles
  reserved-stock repair/release, and prove Pub/Sub/native DLQ or outbox DLQ
  messages are visible and replayable/discardable through the intended dev path.
- Include idempotency retention/replay behavior in the Goal 2 evidence, either
  through `idempotency-key-sweeper` or a documented reason it is intentionally
  disabled in dev and how replay-after-expiry is still safe.
- Before calling Goal 2 complete, update
  `context/execution/ceo-vaccination-kernel-dev-guide.md` so it is shareable:
  remove draft placeholders, add the final dev URL, seeded count/source summary,
  tested screen names, what config/SOP is already set, how a new goat/shed
  triggers vaccination work, where to see broken process and escalation, and
  which buttons/flows are explicitly unavailable in this dev slice. Do not call
  any built UI "basic" or "half-polished"; built flows must be modern,
  mock-aligned, backend-contract-owned, target-reviewer usable, and E2E-tested
  when part of the vaccination acceptance path.
- Keep detailed workflow follow-up/backlog language in
  `context/execution/vaccination-workflow-followups.md`, not in the CEO
  walkthrough.

Goal 2 authorization note:

- The workspace owner explicitly authorized the Goal 2 Google dev rollout after
  Goal 1 is absolutely complete. Do not ask again merely because the user is not
  at the keyboard.
- This authorization does not bypass the cloud authority gate. Before every
  cloud mutation, verify and record the active Google account, `vgoats.com`
  organization, `goat-os` folder, `goatos-dev` project, and `vgoats/goatos`
  repo. If auth has expired, re-run the approved gcloud browser-auth flow from
  `docs/runbooks/google-cloud-environments.md`; if auth cannot be completed,
  mark Goal 2 blocked rather than touching the wrong project.
- Authorization is only for `goatos-dev` Goal 2 after Goal 1. It does not permit
  stg/prod, Heva/Slice projects, `hevaplatform`, or raw `goatos-sheets`
  mutations.

Goal 2 final answer must distinguish:

- local kernel/vaccination closure status.
- Google dev deployment status.
- old dashboard archive/rollback status.
- preserved users/grants.
- clean-slate seed source and count.
- Google E2E results.
- CEO guide path and whether it is final/shareable.

### Goal 2 preflight captured on 2026-06-29

Read-only cloud preflight was run after browser auth. No cloud resources were
created, updated, deleted, enabled, or deployed.

Verified context:

- active gcloud account: verified Mesha/VGoats operator account from
  `docs/runbooks/google-cloud-environments.md`.
- active gcloud project: `goatos-dev`.
- organization visible: `vgoats.com` / `563962826703`.
- `goatos-dev` parent folder: `goat-os` / `188649904255`.
- repo remote: `https://github.com/vgoats/goatos.git`.
- Cloud SQL metadata visible: `goatos-dev-core-db`,
  `goatos-dev:asia-south1:goatos-dev-core-db`, Postgres 16, `RUNNABLE`.
- expected Secret Manager containers are visible, including
  `goatos-dev-database-url` and `goatos-dev-admin-web-tenant-id`.

Live source access verified:

- BigQuery legacy source project `goatos-sheets` is readable; datasets include
  `farm`, `goatsDB`, `healthDB`, `weights`, `Shiftings`, and
  `procurement_farm`.
- `goatos-sheets.farm.daily_summary_dev` count query succeeded.
- Drive-scoped auth was refreshed with `gcloud auth login
  --enable-gdrive-access --brief --update-adc`.
- The three runbook Sheet IDs are metadata-readable:
  - RFID source of truth:
    `1FulMrlb8_AGwL5nFwoORACnbDstSFMg-GCPaKIZnnF8`.
  - Census DB: `1tye3uknlVMoPIiYk2pdkwy9PLQwo5m8wdFIsc7yI5Ho`.
  - Goats DB: `1R648AutCSXS247DZb7dc07dDgyue6_R4mZDd93oW3M8`.

Current goatos-dev resource drift to expect:

- Cloud Run services exist: `goatos-admin-web-dev` and `goatos-api-dev`.
- Current deployed images are older than the current repo state and must not be
  assumed current for Goal 2.
- Cloud Run jobs currently listed only `goatos-dev-migrate` and
  `goatos-dev-seed-email-grants`; the kernel jobs described in
  `infra/envs/dev/cloud_run_jobs.tf` were not listed.
- Cloud Scheduler listed no jobs in `asia-south1`.
- Pub/Sub topics `goatos-dev-outbox-events` and
  `goatos-dev-outbox-events-dlq` exist.
- Pub/Sub subscription listed only `goatos-dev-analytics-export`; the
  `goatos-dev-domain-events` subscription described in Terraform was not listed.
- Cloud Tasks query prompted to enable `cloudtasks.googleapis.com`; do not enable
  it during Goal 1. Goal 2 must verify/apply it deliberately under the cloud
  mutation guard.

Cloud SQL read-only proxy check:

- `cloud-sql-proxy` is installed locally.
- Read-only connection through `127.0.0.1:5433` succeeded using the secret-backed
  `goatos-dev-database-url`.
- Non-sensitive counts observed: `goats=2807`, `locations=158`,
  `user_scope_grants=4`, `outbox_messages=1223`.
- Current dev DB is stale for the latest kernel: `protocol_versions` and
  `obligation_instances` were missing in the read-only probe. Goal 2 should
  treat dev DB as needing migration/reset/reseed after Goal 1, while preserving
  the intended user grants.
