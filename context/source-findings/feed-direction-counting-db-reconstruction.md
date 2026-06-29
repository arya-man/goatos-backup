# Feed Direction Counting DB Reconstruction

Status: source finding for Feed Direction implementation reference.

This note records a sanitized inventory of the shared Google Sheet `Counting DB`
recreated locally on 2026-06-29. Raw sheet rows are intentionally not committed.

## Local Source Artifact

```text
wiki/Counting DB recreated 2026-06-29/
  Counting DB - values only.xlsx
  csv/
  metadata.json
  README.md
```

Source spreadsheet:

```text
title: Counting DB
identifier: recorded in local metadata.json, not committed here
```

Extraction context:

```text
account: authorized Mesha/VGoats Google account
project: goatos-dev
organization: vgoats.com / 563962826703
method: gcloud OAuth token + Google Sheets values API
Drive export result: 403 cannotExportFile
```

The local workbook is values-only. Original formulas, formatting, protected
ranges, filter views, validation chips, comments, and hidden-sheet state are not
preserved. Use the local CSV/XLSX as source evidence and fixture/reference data,
not as canonical Goat OS runtime truth.

## Recovered Tabs

| # | Source tab | Hidden | Recovered rows | Recovered cols |
| ---: | --- | --- | ---: | ---: |
| 0 | Copy of DB | yes | 5384 | 8 |
| 1 | Comparision-DB | yes | 155 | 12 |
| 2 | Sheet38 | yes | 41 | 10 |
| 3 | DB | no | 83868 | 8 |
| 4 | FutureDB | no | 131 | 8 |
| 5 | 23/04/2026 - CBE Count Update | yes | 141 | 42 |
| 6 | Copy of Comparision-DB | yes | 155 | 12 |
| 7 | Projected-DB | yes | 323 | 8 |
| 8 | Pivot Table 12 | yes | 3 | 6 |
| 9 | CompareDB | yes | 80 | 40 |
| 10 | CBE Shed Overview | yes | 15 | 11 |
| 11 | CPT Shed Overview | yes | 9 | 12 |
| 12 | Template | no | 322 | 8 |
| 13 | Compare-DB-ProjectedDB | yes | 38 | 15 |
| 14 | CPT-Shedwise | no | 461 | 80 |
| 15 | CBE-Shedwise | no | 446 | 78 |
| 16 | Total-Count | no | 904 | 11 |
| 17 | CBE-Kids-Adults | no | 447 | 27 |
| 18 | CPT-Kids-Adults | no | 369 | 26 |
| 19 | Sheet32 | yes | 427 | 24 |
| 20 | CBE-Tagswise | no | 446 | 22 |
| 21 | CPT-Tagswise | no | 456 | 19 |
| 22 | Born-Datewise | no | 368 | 3 |
| 23 | Death-Datewise | no | 263 | 3 |
| 24 | Sales-Datewise | no | 68 | 3 |
| 25 | Active-Goats | no | 987 | 8 |
| 26 | Validation | no | 96 | 8 |
| 27 | CBE-Breedwise | no | 446 | 13 |
| 28 | CPT-Breedwise | no | 460 | 14 |
| 29 | Breedwise-Count | yes | 16 | 12 |
| 30 | Farmwise-Count | yes | 1 | 5 |
| 31 | Gender-Ratio | no | 3 | 2 |

## Field Shape Relevant To Feed Direction

The main `DB` sheet has the source-facing headcount shape:

```text
Date
Farm
Shed
Shed Tag
Breed
Age
Count
Staff (Counted)
```

This supports the Feed Direction TRD assumption that feed planning consumes a
daily or intra-day headcount by date, farm/park, shed, animal stage/tag, breed,
and count. The source tabs also expose shedwise, tagwise, breedwise, kids/adults,
active-goat, birth/death/sales, validation, projected, and total-count views
that are useful for migration fixtures and parity checks.

## Goat OS Design Implications

- `Farm` should map to canonical park/farm `locations`, not a hardcoded enum.
- `Shed` should map to shed `locations` and `shed_profiles`.
- `Shed Tag` / `Age` should be normalized through `animal_stage_lookup` and
  source aliases instead of being embedded in feed code.
- `Breed` should map through breed/species reference data with aliases.
- `Count` is source evidence for headcount snapshots and feed planning fixtures;
  Goat OS runtime feed generation still computes from canonical goat/location
  state and writes `supply_planning` plus `feed_directions`.
- The 2 PM feed-direction recompute in the TRD should be validated against this
  source family: count updates and shifted/active goat views are separate
  evidence surfaces that need deterministic import/replay semantics.

Do not commit the raw CSV rows into `goatos`. If a seeded development fixture is
needed, derive the smallest sanitized fixture from this local source and document
the derivation separately.
