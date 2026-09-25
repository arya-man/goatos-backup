-- +goose Up
-- seed-fixture-guard:ignore: adds breeds.tenant_id / breed_aliases.tenant_id and fills them from
-- the existing rows; every seeded breed keeps its id and lands on the seeded tenant, so no seed
-- command, fixture or projection changes shape (seed-vaccination-real names the tenant itself).
--
-- BREEDS ARE PER FARM (maintainer decision 2026-09-25). A breed is added on Configuration > Items &
-- settings > Breeds exactly like a park, a pen or a species. Until now `breeds` was one product-wide
-- list with no tenant column, which is why the Configuration register was read-only (2026-09-18)
-- and pointed at the herd register -- which never wrote a breed row either, so there was NO way to
-- add a breed at all: a breed typed on Register animal lived only as goats.breed text and never
-- reached Sales, the web breed pickers or a sale product's breed list.
--
-- How the one list becomes one per farm, without moving any animal:
--   1. The OLDEST tenant owns every existing row and keeps its breed_id, so goats.breed_id,
--      breed_aliases, shifting_event_impacts, count_base_anchors and count_projection_snapshot_rows
--      stay valid for it untouched. (STG and the OCI clone hold exactly one tenant.)
--   2. Every OTHER tenant gets its own copy of each row under a new id, and that tenant's own
--      references are repointed to its copy, so no tenant ever points at another tenant's breed.
--   3. A breed the herd already carries as text but the list lacks (goats.breed with no matching
--      row) is added to that farm's list, so the list a farm starts from is the herd it has.
--   4. breed_aliases follows its breed's tenant, and its uniqueness becomes per tenant.
--   5. The admin-ui revision trigger bumps the ROW's tenant, not every tenant.
--
-- LOCK SAFETY: breeds / breed_aliases are small catalogs. The repoint UPDATEs touch only rows of a
-- non-owner tenant (none on STG). lock_timeout bounds every wait.
SET lock_timeout = '5s';

ALTER TABLE public.breeds ADD COLUMN IF NOT EXISTS tenant_id uuid REFERENCES public.tenants(tenant_id);
ALTER TABLE public.breed_aliases ADD COLUMN IF NOT EXISTS tenant_id uuid REFERENCES public.tenants(tenant_id);

-- The product-wide key goes first: the copies below repeat (species, canonical_name) per tenant.
ALTER TABLE public.breeds DROP CONSTRAINT IF EXISTS breeds_unique_name;
ALTER TABLE public.breed_aliases DROP CONSTRAINT IF EXISTS breed_aliases_unique_alias;

-- 1. The oldest tenant owns the existing rows.
UPDATE public.breeds
SET tenant_id = (SELECT t.tenant_id FROM public.tenants t ORDER BY t.created_at, t.tenant_id LIMIT 1)
WHERE tenant_id IS NULL;

-- A database with no tenant at all has no animal to carry a breed; nothing can be owned.
DELETE FROM public.breed_aliases a USING public.breeds b WHERE a.breed_id = b.breed_id AND b.tenant_id IS NULL;
DELETE FROM public.breeds WHERE tenant_id IS NULL;

-- 2. Every other tenant gets its own copy, and its references move to it.
-- A plain temp table (not ON COMMIT DROP) so the steps below still see it when a harness runs
-- this file statement by statement outside one transaction; it is dropped explicitly below.
CREATE TEMP TABLE breed_copy_map AS
SELECT t.tenant_id, b.breed_id AS old_id, gen_random_uuid() AS new_id
FROM public.tenants t
CROSS JOIN public.breeds b
WHERE t.tenant_id <> b.tenant_id;

INSERT INTO public.breeds (breed_id, tenant_id, species, canonical_name, status, review_notes, created_at, updated_at)
SELECT m.new_id, m.tenant_id, b.species, b.canonical_name, b.status, b.review_notes, b.created_at, now()
FROM breed_copy_map m
JOIN public.breeds b ON b.breed_id = m.old_id;

UPDATE public.goats g SET breed_id = m.new_id
FROM breed_copy_map m WHERE g.tenant_id = m.tenant_id AND g.breed_id = m.old_id;
UPDATE public.shifting_event_impacts i SET breed_id = m.new_id
FROM breed_copy_map m WHERE i.tenant_id = m.tenant_id AND i.breed_id = m.old_id;
UPDATE public.count_base_anchors a SET breed_id = m.new_id
FROM breed_copy_map m WHERE a.tenant_id = m.tenant_id AND a.breed_id = m.old_id;
UPDATE public.count_projection_snapshot_rows s SET breed_id = m.new_id
FROM breed_copy_map m WHERE s.tenant_id = m.tenant_id AND s.breed_id = m.old_id;

-- 4. Aliases follow their breed; the other tenants get their own.
UPDATE public.breed_aliases a SET tenant_id = b.tenant_id
FROM public.breeds b WHERE a.breed_id = b.breed_id AND a.tenant_id IS NULL;

INSERT INTO public.breed_aliases (tenant_id, breed_id, alias, normalized_alias, source_system, created_at)
SELECT m.tenant_id, m.new_id, a.alias, a.normalized_alias, a.source_system, a.created_at
FROM public.breed_aliases a
JOIN breed_copy_map m ON m.old_id = a.breed_id;

-- 3. A breed the herd already carries joins its farm's list.
INSERT INTO public.breeds (tenant_id, species, canonical_name, status, review_notes)
SELECT DISTINCT ON (g.tenant_id, lower(btrim(g.breed)))
       g.tenant_id, btrim(g.species), btrim(g.breed), 'active',
       'Added from the herd register when breeds became per farm.'
FROM public.goats g
WHERE g.merged_into_goat_id IS NULL
  AND g.lifecycle_status = 'alive'
  AND btrim(COALESCE(g.breed, '')) <> ''
  AND btrim(COALESCE(g.species, '')) <> ''
  AND NOT EXISTS (
    SELECT 1 FROM public.breeds b
    WHERE b.tenant_id = g.tenant_id AND lower(btrim(b.canonical_name)) = lower(btrim(g.breed))
  )
ORDER BY g.tenant_id, lower(btrim(g.breed)), btrim(g.species);

DROP TABLE breed_copy_map;

ALTER TABLE public.breeds ALTER COLUMN tenant_id SET NOT NULL;
ALTER TABLE public.breed_aliases ALTER COLUMN tenant_id SET NOT NULL;
ALTER TABLE public.breeds ADD CONSTRAINT breeds_unique_name UNIQUE (tenant_id, species, canonical_name);
ALTER TABLE public.breed_aliases ADD CONSTRAINT breed_aliases_unique_alias UNIQUE (tenant_id, normalized_alias, source_system);
CREATE INDEX IF NOT EXISTS breeds_tenant_status_idx ON public.breeds (tenant_id, status);

-- 5. A breed change refreshes only its own farm's pickers.
DROP TRIGGER IF EXISTS admin_ui_breeds_revision_trg ON public.breeds;
CREATE TRIGGER admin_ui_breeds_revision_trg
  AFTER INSERT OR DELETE OR UPDATE ON public.breeds
  FOR EACH ROW EXECUTE FUNCTION public.admin_ui_bump_row_family_trg('breeds');

-- +goose Down
-- Refused once a second tenant owns breeds: dropping the column would merge two farms' lists
-- under one product-wide key and the unique constraint below could not be restored.
DO $$
BEGIN
  IF (SELECT count(DISTINCT tenant_id) FROM public.breeds) > 1 THEN
    RAISE EXCEPTION 'breeds belong to more than one tenant; 000442 cannot be rolled back';
  END IF;
END $$;

DROP TRIGGER IF EXISTS admin_ui_breeds_revision_trg ON public.breeds;
CREATE TRIGGER admin_ui_breeds_revision_trg
  AFTER INSERT OR DELETE OR UPDATE ON public.breeds
  FOR EACH ROW EXECUTE FUNCTION public.admin_ui_bump_global_family_trg('breeds');
DROP INDEX IF EXISTS public.breeds_tenant_status_idx;
ALTER TABLE public.breed_aliases DROP CONSTRAINT IF EXISTS breed_aliases_unique_alias;
ALTER TABLE public.breeds DROP CONSTRAINT IF EXISTS breeds_unique_name;
ALTER TABLE public.breed_aliases DROP COLUMN IF EXISTS tenant_id;
ALTER TABLE public.breeds DROP COLUMN IF EXISTS tenant_id;
ALTER TABLE public.breeds ADD CONSTRAINT breeds_unique_name UNIQUE (species, canonical_name);
ALTER TABLE public.breed_aliases ADD CONSTRAINT breed_aliases_unique_alias UNIQUE (normalized_alias, source_system);
