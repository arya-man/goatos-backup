-- +goose Up
-- A PURCHASE LOAD'S FARM IS ANY OF THE TENANT'S PARKS, NOT A CBE/CPT PAIR.
--
-- 000299 pinned animal_purchase_loads.farm_label to CHECK (farm_label IN ('CBE','CPT')), so a
-- load could never be bought for a park added on Configuration > Items & settings. The animal
-- purchase service now checks the farm against the tenant's ACTIVE park codes
-- (platform/parkcatalog) and serves the same list as the load form's farm choices. The column
-- keeps a not-blank guard under the same name.
--
-- Every stored row is 'CBE' or 'CPT', so the looser check validates without rewriting anything.
--
-- LOCK SAFETY: DROP CONSTRAINT and ADD ... NOT VALID take a brief ACCESS EXCLUSIVE lock with no
-- scan; VALIDATE CONSTRAINT scans under SHARE UPDATE EXCLUSIVE, which does not block writes.
SET lock_timeout = '5s';

ALTER TABLE public.animal_purchase_loads DROP CONSTRAINT IF EXISTS animal_purchase_loads_farm_check;
ALTER TABLE public.animal_purchase_loads
  ADD CONSTRAINT animal_purchase_loads_farm_check CHECK (btrim(farm_label) <> '') NOT VALID;
ALTER TABLE public.animal_purchase_loads VALIDATE CONSTRAINT animal_purchase_loads_farm_check;

-- +goose Down
SET lock_timeout = '5s';

ALTER TABLE public.animal_purchase_loads DROP CONSTRAINT IF EXISTS animal_purchase_loads_farm_check;
ALTER TABLE public.animal_purchase_loads
  ADD CONSTRAINT animal_purchase_loads_farm_check CHECK (farm_label IN ('CBE', 'CPT')) NOT VALID;
