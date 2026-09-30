# Goat OS Active Progress

Last updated: 2026-10-01 02:47 IST

## Current Scope

- PR: `#460` - `feat(kid shifts): farm-born females owe the park head a move to Non-Pregnant at 10 weeks`
- Isolated worktree: `/Users/raviteja/mesha/goatos-wt-pr460-review`
- PR head at start: `20d0a713db543701b107452b70ab8ebdc793f060`
- Base at start: `origin/main` = `7863cdc9566467511a53a1611b7af94b9d1d2014`
- Requested outcome: land/merge to `main`, then deploy STG backend/admin-web/mobile.

## Done

- Read-only PR review completed before landing request.
- No blocking review findings were found.
- Focused checks passed:
  - `go test ./internal/counts/domain ./internal/tasks/domain`
  - `go test ./internal/counts/adapters/http ./internal/counts/app ./internal/tasks/adapters/http`
  - `go test ./internal/tasks/adapters/postgres -run 'TestKidShift|TestLitter|Test.*Workflow|Test.*Board'`
  - `node --test --experimental-strip-types apps/admin-web/features/sops/followup-model.test.mjs`
- Landing prep identified one whitespace gate issue:
  - `backend/internal/counts/adapters/postgres/shifting_destinations_integration_test.go:831: new blank line at EOF`
- First `make land-main` attempt failed on `sop-driven-herd-operations-guard` because the updated
  `counts_birth_litter_track.json` seed was not present verbatim in a `$seed$` migration block.
- Repaired `000463_kid_shift_non_pregnant_step.sql` with a full seed pin block comment while leaving
  runtime SQL additive over `000462`.
- Focused repair checks passed:
  - `GOATOS_CI_ONLY_STEP='sop-driven-herd-operations-guard' tools/ci/run-local-ci.sh backend`
  - `go test ./internal/tasks/domain -run TestMigrationEmbedsTheKidShiftSeed`
  - `git diff --check`
- Second `make land-main` attempt failed on `seed-migration-guard`; it flagged the
  `animal_stage_lookup` default update in `000463`.
- Added the required reviewed `seed-migration-guard:ignore` marker adjacent to the `animal_stage_lookup`
  update, tied to the existing growth age-entry tests.
- Focused seed repair checks passed:
  - `GOATOS_CI_ONLY_STEP='seed-migration-guard' tools/ci/run-local-ci.sh backend`
  - `go test ./internal/counts/domain -run 'TestGrowthTakesAnOldEnoughFemaleStraightToNonPregnant|TestGrowthStagesBeforeIgnoresTheReverseEdge'`
  - `git diff --check`

## Pending

- Commit the seed-migration guard marker repair.
- Rerun authoritative local landing receipt: `make land-main`.
- Verify local `HEAD`, local `origin/main`, and remote `main` all match the landed SHA.
- Deploy using guarded launcher:
  - `GOATOS_REPO=/Users/raviteja/mesha/goatos-wt-pr460-review /Users/raviteja/bin/goatos-stg-deploy backend-web-mobile`
- Verify STG separately:
  - Cloud Build / rollout success
  - Cloud Run 100% traffic
  - public API `/livez` and `/readyz`
  - `/version` SHA and `migration_drift=false`
  - mobile distribution terminal receipt (`MOBILE_DISTRIBUTED`)

## Known Failures Or Blockers

- GitHub reports PR merge state as `BLOCKED`; local certified direct landing is being used only if `make land-main` passes.
- First landing attempt was red on `sop-driven-herd-operations-guard`; focused repair is green, full receipt still pending.
- Second landing attempt was red on `seed-migration-guard`; focused repair is green, full receipt still pending.
- A broad admin-web test attempt expanded to the full suite and failed on missing local dependencies (`typescript`, `@grafana/faro-core`); changed SOP model test passed directly.
- No STG or mobile deployment has started yet.

## Stop Rule

- Do not claim main landing until `make land-main` passes and SHA readback matches local and remote `main`.
- Do not claim STG/mobile deployment from Cloud Build alone; verify runtime health, version/migration state, traffic, and mobile distribution separately.
