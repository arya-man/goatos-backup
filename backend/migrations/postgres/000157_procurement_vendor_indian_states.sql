-- +goose Up
-- The vendor register's STATE vocabulary becomes the full Indian list.
--
-- Maintainer decision 2026-08-13: a vendor may sit in any Indian state, not only the twelve the
-- imported sheet happened to contain (AP, GJ, JH, KA, KL, MH, MP, OR, RJ, TN, TS, UP). Adding a
-- supplier in Punjab or Bihar previously required editing the source sheet's Validation tab first,
-- which is exactly the coupling this module removed.
--
-- VALUES ARE CODES, LABELS ARE NAMES. The 306 imported rows store two-letter codes in
-- procurement_vendors.state, so the code is the stable identity and must not change; the label is
-- what the operator reads. Seeding full names as VALUES would orphan every existing vendor from its
-- own dropdown.
--
-- 'OR' is kept for Odisha rather than the modern 'OD'. Two imported vendors already carry 'OR', and
-- renaming the value would strand them on a code no longer in the catalog. The label carries the
-- current name, so the screen reads "Odisha" either way.
--
-- UNION TERRITORIES ARE INCLUDED. Delhi, Chandigarh and Puducherry are UTs rather than states, and
-- a feed or transport supplier in Delhi is entirely ordinary -- a list that omitted them would send
-- someone back to editing the vocabulary within a week. 28 states + 8 UTs = 36 rows.
--
-- sort_order is 0 for every row so the API's `ORDER BY kind, sort_order, value` falls through to an
-- alphabetical-by-code ordering, which is stable and needs no maintenance as the list changes.
INSERT INTO public.procurement_vendor_catalog (tenant_id, kind, value, label, sort_order, is_active)
SELECT t.tenant_id, 'state', s.code, s.label, 0, true
FROM (SELECT DISTINCT tenant_id FROM public.procurement_vendor_catalog) t
CROSS JOIN (VALUES
    -- States (28)
    ('AP', 'Andhra Pradesh'),
    ('AR', 'Arunachal Pradesh'),
    ('AS', 'Assam'),
    ('BR', 'Bihar'),
    ('CG', 'Chhattisgarh'),
    ('GA', 'Goa'),
    ('GJ', 'Gujarat'),
    ('HR', 'Haryana'),
    ('HP', 'Himachal Pradesh'),
    ('JH', 'Jharkhand'),
    ('KA', 'Karnataka'),
    ('KL', 'Kerala'),
    ('MP', 'Madhya Pradesh'),
    ('MH', 'Maharashtra'),
    ('MN', 'Manipur'),
    ('ML', 'Meghalaya'),
    ('MZ', 'Mizoram'),
    ('NL', 'Nagaland'),
    ('OR', 'Odisha'),
    ('PB', 'Punjab'),
    ('RJ', 'Rajasthan'),
    ('SK', 'Sikkim'),
    ('TN', 'Tamil Nadu'),
    ('TS', 'Telangana'),
    ('TR', 'Tripura'),
    ('UP', 'Uttar Pradesh'),
    ('UK', 'Uttarakhand'),
    ('WB', 'West Bengal'),
    -- Union territories (8)
    ('AN', 'Andaman and Nicobar Islands'),
    ('CH', 'Chandigarh'),
    ('DH', 'Dadra and Nagar Haveli and Daman and Diu'),
    ('DL', 'Delhi'),
    ('JK', 'Jammu and Kashmir'),
    ('LA', 'Ladakh'),
    ('LD', 'Lakshadweep'),
    ('PY', 'Puducherry')
) AS s(code, label)
ON CONFLICT (tenant_id, kind, value) DO UPDATE
SET label = EXCLUDED.label,
    is_active = true,
    sort_order = EXCLUDED.sort_order,
    updated_at = now();

-- CITY becomes free text on the entry form (same decision), so its frozen vocabulary is no longer
-- the authority on what a city may be. The rows are RETIRED rather than deleted: the catalog read
-- for the city filter is now derived live from the cities vendors actually carry, and deleting
-- these would not change that read, while keeping them costs nothing and preserves the record of
-- what the sheet once offered.
UPDATE public.procurement_vendor_catalog
SET is_active = false, updated_at = now()
WHERE kind = 'city';

-- +goose Down
-- Reactivate the imported city vocabulary. The added states are left in place: they are additive
-- reference data, and removing them would strand any vendor recorded in one of them.
UPDATE public.procurement_vendor_catalog
SET is_active = true, updated_at = now()
WHERE kind = 'city';
