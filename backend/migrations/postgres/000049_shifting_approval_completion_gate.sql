-- +goose Up
-- Shifting applies when BOTH independent facts exist: Park Head approval and operator completion.
-- Verification is post-completion evidence review and never owns goat location or census truth.
-- Completion-before-approval remains event_status='pending'; completed_at/by + proof_ref are the
-- independent completion fact. This avoids redefining a CHECK constraint on the hot table.

ALTER TABLE public.shifting_events
    ADD COLUMN IF NOT EXISTS completed_at timestamp with time zone,
    ADD COLUMN IF NOT EXISTS completed_by uuid;

-- +goose Down
ALTER TABLE public.shifting_events
    DROP COLUMN IF EXISTS completed_by,
    DROP COLUMN IF EXISTS completed_at;
