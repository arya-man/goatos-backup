-- +goose Up
-- seed-fixture-guard:ignore: repairs per-person page ticks for an existing module; no
-- source fixture schema, HRMS seed contract, or read-model change.
-- Rewrite the retired Sales board tick into the two pages it was divided into.
--
-- WHY THIS EXISTS
--
-- The Sales board (/sales, page key `sales-board`) carried the live-herd valuation, the
-- closed-sale blocks and the deals ledger on one screen. On 2026-09-11 the maintainer divided
-- it into /sales/sold ("Sold": what has sold, with the ledger last) and /sales/farm-value
-- ("Farm value": the live-herd valuation) and retired the board; /sales now redirects to Sold.
-- Nothing new was built -- the same blocks, re-homed.
--
-- A person's sidebar is exactly the pages ticked for them on /people (maintainer decision
-- 2026-08-27). An EMPTY tick list means every page of the module, so anyone holding Sales
-- with no explicit ticks reaches both new pages on their own. But a person whose Sales row
-- names its pages explicitly -- `{sales-board, sales-loads, sales-config}` -- holds a key that
-- no longer names a page: the catalog drops it, so they would lose the blocks they could see
-- yesterday. That is not a decision anyone made on the access screen; it is the snapshot
-- going stale, the same shape the weighing-analytics tick went stale in.
--
-- So: where a stored Sales row ticks the retired board, REPLACE that key with the two pages
-- it became. A row that deliberately withholds the board (ticks Purchase and Born alone, say)
-- is left exactly as it is -- someone who was not shown the board is not shown its halves.
--
-- Idempotent: the retired key is removed and a key already present is not appended twice,
-- which is also what keeps the distinct-array CHECK satisfied.
UPDATE public.person_module_access
SET pages = array_remove(pages, 'sales-board')
    || CASE WHEN 'sales-sold' = ANY(pages) THEN '{}'::text[] ELSE ARRAY['sales-sold'] END
    || CASE WHEN 'sales-farm-value' = ANY(pages) THEN '{}'::text[] ELSE ARRAY['sales-farm-value'] END
WHERE surface = 'web'
  AND module_key = 'sales'
  AND 'sales-board' = ANY(pages);

-- Designation defaults are the template a NEW person's rows are written from; the same rule
-- applies, or a person created tomorrow from a template ticking the board would get a dead key.
UPDATE public.designation_module_defaults
SET pages = array_remove(pages, 'sales-board')
    || CASE WHEN 'sales-sold' = ANY(pages) THEN '{}'::text[] ELSE ARRAY['sales-sold'] END
    || CASE WHEN 'sales-farm-value' = ANY(pages) THEN '{}'::text[] ELSE ARRAY['sales-farm-value'] END
WHERE surface = 'web'
  AND module_key = 'sales'
  AND 'sales-board' = ANY(pages);

-- +goose Down
-- Puts the board key back where either of its halves is ticked and removes the two keys this
-- migration wrote. A tick an admin added on /people for either page is indistinguishable from
-- one written here, which is the honest cost of an additive repair; Down is the rollback of the
-- release, not an access edit.
UPDATE public.person_module_access
SET pages = array_remove(array_remove(pages, 'sales-sold'), 'sales-farm-value')
    || CASE WHEN 'sales-board' = ANY(pages) THEN '{}'::text[] ELSE ARRAY['sales-board'] END
WHERE surface = 'web' AND module_key = 'sales'
  AND ('sales-sold' = ANY(pages) OR 'sales-farm-value' = ANY(pages));

UPDATE public.designation_module_defaults
SET pages = array_remove(array_remove(pages, 'sales-sold'), 'sales-farm-value')
    || CASE WHEN 'sales-board' = ANY(pages) THEN '{}'::text[] ELSE ARRAY['sales-board'] END
WHERE surface = 'web' AND module_key = 'sales'
  AND ('sales-sold' = ANY(pages) OR 'sales-farm-value' = ANY(pages));
