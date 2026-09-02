-- +goose Up
-- UHT LEDGER FALLBACK IS SUPPRESSED ON BOTH OF A PREPARATION'S DATES.
--
-- Migration 000216 made UHT stock deplete from the Milk Preparation workflow on submit and kept
-- feed_external_consumption as the fallback for days the workflow does not cover. Its precedence
-- test was `p.feed_day = x.feed_day`, and the workflow side books at `feeding_date`
-- (= preparation_date + 1, milk_preparation_completions_date_check). A ledger row landing on the
-- PREPARATION date therefore matched nothing and was added on top of the same milk the workflow
-- had already counted a day later.
--
-- That was not hypothetical. The retired 2026-08-22 seam (countsapp.MilkPreparationUHTRecorder,
-- composed in eventwiring) wrote exactly such a row on every verifier APPROVE, keyed at
-- preparation_date. It only ever looked correct because a farm that prepares milk EVERY day has
-- yesterday's preparation covering today's ledger date by coincidence; the first skipped day, the
-- first day the module went live, or a retired preparation exposed the double deduction. That
-- writer is deleted in the same change as this migration.
--
-- The same shape bit the sheet handover: the legacy Feed DB sheet books UHT on the day it is
-- prepared, so on the crossover day the sheet's row and the workflow's row are the SAME milk one
-- day apart, and the old predicate counted both.
--
-- So the fallback is now suppressed when a preparation covers EITHER of its two dates for that
-- park. This is de-duplication only: it does not re-date the workflow side, which still books at
-- feeding_date exactly as 000216 decided. A ledger row survives only for a (park, day) no
-- preparation touches at all -- which is what "fallback for days the workflow does not cover"
-- always meant.
--
-- Everything else from 000216 is unchanged and still load-bearing: `retired` stays excluded
-- (retired rows are the legacy SHED-grain ones and would add a park's sheds on top of its farm
-- row), the accepted attempt is the CURRENT one, and litres are read as kilograms 1:1.

CREATE OR REPLACE VIEW public.feed_effective_external_consumption AS
WITH prep AS (
    SELECT c.tenant_id,
           c.park_id,
           c.preparation_date,
           c.feeding_date                                        AS feed_day,
           (a.answers ->> 'uht_milk_quantity_litres')::numeric(12,3) AS quantity_kg
    FROM public.milk_preparation_completions c
    JOIN public.milk_preparation_proof_attempts a
      ON a.tenant_id     = c.tenant_id
     AND a.completion_id = c.completion_id
     AND a.attempt_no    = c.current_attempt_no
    WHERE c.status <> 'retired'
      AND c.park_id IS NOT NULL
      AND a.answers ? 'uht_milk_quantity_litres'
      AND jsonb_typeof(a.answers -> 'uht_milk_quantity_litres') = 'number'
)
SELECT p.tenant_id,
       p.park_id,
       'UHT Milk'::text                        AS feed_item_label,
       public.feed_config_norm('UHT Milk')     AS feed_item_key,
       p.feed_day,
       p.quantity_kg
FROM prep p
UNION ALL
SELECT x.tenant_id,
       x.park_id,
       x.feed_item_label,
       x.feed_item_key,
       x.feed_day,
       x.quantity_kg
FROM public.feed_external_consumption x
WHERE NOT EXISTS (
    SELECT 1 FROM prep p
     WHERE p.tenant_id = x.tenant_id
       AND p.park_id   = x.park_id
       AND x.feed_day IN (p.feed_day, p.preparation_date)
       AND x.feed_item_key = public.feed_config_norm('UHT Milk')
);

COMMENT ON VIEW public.feed_effective_external_consumption IS
'Consumption of feeds the ration grid does not direct, at (tenant, park, feed_item, feed_day). '
'UHT Milk resolves from the Milk Preparation workflow on SUBMIT (litres read as kg 1:1, booked on '
'the feeding date); the feed_external_consumption ledger is the fallback for days no preparation '
'covers -- suppressed on BOTH the preparation and the feeding date of a covered day, so the same '
'milk cannot be counted twice one day apart. Maintainer decisions 2026-08-27 and 2026-09-02; see '
'migrations 000216 and 000241.';

-- +goose Down
CREATE OR REPLACE VIEW public.feed_effective_external_consumption AS
WITH prep AS (
    SELECT c.tenant_id,
           c.park_id,
           c.feeding_date                                        AS feed_day,
           (a.answers ->> 'uht_milk_quantity_litres')::numeric(12,3) AS quantity_kg
    FROM public.milk_preparation_completions c
    JOIN public.milk_preparation_proof_attempts a
      ON a.tenant_id     = c.tenant_id
     AND a.completion_id = c.completion_id
     AND a.attempt_no    = c.current_attempt_no
    WHERE c.status <> 'retired'
      AND c.park_id IS NOT NULL
      AND a.answers ? 'uht_milk_quantity_litres'
      AND jsonb_typeof(a.answers -> 'uht_milk_quantity_litres') = 'number'
)
SELECT p.tenant_id,
       p.park_id,
       'UHT Milk'::text                        AS feed_item_label,
       public.feed_config_norm('UHT Milk')     AS feed_item_key,
       p.feed_day,
       p.quantity_kg
FROM prep p
UNION ALL
SELECT x.tenant_id,
       x.park_id,
       x.feed_item_label,
       x.feed_item_key,
       x.feed_day,
       x.quantity_kg
FROM public.feed_external_consumption x
WHERE NOT EXISTS (
    SELECT 1 FROM prep p
     WHERE p.tenant_id = x.tenant_id
       AND p.park_id   = x.park_id
       AND p.feed_day  = x.feed_day
       AND x.feed_item_key = public.feed_config_norm('UHT Milk')
);
