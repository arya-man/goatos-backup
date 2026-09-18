-- +goose Up
-- seed-fixture-guard:ignore: drops a CHECK whose values the movement_reasons reference list now
-- carries (migration 000348); no vaccination/HRMS seed contract, source fixture schema, or
-- read-model change.
--
-- MOVEMENT REASONS ARE THE FARM'S LIST (maintainer instruction 2026-09-18, "whatever we add
-- here should be reflected in those fields"). The seven typed-raise categories keep their tag
-- rules in counts/domain.ResolveShiftTypeDecision and are the built-in entries of the
-- movement_reasons list; a reason the farm adds is accepted by the raise as a plain move (the
-- 'normal' rule: nothing stamped) once it is an ACTIVE entry of that list. The column-level
-- CHECK therefore goes; the raise handler validates the shape, the list validates membership.
SET lock_timeout = '5s';
ALTER TABLE public.shifting_events DROP CONSTRAINT IF EXISTS shifting_events_category_check;
RESET lock_timeout;

-- +goose Down
SET lock_timeout = '5s';
ALTER TABLE public.shifting_events
    ADD CONSTRAINT shifting_events_category_check
    CHECK ((category = ANY (ARRAY['growth'::text, 'health'::text, 'breeding'::text, 'delivery'::text, 'spacing'::text, 'flushing'::text, 'normal'::text]))) NOT VALID;
RESET lock_timeout;
