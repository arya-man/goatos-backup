-- +goose Up
-- MAKING A LEAD CALLABLE, AND FINDABLE (maintainer report 2026-09-05).
--
-- THE NUMBER TO DIAL DID NOT EXIST. A buyer lead carried name, place, animal type, breed and a call
-- status; a farmer group carried name, crops, district, taluk and state. Neither carried a PHONE
-- NUMBER -- not in the table (000173) and not in the 2026-08-17 sheet the rows were imported from,
-- whose buyer columns are exactly recorded_date/farm/source_sales_id/buyer_name/buyer_place/
-- animal_type/breed/call_status. So a board built to record "did you reach them" never held the one
-- fact needed to reach them, and the only way to call a lead was to look the number up elsewhere.
--
-- phone_number is NULLABLE and free text. Nullable because all 261 imported rows have none and a NOT
-- NULL would either reject that history or invent numbers. Free text because these are dictated over
-- WhatsApp in whatever shape the caller wrote them down, sometimes two numbers separated by a slash
-- -- exactly the same reasoning procurement_vendors.phone_number carries (000156), and normalising
-- to digits here would destroy that. Unlike the vendor register, it is NOT part of any uniqueness
-- rule: two leads for one number is a real thing (a shop called twice, months apart) and refusing it
-- would lose a call record.
--
-- REACHING EVERY LEAD, NOT JUST THE NEWEST TWENTY (same report).
--
-- The pipeline drawers on /sales/config can create a lead and change a lead's call status, and have
-- been able to since 000173. What they could not do is FIND one. Both boards read the newest 20 rows
-- with no search and no paging, so of 208 buyer leads exactly 20 were reachable and 188 could never
-- have their status changed by anyone -- which is indistinguishable, from the desk, from the feature
-- not existing. The maintainer reported it as "there is no way to add one guy or change status".
--
-- The write path needed nothing. This adds the READ that makes it usable: a search column per lead
-- table, so the boards can offer "find the person you just called" instead of "hope they are in the
-- last twenty".
--
-- WHY A GENERATED COLUMN AND A TRIGRAM INDEX. The search a caller wants is an INFIX match -- they
-- type "narayan" and expect "S Narayanswamy", or the last digits of a place. That is a
-- non-SARGable LIKE against an ordinary btree and is exactly what make scale-guard blocks; AGENTS.md
-- names the approved fix, and 000156 already used it for the vendor register: a normalized column
-- plus a pg_trgm GIN index. This is the same shape, deliberately, so there is one pattern to know.
--
-- search_text is GENERATED, never maintained by application code, so it cannot drift from the
-- columns it summarises and cannot be forgotten on a write path added later. Both tables' writes
-- (000173) insert these columns directly, so every existing row and every future one is covered
-- with no backfill.
--
-- The buyer column folds NAME, PLACE, ANIMAL TYPE and BREED, because those are the four facts on a
-- buyer card and any of them is a plausible way to recall who you spoke to. The farmer-group column
-- folds NAME, DISTRICT, TALUK and STATE for the same reason. Both fold the PHONE NUMBER, so a caller
-- with a number on their screen and no memory of the name can still find the lead. call_status is
-- deliberately NOT folded
-- in: it is a filter of its own (the boards offer the status vocabulary as a facet), and mixing it
-- into free-text search would make a search for "answer" return every "No Answer" row.

ALTER TABLE public.sales_buyer_leads
    ADD COLUMN IF NOT EXISTS phone_number text;

ALTER TABLE public.sales_fpo_leads
    ADD COLUMN IF NOT EXISTS phone_number text;

COMMENT ON COLUMN public.sales_buyer_leads.phone_number IS
  'The number to call this buyer on. Free text: dictated numbers arrive in many shapes and one lead may carry two. Never part of a uniqueness rule.';
COMMENT ON COLUMN public.sales_fpo_leads.phone_number IS
  'The number to call this farmer group on. Free text, same reasoning as the buyer column.';

CREATE EXTENSION IF NOT EXISTS pg_trgm;

ALTER TABLE public.sales_buyer_leads
    ADD COLUMN IF NOT EXISTS search_text text GENERATED ALWAYS AS (
        lower(
            coalesce(buyer_name, '') || ' ' ||
            coalesce(buyer_place, '') || ' ' ||
            coalesce(animal_type, '') || ' ' ||
            coalesce(breed, '') || ' ' ||
            coalesce(phone_number, '')
        )
    ) STORED;

ALTER TABLE public.sales_fpo_leads
    ADD COLUMN IF NOT EXISTS search_text text GENERATED ALWAYS AS (
        lower(
            coalesce(fpo_name, '') || ' ' ||
            coalesce(district, '') || ' ' ||
            coalesce(taluk, '') || ' ' ||
            coalesce(state, '') || ' ' ||
            coalesce(phone_number, '')
        )
    ) STORED;

CREATE INDEX IF NOT EXISTS sales_buyer_leads_search_trgm_idx
    ON public.sales_buyer_leads USING gin (search_text public.gin_trgm_ops);

CREATE INDEX IF NOT EXISTS sales_fpo_leads_search_trgm_idx
    ON public.sales_fpo_leads USING gin (search_text public.gin_trgm_ops);

-- The boards' page order, so paging past the first screen is an index walk rather than a sort of the
-- whole table. created_at DESC with id as the tiebreaker, matching the read exactly: without the
-- tiebreaker two leads imported in the same statement share a timestamp and a page boundary can
-- repeat or skip one of them.
CREATE INDEX IF NOT EXISTS sales_buyer_leads_page_idx
    ON public.sales_buyer_leads (tenant_id, created_at DESC, id);

CREATE INDEX IF NOT EXISTS sales_fpo_leads_page_idx
    ON public.sales_fpo_leads (tenant_id, created_at DESC, id);

-- The status facet: a low-cardinality equality the boards offer beside the search box, so "work
-- through the 166 nobody has called" is one click rather than a scroll.
CREATE INDEX IF NOT EXISTS sales_buyer_leads_status_idx
    ON public.sales_buyer_leads (tenant_id, call_status, created_at DESC, id);

CREATE INDEX IF NOT EXISTS sales_fpo_leads_status_idx
    ON public.sales_fpo_leads (tenant_id, call_status, created_at DESC, id);

-- +goose Down

DROP INDEX IF EXISTS public.sales_buyer_leads_status_idx;
DROP INDEX IF EXISTS public.sales_fpo_leads_status_idx;
DROP INDEX IF EXISTS public.sales_buyer_leads_page_idx;
DROP INDEX IF EXISTS public.sales_fpo_leads_page_idx;
DROP INDEX IF EXISTS public.sales_buyer_leads_search_trgm_idx;
DROP INDEX IF EXISTS public.sales_fpo_leads_search_trgm_idx;

ALTER TABLE public.sales_buyer_leads DROP COLUMN IF EXISTS search_text;
ALTER TABLE public.sales_fpo_leads DROP COLUMN IF EXISTS search_text;

-- The phone columns go last: search_text is generated FROM them, so they cannot be dropped first.
ALTER TABLE public.sales_buyer_leads DROP COLUMN IF EXISTS phone_number;
ALTER TABLE public.sales_fpo_leads DROP COLUMN IF EXISTS phone_number;

-- pg_trgm is deliberately NOT dropped: it is database-wide and the vendor register (000156) depends
-- on it.
