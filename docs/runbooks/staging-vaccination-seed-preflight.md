# Staging Vaccination Seed Preflight

Run this before any `goatos-stg` vaccination seed. Do not seed staging when a
blocker below is red.

## Source Of Record

- Use `Vaccination_DB_-V2` / `Demo DB` as the vaccination seed source. Demo DB
  must stay an exact staging workbook derived from V2, not an independently
  edited copy.
- Current verified source counts: V2 has 1,311 animal rows, 1,310 unique
  nonblank RFIDs, and 1 blank-RFID-but-old-tag-present row. Demo DB has the same
  1,311 vaccination rows, the same 1,310 nonblank RFIDs, and zero normalized
  vaccination-cell diffs from V2.
- Local must be a rehearsal of the staging seed. Before seeding `goatos-stg`,
  local must contain the exact same 1,311 source animal keys and no extra Goat OS
  animal keys.
- The known blank-RFID row is not identity-less: farm `CPT`, old ID `1388`,
  old ID suffix `BLR`, age `Kid`, gender `Male`, breed `Beetal`, source tag
  `K2`, shed `Yashoda`, partition `2`. It may be seeded using the old tag as a
  real animal identifier; do not label it "missing ID". Only rows where both
  RFID and old/source tag are blank are identifier blockers.
- RFID and old/source tag are both valid animal identifiers. Use RFID as the
  join key where present. When RFID is blank but old/source tag exists, use the
  old/source tag as the available animal identifier. Never invent a composite
  key from farm + shed + age + gender + breed + partition.

## Strict Blockers

- Founder/builder visibility: `ravi@mesha.sg`, `manohark@mesha.sg`,
  `manju@mesha.sg`, `abhishek@mesha.sg`, and `aryaman@mesha.sg` must be
  `ceo_internal`, tenant-scoped, and must have every built visible module
  available, including `admin.people`.
- Identity columns: Goat OS `display_id` is the short readable internal ID.
  `animal_identifier_1` and `animal_identifier_2` are real-world animal ID
  slots. RFID and old/source tag are both valid values for these slots. Herd
  Register must display all three columns: `display_id`, `animal_identifier_1`,
  and `animal_identifier_2`. The second identifier may render `—`; an animal
  must not render as missing ID when either RFID or old/source tag exists.
- Cohorts: vaccination cohort rows must not show `Unknown`. Blank source stage
  must be fixed in the source sheet or deliberately mapped from age before
  import.
- Protocol shape: real vaccination seed must publish one canonical
  `vaccination.matrix` version (`V1 Real Vaccination`) containing the
  vaccination rules. Do not create separate obligation families such as
  `vaccination.ppr`, `vaccination.fmd`, or "Real herd import" protocols for the
  same real seed.
- Source identifier completeness: reject or fix goat rows only when both RFID
  and old/source tag are blank. The known blank-RFID row
  `CPT / 1388 / BLR / K2 / Yashoda / 2` still has an old/source tag identity.
- Farm/shed placement: every imported goat must resolve to a real park and shed.
  Blank shed means the animal is rejected from the seed.
- Sex and dates: sex must be explicit `Male`/`Female`; dates must be real
  `YYYY-MM-DD`. Do not let importer defaults hide source mistakes.
- Workflow links: Passport/history links may point only to real workflow rows.
  Source obligation IDs are lineage, not clickable workflow records.
- No E2E/story/dev data in shared staging: reject seed inputs, protocol codes,
  idempotency keys, or labels containing `e2e`, `story`, `stub`, `dummy`,
  `local`, `test`, `goatos-pgtest`, or `seed-calendar-vaccination-dev` unless a
  migration note explicitly proves they are production-safe.
- Action Center counts must be explained before staging. The sidebar badge is
  grouped open process-integrity work, not raw goat count. Export the counts by
  obligation status, dose rule, distinct goats, and grouped work state before
  importing into `goatos-stg`.

## Clean Seed Checks

After local seed and before staging:

```sql
-- No long RFID values in short display IDs.
SELECT count(*) FROM goats
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'
  AND display_id LIKE 'G-901%';

-- All alive seeded goats have at least one real-world animal identifier.
WITH goat_ids AS (
  SELECT g.goat_id,
         g.lifecycle_status,
         max(gi.identifier_value) FILTER (
           WHERE gi.identifier_type = 'animal_identifier_1' AND gi.status = 'active'
         ) AS animal_identifier_1,
         max(gi.identifier_value) FILTER (
           WHERE gi.identifier_type = 'animal_identifier_2' AND gi.status = 'active'
         ) AS animal_identifier_2
  FROM goats g
  LEFT JOIN goat_identifiers gi
    ON gi.tenant_id = g.tenant_id
   AND gi.goat_id = g.goat_id
  WHERE g.tenant_id = '00000000-0000-4000-8000-000000000001'
  GROUP BY g.goat_id, g.lifecycle_status
)
SELECT count(*) FILTER (WHERE lifecycle_status = 'alive') AS alive,
       count(*) FILTER (
         WHERE lifecycle_status = 'alive' AND NULLIF(btrim(animal_identifier_1), '') IS NOT NULL
       ) AS alive_with_identifier_1,
       count(*) FILTER (
         WHERE lifecycle_status = 'alive' AND NULLIF(btrim(animal_identifier_2), '') IS NOT NULL
       ) AS alive_with_identifier_2,
       count(*) FILTER (
         WHERE lifecycle_status = 'alive'
           AND (
             NULLIF(btrim(animal_identifier_1), '') IS NOT NULL OR
             NULLIF(btrim(animal_identifier_2), '') IS NOT NULL
           )
       ) AS alive_with_any_identifier
FROM goat_ids;

-- This must return zero rows before staging.
WITH goat_ids AS (
  SELECT g.display_id,
         g.lifecycle_status,
         max(gi.identifier_value) FILTER (
           WHERE gi.identifier_type = 'animal_identifier_1' AND gi.status = 'active'
         ) AS animal_identifier_1,
         max(gi.identifier_value) FILTER (
           WHERE gi.identifier_type = 'animal_identifier_2' AND gi.status = 'active'
         ) AS animal_identifier_2
  FROM goats g
  LEFT JOIN goat_identifiers gi
    ON gi.tenant_id = g.tenant_id
   AND gi.goat_id = g.goat_id
  WHERE g.tenant_id = '00000000-0000-4000-8000-000000000001'
  GROUP BY g.goat_id, g.display_id, g.lifecycle_status
)
SELECT display_id, animal_identifier_1, animal_identifier_2
FROM goat_ids
WHERE lifecycle_status = 'alive'
  AND NULLIF(btrim(animal_identifier_1), '') IS NULL
  AND NULLIF(btrim(animal_identifier_2), '') IS NULL;

-- Local rehearsal count. This must match the V2/Demo source animal-key count.
SELECT count(*) AS local_goats,
       count(DISTINCT gi.identifier_value) FILTER (
         WHERE gi.identifier_type = 'animal_identifier_1' AND gi.status = 'active'
       ) AS local_distinct_primary_animal_ids
FROM goats g
LEFT JOIN goat_identifiers gi
  ON gi.tenant_id = g.tenant_id
 AND gi.goat_id = g.goat_id
WHERE g.tenant_id = '00000000-0000-4000-8000-000000000001';

-- No vaccination cohort Unknown labels.
SELECT count(*) FROM goats
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'
  AND lifecycle_status = 'alive'
  AND NULLIF(btrim(management_stage), '') IS NULL;

-- One real vaccination config family.
SELECT pd.code, pv.version_label, pv.status, count(pr.rule_id) AS rules
FROM protocol_versions pv
JOIN protocol_definitions pd USING (tenant_id, protocol_id)
LEFT JOIN protocol_rules pr USING (tenant_id, protocol_version_id)
WHERE pv.tenant_id = '00000000-0000-4000-8000-000000000001'
  AND pd.category = 'vaccination'
  AND pd.status = 'active'
  AND pv.status <> 'retired'
GROUP BY pd.code, pv.version_label, pv.status
ORDER BY pd.code;

-- No test/E2E vaccination protocol families in the shared environment.
SELECT pd.code, pd.name, pd.category, pd.status
FROM protocol_definitions pd
WHERE pd.tenant_id = '00000000-0000-4000-8000-000000000001'
  AND pd.category = 'vaccination'
  AND (
    pd.code ILIKE '%e2e%' OR pd.code ILIKE '%test%' OR
    pd.name ILIKE '%story%' OR pd.name ILIKE '%stub%' OR
    pd.name ILIKE '%dummy%' OR pd.name ILIKE '%local%'
  );

-- No test/E2E seeded obligations in the shared environment.
SELECT count(*)
FROM obligation_instances oi
WHERE oi.tenant_id = '00000000-0000-4000-8000-000000000001'
  AND (
    oi.idempotency_key ILIKE '%e2e%' OR
    oi.idempotency_key ILIKE '%test%' OR
    oi.idempotency_key ILIKE '%story%' OR
    oi.idempotency_key ILIKE '%stub%' OR
    oi.idempotency_key ILIKE '%dummy%' OR
    oi.idempotency_key ILIKE '%local%'
  );

-- Explain the Action Center/PC sidebar badge before staging.
SELECT pd.code,
       pv.version_label,
       pr.dose_code,
       oi.status,
       count(oi.obligation_id) AS obligations,
       count(DISTINCT oi.target_id) FILTER (WHERE oi.target_type = 'goat') AS goats
FROM obligation_instances oi
JOIN protocol_versions pv USING (tenant_id, protocol_version_id)
JOIN protocol_definitions pd USING (tenant_id, protocol_id)
JOIN protocol_rules pr
  ON pr.tenant_id = oi.tenant_id
 AND pr.rule_id = oi.rule_id
WHERE oi.tenant_id = '00000000-0000-4000-8000-000000000001'
  AND pd.category = 'vaccination'
GROUP BY pd.code, pv.version_label, pr.dose_code, oi.status
ORDER BY pd.code, pr.dose_code, oi.status;

-- Campaign/grouping check. If this returns zero, Action Center rows are grouped
-- directly by process-integrity dimensions rather than an explicit batch table.
SELECT count(*) FROM obligation_batches
WHERE tenant_id = '00000000-0000-4000-8000-000000000001';
```
