# Configuration logic cards

Index: C1 parks (active, pens, animals, capacity) | C2 pens per park | C3 partitions | C4 register rail counts (items, breeds, stages) | C5 SOP library (published/draft/versions) | C6 vaccination protocol (live plan version, rules) | C7 feed ration grid (live rates) | C8 feed items | C9 alert rules config | C10 pen capacity (shed_capacity_current trap)

Paths: backend `backend/internal/...`, web `apps/admin-web/...`. Verified read-only on goatos-stg, 24/09/2026.
Status rule for all registers: a row is shown as `active` or `archived`. The rail count = active rows only (`adapters/postgres/common.go:87` `activeCountSQL`, called from `repository.go:110 Counts`).

---
## C1 Parks
- Screen: Configuration > Items and settings, `?register=parks` (`app/(admin)/configuration/items/page.tsx`).
- Endpoint: `GET /admin/configuration/registers` (rail counts), `GET /admin/configuration/parks` (rows) (`configuration/adapters/http/handler.go:60-61`).
- Code: `configuration/adapters/postgres/stores_places.go:33-48` (parkProjection).
- Formula: park = `locations.location_type='park'`; active if `locations.status='active'`, anything else archived. Row counts: `pens` = active shed children minus legacy partition alias rows (C2); `animals` = goats with `lifecycle_status='alive'` whose `shed_id` sits under the park. Capacity and code come from `park_profiles` (code falls back to `locations.location_code`).
- Filters->SQL: `status` tab -> `r.status = $2` ('all' skips it); search -> display ILIKE.
- Traps: park capacity is only filled for Parigi; CBE/CPT have no capacity or park_code. Parigi is an active park with 0 pens and 0 animals: leave it out of "our parks" answers unless asked.
- SQL (verified: Channapatna 8 pens / 712 alive; Coimbatore 10 / 850; Mesha Biome Parigi 0 / 0, capacity 100000):
```sql
SELECT l.name, pp.capacity,
  (SELECT count(*) FROM goats g JOIN locations s ON s.location_id=g.shed_id
    WHERE s.parent_location_id=l.location_id AND g.lifecycle_status='alive') AS animals
FROM locations l LEFT JOIN park_profiles pp ON pp.location_id=l.location_id
WHERE l.location_type='park' AND l.status='active';
```
- Questions: "How many parks do we have?" / "Kitne park active hain?" / "Coimbatore mein kitne animals hain abhi?"

## C2 Pens per park
- Screen: same page, `?register=pens`; column Park, counts Partitions / Animals.
- Endpoint: `GET /admin/configuration/pens` (`handler.go:61`).
- Code: `stores_places.go:118-140` (penProjection) + alias exclusion `platform/oploc/resolve.go:69-93`.
- Formula: pen = `locations.location_type='shed'` joined to a park parent, **excluding legacy alias rows** ("Castro 1", "Godel 1 - Part 3") where another active shed in the same park has a name prefix and an active partition whose `normalized_label` matches the leftover suffix. Active = `status='active'`. Row counts: partitions = active `shed_partitions`; animals = alive goats with `shed_id` = pen.
- Filters->SQL: `f_park_id` -> `fields @> {"park_id":...}`; status tab; search.
- Traps: raw `locations` has 122 active shed rows (CBE 73, CPT 49); 29 of those active ones (and more archived) are alias rows. Counting raw sheds over-reports pens ~5x. Archived pens (after alias exclusion): CBE 14, CPT 26. Always use `references/pens.sql` for pen labels.
- SQL (verified: CBE 10 active, CPT 8 active, total 18; 40 archived):
```sql
SELECT p.name park, count(*) FILTER (WHERE l.status='active') active_pens
FROM locations l JOIN locations p ON p.location_id=l.parent_location_id AND p.location_type='park'
WHERE l.location_type='shed' AND NOT EXISTS (
  SELECT 1 FROM locations ps JOIN shed_partitions pq ON pq.shed_id=ps.location_id AND pq.status='active'
  WHERE ps.parent_location_id=l.parent_location_id AND ps.location_id<>l.location_id
    AND ps.location_type='shed' AND ps.status='active' AND ps.retired_at IS NULL
    AND starts_with(btrim(l.name), btrim(ps.name))
    AND NULLIF(regexp_replace(btrim(replace(btrim(l.name),btrim(ps.name),'')),'^\s*-\s*part\s*|\s+','','gi'),'') = pq.normalized_label)
GROUP BY 1;
```
- Questions: "How many pens in Coimbatore?" / "CPT mein kitne shed/pen hain?" / "Which pens were archived?"

## C3 Partitions (pen parts)
- Screen: `?register=partitions`; display "Godel 1 - Part 3".
- Endpoint: `GET /admin/configuration/partitions`.
- Code: `stores_places.go:262-282` (partitionProjection), display composed in Go `decoratePartition`.
- Formula: one row per `shed_partitions` (id = `shed_id:normalized_label`); status `retired` shown as archived. Animals = alive goats in `goat_shed_partitions` whose label, lower-trimmed with leading "part " stripped, equals `normalized_label`.
- Traps: "Part 3" and "3" are the same partition. Retired partitions: CBE 9, CPT 4.
- SQL (verified: active CBE 67, CPT 50 = 117):
```sql
SELECT p.name, count(*) FROM shed_partitions x JOIN locations s ON s.location_id=x.shed_id
JOIN locations p ON p.location_id=s.parent_location_id WHERE x.status='active' GROUP BY 1;
```
- Questions: "How many partitions in Godel 1?" / "Godel 1 ke kitne part hain?"

## C4 Register rail counts (items, breeds, stages)
- Screen: left rail number next to each register; Items & categories uses the catalogue layout with list counts.
- Endpoint: `GET /admin/configuration/registers` -> `counts{register:n}`.
- Code: `repository.go:110`; items `stores_catalogue.go:296-340` (inventory_items UNION feed_item_catalog); list counts `stores_catalogue.go:48-50`; breeds `stores_animals.go:371`; stages `stores_animals.go:143`.
- Formula: active rows per register. Items = active `inventory_items` + active `feed_item_catalog` (feed rows id `feed:<uuid>`). Category "items" = active inventory items in that category (+ active feed catalog on the built-in top-level feed list); the UI sums a list's whole subtree.
- Traps: feed items live in a separate table (`feed_item_catalog`) and are merged here; inventory `inactive`/`retired` both read as archived.
- SQL (verified: items 41+4=45; categories 7; breeds 16; stages 19):
```sql
SELECT (SELECT count(*) FROM inventory_items WHERE status='active')
     + (SELECT count(*) FROM feed_item_catalog WHERE status='active') AS items;
```
- Questions: "How many medicines/vaccines are configured?" / "Kitne breeds set up hain?"

## C5 SOP library
- Screen: module SOP pages (`/vaccination/sops`, `/feed/sops`, `/counts/sops`, `/configuration/work-instructions` = `general` slice), `features/sops/module-page.tsx:258-276`, card `sop-library.tsx:63-65`.
- Endpoint: `GET /admin/sops?limit=200` (`sop/adapters/http/handler.go:33`), with embedded `latest_versions`.
- Code: list `sop/adapters/postgres/repository.go:39-58`; latest version `repository.go:2055-2079` (DISTINCT ON sop_id ORDER BY version DESC); publish retires the previous published version `repository.go:1920-1950`.
- Formula: SOP = `sop_definitions` (status draft/active/retired); card badge "published · vN" when definition is active. Versions in `sop_versions` (draft -> published -> retired; one published per SOP). Domain group = code prefix classifier (`sop-derive.ts classifyDomain`).
- Traps: the card's version number is the **latest** version (can be a draft), while the phone runs the published one. SOPs outside every module slice are not shown anywhere. Step counts come from `form_dsl`, not a column.
- SQL (verified: 21 SOPs all active, each with a published version; versions 21 published + 7 retired, 0 drafts; by prefix procurement 6, counts 3, feed 3, milk 2, sales 2, general/pc_care/shifting/vaccination/weighing 1):
```sql
SELECT split_part(code,'.',1) prefix, count(*) FROM sop_definitions WHERE status='active' GROUP BY 1;
SELECT status, count(*) FROM sop_versions GROUP BY 1;
```
- Questions: "How many SOPs are live?" / "Kaunse SOP draft mein hain?" / "Feed SOP ka latest version kya hai?"

## C6 Vaccination protocol (plan)
- Screen: Vaccination > Plan (`app/(admin)/vaccination/plan/page.tsx:22-50`).
- Endpoint: `GET /protocols?category=vaccination` (`protocol/adapters/http/handler.go:55`), then `GET /protocols/versions/{id}` per recent version.
- Code: `protocol/adapters/postgres/sqlc/query.sql:101` (ListProtocolConfigsForCategory, rule_count subquery); plan grouping `features/vaccination-plan/plan-model.ts:59-79`.
- Formula: live plan = the version with `status='published'`; history sorted by version desc. Rules = `protocol_rules` rows per version (one per dose_code). Vaccines on screen = `rule_dsl.matrix_rows` with a non-empty schedule, merged with the published rules.
- Traps: effective_from is not "when it went live" (v9 effective 30/08 while v8 is 31/08); use `published_at`. A draft (v10, 0 rules) exists and is NOT in force.
- SQL (verified: vaccination.matrix v9 published, 30 rules / 30 dose codes across 8 vaccines (BT, ET+TT, FMD, Goat pox, HS, PPR, Sheep pox, Z1+Z3); v1-v8 retired; v10 draft):
```sql
SELECT pv.version, pv.status, pv.published_at,
  (SELECT count(*) FROM protocol_rules pr WHERE pr.protocol_version_id=pv.protocol_version_id) rules
FROM protocol_definitions pd JOIN protocol_versions pv USING (protocol_id)
WHERE pd.category='vaccination' ORDER BY pv.version DESC;
```
- Questions: "Which vaccination plan version is live?" / "Vaccine plan mein kitne doses hain?" / "Is there an unpublished draft?"

## C7 Feed ration grid (live rates)
- Screen: Feed > Config (`app/(admin)/feed/config`, `features/feed/ration-grid-table.tsx`).
- Endpoint: `GET /feed-config/ration-rates?park_id=` (`feedconfig/adapters/http/handler.go:49,134`).
- Code: `feedconfig/adapters/postgres/repository.go:153-190`.
- Formula: in-force rate = `feed_ration_rates.valid_to IS NULL` for the park AND its feed item is `active` in `feed_item_catalog` (same predicate the feed sheet uses). grams_per_head per (ration group, shed tag, feed item).
- Filters->SQL: park (required), ration_group -> `ration_group_key = feed_config_norm()`, shed_tag, feed_items set.
- Traps: 1670 rows have `valid_to IS NULL` but only 462 are live; the rest point to retired feed items. Superseded history (valid_to set) is not shown.
- SQL (verified: CBE 231 live rates, 7 ration groups, 31 shed tags, 3 feed items; CPT identical 231):
```sql
SELECT l.name, count(*) FROM feed_ration_rates r JOIN locations l ON l.location_id=r.park_id
WHERE r.valid_to IS NULL AND EXISTS (SELECT 1 FROM feed_item_catalog c
  WHERE c.feed_item_key=r.feed_item_key AND c.status='active') GROUP BY 1;
```
- Questions: "What is the ration per head for pen X?" / "Kitne feed rates live hain?"

## C8 Feed items
- Screen: Feed > Config feed items table (`features/feed/feed-items-table.tsx`); also merged into Items & categories.
- Endpoint: `GET /feed-config/feed-items` (`handler.go:137`). Code: `feedconfig/adapters/postgres/repository.go:385-398`.
- Formula: all catalog rows (active and retired) ordered by display_order; status shown per row.
- SQL (verified: 4 active, 13 retired): `SELECT status, count(*) FROM feed_item_catalog GROUP BY 1;`
- Questions: "Which feeds are active?" / "Kaunsa feed band kiya gaya?"

## C9 Alert rules config
- Screen: Alerts > Configure drawer (`features/alerts/alerts-configure.tsx`).
- Endpoint: `GET /alerts/config` (`alerts/adapters/http/handler.go:53`).
- Code: catalog `alerts/domain/rules.go:60-86`; stored overrides `alerts/adapters/postgres/repository.go:43-56` (`alert_rule_config`); custom event rules `events.go:26` (`alert_event_rules`).
- Formula: two built-in rules: `pen_feed_quantity_change` (default ON, threshold 1 animal, 1-1000) and `feed_low_stock` (default ON, threshold 5 days left, 1-90). A row in `alert_rule_config` overrides enabled/threshold; no row = default.
- Traps: empty table does not mean alerts are off; it means defaults apply.
- SQL (verified: 0 override rows, 0 event rules -> both rules ON at defaults):
```sql
SELECT rule_key, enabled, threshold FROM alert_rule_config;
SELECT count(*) FROM alert_event_rules;
```
- Questions: "What is the low-stock alert threshold?" / "Alerts ka setting kya hai?"

## C10 Pen capacity (trap)
- Source: `ceo_ai.shed_capacity_current`; pen capacity field = `shed_profiles.capacity` (`stores_places.go:125-130`).
- Formula: view emits one row per shed row (including archived and alias rows) PLUS one row per partition label; animals = goats not in a terminal lifecycle; status unknown_capacity when capacity NULL.
- Traps: `shed_profiles.capacity` is NULL for all 175 profiles, so every row is `unknown_capacity`. Summing `animals` double counts (shed + its partitions): CBE 1700 vs 850 alive.
- SQL (verified: 0 of 175 profiles have capacity): `SELECT count(*), count(capacity) FROM shed_profiles;`
- Questions: "Which pens are over capacity?" -> answer: capacity is not configured. / "Pen ki capacity kitni hai?"

## C11 Other registers (roles, species, gender, stages lookups, status definitions, SOP categories, task types, reference lists, animals)
- Screen: same page `?register=<key>`; definitions `backend/internal/configuration/domain/registers.go:240-470`; stores `adapters/postgres/repository.go:73-88`.
- Endpoint: `GET /admin/configuration/{register}` rows + `counts` (`handler.go:61`); rail = active count (`common.go:87`).
- Formula / source per register:
  - roles -> `designation_catalog` (retired shown as archived); row count "people" = `person_access` with that designation (`stores_animals.go:251-262`).
  - species -> `species_lookup`, gender -> `sex_lookup`; row count animals = alive goats with that code (`stores_animals.go:37-47`).
  - status_definitions -> `status_definitions` (`active` bool; read-only built-ins; `stores_reference.go:30-40`).
  - sop_categories -> `sop_categories`, task_types -> `sop_task_types`, reference_lists -> `reference_lists` (+ entries count from `reference_list_entries`) (`stores_reference.go:271-277`).
  - animals (read-only, importable) -> `goats` not merged; rail = alive only (`stores_herd.go:74-77`), list shows all lifecycle statuses.
- Traps: the Animals rail (alive) differs from the list (all). Role "people" counts are not tenant-filtered when tenant is null. Parigi etc. do not matter here.
- SQL (verified 24/09: animals 1562; species 2 (Goat 693, Sheep 869 alive); gender 2 (Female 984, Male 578); status definitions 23 (health 6, lifecycle 5, growth_cohort 5, reproductive 5, management 2); SOP categories 5; task types 25; reference lists 3 (exit_reasons 5, weight_bands 4, animal_purposes 2); roles 20 (19 active, Operator retired; Feed Manager 21 people, Health Manager 6, CEO/CXO 5, Cleaning Manager 2, Verifier 1, Procurement Director 1)):
```sql
SELECT (SELECT count(*) FROM goats WHERE lifecycle_status='alive' AND merged_into_goat_id IS NULL) animals,
  (SELECT count(*) FROM species_lookup WHERE status='active') species,
  (SELECT count(*) FROM status_definitions WHERE active) status_defs,
  (SELECT count(*) FROM sop_task_types WHERE status='active') task_types;
SELECT d.label, d.status, (SELECT count(*) FROM person_access pa WHERE pa.designation_code=d.designation_code) people
FROM designation_catalog d ORDER BY 3 DESC;
```
- Questions: "How many people hold each role?" / "Kis role mein kitne log hain?" / "Kitne sheep aur goat hain?" / "Male female kitne hain?"

## C12 Row drawer usage + bulk sheet / workbook drawers
- Row drawer (`features/configuration/row-drawer.tsx`) calls `GET /admin/configuration/{register}/{row_id}/usage` (`handler.go:67`): per-register checks (`common.go:266 usageOf`), e.g. stages -> alive animals + pens using the stage (`stores_animals.go:162-166`); used rows cannot be deleted, only archived.
- Sheet/workbook drawers (`sheet-drawer.tsx`, `workbook-drawer.tsx`): download/upload of a register; import validation is Go-only (`adapters/postgres/import_store.go`), no reporting figures.
- Questions: "Can I delete this stage?" / "Yeh stage kahin use ho raha hai kya?"
