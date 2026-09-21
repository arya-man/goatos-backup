-- +goose Up

-- seed-fixture-guard:ignore: sales-only line-weight estimate columns and row repair; no vaccination or HRMS source contract moves.
-- SEVEN OLD SALES WITH NO USABLE WEIGHT (maintainer decision 2026-09-21).
--
-- The Sold page's weight bands counted 151 of 679 sold animals, because only an animal TAGGED to
-- its sale carries a weight. The other 528 are covered by their sale's own recorded weight -- a
-- load total the desk entered -- which bands the whole line at its average. Seven animals across
-- seven rows could not be covered either way, and the farm was asked in the group: four rows have
-- no weight at all, and three carry a number that cannot be a weight. The answer was to complete
-- them with minimal assumptions rather than leave a hole, so this migration records the
-- assumption AS an assumption, beside the recorded figure, never on top of it.
--
--   sales id 28   11 Mar 2026  CBE  42 Malai goats   Rs 4,20,000  recorded weight 35 kg
--       35 kg cannot be 42 goats: as a total it implies Rs 12,000/kg, as an average Rs 286/kg,
--       which is below every CBE goat deal in the ledger. Rs 4,20,000 at the nearest CBE goat
--       rate in time (sales id 24, 28 Feb 2026, Rs 400/kg) is 1,050 kg -- 25.0 kg an animal. The
--       band is 20-35 anywhere in the Rs 400-450 range the farm was actually getting, so the
--       assumed rate moves the kilograms and not the answer.
--
--   sales id 33 + 34  1 Apr 2026  CBE  3 Malai males  Rs 41,250 (40,250 + a 1,000 advance, one
--       payment -- both rows carry the same comment). Both rows record a weight of 350, which is
--       350 kg for two goats and 350 kg for one. Read as the RATE typed into the weight column,
--       Rs 41,250 / 350 = 117.86 kg, 39.3 kg an animal -- the same size as sales id 24's Malai
--       males (39.2 kg). Split by head: 78.57 kg for the two, 39.29 kg for the one.
--
--   sales ids 20, 21, 22, 23  Jan-Feb 2026  CBE  7 Sojat goats, Rs 1,000 to Rs 4,500 an animal.
--       No weight was ever recorded, and the price is far below a live-weight sale: divided by
--       the farm's own rate these would be 2.5-11 kg, so they were sold by the head (kids or
--       culls), not by the kilogram. There is no honest kilogram to write, and every reading of
--       them lands in the same band, so they carry a BAND and no weight.
--
-- THREE RULES THIS ENCODES, and each is the reason for a column rather than an UPDATE:
--
-- 1. AN ESTIMATE NEVER BECOMES A RECORDING. estimated_weight_kg sits BESIDE total_weight_kg and
--    the reader chooses; nothing overwrites what the desk wrote.
--
-- 2. A NUMBER THAT IS NOT A WEIGHT IS NOT A WEIGHT. The 35 and the two 350s are cleared, because
--    leaving them would keep feeding 735 phantom kilograms into realized price per kg -- the one
--    place a wrong weight does real damage. They are preserved verbatim in the estimate basis, so
--    nothing is lost.
--
-- 3. AN ESTIMATE DERIVED FROM PRICE MAY NEVER RE-ENTER PRICE. Sales id 28's 1,050 kg was computed
--    as revenue / Rs 400 per kg; feeding it back into realized price per kg would just re-assert
--    the rate it was derived from. Clearing the recorded weight is exactly what keeps it out --
--    the price bands already skip a line with no weight -- and the estimate is read only by the
--    weight bands, which is the question it was made to answer.
--
-- The band-only case is a SEPARATE column rather than a nominal kilogram for the same reason: a
-- 10 kg goat nobody weighed would be a fact, while "below 20 kg" is what is actually known.
--
-- Every write below is matched on the sheet's own source_sales_id, the farm, the date AND the
-- figure being replaced, so it touches exactly the rows described and re-running it is a no-op.

ALTER TABLE public.sales_deal_lines
    ADD COLUMN estimated_weight_kg numeric,
    ADD COLUMN estimated_weight_band text,
    ADD COLUMN weight_estimate_basis text;

ALTER TABLE public.sales_deal_lines
    ADD CONSTRAINT sales_deal_lines_estimated_weight_nonneg
        CHECK (estimated_weight_kg IS NULL OR estimated_weight_kg > 0),
    -- The four bands are the maintainer's own edges (2026-09-08), named here so a typo cannot
    -- create a fifth bucket that no surface renders.
    ADD CONSTRAINT sales_deal_lines_estimated_band_check
        CHECK (estimated_weight_band IS NULL OR estimated_weight_band IN
            ('under_20', 'from_20_to_35', 'from_35_to_40', 'at_or_above_40')),
    -- A line is estimated by KILOGRAMS or by BAND, never both: two answers to one question is a
    -- reader picking whichever it happens to check first.
    ADD CONSTRAINT sales_deal_lines_estimate_is_one_kind
        CHECK (estimated_weight_kg IS NULL OR estimated_weight_band IS NULL),
    -- An estimate with no stated basis is a number nobody can audit.
    ADD CONSTRAINT sales_deal_lines_estimate_has_basis
        CHECK ((estimated_weight_kg IS NULL AND estimated_weight_band IS NULL)
               = (btrim(coalesce(weight_estimate_basis, '')) = ''));

-- Sales id 28: 42 Malai goats. Clear the impossible 35 and record the price-derived estimate.
UPDATE public.sales_deal_lines l
SET total_weight_kg = NULL,
    estimated_weight_kg = 1050,
    weight_estimate_basis = 'Recorded weight 35 kg is impossible for 42 goats. Estimated from '
        || 'Rs 4,20,000 at Rs 400/kg, the nearest CBE goat rate in the ledger (sales id 24, '
        || '28 Feb 2026): 1,050 kg, 25.0 kg an animal.'
FROM public.sales_deals d
WHERE d.id = l.deal_id AND d.tenant_id = l.tenant_id
  AND d.source_sales_id = 28 AND d.farm = 'CBE' AND d.sale_date = DATE '2026-03-11'
  AND l.product_type = 'Goat' AND l.total_weight_kg = 35;

-- Sales id 33: two Malai males of the same 3-animal payment.
UPDATE public.sales_deal_lines l
SET total_weight_kg = NULL,
    estimated_weight_kg = 78.57,
    weight_estimate_basis = 'Recorded weight 350 read as the RATE (Rs 350/kg) entered in the '
        || 'weight column: sales ids 33 and 34 are one payment of Rs 41,250, which is 117.86 kg '
        || 'for three Malai males, 39.3 kg each. This line carries two of them.'
FROM public.sales_deals d
WHERE d.id = l.deal_id AND d.tenant_id = l.tenant_id
  AND d.source_sales_id = 33 AND d.farm = 'CBE' AND d.sale_date = DATE '2026-04-01'
  AND l.product_type = 'Goat' AND l.total_weight_kg = 350;

-- Sales id 34: the third male of that payment.
UPDATE public.sales_deal_lines l
SET total_weight_kg = NULL,
    estimated_weight_kg = 39.29,
    weight_estimate_basis = 'Recorded weight 350 read as the RATE (Rs 350/kg) entered in the '
        || 'weight column: sales ids 33 and 34 are one payment of Rs 41,250, which is 117.86 kg '
        || 'for three Malai males, 39.3 kg each. This line carries one of them.'
FROM public.sales_deals d
WHERE d.id = l.deal_id AND d.tenant_id = l.tenant_id
  AND d.source_sales_id = 34 AND d.farm = 'CBE' AND d.sale_date = DATE '2026-04-01'
  AND l.product_type = 'Goat' AND l.total_weight_kg = 350;

-- Sales ids 20-23: seven Sojat goats sold by the head, banded and not weighed.
UPDATE public.sales_deal_lines l
SET estimated_weight_band = 'under_20',
    weight_estimate_basis = 'No weight recorded. Sold by the head at Rs 1,000-4,500 an animal, '
        || 'which against the farm''s own Rs 400-450/kg is 2.5-11 kg -- kids or culls, not a '
        || 'live-weight sale. Banded below 20 kg; no kilogram is claimed.'
FROM public.sales_deals d
WHERE d.id = l.deal_id AND d.tenant_id = l.tenant_id
  AND d.source_sales_id IN (20, 21, 22, 23) AND d.farm = 'CBE'
  AND d.sale_date BETWEEN DATE '2026-01-22' AND DATE '2026-02-17'
  AND l.product_type = 'Goat' AND l.total_weight_kg IS NULL
  AND l.estimated_weight_kg IS NULL AND l.estimated_weight_band IS NULL;

-- The deal's weight is a rollup of its lines (000296), so clearing a line clears the deal's too.
-- Recomputed from the lines rather than written by hand, so the two cannot disagree about the
-- same sale, and NULL when no line carries a weight -- which is what "no weight recorded" is.
UPDATE public.sales_deals d
SET total_weight_kg = (
        SELECT sum(l.total_weight_kg)
        FROM public.sales_deal_lines l
        WHERE l.tenant_id = d.tenant_id AND l.deal_id = d.id
    )
WHERE EXISTS (
    SELECT 1 FROM public.sales_deal_lines l
    WHERE l.tenant_id = d.tenant_id AND l.deal_id = d.id
      AND btrim(coalesce(l.weight_estimate_basis, '')) <> ''
);

-- +goose Down

ALTER TABLE public.sales_deal_lines
    DROP CONSTRAINT IF EXISTS sales_deal_lines_estimate_has_basis,
    DROP CONSTRAINT IF EXISTS sales_deal_lines_estimate_is_one_kind,
    DROP CONSTRAINT IF EXISTS sales_deal_lines_estimated_band_check,
    DROP CONSTRAINT IF EXISTS sales_deal_lines_estimated_weight_nonneg;

-- Restore the figures the Up cleared, so Down leaves the ledger exactly as it found it.
UPDATE public.sales_deal_lines l
SET total_weight_kg = 35
FROM public.sales_deals d
WHERE d.id = l.deal_id AND d.tenant_id = l.tenant_id
  AND d.source_sales_id = 28 AND d.farm = 'CBE' AND d.sale_date = DATE '2026-03-11'
  AND l.product_type = 'Goat' AND l.total_weight_kg IS NULL AND l.estimated_weight_kg = 1050;

UPDATE public.sales_deal_lines l
SET total_weight_kg = 350
FROM public.sales_deals d
WHERE d.id = l.deal_id AND d.tenant_id = l.tenant_id
  AND d.source_sales_id IN (33, 34) AND d.farm = 'CBE' AND d.sale_date = DATE '2026-04-01'
  AND l.product_type = 'Goat' AND l.total_weight_kg IS NULL
  AND l.estimated_weight_kg IN (78.57, 39.29);

-- And put the deal rollups back, from the restored lines.
UPDATE public.sales_deals d
SET total_weight_kg = (
        SELECT sum(l.total_weight_kg)
        FROM public.sales_deal_lines l
        WHERE l.tenant_id = d.tenant_id AND l.deal_id = d.id
    )
WHERE EXISTS (
    SELECT 1 FROM public.sales_deal_lines l
    WHERE l.tenant_id = d.tenant_id AND l.deal_id = d.id
      AND btrim(coalesce(l.weight_estimate_basis, '')) <> ''
);

ALTER TABLE public.sales_deal_lines
    DROP COLUMN weight_estimate_basis,
    DROP COLUMN estimated_weight_band,
    DROP COLUMN estimated_weight_kg;
