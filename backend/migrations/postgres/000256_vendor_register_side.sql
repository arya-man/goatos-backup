-- +goose Up
-- THE VENDOR REGISTER HAS TWO SIDES (maintainer decision 2026-09-05).
--
-- 000217 added five buyer-side record types -- Agent, Butcher, Company, Farmer, Slaughter House --
-- to the one shared register, on the reasoning that "every buyer is a vendor" and one register can
-- hold both. That is still true of the STORAGE; it turned out to be false of the SCREEN. A person
-- running the buying desk opening Procurement > Vendors was offered five categories that have
-- nothing to do with buying, and a person recording a sale had to go to the Procurement page to add
-- the butcher they just sold to.
--
-- So the vocabulary -- not the table -- is split. Each record_type now declares which register it
-- belongs to, and the two pages read the same table through opposite halves of that declaration:
--
--   Procurement > Vendors   record types on the 'procurement' side (the supply desk's 35)
--   Sales > Vendors         record types on the 'sales' side (the five from 000217)
--
-- WHY A CATALOG COLUMN AND NOT A LIST IN GO. Per AGENTS.md, business-managed dropdown vocabularies
-- come from Postgres, never from backend-code literals -- which is the same reason 000156 put
-- record types in this table instead of a CHECK. A hardcoded Go list of the five would mean the
-- farm could add a sixth sales category as data and then find it silently filed under Procurement.
--
-- WHY IT LIVES ON THE CATALOG ROW AND NOT ON THE VENDOR ROW. A vendor's side is not an independent
-- fact about the vendor; it is implied entirely by its record type. Storing it twice would let the
-- two disagree, and every rename or re-categorisation would need a backfill.
--
-- THE DEFAULT IS 'procurement', AND THAT IS THE SAFE DIRECTION. The two pages are complementary by
-- construction: sales lists the record types marked 'sales', procurement lists everything else. So
-- a record type nobody has catalogued -- possible, because domain.Validate deliberately does not
-- check record_type against the catalog -- keeps appearing exactly where it appears today, on the
-- procurement register, rather than vanishing from both. THAT is the property under test; the
-- read-side predicate spells it NOT EXISTS, which is equivalent to NOT IN here because `value` is
-- NOT NULL in this table's primary key, and is preferred only because it does not depend on that
-- staying true. Every vendor is on exactly one side; none is on neither.
--
-- The column is NOT NULL DEFAULT, so it applies to the kinds that have no side (breed, state, city,
-- status, feed, capacity_unit, supply_frequency) as an inert value. Only kind='record_type' is ever
-- read for it. A partial CHECK could enforce that, but it would buy nothing and would reject a
-- future kind that legitimately wants a side.
--
-- The importer (backend/cmd/import-procurement-vendors) upserts only label and sort_order, so a
-- re-import of the 2026-08-12 sheet preserves every side set here and files any newly-imported
-- record type on the procurement side -- which is what a sheet of suppliers means.

ALTER TABLE public.procurement_vendor_catalog
  ADD COLUMN IF NOT EXISTS register_side text NOT NULL DEFAULT 'procurement';

ALTER TABLE public.procurement_vendor_catalog
  DROP CONSTRAINT IF EXISTS procurement_vendor_catalog_register_side_check,
  ADD CONSTRAINT procurement_vendor_catalog_register_side_check
    CHECK (register_side = ANY (ARRAY['procurement'::text, 'sales'::text]));

COMMENT ON COLUMN public.procurement_vendor_catalog.register_side IS
  'Which vendor register offers this record_type: procurement (the buying desk) or sales. Read only for kind=record_type; inert on every other kind.';

-- The five from 000217, and only those. They are named as literals rather than derived, because
-- this is the one-time statement of which existing categories the maintainer classified as sales.
UPDATE public.procurement_vendor_catalog
   SET register_side = 'sales',
       updated_at    = now()
 WHERE kind = 'record_type'
   AND value IN ('Agent', 'Butcher', 'Company', 'Farmer', 'Slaughter House')
   AND register_side <> 'sales';

-- The side filter is an EXISTS against (tenant_id, kind, value) -- already the primary key -- so
-- the lookup is a unique index probe and needs no new index. This one supports the reverse
-- direction: listing a side's record types to build the Sales page's category dropdown.
CREATE INDEX IF NOT EXISTS procurement_vendor_catalog_side_idx
    ON public.procurement_vendor_catalog (tenant_id, kind, register_side, sort_order, value);

-- +goose Down

DROP INDEX IF EXISTS public.procurement_vendor_catalog_side_idx;

ALTER TABLE public.procurement_vendor_catalog
  DROP CONSTRAINT IF EXISTS procurement_vendor_catalog_register_side_check;

ALTER TABLE public.procurement_vendor_catalog
  DROP COLUMN IF EXISTS register_side;
