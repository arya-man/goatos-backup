-- +goose Up
-- +goose NO TRANSACTION
-- Normal shifting (maintainer decision 2026-09-12): a seventh shift type, 'normal', joins the
-- category vocabulary. It is the plain move -- any selection of animals, the tag never changes, no
-- pen is re-tagged -- accepted only into an EMPTY pen or one already holding an animal with the
-- moving animals' tag. Rulebook: backend/internal/counts/domain/shifting_type.go
-- (resolveNormalShift); canonical prose: docs/features/shifting/shifting-rewrite-tag-rules.md.
--
-- Same lock-safe CHECK swap shape as 000179: ADD ... NOT VALID takes only a short metadata lock,
-- VALIDATE runs after goose's autocommit released it.
SET lock_timeout = '5s';
ALTER TABLE public.shifting_events DROP CONSTRAINT IF EXISTS shifting_events_category_check;
ALTER TABLE public.shifting_events
    ADD CONSTRAINT shifting_events_category_check
    CHECK ((category = ANY (ARRAY['growth'::text, 'health'::text, 'breeding'::text, 'delivery'::text, 'spacing'::text, 'flushing'::text, 'normal'::text]))) NOT VALID;
ALTER TABLE public.shifting_events VALIDATE CONSTRAINT shifting_events_category_check;
RESET lock_timeout;

-- +goose Down
-- +goose NO TRANSACTION
-- Rows raised as 'normal' keep their stored category (they record what really happened), so the
-- narrower CHECK is re-added NOT VALID and deliberately NOT validated.
SET lock_timeout = '5s';
ALTER TABLE public.shifting_events DROP CONSTRAINT IF EXISTS shifting_events_category_check;
ALTER TABLE public.shifting_events
    ADD CONSTRAINT shifting_events_category_check
    CHECK ((category = ANY (ARRAY['growth'::text, 'health'::text, 'breeding'::text, 'delivery'::text, 'spacing'::text, 'flushing'::text]))) NOT VALID;
RESET lock_timeout;
