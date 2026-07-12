EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)
SELECT row_id, work_state, severity, due_at
FROM process_integrity_projection_rows
WHERE tenant_id = :'tenant_id'::uuid
  AND projection_version = 9001
  AND category = 'vaccination'
  AND work_state <> 'completed'
  AND due_at <= now() + interval '183 days'
ORDER BY sort_priority, due_at, row_id
LIMIT 51;
