-- +goose Up
-- +goose StatementBegin
-- C35-024: side effects dispatched by the domain consumer (bus.Publish -> handlers) could commit
-- successfully and then, if the finalize step (MarkProcessed) failed or the "processing" claim went
-- stale, redelivery would reclaim the row and re-invoke Publish, replaying non-idempotent handler
-- side effects. 'effects_committed' is a new durable intermediate status the consumer writes right
-- after Publish succeeds and before attempting the terminal 'processed' mark. A redelivery that
-- observes 'effects_committed' must skip Publish entirely and only retry the finalize.
--
-- Lock safety (domain_event_processed_events is a hot table): the new CHECK only ADDS an allowed
-- value, so every existing row already satisfies it. We add it NOT VALID (brief catalog-only
-- ACCESS EXCLUSIVE, no table scan) then VALIDATE CONSTRAINT (SHARE UPDATE EXCLUSIVE, concurrent
-- reads/writes allowed). The unavoidable DROP of the old CHECK is catalog-only and is recorded as
-- reviewed hot-table debt in validate-hot-index-migrations.sh (mirrors 000152).
ALTER TABLE domain_event_processed_events
  DROP CONSTRAINT IF EXISTS domain_event_processed_events_status_check;

ALTER TABLE domain_event_processed_events
  ADD CONSTRAINT domain_event_processed_events_status_check
  CHECK (status IN ('processing', 'effects_committed', 'processed', 'failed'))
  NOT VALID;

ALTER TABLE domain_event_processed_events
  VALIDATE CONSTRAINT domain_event_processed_events_status_check;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE domain_event_processed_events
  DROP CONSTRAINT IF EXISTS domain_event_processed_events_status_check;

ALTER TABLE domain_event_processed_events
  ADD CONSTRAINT domain_event_processed_events_status_check
  CHECK (status IN ('processing', 'processed', 'failed'))
  NOT VALID;

ALTER TABLE domain_event_processed_events
  VALIDATE CONSTRAINT domain_event_processed_events_status_check;
-- +goose StatementEnd
