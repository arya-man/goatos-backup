-- Park-scope drift check (read-only). Maintainer decision 2026-09-04: the People screen
-- ticks are the ONE authored park scope; grants and the home park are derived. Every row
-- this prints is a person whose records disagree and must be fixed through the People
-- screen (never by editing a grant row by hand).
--
--   psql "$DATABASE_URL" -f tools/dev/park-scope-drift.sql
WITH parks AS (SELECT location_id, name FROM locations WHERE location_type = 'park'),
person AS (
  SELECT m.tenant_id, m.workforce_member_id, m.user_id, m.display_name,
         a.scope_mode,
         coalesce((SELECT array_agg(ps.park_id ORDER BY ps.park_id) FROM person_park_scope ps
                    WHERE ps.tenant_id = m.tenant_id AND ps.workforce_member_id = m.workforce_member_id), '{}') AS tick_parks,
         m.primary_location_id AS home_park
    FROM workforce_members m
    JOIN person_access a ON a.tenant_id = m.tenant_id AND a.workforce_member_id = m.workforce_member_id
   WHERE m.status = 'active' AND m.user_id IS NOT NULL
),
grants AS (
  SELECT g.user_id, g.role,
         bool_or(g.scope_type = 'tenant') AS tenant_row,
         coalesce(array_agg(g.scope_id ORDER BY g.scope_id) FILTER (WHERE g.scope_type = 'park'), '{}') AS park_rows
    FROM user_scope_grants g
   WHERE g.status = 'active' AND g.scope_type IN ('tenant', 'park')
   GROUP BY g.user_id, g.role
)
SELECT p.display_name, gr.role, p.scope_mode,
       (SELECT string_agg(name, ',') FROM parks WHERE location_id = ANY(p.tick_parks)) AS ticks,
       CASE WHEN gr.tenant_row THEN 'tenant' ELSE (SELECT string_agg(name, ',') FROM parks WHERE location_id = ANY(gr.park_rows)) END AS grant_scope,
       (SELECT name FROM parks WHERE location_id = p.home_park) AS home_park,
       CASE
         WHEN p.scope_mode = 'tenant' AND NOT gr.tenant_row THEN 'tenant person with a park-scoped role row'
         WHEN p.scope_mode = 'parks' AND gr.tenant_row THEN 'parks person with a tenant-scoped role row'
         WHEN p.scope_mode = 'parks' AND gr.park_rows <> p.tick_parks THEN 'grant parks differ from ticks'
         WHEN p.scope_mode = 'parks' AND NOT (p.home_park = ANY(p.tick_parks)) THEN 'home park is not a ticked park'
       END AS drift
  FROM person p
  JOIN grants gr ON gr.user_id = p.user_id
 WHERE (p.scope_mode = 'tenant' AND NOT gr.tenant_row)
    OR (p.scope_mode = 'parks' AND (gr.tenant_row OR gr.park_rows <> p.tick_parks OR NOT (p.home_park = ANY(p.tick_parks))))
 ORDER BY p.display_name, gr.role;
