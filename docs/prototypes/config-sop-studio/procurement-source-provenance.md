# Procurement source provenance

## Committed baseline (not current published export)

`procurement-source-seed.json` copied read-only with `git show 606578151:backend/internal/animalpurchase/domain/inspectionseed/animal_purchase.json` from the source repository. Schema `goatos.sop-inspection.v1`. 38 animal questions in five pages (identity13, face6, body8, udder8, decision3), five load questions. Source owner Animal Purchase module.

Root independently reports live dashboard published v5 with38 animal questions and7 load questions. Consequently this seed MUST NOT be labeled exact current production. Root will supply live UI export/text for reconciliation. Runtime stable ids/options of new load questions cannot be inferred from titles; mark unknown unless read from authored controls or API DSL.

## Last-month contract review

- e637634c9: procurement inspection authoring added; pages/questions are module-specific `form_dsl.inspection`, not generic `form_dsl.fields` alone.
- 606578151: load form also authored; Change SOP starts from `published_version ?? latest_version`, preserving in-force definition. Server enforces capture kind.
- f2a9a17ef: authored media slots storable; avoids storage errors and generic drawer duplicate fields.
- Current working checkout c09c95643 is older than these source commits, so checkout absence is not evidence of missing implementation.
- Newer list model includes batched `latest_versions`; avoid reintroducing one detail call per row. Read detail/published version for precise production provenance.

## Import mapping constraints

Preserve page key/title/hint, question id/kind/title/hint/required, exact option value AND label, allow_other, media slot/max_files/accepts, min/max/unit and only_if.question_id/value. Closed values are lower-case source keys (goat/sheep, female/male, yes/no), not display labels. Seven conditional questions in seed reference sex or lactating; preserve those relations. Preserve load_form as once per load separate from per-animal inspection.

Nine animal typed-field keys have kind/closed-choice locks: species,goat_id,sex,weight_kg,height_cm,rectal_temp_c,field_verdict,breed,notes. Titles/hints/compulsory/page/position remain editable; lock does not mean whole question immutable. Locked load keys: load_ref,vendor,farm,expected_count,notes; required load keys load_ref,vendor,farm.

No live mutations performed. Root owns browser verification and source capture. No backend performance or deployed-route availability certification is implied by this read-only source mapping.

## Live UI reconciliation supplied by root

`procurement-production-source.json` and `.js` retain baseline source schema and add live load questions5 and6, before optional Note. Seven load questions: Load number, Vendor, Farm, Roughly how many animals, What feed was given to animals at source., How much quantity was given per day per animal?, Note. Live version5 observed2026-09-15. Both added questions required; source quantity question is Pick one with Yes/No. This oddness is retained unchanged.

Exact production keys for the two additions were not exposed by rendered UI: local placeholders `ui_source_feed` and `ui_source_daily_quantity` explicitly carry `source_id_verified:false`. Original38 animal controls still derive from the exact committed seed pending final expanded live comparison. This is a reconciled view fixture, not a byte-exact published DSL export.

## Final UI reconciliation

Root read expanded live Identity, Face, Body, Udder and Verdict controls today. Labels/options/bounds match seed; scrotum5..60cm confirmed; Udder multi `allow_other:true` confirmed. Proof `both` means either photo or video accepted, not compulsory capture of both. UI editor row ids such as ip-mu2ygvlo-88 are ephemeral and must not substitute persisted source page/question keys. Keys absent from rendered UI remain seed-backed only. JSON/JS provenance updated accordingly.
