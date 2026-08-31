-- Seed the 8 LEGACY procurement loads into STG for the Sales load-wise card.
--
-- Maintainer decisions 2026-08-31 (docs/decisions/sales-loadwise.md, "App loads only" narrowed
-- the same day): the loads the farm already tracks on the Weights "Daily gain by load" card
-- (weighing_shed_load_tags: 100, 101, 113, 126, 128, 129, 130, 131) are seeded from the legacy
-- records, everything else waits for procurement source entry.
--
-- Sources, recorded per number:
--   - Load facts + landed cost: the "Goat/Sheep DB" Google Sheet, tab DB (spreadsheet
--     1_856nbDd5jHORrq1ISpaELGn6dewzG2AedlsxE1_bXE), read 2026-08-31. Cost lands in animal_cost
--     (the sheet keeps one total; no transport split exists there).
--   - Prior outcomes (already sold / already died, with dates): BigQuery
--     goatos-sheets.procurement_farm.loadwise_summary + load_wise_procurement_with_status
--     (death dates) + fattening_load_sales_comparison (per-load sold revenue) + salesDB_clean
--     grouped by purchase_id (sale date ranges), read 2026-08-31.
--   - Membership: the animals ALIVE TODAY in each load's tagged shed/partition (the same cohort
--     the Daily-gain card attributes). Loads 100 and 101 share one tagged pen (CPT Mandela 1 -
--     Part 1), so neither gets membership — their handful of survivors shows as a red
--     Unaccounted count, which is the honest state. Load 113 is sold out; no membership.
--
-- RUN AFTER migration 000229 is live on STG (the cost columns and the prior-outcomes table are
-- created there). Idempotent: loads key on their idempotency_key, membership inserts ON CONFLICT
-- DO NOTHING, prior outcomes upsert on (tenant, load, outcome). Run inside the transaction below
-- and eyeball the verification SELECT before COMMIT.
--
-- psql "$STG_URL" -f tools/dev/seed-stg-loadwise-legacy-loads.sql

BEGIN;

CREATE TEMP TABLE legacy_loads (
    load_ref text PRIMARY KEY,
    vendor text NOT NULL,
    purchase_date date NOT NULL,
    expected_count integer NOT NULL,
    animal_cost numeric(14, 2) NOT NULL,
    -- The farm the load went to, per the sheet. It answers the Farm column for a load whose
    -- animals are all gone, where no resident is left to agree on a park.
    farm text NOT NULL
) ON COMMIT DROP;

INSERT INTO legacy_loads VALUES
    ('100', 'Green Fresh Farm',         DATE '2025-10-23',  76, 539000, 'CPT'),
    ('101', 'Dr Praneeth',              DATE '2025-10-21',  70, 572000, 'CPT'),
    ('113', 'Nutriplus Foods Pvt Ltd.', DATE '2025-11-13', 100, 980000, 'CBE'),
    ('126', 'Ramesh Reddy',             DATE '2026-05-11',  67, 670000, 'CBE'),
    ('128', 'Krishnamorrthy',           DATE '2026-05-26',  70, 580000, 'CBE'),
    ('129', 'Krishnamorrthy',           DATE '2026-06-01',  78, 675100, 'CPT'),
    ('130', 'Green Fresh Farm',         DATE '2026-06-12',  77, 669465, 'CBE'),
    ('131', 'Krishnamorrthy',           DATE '2026-06-22',  63, 502000, 'CPT');

-- Vendor parties, created only where the register does not already carry the name.
INSERT INTO parties (party_type, display_name, status)
SELECT 'org', ll.vendor, 'active'
FROM (SELECT DISTINCT vendor FROM legacy_loads) ll
WHERE NOT EXISTS (
    SELECT 1 FROM parties p WHERE lower(p.display_name) = lower(ll.vendor)
);

-- The load rows. animal_cost is the sheet's recorded total; context carries the farm's load
-- number for display. Keyed on idempotency_key so a re-run updates nothing and adds nothing.
INSERT INTO procurement_loads (
    tenant_id, source_party_id, purchase_date, expected_count, status, notes, context,
    idempotency_key, animal_cost, cost_recorded_at
)
SELECT '00000000-0000-4000-8000-000000000001'::uuid,
       (SELECT p.party_id FROM parties p
         WHERE lower(p.display_name) = lower(ll.vendor)
         ORDER BY p.created_at LIMIT 1),
       ll.purchase_date, ll.expected_count, 'accepted_intake',
       'Seeded from the legacy load sheet (2026-08-31)',
       jsonb_build_object('load_ref', ll.load_ref, 'farm', ll.farm),
       'legacy-load-' || ll.load_ref,
       ll.animal_cost, now()
FROM legacy_loads ll
ON CONFLICT (tenant_id, idempotency_key) DO NOTHING;

CREATE TEMP TABLE legacy_load_ids ON COMMIT DROP AS
SELECT pl.context->>'load_ref' AS load_ref, pl.load_id
FROM procurement_loads pl
WHERE pl.tenant_id = '00000000-0000-4000-8000-000000000001'
  AND pl.idempotency_key LIKE 'legacy-load-%';

-- Membership is PER ANIMAL, selected and recorded by the animal's own TAG.
--
-- The farm tagged each load to a PEN (weighing_shed_load_tags), and every animal that arrived on
-- those loads carries a placeholder tag naming the pen it landed in: TEMP-CBE-CASTRO2-014. That
-- TAG is the animal's identity and does not change when it walks; its pen does. Selecting by
-- CURRENT pen therefore attributed the wrong animals -- verified against the register 2026-08-31:
-- three CBE-CASTRO2 animals and one CBE-CASTRO3 animal had moved to Godel 2 / Yashoda and were
-- MISSED, while two CBE-CASTRO3 animals sitting in Castro pen 2 were attached to load 130 instead
-- of their own load 128. Selecting by the tag fixes both, and load 128 then reconciles exactly
-- (70 declared = 4 died + 66 attached), which is the evidence the tag is the right key.
--
-- Nothing here joins a location, so this seed no longer has a location dependency at all.
--
-- KNOWN DATA GAP: these tags are PLACEHOLDERS, not RFIDs -- 0 of the legacy sheet's 601 tag
-- numbers resolve to a real RFID in the register, by exact value or last-4. The identifiers
-- recorded below are what the register actually holds; a re-run re-reads them, so the mapping
-- becomes RFID-backed the day those animals are tagged for real, with no schema or code change.
CREATE TEMP TABLE legacy_load_tag_prefixes (load_ref text, tag_prefix text) ON COMMIT DROP;
INSERT INTO legacy_load_tag_prefixes VALUES
    ('126', 'TEMP-CBE-CASTRO1-'),
    ('130', 'TEMP-CBE-CASTRO2-'),
    ('128', 'TEMP-CBE-CASTRO3-'),
    ('131', 'TEMP-CPT-CASTRO1-'),
    ('131', 'TEMP-CPT-CASTRO2-'),
    ('129', 'TEMP-CPT-GODEL2P1-'),
    ('129', 'TEMP-CPT-GODEL2P2-');

-- A re-run REBUILDS this seed's membership rather than adding to it: an animal whose tag says it
-- belongs elsewhere must LOSE its old row, which ON CONFLICT alone could never do. Scoped to the
-- loads this script owns, so no app-recorded load is touched.
DELETE FROM procurement_load_goats plg
USING legacy_load_ids li
WHERE plg.load_id = li.load_id AND plg.tenant_id = '00000000-0000-4000-8000-000000000001';

INSERT INTO procurement_load_goats (
    tenant_id, load_id, goat_id, selection_state, current_state, intake_accepted_at,
    animal_identifier_1, animal_identifier_2
)
SELECT g.tenant_id, li.load_id, g.goat_id, 'accepted_herd_intake', 'accepted_herd_intake',
       ll.purchase_date::timestamptz,
       gi.identifier_value,
       (SELECT g2.identifier_value FROM goat_identifiers g2
         WHERE g2.tenant_id = g.tenant_id AND g2.goat_id = g.goat_id
           AND g2.identifier_type = 'animal_identifier_2'
           AND g2.status = 'active' AND g2.valid_to IS NULL
         ORDER BY g2.is_primary_for_goat DESC, g2.valid_from DESC LIMIT 1)
FROM legacy_load_tag_prefixes pfx
JOIN legacy_load_ids li ON li.load_ref = pfx.load_ref
JOIN legacy_loads ll ON ll.load_ref = pfx.load_ref
JOIN goat_identifiers gi
  ON gi.tenant_id = '00000000-0000-4000-8000-000000000001'
 AND gi.identifier_type = 'animal_identifier_1'
 AND gi.status = 'active' AND gi.valid_to IS NULL
 AND upper(gi.identifier_value) LIKE pfx.tag_prefix || '%'
JOIN goats g ON g.tenant_id = gi.tenant_id AND g.goat_id = gi.goat_id
-- Only animals still ON the farm are attached; one that already left is counted by the prior
-- outcomes below, and attaching it here as well would double it.
WHERE g.lifecycle_status = 'alive'
ON CONFLICT (tenant_id, load_id, goat_id) DO UPDATE
SET animal_identifier_1 = EXCLUDED.animal_identifier_1,
    animal_identifier_2 = EXCLUDED.animal_identifier_2;

-- The SURVIVORS of loads 100 and 101 (maintainer decision 2026-08-31).
--
-- Both loads were tagged to the SAME pen (CPT Mandela 1 - Part 1), so neither can claim it by the
-- tag rule above and both were reporting their survivors as Unaccounted: 1 on load 100 and 3 on
-- load 101, which is exactly the current_count the legacy records carry for each. The maintainer
-- directed that any four animals in that pen carry those survivors, so the register agrees with
-- the record.
--
-- Picked DETERMINISTICALLY by RFID order, not at random: a random pick would attach different
-- animals on every run, so a re-seed would silently reshuffle which animal belongs to which load
-- and no two environments would agree. Lowest RFID goes to load 100, the next three to load 101.
--
-- These are the FIRST load animals carrying real RFIDs; every other attached animal is
-- placeholder-tagged. The identifiers are snapshotted onto the membership row like any other.
INSERT INTO procurement_load_goats (
    tenant_id, load_id, goat_id, selection_state, current_state, intake_accepted_at,
    animal_identifier_1, animal_identifier_2
)
SELECT g.tenant_id, li.load_id, g.goat_id, 'accepted_herd_intake', 'accepted_herd_intake',
       ll.purchase_date::timestamptz, ranked.rfid,
       (SELECT g2.identifier_value FROM goat_identifiers g2
         WHERE g2.tenant_id = g.tenant_id AND g2.goat_id = g.goat_id
           AND g2.identifier_type = 'animal_identifier_2'
           AND g2.status = 'active' AND g2.valid_to IS NULL
         ORDER BY g2.is_primary_for_goat DESC, g2.valid_from DESC LIMIT 1)
FROM (
    SELECT g.goat_id, gi.identifier_value AS rfid,
           row_number() OVER (ORDER BY gi.identifier_value) AS rn
    FROM goats g
    JOIN locations sh ON sh.location_id = g.shed_id
    JOIN locations pk ON pk.location_id = sh.parent_location_id
    JOIN goat_shed_partitions gsp ON gsp.goat_id = g.goat_id AND gsp.tenant_id = g.tenant_id
    JOIN goat_identifiers gi ON gi.goat_id = g.goat_id
      AND gi.identifier_type = 'animal_identifier_1'
      AND gi.status = 'active' AND gi.valid_to IS NULL
    WHERE g.tenant_id = '00000000-0000-4000-8000-000000000001'
      AND g.lifecycle_status = 'alive'
      AND upper(pk.location_code) = 'CPT' AND sh.name = 'Mandela 1'
      AND gsp.partition_label = 'Part 1'
      -- Never take an animal another load already claims by its tag.
      AND NOT EXISTS (SELECT 1 FROM procurement_load_goats x WHERE x.goat_id = g.goat_id)
) ranked
JOIN (VALUES ('100', 1, 1), ('101', 2, 4)) AS want(load_ref, first_rn, last_rn)
  ON ranked.rn BETWEEN want.first_rn AND want.last_rn
JOIN legacy_load_ids li ON li.load_ref = want.load_ref
JOIN legacy_loads ll ON ll.load_ref = want.load_ref
JOIN goats g ON g.goat_id = ranked.goat_id
ON CONFLICT (tenant_id, load_id, goat_id) DO UPDATE
SET animal_identifier_1 = EXCLUDED.animal_identifier_1,
    animal_identifier_2 = EXCLUDED.animal_identifier_2;

-- Prior outcomes: what already happened before these animals were tracked here, with dates.
INSERT INTO procurement_load_prior_outcomes
    (tenant_id, load_id, outcome, animal_count, sales_value, first_on, last_on, source_ref)
SELECT '00000000-0000-4000-8000-000000000001'::uuid, li.load_id,
       v.outcome, v.animal_count, v.sales_value, v.first_on, v.last_on,
       CASE WHEN v.load_ref = '126' AND v.outcome = 'died'
            -- Load 126: the legacy extract records THREE deaths and 64 still on farm, but the
            -- register holds 63. The maintainer confirmed on 2026-08-31 that the missing animal
            -- had already died, so the fourth death is recorded on their word, not from the
            -- extract, and it carries no date of its own -- the range below is the three the
            -- extract dated. Kept separable so a later source can correct it.
            THEN 'legacy records 2026-08-31 + maintainer-confirmed 4th death (undated)'
            ELSE 'legacy records 2026-08-31 (loadwise_summary / salesDB / load status)'
       END
FROM (VALUES
    ('100', 'sold', 69, 1226428::numeric, DATE '2026-04-30', DATE '2026-08-17'),
    ('100', 'died',  6, NULL::numeric,    DATE '2025-11-24', DATE '2025-11-24'),
    ('101', 'sold', 66, 1118399::numeric, DATE '2026-05-02', DATE '2026-08-17'),
    ('101', 'died',  1, NULL::numeric,    DATE '2025-11-24', DATE '2025-11-24'),
    ('113', 'sold', 91, 1246533::numeric, DATE '2026-05-13', DATE '2026-05-20'),
    ('113', 'died',  9, NULL::numeric,    DATE '2025-11-24', DATE '2026-03-08'),
    ('126', 'died',  4, NULL::numeric,    DATE '2026-05-12', DATE '2026-07-18'),
    ('128', 'died',  4, NULL::numeric,    DATE '2026-06-29', DATE '2026-07-15'),
    ('129', 'died',  1, NULL::numeric,    DATE '2026-06-02', DATE '2026-06-02'),
    ('130', 'died',  1, NULL::numeric,    DATE '2026-07-22', DATE '2026-07-22')
) AS v(load_ref, outcome, animal_count, sales_value, first_on, last_on)
JOIN legacy_load_ids li ON li.load_ref = v.load_ref
ON CONFLICT (tenant_id, load_id, outcome) DO UPDATE
SET animal_count = EXCLUDED.animal_count,
    sales_value = EXCLUDED.sales_value,
    first_on = EXCLUDED.first_on,
    last_on = EXCLUDED.last_on,
    source_ref = EXCLUDED.source_ref;

-- VERIFY before COMMIT. Expected: 8 loads; members 126=63, 128=63, 129=77, 130=74, 131=63,
-- 100/101/113=0; every load carrying its sheet cost.
--
-- projection-review: membership=procurement_loads seeded by this script, keyed by its own
-- idempotency_key prefix; group_key=pl.load_id (its primary key) carried in the GROUP BY beside
-- the display columns, so one row per load; join_cardinality=parties 1:1 on party_id (a load has
-- exactly one source party), procurement_load_goats one-to-MANY and therefore counted rather
-- than multiplied, and the prior-outcome count is a correlated subquery so the many side never
-- fans the row out; pagination=none, this is a one-time seed verification over the 8 rows just
-- written, never a served read; scope=tenant_id fixed to the STG tenant in every statement above
-- and the idempotency_key prefix confines it to this seed.
SELECT pl.context->>'load_ref' AS load_ref, p.display_name AS vendor, pl.purchase_date,
       pl.expected_count, pl.animal_cost,
       count(plg.goat_id) AS members,
       count(plg.goat_id) FILTER (
           WHERE regexp_replace(lower(plg.animal_identifier_1), '[^0-9]', '', 'g') = lower(plg.animal_identifier_1)
             AND length(plg.animal_identifier_1) BETWEEN 12 AND 16
       ) AS members_with_real_rfid,
       (SELECT count(*) FROM procurement_load_prior_outcomes po WHERE po.load_id = pl.load_id) AS prior_rows
FROM procurement_loads pl
JOIN parties p ON p.party_id = pl.source_party_id
LEFT JOIN procurement_load_goats plg ON plg.load_id = pl.load_id
WHERE pl.idempotency_key LIKE 'legacy-load-%'
GROUP BY 1, 2, 3, 4, 5, pl.load_id
ORDER BY 1;

COMMIT;
