# Cube Core — staging (`mesha-cube-stg`) deploy notes

**Docs only. No deploy is performed from here.** Prove locally first
(`docs/runbooks/cube-local.md`), then a maintainer promotes through the normal
Goat OS deploy path. Verify `account=ravi@mesha.sg`, `project=goatos-stg`,
org `vgoats.com` before any cloud mutation.

## Target service

```
Service:          mesha-cube-stg   (later mesha-cube-prod)
Region:           asia-south1
Runtime:          Cloud Run
Ingress:          internal / authenticated service-to-service only
Service account:  mesha-cube-stg
Cloud SQL role:   Cloud SQL Client
Secret access:    Cube API secret/JWT secret + read-only DB credential only
Called by:        the Mesha backend ONLY — never the browser, never MCP, never Vertex
```

Cube queries stg Cloud SQL Postgres through **`mesha_cube_readonly`**. As
BigQuery/dbt marts land, historical official metrics can point to BigQuery while
current-state metrics stay on Postgres/read models — with no change to the metric
formulas in `analytics/cube/model/**`.

## Image & config

- Image: `cubejs/cube` (pin the same tag used locally in
  `tools/dev/run-cube-local.sh`, currently `v1.3.63`).
- Mount `analytics/cube/cube.js` + `analytics/cube/model/` into `/cube/conf`
  (bake into the image or mount from a config source). Set
  `CUBEJS_DEV_MODE=false` in stg.
- Env (values from Secret Manager, project `goatos-stg`; never committed):
  `CUBEJS_API_SECRET` ← `MESHA_CUBE_API_SECRET`,
  `CUBEJS_DB_TYPE=postgres`, `CUBEJS_DB_*` ← `MESHA_CUBE_DB_*`
  (`MESHA_CUBE_DB_USER=mesha_cube_readonly`). Connect Cloud SQL via the
  `/cloudsql` connector, same pattern as the app service.

## Read-only DB role (owned by the ceo_ai schema migration, not this task)

Cube must connect as a non-privileged, read-only role:

```sql
CREATE ROLE mesha_cube_readonly LOGIN PASSWORD '<secret>';
ALTER ROLE mesha_cube_readonly SET default_transaction_read_only = on;
ALTER ROLE mesha_cube_readonly SET statement_timeout = '8000ms';
-- GRANT SELECT on the ceo_ai.* views (and any canonical read models Cube reads).
```

The role, `ceo_ai` schema, and views are created by the ceo_ai reporting-schema
migration. Until those views exist, the cubes read canonical `public.*` tables,
so `mesha_cube_readonly` must be granted SELECT on exactly the tables the cubes
reference — nothing more:

```sql
GRANT USAGE ON SCHEMA public TO mesha_cube_readonly;
GRANT SELECT ON public.goats, public.locations, public.obligation_instances,
  public.feed_direction_completions, public.procurement_loads, public.sop_tasks
  TO mesha_cube_readonly;
```

When each cube's `sql:` FROM migrates to a `ceo_ai.*` view, drop the matching
`public.*` grant so the role's surface shrinks back to `ceo_ai` only. This grant
is what makes the governed path work end-to-end through the read-only role rather
than a superuser (verified locally 2026-07-22).

## Secret Manager entries (names only)

Provision under `goatos-stg` and mirror to GitHub Actions secrets (repo
`vgoats/goatos`) for CI:

- `mesha-cube-api-secret` → `MESHA_CUBE_API_SECRET`
- `mesha-cube-readonly-db-password` → `MESHA_CUBE_DB_PASSWORD`

## Promotion checklist

1. Local proof green (metric == SQL oracle; tenant-less call rejected).
2. `mesha_cube_readonly` role + grants exist in stg Cloud SQL.
3. Secrets present in Secret Manager + GitHub secrets.
4. Cloud Run service private (internal ingress), SA has Cloud SQL Client.
5. Backend `MESHA_CUBE_URL` points at the stg service; browser has no path to it.
6. Smoke a governed metric (e.g. `kpi_vaccination.vaccination_overdue` by park)
   from the backend and compare to a read-only SQL oracle on stg.
