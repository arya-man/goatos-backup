-- +goose Up
-- Shifting rewrite: the shift TYPE (category) decides what happens to the animals' tag
-- (maintainer decisions 2026-08-20, docs/features/shifting/shifting-rewrite-tag-rules.md).
--
-- Two schema changes, both additive on a hot table:
--
-- 1. The category vocabulary gains 'spacing' and 'flushing'. The 2026-07-20 taxonomy
--    (growth/health/breeding/delivery) named the REASON a movement was raised but drove nothing;
--    under the rewrite the category IS the rule selector, and two reasons that were previously
--    unrepresentable get their own values:
--      spacing  -- a crowded pen's whole population relocates carrying its tag; the pen empties
--      flushing -- a non-pregnant female moves onto flushing ration and adopts the Flushing tag
--
-- 2. adopt_pen_tag records, at RAISE time, the tag the DESTINATION PEN itself must adopt when the
--    movement applies (spacing or delivery into an empty pen adopts the group's tag; flushing into
--    an empty pen tags it Flushing). NULL means the movement configures nothing. Snapshotting it on
--    the event -- like target_management_stage -- is what lets the park head approve the exact pen
--    configuration the apply will write, and lets the apply transaction re-validate it under the
--    row lock instead of re-deriving a decision nobody approved.
--
-- The CHECK swap follows the baseline's own pattern for this constraint (drop + re-add NOT VALID +
-- VALIDATE) so the table is never rewritten and existing rows are validated without a long lock.

ALTER TABLE public.shifting_events DROP CONSTRAINT IF EXISTS shifting_events_category_check;
ALTER TABLE public.shifting_events
    ADD CONSTRAINT shifting_events_category_check
    CHECK ((category = ANY (ARRAY['growth'::text, 'health'::text, 'breeding'::text, 'delivery'::text, 'spacing'::text, 'flushing'::text]))) NOT VALID;
ALTER TABLE public.shifting_events VALIDATE CONSTRAINT shifting_events_category_check;

ALTER TABLE public.shifting_events
    ADD COLUMN IF NOT EXISTS adopt_pen_tag text;
ALTER TABLE public.shifting_events DROP CONSTRAINT IF EXISTS shifting_events_adopt_pen_tag_check;
ALTER TABLE public.shifting_events
    ADD CONSTRAINT shifting_events_adopt_pen_tag_check
    CHECK ((adopt_pen_tag IS NULL) OR (btrim(adopt_pen_tag) <> '')) NOT VALID;
ALTER TABLE public.shifting_events VALIDATE CONSTRAINT shifting_events_adopt_pen_tag_check;

COMMENT ON COLUMN public.shifting_events.adopt_pen_tag IS
    'Tag the destination pen itself adopts when this movement applies (spacing/delivery/flushing into an empty pen). NULL: the movement configures no pen. Snapshotted at raise so approval and apply see the same decision.';

-- +goose Down
-- Reverting the vocabulary requires no data rewrite: rows raised as spacing/flushing keep their
-- stored category (they record what really happened), so the narrower CHECK is re-added NOT VALID
-- and deliberately NOT validated -- validating would fail on those historical rows.
ALTER TABLE public.shifting_events DROP CONSTRAINT IF EXISTS shifting_events_adopt_pen_tag_check;
ALTER TABLE public.shifting_events DROP COLUMN IF EXISTS adopt_pen_tag;
ALTER TABLE public.shifting_events DROP CONSTRAINT IF EXISTS shifting_events_category_check;
ALTER TABLE public.shifting_events
    ADD CONSTRAINT shifting_events_category_check
    CHECK ((category = ANY (ARRAY['growth'::text, 'health'::text, 'breeding'::text, 'delivery'::text]))) NOT VALID;
