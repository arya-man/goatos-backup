-- +goose Up
-- seed-fixture-guard:ignore: durable bulk-import bookkeeping for Configuration -> Items and
-- settings (the onboarding WORKBOOK); no vaccination/HRMS seed contract, source fixture schema,
-- or read-model change.
--
-- ONBOARDING WORKBOOK (maintainer instruction 2026-09-19). A farm is set up from ONE Excel file
-- with one tab per register -- Parks, Pens, Partitions, Species, Gender, Lifecycle stages, Breeds,
-- Lists, Items & categories, ..., Animals -- instead of one upload per list. Each tab is staged as
-- an ordinary configuration_import_jobs row (the same validate -> preview -> apply pipeline as
-- migration 000349, the same chunked, resumable, idempotent mechanics), and the bundle row below
-- ties them together: one file name, one status, one Apply. Tabs are validated and applied in a
-- FIXED DEPENDENCY ORDER (bundle_order), so a Pens row may name a park that only exists on the
-- Parks tab of the same workbook: at validation it resolves to a token naming that row, at apply
-- the token resolves to the row the Parks tab actually wrote. A tab waits its turn in the new
-- 'queued' job status, which the recovery sweep deliberately never picks up on its own.
CREATE TABLE public.configuration_import_bundles (
    bundle_id     uuid        PRIMARY KEY,
    tenant_id     uuid        NOT NULL REFERENCES public.tenants (tenant_id),
    file_name     text        NOT NULL DEFAULT '',
    -- validating -> previewed -> applying -> applied; failed / cancelled are terminal.
    status        text        NOT NULL CHECK (status IN ('validating', 'previewed', 'applying', 'applied', 'failed', 'cancelled')),
    -- Worksheet names the workbook carried that matched no register (reported, never imported).
    unknown_sheets jsonb      NOT NULL DEFAULT '[]'::jsonb,
    error         text        NOT NULL DEFAULT '',
    created_by    text        NOT NULL DEFAULT '',
    apply_requested_at timestamptz,
    finished_at   timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);
-- The screen's "recent workbooks" read and the recovery sweep's "bundles still in flight" scan.
CREATE INDEX configuration_import_bundles_recent_idx
    ON public.configuration_import_bundles (tenant_id, created_at DESC);
CREATE INDEX configuration_import_bundles_due_idx
    ON public.configuration_import_bundles (tenant_id, status)
    WHERE status IN ('validating', 'applying');

ALTER TABLE public.configuration_import_jobs
    ADD COLUMN bundle_id    uuid    REFERENCES public.configuration_import_bundles (bundle_id) ON DELETE CASCADE,
    ADD COLUMN bundle_order integer NOT NULL DEFAULT 0 CHECK (bundle_order >= 0),
    -- The worksheet the tab came from (blank for a single-sheet upload).
    ADD COLUMN sheet_name   text    NOT NULL DEFAULT '';

-- 'queued': a workbook tab waiting for the tabs before it. Never claimed by the job sweep; the
-- bundle orchestrator promotes it to validating / applying when its turn comes.
ALTER TABLE public.configuration_import_jobs DROP CONSTRAINT configuration_import_jobs_status_check;
ALTER TABLE public.configuration_import_jobs
    ADD CONSTRAINT configuration_import_jobs_status_check
    CHECK (status IN ('queued', 'validating', 'previewed', 'applying', 'applied', 'failed', 'cancelled'));

-- The orchestrator's walk over a bundle's tabs, in order.
CREATE INDEX configuration_import_jobs_bundle_idx
    ON public.configuration_import_jobs (tenant_id, bundle_id, bundle_order)
    WHERE bundle_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS public.configuration_import_jobs_bundle_idx;
ALTER TABLE public.configuration_import_jobs DROP CONSTRAINT configuration_import_jobs_status_check;
ALTER TABLE public.configuration_import_jobs
    ADD CONSTRAINT configuration_import_jobs_status_check
    CHECK (status IN ('validating', 'previewed', 'applying', 'applied', 'failed', 'cancelled'));
ALTER TABLE public.configuration_import_jobs
    DROP COLUMN IF EXISTS sheet_name,
    DROP COLUMN IF EXISTS bundle_order,
    DROP COLUMN IF EXISTS bundle_id;
DROP TABLE IF EXISTS public.configuration_import_bundles;
