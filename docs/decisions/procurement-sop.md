# Procurement SOP: the animal purchase inspection is authored on the web (maintainer decision 2026-09-14)

Status: accepted and implemented · Owner: animalpurchase + sop + adminui + admin-web + Android
Machine enforcement: `make procurement-sop-guard` (`tools/agent-hooks/check-procurement-sop-guard.mjs`)
plus the golden test `backend/internal/animalpurchase/domain/inspection_test.go`.

## Decision

The per-animal inspection the procurement desk records inside a purchase load -- **which questions,
on which page, in which order, which take a photo / a video / either (and how many), which are
compulsory, and which are asked only after a given answer** -- is authored on the web under
**Procurement → Procurement SOP** (`/procurement/sops`) and served to the phone from the PUBLISHED
`procurement.animal_purchase` SOP version. Nothing about the inspection is a code constant any more;
changing a question is a publish on that page, never an app or backend release.

This supersedes rule 2 of `docs/decisions/animal-purchases.md` ("the questionnaire is a versioned
catalog the backend serves ... changing a question is a backend version bump"): the backend still
serves and validates it, but the catalog is the SOP document, not Go.

## The shape

- `form_dsl.inspection` (`schema_version: goatos.sop-inspection.v1`) = `pages[]` of `questions[]`.
  A page is one phone page; a titled page compiles to a section row (`sec_<key>`) and the first page
  may be untitled (the SOP form's opening questions). A question carries the same fields the phone
  already rendered: `id`, `kind` (`choice` / `multi` / `text` / `number` / `media`), `title`, `hint`,
  `required`, `options` (+ `allow_other`), `slot` / `max_files` / `accepts` (`photo`, `video`, or
  both), `min` / `max` / `unit`, `only_if {question_id, value}`.
- `animalpurchase/domain.CompileInspection` flattens pages into the catalog the phone and the
  validator consume -- the SAME wire shape as before, so the phone's paging, widgets and validation
  did not change. `ValidateInspection` names every problem by path; `sop/app` runs it at version
  create through `WithFormDSLContract`, so a document the phone could not run is never saved.
- **Versioning.** `/options` serves the published version and its number; the phone sends
  `questionnaire_version` with the answers; the write is validated against THAT version (published
  or since retired) and the row is stamped with it; the CEO's review labels each animal's answers by
  the version it was answered on (`Service.CatalogFor`, one read per distinct version per page).
  A version the farm never published is refused (`409 questionnaire_unknown`). Publishing changes
  the next animal recorded; a form already open on a phone submits on the version it rendered.
- **Locked questions.** `species`, `goat_id`, `sex`, `weight_kg`, `height_cm`, `rectal_temp_c`,
  `field_verdict`, `breed`, `notes` feed the typed columns, list titles and review chips: their id
  and kind (and, for the closed vocabularies, their choices) are fixed and they must stay in the
  document; title, hint, compulsory flag, page and position are the author's. Everything else --
  including every media question -- may be added, moved, re-worded, made optional or removed.
- **Day one is the current flow.** `inspectionseed/animal_purchase.json` is the old Go catalog split
  into its five pages; `TestSeededInspectionCompilesToTheLegacyQuestionnaire` pins that it compiles
  to EXACTLY the old catalog, and migration `000304` publishes it as **v1** (the document embedded
  verbatim, pinned by `TestMigrationEmbedsTheSeededInspection`). Animals already recorded carry
  `questionnaire_version = 1` and keep reading it. A tenant with no authored version runs the seed.
- **Web.** `/procurement/sops` is the sixth module-surface SOP route (recorded in
  `check-ia-guard.mjs`). The drawer lists every page and question with kind, capture, compulsory
  and condition; **Change SOP** opens the inspection editor (`features/sops/inspection-editor.tsx`,
  model `inspection-model.ts` -- round-trip of the seed is byte-faithful, pinned by
  `inspection-model.test.mjs`); Save as draft / Publish SOP go through `saveInspectionVersion` /
  `publishInspectionVersion`, which keep the load form and proof policy of the published version and
  replace only `inspection`.
- **Phone.** Renders the served catalog as before; the only change is sending
  `questionnaire_version` with the answers (`AnimalPurchaseViewModelsTest` pins it).

## Proof (2026-09-14, OCI `goatos_procsop` cloned at main's 000296 and migrated to 000304)

- `/options` serves v1 with the 42 legacy questions and the same four page headings.
- Web: opened Procurement SOP → Animal Purchase Inspection → Change SOP; made "Photo of teeth"
  photo-only and optional, moved "Breed" to page 1, added page 6 "Extra checks" with a compulsory
  "Photo of the hooves" (≤ 2) → Published v3. `/options` then served v3 (44 items) with exactly those
  changes.
- Write path: the same answers submitted with `questionnaire_version` 3 were refused for the missing
  hoof photo; with 1 (retired) they were judged by v1's rules (teeth photo still compulsory); with 99
  they were refused as unknown.

## The load form is authored too (same day)

`form_dsl.inspection.load_form` is what the desk answers when it opens a load: `load_ref`, `vendor`
(the register picker, kind `vendor`), `farm` stay locked and compulsory; `expected_count` and `notes`
may be re-worded or made compulsory; pick-one / pick-many / number / text questions (with "other"
free text and "ask only when") may be added. Photos and videos are recorded per ANIMAL, never on a
load -- the validator refuses a media question there. Answers land in
`animal_purchase_loads.sop_answers` + `questionnaire_version`; the identity answers fill the typed
columns (an older APK sending typed fields only still works); the extra answers render as
`answer_rows` on the load payload and as "About this load" on the CEO's review page. The phone's
Add-load screen renders the served `load_form` (locked questions keep their widgets, authored ones
render by kind) and validates as the server does.

Two things the edge run found and fixed the same day: 000303's closed CHECK on the media slot
refused every authored media question (000304 widens it to the slot-key shape; the SOP version
validates the slot), and the editor was building on `latest_version` (a stray draft or a retired
version above the published one) -- `SOPResponse.published_version` now exists and both the editor
and the saves build on it. Also: a renamed choice's wire value follows its label, a capture whose
kind the slot does not accept is refused server-side, and the animalpurchase transport logs the
cause of a 500.

## Edge run (2026-09-14, v6 authored in Chrome, phone as the procurement director)

Load form: compulsory count refused blank; Truck / Tractor / Other with free text; documents
pick-many; distance 0-2000 km refused at 9999 on the phone and at 5000 on the server; driver phone
only when Truck (given with Tractor it is ignored); an old-APK create (v1, typed fields only)
accepted; unknown version refused. Inspection page 6: photo-only hoof (compulsory), yes/no with a
conditional text, 4-choice radio, optional pick-many, video-only walking (compulsory), photo-only
ear tag (optional), either-with-cap-3 evidence (4 refused), horn 0-60 cm refused at 75; a photo in
the video-only slot refused server-side. The animal landed on v6 with every answer and slot, the
phone showed the authored slots by title with the captures inline, the CEO accepted it on the web
and the phone read "1 accepted".

## Not here

Editing the load's identity questions (vendor, farm, load number) -- they stay canonical; the `/config` registry
editor for question kinds; promotion of accepted animals into the herd (unchanged scope of
`animal-purchases.md`).
