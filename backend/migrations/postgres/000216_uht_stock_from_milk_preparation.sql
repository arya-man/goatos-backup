-- +goose Up
-- UHT MILK STOCK NOW DEPLETES FROM MILK PREPARATION (maintainer decision 2026-08-27).
--
-- Migration 000185 introduced feed_external_consumption for "sheet-tracked feeds GoatOS does not
-- direct" -- UHT Milk being the only one. That premise no longer holds: the Milk Preparation
-- workflow HAS the number. Its operator submits, per farm per day,
-- `answers->>'uht_milk_quantity_litres'`, and those values match the Feed DB sheet to the litre
-- (verified 2026-08-24..08-27 on both farms).
--
-- Hand-entering the same figure a second time into the ledger fell behind TWICE in one week --
-- most recently on 2026-08-27, when the stock card read 168 kg against a physical count of 141,
-- because that day's row had not been typed in yet. There is no second capture to keep in sync
-- once the read points at the workflow that already owns the fact.
--
-- DEPLETES ON SUBMIT, NOT ON VERIFY (maintainer decision 2026-08-27). Unlike feed distribution --
-- where the verifier's approve is the gate that COMPLETES the work -- the operator's entered
-- quantity IS the measurement here, and holding stock behind review would make every card lag by
-- the length of the verification queue. So `pending_verification` and `rework` count exactly like
-- `completed`; a rework re-shoots the PROOF, it does not un-feed the milk.
--
-- `retired` is the one excluded status, and that exclusion is load-bearing: by
-- milk_preparation_completions_active_farm_grain_check a live preparation is FARM-grain
-- (shed_id IS NULL) while retired rows are the legacy SHED-grain ones. Counting them would add a
-- park's sheds on top of that park's farm row and double-count the day.
--
-- The ledger is KEPT as the fallback, not dropped: it holds the pre-workflow history (Milk
-- Preparation starts 2026-08-24 on CPT and 2026-08-25 on CBE) and remains the seam for any future
-- sheet-tracked feed GoatOS genuinely does not capture. Precedence is per (tenant, park, day):
-- a day the workflow covers ignores the ledger row entirely, so the two sources can never both
-- contribute and double the day.
--
-- LITRES ARE READ AS KILOGRAMS 1:1. That is what the Feed DB sheet has always done -- it books the
-- submitted litre figure as kg and prices it at the per-kg load rate -- so applying UHT's ~1.03
-- kg/l density here would silently put GoatOS ~3% below the sheet on every milk day. Changing it
-- is a maintainer decision, not a rounding fix.

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

COMMENT ON VIEW public.feed_effective_external_consumption IS
'Consumption of feeds the ration grid does not direct, at (tenant, park, feed_item, feed_day). '
'UHT Milk resolves from the Milk Preparation workflow on SUBMIT (litres read as kg 1:1); the '
'feed_external_consumption ledger is the fallback for days the workflow does not cover. '
'Maintainer decision 2026-08-27; see migration 000216.';

-- +goose Down
DROP VIEW IF EXISTS public.feed_effective_external_consumption;
