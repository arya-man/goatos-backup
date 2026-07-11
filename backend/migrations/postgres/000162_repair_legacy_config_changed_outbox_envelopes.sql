-- +goose Up
-- +goose StatementBegin
UPDATE outbox_messages
SET schema_version = '1.0.0',
    payload = jsonb_build_object(
      'event_id', event_id,
      'event_type', 'config.changed',
      'schema_version', '1.0.0',
      'schema_ref', 'contracts/jsonschema/domain-event-envelope.schema.json#config.changed',
      'aggregate_type', 'admin_ui_config_family',
      'aggregate_id', aggregate_id,
      'occurred_at', to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
      'recorded_at', to_char(updated_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
      'producer', jsonb_build_object(
        'service', 'postgres',
        'module', 'admin_ui_config_family_revisions',
        'version', NULL
      ),
      'idempotency_key', idempotency_key,
      'actor', jsonb_build_object(
        'actor_type', 'system_rule',
        'actor_id', NULL,
        'actor_ref', NULL
      ),
      'subject_type', 'admin_ui_config_family',
      'subject_id', (payload->>'family_key') || ':' || COALESCE(payload->>'revision', 'unknown'),
      'visibility_scope', jsonb_build_object('tenant_id', tenant_id),
      'evidence_refs', jsonb_build_array(jsonb_build_object(
        'evidence_type', 'event',
        'evidence_id', (payload->>'family_key') || ':' || COALESCE(payload->>'revision', 'unknown')
      )),
      'payload', payload,
      'trace_id', COALESCE(trace_id, idempotency_key, event_id::text)
    ),
    headers = COALESCE(headers, '{}'::jsonb) || jsonb_build_object(
      'producer', 'postgres.admin_ui_config_family_revisions',
      'schema_version', '1.0.0',
      'legacy_repaired_by', '000162_repair_legacy_config_changed_outbox_envelopes'
    ),
    trace_id = COALESCE(trace_id, idempotency_key, event_id::text),
    status = CASE
      WHEN status = 'failed' AND last_error = 'invalid_event_envelope' THEN 'pending'
      ELSE status
    END,
    attempt_count = CASE
      WHEN status = 'failed' AND last_error = 'invalid_event_envelope' THEN 0
      ELSE attempt_count
    END,
    next_attempt_at = CASE
      WHEN status = 'failed' AND last_error = 'invalid_event_envelope' THEN NULL
      ELSE next_attempt_at
    END,
    last_error = CASE
      WHEN status = 'failed' AND last_error = 'invalid_event_envelope' THEN NULL
      ELSE last_error
    END,
    updated_at = now()
WHERE event_type = 'config.changed'
  AND aggregate_type = 'admin_ui_config_family'
  AND payload ? 'family_key'
  AND NOT (payload ? 'event_id');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT 1;
-- +goose StatementEnd
