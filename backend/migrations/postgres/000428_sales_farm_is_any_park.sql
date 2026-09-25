-- +goose Up
-- A SALE'S FARM IS ANY OF THE TENANT'S PARKS, NOT A CBE/CPT PAIR.
--
-- 000173 pinned sales_deals.farm to CHECK (farm IN ('CBE','CPT')), so a park added on
-- Configuration > Items & settings could never record a sale: the app refused it, and had the app
-- let it through, this constraint would have. The farm is now validated by the sales service
-- against the tenant's ACTIVE park codes (platform/parkcatalog), which is the only list that
-- knows about a park added yesterday. The column keeps a not-blank guard under the same name.
--
-- Every stored row is 'CBE' or 'CPT', so the looser check validates without rewriting anything.
--
-- LOCK SAFETY: DROP CONSTRAINT and ADD ... NOT VALID take a brief ACCESS EXCLUSIVE lock with no
-- scan; VALIDATE CONSTRAINT scans under SHARE UPDATE EXCLUSIVE, which does not block writes.
SET lock_timeout = '5s';

ALTER TABLE public.sales_deals DROP CONSTRAINT IF EXISTS sales_deals_farm_check;
ALTER TABLE public.sales_deals
  ADD CONSTRAINT sales_deals_farm_check CHECK (btrim(farm) <> '') NOT VALID;
ALTER TABLE public.sales_deals VALIDATE CONSTRAINT sales_deals_farm_check;

-- +goose Down
SET lock_timeout = '5s';

ALTER TABLE public.sales_deals DROP CONSTRAINT IF EXISTS sales_deals_farm_check;
ALTER TABLE public.sales_deals
  ADD CONSTRAINT sales_deals_farm_check CHECK (farm IN ('CBE', 'CPT')) NOT VALID;
