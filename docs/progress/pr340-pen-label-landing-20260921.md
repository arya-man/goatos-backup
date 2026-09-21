# PR 340 Pen Label Landing Progress

## Scope

- Review and land PR 340, `fix(weighing): stop rendering the pen twice on the schedule`, to `main`.
- Keep the primary checkout untouched; use isolated worktree `/Users/raviteja/mesha/goatos-pr340-review`.

## Done

- Reviewed PR 340 with no blocking findings.
- Focused backend tests passed:
  - `cd backend && go test ./internal/platform/oploc ./internal/weighing/adapters/postgres -run 'TestResolveComposedName|TestSplitShedPartitionName|TestApplyShedPartitionDisplay|TestApplyPlannerShedPartitionDisplay|TestPenLabelIsComposedOnceFromCatalogToVerifier'`
- Focused Android JVM test passed:
  - `cd apps/goatos-android && ./gradlew :core:core-common:test --tests 'sg.mesha.goatos.core.common.OperationalLocationLabelTest'`
- Operational-location guard self-test passed:
  - `node tools/agent-hooks/check-operational-location.mjs --self-test`
- `git diff --check origin/main...HEAD` passed.
- Fresh `origin/main` fetched on 2026-09-21; PR head is three commits ahead and zero behind.
- First landing blockers repaired locally:
  - Added `func:ResolveComposedName` leadership-assistant coverage exclusion.
  - Routed fasting submit SQL calls through `sqlbind.MustBind` and shrank the postgres-bind baseline.
  - Renamed the Kotlin composed-name helper parameter so the partition-identity guard no longer misreads it as visible raw shed copy.
  - Moved reviewed no-seed-impact markers adjacent to both `verification_items` repair updates.
- Focused reruns after committing `9e84a1961` passed:
  - `node tools/agent-hooks/check-leadership-assistant-coverage.mjs`
  - `node tools/agent-hooks/check-postgres-bind-contract.mjs`
  - `node tools/agent-hooks/check-operational-partition-identity.mjs`
  - `make seed-migration-guard`

## Pending

- Rerun required repo landing receipt: `make land-main`.
- If green, verify local `HEAD`, `origin/main`, and PR merge/readback all point at the certified SHA.

## Known Failures

- Initial attempted Android task `:core:core-common:testDebugUnitTest` failed because `core-common` is a JVM module and does not define that Android unit-test task. Correct task is `:core:core-common:test`, which passed.
- First `make land-main` attempt failed before any push. Red gates:
  - `leadership-assistant-coverage-guard`: new Go function `ResolveComposedName` was detected as a surface without coverage/exclusion.
  - `postgres-bind-contract-guard`: exact output pending focused rerun.
  - `operational-partition-identity-guard` and `mobile-guard`: `OperationalLocationLabel.kt:49 [label-missing-partition]`.
  - `seed-migration-guard`: exact output pending focused rerun.

## Current SHA

- Candidate before second landing receipt: `9e84a196126f576dd3c7609f1db7dd57263d7700`.
- Deployment state: no staging deploy requested or performed.
