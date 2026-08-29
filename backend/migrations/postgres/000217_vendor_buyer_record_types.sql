-- +goose Up
-- BUYER-SIDE VENDOR CATEGORIES (maintainer decision 2026-08-27).
--
-- Migration 000193 settled that "every buyer is a vendor": a sale recorded in the app maps to one
-- row of the procurement vendor register (sales_deals.buyer_vendor_id). The register's vocabulary,
-- however, was imported from a legacy sheet that only ever described the SUPPLY side -- 35
-- record_type values, every one of them somebody the farm buys FROM. There was no way to classify
-- who the farm SELLS TO, so buyers were being filed under supply-side types or left mismatched.
--
-- Five categories are added, all singular to match the existing 35 (Sheep Agent, Vet Doctor,
-- Welder, ...); the maintainer's list said "Agents"/"Butchers" and confirmed singular here so the
-- dropdown does not mix conventions.
--
-- 'Agent' IS DELIBERATELY GENERIC AND SITS BESIDE SEVEN SPECIFIC ONES. The catalog already carries
-- Sheep Agent, Goats Agent, Feed Agent, Manure Agent, Transport Agent, Labor Agent and Land Agent,
-- every one of them naming the commodity the agent supplies. A buyer's agent has no commodity to
-- name -- he is simply the counterparty's agent -- so a generic entry is the honest classification
-- rather than filing him under a supply commodity he has nothing to do with. Confirmed with the
-- maintainer before adding, precisely because a bare 'Agent' next to seven qualified ones is a
-- choice an operator could otherwise misread.
--
-- NO CHECK CONSTRAINT IS TOUCHED, because there is none to touch: 000156 states the register's
-- record types live in procurement_vendor_catalog as DATA rather than a CHECK, per the AGENTS.md
-- rule that business-managed dropdown vocabularies come from the database. That is exactly why
-- this change is a few rows and no schema change, and why admin-web needs no edit -- it renders
-- whatever /procurement/vendors returns.
--
-- WHY sort_order = 100 AND NOT AN ALPHABETICAL INTERLEAVE. The catalog is read
-- `ORDER BY kind, sort_order, value`, and import-procurement-vendors UPSERTS sort_order to each
-- value's INDEX in the committed fixture (0..34) every time it runs. Renumbering all forty rows to
-- interleave these five alphabetically would therefore be undone for the fixture's thirty-five on
-- the next import and leave the ordering jumbled. A shared 100 keeps the five together, after the
-- imported set, ordered among themselves by value -- and is stable no matter how often the importer
-- re-runs.
--
-- The committed fixture at fixtures/procurement-vendors-2026-08-12/vendors.json is deliberately NOT
-- edited. It is a snapshot of what the legacy sheet actually contained on that date; adding
-- categories the sheet never had would make an import record claim something untrue. A re-import
-- upserts the fixture's rows and deletes nothing, so these five survive it.
--
-- Applied to every tenant in public.tenants rather than a hardcoded id, so the migration behaves
-- the same on a fresh local database, staging and production. It is deliberately NOT scoped to
-- tenants that already hold a record_type vocabulary: on a FRESH database the vendor import has not
-- run yet, that vocabulary does not exist, such a filter would insert nothing, and the five
-- categories would then be missing forever -- the import upserts only the fixture's own values and
-- would never add them afterwards.

INSERT INTO public.procurement_vendor_catalog (tenant_id, kind, value, label, sort_order, is_active)
SELECT t.tenant_id, 'record_type', v.value, v.value, 100, true
FROM public.tenants t
CROSS JOIN (VALUES
    ('Agent'),
    ('Butcher'),
    ('Company'),
    ('Farmer'),
    ('Slaughter House')
) AS v(value)
ON CONFLICT (tenant_id, kind, value) DO UPDATE
SET label      = EXCLUDED.label,
    is_active  = true,
    updated_at = now();

-- +goose Down
-- Only the five values, and only where nothing was filed under them: a vendor already carrying one
-- of these record types must not be left holding a value its own edit form can no longer offer,
-- which is the same reasoning 000156 gives for retiring rather than deleting a vocabulary entry.
DELETE FROM public.procurement_vendor_catalog c
WHERE c.kind = 'record_type'
  AND c.value IN ('Agent', 'Butcher', 'Company', 'Farmer', 'Slaughter House')
  AND NOT EXISTS (
      SELECT 1 FROM public.procurement_vendors v
       WHERE v.tenant_id = c.tenant_id AND v.record_type = c.value
  );
