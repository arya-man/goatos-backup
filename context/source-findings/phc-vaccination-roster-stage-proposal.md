# PHC Vaccination — Roster & Stage-Band Source Proposal

Date: 2026-06-26

## Purpose

This is an **evidence-backed proposal for human approval**, NOT a production
source of truth and NOT publishable config. It collects the clues found in the
Mesha wiki, the Goat OS docs, the procurement source data, and the SOP
playground so a named Mesha authority can review, correct, and approve real
vaccination protocol content.

Nothing in this file authorizes obligation generation against real animals. The
local/code path (contracts, Config UI, publish gate, SOP binding, proof-policy
validation) is closed independently of these values — see
[vaccination-pre-e2e-readiness-audit.md](../execution/vaccination-pre-e2e-readiness-audit.md).

## Approval owner

**TBD human owner** — to be one of: PHC Director / responsible vet / COO, or an
explicitly named Mesha authority. No named owner is recorded in the repo today;
do not infer one. Until a named owner signs off, every row below is a *clue*,
not approved content.

## Stage bands & vaccine clues

| item | proposed value / clue | source path | confidence | conflict / open question | approval needed |
| --- | --- | --- | --- | --- | --- |
| K0 stage band | newborn-with-mother, **max ~1 day** | `wiki/graphify-out/converted/Goats and Parks_0e7e3494.md`; `context/product/glossary.md:205` | medium | wording is "about one day" — exact upper bound not pinned | PHC Director / vet confirm exact band |
| **K1 stage band** | milk-training, **max ~7 days** (`min_age` after K0) | `wiki/graphify-out/converted/Goats and Parks_0e7e3494.md:272`; `wiki/graphify-out/graph.json:322`; `context/product/glossary.md:208` | medium | "maximum of 7 days" is a husbandry stage, not a vaccination trigger day | PHC Director / vet confirm K1 band → `animal_stage_lookup` |
| **K2 stage band** | milk-drinking, **~42 days / 6 weeks** | `wiki/graphify-out/converted/Goats and Parks_0e7e3494.md:273`; `wiki/graphify-out/graph.json:322`; `context/product/glossary.md:213` | medium | **CONFLICT** (see below) | PHC Director / vet reconcile 42 vs 45 |
| **K2 = 45 (legacy dashboard)** | legacy mock `SHIFT_THRESH` hardcodes **K2 = 45** | `docs/phc-vaccination/TRD.md:64`; `docs/phc-vaccination/PRD.md:94` ("45 (mock) vs 42 (SOP)") | high (that the conflict exists) | legacy dashboard says 45; wiki/SOP say 42 — must not ship either silently | reconcile to ONE config value on `animal_stage_lookup` |
| K2 reconcile directive | "Reconcile K2 = 42 vs 45 with PHC"; make it config, not hardcoded | `docs/phc-vaccination/TRD.md:64`, `:145`; `docs/protocol-engine/IMPLEMENTATION-PLAN.md:36`; `docs/schema-and-system-design.html:604` | high | Goat OS TRD already flags this as an open input, not a decision | PHC Director / vet decides the number |
| K3 / F2 / M0 / Mother / Pregnant stages | enumerated stage codes exist | `docs/phc-vaccination/TRD.md:58` (`animal_stage_lookup` CHECK); `context/product/glossary.md` | medium | bands beyond K2 not pinned to vaccination triggers | vet confirm if any vaccine keys off these |
| Vaccine clue — PPR | "PPR" appears in procurement intake + a process-integrity adherence example | `wiki/graphify-out/converted/Procurement DB [Goats]_dda03a25.md:86`; `context/frontend/vaccination-process-integrity-frontend-handoff.md:256` | low | appears as procurement-supplier history / UI demo text, not an approved schedule | vet approve name + schedule |
| Vaccine clue — ET (Enterotoxaemia) | Enterotoxaemia used in a PRD *example* dose row (0.5 ml · K1 · day 21 · +14d booster) | `docs/phc-vaccination/PRD.md:60`; `context/frontend/vaccination-process-integrity-frontend-handoff.md:344` | low | **explicitly an illustrative example** in the PRD, never approved values | vet approve name + dose + timing |
| Vaccine clue — FMD | "FMD" appears in procurement intake history | `wiki/graphify-out/converted/Procurement DB [Goats]_dda03a25.md:78` | low | supplier-reported history, not a Mesha schedule | vet approve name + schedule |
| Vaccine clue — HS | named in SOP-playground / source artifact discussion as a candidate | SOP playground / source artifact (not a committed Goat OS doc) | low | not found in committed Goat OS docs or wiki; clue only | vet confirm whether HS is in roster |
| Vaccine clue — BQ | named in SOP-playground / source artifact discussion as a candidate | SOP playground / source artifact (not a committed Goat OS doc) | low | not found in committed Goat OS docs or wiki; clue only | vet confirm whether BQ is in roster |
| Vaccine clue — Sheep Pox | appears alongside PPR/FMD in procurement intake history | `wiki/graphify-out/converted/Procurement DB [Goats]_dda03a25.md:78,:86` | low | species/supplier context (sheep), may not apply to goat roster | vet confirm applicability |

## Explicit non-approval statement on vaccine names

The vaccine-name clues above (PPR, ET/Enterotoxaemia, FMD, HS, BQ, Sheep Pox)
are **NOT approved schedule values**. They are scraped from supplier procurement
history, a PRD illustrative example, UI demo text, and SOP-playground
discussion. None of them carries an approved roster entry (dose, volume,
route/site, trigger band, booster interval, withdrawal, cold-chain). The
quantitative values that do appear (e.g. the PRD's "0.5 ml · day 21 · +14d") are
**example values for engine shape only** and must never be published.

## Conflict summary (must resolve before production content)

- **K2 age band: 42 (wiki/SOP) vs 45 (legacy dashboard mock).** The Goat OS TRD
  already requires this be reconciled with PHC and stored as config on
  `animal_stage_lookup`, not hardcoded. Engineering will not pick a number.
- **Vaccine roster is unproven.** No committed Goat OS doc lists an approved
  goat vaccine roster with doses/timing/boosters; only supplier-history and
  example clues exist.

## Closure statement

This proposal is not production-publishable until a named Mesha human authority
approves roster, stage bands, dose schedule, booster rules, and naming.
