-- +goose Up

-- EVERY SALE NAMES ITS BUYER FROM THE VENDOR REGISTER.
--
-- Maintainer decision 2026-08-27: "we don't do random sales. We do sales of anything to the
-- vendor." A sale recorded in the app must be mapped to one row of the procurement vendor
-- register, so revenue can be read back per counterparty instead of per free-text spelling of a
-- name ("Ramesh Traders" / "ramesh traders" / "Ramesh" were three buyers on the buyer board).
--
-- WHY THIS IS AN OPAQUE UUID AND NOT A FOREIGN KEY
-- ------------------------------------------------
-- 000173_sales_ledger.sql states the sales lock: "Sales therefore owns its own tables and reads
-- NOTHING from the herd, vaccination, or procurement schemas." A real FK to
-- public.procurement_vendors would couple the sales write path to the procurement schema and let a
-- procurement migration block a sale -- exactly the coupling the lock exists to prevent.
--
-- 000177_goat_sale_allocations.sql already settled this shape in the other direction: it carries
-- sales_deal_id as an OPAQUE REFERENCE, deliberately not a foreign key, "validated by the
-- application against the deal read, not by the database". This column is the mirror of that. The
-- sales module still reads no procurement table: the VENDOR IS PICKED IN ADMIN-WEB, which already
-- reads the register through its own /procurement/vendors endpoint under VendorRead, and the sale
-- stores the id it was handed.
--
-- WHY buyer_name STAYS
-- --------------------
-- buyer_name is a SNAPSHOT of who the buyer was called at the moment of sale, and it stays NOT
-- NULL. A vendor row is edited (renamed, merged, re-contacted) and an old sale must not silently
-- re-describe itself -- the same reason 000177 snapshots the animal's location instead of reading
-- it back off the goat. It is also what keeps the buyer board readable for the 2026-08-17 sheet
-- import, whose rows predate the register and carry no vendor at all.
--
-- WHY THE COLUMN IS NULLABLE
-- --------------------------
-- Imported sheet history has no vendor and never will. Requiring one here would either reject real
-- history or invent a counterparty. The requirement lives on the WRITE PATH instead
-- (sales/domain.DealWrite.Validate), where every deal recorded in the app must carry a vendor.

ALTER TABLE public.sales_deals
  ADD COLUMN IF NOT EXISTS buyer_vendor_id uuid;

COMMENT ON COLUMN public.sales_deals.buyer_vendor_id IS
  'The procurement vendor register row this sale was made to, as an OPAQUE reference (deliberately not a foreign key -- see 000193 and the 000177 precedent). Required for deals recorded in the app; NULL for the 2026-08-17 sheet import, which predates the register.';

-- "Every sale to this vendor", the read behind per-counterparty revenue. Partial so the imported
-- history (NULL) never enters the index.
CREATE INDEX IF NOT EXISTS sales_deals_buyer_vendor_idx
  ON public.sales_deals (tenant_id, buyer_vendor_id, sale_date DESC)
  WHERE buyer_vendor_id IS NOT NULL;

-- +goose Down

DROP INDEX IF EXISTS sales_deals_buyer_vendor_idx;
ALTER TABLE public.sales_deals
  DROP COLUMN IF EXISTS buyer_vendor_id;
