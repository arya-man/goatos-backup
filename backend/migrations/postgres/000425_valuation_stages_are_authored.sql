-- +goose Up
--
-- THE STAGES THE HERD IS VALUED IN ARE AUTHORED (maintainer instruction 2026-09-24: "even new
-- stage should also be configurable don't hardcode ... we have warmup with animals i need to add
-- them to do it i should not change code it should also be configurable in sales config once i add
-- it and define price, farm values should change and card should come").
--
-- The valuation knew six stages because six stages were written into a SQL CASE. The farm's stage
-- register carries nineteen. On the day this was written 58 live kids stood in Warmup -- a stage
-- the register has always had -- and were valued at nothing, not by any decision but because there
-- was no way to tell the software the stage existed. Adding it was a deploy.
--
-- A valuation stage is now a row the farm writes: what it is called, which entries of its own stage
-- register it covers, and -- through its two bucket rows -- what a female and a male in it are
-- carried at.
--
-- NOTHING MOVES TODAY. The six rows seeded here are the retired CASE, written out: the same
-- register entries filed into the same buckets, so every farm values its herd this afternoon at
-- exactly what it valued this morning. What changes is that Warmup can now be added on the screen.
--
-- TWO RULES OF THE CASE ARE NOT REPRODUCED, and both are deliberate.
--
--   age_band = 'adult' was a catch-all for an adult stage nobody had named. It is replaced by
--   NAMING the adult stages, because the catch-all is precisely what made Warmup's opposite number
--   invisible: a stage swallowed by a band is valued at a figure nobody chose for it. A stage the
--   farm adds tomorrow now stands in the not-valued list with its own name and head count, asking
--   to be priced. Do not restore a fallback bucket.
--
--   Mother was forced onto the female row whatever the sex column said. Every Mother on this farm
--   is recorded female, so dropping it moves nothing; a Mother the register calls male is now a
--   register error that shows as one, rather than being quietly corrected inside a valuation.
--
-- A register entry belongs to ONE valuation stage: overlapping matches are refused by the write
-- path rather than resolved by display order, because an animal valued by whichever row sorts
-- first is a farm value that changes when someone reorders the screen.

ALTER TABLE public.sales_valuation_assumptions
	ADD COLUMN IF NOT EXISTS stages jsonb NOT NULL DEFAULT '[]'::jsonb;

-- The retired CASE, written out as rows. Matches are the register's own spellings; the read
-- normalizes both sides (upper, strip non-alphanumerics) exactly as the CASE did, because this
-- herd genuinely carries both 'ICU- kid' and 'ICU-Kid'.
UPDATE public.sales_valuation_assumptions
SET stages = '[
	{"stage": "fattening", "label": "Fattening", "display_order": 1,
	 "matches": ["F2", "F2-Male", "F2-Female"]},
	{"stage": "adult", "label": "Adult", "display_order": 2,
	 "matches": ["Buck", "Mother", "Milking", "M0", "Pregnant", "Non-Pregnant", "ICU"]},
	{"stage": "K0", "label": "K0", "display_order": 3, "matches": ["K0"]},
	{"stage": "K1", "label": "K1", "display_order": 4, "matches": ["K1"]},
	{"stage": "K2", "label": "K2", "display_order": 5, "matches": ["K2", "ICU-Kid"]},
	{"stage": "K3", "label": "K3", "display_order": 6, "matches": ["K3"]}
]'::jsonb
WHERE stages = '[]'::jsonb OR jsonb_array_length(stages) = 0;

ALTER TABLE public.sales_valuation_assumptions
	ADD CONSTRAINT sales_valuation_assumptions_stages_present
	CHECK (jsonb_typeof(stages) = 'array' AND jsonb_array_length(stages) >= 1);

COMMENT ON COLUMN public.sales_valuation_assumptions.stages IS
	'Authored valuation stages (2026-09-24): [{stage,label,display_order,matches[]}]. matches are management-stage / milk-cohort values as animal_stage_lookup spells them; buckets carries two rows per stage, keyed <stage>_female and <stage>_male.';

-- +goose Down
ALTER TABLE public.sales_valuation_assumptions
	DROP CONSTRAINT IF EXISTS sales_valuation_assumptions_stages_present;
ALTER TABLE public.sales_valuation_assumptions DROP COLUMN IF EXISTS stages;
