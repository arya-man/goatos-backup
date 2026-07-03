# Preventive Care (PC) Vaccination — Source-Derived Dev Baseline

Date: 2026-06-26

> **Historical baseline note, 2026-07-02:** this file records the older local/dev
> ET-only proof baseline. Current approved vaccine timing, dose, vial,
> repeat, procurement, pregnancy, and gap rules now live in
> `docs/preventive-care-vaccination/APPROVED-SCHEDULE-MATRIX.md`. Do not use this file to
> block the approved schedule rows for ET+TT, PPR, Goat Pox, Sheep Pox, FMD, HS,
> or Blue Tongue. BQ remains label-only until a later reviewed source adds
> schedule-bearing values.

## Purpose

This file records the baseline we are choosing from existing Mesha artifacts so
local/dev work does not stay blocked on a vague "business approval" placeholder.
It is source-derived config for Goat OS local/dev, E2E, and demos. If production
medical operations later changes a value, create a new governed protocol version
through the normal CEO/COO Config authoring flow.

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
legacy drift. The source-derived local/dev decision is 42 days / six weeks. If a
real Preventive Care (PC) override arrives later, it should update `animal_stage_lookup` as data,
not reintroduce a frontend hardcode.

ICU/quarantine/holding are operational states/flags, not replacement age bands.
Do not mix them into `animal_stage_lookup` schedule eligibility unless a later
source explicitly defines them as stage rows.

## Vaccine and SOP baseline (historical 2026-06-26)

| item | local/dev decision | source |
| --- | --- | --- |
| Schedule-bearing protocol | Use the Vaccination Rules vaccination matrix as the dev/config preset: ET+TT (`2 ml`, `28d` + `49d`, revaccination 6mo), PPR (`1 ml`, `112d`, revaccination 3y), FMD (`1 ml`, `84d`, revaccination 9mo), HS (`2 ml`, `84d`, revaccination 1y), and Goat Pox (`1 ml`, source 112d but V1 shifted to 140d for live-live spacing with PPR, revaccination 1y). | `docs/preventive-care-vaccination/vaccination-rules.md`; `Vaccination Rules.docx` (formerly `Vaccination Rules.docx`) graph extraction |
| SOP execution labels | SOP picker/form labels may include `PPR`, `ET`, `FMD`, `HS`, `BQ`; required execution fields are scheduled date, operator, goat scan, vaccine name, medicine batch, dose ml, administered date, proof photo, adverse reaction, verifier, notes. | `source-material/sop-playground-local/playground.html:1057-1083` |
| Proof/verification shape | Keep proof + medicine batch + park-head verification gates; missing schedule escalates, adverse reaction requires notes/follow-up, empty medicine batch blocks submission. | `source-material/sop-playground-local/playground.html:1067-1082` |
| PPR/FMD/HS/Goat Pox | Schedule-bearing V1 matrix rows. They generate only when present in the active scoped vaccination ruleset, not because a SOP label exists. | `docs/preventive-care-vaccination/vaccination-rules.md`; `docs/preventive-care-vaccination/RULE-MATRIX-AUTHORING-HANDOFF.md` |
| BQ | Label-only until a reviewed goat schedule/dose/revaccination row is added to the governed matrix. | SOP playground labels above |
| Sheep Pox | Exclude from goat dev roster for now; the evidence found is sheep/procurement-history context. | `wiki/graphify-out/converted/Procurement DB [Goats]_dda03a25.md:78,:86,:91` |

## Seed/runtime decision

The repeatable local proof pack should use this as its governed matrix baseline:

- protocol/ruleset family: `vaccination.matrix`
- rows: ET+TT, PPR, FMD, HS, Goat Pox with the timing/dose/revaccination values above
- no `rule_dsl.source` publish gate; durable audit is protocol version metadata
  (`created_by`, `created_at`, `published_by`, `published_at`, version)
- stage default: K1/K2 from `animal_stage_lookup`, not frontend literals
- due dates: computed from row `offset_days` and accepted vaccination history

This closes the local/dev roster and K1/K2/stage pending item. Remaining
production work is normal governed versioning if real Preventive Care (PC) / vet data later
changes these values; it is not a local code or E2E blocker. The exact follow-up
scope and edge-case checklist for PPR/FMD/HS/BQ expansion lives in
`context/execution/vaccination-roster-expansion-followup.md`.

## 2026-07-02 matrix alignment

The earlier 2026-06-26 ET-only local proof baseline is superseded for new V1
Config work. The vaccination-rule matrix now supplies goat-applicable schedule
math for PPR/FMD/HS/Goat Pox in addition to ET+TT. Closed states:

| Vaccine | Closed state |
| --- | --- |
| ET+TT | `schedule-bearing matrix row` |
| PPR | `schedule-bearing matrix row` |
| FMD | `schedule-bearing matrix row` |
| HS | `schedule-bearing matrix row` |
| Goat Pox | `schedule-bearing matrix row` |
| BQ | `label-only until matrix row exists` |

No new draft should use the old ET/K1/day-21/0.5 ml fixture as the ruleset
contract. Keep it only as historical context for why the first local proof was
small. Current schedule authoring is superseded by
`docs/preventive-care-vaccination/APPROVED-SCHEDULE-MATRIX.md`.
