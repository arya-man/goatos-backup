# Weighing SOP: the weighing session rules are authored on the web (maintainer decision 2026-09-15)

Status: accepted and implemented · Owner: weighing + weighingsop + sop + adminui + admin-web + Android
Machine enforcement: `make weighing-sop-guard` (`tools/agent-hooks/check-weighing-sop-guard.mjs`)
plus the golden tests `backend/internal/weighing/domain/sop_test.go`
(`TestSeededWeighingSOPIsThePreSOPBehaviour`, `TestMigrationEmbedsTheSeededWeighingSOP`).

## Decision

The Weighing Session SOP (`weighing.session`, seeded as a library document by migration 000186)
stops being a library document. The rules a weighing task is planned on and runs under are the
**`weighing` section of the PUBLISHED `weighing.session` version**, authored on **Weighing →
Weighing SOP** (`/weighing/sops`):

| Rule | What the author decides | Who reads it |
| --- | --- | --- |
| **Planning** | which ways of weighing the planner may pick (animal by animal / whole pen) and the default animals per day | the plan wizard (offered modes, prefilled cap); the create (a mode the SOP does not offer is refused, `422 weighing_mode_not_offered`) |
| **Feed & water removal** | `required` (every task, the 2026-09-03 rule) / **`optional` (the planner decides per task, on by default)** / `off` (never); the instruction on the removal card; the two proof slots' titles and hints; extra **questions** the removal operator answers per pen | the plan wizard (removal step shown / toggle / hidden; today offerable when the removal does not apply); the create and edit (operator mandatory only when the removal applies; the evening cutoff only then; a past date `422 weigh_date_in_past`); the removal card (copy, questions); the pen submit (`422 fasting_answer_invalid` naming the question) |
| **Capture** | the lump-sum video window (min..max, inside the proof policy's ceiling of 5); the per-animal video, shown **locked on** | the phone's lump-sum capture (cap and submit gate); the backend submit (`422 weighing_video_count`) |

This is what the maintainer asked for on 2026-09-15 -- "everything should be SOP; keeping
optional of feed and water removal, that should be from SOP" -- and it supersedes, for the
weighing module only, two earlier words:

1. migration `000186`'s header -- "library documents, not a second execution engine";
2. `docs/decisions/feed-water-removal-precondition.md` rule 1 -- "for weighing this applies to
   **every** task". It applies to every task **when the published SOP says `required`**, which
   is what the seed says, so nothing changed on deploy; a farm that publishes `optional` or
   `off` has chosen otherwise on the SOP page, with the trade stated there (an unfasted weigh is
   a wrong weight).

## What is NOT authorable here, on purpose

The scan-and-submit locks stay locks and the document says so on screen: free-flow capture (a
tag is stored as scanned, never checked against a pen or roster), the one business rule (no
duplicate scan in a pen before submit), evidence-grain verification, the approve-carries-the-
weight correction, the unconditional close gate. The per-animal video is a field in the
document only so that it states the rule; `video_required: false` is refused at save. The two
removal clips are fixed in KEY and KIND (`feed_video`, `water_video`, video): the evidence table
carries exactly those two, the verifier item is built from them and the midnight gate counts
them -- their WORDING is the author's. The removal evening cutoff stays in
`feed_water_removal_config` (one farm evening, shared with PC Care deworming; maintainer
decision 2026-09-07) and is not moved into this document.

## The shape

- `form_dsl.weighing` (`schema_version: goatos.sop-weighing.v1`) = `planning {modes[],
  default_cap_per_day}`, `feed_water_removal {mode, instruction, proofs[{key,title,hint,kind}],
  questions[]}`, `capture {individual {video_required}, lump_sum {video_min, video_max}}`. A
  question carries the same fields the procurement inspection's questions do (`id`, `kind`
  choice / multi / text / number, `title`, `hint`, `required`, `options` + `allow_other`,
  `min` / `max` / `unit`, `only_if`); there is no media kind, because the two clips ARE the
  card's media.
- `weighing/domain.ParseWeighingSOP` + `ValidateWeighingSOP` name every problem by path;
  `weighingsop/app.WeighingSOPContract` runs them at version create through
  `sop/app.WithFormDSLContract`, so a document the planner could not run is never saved and the
  section is REQUIRED on a `weighing.session` version.
- **Versioning and the pin.** `PublishedRules` is what a task planned now is stamped with
  (`weighing_campaigns.sop_version`); the task runs on that version to the end -- an edit, its
  lump-sum submits and its removal cards all read `RulesVersion(pinned)`, never the latest
  publish (the pin-at-start rule of the herd-operations and procurement SOPs). Version 0 is the
  seeded document. A task pinned to a version the farm never published is refused by name on
  the write paths (`409 weighing_sop_version_unknown`) and rendered with the seeded copy on the
  read paths so tonight's card is still workable.
- **Weighing stays isolated.** `backend/internal/weighing` never names `sop_versions`: it holds
  `ports.SOPRulesSource` / `ports.SOPPinReader`, and the only file naming the SOP tables on its
  behalf is `backend/internal/weighingsop/adapters/postgres/rules_source.go` -- the
  `feedwaterremoval` shape. Unwired, the service runs the seeded rules, so every existing fake
  and every process without the adapter behaves exactly as before.
- **Day one is the current behaviour.** `weighing/domain/sopseed/weighing_session.json` is the
  pre-SOP behaviour byte for byte -- removal required, both clips, no questions, cap 100, 1..5
  lump-sum videos, both modes -- pinned by `TestSeededWeighingSOPIsThePreSOPBehaviour`.
  Migration `000314` adds it IN PLACE to each tenant's currently published `weighing.session`
  version (the 000308 shape: the version a farm sees on deploy is the one it already had, now
  carrying the rules), inserts the 000186 definition + v1 for a tenant that has none, adds the
  two columns, and embeds the seed verbatim (`TestMigrationEmbedsTheSeededWeighingSOP`).
  Tasks planned before the migration carry `sop_version NULL` = the seed = what they already do.
- **Web.** The `/weighing/sops` drawer summarises the rules (`weighing-summary.tsx`); **Change
  SOP** opens the rules editor (`features/sops/weighing-editor.tsx`, model
  `weighing-model.ts` -- round-trip of the seed is byte-faithful, `weighing-model.test.mjs`);
  Save as draft / Publish SOP go through `saveWeighingVersion` / `publishWeighingVersion`, which
  keep the capture form and proof policy of the published version and replace only `weighing`.
  Copy and option groups are the page contract's (`weighingSOPEditorCopy`,
  `weighingSOPOptionGroups`).
- **Phone.** The planner catalog carries `sop` (the published rules) and the task read carries
  the pinned `sop`; both are Room-cached (the rules ride the weighing blob cache under
  `weighing.planner.sop`, no new table). The plan wizard reads the removal mode BEFORE a date is
  picked (`observePlannerSop` / `refreshPlannerSop`), offers today when the removal does not
  apply, shows a toggle under `optional` (forced off, with the reason, for a date whose evening
  is already gone), hides the step under `off`, filters the offered modes, prefills the cap, and
  sends `feed_water_removal_requested` with no operator when the removal does not apply. An
  edit runs on the task's pinned rules from the seed. The removal card renders the instruction,
  slot wording and authored questions verbatim; a required question blocks the submit by name;
  the answers ride the outbox submit and survive process death in the SavedStateHandle. Lump-sum
  capture reads the pinned video window from its assignment rows.

## Backend contract additions

`WeighingSOPRules` on `WeighingPlannerCatalogResponse.sop` and `WeighingCampaign.sop`
(+ `sop_version`); `feed_water_removal_requested` on `CreateWeighingCampaignRequest`
(`fasting_operator_user_id` no longer required by the contract -- it is required by the RULES
when the removal applies); `instruction`, `proofs`, `questions`, `answers` on
`WeighingFastingShedCard`; `answers` on `SubmitWeighingFastingShedRequest`. New refusals:
`weighing_mode_not_offered`, `feed_water_removal_not_offered`, `weigh_date_in_past`,
`feed_water_removal_locked` (switching the removal off after a pen was submitted),
`weighing_video_count`, `weighing_sop_version_unknown`, `fasting_answer_invalid`.

## Proof

- Backend: `go test ./internal/weighing/... ./internal/weighingsop/... ./internal/adminui/app/`
  green; `TestCreateCampaignUnderOptionalRemovalHonoursThePlannerChoice`,
  `TestCreateCampaignUnderOffDropsTheOperatorAndRefusesAnExplicitAsk`,
  `TestCreateCampaignUnderRequiredIgnoresADecline`,
  `TestUpdateCampaignRunsOnThePinnedVersionNotTheLatestPublish`,
  `TestUpdateCampaignSwitchingRemovalOffIsLockedByASubmittedPen`,
  `TestRecordShedObservationHonoursThePinnedLumpSumVideoWindow`,
  `TestSubmitFastingShedValidatesAnswersAgainstThePinnedVersion`,
  `TestListMyFastingShedCardsCarriesEachTasksPinnedCopy` and the domain refusal matrix
  (`TestValidateWeighingSOPRefusesTheLockedRules`, `TestValidateRemovalAnswersJudgesByTheDocument`).
  Mutation-tested when written: dropping the OFF operator-drop and reading the published rules
  on an edit each turn the named test red.
- Web: `features/sops/*.test.mjs` (18) green, `tsc --noEmit` clean, IA guard clean.
- Android: `WeighingPlanWizardSopRulesTest` (5), `WeighingFastingDetailViewModelTest` (7 incl.
  the authored-questions case), `WeighingPlanWizardFastingOperatorTest`,
  `WeighingPlanWizardEditHydrationTest`, core-data weighing / sync suites green; guards
  (`weighing-free-flow-guard`, `mobile-guard`, `design-system-guard`, `telemetry-guard`,
  `exception-guard`, `room-migration-guard`, `android-navigation-stack-guard`) green.
- **Not yet done:** a live run -- migration 000314 on a STG clone, the editor exercised in
  Chrome, a plan under `optional` and a removal card with questions on the Realme. The phone
  and web work is unit-tested and compiled, not surface-proven.

## Not here (phase 2)

Authored questions on the lump-sum pen submit and per animal; the `/config` registry editor for
question kinds; moving the removal cutoff into the document (it is a farm-wide evening shared
with deworming and stays config); making a removal clip optional (the evidence table, the
midnight gate and the verifier item are built on both).
