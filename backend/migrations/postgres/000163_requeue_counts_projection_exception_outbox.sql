-- +goose Up
-- +goose StatementBegin
UPDATE outbox_messages
SET status = 'pending',
    attempt_count = 0,
    next_attempt_at = NULL,
    last_error = NULL,
    updated_at = now()
WHERE event_type = 'counts.projection_exception.opened'
  AND aggregate_type = 'count_projection_exception'
  AND status = 'failed'
  AND last_error = 'invalid_event_envelope';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT 1;
-- +goose StatementEnd
