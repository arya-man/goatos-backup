Index: I1 identifier types per animal · I2 find an animal by tag / display id · I3 placeholder (TEMP-) RFIDs · I4 tag replacement / retirement history · I5 duplicates & double tags · I6 goat passport header fields · I7 goat timeline (identity events) · I8 sale allocation (tagged for sale)

# Goat passport + identity (tags, RFID, sale allocation): logic cards (verified 24/09/2026, goatos-stg)

Screens: admin-web `/goats/[goat_id]` (`features/goat-passport/index.tsx`), Herd register passport drawer (`features/counts/herd-passport-local-drawer.tsx`).
Endpoints (`backend/internal/identity/adapters/http/handler.go:37-57`): `GET /goats/search`, `GET /goats/{goat_id}` (passport),
`GET /goats/{goat_id}/timeline`, `GET /identifiers/{type}/{value}/resolve`, `POST /admin/goats/{id}/identifiers` (attach),
`POST /admin/goats/{id}/identifiers/{identifier_id}/retire`. Vaccination passport tab: `GET /goats/{goat_id}/passport`
(`backend/internal/passport/adapters/http/handler.go:33`). SQL: `backend/internal/identity/adapters/postgres/repository.go`, `sqlc/query.sql`.
No ceo_ai view covers identifiers: use `public.goat_identifiers` (+ `goats`).

## I1 What identifiers exist ("RFID kya hai", "ear tag number", "tag 1 / tag 2")
- `goats.display_id` = system id `G-000123` (unique, never on the animal). `goats.goat_id` = uuid.
- `goat_identifiers` (one row per tag ever): `identifier_type` animal_identifier_1 = **Tag 1 / RFID** (15-digit 9010070005xxxxx),
  animal_identifier_2 = **Tag 2 / ear tag** (e.g. `SA2328392`, `BLR-624`, `GOKUL-168`, `CBE-1797`), smart_ble_tag = BLE sensor
  (2 rows per sensor goat: short id `A00033` + MAC `F0:C9:90:A0:00:33`), temporary_tag. `status` active / retired / invalid / disputed;
  `valid_from`/`valid_to`; `source_system` (NULL = original import, double-tagging-csv, admin_herd_register, herd_signals, retag_2026_09_22, ...).
- Unique on (tenant_id, normalized_value) for the whole lifetime: a tag value can never be reused by another animal.
- 24/09/2026:
```sql
SELECT identifier_type, status, count(*) rows, count(DISTINCT goat_id) goats FROM goat_identifiers GROUP BY 1,2 ORDER BY 1,2;
```
  animal_identifier_1 active 1,728 / retired 87 · animal_identifier_2 active 1,509 (1,336 goats) / retired 91 · smart_ble_tag active 38 (19 goats) ·
  temporary_tag invalid 1, retired 1. Alive goats without an active Tag 1: 0 (13 inactive CPT goats have none); without Tag 2: 332 alive.

## I2 Find an animal by tag ("SA2328392 kaunsa bakra hai", "is RFID ka goat kahan hai")
- Screen search (`SearchGoats`, `repository.go:109`): `display_id = q` OR active identifier `normalized_value = q` (q only trimmed).
  Resolver (`FindIdentifierMatches` `repository.go:274`, app `service.go:318`): value UPPER-cased, matches active+retired+disputed; active
  wins, retired-only = "needs review", >1 active = multiple match.
- Trap: stored `normalized_value` is mixed case (alphanumeric tags are lower-case, e.g. `sa2328392`; BLE tags upper), so both screen paths can
  miss depending on typing. Ask Mesha: always compare `lower(btrim(identifier_value))`, include retired rows, and report status:
```sql
SELECT g.display_id, g.lifecycle_status, p.location_code park, s.name shed, gsp.partition_label part, gi.identifier_type, gi.identifier_value,
  gi.status, (gi.valid_from AT TIME ZONE 'Asia/Kolkata')::date from_d, (gi.valid_to AT TIME ZONE 'Asia/Kolkata')::date to_d
FROM goat_identifiers gi JOIN goats g ON g.goat_id=gi.goat_id
LEFT JOIN locations p ON p.location_id=g.park_id LEFT JOIN locations s ON s.location_id=g.shed_id
LEFT JOIN goat_shed_partitions gsp ON gsp.goat_id=g.goat_id
WHERE lower(btrim(gi.identifier_value)) = lower(btrim(:tag));
```
  24/09/2026 `SA2328392` -> G-000100, alive, Tag 2 active since 25/07/2026. For a display id: `WHERE g.display_id = upper(:id)`.
  Pen: prefer `references/pens.sql` (shed_id + partition); never `goats.current_location_id` (vestigial).
- BLE sensor lookups: match either the short id or the MAC; live signal is in `herd_signal_tag_latest` (SKILL).

## I3 Placeholder RFIDs ("kitne bina asli RFID ke")
- Some animals got `TEMP-<PARK>-<SHED>-NNN` (e.g. `TEMP-CBE-CASTRO2-038`) as their ACTIVE animal_identifier_1 instead of a real RFID.
  They count as "tagged" on Herd register (untagged kids = 0).
- `SELECT g.lifecycle_status, count(*) FROM goat_identifiers gi JOIN goats g USING (goat_id) WHERE gi.identifier_type='animal_identifier_1' AND gi.status='active' AND gi.identifier_value ILIKE 'TEMP-%' GROUP BY 1;`
- 24/09/2026: **329 alive**, 13 sold, 3 dead. The separate `temporary_tag` type (app "Awaiting RFID" list, `ListTemporaryTaggedGoats`) has 0 active.

## I4 Tag replacement / retirement history ("tag kab badla", "purana tag kya tha", "kitne tag replace hue")
- Retire = `POST .../identifiers/{id}/retire`: sets status 'retired' + `valid_to`, writes `identity_decisions` (decision_type retire_identifier)
  and `goat_identity_events` `goat.identifier.retired`. Attach = `attach_identifier` / `goat.identifier.added`.
- Replaced = goat with a retired AND an active row of the same type:
```sql
SELECT r.identifier_type, count(DISTINCT r.goat_id) replaced FROM goat_identifiers r
JOIN goat_identifiers a ON a.goat_id=r.goat_id AND a.identifier_type=r.identifier_type AND a.status='active'
WHERE r.status='retired' GROUP BY 1;
```
  24/09/2026: Tag 1 replaced on **75** goats, Tag 2 on **81**. Retired rows by IST valid_to: 20/09/2026 58 Tag 1; 14-15/08/2026 27 Tag 1 + 91 Tag 2;
  singles 07/09, 21/09 (temporary_tag), 22/09. 12 inactive goats have retired tags and no replacement (e.g. G-000148: RFID 901007000504382 +
  GOKUL-168 retired 15/08/2026).
- Decisions 24/09/2026: attach_identifier 117, retire_identifier 58 (all approved). Imported retirements (Aug bulk) have no decision row:
  count history from `goat_identifiers.status='retired'`, not decisions.

## I5 Duplicates ("same tag do goats pe?", "double tag")
- Same value on two animals is impossible (lifetime unique index); checked case-insensitively: 0 values on >1 goat. `display_id` duplicates: 0.
  Merged goats (`merged_into_goat_id` set): 0. `identity_conflicts`: 0 rows.
- Double tags are real: 173 goats (143 alive, 30 sold) carry TWO active animal_identifier_2 (a second 15-digit RFID + an ear tag, source
  double-tagging-csv). Passport/list SQL LEFT JOINs one active row per type, so a goat can show twice in the list and the header Tag 2 is arbitrary;
  the passport "Identifiers" table (`ListIdentifiersForGoat`, `sqlc/query.sql:107`) lists all rows (primary first, then status, newest).

## I6 Passport header ("is bakre ki details")
- `GetGoatByID` (`sqlc/query.sql:1`): display_id, active Tag 1/Tag 2, breed, sex, age_band, lifecycle_status, reproductive_status,
  growth_cohort_tag, management_stage, health_status, species, park/shed/partition (`goat_shed_partitions`), merged_into, row_version.
  Location shown = shed name else park name (not current_location_id).
- Trap: every field is the CURRENT value on `goats`; history is in the timeline (I7). health_status here is a goats column, not health_cases.

## I7 Timeline ("is goat ke saath kya hua")
- `GET /goats/{id}/timeline` -> `ListGoatTimeline` (`sqlc/query.sql:135`, `timeline_corrections_read.go:27`): `goat_identity_events` for the goat,
  newest `occurred_at` first, 20 per page; actor 'system' when actor_id NULL; decision_id links `identity_decisions`.
- Event mix 24/09/2026: goat.created 1,741, goat.stage_changed 170, goat.exited 149, goat.identifier.added 117, goat.health.changed 63,
  goat.identifier.retired 59, goat.location.changed 41, goat.identity.changed 13.
- Trap: not a full history: weighing, vaccination, feed and shifting_events live in their own modules; pen moves via shifting may not emit
  location.changed (41 only). For "where was it" use `goat_location_history` (SKILL).

## I8 Sale allocation ("kaunse bakre sale ke liye tag hue", "deal X mein kaunse goats")
- Screens: Sales allocation (`GET /admin/goats/sale-candidates`, `/sale-locations`, `POST /admin/goats/sale-allocations/preview|confirm`,
  `GET /admin/goats/sale-allocations/{sales_deal_id}`; `identity/adapters/http/sale_allocation_handler.go:61-65`).
  Candidates: `saleCandidateSelect` (`identity/adapters/postgres/sale_allocation.go:41`) = goats with up to 2 active tags, vaccination
  withdrawal (`vaccination_completions.withdrawal_until_date`, status accepted) and existing `goat_sale_allocations` status 'tagged'.
- `goat_sale_allocations`: one row per goat per deal (sales_deal_id -> sales_deals.id), status 'tagged' (or released with released_at/reason),
  weight_kg, tag_number, park/shed/partition at allocation.
- `SELECT a.status, g.lifecycle_status, count(*), count(DISTINCT a.sales_deal_id) deals FROM goat_sale_allocations a JOIN goats g USING (goat_id) GROUP BY 1,2;`
- 24/09/2026: **160 tagged, all lifecycle 'sold', across 14 deals** (= Herd register sold 160). Deal animal_count totals are larger (pre-app
  deals): see SKILL Two-source traps.

## I9 Passport warnings + evidence cards (`features/goat-passport/index.tsx:190-225`, `:320-350`) (Go-only)
- `GET /goats/{id}` passport (`identity/app/service.go:85-117`). Warnings = one `merged_redirect` per merge hop followed (goat with `merged_into_goat_id`), plus `summary.warnings` (tag lookups add `identifier_history_only` / `identifier_history_present`, `service.go:237,260`).
  Evidence refs: repository always returns `[]` (`adapters/postgres/repository.go:102`), so the card is always 0.
- Verified 24/09/2026: `SELECT count(*) FROM goats WHERE merged_into_goat_id IS NOT NULL` => **0**, so the warnings card is 0 for every goat today.
- Question: "Is this goat's record merged / any warning?" - "Is bakre ke record mein koi warning hai?" -> No (no merges on STG).
