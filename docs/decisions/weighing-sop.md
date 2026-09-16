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
| **Weights pages** (maintainer request 2026-09-16) | the period the admin-web Weights and ADG Analytics pages OPEN on -- a fixed date or the last N days -- and the earliest day their calendars offer (earlier days greyed) | the two page contracts (`weights.window.*` copy, compiled by adminui from the PUBLISHED version; page settings, not pinned per task); the seed carries the values the pages used to hardcode (`2026-08-03`, `2026-08-01`) so deploy changes nothing |

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
(migration `000316`; the legacy `feed_proof_ref` / `water_proof_ref` pair mirrors the seeded
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
  lump_sum {video_min, video_max}}`, `weights_pages {default_from_mode fixed_date|rolling_days,
  default_from_date, default_from_days, earliest_date}` (absent on an older document = the
  seed). A
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
  Migration `000315` adds it IN PLACE to each tenant's currently published `weighing.session`
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
  000315; API :8107, admin-web :3397, Realme JJ6LVC8DCMFYMN4P): 000315 added the section IN
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

- Second live run (2026-09-15, same stack, API :8108, migrated to 000316). Web: v3 published
  with cutoff 21:30, feed VIDEO + water PHOTO + optional "Gate closed" either, four question
  kinds, lump max 2; all-optional slots / a bad time / min > max / no captures each refused
  before publish; v4 = off (drawer "Never"); `video_required: false` refused by name. API under
  the pins: catalog carries the effective evening; a create for tomorrow with the removal at
  13:40 (farm 12:00 gone, SOP 21:30 ahead) → 200 pinned v5 with a round, and under v6 (13:00)
  → `422 fasting_window_closed`; submit refusals by name -- compulsory photo missing, unknown
  slot, one capture in two slots, legacy pair mapped onto slots then judged, number out of
  range, conditional "Why not?" missing, choice outside options, "other" without its text,
  multi with an unknown option; removal switched off after a submitted pen → `409
  feed_water_removal_locked` while an ordinary edit stays on v2; three lump-sum videos under max
  2 → `422 weighing_video_count`; the v5 (21:30) card stayed hidden at 14:01 after v6 published
  13:00 (per-pin window). Phone as Amit (v7): the card rendered the instruction, three slots
  with kind-matched buttons, the conditional question appearing on "No" and vanishing on
  "Yes", "Other" with its free text, number with its range, multi choice; feed video, water
  PHOTO and the optional gate photo → `sop_proofs` holds all three (video, photo, photo, all
  completed), the legacy pair mirrored, the round stamped, one verifier item with three media;
  a verifier reject (relayed through the outbox) put the pen in rework with the reason on the
  card and every slot emptied; replaying the rejected refs → `409 weighing_rejected_proof_reuse`;
  fresh video + photo resubmitted with the optional slot omitted → a second verifier item with
  two media. Phone as the CEO (v7 optional, evening 14:07 gone): today and tomorrow marked
  "Too late … weighed without it", tomorrow's configure step forces the toggle off with the
  reason, Thursday's toggle is on by default with the operator picker, off hides it, the review
  reads "Not planned", Publish → pinned v7, no round, cap 100 from the document. Three defects
  the surface found and the same commits fixed: the removal route never bound the photo capture
  delegate (every "Take photo" read as cancelled), a photo slot's status said "Video sent", and
  the card list windowed on the published evening rather than each card's pin. A fourth, older
  than this branch: a person ticked Weighing "Set up" on `/people` was admitted by the route
  gate on the person set and refused by the service's role re-check -- weighing now judges from
  the person-resolved set (`domain.Actor.Holds`), the pccare shape; Amit's planner catalog went
  403 → 200 on the tick alone.

- Third pass (2026-09-15, evening). The SOP document validation matrix through the API -- 24
  refusals each naming its path (duplicate / malformed / ninth slot, unknown kind, empty title,
  duplicate question id, `only_if` on a later or missing question, a choice with no options,
  `allow_other` without an "other" option, min > max, cutoff `24:00` / prose, unknown mode, no
  planning mode, cap 0 / 10001, lump min 0 / max 6, the per-animal video off, a wrong schema
  version, the section missing) plus **an unknown key** (`cutoff_tme`), which the lenient
  parser used to drop silently and is now refused at save by path; a draft never moves the
  served rules. Task lifecycle: `required` refuses a missing operator and ignores a decline
  (round created); cap 0 takes the document's default and an explicit cap is kept; the same
  idempotency key replays the same task and a changed payload is `409 idempotency_conflict`;
  under `optional` an edit adds the removal before the evening (round created) and removes it
  again (round deleted) while an edit silent on the removal keeps what the task has; a mode the
  publish no longer offers is refused on a NEW task while a task pinned to the older version
  still edits under its own modes. The verifier's queue named and typed every proof BY
  POSITION from the registry ("Water removal video", `video/mp4` for a photo) -- fixed by
  `verification_items.media_meta` (000317), proven on the web drawer (three tabs titled by the
  SOP, the photo slots rendering an image). The campaign PUT has never carried a version fence
  (a stale `row_version` is accepted, last write wins) -- pre-existing, unchanged here.

- PR #274 review (2026-09-15, four findings, all fixed with red→green tests): (1) a corrected
  answer over the same captures collided with the refused submit's outbox row -- the phone's
  submit key now folds a digest of the normalized answers in (a card with no answers keeps the
  pre-SOP key shape); (2) a create retried after a later publish conflicted because the stored
  fingerprint was the RULE-STAMPED command -- the service fixes `RequestFingerprint` from the
  client's request before the rules touch it and the store keys replay on that
  (`TestCreateCampaignReplaysAfterALaterPublishMovedTheStamps`); (3) a retried verification
  enqueue lost an `either` slot's photo kind -- the kind each capture IS is read from the proof
  register on every path (`TestEitherSlotCapturedKindRidesEveryRead`); (4) the phone reopened an
  `either` photo as a video without local state -- the card serves `proof_kinds {slot: kind}`
  and the preview runs after the slot list exists.

- PR #274 review round 2 (three findings, fixed with red→green and mutation-tested where
  client-side): (1) an EDIT under `optional` opens on the task's own choice and an existing
  round is never "too late", so saving an unchanged task past the evening keeps its round
  (proven on the Realme: the 16/09 task edited after its evening, toggle on, Amit kept,
  round intact); (2) the service asks the store for an exact-fingerprint replay BEFORE the
  current publish's rules judge the retry (`ports.Repository.CampaignByIdempotencyKey`), so a
  mode withdrawn since cannot refuse the identical retry of a task that exists; (3) an
  authored slot's upload observer is reconnected when the card's slot list lands after
  process death.

- PR #274 review round 3 (two findings, fixed): (1) the card list read every historically pinned
  version on every refresh -- the versions read is now bounded (rounds of the last 90 days, the
  20 newest versions) and a pinned version's rules are cached per process
  (`TestCardRefreshReadsEachPinnedVersionOnceNotPerRefresh`: 50 versions, one read each, none on
  the second refresh); (2) the operator's answers now ride the verifier item as context rows in
  farm words (`Rules.RemovalAnswerRows`), on the fresh enqueue and on a replay.
- Final E2E on the landing code (2026-09-16 02:00-03:00 IST, API :8108 on the throwaway clone,
  admin-web :3397, Realme as CEO and as Amit): the Weights-pages window authored on the SOP
  (fixed 10/08 + floor 05/08 → both pages open 10/08 with 1-4 Aug greyed; rolling 30 → both
  open 18/08; refusals for from < floor and 0 days; seed restored); the 25-case document matrix
  and the lifecycle matrix on the final binary; the wizard creating tomorrow's task with the
  removal under a still-open evening and pinning it; the removal card with four question kinds
  (No → "Why not?", 7 buckets, Broken trough), feed VIDEO + water PHOTO + gate as VIDEO →
  `sop_proofs` three slots, verifier item with three media metas and four context rows,
  rendered on the web drawer with the SOP titles, the answers and an image player; verifier
  reject → rework on the phone (reason, slots emptied, answers kept) → fresh video + photo
  resubmit → a second item; an identical create retried after a publish withdrew its mode
  replays the task while a new request is refused; Amit's per-person tick keeps Tasks + My
  work + Alerts and the planner catalog.

## PR 274 regression follow-up (2026-09-16)

- A fixed or rolling Weights start newer than the latest observation now ends today,
  preserving the requested empty period instead of sending a reversed range to the API.
- Authored capture keys `feed` and `water` use a disjoint saved-state namespace; legacy
  `feed_video` / `water_video` drafts keep their existing keys. A ViewModel test captures
  all four, restores the draft, and verifies four distinct submitted proofs.
- Supersedes round 3's newest-20 bound: load every pin for tomorrow's weigh date in IST,
  the only date whose evening cutoff affects today's card membership. Cold rules are
  read in batches of 256; repeat reads use the bounded cache without truncating results.
  Guards cover 51 tonight pins, an old active version, IST midnight, and 600 cached pins.
- Real-route validation exposed inherited weekly-grid SQL syntax errors. All three CASE
  branches are corrected; PostgreSQL coverage exercises grids enabled and disabled.
  The broader demographics suite still has pre-existing invalid display-ID/proof fixtures
  and a plain numbered-shed composition failure; these are not a green merge receipt.

## Not here (phase 2)

Authored questions on the lump-sum pen submit and per animal; the `/config` registry editor for
question kinds; authored captures on the WEIGH itself (the per-animal video stays locked on and
the lump-sum window is a count, not a slot list); a media kind for questions (the slots ARE the
card's media).

## PR 274 review follow-up: request reads and conditional ancestry

Campaign list decoration resolves the live farm cutoff once per request, shared across
all fallback versions; an explicit pinned evening and removal-off rules do not read it.
The immutable pinned-rule cache never stores the resolved farm evening. Regression tests
cover 20/100 rows, mixed pins, and a farm configuration change between requests.

Conditional questions require their entire earlier-question ancestry to apply. Hidden
draft answers may remain locally so toggling back restores input, but they cannot activate
a descendant or enter submitted evidence. Backend validation/normalization and Android
visibility/submit gating/serialization use the same ordered applicability rule. Tests
exercise A -> B -> C, changing A after answering B, and reopening the branch.


### Replay authorization and independent evening configuration

Create retries may bypass the latest published rules, but never the caller's current
park grants. Authorize the park on the saved campaign returned by replay, because
both grants and the campaign's park may have changed since the original request.

Removal card lists resolve the farm cutoff only for tonight's candidate versions
that lack their own cutoff (including legacy or unknown pins). A SOP-owned evening
works without farm configuration. Earlier removal evenings are already open and
later evenings remain closed; neither needs a substitute literal cutoff. An
unresolved tonight pin stays hidden rather than opening at midnight.

Regression coverage: `pr274_review_repro_test.go` and
`TestSOPCardVisibilityWithoutFarmDefault` in `sop_pin_integration_test.go`.


### Further independent review regressions

- Optional-removal disable locks the fasting task before reading whether any pen
  has submitted. The evidence check uses a fresh statement snapshot after the
  lock, so a concurrent partial submission cannot be cascade-deleted.
- Nonblank number-question bounds must be finite numbers before the editor allows
  save or publish; malformed values must not silently become unbounded questions.

- A restored rework card hydrates its authored slots before clearing rejected
  captures. Reset covers both rendered slots and incoming authored slots, so
  process restoration cannot reconnect uploads from the rejected submission.

### Deployment replay and reporting windows

- Adding SOP command fields must preserve the fingerprint of legacy requests.
  Absent optional fields and derived rule metadata do not change that identity.
  The seeded proof-slot map is equivalent to the same legacy feed/water pair;
  extra slots, changed refs, answers, and explicit removal choices still change
  the fingerprint. Pre-SOP campaign fingerprints may contain normalized partition
  labels and must be matched before applying the current planning rules.
- CSV export accepts the same explicit date window as the Weights report,
  including fixed-date and multi-year SOP defaults. It streams through the existing
  repository timeout; inverted dates and unauthorized scope remain refused.
- Regression coverage: `review274_legacy_fingerprint_test.go` covers pre-deploy
  create/update/removal identities, database replay, and changed-request refusal;
  `review274_export_window_test.go` covers the served report range and timeout
  propagation.

## Reporting calendar configuration

Reporting dates are database-only settings in `weighing_calendar_config`, independent of SOP publication and pinned task execution rules. Claude/Codex/operators should use the exact SQL in [Weighing calendar settings](../runbooks/weighing-calendar-settings.md). Do not add UI controls or publish a SOP to change reporting dates. Historical SOP `weights_pages` metadata is not the reporting calendar authority.
