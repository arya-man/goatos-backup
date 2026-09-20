# Weighing demographics parameter guard — 2026-09-20

## Scope

- Fix the production `expected 30 arguments, got 31` failure in sectioned weighing-demographics reads.
- Add regression coverage for every pruned query shape used by the Weighing Analytics tabs.
- Validate locally, obtain an independent judge review, then use the repository landing receipt before pushing `main`.
- No staging or production deployment is authorized in this task.

## Done

- Reproduced and traced the failure to `GetWeightDemographics`: the function always passes 31 arguments, while `$31` existed only in the `weight_bands` SELECT removed for other sections.
- Confirmed affected section requests: `dimensions`, `origin`, `shed_type`, and `weekly_gain`; `weight_bands` retains `$31` and does not hit this mismatch.
- Confirmed production logs report `expected 30 arguments, got 31` on the affected endpoint.
- Created clean isolated worktree `goatos-fix-weighing-param-guard` from `origin/main` at `6b2f3b470`.
- Started an independent judge review covering the last month of relevant history, the current sync/query architecture, and the proposed guard.
- Added `$31::numeric[]` to the always-retained `_param_types` CTE so pruning a tab-specific result SELECT cannot remove the final bound parameter.
- Added a table-driven guard over the real SQL for default/all, dimensions, origin, shed type, weight bands, and weekly gain variants. It checks all placeholders `$1` through `$31` survive and inactive result producers are actually pruned.
- Added a compact real-Postgres integration matrix that executes every frontend section shape against the migrated schema, specifically guarding argument binding independently of large business-data fixtures.
- Independent judge verdict incorporated: immediate fix accepted; real-query arity matrix, inactive-pruning assertions, and opt-in Postgres execution required before landing.
- Repository-wide `make ci-local`: GREEN across backend, required query plans, admin-web lint/typecheck/unit/phone guards/production build, and Android compile/unit/lint.

## Pending

- Rebase onto current `origin/main`, rerun the exact local landing receipt, push, and verify remote `main` read-back.

## Exact tests and E2E

- Existing focused guards: PASS on the broken code, demonstrating the coverage gap:
  - `go test ./internal/weighing/adapters/postgres -run 'TestWeightDemographicsSectionedReadsGateProducerCTEs|TestWeightDemographicsPrunesInactiveSectionSelectReferences' -count=1`
- Production evidence: FAIL for sectioned requests with HTTP 500 and `expected 30 arguments, got 31`.
- Post-fix local tests and browser/API evidence: pending.
- New real-query guard: PASS for default/all and all five frontend section shapes.
- New throwaway-Postgres binding matrix: PASS and executed (not skipped) for `dimensions`, `origin`, `shed_type`, `weight_bands`, and `weekly_gain`.
- Existing business-fixture Postgres tests: four PASS; shed-type fixture blocked before the target query by its unrelated stale `WG-ST-*` display IDs violating `goats_display_id_format_check`.
- `make ci-local`: GREEN. Admin-web sectioned aggregate reads, request plan, phone viewport guard, unit suite, and production build all passed. No authenticated local browser session was available; the exact backend contract was executed directly against fresh PostgreSQL instead.

## Known failures

- The current production-facing deployment is broken for Breed, Birth, Pen/Shed, and Time tab demographics requests.
- Existing source guards do not validate the final placeholder arity after inactive SELECT pruning.
- First version of the new pruning assertion incorrectly required inactive section flags to remain only in `_param_types`; shared CTE gating legitimately also uses them. The guard was refined to require a reduced occurrence count versus the real unpruned SQL, still detecting a silent pruning no-op.
- The pre-existing shed-type business fixture currently fails an unrelated newer `goats_display_id_format_check` constraint (`WG-ST-*` display IDs). Four other real-Postgres business tests passed. The new binding-focused matrix avoids that stale fixture while exercising the exact failing SQL contract.
- Optional non-blocking `ai-doctor` warned that fresh-worktree Repowise/code-review indexes are absent; all required CI checks remained green.

## Before / after

- Before: four tab-specific demographics query shapes return HTTP 500 because 31 arguments are supplied to SQL containing only 30 placeholders.
- After: all five pruned SQL variants execute successfully with the same 31 bound arguments on a fresh migrated PostgreSQL 16 database.

## Judge status

- Independent review complete; all requested P1/P2 guard refinements were incorporated.

## Source and deployment state

- Worktree base SHA: `6b2f3b470` (`origin/main` when work began).
- Production-facing deployed SHA observed: `08ab0c64fafc` (contains the regression).
- Current task deployment state: not deployed; deployment is out of scope.
