-- +goose Up
-- projection-review: membership=one row per (tenant_id, location_id, load_ref) authored mapping; group_key=(tenant_id, location_id) in the consumer, which collapses to ONE load only when the shed carries exactly one tag; join_cardinality=locations 1 per location_id (composite FK to the tenant-scoped unique key), and the consumer's HAVING count(*) = 1 makes the tag side 0..1 against a shed so it can never multiply a shed row; pagination=NONE, the mapping is authored reference data bounded by the shed estate; scope=tenant_id, with the consumer additionally bounded by park_id = ANY(...) through weighing_campaigns
--
-- Load-wise growth on the Weights screen (maintainer decision 2026-08-08).
--
-- WHAT THIS IS. Animals are procured in LOADS from named suppliers, and a load is
-- placed into one or more sheds. The farm wants to know which supplier's animals
-- grow best, which is a weighing question asked along a procurement dimension.
--
-- WHY IT IS A WEIGHING TABLE AND NOT A PROCUREMENT READ. Weighing isolation
-- (AGENTS.md) allows weighing exactly three org tables -- locations,
-- workforce_members, user_scope_grants -- and adding procurement_loads to that
-- list is a maintainer decision, not a developer convenience. The mapping the farm
-- actually maintains is SHED-level ("CPT Castro 1 + Castro 2 came from load 131"),
-- not animal-level, so weighing does not need procurement's per-animal tables to
-- answer the question. Storing the shed-level tag here keeps weighing reading only
-- weighing_* plus locations, and the isolation rule stands unchanged.
--
-- THE TRADE-OFF, STATED SO IT IS NOT REDISCOVERED AS A BUG. This is a shed-level
-- attribution. It cannot split one shed's average between two loads, and it does
-- not follow an animal that is later moved to another shed. Both limits are real
-- and are handled by REFUSING to attribute rather than by guessing:
--
--   * The primary key deliberately admits SEVERAL loads per shed, because that is
--     the truth on the ground -- CPT Mandela 1 Part 1 today holds both load 100 and
--     load 101. The consumer then drops that shed from the load rollup (its
--     `HAVING count(*) = 1`) and reports it as unattributed. Splitting one shed
--     average across two loads by head count would invent a distribution nobody
--     measured, the same rule ResolveShiftingDestinationStage already applies to an
--     ambiguous destination cohort.
--   * If per-animal load membership is ever wanted, that is procurement_load_goats
--     populated for real plus a recorded maintainer exception to the isolation
--     lock. It is deliberately NOT what this table grows into.
--
-- NO DATA IS SEEDED HERE. The table ships empty: the supplier mapping is live
-- business data belonging to a tenant, not schema, and committing real counterparty
-- names into a migration would put them in every checkout of this repo.
CREATE TABLE IF NOT EXISTS public.weighing_shed_load_tags (
    tenant_id   uuid        NOT NULL REFERENCES public.tenants (tenant_id),
    -- The SHED (or partition) the load was placed into. A load spanning two sheds
    -- is two rows, which is why load_ref is part of the key rather than a column on
    -- locations.
    location_id uuid        NOT NULL,
    -- The farm's own load number ("131"), kept as text because it is an external
    -- reference the farm types, not a Goat OS identifier. It is NOT a foreign key to
    -- procurement_loads: that would be the cross-module dependency this table exists
    -- to avoid, and those rows do not exist for historical loads.
    load_ref    text        NOT NULL,
    -- Supplier / owner as the farm records it. Free text for the same reason: the
    -- parties table does not carry these counterparties.
    owner_name  text        NOT NULL DEFAULT '',
    -- When the load entered this shed, when known. Advisory only today -- the
    -- consumer attributes a shed's whole weighing history to its tag -- but recorded
    -- so a future time-sliced attribution has the anchor it needs.
    placed_on   date,
    notes       text        NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, location_id, load_ref),
    CONSTRAINT weighing_shed_load_tags_load_ref_not_blank CHECK (btrim(load_ref) <> ''),
    CONSTRAINT weighing_shed_load_tags_location_tenant_fk
        FOREIGN KEY (tenant_id, location_id) REFERENCES public.locations (tenant_id, location_id)
);

-- The consumer enters by tenant and looks the tag up per shed, so the PK's leading
-- (tenant_id, location_id) already serves it. This index serves the other direction
-- -- "which sheds hold load 131" -- which the load rollup uses to count sheds and a
-- future load detail view will need.
CREATE INDEX IF NOT EXISTS weighing_shed_load_tags_load_idx
    ON public.weighing_shed_load_tags (tenant_id, load_ref, location_id);

-- +goose Down
DROP INDEX IF EXISTS public.weighing_shed_load_tags_load_idx;
DROP TABLE IF EXISTS public.weighing_shed_load_tags;
