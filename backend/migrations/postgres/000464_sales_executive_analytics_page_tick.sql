-- +goose Up
-- seed-fixture-guard:ignore: per-person page ticks for a new Sales page; no source fixture
-- schema, HRMS seed contract, or read-model change.
-- SALES EXECUTIVE ANALYTICS PAGE TICK (maintainer request 2026-10-02). A new page under Sales
-- (/sales/executive-analytics, key sales-executive-analytics) served by the procurement sales-desk activity read.
--
-- Person rows DECIDE after the 2026-08-24 cutover. An EMPTY tick list means every page of the
-- module, so anyone holding Sales with no explicit ticks reaches the new page on their own; a
-- person whose Sales row names its pages explicitly would never see a page shipped after their
-- rows were written. So the key is appended to every explicit web Sales row -- the 000245 /
-- 000309 shape, additive only. The CEO floor makes ceo_internal need no row.
-- seed-migration-guard:ignore owner=manohark issue=sales-executive-analytics reason=additive-page-tick-for-existing-sales-module-holders expiry=2027-03-31
UPDATE public.person_module_access
   SET pages = array_append(pages, 'sales-executive-analytics')
 WHERE surface = 'web'
   AND module_key = 'sales'
   AND pages IS NOT NULL
   AND array_length(pages, 1) > 0
   AND NOT ('sales-executive-analytics' = ANY (pages));

-- Designation defaults are the template a NEW person's rows are written from; the same rule
-- applies, or a person created tomorrow from a template naming the Sales pages would miss it.
-- seed-migration-guard:ignore owner=manohark issue=sales-executive-analytics reason=additive-page-tick-for-designation-templates expiry=2027-03-31
UPDATE public.designation_module_defaults
   SET pages = array_append(pages, 'sales-executive-analytics')
 WHERE surface = 'web'
   AND module_key = 'sales'
   AND pages IS NOT NULL
   AND array_length(pages, 1) > 0
   AND NOT ('sales-executive-analytics' = ANY (pages));

-- +goose Down
-- seed-migration-guard:ignore owner=manohark issue=sales-executive-analytics reason=additive-page-tick-for-existing-sales-module-holders expiry=2027-03-31
UPDATE public.person_module_access
   SET pages = array_remove(pages, 'sales-executive-analytics')
 WHERE surface = 'web' AND module_key = 'sales' AND 'sales-executive-analytics' = ANY (pages);
UPDATE public.designation_module_defaults
   SET pages = array_remove(pages, 'sales-executive-analytics')
 WHERE surface = 'web' AND module_key = 'sales' AND 'sales-executive-analytics' = ANY (pages);
