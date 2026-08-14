-- +goose Up
-- Pens get their own configured COHORT (the "tag").
--
-- Maintainer decision 2026-08-14: the Counts -> Sheds directory lists operational locations at PEN
-- grain and an operator retags one from that screen, so the tag has to belong to the pen. Until
-- now the only configured cohort was shed_profiles.animal_stage_id, a SHED-level fact -- so
-- retagging "Godel 1 - Part 3" would have silently changed the tag shown against all eight of that
-- shed's pens. The farm's own Sheds DB sheet already records pens separately (Mandela 1 - Part 1 is
-- warmup while Part 2 is Buck), so shed-level was the wrong grain for this fact, not merely an
-- inconvenient one.
--
-- BACKFILLED from the parent shed's profile, so every pen opens with exactly the tag the directory
-- showed the day before this migration and nothing appears to change under the operator. NULL stays
-- possible and means "no cohort configured", which is distinct from any particular cohort.
--
-- WHAT THIS MIGRATION DOES NOT DO, deliberately. shed_profiles.animal_stage_id KEEPS its meaning
-- and its readers: shifting's destination-cohort resolution (identity resolveDestinationTag /
-- ResolveShiftingDestinationStage), vaccination, the feed projection and process integrity all
-- still read the SHED's cohort. Repointing those at the pen changes which stage an animal adopts
-- when it is moved, which is a business rule under the AGENTS.md maintainer lock and needs its own
-- decision. Until that decision exists:
--
--   * editing a PEN's tag writes shed_partitions.animal_stage_id and retags that pen's animals;
--   * editing a shed that HAS NO PENS writes shed_profiles.animal_stage_id, because for such a shed
--     the shed IS the operational location and there is nowhere else for the fact to live;
--   * so a shed's profile can legitimately differ from its pens' tags, and a move into that shed
--     still adopts the SHED's cohort.
--
-- The FK mirrors shed_profiles' exactly -- (tenant_id, animal_stage_id) against the lookup's
-- tenant-scoped unique key -- so a pen can never carry another tenant's cohort.
ALTER TABLE public.shed_partitions
    ADD COLUMN IF NOT EXISTS animal_stage_id uuid;

ALTER TABLE public.shed_partitions
    DROP CONSTRAINT IF EXISTS shed_partitions_animal_stage_tenant_fk;

ALTER TABLE public.shed_partitions
    ADD CONSTRAINT shed_partitions_animal_stage_tenant_fk
    FOREIGN KEY (tenant_id, animal_stage_id)
    REFERENCES public.animal_stage_lookup (tenant_id, animal_stage_id);

-- Backfill from the parent shed. Set-based and bounded (~120 pens); a pen whose shed has no profile
-- stays NULL rather than inheriting a fabricated cohort.
UPDATE public.shed_partitions sp
SET animal_stage_id = profile.animal_stage_id,
    updated_at      = now()
FROM public.shed_profiles profile
WHERE profile.tenant_id = sp.tenant_id
  AND profile.location_id = sp.shed_id
  AND profile.animal_stage_id IS NOT NULL
  AND sp.animal_stage_id IS NULL;

COMMENT ON COLUMN public.shed_partitions.animal_stage_id IS
    'Cohort ("tag") configured for THIS pen. NULL means none configured. Backfilled from the parent shed_profiles row in migration 000161; shifting/vaccination/feed still read the SHED-level shed_profiles.animal_stage_id.';

-- +goose Down
ALTER TABLE public.shed_partitions
    DROP CONSTRAINT IF EXISTS shed_partitions_animal_stage_tenant_fk;

ALTER TABLE public.shed_partitions
    DROP COLUMN IF EXISTS animal_stage_id;
