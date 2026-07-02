# Preventive Care (PC) Vaccination — Source-Derived Dev Baseline

Date: 2026-06-26

## Purpose

This file records the baseline we are choosing from existing Mesha artifacts so
local/dev work does not stay blocked on a vague "business approval" placeholder.
It is source-derived config for Goat OS local/dev, E2E, and demos. If production
medical operations later changes a value, create a new source-backed protocol
version through the normal Config source/review gate.

The baseline is deliberately conservative: use exact values where the wiki/PRD
gives them, use source labels where the SOP artifact gives labels, and do not
turn procurement-history clues into schedule math.

## Stage-band baseline

| stage_code | name | min_age_days | max_age_days | decision | source |
| --- | --- | ---: | ---: | --- | --- |
| K0 | Newborn | 0 | 1 | Use for local/dev. | `context/source-findings/goats-and-parks-source-findings.md`; `context/product/glossary.md:205` |
| K1 | Milk Training | 2 | 7 | Use for local/dev. | `context/source-findings/goats-and-parks-source-findings.md`; `context/product/glossary.md:210` |
| K2 | Milk Drinking | 8 | 42 | Use **42**, not legacy mock 45, for local/dev. | `context/source-findings/goats-and-parks-source-findings.md`; `context/product/glossary.md:215`; `backend/migrations/postgres/000001_phase_1_identity_foundation.sql:922` |
| K3 | Weaning | 43 | NULL | Use as the next age band when a seed needs K3. | `context/source-findings/goats-and-parks-source-findings.md`; `context/product/glossary.md:218` |

Legacy dashboard `SHIFT_THRESH` used K2 = 45, but Goat OS treats that as mock
legacy drift. The source-backed local/dev decision is 42 days / six weeks. If a
real Preventive Care (PC) override arrives later, it should update `animal_stage_lookup` as data,
not reintroduce a frontend hardcode.

ICU/quarantine/holding are operational states/flags, not replacement age bands.
Do not mix them into `animal_stage_lookup` schedule eligibility unless a later
source explicitly defines them as stage rows.

## Vaccine and SOP baseline

| item | local/dev decision | source |
| --- | --- | --- |
| Schedule-bearing protocol | Use **Enterotoxaemia / ET** as the source-derived dev protocol row: goat, all sexes/breeds, stage K1, primary dose, `dose_ml=0.5`, `trigger_type=birth_age`, `offset_days=21`, `due_window_days=7`, booster clue `+14d`. | `docs/preventive-care-vaccination/PRD.md:60` |
| SOP execution labels | SOP picker/form labels may include `PPR`, `ET`, `FMD`, `HS`, `BQ`; required execution fields are scheduled date, operator, goat scan, vaccine name, medicine batch, dose ml, administered date, proof photo, adverse reaction, verifier, notes. | `source-material/sop-playground-local/playground.html:1057-1083` |
| Proof/verification shape | Keep proof + medicine batch + park-head verification gates; missing schedule escalates, adverse reaction requires notes/follow-up, empty medicine batch blocks submission. | `source-material/sop-playground-local/playground.html:1067-1082` |
| PPR/FMD | Label-only closed in the 2026-06-26 roster expansion pass. PPR/FMD remain SOP/vocabulary labels; the source audit found sheep/procurement-history notes but no goat timing/dose/booster policy or approved Preventive Care (PC) / vet metadata, so no protocol rows or obligations were added. | `wiki/graphify-out/converted/Procurement DB [Goats]_dda03a25.md:78,:86`; SOP playground labels above; `context/execution/vaccination-roster-expansion-followup.md` |
| HS/BQ | Label-only closed in the 2026-06-26 roster expansion pass. HS/BQ remain SOP/vocabulary labels; the source audit found no timing/dose/booster policy or approved Preventive Care (PC) / vet metadata, so no protocol rows or obligations were added. | SOP playground labels above; `context/execution/vaccination-roster-expansion-followup.md` |
| Sheep Pox | Exclude from goat dev roster for now; the evidence found is sheep/procurement-history context. | `wiki/graphify-out/converted/Procurement DB [Goats]_dda03a25.md:78,:86,:91` |

## Seed/runtime decision

The repeatable local proof pack should use this as its source-derived baseline:

- protocol: `Enterotoxaemia K1 Primary`
- dose code: `ET-PRIMARY-1`
- source metadata: `source_system=phc`, `source_ref=docs/preventive-care-vaccination/PRD.md:60; context/source-findings/preventive-care-vaccination-roster-stage-proposal.md`, `review_status=approved`, `approved_by=source-derived-dev-baseline`
- stage default: K1 for the proof goat
- due date: day 21 from DOB, with the proof script creating a day-21 goat so the
  chain is due immediately

This closes the local/dev roster and K1/K2/stage pending item. Remaining
production work is normal source-backed versioning if real Preventive Care (PC) / vet data later
changes these values; it is not a local code or E2E blocker. The exact follow-up
scope and edge-case checklist for PPR/FMD/HS/BQ expansion lives in
`context/execution/vaccination-roster-expansion-followup.md`.

## 2026-06-26 roster expansion follow-up result

The optional PPR/FMD/HS/BQ expansion pass queried the Mesha docs/visual graphs,
goatos-docs graph, SOP playground, converted wiki docs, and legacy/procurement
source snippets. None of the four labels had goat-applicable schedule math plus
publishable source metadata. Closed states:

| Vaccine | Closed state |
| --- | --- |
| PPR | `label-only closed` |
| FMD | `label-only closed` |
| HS | `label-only closed` |
| BQ | `label-only closed` |

No draft/reviewed candidates were created, because the audit did not find
timing/dose evidence without approval metadata. No additional vaccination
protocol rows were seeded/imported. Only the ET/K1/day-21 source-derived dev
baseline is schedule-backed and allowed to generate obligations.
