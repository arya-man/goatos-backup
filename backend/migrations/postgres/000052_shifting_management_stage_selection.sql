-- +goose Up
-- The raiser chooses how management_stage changes. Existing rows remain compatible and preserve
-- the animals' current stage; every new app submission snapshots one concrete target stage.
ALTER TABLE public.shifting_events
    ADD COLUMN IF NOT EXISTS management_stage_mode text,
    ADD COLUMN IF NOT EXISTS target_management_stage text;

ALTER TABLE public.shifting_events
    ADD CONSTRAINT shifting_events_management_stage_mode_check
    CHECK (management_stage_mode IS NULL OR management_stage_mode IN
           ('keep_current', 'select_stage', 'destination_stage')) NOT VALID;

ALTER TABLE public.shifting_events
    VALIDATE CONSTRAINT shifting_events_management_stage_mode_check;

-- +goose Down
-- Intentionally irreversible. Dropping a validated constraint and columns from the hot
-- shifting_events table requires an explicitly reviewed maintenance rollout; an automatic
-- goose rollback would take an unsafe table lock and could discard captured raise-time intent.
SELECT 1;
