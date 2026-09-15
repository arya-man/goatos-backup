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
| **Feed & water removal** | `required` (every task, the 2026-09-03 rule) / **`optional` (the planner decides per task, on by default)** / `off` (never); **the evening cutoff** (`cutoff_time`, blank = the farm-wide `feed_water_removal_config` evening shared with deworming); the instruction on the removal card; **the captures the card asks for** -- up to eight slots, each with a key, title, hint, kind (`video` / `photo` / `either`) and a required flag, at least one compulsory; extra **questions** the removal operator answers per pen | the plan wizard (removal step shown / toggle / hidden; today offerable when the removal does not apply; the toggle forced off with the reason once the pinned evening is gone); the create and edit (operator mandatory only when the removal applies; the effective evening only then, `422 fasting_window_closed` past it; a past date `422 weigh_date_in_past`); the removal card (copy, slots, questions, the effective evening); the pen submit (`422 fasting_proof_slot_invalid` naming the slot, `422 fasting_answer_invalid` naming the question); the card list (opens at each card's PINNED evening) |
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
document only so that it states the rule; `video_required: false` is refused at save.

## The removal card's captures are authored (maintainer ask 2026-09-15, same day)

"If I want to add a photo also with video, or replace video with photo, or add one more step,
everything should work." So the removal card's captures are no longer two fixed clips. The
document carries `feed_water_removal.proofs[]` -- key, title, hint, kind `video` / `photo` /
`either`, `required` -- and the evidence row stores them as `sop_proofs {slot key: proof ref}`
(migration `000315`; the legacy `feed_proof_ref` / `water_proof_ref` pair mirrors the seeded
`feed_video` / `water_video` slots and is backfilled, so every pre-existing reader still
reads). The verifier item and the midnight gate are built from the ordered slot refs. Limits:
one to eight slots, unique keys in the id pattern, at least one compulsory slot unless the
mode is `off`, a slot `required` flag that is ABSENT reads as compulsory (a document authored
before the flag existed keeps its meaning). The submit is judged slot by slot: a compulsory
slot missing, an unknown slot, one capture proving two slots, or a capture of the wrong kind
for its slot is `422 fasting_proof_slot_invalid` naming the slot; an older phone that still
sends the legacy pair is mapped onto the seeded slot keys and judged the same way.

**The evening cutoff is authored too.** `feed_water_removal.cutoff_time` (`HH:MM`, Asia/Kolkata)
overrides the farm-wide `feed_water_removal_config` evening for weighing; blank keeps the farm
evening, which is what the seed says, so deploy changes nothing and deworming is untouched.
Every served rule set carries the EFFECTIVE evening (`cutoff_time` filled with the SOP's own or
the farm's) -- planner catalog, task read, list rows and removal cards -- so no client resolves
it. The pin decides: a task planned under 21:30 runs under 21:30 to the end, its card prints
21:30 and its card OPENS at 21:30 (`ports.RemovalCutoffs`, resolved per pinned version and
bound into the list SQL as jsonb), whatever a later publish chose.

**Who may plan is already per person and stays that way.** `/people` -> Weighing module ->
level "Set up" grants `weighing.plan` to a named person (`permissions/capability.go`,
`LevelConfigure`); the SOP document does not name people, because access is an HRMS fact and
a rulebook is not. The maintainer's "for the person who is creating the task, access should be
configurable" is answered by that existing tick, not by a new field here.

**What "compulsory or configurable" means, precisely.** The removal mode is the farm's rule:
`required` = every task, no one is asked; `optional` = the person planning the task decides
per task (on by default, forced off when the pinned evening is gone); `off` = never. The
questions are free-form authored -- "Every pen emptied?" in the live run was a sample the
author typed, not a built-in.

## The shape

- `form_dsl.weighing` (`schema_version: goatos.sop-weighing.v1`) = `planning {modes[],
  default_cap_per_day}`, `feed_water_removal {mode, cutoff_time, instruction,
  proofs[{key,title,hint,kind,required}], questions[]}`, `capture {individual {video_required},
  lump_sum {video_min, video_max}}`. A
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
when the removal applies); `instruction`, `proofs`, `questions`, `answers`, `proof_refs` on
`WeighingFastingShedCard`; `answers` and `proofs` (slot key -> proof ref) on
`SubmitWeighingFastingShedRequest`, the legacy `feed_proof_ref` / `water_proof_ref` pair still
accepted. New refusals: `weighing_mode_not_offered`, `feed_water_removal_not_offered`,
`weigh_date_in_past`, `feed_water_removal_locked` (switching the removal off after a pen was
submitted), `weighing_video_count`, `weighing_sop_version_unknown`, `fasting_answer_invalid`,
`fasting_proof_slot_invalid`.

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
- Live run (2026-09-15, OCI `goatos_wsopqa` cloned from a 000304 STG-like DB and migrated to
  000314; API :8107, admin-web :3397, Realme JJ6LVC8DCMFYMN4P): 000314 added the section IN
  PLACE to the published v1 (45 tasks read `sop_version NULL`). Chromium: the drawer summary,
  Change SOP → mode `optional`, instruction, a pick-one question, lump-sum max 3 → **Published
  v2**; a max of 9 is refused before publish. API under v2: declined today → 200 pinned v2 with
  no round and cap 100; requested without operator → 422 `fasting_operator_required`; yesterday
  → 422 `weigh_date_in_past`; requested with operator → 200 with a round; the task read carries
  v2's question and max 3. Phone as the CEO: the date step offered today with the SOP note, the
  configure step showed the toggle on, switching it off hid the operator picker and the review
  read "Not planned", Publish → task pinned v2, no round, request logged from `realme RMX3785`.
  Phone as Amit Kumar: tonight's card showed the authored instruction, slot wording and the
  question; answered Yes, recorded both clips, Submit → the evidence row holds
  `{"every_pen_emptied": "yes"}`, both clips, round stamped. Two defects the surface found and
  the same commits fixed: the planner handler dropped `sop`, and the editor's checkbox rows were
  stretched by the inspection page's input rule.

## Not here (phase 2)

Authored questions on the lump-sum pen submit and per animal; the `/config` registry editor for
question kinds; authored captures on the WEIGH itself (the per-animal video stays locked on and
the lump-sum window is a count, not a slot list); a media kind for questions (the slots ARE the
card's media).
