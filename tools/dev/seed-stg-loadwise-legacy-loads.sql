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
    animal_cost numeric(14, 2) NOT NULL
) ON COMMIT DROP;

INSERT INTO legacy_loads VALUES
    ('100', 'Green Fresh Farm',         DATE '2025-10-23',  76, 539000),
    ('101', 'Dr Praneeth',              DATE '2025-10-21',  70, 572000),
    ('113', 'Nutriplus Foods Pvt Ltd.', DATE '2025-11-13', 100, 980000),
    ('126', 'Ramesh Reddy',             DATE '2026-05-11',  67, 670000),
    ('128', 'Krishnamorrthy',           DATE '2026-05-26',  70, 580000),
    ('129', 'Krishnamorrthy',           DATE '2026-06-01',  78, 675100),
    ('130', 'Green Fresh Farm',         DATE '2026-06-12',  77, 669465),
    ('131', 'Krishnamorrthy',           DATE '2026-06-22',  63, 502000);

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
       jsonb_build_object('load_ref', ll.load_ref),
       'legacy-load-' || ll.load_ref,
       ll.animal_cost, now()
FROM legacy_loads ll
ON CONFLICT (idempotency_key) DO NOTHING;

CREATE TEMP TABLE legacy_load_ids ON COMMIT DROP AS
SELECT pl.context->>'load_ref' AS load_ref, pl.load_id
FROM procurement_loads pl
WHERE pl.tenant_id = '00000000-0000-4000-8000-000000000001'
  AND pl.idempotency_key LIKE 'legacy-load-%';

-- Membership: today's ALIVE residents of each load's tagged shed/partition (verified against
-- goat_shed_partitions on 2026-08-31: CPT Godel 2 Part 1+2 = 77 for load 129; CPT Castro 1+2 =
-- 63 for load 131; CBE Castro 1/2/3 for 126/130/128).
CREATE TEMP TABLE legacy_load_pens (load_ref text, park_code text, shed_name text, partition_label text) ON COMMIT DROP;
INSERT INTO legacy_load_pens VALUES
    ('126', 'CBE', 'Castro',  '1'),
    ('128', 'CBE', 'Castro',  '3'),
    ('130', 'CBE', 'Castro',  '2'),
    ('131', 'CPT', 'Castro',  '1'),
    ('131', 'CPT', 'Castro',  '2'),
    ('129', 'CPT', 'Godel 2', 'Part 1'),
    ('129', 'CPT', 'Godel 2', 'Part 2');

INSERT INTO procurement_load_goats (
    tenant_id, load_id, goat_id, selection_state, current_state, intake_accepted_at
)
SELECT g.tenant_id, li.load_id, g.goat_id, 'accepted_herd_intake', 'accepted_herd_intake',
       ll.purchase_date::timestamptz
FROM legacy_load_pens pen
JOIN legacy_load_ids li ON li.load_ref = pen.load_ref
JOIN legacy_loads ll ON ll.load_ref = pen.load_ref
JOIN locations pk ON pk.tenant_id = '00000000-0000-4000-8000-000000000001'
  AND pk.location_type = 'park' AND upper(pk.location_code) = pen.park_code
JOIN locations sh ON sh.parent_location_id = pk.location_id
  AND sh.location_type = 'shed' AND sh.name = pen.shed_name
JOIN goats g ON g.shed_id = sh.location_id AND g.lifecycle_status = 'alive'
JOIN goat_shed_partitions gsp ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id
  AND gsp.partition_label = pen.partition_label
ON CONFLICT (tenant_id, load_id, goat_id) DO NOTHING;

-- Prior outcomes: what already happened before these animals were tracked here, with dates.
INSERT INTO procurement_load_prior_outcomes
    (tenant_id, load_id, outcome, animal_count, sales_value, first_on, last_on, source_ref)
SELECT '00000000-0000-4000-8000-000000000001'::uuid, li.load_id,
       v.outcome, v.animal_count, v.sales_value, v.first_on, v.last_on,
       'legacy records 2026-08-31 (loadwise_summary / salesDB / load status)'
FROM (VALUES
    ('100', 'sold', 69, 1226428::numeric, DATE '2026-04-30', DATE '2026-08-17'),
    ('100', 'died',  6, NULL::numeric,    DATE '2025-11-24', DATE '2025-11-24'),
    ('101', 'sold', 66, 1118399::numeric, DATE '2026-05-02', DATE '2026-08-17'),
    ('101', 'died',  1, NULL::numeric,    DATE '2025-11-24', DATE '2025-11-24'),
    ('113', 'sold', 91, 1246533::numeric, DATE '2026-05-13', DATE '2026-05-20'),
    ('113', 'died',  9, NULL::numeric,    DATE '2025-11-24', DATE '2026-03-08'),
    ('126', 'died',  3, NULL::numeric,    DATE '2026-05-12', DATE '2026-07-18'),
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
SELECT pl.context->>'load_ref' AS load_ref, p.display_name AS vendor, pl.purchase_date,
       pl.expected_count, pl.animal_cost,
       count(plg.goat_id) AS members,
       (SELECT count(*) FROM procurement_load_prior_outcomes po WHERE po.load_id = pl.load_id) AS prior_rows
FROM procurement_loads pl
JOIN parties p ON p.party_id = pl.source_party_id
LEFT JOIN procurement_load_goats plg ON plg.load_id = pl.load_id
WHERE pl.idempotency_key LIKE 'legacy-load-%'
GROUP BY 1, 2, 3, 4, 5, pl.load_id
ORDER BY 1;

COMMIT;
