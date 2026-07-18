SELECT pg_stat_clear_snapshot();

SELECT json_build_object(
  'captured_at', now(),
  'tables', COALESCE(json_object_agg(
    relname,
    json_build_object(
      'seq_scan', seq_scan,
      'idx_scan', idx_scan,
      'seq_tup_read', seq_tup_read,
      'idx_tup_fetch', idx_tup_fetch
    )
  ), '{}'::json)
)
FROM pg_stat_user_tables
WHERE relname IN (
  'goats',
  'obligation_instances',
  'vaccination_completions',
  'protocol_rules',
  'locations',
  'process_integrity_projection_rows',
  'process_integrity_projection_summaries',
  'calendar_event_projections',
  'vaccination_shed_projection_rows',
  'vaccination_execution_projection_rows',
  'vaccination_operations_projection_rows'
);
