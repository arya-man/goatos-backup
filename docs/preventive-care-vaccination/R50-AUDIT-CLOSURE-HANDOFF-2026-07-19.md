# R50 Audit Closure Handoff — 2026-07-19

## Purpose

This document is the durable handoff for the 2026-07-19 R50 audit that reported
`NOT APPROVED · 32 pending`. It records the exact repository state, work already
attempted, remaining gaps, and verification boundary. Nothing described here has
been committed or landed unless explicitly stated.

## Repository state

- Repository: `<repo-root>`
- Working tree containing the fixes:
  `<worktree>`
- Branch: `fix/r50-audit-closure`
- Base commit: `b5e53201`
- Remote target: `https://github.com/vgoats/goatos.git`
- Current diff at handoff: 32 tracked files changed, one new source file,
  approximately `+794/-326`, plus this handoff document.
- Commit status: not committed
- Push/land status: not pushed and not landed to `main`

The original `<repo-root>` checkout was left untouched because it
contained unrelated local changes.

## Past fixes reviewed before changing code

The following earlier commits were inspected so this pass would not repeat or
undo prior closure work:

- `c8df4e57`
- `00b88d1a`
- `565dc3c5`
- `10568bc2`
- `aaaaca92`
- `5156ef6b`
- `2ce830f2`
- `0b8a7e20` (identified by blame as the source of parts of verification
  closure/scope behavior)

The audit was against the same `main` state used as this branch's base; there
were no later upstream commits that already closed the reported residual gaps.

## Issue-by-issue status

Status meanings:

- **Implemented, unverified**: production code was changed, but the required
  regression/full gate has not completed.
- **Partial**: some production code was changed, but known work remains.
- **Pending**: no complete fix was implemented in this worktree.
- **Previously fixed**: the audit itself recognized the item as closed.

| ID | Audit gap | Handoff status | Remaining work |
|---|---|---|---|
| R50-001 | Vaccination seed ignored accepted dose history | Implemented, unverified | Add a multi-dose kid-course regression test and run the seed package tests. |
| R50-002 | Make recipe coupling could regress | Pending | Add a contract/regression test proving the migration and seed flags remain in the same recipe. |
| R50-003 | Stage lookup writes occurred before the seed count gate | Implemented, unverified | Add rollback/count-gate regression coverage and run seed tests. |
| R50-004 | Previously reported closure item | Previously fixed | Preserve the existing behavior. |
| R50-005 | DOB disposition audit sidecar was written after DB commit | Implemented, unverified | DB-backed disposition proof is now written inside the seed transaction; add a regression test and verify JSON shape uses an empty array instead of `null` where required. |
| R50-006 | Deferred successor row and event were split writes | Implemented; focused tests passed | Add explicit crash/replay integration coverage for missing-event repair. |
| R50-007 | Fresh RFID roster rows were absent | Partial | Android roster persistence was reworked, but Android compilation, Room migration, and fresh-install/reopen tests remain. |
| R50-008 | Intermediate pages, 50×20 cap, and partial roster cache | Partial | Full-page staging and terminal atomic publication were added; finish Android compile fixes and add pagination/cursor-cycle/cache-integrity tests. |
| R50-009 | Cold refresh failures rendered empty state | Partial | Scan and Calendar now retain explicit cold-cache errors; compile and add UI/ViewModel regressions. Audit other affected resources if the original finding covered more screens. |
| R50-010 | Mobile list-fetch guard accepted an empty diff | Implemented, unverified | Re-run the guard in full-tree and diff modes and add a guard self-test. |
| R50-011 | Successor generation stopped after 32 attempts | Implemented; focused package tests passed | Add a direct `>32` successor collision regression test. |
| R50-012 | `planned_date` depended on PostgreSQL session timezone | Implemented, unverified | Explicit Asia/Kolkata conversion and a multi-timezone test were added; rerun the calendar package after the last test compile correction. |
| R50-013 | Closure audit report contained stale claims | Pending | Update/rebuild the closure report only after all code and live-evidence claims are true. Do not claim a production reseed that was not performed. |
| R50-014 | Event envelope rejected `verification.item.closed` | Implemented, unverified | Enum was added; add producer→schema-validator→consumer coverage. |
| R50-015 | Baseline-only schema changes lacked a forward migration | Pending, release-blocking | Create idempotent `000003_r50_forward_compatibility.sql` and test upgrade from the pre-change schema. Required deltas are listed below. |
| R50-016 | Verification verdict could be reversed | Implemented, unverified | Storage write now requires pending/open status; add explicit approve→reject and reject→approve regression tests. |
| R50-017 | Missing/signing-failed evidence failed open | Implemented, unverified | Resolver/service now fail the whole request; add missing-object and signing-failure tests, plus Android consumption of evidence availability. |
| R50-018 | Operators could verify their own work | Implemented, unverified | Storage predicate excludes the originating operator; add an integration regression test. |
| R50-019 | Mixed permission grants escaped into tenant-wide scope | Implemented, unverified | Handler now evaluates tenant scope for the requested permission; add mixed-grant handler tests. |
| R50-020 | Per-item close bypassed whole-submission closure | Implemented, unverified | Per-item close excludes submission-owned items and submission close remains atomic; add integration coverage. |
| R50-021 | Server ignored mobile verification idempotency | Implemented, unverified | Request-level reservation/completion was added and write endpoints require `Idempotency-Key`; improve the invalid-key error code and add lost-response/same-key-different-payload tests. |
| R50-022 | Cancel-by-key failed to repair batch quantities | Implemented, unverified | Cancellation now captures the old batch and repairs estimated targets/quantity/ledger in one transaction; add mixed-batch integration tests. |
| R50-023 | Partial legacy cell ledgers corrupted quantities | Implemented, unverified | A conservative `legacy_cell_total` fallback was added; update stale comments and add partial-ledger regressions. |
| R50-024 | Manual UI tap fabricated an RFID scan | Implemented, Android unverified | Manual tap now changes only the draft UI; compile and add ViewModel persistence/outbox assertions. |
| R50-025 | Persisted roster rows were not task-scoped | Partial | Task/scope fields, indexes, DAO queries, and a destructive `10→11` roster migration were drafted; update all fakes/tests, generate Room schema 11, and compile. |
| R50-026 | RFID canonicalization differed across lookup paths | Partial | Canonical tags are now drafted into roster persistence/lookup; compile and add equivalent-format tests. |
| R50-027 | Backend proof policy was discarded by Android | Pending | Parse typed proof policy from `SopVersionDto`, retain it in `TaskDetail`, and replace hardcoded proof counts/subjects/capture-source assumptions. |
| R50-028 | Proof collectors and local video files leaked | Pending | Make startup reconciliation asynchronous and batched, terminate per-proof status collection at a terminal state, and delete app-owned video files after durable server acknowledgement/removal/clear. |
| R50-029 | Proof reads were capped at 10,000 and repeatedly filtered per goat | Pending | Replace the broad recovery/read behavior with bounded batches and index proofs by subject once per state build. |
| R50-030 | Leadership close spinner never cleared | Implemented, Android unverified | It now follows the returned outbox row to terminal state, clears the submission flag, and refreshes closure rows on success; compile and add success/failure tests. |
| R50-031 | Verify queue refresh stayed true on non-vaccination tabs | Implemented, Android unverified | Refresh flag now clears in `finally`; compile and add tab-switch regression coverage. |
| R50-032 | Empty notification backlog surfaced `ErrNoRows` | Implemented; focused package tests passed | Add/confirm a direct empty-backlog repository regression if not already covered. |
| R50-033 | Notification event subject type did not match the request | Implemented; focused package tests passed | Add/confirm an event-envelope subject assertion. |

## Implemented code areas

### Verification backend

- Added request idempotency reservation/completion in
  `backend/internal/verification/adapters/postgres/idempotency.go`.
- Verdict and closure endpoints require and propagate `Idempotency-Key`.
- Same-key replay can return the stored response; conflicting payloads are
  rejected.
- Verdict writes are restricted to pending, open items and exclude the original
  operator.
- Per-item closure excludes items owned by a source submission.
- Submission closure remains the whole-drive path.
- Evidence resolution now fails closed if any requested proof is absent or
  cannot be signed.
- Queue rows expose evidence availability.
- Tenant scope checks are permission-specific instead of accepting any broad
  grant.

Known cleanup: invalid idempotency keys currently use the generic invalid JSON
response helper; return a dedicated error code before landing.

### Vaccination generation and obligation storage

- Added atomic deferred-obligation plus domain-event insertion.
- Replay repairs a missing deferred event without duplicating the obligation.
- Removed the 32-attempt successor-generation ceiling.
- Cancel-by-idempotency-key captures the old batch and repairs estimated target,
  planned quantity, and cell ledger in the same transaction.
- Added conservative handling for legacy or partial cell ledgers.

### Seed behavior

- Accepted completion history is sorted and passed into schedule generation.
- Stage lookup writes moved inside the main seed transaction and after the
  relevant gate.
- DOB-null/disposition evidence is persisted in `seed_runs.detail` before
  commit; the sidecar is no longer the only durable proof.

### Calendar, notification, and contracts

- `planned_date` is derived using an explicit Asia/Kolkata timezone conversion.
- Empty notification backlog treats `pgx.ErrNoRows` as no backlog.
- `notification.sent` now uses `notification_request` as its subject type.
- `verification.item.closed` was added to the domain-event schema enum.

### Android work in progress

- Drafted task-scoped, canonicalized RFID roster rows and indexes.
- Drafted complete-page staging with atomic publication at the terminal cursor.
- Removed the fixed page-count cutoff from roster refresh.
- Manual row taps no longer persist an accepted RFID attempt.
- Added cold-cache error state for Scan and Calendar.
- Added terminal outbox observation for leadership submission closure.
- Fixed Verify Queue refresh cleanup with `try/finally`.
- Bumped Room database version from 10 to 11 and drafted the roster migration.

This Android work is not compile-verified and must not be committed as complete
until the items in the next section are resolved.

## Known Android compile/test follow-up

1. Update all `ExecutionRepository` fakes and call sites for task-scoped roster
   methods.
2. Update `ScanRosterRowEntity` constructors in tests for the new fields.
3. Add `MIGRATION_10_11` to migration/upgrade tests.
4. Generate and commit Room schema `11.json`.
5. Confirm the new Calendar error UI uses existing theme colors/components.
6. Carry `evidence_available` through network DTOs and disable verdict controls
   when evidence is unavailable.
7. Implement typed proof-policy propagation and replace hardcoded proof limits.
8. Implement proof collector termination, bounded recovery, and local-file
   deletion.
9. Pre-index proofs by goat/subject in Scan and Submit state builders.
10. Run the correct variant task; `:app:testDebugUnitTest` is ambiguous. Start
    with `:app:testDevDebugUnitTest`.

## Forward migration R50-015

Create `backend/migrations/postgres/000003_r50_forward_compatibility.sql` with
idempotent `Up` behavior for installations that already ran the older baseline.
The forward migration must cover at least:

- notification type check additions for verification approval/closure;
- SOP scan tables, indexes, foreign keys, and constraints;
- vaccination capacity-overflow constraint/value changes;
- verification item `subject_label`, `closed_by`, and `closed_at` columns and
  constraints;
- verification outbox partial-index coverage for closed events;
- leadership indexes added to the edited baseline;
- vaccination SOP form/proof-policy updates required by the current baseline.

Test both paths:

1. clean install from the current baseline plus migrations;
2. upgrade from the schema immediately before the baseline edits.

## Verification already completed

The following focused checks passed before handoff:

```text
go test ./internal/verification/...
go test ./internal/vaccination/app ./internal/obligation/...
```

Focused notification/outbox tests also passed. The calendar test was corrected
after an initial compile error but was not rerun before handoff.

An attempted Android command did not reach compilation because
`:app:testDebugUnitTest` is ambiguous among dev/staging/production variants. It
was a task-selection error, not a reported code-test failure.

## Required verification before landing

From the worktree root:

```bash
cd <worktree>

# Inspect all unfinished changes first.
git status --short
git diff --check

# Backend focused suites after completing tests/migration.
cd backend
go test ./internal/verification/...
go test ./internal/vaccination/app ./internal/obligation/...
go test ./internal/calendar/... ./internal/notification/...
cd ..

# Android focused compile/tests using an explicit variant.
cd apps/goatos-android
./gradlew :app:testDevDebugUnitTest :core:core-data:testDebugUnitTest
cd ../..

# Repository guards.
node tools/agent-hooks/check-mobile-list-fetch.mjs --all

# Required full local gate against the exact final commit.
make ci-local
```

Adjust the core-data task to its exact available variant name if Gradle reports
another ambiguity; list tasks rather than guessing.

## Commit and landing procedure

Do not push this unfinished branch directly to `main`.

After every issue above is closed and the final exact commit passes
`make ci-local`:

1. Verify the active GitHub account and that the target remains
   `vgoats/goatos` under Mesha/VGoats.
2. Commit the complete, reviewed diff intentionally.
3. Run the repository-required `make land-main` workflow.
4. Confirm remote `main` contains the landed commit and required checks are
   green.

## Current changed-file inventory

At handoff, the production/test diff touched these areas:

```text
apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/di/AppModule.kt
apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/CalendarViewModel.kt
apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/LeadershipViewModel.kt
apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/ScanViewModel.kt
apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/VerifyQueueViewModel.kt
apps/goatos-android/core/core-data/src/main/kotlin/sg/mesha/goatos/core/data/CalendarRepository.kt
apps/goatos-android/core/core-data/src/main/kotlin/sg/mesha/goatos/core/data/DatabaseFactory.kt
apps/goatos-android/core/core-data/src/main/kotlin/sg/mesha/goatos/core/data/ExecutionRepository.kt
apps/goatos-android/core/core-data/src/main/kotlin/sg/mesha/goatos/core/data/GoatDatabase.kt
apps/goatos-android/core/core-data/src/main/kotlin/sg/mesha/goatos/core/data/Migrations.kt
apps/goatos-android/core/core-data/src/main/kotlin/sg/mesha/goatos/core/data/VaccinationInsightsRepository.kt
apps/goatos-android/core/core-data/src/main/kotlin/sg/mesha/goatos/core/data/cache/ExecutionCache.kt
apps/goatos-android/core/core-data/src/test/kotlin/sg/mesha/goatos/core/data/ExecutionRepositoryPaginationTest.kt
apps/goatos-android/core/core-database/src/main/kotlin/sg/mesha/goatos/core/database/outbox/OutboxDao.kt
apps/goatos-android/feature/feature-calendar/src/main/kotlin/sg/mesha/goatos/feature/calendar/CalendarScreen.kt
apps/goatos-android/feature/feature-calendar/src/main/kotlin/sg/mesha/goatos/feature/calendar/CalendarUiState.kt
backend/cmd/seed-vaccination-real/main.go
backend/internal/calendar/adapters/postgres/canonical_read_test.go
backend/internal/calendar/adapters/postgres/targets.go
backend/internal/notification/adapters/postgres/repository.go
backend/internal/obligation/adapters/postgres/repository.go
backend/internal/vaccination/app/booster_test.go
backend/internal/vaccination/app/generation.go
backend/internal/vaccination/app/generation_test.go
backend/internal/verification/adapters/http/handler.go
backend/internal/verification/adapters/postgres/idempotency.go
backend/internal/verification/adapters/postgres/repository.go
backend/internal/verification/adapters/proofmedia/resolver.go
backend/internal/verification/app/service.go
backend/internal/verification/domain/types.go
backend/internal/verification/ports/ports.go
contracts/jsonschema/domain-event-envelope.schema.json
tools/agent-hooks/check-mobile-list-fetch.mjs
```

