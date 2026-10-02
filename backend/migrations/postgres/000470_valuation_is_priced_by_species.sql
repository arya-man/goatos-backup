-- +goose Up
--
-- THE HERD IS VALUED BY SPECIES AS WELL (maintainer decision 2026-10-02: "one price for both in
-- config, divide both ... stage will be same, divide between goats and sheep also").
--
-- A goat and a sheep in the same stage fetch different rupees per kg -- ₹450 against ₹430 for
-- fattening on the day this was written, the split the Weighing Assumptions prices already carry --
-- but Sales Config's Farm valuation had one row per stage and gender for both. Every stage is now a
-- row per SPECIES and gender: `<stage>_<species>_<gender>`, four rows where there were two. The
-- stages themselves stay one list for both species.
--
-- NOTHING THE FARM ALREADY VALUES MOVES. Each stored row's figures are copied into BOTH of its
-- species rows, so the herd values today at exactly what it valued yesterday, and the farm then
-- types the sheep figures that really differ.
--
-- Idempotent: a tenant whose buckets already carry a species is left alone.
--
-- projection-review: membership=public.sales_valuation_assumptions, ONE row per tenant (tenant_id
-- is its primary key), both the source and the target of this rewrite; group_key=a.tenant_id, the
-- key the outer UPDATE joins back on, so each tenant's rows are built from that tenant's own rows
-- and no other; join_cardinality=jsonb_array_elements over the row itself and a scalar two-row
-- VALUES list for the species -- no table is read, so each stored row becomes exactly two;
-- pagination=none, a whole-table one-shot migration; scope=every tenant, deliberately.
UPDATE public.sales_valuation_assumptions a
SET buckets = split.buckets,
    row_version = a.row_version + 1,
    updated_at = now()
FROM (
    SELECT o.tenant_id,
           jsonb_agg(o.value ORDER BY o.ord) AS buckets
    FROM (
        SELECT a.tenant_id,
               row_number() OVER (PARTITION BY a.tenant_id ORDER BY ceil(e.ord / 2.0), sp.ord, e.ord) AS ord,
               jsonb_strip_nulls(jsonb_build_object(
                   'bucket', regexp_replace(e.b->>'bucket', '_(female|male)$', '') || '_' || sp.key || '_' ||
                             substring(e.b->>'bucket' from '_(female|male)$'),
                   -- The card's words come from the STAGE's own label, exactly as a save composes them
                   -- (domain.BucketLabel): stored labels predate the stage list and may still read
                   -- "Adult females", which would print as "Adult females · Goat · Female".
                   'label', COALESCE(
                                (SELECT st->>'label' FROM jsonb_array_elements(a.stages) st
                                 WHERE st->>'stage' = regexp_replace(e.b->>'bucket', '_(female|male)$', '') LIMIT 1),
                                regexp_replace(e.b->>'label', ' · (Female|Male)$', '')
                            ) || ' · ' || sp.label || ' · ' || initcap(substring(e.b->>'bucket' from '_(female|male)$')),
                   'fixed_weight_kg', e.b->'fixed_weight_kg',
                   'price_per_kg', e.b->'price_per_kg'
               )) AS value
        FROM public.sales_valuation_assumptions a
        CROSS JOIN LATERAL jsonb_array_elements(a.buckets) WITH ORDINALITY AS e(b, ord)
        CROSS JOIN (VALUES ('goat', 'Goat', 1), ('sheep', 'Sheep', 2)) AS sp(key, label, ord)
        WHERE NOT EXISTS (
            SELECT 1 FROM jsonb_array_elements(a.buckets) z
            WHERE z->>'bucket' ~ '_(goat|sheep)_(female|male)$'
        )
    ) o
    GROUP BY o.tenant_id
) split
WHERE a.tenant_id = split.tenant_id;

-- display_order follows the new order (the ordinal the rows were aggregated in).
UPDATE public.sales_valuation_assumptions a
SET buckets = (
    SELECT jsonb_agg(e.b || jsonb_build_object('display_order', e.ord) ORDER BY e.ord)
    FROM jsonb_array_elements(a.buckets) WITH ORDINALITY AS e(b, ord)
)
WHERE EXISTS (
    SELECT 1 FROM jsonb_array_elements(a.buckets) z
    WHERE z->>'bucket' ~ '_(goat|sheep)_(female|male)$'
);

-- +goose Down
-- Collapse back to one row per stage and gender, keeping the GOAT figure (the one both species
-- carried before the split). A sheep figure typed since is lost on the way down, by design.
UPDATE public.sales_valuation_assumptions a
SET buckets = (
    SELECT jsonb_agg(
               jsonb_strip_nulls(jsonb_build_object(
                   'bucket', regexp_replace(e.b->>'bucket', '_goat_(female|male)$', '_\1'),
                   'label', regexp_replace(e.b->>'label', ' · Goat · ', ' · '),
                   'fixed_weight_kg', e.b->'fixed_weight_kg',
                   'price_per_kg', e.b->'price_per_kg',
                   'display_order', row_number_ord
               )) ORDER BY row_number_ord)
    FROM (
        SELECT b, row_number() OVER (ORDER BY ord) AS row_number_ord
        FROM jsonb_array_elements(a.buckets) WITH ORDINALITY AS x(b, ord)
        WHERE b->>'bucket' ~ '_goat_(female|male)$'
    ) e
),
    row_version = a.row_version + 1,
    updated_at = now()
WHERE EXISTS (
    SELECT 1 FROM jsonb_array_elements(a.buckets) z
    WHERE z->>'bucket' ~ '_goat_(female|male)$'
);
