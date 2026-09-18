-- +goose Up
-- seed-fixture-guard:ignore: durable bulk-import bookkeeping for Configuration -> Items and
-- settings; no vaccination/HRMS seed contract, source fixture schema, or read-model change.
--
-- BULK UPLOAD AT SCALE (maintainer instruction 2026-09-18: "think of it at a scale ... one lakh
-- animals"). A file is never applied inside the request that carried it. It is STAGED row by row
-- into configuration_import_rows, VALIDATED in bounded chunks by a worker, PREVIEWED (counts plus
-- every row error, downloadable), and only then APPLIED -- again in bounded chunks, one idempotent
-- write per row keyed (job, row_no), so a worker that dies mid-file resumes where it stopped and
-- never writes a row twice. A hundred-thousand-row sheet is a few minutes of worker time and a
-- fixed few hundred kilobytes of memory, never a request-sized array.
CREATE TABLE public.configuration_import_jobs (
    job_id        uuid        PRIMARY KEY,
    tenant_id     uuid        NOT NULL REFERENCES public.tenants (tenant_id),
    register_key  text        NOT NULL,
    file_name     text        NOT NULL DEFAULT '',
    file_format   text        NOT NULL CHECK (file_format IN ('csv', 'xlsx')),
    -- validating -> previewed -> applying -> applied; failed / cancelled are terminal.
    status        text        NOT NULL CHECK (status IN ('validating', 'previewed', 'applying', 'applied', 'failed', 'cancelled')),
    total_rows    integer     NOT NULL DEFAULT 0 CHECK (total_rows >= 0),
    valid_rows    integer     NOT NULL DEFAULT 0 CHECK (valid_rows >= 0),
    invalid_rows  integer     NOT NULL DEFAULT 0 CHECK (invalid_rows >= 0),
    applied_rows  integer     NOT NULL DEFAULT 0 CHECK (applied_rows >= 0),
    failed_rows   integer     NOT NULL DEFAULT 0 CHECK (failed_rows >= 0),
    -- The last row_no the current phase has finished, so a resumed worker continues after it.
    progress_row_no integer   NOT NULL DEFAULT 0 CHECK (progress_row_no >= 0),
    error         text        NOT NULL DEFAULT '',
    created_by    text        NOT NULL DEFAULT '',
    claimed_at    timestamptz,
    claimed_by    text        NOT NULL DEFAULT '',
    apply_requested_at timestamptz,
    finished_at   timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);
-- The worker's claim scan and the screen's "recent imports of this register" read.
CREATE INDEX configuration_import_jobs_due_idx
    ON public.configuration_import_jobs (tenant_id, status, claimed_at)
    WHERE status IN ('validating', 'applying');
CREATE INDEX configuration_import_jobs_register_idx
    ON public.configuration_import_jobs (tenant_id, register_key, created_at DESC);

CREATE TABLE public.configuration_import_rows (
    tenant_id uuid    NOT NULL,
    job_id    uuid    NOT NULL REFERENCES public.configuration_import_jobs (job_id) ON DELETE CASCADE,
    row_no    integer NOT NULL CHECK (row_no >= 1),
    -- The row exactly as the file carried it, keyed by the register's column keys.
    fields    jsonb   NOT NULL,
    -- staged -> valid | invalid -> applying -> applied | failed; skipped is a valid row the
    -- apply never reached (a cancel mid-apply).
    state     text    NOT NULL DEFAULT 'staged' CHECK (state IN ('staged', 'valid', 'applying', 'invalid', 'applied', 'failed', 'skipped')),
    errors    jsonb   NOT NULL DEFAULT '[]'::jsonb,
    -- The id of the row the apply wrote (create) or touched (update), for the result sheet.
    result_id text    NOT NULL DEFAULT '',
    PRIMARY KEY (tenant_id, job_id, row_no)
);
-- The preview's per-state pages and the error-sheet stream.
CREATE INDEX configuration_import_rows_state_idx
    ON public.configuration_import_rows (tenant_id, job_id, state, row_no);

-- +goose Down
DROP TABLE IF EXISTS public.configuration_import_rows;
DROP TABLE IF EXISTS public.configuration_import_jobs;
