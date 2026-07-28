-- +goose Up
-- +goose NO TRANSACTION
-- Birth work is delivery-grained, not goat-lifetime-grained. A dam must receive a fresh mother
-- workflow for every litter while redelivery of the same goat.created event remains idempotent.
SET lock_timeout = '5s';
SET statement_timeout = '60s';

ALTER TABLE public.workflow_instances ADD COLUMN IF NOT EXISTS birth_event_id uuid;

UPDATE public.workflow_instances wi
SET birth_event_id = gb.birth_event_id
FROM public.goat_births gb
WHERE wi.tenant_id = gb.tenant_id AND wi.template_key = 'birth_kid'
  AND wi.subject_goat_id = gb.child_goat_id AND wi.birth_event_id IS NULL;

UPDATE public.workflow_instances wi
SET birth_event_id = gb.birth_event_id
FROM public.goat_births gb
WHERE wi.tenant_id = gb.tenant_id AND wi.template_key = 'birth_mother'
  AND wi.subject_goat_id = gb.mother_goat_id AND wi.dam_goat_id = gb.child_goat_id
  AND wi.birth_event_id IS NULL;

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS workflow_instances_birth_event_uq
  ON public.workflow_instances (tenant_id, template_key, subject_goat_id, birth_event_id)
  WHERE template_key IN ('birth_kid', 'birth_mother') AND birth_event_id IS NOT NULL;

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS workflow_instances_birth_legacy_uq
  ON public.workflow_instances (tenant_id, template_key, subject_goat_id)
  WHERE template_key IN ('birth_kid', 'birth_mother') AND birth_event_id IS NULL;

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS workflow_instances_death_uq
  ON public.workflow_instances (tenant_id, template_key, subject_goat_id)
  WHERE template_key = 'death';

DROP INDEX CONCURRENTLY IF EXISTS public.workflow_instances_natural_uq;

-- +goose Down
-- Multiple legitimate litter workflows may now exist for one dam, so restoring the lifetime key
-- would be destructive. Rollback removes only the additive event-grain indexes/column when safe.
-- +goose NO TRANSACTION
SET lock_timeout = '5s';
SET statement_timeout = '60s';
DROP INDEX CONCURRENTLY IF EXISTS public.workflow_instances_birth_event_uq;
DROP INDEX CONCURRENTLY IF EXISTS public.workflow_instances_birth_legacy_uq;
DROP INDEX CONCURRENTLY IF EXISTS public.workflow_instances_death_uq;
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS workflow_instances_natural_uq
  ON public.workflow_instances (tenant_id, template_key, subject_goat_id);
ALTER TABLE public.workflow_instances DROP COLUMN IF EXISTS birth_event_id;
