# Live Legacy Critical Guardrails Findings - 2026-06-28

This note records sanitized read-only findings from the legacy `goatos-sheets`
project and shared Google Sheets. It supports the Goat OS critical-action
guardrails docs without committing raw private rows, goat-level incident data,
PII, proof media, tokens, or private URLs.

## Access Boundary

- Date checked: 2026-06-28
- Account context: `ravi@mesha.sg`
- Organization: `vgoats.com` / `organizations/563962826703`
- Project checked read-only: `goatos-sheets`
- Purpose: verify legacy capability parity and known gaps for movement,
  quarantine/warmup, health follow-up, vaccination evidence, shed metadata, and
  timetable/round guardrails.

## BigQuery Schemas Checked

| Source | Rows observed | Relevant fields/signals | Sanitized finding | Confidence |
| --- | ---: | --- | --- | --- |
| `goatos-sheets.Shiftings.shiftings_fact` | 9,718 | source/destination shed, destination tag, comments, requested-by, approval status, workflow status, overdue flags, counts | Preserves useful movement workflow state, but does not expose structured quarantine reason, exception type, linked evidence, quarantine episode, shed-fitness decision, or follow-up task linkage. | High |
| `goatos-sheets.Shiftings.shiftings_reports_clean` | 796 | request/direction type, category, priority, goat id, source/destination shed, comments, scheduled/completed dates, approval/status | Confirms legacy shifting captures operational workflow fields that Goat OS must preserve during import/cutover. | High |
| `goatos-sheets.healthDB.health_db_clean_dev` | 1,407 | problem, diagnosis, medicine, status | Confirms health problem/treatment status exists, but quarantine-placement validation is not represented as a first-class policy decision. | High |
| `goatos-sheets.goatsDB.goats_db_clean_dev` | 13,226 | event, goat id, shifting id, source/destination shed, destination tag, weight, comments | Confirms goat movement history is represented, but destination names/tags and comments are not enough to determine biological quarantine vs operational separation. | High |
| `goatos-sheets.weights.weights_db_clean` | 3,642 | date, farm, shed, shed tag, breed, gender, weight, goat type | Confirms weight snapshots can support checks, but not automatic critical-action obligations by themselves. | High |
| `goatos-sheets.procurement_farm.procurement_dB_clean` | 100 | procurement load/date/vendor/type/cost/weight fields | Cleaned procurement table does not carry the sheet-level `Vaccination` header observed in the source sheet. | Medium |
| `goatos-sheets.procurement_farm.procurement_details_clean` | 100 | procurement load details and problems | Preserves procurement problem context, but not the full source health/vaccination evidence surface. | Medium |
| `goatos-sheets.procurement_farm.procurement_holding_farm_clean` | 455 | holding-farm animal status, tag, weight, breed, gender | Supports separate procurement holding/warmup modeling before accepted herd intake. | High |

## Google Sheets Metadata/Headers Checked

| Sheet | Metadata checked | Relevant tabs/headers | Sanitized finding | Confidence |
| --- | --- | --- | --- | --- |
| Health DB | 19 tabs listed; headers sampled for DB, Problem, Diagnosis Form, Follow Up, Treatments-Schedule | problem id/name, diagnosis, medicine, status, ICU bit, due date, assignee, follow-up, treatment details | Legacy has health problem/follow-up/treatment tracking that Goat OS must preserve and strengthen with state machines, obligations, proof, and escalation. | High |
| Goats DB | 30 tabs listed; headers sampled for DB, Active-Goats-List, Validation, Verify-Shedwise-Count | event, goat id, shifting id, source shed, destination shed/tag, shifting type/category/priority, comments, weight/load id | Legacy preserves movement and active-goat context, but quarantine intent is not a typed state. | High |
| Sheds DB | Single `DB` sheet; header sampled; Q1/Q2-style shed labels observed without private goat rows | shed labels/tags and capacity-like values | Legacy shed names/tags are useful source context, but must become canonical location profiles with capability, fitness, timetable, and active-state policy. | High |
| Procurement DB [Goats] | 14 tabs listed; headers sampled for DB, Quratine Center Goat DB, Procurment SOP Selection DB, Procurement App Responses, Procurement Transit Responses, Validation | procurement load, holding/quarantine center, repeated weight/health check columns, vaccination, SOP selection, transit response | Confirms procurement warmup/holding/quarantine is a separate flow from farm movement into a quarantine-named shed. | High |

## Sanitized Live Observations

- Quarantine-like movement history exists as free-text comments or destination
  tags. Examples of themes found: ORF, fever, recovered animals, and
  quarantine-tagged destinations. These prove useful legacy context exists, but
  not source-backed policy validation.
- Sheds with names/tags that look like quarantine can also be operational shed
  labels. Goat OS must separate biological/process quarantine from location
  naming.
- Procurement has a source-sheet `Vaccination` header, but cleaned BigQuery
  tables inspected do not carry a first-class vaccination evidence field. Goat
  OS must preserve imported vaccination source facts and add proof/confidence
  semantics instead of assuming a row means the dose was truly administered.
- Legacy exposes useful status/proof/timing fields, but does not appear to
  enforce typed reasons, source-backed evidence validation, location fitness,
  automatic timetable obligations, durable escalation, or proof confidence as a
  generic kernel decision.

## Doc Impact

These findings support the following permanent Goat OS rules:

- Legacy replacement means capability parity plus gap closure, not bug-for-bug
  behavior parity.
- Every legacy-replacing policy pack must declare its legacy capability parity
  floor, known gaps to close, and import/replay mapping.
- Critical-action policy must classify the action, not infer process state from
  a shed name or free-text comment.
- Imported legacy rows must keep raw-source references for audit and
  reconciliation while canonical Goat OS state is produced separately.
