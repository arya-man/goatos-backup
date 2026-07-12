EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)
SELECT event_id, status, severity, due_at
FROM calendar_event_projections
WHERE tenant_id = :'tenant_id'::uuid
  AND slice_key = 'vaccination'
  AND system = false
  AND due_at >= date_trunc('day', now()) - interval '7 days'
  AND due_at < date_trunc('day', now()) + interval '39 days'
  AND event_type <> 'vaccination_dose_due'
  AND status NOT IN ('completed', 'canceled')
ORDER BY due_at, event_id
LIMIT 51;
