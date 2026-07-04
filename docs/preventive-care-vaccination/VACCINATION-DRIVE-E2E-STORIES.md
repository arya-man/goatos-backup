# Preventive Care Vaccination Drive E2E Stories

Date: 2026-07-04
Status: closeout evidence for the vaccination matrix publish path, generation
kernel, recovery/micro-drive rules, trusted-history handling, and leadership
screens.

This file is the business-readable proof log for the current GoatOS
Preventive Care vaccination slice. The PRD/TRD/rules documents define the
contract; this file records the concrete stories and automated checks used to
prove that contract during closeout.

## Ground Rules Proven In This Run

- One scoped `vaccination.matrix` version is the authoring source of truth for a
  company or park. The UI/backend may materialize `protocol_rules` and
  `protocol_rule_dimensions`, but those are derived execution rows, not separate
  per-vaccine source rules.
- Publish of a draft `vaccination.matrix` regenerates derived executable rows
  from `rule_dsl` inside one backend transaction. Stale manual row edits cannot
  override the current matrix JSON.
- Matrix row selectors inherit version-level eligibility defaults and normalize
  `any`/`*` to indexed `all` where the database prefilter needs wildcard
  semantics. The Go kernel still re-checks full eligibility before generating
  work.
- Publish rejects impossible or sloppy matrix data: unknown sex, duplicate
  normalized matrix row IDs, duplicate source-dose aliases, missing row
  metadata, missing matrix cells, procurement combo rows with more than two
  vaccines, and negative timing values.
- Cross-vaccine spacing reads the full recent vaccine history, not only the
  latest completion. A later killed vaccine cannot hide an earlier live vaccine
  and accidentally allow the next live vaccine too early.
- Sick/ICU/quarantine/defer recovery reopens missed work. The animal can join a
  compatible nearby park drive within the 7-day recovery buffer; otherwise it
  becomes a micro-drive. Recovery scheduling also re-checks live/killed spacing.
- Trusted vaccination history is limited to our supervised lifecycle: our parks
  and procurement holding parks with governed validation/proof. Third-party
  source notes do not suppress GoatOS work.
- Control Tower and Protocol Adherence use bounded backend pagination/filter
  behavior. Extreme page numbers do not produce backend offset failures, and
  filtered-empty screens say the filters matched no rows rather than giving a
  false "everything is healthy" signal.

## Story 1: CEO Publishes One Matrix, Not One Rule Per Vaccine

Scenario:

The CEO/COO edits one scoped vaccination matrix for the company or a park. The
matrix contains all vaccine rows and all animal selectors: species, breed,
shed/tag/stage, sex, health, reproductive blockers, procurement path, timing,
repeat/catch-up behavior, dose details, and compatibility policy.

Expected behavior:

1. Save/publish treats the matrix as one versioned ruleset:
   `vaccination.matrix`.
2. Backend derives executable `protocol_rules` and indexed
   `protocol_rule_dimensions` from the current `rule_dsl`.
3. Existing draft row data is deleted and regenerated at publish, so stale row
   metadata cannot drift from the matrix JSON.
4. The version becomes active only after all derived rows and dimensions are
   written successfully.

Proof:

- `(cd backend && go test ./internal/protocol/app -run 'TestPublishVersion.*Matrix|TestValidateVaccinationMatrix' -count=1)`
- `(cd backend && go test ./internal/protocol/app ./internal/protocol/adapters/postgres)`
- Specific tests:
  - `TestPublishVersionRegeneratesMatrixRulesFromRuleDSL`
  - `TestPublishVersionMatrixInheritsVersionEligibilityAndCanonicalizesAny`
  - `TestPublishVersionRejectsDuplicateMatrixRowIDAndSourceDoseAlias`
  - `TestPublishVersionRejectsNegativeMatrixTiming`
  - `TestPublishVersionRejectsProcurementComboOverTwoVaccines`

## Story 2: A Herd Animal Enters GoatOS And Gets Real Vaccination Work

## Story 1A: CEO-Friendly Matrix Authoring

Scenario:

The CEO/COO opens Config, creates one company vaccination matrix, loads the
approved Vaccination Rules plan, reads the timing in business language, toggles
one vaccine off/on, checks goat-only/sheep-only matrix scopes, checks the park
override selector, edits proof requirements, previews impact, saves, publishes,
and reopens the published drawer.

Expected behavior:

1. The screen looks like a guided matrix plan, not a raw engineering table.
2. Vaccine cards show readable timing such as `primary: 4w`, `booster: 7w`,
   and `repeat 6 months`.
3. Predefined safety rules are visible as read-only facts: max 2 vaccines per
   visit, live-live 28-day gap, live/killed safety spacing, pregnancy months
   4-5 skip, clinical defer states, mother unknown ignored, and one 7-day
   batching buffer.
4. Editable fields are only the matrix business policy: active vaccines,
   species/stage/sex/breed selectors, dose timing, dose amount, vial size,
   revaccination, max delay, minimum gap, proof policy, and procurement holding
   waves.
5. The animal segment is not a cosmetic filter: selecting `Goats` serializes
   only goat-applicable rows and narrows shared goat/sheep vaccines to goat;
   selecting `Sheep` does the same for sheep. Returning to `Goats + sheep`
   serializes the full mixed-species matrix.
6. The `One park` control actually selects a park scope; `Whole company`
   restores the company default scope.
7. Save stores one `vaccination.matrix` draft version; publish replaces the
   older active company/park matrix for the same scope by retiring it, then
   activates the new version. Historical completed work keeps its old
   `protocol_version_id`.

Proof:

- `npm --prefix apps/admin-web run smoke:vaccination-authoring:live`
- Report:
  `.codex-goatos-render/vaccination-authoring/AUTHORING-2026-07-04T21-04-59-968Z/authoring.md`
- Current run verified:
  - SOP builder create/validate/dry-run/publish/reopen/edit/republish.
  - Config matrix load shows ET+TT, PPR, Goat Pox, FMD, HS, Blue Tongue, and
    Sheep Pox.
  - Goats scope shows 5 active rows and JSON species `goat`.
  - Sheep scope shows 6 active rows and JSON species `sheep`.
  - Goats + sheep restores 7 active rows.
  - One park selects a `park:*` scope; Whole company restores `tenant`.
  - ET+TT business summary includes `primary: 4w` and `repeat 6 months`.
  - Read-only safety copy includes live-live, pregnancy, mother-unknown ignore,
    and trusted holding-source rules.
  - Blue Tongue toggle changes active count from 7 to 6 and back to 7.
  - Goat Pox row shows 20-week timing and revaccination metadata.
  - Proof policy edit persists a new `video` token.
  - Preview, save, publish, and published drawer reopen pass.

## Story 2: A Herd Animal Enters GoatOS And Gets Real Vaccination Work

Scenario:

A valid animal is registered or accepted into GoatOS with species, breed, sex,
age/stage, shed/tag, lifecycle, health, and reproductive facts. The active
matrix applies to that animal.

Expected behavior:

1. The animal event/generation path evaluates the matrix per animal.
2. The kernel creates per-animal due obligations.
3. The sweeper/planner groups due obligations into park-level drive work with
   shed/tag context.
4. SOP proof is attached to the generated work.
5. Accepted proof records vaccination history and schedules configured next
   doses/repeats.

Proof:

- Full local browser/API smoke: `GOATOS_E2E_RUN_ID=VACCINATION-CLOSEOUT-20260704-R3 bash tools/dev/admin-web-e2e-smoke.sh`
- Smoke report:
  `.codex-goatos-render/e2e-smoke/VACCINATION-CLOSEOUT-20260704-R3`
- Screenshots:
  `.codex-goatos-render/admin-web-screenshots/2026-07-04T19-02-07-448Z`
- Concrete smoke IDs:
  - chain animal: `c961f3ae-0c82-4d87-a5fe-6989197256db`
  - chain obligation: `27322cf7-bf45-4074-8fc1-285a9bf76a83`
  - chain batch: `6dc09507-0de1-4edb-b0d3-4c84025301e7`
  - chain task: `7da2941b-6e10-4695-840d-fa7b68db1ecf`
  - accepted completion: `0026324b-0ebd-40fe-818f-4122e22add3b`
  - booster obligation: `b53ab7d5-7fdc-4146-8aad-80fd17337937`
  - visual animal used for browser clicks: `3cf77ffc-5a76-4532-ba57-b425e611f59b`

## Story 3: Procurement Intake Starts Only After Valid Acceptance

Scenario:

Purchased animals arrive through Procurement Source Entry. Some animals are
accepted as canonical herd animals; others are rejected, owner-missing, or
source-only.

Expected behavior:

1. Accepted intake creates canonical herd animals that can enter Preventive Care
   vaccination.
2. Rejected/source-only/owner-missing rows do not create normal vaccination
   obligations.
3. Sex is always male/female; unknown sex is invalid and never accepted into the
   herd.
4. Procurement holding-park evidence can suppress duplicate work only when it
   is our governed holding flow with accepted proof.

Proof:

- Full smoke procurement matrix inside
  `VACCINATION-CLOSEOUT-20260704-R3`
- Concrete smoke IDs:
  - load: `47133cbb-5601-427c-9b4b-fdfa4e07fdfb`
  - clean accepted animal: `7c8e0a38-a58b-411a-83ae-5eabae61314d`
  - rejected animal: `edd63fff-84af-4ca7-9a0b-955a658125ed`
  - owner-missing row: `65f69398-bca5-40b6-8e0f-27d0275e2d7a`
  - generated obligation: `e3356447-ec04-4c7e-aa73-d5faa2da13e1`
  - generated batch: `5d92fb34-a751-42f9-8462-007ef9673a78`
  - generated task: `00acb897-796b-4dd6-a1b4-194917aed8af`

## Story 4: Trusted Holding-Park History Suppresses Duplicate Doses

Scenario:

A kid has a trusted ET+TT first dose recorded from our supervised lifecycle.
GoatOS must not duplicate that first dose. It must use the trusted history to
create the next configured dose at the correct gap.

Expected behavior:

1. Trusted dose 1 suppresses the matching due obligation.
2. The next missing dose is generated from the trusted administered date.
3. The kernel does not trust third-party/vendor source claims outside our parks
   or procurement holding parks.

Proof:

- `bash tools/dev/vaccination-trusted-history-proof.sh`
- Run stamp: `1783192047`
- Concrete IDs:
  - animal: `49bdd9a7-6ce3-4b43-8b16-8fe4425bd2a5`
  - accepted old 4-week obligation:
    `07aec402-5eba-461d-bbba-bd26c6022228`
  - next 7-week obligation:
    `4f8cb6b9-ead2-44ec-a448-8c0e5ae61667`
  - completion: `826d97cc-a803-4bc7-b033-52cd6768d434`
  - batch: `9647dbce-62de-49f4-83ef-db68b34f9214`
  - task: `bc794cfe-212c-4700-9005-03a210bd061e`
- Unit proof:
  - `TestOlderGoatTrustedFirstDoseAllowsNextMissingDose`
  - `TestTrustedPreviousCompletionAllowsAfterPreviousCompletionDose`
  - `TestTrustedAfterPreviousCompletionSuppressesAlreadyAcceptedDose`

## Story 5: Rejected Proof Creates Rework, Corrected Proof Completes

Scenario:

A vaccination proof submission is rejected. The animal must not silently become
vaccinated. The system must create rework, then accept corrected proof and
record history only after acceptance.

Expected behavior:

1. Rejected submission is visible as rejected/rework.
2. Stock/history are not treated as accepted for the rejected proof.
3. Corrected accepted proof records vaccination completion and closes the work.

Proof:

- `bash tools/dev/vaccination-rework-proof.sh`
- Run stamp: `1783191828`
- Concrete IDs:
  - animal: `e946805c-8281-4e49-a748-f1ceae73c042`
  - original obligation: `b6deb7e2-4d7a-495b-a0db-14c3a37008f2`
  - rejected submission: `f959c4db-8f22-49f3-b74e-6098f72dc78a`
  - rejected completion: `14d35fa9-8f60-4d7a-be54-7f2b37d8bb32`
  - corrected submission: `0d19c1c3-429f-4f57-82cd-700a3ff43fd4`
  - accepted completion: `b03b74a4-9897-4949-becc-9c84cc8edb8a`
  - booster obligation: `b7a2e5a7-0ec0-435b-bdbb-170e614c2e83`

## Story 6: Recovery Rejoins Nearby Drive Or Becomes Micro-Drive

Scenario:

An animal was deferred because it was sick/ICU/quarantine. It recovers and
returns to its original valid shed/tag. A park drive may already be planned.

Expected behavior:

1. If a compatible park drive is within 7 calendar days of recovery, place the
   animal into that drive.
2. If the compatible park drive is more than 7 days away, schedule a micro-drive
   inside the buffer, even for one animal.
3. Multiple recovered animals with overlapping 7-day buffers can be batched.
4. The 7-day batching rule cannot override medical spacing. If a recent live
   vaccine requires a 4-week gap, the recovered animal waits until that safe
   date instead of joining an unsafe nearby drive.

Proof:

- `(cd backend && go test ./internal/vaccination/app -run 'TestRecoveryReschedule|TestGoatRecheck' -count=1)`
- Specific tests:
  - `TestRecoveryRescheduleDueUsesOneWeekMaxBuffer`
    - July 4 and July 5 recovery can join July 10.
    - July 4 recovery cannot wait for July 12; it becomes a micro-drive.
  - `TestGoatRecheckHandlerReopensOnHealthRecovery`
  - `TestGoatRecheckHandlerAlignsRecoveredGoatToNearbyDrive`
  - `TestGoatRecheckRecoveryRescheduleKeepsCrossVaccineGapFloor`
    - recent live PPR on June 1 blocks recovered Goat Pox from joining a June 5
      drive and pushes it to June 29.

## Story 7: Compatibility And Max-Safety Rules Survive Booster Scheduling

Scenario:

A later same-day-compatible killed dose exists after an earlier live dose. A
future live vaccine must still honor the earlier live dose's 4-week gap.

Expected behavior:

1. Cross-vaccine gap checks look across the full recent history.
2. Generation and booster scheduling both apply the same safety logic.
3. Live-live, live-killed, killed-killed, and allowed same-day bacterial/viral
   combinations stay explicit.

Proof:

- `(cd backend && go test ./internal/vaccination/app -run 'TestGenerateForVersionChecksAllRecentVaccinesForCrossGap|TestScheduleNextDoseChecksAllRecentVaccinesForCrossGap|TestCrossVaccineGapDays|TestApplyCrossVaccineGapFloor' -count=1)`
- Specific tests:
  - `TestGenerateForVersionChecksAllRecentVaccinesForCrossGap`
  - `TestScheduleNextDoseChecksAllRecentVaccinesForCrossGap`
  - `TestCrossVaccineGapDaysLiveToLive`
  - `TestCrossVaccineGapDaysLiveToKilledDefaultGap`
  - `TestCrossVaccineGapDaysBacterialViralSameDay`
  - `TestCrossVaccineGapDaysLiveViralKilledViralSameDay`

## Story 8: Leadership Screens Do Not Lie When Filters Are Empty

Scenario:

Control Tower or Protocol Adherence filters produce zero rows, or a user opens
an extreme page number.

Expected behavior:

1. The page loads with HTTP 200.
2. Extreme page numbers are clamped to a safe backend offset.
3. Filtered-empty state says no rows match the filters.
4. It must not show a global "no risk/everything healthy" message merely
   because the current filter set is empty.

Proof:

- Full browser/API smoke:
  `GOATOS_E2E_RUN_ID=VACCINATION-CLOSEOUT-20260704-R3 bash tools/dev/admin-web-e2e-smoke.sh`
- Live checks against running local stack:
  - `/?ct_page=999999999&ct_limit=50&scope_mode=company`
  - `/protocol-adherence?adh_page=999999999&adh_limit=50&scope_mode=company`
  - `/?ct_severity=ok&ct_state=completed&ct_page=1&ct_limit=10&scope_mode=company`
- Unit proof:
  - `TestProtocolAdherenceSummaryUsesFullFilteredSetNotCurrentPage`

## Closeout Checklist

| Area | Required variation | Evidence | Result |
| --- | --- | --- | --- |
| Matrix storage | Single scoped `vaccination.matrix`, not one top-level rule per vaccine | Protocol publish tests | PASS |
| CEO matrix authoring | Guided plan cards, read-only predefined safety rules, animal scope serializes into JSON, park/company scope controls, editable timing/proof/procurement fields, save/publish/reopen | `smoke:vaccination-authoring:live` `AUTHORING-2026-07-04T21-04-59-968Z` | PASS |
| Active version replacement | Publishing a new scoped matrix retires the prior overlapping company/park active matrix instead of blocking forever | `TestPublishVersionWithDerivedRulesReplacesOverlappingVaccinationMatrixFamily` | PASS |
| Matrix drift | Stale draft `protocol_rules` regenerated from current `rule_dsl` | `TestPublishVersionRegeneratesMatrixRulesFromRuleDSL` | PASS |
| Matrix selectors | `any` wildcard keeps old semantics through compiled dimensions | `TestPublishVersionMatrixInheritsVersionEligibilityAndCanonicalizesAny` | PASS |
| Scale prefilter | Derived dimensions exist for indexed rule prefilter, Go re-checks full eligibility | Protocol adapter/app tests | PASS |
| Bad authoring | Unknown sex, negative days, duplicate row/source aliases rejected | Protocol publish tests | PASS |
| Procurement combo | More than 2 vaccines in one procurement combo rejected | `TestPublishVersionRejectsProcurementComboOverTwoVaccines` | PASS |
| Trusted source | Our supervised holding/park history suppresses duplicate dose and schedules next | `vaccination-trusted-history-proof.sh` | PASS |
| Untrusted source | Third-party claims do not suppress GoatOS schedule | Contract in `vaccination-rules.md`; trusted proof path scopes accepted evidence | PASS |
| Recovery | July 4/5 recovery can join July 10; July 4 cannot wait for July 12 | `TestRecoveryRescheduleDueUsesOneWeekMaxBuffer` | PASS |
| Micro-drive | No nearby safe drive means reopen now, even for one animal | `TestGoatRecheckHandlerReopensOnHealthRecovery` | PASS |
| Recovery + spacing | Nearby drive loses when live-live gap requires later safe date | `TestGoatRecheckRecoveryRescheduleKeepsCrossVaccineGapFloor` | PASS |
| Full vaccine history | Later killed dose cannot hide earlier live dose | Generation + booster cross-history tests | PASS |
| Proof rework | Reject -> rework -> corrected accepted proof | `vaccination-rework-proof.sh` | PASS |
| UI shell | CT, PA, Vaccination, Action Center, Calendar, Workflows, Config, SOP, Procurement, Passport render from local stack | `admin-web-e2e-smoke.sh` | PASS |
| Click matrix | Shell/sidebar, Vaccination, Action Center, Protocol Adherence, Workflows, Config, and full-page SOP Library controls | `smoke:vaccination-click-matrix:live` `CLICK-2026-07-04T21-05-45-132Z` | PASS |
| CT/PA pagination | Extreme pages do not hit backend offset error | Live local HTTP checks | PASS |
| CT filtered empty | Filtered zero rows shows filtered-empty copy, not false green | Live local HTTP check | PASS |

## Commands Run For This Closeout

Run the two proof scripts sequentially. They intentionally clean up open rows
from prior proof sheds, so parallel proof runs can cancel each other's temporary
fixtures.

```bash
(cd backend && go test ./internal/protocol/app ./internal/protocol/adapters/postgres ./internal/vaccination/app)
npm --prefix apps/admin-web run typecheck
npm --prefix apps/admin-web run check:mock-fidelity
npm --prefix apps/admin-web run smoke:vaccination-authoring:live
npm --prefix apps/admin-web run smoke:vaccination-click-matrix:live
GOATOS_E2E_RUN_ID=VACCINATION-CLOSEOUT-20260704-R3 bash tools/dev/admin-web-e2e-smoke.sh
bash tools/dev/vaccination-chain-proof.sh
bash tools/dev/procurement-vaccination-e2e-matrix.sh
bash tools/dev/vaccination-trusted-history-proof.sh
bash tools/dev/vaccination-rework-proof.sh
(cd backend && go test ./internal/vaccination/app -run 'TestRecoveryReschedule|TestGoatRecheck|TestOlderGoat|TestSchedule' -count=1)
(cd backend && go test ./internal/protocol/app -run 'TestPublishVersion.*Matrix|TestValidateVaccinationMatrix' -count=1)
```

The final push gate for this branch also requires the full repo verification
suite listed in the final assistant response for the commit.
