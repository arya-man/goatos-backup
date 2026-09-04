-- +goose Up
-- The Weights leaf was renamed on 2026-09-03 (commit 8e5a18468): page key
-- `weighing-weights` was parked and `weighing-analytics` (ADG Analytics) took its place.
-- Stored page ticks were never remapped, so everyone whose weighing ticks were an explicit
-- list from before the rename lost the screen (the CXO and the Growth Director on STG).
-- permissions.RetiredPageKeys now resolves the old key at read time; this migration makes
-- the stored data say the new name too. Idempotent.
UPDATE public.person_module_access
   SET pages = (SELECT array_agg(DISTINCT p ORDER BY p)
                  FROM unnest(array_replace(pages, 'weighing-weights', 'weighing-analytics')) AS p),
       updated_at = now()
 WHERE 'weighing-weights' = ANY(pages);

UPDATE public.designation_module_defaults
   SET pages = (SELECT array_agg(DISTINCT p ORDER BY p)
                  FROM unnest(array_replace(pages, 'weighing-weights', 'weighing-analytics')) AS p)
 WHERE 'weighing-weights' = ANY(pages);

-- +goose Down
-- Deliberately empty: the old key no longer names a screen, so writing it back would only
-- recreate the lost-page state.
SELECT 1;
