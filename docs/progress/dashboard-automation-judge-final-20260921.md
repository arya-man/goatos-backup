# Dashboard Automation Judge Final Pass - 2026-09-21

## Scope

Review and harden the dashboard automation / OCI parity candidate before landing to `main`.

Covered surfaces:
- dashboard automation runner defaults, Slack, self-heal PR path, and OCI runbook;
- admin-web route, module journey, laptop/mobile visual smoke guards;
- STG/OCI business-data parity safety and critical-table comparison semantics;
- recent bug classes since 2026-08-01, including feed config dropdown identity, Godel/Manohar ADG alias/window drift, operational-location doubled partition labels, vaccination lifecycle tests, latency/Lighthouse/Grafana toggles, and Slack alerts.

## Done

- Made STG/OCI business-data parity default-on for the OCI runner.
- Added catalog/grant read-only proof for parity DB identities without attempting writes.
- Upgraded critical-table parity from count-only to count plus deterministic content fingerprints.
- Changed CBE Herd Analytics sentinel to use the current IST weekly window rather than a fixed September 2026 date.
- Marked Castro reconciliation as dated field evidence, not evergreen live truth.
- Surfaced Herd Signal best-effort residual risk in the runbook.
- Moved self-heal PR reporting into a separate temporary git worktree so a failing run does not mutate the runner checkout.
- Added live ADG API semantic smoke over 14/21/28-day windows and a visual failure for doubled operational partition labels.
- Locked the above protections into `make dashboard-automation-guard`.

## Tests / Evidence

- `make dashboard-automation-self-test` - PASS.
- `make dashboard-automation-guard` - PASS (`63 filesystem routes`, `130 smoke entries`).
- `node --test apps/admin-web/scripts/smoke-visual-route-coverage.test.mjs apps/admin-web/features/weighing/time-wise-controls.contract.test.mjs` - PASS (`19` tests).
- Earlier focused vaccination lifecycle gates in this pass:
  - `go test ./internal/vaccination/app -run 'TestGenerateForVersionUsesTopLevelProcurementFirstWaveWhenPurposeBlank|TestGenerateForVersionUsesProcurementPurposePlans|TestGenerateForVersionDefersDuringWarmupHold|TestGenerateForVersionTreatsTerminalAndClinicalStatesDifferently' -count=1` - PASS.
  - `go test ./internal/obligation/adapters/postgres -run 'TestPublishingAnAddedVaccineLeavesTheOtherFiveUntouched|TestCarryOverRebindsMedicalEquivalentPrimaryCourseFollowUp|TestGoatExitedRemovesAnimalFromPlannedDriveAssignments|TestMatrixClinicalDeferHoldsWorkAndReleasesPlannedDrive|TestMatrixClinicalRecoveryReopensWithoutCorruptingPlannedDrive' -count=1` - PASS.
- `make api-latency-policy-test` - PASS (`76` tests).
- `git diff --check` - PASS.

## Judge Status

- OCI parity/data guard judge: SIGN-OFF after fixes.
- Dashboard automation coverage judge: SIGN-OFF after fixes.

Residual risks called out by judges:
- Full credentialed production smoke, DB parity, Slack posting, GitHub PR creation, and live browser journeys still require the real OCI/STG/prod env and tokens.
- Critical-table fingerprints can be expensive on large tables.
- Dynamic IST windows can drift if STG and OCI are queried across midnight.
- Castro reconciliation is dated field evidence, not evergreen live truth.

## Pending

- Commit final hardening changes.
- Run `make land-main` from this isolated worktree.
- Verify landed SHA equals local `HEAD`, `origin/main`, and remote main.

## Current SHA / Deployment

- Candidate before final commit: `dde6fc2ccd573ea391e3f7d859c629fdecf677f7`.
- Deployment state: no staging or production deploy run in this pass.
