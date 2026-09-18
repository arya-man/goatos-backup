-- +goose Up
-- seed-fixture-guard:ignore: reference catalogs the CEO edits on /configuration/items; the
-- built-in rows below are the same values every seed already writes as literals (goat/sheep,
-- female/male, the inventory category enum), so no vaccination/HRMS seed contract or source
-- fixture schema changes.
--
-- CONFIGURATION: ITEMS AND SETTINGS (maintainer instruction 2026-09-18, from the Claude
-- prototype merged in #297). The farm's reference lists stop being seed literals and become
-- tenant-scoped rows a person edits on screen: a new owner adds their own animal types, farm
-- places, feeds and medicines without a developer. Three schema moves:
--
--   1. species_lookup / sex_lookup. goats.species and goats.sex were CHECK constraints
--      (goat|sheep, female|male). They become codes in a per-tenant lookup so a new species is
--      one row, not a migration (the identity write paths validate against the lookup; see
--      below for why it is not a foreign key). The four built-in rows are flagged is_builtin: they
--      may be renamed on screen but never archived or deleted, because vaccination, feed and
--      weighing rules still name them by code. A NEW species gets no vaccination/feed rule until
--      someone authors one; that is an honest boundary, not a defect.
--
--   2. item_categories: an editable TREE over inventory_items. The old fixed `category` enum
--      stays as the row's KIND (vaccine, medicine, feed, ...) because obligation stock reserve,
--      PC Care requirements and the vaccines detail table key on it; the tree gives the farm its
--      own subcategories ("Medicines > Antibiotics") without touching those consumers. Every root
--      carries its kind; a child inherits its root's.
--
--   3. inventory_items.category_id, the row's leaf in that tree, backfilled to the built-in root
--      of its kind so no existing item is left uncategorised.
--
-- Farm places (farms, parks, pens, partitions) need NO new table: locations + farm_profiles /
-- park_profiles / shed_profiles + shed_partitions already carry them; this module only gives
-- them a write path with a screen.

CREATE TABLE public.species_lookup (
    tenant_id    uuid    NOT NULL REFERENCES public.tenants (tenant_id),
    species_code text    NOT NULL CHECK (species_code ~ '^[a-z][a-z0-9_]{0,39}$'),
    name         text    NOT NULL CHECK (btrim(name) <> ''),
    sort_order   integer NOT NULL DEFAULT 100,
    status       text    NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
    is_builtin   boolean NOT NULL DEFAULT false,
    row_version  integer NOT NULL DEFAULT 1 CHECK (row_version >= 1),
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, species_code)
);
CREATE UNIQUE INDEX species_lookup_tenant_name_uidx ON public.species_lookup (tenant_id, lower(btrim(name)));

CREATE TABLE public.sex_lookup (
    tenant_id   uuid    NOT NULL REFERENCES public.tenants (tenant_id),
    sex_code    text    NOT NULL CHECK (sex_code ~ '^[a-z][a-z0-9_]{0,39}$'),
    name        text    NOT NULL CHECK (btrim(name) <> ''),
    sort_order  integer NOT NULL DEFAULT 100,
    status      text    NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
    is_builtin  boolean NOT NULL DEFAULT false,
    row_version integer NOT NULL DEFAULT 1 CHECK (row_version >= 1),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, sex_code)
);
CREATE UNIQUE INDEX sex_lookup_tenant_name_uidx ON public.sex_lookup (tenant_id, lower(btrim(name)));

-- The built-in rows for EVERY tenant: the two species and two sexes every seed wrote as literals.
INSERT INTO public.species_lookup (tenant_id, species_code, name, sort_order, is_builtin)
SELECT t.tenant_id, v.code, v.name, v.sort, true
FROM public.tenants t
CROSS JOIN (VALUES ('goat', 'Goat', 10), ('sheep', 'Sheep', 20)) AS v(code, name, sort)
ON CONFLICT DO NOTHING;

INSERT INTO public.sex_lookup (tenant_id, sex_code, name, sort_order, is_builtin)
SELECT t.tenant_id, v.code, v.name, v.sort, true
FROM public.tenants t
CROSS JOIN (VALUES ('female', 'Female', 10), ('male', 'Male', 20)) AS v(code, name, sort)
ON CONFLICT DO NOTHING;

-- The CHECK constraints go; the lookup is the rule from here on and the identity write paths
-- (create / import / edit) validate a code against it. It is NOT re-added as a foreign key:
-- goats carries no FK to tenants, and the integration fixtures across 86 files insert goats
-- under ad-hoc tenant ids with no tenants row, which an FK into a tenant-owned lookup would
-- refuse wholesale. A lookup-backed app validator is the same guarantee on every real write.
ALTER TABLE public.goats DROP CONSTRAINT IF EXISTS goats_species_check;
ALTER TABLE public.goats DROP CONSTRAINT IF EXISTS goats_sex_check;

-- 2. The category tree.
CREATE TABLE public.item_categories (
    category_id        uuid    PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          uuid    NOT NULL REFERENCES public.tenants (tenant_id),
    parent_category_id uuid    REFERENCES public.item_categories (category_id) ON DELETE RESTRICT,
    name               text    NOT NULL CHECK (btrim(name) <> ''),
    normalized_name    text    NOT NULL,
    -- The inventory kind (inventory_items.category enum). NOT NULL on a root, NULL on a child:
    -- a child's kind is its root's, resolved by walking up, so one subtree is one kind.
    item_kind          text    CHECK (item_kind IS NULL OR item_kind IN ('vaccine', 'dewormer', 'medicine', 'feed', 'supplement', 'consumable', 'other')),
    sort_order         integer NOT NULL DEFAULT 100,
    status             text    NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
    is_builtin         boolean NOT NULL DEFAULT false,
    row_version        integer NOT NULL DEFAULT 1 CHECK (row_version >= 1),
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT item_categories_root_kind_check CHECK ((parent_category_id IS NULL) = (item_kind IS NOT NULL)),
    CONSTRAINT item_categories_tenant_id_unique UNIQUE (tenant_id, category_id)
);
-- One name per level: two "Antibiotics" under Medicines cannot both exist, but Medicines >
-- Antibiotics and Feed > Antibiotics can.
CREATE UNIQUE INDEX item_categories_sibling_name_uidx
    ON public.item_categories (tenant_id, COALESCE(parent_category_id, '00000000-0000-0000-0000-000000000000'::uuid), normalized_name);
CREATE INDEX item_categories_parent_idx ON public.item_categories (tenant_id, parent_category_id);

-- The seven built-in roots, one per inventory kind, for every tenant. They are the roots existing
-- items are filed under below and the only ones a fresh tenant starts with.
INSERT INTO public.item_categories (tenant_id, name, normalized_name, item_kind, sort_order, is_builtin)
SELECT t.tenant_id, v.name, lower(btrim(v.name)), v.kind, v.sort, true
FROM public.tenants t
CROSS JOIN (VALUES
    ('Medicines',   'medicine',   10),
    ('Vaccines',    'vaccine',    20),
    ('Dewormers',   'dewormer',   30),
    ('Feed',        'feed',       40),
    ('Supplements', 'supplement', 50),
    ('Consumables', 'consumable', 60),
    ('Other',       'other',      70)
) AS v(name, kind, sort)
ON CONFLICT DO NOTHING;

-- 3. Every item names its leaf category; existing rows land on their kind's built-in root.
ALTER TABLE public.inventory_items ADD COLUMN IF NOT EXISTS category_id uuid;
UPDATE public.inventory_items i
SET category_id = c.category_id
FROM public.item_categories c
WHERE c.tenant_id = i.tenant_id
  AND c.parent_category_id IS NULL
  AND c.item_kind = i.category
  AND i.category_id IS NULL;
ALTER TABLE public.inventory_items
    ADD CONSTRAINT inventory_items_category_fk FOREIGN KEY (tenant_id, category_id)
        REFERENCES public.item_categories (tenant_id, category_id) ON DELETE RESTRICT;
CREATE INDEX inventory_items_category_idx ON public.inventory_items (tenant_id, category_id);
-- Items are listed by name on screen and searched by it.
CREATE INDEX IF NOT EXISTS inventory_items_tenant_name_idx ON public.inventory_items (tenant_id, lower(name));

-- +goose Down
DROP INDEX IF EXISTS public.inventory_items_tenant_name_idx;
DROP INDEX IF EXISTS public.inventory_items_category_idx;
ALTER TABLE public.inventory_items DROP CONSTRAINT IF EXISTS inventory_items_category_fk;
ALTER TABLE public.inventory_items DROP COLUMN IF EXISTS category_id;
DROP TABLE IF EXISTS public.item_categories;
ALTER TABLE public.goats ADD CONSTRAINT goats_species_check CHECK (species = ANY (ARRAY['goat'::text, 'sheep'::text])) NOT VALID;
ALTER TABLE public.goats ADD CONSTRAINT goats_sex_check CHECK (sex = ANY (ARRAY['female'::text, 'male'::text])) NOT VALID;
DROP TABLE IF EXISTS public.sex_lookup;
DROP TABLE IF EXISTS public.species_lookup;
