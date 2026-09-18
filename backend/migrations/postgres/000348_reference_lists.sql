-- +goose Up
-- seed-fixture-guard:ignore: reference vocabularies the CEO edits on /configuration/items; the
-- built-in entries are the codes the product already names in Go, so no vaccination/HRMS seed
-- contract, source fixture schema, or read-model change.
--
-- REFERENCE LISTS (maintainer instruction 2026-09-18, "give option to add category"): a generic
-- pair of tables so the farm can keep ANY small vocabulary on screen -- and add a new one --
-- without a developer. A list is a row of reference_lists; its values are rows of
-- reference_list_entries. Each list becomes its own register on Configuration -> Items and
-- settings under Reference lists (register key `ref:<list_key>`).
--
-- Four lists are seeded for every tenant. Two mirror vocabularies the product names in Go and
-- are built in (rename only): exit reasons (identity's allowedExitReasons) and movement reasons
-- (counts' shift types). Two are new, empty of consumers today, seeded with the prototype's
-- values so the screen is not blank: animal purposes and weight bands (the sales Sold bands).
-- Status definitions, SOP categories and task types keep their own tables and get registers of
-- their own; they are not folded in here.
CREATE TABLE public.reference_lists (
    tenant_id   uuid    NOT NULL REFERENCES public.tenants (tenant_id),
    list_key    text    NOT NULL CHECK (list_key ~ '^[a-z][a-z0-9_]{0,39}$'),
    name        text    NOT NULL CHECK (btrim(name) <> ''),
    description text    NOT NULL DEFAULT '',
    sort_order  integer NOT NULL DEFAULT 100,
    status      text    NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
    is_builtin  boolean NOT NULL DEFAULT false,
    row_version integer NOT NULL DEFAULT 1 CHECK (row_version >= 1),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, list_key)
);
CREATE UNIQUE INDEX reference_lists_tenant_name_uidx ON public.reference_lists (tenant_id, lower(btrim(name)));

CREATE TABLE public.reference_list_entries (
    tenant_id   uuid    NOT NULL,
    list_key    text    NOT NULL,
    entry_code  text    NOT NULL CHECK (entry_code ~ '^[a-z][a-z0-9_]{0,39}$'),
    name        text    NOT NULL CHECK (btrim(name) <> ''),
    description text    NOT NULL DEFAULT '',
    sort_order  integer NOT NULL DEFAULT 100,
    status      text    NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
    is_builtin  boolean NOT NULL DEFAULT false,
    row_version integer NOT NULL DEFAULT 1 CHECK (row_version >= 1),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, list_key, entry_code),
    CONSTRAINT reference_list_entries_list_fk FOREIGN KEY (tenant_id, list_key)
        REFERENCES public.reference_lists (tenant_id, list_key) ON DELETE RESTRICT
);
CREATE UNIQUE INDEX reference_list_entries_name_uidx ON public.reference_list_entries (tenant_id, list_key, lower(btrim(name)));

INSERT INTO public.reference_lists (tenant_id, list_key, name, description, sort_order, is_builtin)
SELECT t.tenant_id, v.key, v.name, v.description, v.sort, v.builtin
FROM public.tenants t
CROSS JOIN (VALUES
    ('exit_reasons',     'Exit reasons',     'Why an animal leaves the herd register.',                       10, true),
    ('animal_purposes',  'Animal purposes',  'What an animal is kept for.',                                   20, false),
    ('movement_reasons', 'Movement reasons', 'Why animals move between pens; the shift types the app raises.', 30, true),
    ('weight_bands',     'Weight bands',     'The live-weight bands sales reporting groups animals into.',    40, false)
) AS v(key, name, description, sort, builtin)
ON CONFLICT DO NOTHING;

INSERT INTO public.reference_list_entries (tenant_id, list_key, entry_code, name, sort_order, is_builtin)
SELECT t.tenant_id, v.list, v.code, v.name, v.sort, v.builtin
FROM public.tenants t
CROSS JOIN (VALUES
    ('exit_reasons', 'sold',        'Sold',        10, true),
    ('exit_reasons', 'died',        'Died',        20, true),
    ('exit_reasons', 'culled',      'Culled',      30, true),
    ('exit_reasons', 'transferred', 'Transferred', 40, true),
    ('exit_reasons', 'lost',        'Lost',        50, true),
    ('animal_purposes', 'breeding',  'Breeding',  10, false),
    ('animal_purposes', 'fattening', 'Fattening', 20, false),
    ('movement_reasons', 'health',   'Health',   10, true),
    ('movement_reasons', 'growth',   'Growth',   20, true),
    ('movement_reasons', 'breeding', 'Breeding', 30, true),
    ('movement_reasons', 'delivery', 'Delivery', 40, true),
    ('movement_reasons', 'spacing',  'Spacing',  50, true),
    ('movement_reasons', 'flushing', 'Flushing', 60, true),
    ('movement_reasons', 'normal',   'Normal',   70, true),
    ('weight_bands', 'under_20', 'Under 20 kg', 10, false),
    ('weight_bands', 'kg_20_35', '20 to 35 kg', 20, false),
    ('weight_bands', 'kg_35_40', '35 to 40 kg', 30, false),
    ('weight_bands', 'over_40',  'Over 40 kg',  40, false)
) AS v(list, code, name, sort, builtin)
ON CONFLICT DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS public.reference_list_entries;
DROP TABLE IF EXISTS public.reference_lists;
