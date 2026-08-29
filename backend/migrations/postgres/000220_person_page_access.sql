-- +goose Up
--
-- PAGE-GRAIN ACCESS (People/HRMS rewrite, maintainer decision 2026-08-27).
--
-- 000219 made access assignable per MODULE. A module tick cannot say "Feed
-- Analytics and Feed SOP, but not Feed Config" -- and that sentence is the actual
-- job of the Procurement Director. It was therefore written as a hand-coded LENS
-- over the compiled admin-web contract (adminui/app/procurement_director_lens.go,
-- maintainer decision 2026-08-21), which meant every future "this person should
-- not see that page" was a code change.
--
-- That lens is RETIRED in this change. The narrowing it performed becomes DATA in
-- the column below, editable on /people. The behaviour is preserved exactly: the
-- backfill writes the Procurement Director the same six pages the lens left him,
-- and TestRetiredProcurementDirectorLensIsReproducedByTicks asserts it.
--
-- WEB ONLY. The phone composes its navigation from the mobile module registry and
-- is deliberately untouched (maintainer instruction 2026-08-27: "operator and
-- phone nothing should change"). A mobile row's pages column is unused.
--
-- EMPTY MEANS EVERY PAGE OF THE MODULE. This is what makes a page shipped
-- tomorrow reach whoever already holds the module, instead of silently reaching
-- nobody until someone re-ticks 30 people. Narrowing is opt-in: a stored list is
-- a deliberate "these, and not the rest".
--
-- The page keys are NOT a foreign key, for the same reason module_key is not: the
-- page catalog lives in Go beside the navigation it filters
-- (permissions.ModulePages, asserted against the real navigation() by
-- TestEveryNavLeafIsATickablePage). An unknown key grants NOTHING, so the failure
-- mode of a stale row is a missing page, which is visible and reported.

ALTER TABLE public.person_module_access
  ADD COLUMN IF NOT EXISTS pages text[] NOT NULL DEFAULT '{}';

ALTER TABLE public.designation_module_defaults
  ADD COLUMN IF NOT EXISTS pages text[] NOT NULL DEFAULT '{}';

-- No duplicates, same reason as capabilities in 000215: a repeated element
-- changes nothing about what resolves but makes the stored row disagree with what
-- the screen shows. Reuses the IMMUTABLE helper 000219 declared, because
-- Postgres refuses a subquery in a CHECK.
ALTER TABLE public.person_module_access
  DROP CONSTRAINT IF EXISTS person_module_access_pages_distinct_check;
ALTER TABLE public.person_module_access
  ADD CONSTRAINT person_module_access_pages_distinct_check
  CHECK (public.goatos_text_array_is_distinct(pages));

ALTER TABLE public.designation_module_defaults
  DROP CONSTRAINT IF EXISTS designation_module_defaults_pages_distinct_check;
ALTER TABLE public.designation_module_defaults
  ADD CONSTRAINT designation_module_defaults_pages_distinct_check
  CHECK (public.goatos_text_array_is_distinct(pages));

-- +goose Down
ALTER TABLE public.designation_module_defaults
  DROP CONSTRAINT IF EXISTS designation_module_defaults_pages_distinct_check;
ALTER TABLE public.person_module_access
  DROP CONSTRAINT IF EXISTS person_module_access_pages_distinct_check;
ALTER TABLE public.designation_module_defaults DROP COLUMN IF EXISTS pages;
ALTER TABLE public.person_module_access DROP COLUMN IF EXISTS pages;
