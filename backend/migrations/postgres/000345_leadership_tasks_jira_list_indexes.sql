-- +goose Up
-- +goose NO TRANSACTION

-- THE LEADERSHIP TASK LIST BECOMES A WORKLIST (2026-09-18): the /tasks page grows a search
-- box, person filters, deadline and raise date ranges, and four sort orders. Three reads the
-- old index set could not serve without a scan of every task the tenant has ever raised:
--
-- 1. TEAM PROGRESS is tenant-wide and names no party, so neither leadership_tasks_assignee_idx
--    nor leadership_tasks_raiser_idx (both keyed on the person after the tenant) can drive its
--    keyset. It needs the tenant-leading keyset on its own.
-- 2. THE DEADLINE SORTS page on (deadline_at, task_id) within a tenant. NULL deadlines sort
--    LAST in both directions -- a task with no deadline is not the most urgent and not the
--    least urgent. That needs TWO indexes, not one: a btree's default is NULLS LAST ascending
--    but NULLS FIRST descending, so the descending order needs its own DESC NULLS LAST index.
--    Measured on 40k tenant rows: with both indexes each direction's first page and each
--    keyset page is an Index Only Scan; with only the ascending one the descending page falls
--    back to a Seq Scan plus a top-N sort of the whole tenant.
-- 3. THE SEARCH BOX matches a substring ANYWHERE in the title or the body: a leader searches
--    for "vendor contract" or for the middle of a sentence they remember, not for a prefix.
--    `title ILIKE '%...%'` is the non-SARGable predicate AGENTS.md bans; the recorded
--    alternative is a pg_trgm GIN index, which DOES serve a leading wildcard. Same shape and
--    extension guard as goat_identifiers_value_trgm_idx (000178) and
--    procurement_vendors_search_trgm_idx (000156). The task_no arm of the same search is
--    already served by leadership_tasks_no_uq.
--
-- Lock-safe: CREATE INDEX CONCURRENTLY outside a transaction. leadership_tasks is not one of
-- the validator's hot tables, but it is the live leadership worklist -- a plain CREATE INDEX
-- would hold a write lock against it for the build -- and the trgm build over the body text is
-- the most expensive of the four.
--
-- Seed coupling note (docs/runbooks/initial-seed-migration-coupling.md): leadership_tasks is an
-- OPERATIONAL table born at runtime; no seed command hand-fills it. This migration adds
-- indexes only and moves no data.
-- seed-fixture-guard:ignore: read-path indexes on a runtime leadership table, not seed input.

CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- 1. Team progress: the tenant-wide keyset the monitor scope pages.
CREATE INDEX CONCURRENTLY IF NOT EXISTS leadership_tasks_tenant_keyset_idx
    ON public.leadership_tasks (tenant_id, raised_at DESC, task_id DESC);

-- 2. The deadline sorts, one index per direction (both read NULLS LAST).
CREATE INDEX CONCURRENTLY IF NOT EXISTS leadership_tasks_tenant_deadline_idx
    ON public.leadership_tasks (tenant_id, deadline_at, task_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS leadership_tasks_tenant_deadline_desc_idx
    ON public.leadership_tasks (tenant_id, deadline_at DESC NULLS LAST, task_id DESC);

-- 3. The search box: substring anywhere in either text column.
CREATE INDEX CONCURRENTLY IF NOT EXISTS leadership_tasks_title_trgm_idx
    ON public.leadership_tasks USING gin (title public.gin_trgm_ops);
CREATE INDEX CONCURRENTLY IF NOT EXISTS leadership_tasks_body_trgm_idx
    ON public.leadership_tasks USING gin (body public.gin_trgm_ops);

-- +goose Down
-- +goose NO TRANSACTION

DROP INDEX CONCURRENTLY IF EXISTS public.leadership_tasks_body_trgm_idx;
DROP INDEX CONCURRENTLY IF EXISTS public.leadership_tasks_title_trgm_idx;
DROP INDEX CONCURRENTLY IF EXISTS public.leadership_tasks_tenant_deadline_desc_idx;
DROP INDEX CONCURRENTLY IF EXISTS public.leadership_tasks_tenant_deadline_idx;
DROP INDEX CONCURRENTLY IF EXISTS public.leadership_tasks_tenant_keyset_idx;

-- pg_trgm is deliberately NOT dropped: it is database-wide and 000156/000178 depend on it.
