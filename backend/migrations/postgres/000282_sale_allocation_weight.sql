-- +goose Up
-- seed-fixture-guard:ignore: additive, nullable column on the sale allocation snapshot; no
-- vaccination/HRMS seed contract change.
--
-- WEIGHT AT TAGGING (maintainer decision 2026-09-08).
--
-- When animals are tagged to a sale the operator now records each one's live weight, on the web
-- drawer and the phone alike, and the Sales page shows how many sold animals fell in each weight
-- band (under 20 kg, 20 to 35, 35 to 40, 40 and above). The weight is a fact about THIS
-- allocation -- what the animal weighed the day it left on this sale -- so it lives on the
-- allocation snapshot beside the pen and the tag it was recorded with, never on the goat.
--
-- It is REQUIRED for every animal on a NEW confirm (maintainer answer, same day); rows tagged
-- before this column existed have no weight and stay NULL. The Sales card reports those
-- separately as "no weight recorded" rather than filing them into a band they were never
-- measured for. This is deliberately NOT the Weighing module's data: Weighing is isolated and
-- knows only a scanned string; a sale weight is entered by the sales desk on the allocation.
ALTER TABLE public.goat_sale_allocations
  ADD COLUMN IF NOT EXISTS weight_kg numeric(7, 2);

ALTER TABLE public.goat_sale_allocations
  DROP CONSTRAINT IF EXISTS goat_sale_allocations_weight_check,
  ADD CONSTRAINT goat_sale_allocations_weight_check
    CHECK (weight_kg IS NULL OR weight_kg > 0);

COMMENT ON COLUMN public.goat_sale_allocations.weight_kg IS
  'Live weight (kg) recorded when the animal was tagged to this sale. NULL only on rows tagged before 000282.';

-- +goose Down
ALTER TABLE public.goat_sale_allocations
  DROP CONSTRAINT IF EXISTS goat_sale_allocations_weight_check,
  DROP COLUMN IF EXISTS weight_kg;
