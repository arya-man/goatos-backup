-- +goose Up
-- Shifting-event taxonomy realigned to the GOVERNING product docs (maintainer decision 2026-07-20,
-- "Governing docs win"). The Shifting Reports / Feed-Shiftings-Count source documents define:
--
--   * Priority: High / Low            (was: normal / high / emergency)
--   * Category: Growth / Health / Breeding / Delivery
--                                      (was: routine / high_priority / pregnancy / warmup /
--                                            medical / quarantine / other)
--
-- The prior free-mixed vocabulary is retired. This forward migration remaps every existing
-- shifting_events row to the governing values and swaps the CHECK constraints. shifting_events is a
-- bounded operational table (one row per authored movement), so a validated constraint swap is
-- lock-safe here; it is not one of the large import/goat/event/obligation tables.
--
-- Value mapping (documented so the remap is auditable and the code paths agree):
--
--   PRIORITY   old 'emergency' -> 'high'   (fast lane: keeps the 1-day feed-follow lead + the
--                                            documented high-priority source-ration bridge)
--              old 'high'      -> 'high'    (queue-order signal folds into the fast lane)
--              old 'normal'    -> 'low'     (standard 2-day feed-follow lead)
--
--   CATEGORY   old 'routine'       -> 'growth'
--              old 'high_priority' -> 'growth'
--              old 'other'         -> 'growth'
--              old 'warmup'        -> 'breeding'   (reproductive-readiness prep)
--              old 'pregnancy'     -> 'breeding'
--              old 'medical'       -> 'health'
--              old 'quarantine'    -> 'health'
--              ('delivery' is a new governing category for birth/K0-driven moves; no legacy row maps
--               to it, but it is a valid selectable value going forward.)
--
-- Order matters: DROP the old CHECKs first (the new values violate the OLD allow-list), UPDATE the
-- data, then ADD the governing CHECKs.

-- Lock-safe constraint re-definition (validate-hot-index-migrations): DROP the old CHECK, remap the
-- data, ADD the governing CHECK as NOT VALID (no full-table ACCESS EXCLUSIVE scan on ADD), then
-- VALIDATE it as a separate statement (SHARE UPDATE EXCLUSIVE, does not block writes). shifting_events
-- is bounded, but the safe pattern is used regardless so the guard's hot-table contract holds.
ALTER TABLE public.shifting_events DROP CONSTRAINT IF EXISTS shifting_events_priority_check;
ALTER TABLE public.shifting_events DROP CONSTRAINT IF EXISTS shifting_events_category_check;

UPDATE public.shifting_events
SET priority = CASE priority
        WHEN 'emergency' THEN 'high'
        WHEN 'high'      THEN 'high'
        WHEN 'normal'    THEN 'low'
        ELSE 'low'
    END
WHERE priority NOT IN ('high', 'low');

UPDATE public.shifting_events
SET category = CASE category
        WHEN 'routine'       THEN 'growth'
        WHEN 'high_priority' THEN 'growth'
        WHEN 'other'         THEN 'growth'
        WHEN 'warmup'        THEN 'breeding'
        WHEN 'pregnancy'     THEN 'breeding'
        WHEN 'medical'       THEN 'health'
        WHEN 'quarantine'    THEN 'health'
        ELSE 'growth'
    END
WHERE category NOT IN ('growth', 'health', 'breeding', 'delivery');

ALTER TABLE public.shifting_events
    ADD CONSTRAINT shifting_events_priority_check
    CHECK ((priority = ANY (ARRAY['high'::text, 'low'::text]))) NOT VALID;
ALTER TABLE public.shifting_events VALIDATE CONSTRAINT shifting_events_priority_check;

ALTER TABLE public.shifting_events
    ADD CONSTRAINT shifting_events_category_check
    CHECK ((category = ANY (ARRAY['growth'::text, 'health'::text, 'breeding'::text, 'delivery'::text]))) NOT VALID;
ALTER TABLE public.shifting_events VALIDATE CONSTRAINT shifting_events_category_check;

-- +goose Down
-- Best-effort reverse: restore the pre-governing allow-lists and map values back. The forward remap
-- is lossy (growth <- routine/high_priority/other), so Down picks a representative legacy value.
ALTER TABLE public.shifting_events DROP CONSTRAINT IF EXISTS shifting_events_priority_check;
ALTER TABLE public.shifting_events DROP CONSTRAINT IF EXISTS shifting_events_category_check;

UPDATE public.shifting_events
SET priority = CASE priority WHEN 'high' THEN 'high' WHEN 'low' THEN 'normal' ELSE 'normal' END;

UPDATE public.shifting_events
SET category = CASE category
        WHEN 'growth'   THEN 'routine'
        WHEN 'health'   THEN 'medical'
        WHEN 'breeding' THEN 'pregnancy'
        WHEN 'delivery' THEN 'routine'
        ELSE 'routine'
    END;

ALTER TABLE public.shifting_events
    ADD CONSTRAINT shifting_events_priority_check
    CHECK ((priority = ANY (ARRAY['normal'::text, 'high'::text, 'emergency'::text]))) NOT VALID;
ALTER TABLE public.shifting_events VALIDATE CONSTRAINT shifting_events_priority_check;

ALTER TABLE public.shifting_events
    ADD CONSTRAINT shifting_events_category_check
    CHECK ((category = ANY (ARRAY['routine'::text, 'high_priority'::text, 'pregnancy'::text, 'warmup'::text, 'medical'::text, 'quarantine'::text, 'other'::text]))) NOT VALID;
ALTER TABLE public.shifting_events VALIDATE CONSTRAINT shifting_events_category_check;
