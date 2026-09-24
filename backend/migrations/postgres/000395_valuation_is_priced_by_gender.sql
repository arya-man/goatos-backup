-- +goose Up
--
-- THE HERD IS VALUED BY GENDER (maintainer instruction 2026-09-23: "make this gender wise, we have
-- different price at different genders").
--
-- The adults always were priced apart -- a buck is not worth what a doe is, and the screen carried
-- 60 kg at 500 against 40 kg at 600. Everything else was one row per stage: one fattening rate for
-- both genders, one K0 rate, one K1. So the farm could say a buck differs from a doe but not that
-- a buck kid differs from a doe kid, which is the same fact one year earlier.
--
-- Every stage is now a row per gender: six stages, two genders, twelve rows.
--
-- NOTHING THE FARM ALREADY VALUES MOVES. Each stage's authored figures are copied into BOTH of its
-- new rows, so the herd values today at exactly what it valued yesterday and the farm then edits
-- the halves that really differ. The adults keep their own separate figures, which is the whole
-- point: they were already the split everything else is getting.
--
-- adult_male_buck becomes adult_male. The old key spelled the animal ("bucks") where every other
-- key spells the stage and the gender; carrying one odd name into a set built from stage + gender
-- would leave the join key unguessable. The LABEL still says whatever the farm typed.
UPDATE public.sales_valuation_assumptions a
SET buckets = split.buckets,
    row_version = a.row_version + 1,
    updated_at = now()
FROM (
    SELECT
        a.tenant_id,
        jsonb_agg(row.value ORDER BY row.ord) AS buckets
    FROM public.sales_valuation_assumptions a
    CROSS JOIN LATERAL (
        SELECT b.bucket, b.label, b.fixed_weight_kg, b.price_per_kg
        FROM jsonb_to_recordset(a.buckets)
            AS b(bucket text, label text, fixed_weight_kg float8, price_per_kg float8)
    ) old
    CROSS JOIN LATERAL (
        -- The stage this stored row is about, and the gender it already named (the adults) or
        -- NULL for the rows that named none and therefore become two.
        SELECT
            CASE old.bucket
                WHEN 'adult_female' THEN 'adult'
                WHEN 'adult_male_buck' THEN 'adult'
                ELSE old.bucket
            END AS stage,
            CASE old.bucket
                WHEN 'adult_female' THEN 'female'
                WHEN 'adult_male_buck' THEN 'male'
                ELSE NULL
            END AS gender
    ) m
    CROSS JOIN LATERAL (
        -- One row out for a stage that already named a gender; two for one that did not.
        SELECT g.gender FROM (VALUES ('female'), ('male')) g(gender)
        WHERE m.gender IS NULL OR g.gender = m.gender
    ) g
    CROSS JOIN LATERAL (
        SELECT
            -- The order the screen lists them in: stage first, female before male.
            (CASE m.stage WHEN 'fattening' THEN 1 WHEN 'adult' THEN 2 WHEN 'K0' THEN 3
                          WHEN 'K1' THEN 4 WHEN 'K2' THEN 5 WHEN 'K3' THEN 6 ELSE 9 END) * 2
              + (CASE g.gender WHEN 'female' THEN 0 ELSE 1 END) AS ord,
            jsonb_strip_nulls(jsonb_build_object(
                'bucket', m.stage || '_' || g.gender,
                -- A row that already named its gender keeps the words the farm typed; one being
                -- split gets the stage's own label with the gender appended, so the two halves are
                -- told apart on screen without anybody renaming them.
                'label', CASE WHEN m.gender IS NOT NULL THEN old.label
                              ELSE old.label || ' · ' || initcap(g.gender) END,
                'fixed_weight_kg', old.fixed_weight_kg,
                'price_per_kg', old.price_per_kg,
                'display_order', (CASE m.stage WHEN 'fattening' THEN 1 WHEN 'adult' THEN 2 WHEN 'K0' THEN 3
                                               WHEN 'K1' THEN 4 WHEN 'K2' THEN 5 WHEN 'K3' THEN 6 ELSE 9 END) * 2
                                 + (CASE g.gender WHEN 'female' THEN 0 ELSE 1 END)
            )) AS value
    ) row
    GROUP BY a.tenant_id
) split
WHERE split.tenant_id = a.tenant_id;

COMMENT ON COLUMN public.sales_valuation_assumptions.buckets IS
  'One row per STAGE and GENDER (twelve: fattening/adult/K0..K3 x female/male), each with its own weight and price per kg. An animal whose gender is not recorded is valued on the female row.';

-- +goose Down
--
-- Back to one row per stage. The FEMALE row is the one kept for a stage that was split, because it
-- is the one the herd's larger share is valued on -- and the adults go back to their own two rows,
-- which they never stopped having. A price the farm authored for a MALE kid is lost, because the
-- older shape has nowhere to put it; that is what makes this the down of a widening change.
UPDATE public.sales_valuation_assumptions a
SET buckets = back.buckets,
    row_version = a.row_version + 1,
    updated_at = now()
FROM (
    SELECT a.tenant_id, jsonb_agg(row.value ORDER BY m.ord) AS buckets
    FROM public.sales_valuation_assumptions a
    CROSS JOIN LATERAL (
        SELECT b.bucket, b.label, b.fixed_weight_kg, b.price_per_kg
        FROM jsonb_to_recordset(a.buckets)
            AS b(bucket text, label text, fixed_weight_kg float8, price_per_kg float8)
    ) new
    CROSS JOIN LATERAL (
        SELECT
            CASE WHEN new.bucket = 'adult_female' THEN 'adult_female'
                 WHEN new.bucket = 'adult_male' THEN 'adult_male_buck'
                 ELSE regexp_replace(new.bucket, '_(female|male)$', '')
            END AS bucket,
            CASE new.bucket WHEN 'adult_female' THEN 2 WHEN 'adult_male' THEN 3
                 ELSE CASE regexp_replace(new.bucket, '_(female|male)$', '')
                        WHEN 'fattening' THEN 1 WHEN 'K0' THEN 4 WHEN 'K1' THEN 5
                        WHEN 'K2' THEN 6 WHEN 'K3' THEN 7 ELSE 9 END
            END AS ord
    ) m
    CROSS JOIN LATERAL (
        SELECT jsonb_strip_nulls(jsonb_build_object(
            'bucket', m.bucket,
            'label', regexp_replace(new.label, ' · (Female|Male)$', ''),
            'fixed_weight_kg', new.fixed_weight_kg,
            'price_per_kg', new.price_per_kg,
            'display_order', m.ord
        )) AS value
    ) row
    WHERE new.bucket IN ('adult_female', 'adult_male') OR new.bucket LIKE '%\_female'
    GROUP BY a.tenant_id
) back
WHERE back.tenant_id = a.tenant_id;
