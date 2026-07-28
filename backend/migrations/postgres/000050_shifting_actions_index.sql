-- +goose Up
-- +goose NO TRANSACTION
-- The Android Actions queue is one row per raised shifting_event. It includes initial work
-- (pending/authorized with no proof) and post-verifier evidence rework, ordered by raised_at.
CREATE INDEX CONCURRENTLY IF NOT EXISTS shifting_events_actions_idx
    ON public.shifting_events (tenant_id, raised_at DESC, shifting_event_id DESC)
    WHERE (((event_status = ANY (ARRAY['pending'::text, 'authorized'::text])) AND proof_ref IS NULL)
        OR verification_state = 'rejected'::text);

CREATE INDEX CONCURRENTLY IF NOT EXISTS shifting_events_actions_park_idx
    ON public.shifting_events (tenant_id, source_park_id, raised_at DESC, shifting_event_id DESC)
    WHERE (((event_status = ANY (ARRAY['pending'::text, 'authorized'::text])) AND proof_ref IS NULL)
        OR verification_state = 'rejected'::text);

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS public.shifting_events_actions_park_idx;
DROP INDEX CONCURRENTLY IF EXISTS public.shifting_events_actions_idx;
