-- +goose Up
-- +goose NO TRANSACTION
-- seed-migration-guard:ignore owner=counts issue=birth-rejection-2026-09-25 reason=widens-one-check-vocabulary-no-seed-owned-rows-or-seed-path-change expiry=2027-03-31
-- seed-fixture-guard:ignore: a CHECK vocabulary widening; it seeds no rows and no fixture.
--
-- A REJECTED BIRTH RETIRES THE KID IT CREATED (maintainer decision 2026-09-25, superseding the
-- "existing children remain canonical but count-ineligible" row of
-- docs/decisions/birth-death-workflows.md). The birth report creates its kids at submit; when the
-- approver rejects the report, each kid leaves the live register through identity's canonical
-- terminal exit (goat.exited) with lifecycle_status 'inactive' (already an exited status) and the
-- NEW exit_reason 'recorded_in_error' -- none of sold / died / culled / transferred / lost is true
-- of an animal that was recorded by mistake. The row is kept for audit.
--
-- Same lock-safe CHECK swap as 000294: ADD ... NOT VALID takes only a short metadata lock, VALIDATE
-- runs after goose's autocommit released it (every existing value is in the wider list).
SET lock_timeout = '5s';
ALTER TABLE public.goats DROP CONSTRAINT IF EXISTS goats_exit_reason_check;
ALTER TABLE public.goats
    ADD CONSTRAINT goats_exit_reason_check
    CHECK (((exit_reason IS NULL) OR (exit_reason = ANY (ARRAY['sold'::text, 'died'::text, 'culled'::text, 'transferred'::text, 'lost'::text, 'recorded_in_error'::text])))) NOT VALID;
ALTER TABLE public.goats VALIDATE CONSTRAINT goats_exit_reason_check;
RESET lock_timeout;

-- +goose Down
-- +goose NO TRANSACTION
-- Rows retired as 'recorded_in_error' keep their stored reason (they record what really happened),
-- so the narrower CHECK is re-added NOT VALID and deliberately NOT validated.
SET lock_timeout = '5s';
ALTER TABLE public.goats DROP CONSTRAINT IF EXISTS goats_exit_reason_check;
ALTER TABLE public.goats
    ADD CONSTRAINT goats_exit_reason_check
    CHECK (((exit_reason IS NULL) OR (exit_reason = ANY (ARRAY['sold'::text, 'died'::text, 'culled'::text, 'transferred'::text, 'lost'::text])))) NOT VALID;
RESET lock_timeout;
