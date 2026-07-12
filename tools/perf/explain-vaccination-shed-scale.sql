EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)
SELECT
  park_id, park_name, shed_id, shed_name, animals, due_animals,
  open_cells, sessions, capacity_status, shed_status, last_done, next_due,
  count(*) OVER () AS total_count
FROM vaccination_shed_projection_rows
WHERE tenant_id = :'tenant_id'::uuid
  AND projection_version = 9001
ORDER BY park_name, shed_name
LIMIT 51;
