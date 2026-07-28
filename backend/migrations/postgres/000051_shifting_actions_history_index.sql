-- +goose Up
-- +goose NO TRANSACTION
CREATE INDEX CONCURRENTLY IF NOT EXISTS shifting_events_actions_history_idx
ON public.shifting_events (tenant_id, raised_at DESC, shifting_event_id DESC)
WHERE event_status <> 'canceled';

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS public.shifting_events_actions_history_idx;
