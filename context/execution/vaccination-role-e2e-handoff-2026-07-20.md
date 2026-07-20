# Vaccination role-wise E2E handoff — 2026-07-20

This handoff exists because the prior Codex session became unreliable after
repeated context compactions. Treat this file as the continuation source of
truth. Do not replay old writes blindly; reconstruct current state first.

## Current checkout

```text
Worktree: /Users/ravi/.codex/worktrees/vaccination-closure-integrated/goatos
Branch: manual-vaccination-e2e
HEAD/origin/main at handoff time: 074ac3ea075a08764bdfcd7de132dd72012ecdb5
Production DB/cloud touched: NO
```

Canonical repo `/Users/ravi/mesha/goatos` is on a separate dirty branch
`fix/vaccination-event-guardrails-80842179`. Do not mix that branch with this
manual vaccination E2E lane unless the user explicitly asks to reconcile both.

## User’s actual goal

Run and finish a real role-wise Goat OS vaccination drive E2E on isolated local
infrastructure:

1. Start from latest `origin/main`.
2. Use a throwaway local Postgres DB and local object/video storage only.
3. Use actual backend APIs and the Android app/Room/outbox path, not fake UI
   screenshots.
4. Walk role-by-role:
   - operator sees scheduled vaccination drive,
   - operator opens drive and can switch sheds,
   - operator enters shed scan screen,
   - operator simulates RFID/BLE goat scans,
   - each scanned goat has camera-only proof attached on the goat row,
   - proof syncs Room-first/outbox/immediate backend draft,
   - shed/drive finalize validates already-synced records only,
   - verifier sees generic verification module with Vaccination active and
     future tabs under construction,
   - verifier accepts/rejects per goat/shed proof,
   - accepted proof preserves operator `administered_at` as the medical
     vaccination date,
   - leadership/park head/director/CEO/CxO sees/approves drive close according
     to scope,
   - notifications/deep links land on target screens,
   - screenshots are captured for every screen, popup, bottom sheet, and role.
5. Fix UI/UX defects found during the walkthrough before pushing anything.
6. Re-run focused tests/guards after fixes.
7. Produce an audit report with screenshots grouped by role and exact commands.

## Current dirty files in this worktree

These are local uncommitted changes from the interrupted manual E2E attempt:

```text
apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/ScanViewModel.kt
apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/ShedsViewModel.kt
apps/goatos-android/core/core-data/src/main/kotlin/sg/mesha/goatos/core/data/ExecutionRepository.kt
apps/goatos-android/core/core-network/src/main/kotlin/sg/mesha/goatos/core/network/AppApi.kt
apps/goatos-android/core/core-network/src/main/kotlin/sg/mesha/goatos/core/network/NetworkModule.kt
apps/goatos-android/feature/feature-scan/src/main/kotlin/sg/mesha/goatos/feature/scan/ScanScreen.kt
apps/goatos-android/feature/feature-scan/src/main/res/values/strings.xml
apps/goatos-android/feature/feature-sheds/src/main/kotlin/sg/mesha/goatos/feature/sheds/ShedsScreen.kt
apps/goatos-android/feature/feature-sheds/src/main/res/values/strings.xml
backend/internal/permissions/permissions_test.go
backend/internal/permissions/routes.go
backend/internal/vaccinationexecution/adapters/http/handler.go
backend/internal/vaccinationexecution/adapters/postgres/repository.go
backend/internal/vaccinationexecution/adapters/postgres/repository_integration_test.go
```

Do not assume these are correct. Review them before committing. The visible
intent was:

- scan screen fallback title/copy,
- visible “Switch shed” action,
- Android/API routes for execution and shed drilldown,
- backend route permission entries,
- park-scoped task/shed drilldown fixes,
- ET+TT label normalization in app data paths.

## Work already landed on main before this handoff

`docs/runbooks/vaccination-closure-e2e-evidence-2026-07-19.md` says the
vaccination closure automated slice was pushed at:

```text
1d39d21cd9a0ac41ea2557075fb600216b4bf34b
```

Current `origin/main` at handoff is:

```text
074ac3ea075a08764bdfcd7de132dd72012ecdb5
```

So the prior automated closure work is already included in `origin/main`.

Automated evidence claimed green at that time:

- local isolated backend proof script,
- proof upload per goat, not per vaccine,
- verifier queue and acceptance,
- leadership close UI,
- `make land-main`,
- focused Android Paparazzi vaccination screenshots,
- mobile UI copy/layout guard.

That is not the same as the requested manual tap-by-tap role walkthrough. The
manual walkthrough is still the active follow-up.

## Current throwaway DB / storage state

At the time of this handoff, the prior session had used:

```text
/tmp/goatos-manual-vax-db-url
```

as the local database URL file. Treat it as disposable. If the DB is stale or
missing, create a fresh isolated DB; do not touch production or cloud.

Video/object storage question from user: in local/dev it must land under the
repo/local object-storage adapter path, not GCS. If a new run is started, record
the exact local media root in the final audit report and show the files created
by the camera/proof upload flow.

## ET+TT 3w/4w finding

The user asked why ET+TT still shows `4w` after changing booster to `3w`.

Verified from current tree and throwaway DB:

```text
ET_TT_4W | sequence 1 | birth_age                 | offset_days 28 | min_gap_days 0
ET_TT_7W | sequence 2 | after_previous_completion | offset_days 21 | min_gap_days 21
```

Meaning:

- `ET_TT_4W` is the first kid dose at 4 weeks / 28 days.
- The booster is 21 days / 3 weeks after dose 1.
- The booster code may still be named `ET_TT_7W` because a kid that got dose 1
  at 4w reaches age 7w after the 3-week gap. That internal name must not leak
  into mobile UI as confusing “7w”/“4w” copy.

If the UI shows `ET+TT 4w` for first dose, that is expected. If it shows `4w`
for a booster row, patch the label/source mapping or stale seed.

Relevant history cluster:

```text
843f7c1f Fix vaccination ET TT scheduling and schedule UI
565dc3c5 fix: close vaccination successor, capacity, calendar, and event-truth ledger
1723a670 fix: real cancel-reason successor policy, exact batch cells, overdue hold clamp
aaaaca92 fix: complete vaccination closure proofs
```

## UI/UX issues the user explicitly called out

Fix these during the manual walkthrough if still present:

- calendar/date chips or any cards/buttons with different heights between
  sibling states;
- raw/internal UI labels such as `calendar_coverage_banner`;
- scan screen missing a clear title;
- per-vaccine proof implication in UI; proof is per goat handling, not per
  vaccine;
- no bottom nav on stacked child screens;
- shed switch must be easy from drive/shed scan context;
- shimmer/skeleton when no Room data and network is loading;
- sync/progress indicator when cached Room data exists and refresh/upload is in
  progress;
- modern spacing/text alignment across every screen, not just vaccination.

The existing app-wide guard is not a replacement for human visual review. If a
new visual class of defect is found, add a generic guard so future screens are
covered too.

## Permissions requirements to preserve

The user explicitly required:

- camera, location, Bluetooth, and notifications are mandatory before the app
  proceeds;
- operator execution cannot start unless required permissions are granted;
- no file picker for proof upload;
- only camera recording/capture for proof;
- portrait-only app lock except camera/video recording may allow the camera’s
  needed orientation.

Verify current Android manifest and login/capture gates before claiming done.

## Mobile scan behavior expectation

The scan screen should not hit the backend on every RFID scan just to discover
the roster. The operator should already have the assigned drive/shed roster
available via Room/API data for the selected drive/shed. RFID/BLE scan/tap marks
the matching goat row locally, persists Room-first, and queues backend sync.

Expected operator path:

```text
Drive -> Shed list/switcher -> Shed scan
  -> scan/tap goat RFID
  -> matched goat row changes state locally
  -> record one or more camera clips on that goat row
  -> clip metadata/proof persists Room-first
  -> outbox/background worker uploads/syncs
  -> row shows upload/synced/retry status
  -> operator can switch sheds and return without losing work
  -> finalize shed/drive only validates synced records
```

## Next-session execution plan

1. Start by fetching/rebasing from main:

   ```bash
   cd /Users/ravi/.codex/worktrees/vaccination-closure-integrated/goatos
   git fetch origin
   git status --short
   git rev-parse HEAD origin/main
   ```

   If `HEAD == origin/main` and only the 14 dirty files above exist, review the
   dirty diff and decide whether to keep, patch, or discard with explicit
   justification. Do not run destructive cleanup blindly.

2. Confirm isolated local services only:

   - no prod DB,
   - no real GCS bucket,
   - no cloud writes,
   - record local DB URL file path and media/object-storage root.

3. Rebuild or start the local backend/API + Android app against a fresh
   throwaway DB.

4. Seed or create one vaccination drive with:

   - at least one park,
   - multiple sheds,
   - multiple animals per shed,
   - at least one first-dose ET+TT row and, if validating booster copy, a
     separate booster scenario.

5. Execute real backend/API + Android flow:

   - operator role,
   - verifier role,
   - park head/director role,
   - CEO/CxO role.

6. Capture screenshots under a stable evidence folder, for example:

   ```text
   docs/runbooks/evidence/vaccination-role-e2e-2026-07-20/
     01-operator/
     02-verifier/
     03-leadership/
     04-ceo-cxo/
     05-api-db-storage/
   ```

7. Write final audit report:

   ```text
   docs/runbooks/vaccination-role-e2e-audit-2026-07-20.md
   ```

   Include:

   - exact git SHA,
   - DB/media paths,
   - backend API base URL,
   - Android build/emulator/device used,
   - every command,
   - screenshots grouped by role,
   - latency/performance notes,
   - LeakCanary/memory notes,
   - remaining blockers if any.

8. Run focused gates before any push:

   ```bash
   make validate-migrations
   cd backend && go test ./... -count=1
   GOATOS_RUN_POSTGRES_TESTS=1 go test ./tests/e2e -count=1 -timeout=30m
   cd apps/goatos-android && ./gradlew :app:compileDevDebugKotlin :app:testDevDebugUnitTest
   cd /Users/ravi/.codex/worktrees/vaccination-closure-integrated/goatos
   GOATOS_TENANT_ID=00000000-0000-4000-8000-000000000001 make guardrails
   git diff --check
   ```

   If the run is long, keep output receipts. Do not claim green without exact
   command output tied to the current SHA.

## Suggested prompt for the next Codex session

```text
Continue Goat OS vaccination role-wise E2E from this handoff:

/Users/ravi/.codex/worktrees/vaccination-closure-integrated/goatos/context/execution/vaccination-role-e2e-handoff-2026-07-20.md

Use the integrated worktree:
/Users/ravi/.codex/worktrees/vaccination-closure-integrated/goatos

Do not touch production DB/cloud. Use fresh throwaway local DB/local object
storage only. First reconstruct current state, fetch/rebase from origin/main,
review the 14 dirty files listed in the handoff, then finish the real backend +
Android Room/outbox role-wise vaccination E2E. Capture screenshots grouped by
operator/verifier/leadership/CEO-CxO and write the final audit report. Fix any
UI/UX defects found, rerun focused gates, and only then consider push if the
user explicitly authorizes it.
```
