EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)
SELECT work_state, COALESCE(sum(row_count), 0)::bigint AS row_count
FROM process_integrity_projection_summaries
WHERE tenant_id = :'tenant_id'::uuid
  AND projection_version = 9001
  AND category = 'vaccination'
  AND park_id = ''
  AND shed_id = ''
  AND owner_id = ''
  AND protocol_version_id = ''
  AND due_business_date >= DATE '0001-01-01'
  AND due_business_date <= DATE '9999-12-31'
GROUP BY work_state
ORDER BY work_state;
