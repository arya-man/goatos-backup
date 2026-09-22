# PC Care SOP: the preventive-care cards are authored on the web (maintainer decision 2026-09-22)

Status: accepted and implemented · Owner: pccare + pccaresop + sop + adminui + admin-web + Android
Machine enforcement: `make pc-care-sop-guard` (`tools/agent-hooks/check-pc-care-sop-guard.mjs`,
seven rules, each with an adversarial self-test fixture) plus the golden tests
`backend/internal/pccare/domain/sop_test.go`
(`TestSeededPCCareSOPIsThePreSOPBehaviour`, `TestMigrationEmbedsTheSeededPCCareSOP`).

## What was asked

"We have made everything SOP-driven; the only thing still missing is preventive care. For web
there is no preventive care SOP at all. Create one — deworming, trimming, everything — just like
the others, list and flowchart. Feed and water removal is not correctly defined for preventive
care; I think that should be optional while creating."

Preventive Care was the last module still running on a slot table typed into Go:
`domain.SlotsForCategory` returned a fixed list per category, the feed & water removal was a
`category == deworming` branch on the create, and nothing about either could be changed without a
deployment. That is what the herd-operations lock (`AGENTS.md` → "Herd Operations Are SOP-DRIVEN,
and Every New Operational Feature Must Be") forbids for every new feature, and PC Care predates it.

## Decision

The five hands-on-the-animal PC Care categories — **deworming, anti protozoan, ticks removal, hoof
trimming, hair trimming** — run under the **`pc_care` section of the PUBLISHED `pc_care.tasks` SOP
version**, authored on **Preventive Care › Preventive Care SOP** (`/pc-care/sops`):

| Rule | What the author decides | Who reads it |
| --- | --- | --- |
| **Feed & water removal** | `required` (every task of a listed category carries it, the planner is not asked) / **`optional` (the planner decides per task — the seed, and what the module always did)** / `off` (never offered); **which categories** it applies to (the seed: deworming alone); the **evening cutoff** (blank = the farm-wide `feed_water_removal_config` evening weighing shares); the instruction on the removal card; **the captures the card asks for per pen** (1..8 slots, each a key, title, hint, kind `video` / `photo` / `either` and a required flag, at least one compulsory); **questions** the evening crew answers per pen | the plan wizard (removal step shown / toggled / hidden, and on which categories); the create (`422 feed_water_removal_not_offered` under `off`, `422 feed_removal_not_applicable` on a category the document does not list, `422 fasting_window_closed` past the effective evening); the removal card (copy, slots, questions); the per-pen submit |
| **Per category** | the instruction; the **per-animal capture slots** (1..8, video / photo / either, compulsory or not, at least one compulsory, with the recorder-chrome `min_seconds` hint the trimming "while" clip carries); the **questions** answered once per task at submit | the phone's animal row (slots and questions from the task's pinned rules); the per-animal proof write (`422 invalid_slot` naming the slot, `422 invalid_proof` for a capture of the wrong kind); the submit (`422 proof_incomplete` until every animal carries every compulsory slot, `422 pc_care_answer_invalid` naming the question); the verifier item (every capture titled by its slot, every answer a context row) |

## What is NOT authorable here, on purpose

The module's locks stay locks, and the document says so on screen:

- **The CAPTURE MODE of a category.** Scan-and-record for the 2-second jobs, roster-pick for the
  trimming jobs (`domain.CaptureModeForCategory`). How the operator *reaches* an animal is what the
  work is, not a preference.
- **Free-flow scan.** A tag is stored verbatim and never resolved against the herd.
- **The ONE business rule**: no duplicate tag in the same task.
- **The whole-task submit grain**, one verifier item per task, and the assignee gate
  (`pc_care.execute` alone never authorizes a write).
- **The trimming planner carve-out.** Who may plan hoof and hair trimming is the `pc_trimming`
  tick on `/people` (maintainer decision 2026-09-04) — an HRMS fact, never a rulebook's, the same
  answer the weighing SOP gives to "who may plan".
- **The `inventory_vaccine` fridge check.** Kernel-owned, director-approved
  (`docs/decisions/pc-care-vaccine-stock-director-gate.md`); it keeps its fixed photo + video pair
  and is deliberately absent from the document.

## The shape

- `form_dsl.pc_care` (`schema_version: goatos.sop-pc-care.v1`) =
  `feed_water_removal {mode, applies_to[], cutoff_time, instruction, proofs[{key,title,hint,kind,required}], questions[]}`
  and `categories {<category>: {instruction, proofs[{…, min_seconds}], questions[]}}`.
  The slot, question, answer and evidence types are the SHARED `backend/internal/sop/authored`
  package — the same building blocks the feed cards, the herd-operations capture card and the
  weighing removal card use, so "add a photo beside the video" means the same thing on every card
  the farm authors and the phone renders them all with the same widgets. PC Care adds exactly one
  field of its own, `min_seconds`, because the trimming "while" clip has always carried that hint.
- `pccare/domain.ParsePCCareSOP` + `ValidatePCCareSOP` name every problem by path;
  `pccaresop/app.PCCareSOPContract` runs them at version create through
  `sop/app.WithFormDSLContract`, so a card the operators could not run is never saved. A key the
  schema does not know is refused at SAVE (`UnknownPCCareSOPKeys`): the lenient parser drops it,
  and a misspelt `proofs` would otherwise publish a card with no captures without a word.
- **Versioning and the pin.** `PublishedRules` is what a task planned now is stamped with
  (`pc_care_tasks.sop_version`, and `pc_care_rounds.sop_version` — every pen task of a round and
  the round's removal card carry the SAME number). The task runs on that version to the end: the
  capture writes, the submit and the removal card all read `RulesVersion(pinned)`, never the latest
  publish (the pin-at-start rule of the herd-operations, procurement, weighing and feed SOPs).
  Version 0 / NULL is the seeded document. A task pinned to a version the farm never published is
  refused by name on the write paths (`409 pc_care_sop_version_unknown`) and rendered with the
  seeded copy on the read paths, so tonight's work is still doable.
- **PC Care never names the SOP tables.** It holds `ports.SOPRulesSource` and an
  `app.SOPPinReader`; the only file naming `sop_versions` on its behalf is
  `backend/internal/pccaresop/adapters/postgres/rules_source.go` — the `weighingsop` / `feedsop`
  shape. Unwired, the service runs the seeded rules, so every existing fake and every process
  without the adapter behaves exactly as before.
- **Readiness is a snapshot, not a rule read.** `pc_care_tasks.required_slot_keys` stores the
  pinned card's compulsory keys at create, so the submit's readiness check, the Work Board's
  per-animal count and the subtask card are ONE set-based `sop_proofs ?& required_slot_keys`
  predicate — never a rules read per task, and never the old count over four fixed columns.

## Day one is the current behaviour

`pccare/domain/sopseed/pc_care.json` is the pre-SOP behaviour byte for byte, pinned by
`TestSeededPCCareSOPIsThePreSOPBehaviour` against the legacy slot table itself: the same keys, the
same titles, the same hints, the same order, every capture a compulsory video, the 10-second hint
on both trimming "while" clips, no questions anywhere, and the removal `optional` on **deworming
alone** with its two clips. Migration `000385` seeds the definition and v1 with that document
embedded verbatim (`TestMigrationEmbedsTheSeededPCCareSOP`). Tasks planned before the migration
carry `sop_version NULL` = the seed = what they already do.

**Why `optional` and not `required`:** this is the maintainer's "feed and water removal is not
correctly defined for preventive care; that should be optional while creating". Before this
change the wizard offered the toggle on deworming and the planner chose, so `optional` IS the
behaviour that shipped — the document now says so out loud, and a farm that wants it on every
deworming publishes `required` instead. `docs/decisions/feed-water-removal-precondition.md` rule 1
("for deworming it applies only when feed removal is required") is unchanged; what moved is that
"is it required" is now a published rule rather than a per-create boolean on a hardcoded category.

## Storage

Migration `000385_pc_care_sop.sql`, every change rollout-safe (nullable or defaulted):

- `pc_care_tasks.sop_version` / `pc_care_rounds.sop_version` — the pin.
- `pc_care_tasks.required_slot_keys` — the compulsory keys of the pinned card, backfilled from the
  legacy category table for every existing task so their readiness predicate is exactly what it was.
- `pc_care_tasks.sop_answers` — the answers given at submit.
- `pc_care_task_animals.sop_proofs` `{slot key: proof ref}` + `sop_proof_meta`
  `{slot key: {captured_by, captured_at, kind}}` — the source of truth, backfilled from the four
  legacy columns. **`video_/before_/during_/after_proof_ref` stay as the MIRROR** of the seeded keys
  so every pre-existing reader still finds a ref; an authored key outside that set lives in the map
  alone.
- `pc_care_removal_pen_proofs.sop_proofs` — same shape per pen; `feed_proof_ref` / `water_proof_ref`
  mirror the seeded `feed_video` / `water_video`.
- `pc_care_task_proofs`'s slot CHECK becomes the authored-id pattern instead of a fixed list.

## The verifier sees the card without a deployment

Each verification item is enqueued with its captures in CARD ORDER, `media_meta {label, kind}` from
the pinned card, and the operators' answers as context rows under an **Answers** group. The kind an
`either` slot was captured as is read from the proof REGISTER, never guessed — the feed bridges'
rule, for the same reason. A removal round still fans out into ONE item per pen (the review follows
the evidence), each carrying that pen's authored captures.

## Older apps are never forced to update

A phone that predates this change sends the fixed slot keys and no answers. Those keys ARE the
seeded keys, so they land in the map and are judged exactly as before; a submit with no body is a
submit with no answers, which is what the seeded card asks for. The create's
`feed_removal_required` becomes a *tri-state*: ABSENT means "not said", which under `optional` is a
decline and under `required` is irrelevant — so an older APK that always sends the flag, and one
that never sends it, both keep behaving as they did against the seeded document.

## Web

`/pc-care/sops` is the Preventive Care group's fourth leaf, classified `module-surface` (the
`/feed/sops` shape, allowlisted in `check-ia-guard.mjs`). The drawer summarises the published card;
**Change SOP** opens the rules editor with the **List | Flow** toggle every SOP editor has
(`?view=flow` read on the server so SSR and the client agree). Copy is the page contract's
`pcsop.*` namespace, which starts from `weighingSOPEditorCopy()` so the shared slot and question
rows read identically on every SOP page.

One trap, paid for on 2026-09-22 and now guarded. `copy()` throws on the PAGE contract, and each
SOP page merges its OWN editor map -- so a key declared somewhere in `service.go` is not a key this
page serves. The editor renders the SHARED capture card (`features/sops/feed-editor.tsx`
`SlotCard`), which names `fsop.proof.title` / `.hint` / `.remove` by their original Feed prefix.
They were declared, in `feedSOPEditorCopy()`, which this page does not merge; the whole editor
rendered the error boundary, and the repo-wide `copy-keys` test stayed green the entire time
because it only asks whether a key exists anywhere. The three keys are now in this page's map (the
prefix is historical, the copy is this page's), the Flow view's node chrome moved onto `pcsop.*`
rather than borrowing Feed's words, and rule 8 of the guard resolves what the contract actually
merges and checks it against every literal key the pc-care screens read -- following each shared
card they import into its own function body.

## Phone

The planner catalog carries the PUBLISHED rules and the task read carries the PINNED ones, both
Room-cached. The plan wizard reads the removal mode and its `applies_to` list BEFORE a date is
picked: it hides the removal step under `off`, shows the toggle under `optional`, forces it on under
`required`, and offers it only on the categories the document lists. The task screen renders the
task's own slots and questions; an `either` slot offers both verbs, and the per-animal route binds
the photo capture delegate beside the video one (the defect `WeighingRouteIdentityTest` exists for).
Answers ride the submit through the outbox and survive process death.

## Pinned by

- `pccare/domain`: `TestSeededPCCareSOPIsThePreSOPBehaviour`, `TestMigrationEmbedsTheSeededPCCareSOP`,
  `TestRemovalDecisionFollowsTheMode` (the full required/optional/off × listed/unlisted matrix),
  `TestValidatePCCareSOPNamesEveryProblemByPath`, `TestUnknownPCCareSOPKeysAreNamedByPath`,
  `TestCaptureSlotRoundTripsMinSeconds`, `TestServedRulesFillEveryList`.
- `pccare/adapters/boardsource`:
  `TestPCCareBoardSlotReadinessMultipleDimensionsParkScopePaginationStatusBucketsFailClosed` — the
  board counts an animal done only when it carries every compulsory key of ITS task's pinned card,
  and a task stating NO requirement counts nobody, because `sop_proofs ?& '{}'` is vacuously true
  and would otherwise report a pen nobody worked as finished.
- `make pc-care-sop-guard` — eight rules with adversarial fixtures: the seed embedded verbatim, no
  runtime read of the legacy slot table for an authored category, no SOP table inside
  `backend/internal/pccare`, no category literal deciding the removal, no readiness check on the
  legacy proof columns, no category-built slot list on the phone, `/pc-care/sops` registered end
  to end in the adminui contract, and every copy key the page's screens read served by the page's
  own merged map.

## Not here (phase 2)

Per-category capture MODE (scan vs roster) stays fixed by the work. A media kind for questions (the
slots ARE the card's media). A slot-scoped verifier reject (today a reject reworks the whole task,
the fasting shape).
