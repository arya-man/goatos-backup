# Runbook: Cube Core locally (Mesha governed metrics)

Cube is the governed metric layer for the leadership assistant. This runbook
starts it on your laptop, points it at the local Goat OS Postgres, and verifies a
metric against a direct SQL oracle.

## Prerequisites

- Docker running.
- The local Goat OS DB container `goatos-local-current` up on `127.0.0.1:5433`
  (db `goatos`). This is the normal local app DB.
- `curl`, `python3` (for signing a test JWT).

## Configuration (no secret values committed)

Cube config comes from env vars, or a gitignored `.env.ceo-ai.local` at the repo
root. Only the **names** live in git; values are pulled from Google Secret
Manager (see below) or set locally for dev.

| Var | Purpose | Local default |
|---|---|---|
| `MESHA_CUBE_URL` | Cube origin the backend calls | `http://127.0.0.1:4000` |
| `MESHA_CUBE_API_SECRET` | JWT signing secret (backend signs, Cube verifies) | required |
| `MESHA_CUBE_DB_HOST` | Postgres host from the container | `host.docker.internal` |
| `MESHA_CUBE_DB_PORT` | Postgres port | `5433` |
| `MESHA_CUBE_DB_NAME` | database | `goatos` |
| `MESHA_CUBE_DB_USER` | DB user (`mesha_cube_readonly` in stg) | `postgres` |
| `MESHA_CUBE_DB_PASSWORD` | DB password | required |

### Pulling secrets from Secret Manager (source of truth)

Runtime + CI secrets live in Google Secret Manager (project `goatos-stg`, org
`vgoats.com`). Verify context first, then fetch into the gitignored local env
file (names only shown; never commit a value):

```bash
gcloud config set account ravi@mesha.sg
gcloud config set project goatos-stg
# example (secret names are provisioned by the secrets task):
{
  echo "MESHA_CUBE_API_SECRET=$(gcloud secrets versions access latest --secret=mesha-cube-api-secret)"
  echo "MESHA_CUBE_DB_PASSWORD=$(gcloud secrets versions access latest --secret=mesha-cube-readonly-db-password)"
} >> .env.ceo-ai.local
```

For pure local dev against `goatos-local-current` you can instead set a throwaway
`MESHA_CUBE_API_SECRET` and the local DB password directly in the shell.

## Start / stop

```bash
export MESHA_CUBE_API_SECRET=local-dev-cube-secret
export MESHA_CUBE_DB_PASSWORD=<local goatos db password>
tools/dev/run-cube-local.sh           # start (idempotent — replaces prior container)
tools/dev/run-cube-local.sh status    # curl /readyz
tools/dev/run-cube-local.sh logs
tools/dev/run-cube-local.sh stop
```

The script bind-mounts `analytics/cube/cube.js` and `analytics/cube/model/` so
model edits are picked up on restart.

## Verify a metric against the SQL oracle

The governed number must equal a direct SQL count over the same canonical
tables. Example — `vaccination_overdue` by park.

**Cube** (sign a tenant-scoped JWT, call `/cubejs-api/v1/load`):

```bash
python3 - <<'PY'
import hmac,hashlib,base64,json,urllib.request,time
secret="local-dev-cube-secret"
b64=lambda b: base64.urlsafe_b64encode(b).rstrip(b'=')
h=b64(json.dumps({"alg":"HS256","typ":"JWT"}).encode())
p=b64(json.dumps({"tenant_id":"00000000-0000-4000-8000-000000000001",
                  "iat":int(time.time()),"exp":int(time.time())+3600}).encode())
sig=b64(hmac.new(secret.encode(),h+b'.'+p,hashlib.sha256).digest())
tok=(h+b'.'+p+b'.'+sig).decode()
req=urllib.request.Request("http://127.0.0.1:4000/cubejs-api/v1/load",
  data=json.dumps({"query":{"measures":["kpi_vaccination.vaccination_overdue"],
                            "dimensions":["kpi_vaccination.park_label"]}}).encode(),
  headers={"Authorization":tok,"Content-Type":"application/json"})
print(json.load(urllib.request.urlopen(req))["data"])
PY
```

**Oracle** (same tenant, direct SQL):

```bash
PGPASSWORD=<db pw> psql -h 127.0.0.1 -p 5433 -U postgres -d goatos -c "
  SELECT p.name park, count(*)
  FROM obligation_instances o
  JOIN locations s ON s.location_id = o.scope_id
  JOIN locations p ON p.location_id = s.parent_location_id
  WHERE o.status='scheduled'
    AND (o.due_at AT TIME ZONE 'Asia/Kolkata')::date
        < (now() AT TIME ZONE 'Asia/Kolkata')::date
  GROUP BY 1 ORDER BY 2 DESC;"
```

The two must match (verified 2026-07-22: Coimbatore 108, Channapatna 60).

## Tenant isolation check

A `/load` call with **no** Authorization token (or a token without `tenant_id`)
must be rejected — `queryRewrite` throws and Cube returns an error. Never expose
Cube to the browser; only the Mesha backend calls it via
`backend/internal/ceoai/cubeclient`.

## Go client

`backend/internal/ceoai/cubeclient` is the typed client: `New(Config)` then
`Load(ctx, tenantID, Query)`. Tenant is signed into a short-lived JWT (never a
query field), 8s deadline, bounded retries, typed errors. Tests:
`go test ./internal/ceoai/cubeclient/...` (from `backend/`).

## Troubleshooting: `permission denied for table ...` / assistant says `cube: could not be retrieved.`

**Symptom.** The leadership assistant ("Ask Mesha") answers a KPI question with
`cube: could not be retrieved.` (the masked form of a Cube error in
`backend/internal/ceoai/app/orchestrator.go` `strictRecompose`). A direct signed
`/load` call returns HTTP 200 with
`{"error":"Error: permission denied for table obligation_instances"}` (or
`goats` / `feed_direction_completions` / `procurement_loads` / `sop_tasks`).

**Root cause.** Cube connects to Postgres as `mesha_cube_readonly`, which by
design has `REVOKE ALL ON SCHEMA public` and SELECT on `ceo_ai.*` **only**
(`tools/dev/setup-ceo-ai-local-role.sh`). A Cube model that runs inline
`sql:` / `sql_table:` directly against a raw `public` table executes with the
connecting role's own privileges (no view-owner indirection), so the query is
denied. This is NOT a secret/URL/health problem — Cube and the JWT are fine.

**Rule.** Every Cube model MUST read from a `ceo_ai.*` view, never a raw `public`
table. The `ceo_ai.*` views are owned by the migration role, so Postgres runs
them with owner rights and the read-only Cube role can select them without any
grant on `public`. Migration `000030_ceo_ai_cube_source_views.sql` added the
per-cube passthrough source views
(`ceo_ai.vaccination_obligations_base`, `animals_base`, `feed_completions_base`,
`procurement_loads_base`, `workforce_tasks_base`) and repointed the
`vaccination` / `animals` / `feed` / `procurement` / `workforce` cubes to them;
`operator` was already repointed in `000027`. Same fix applies identically in
local, stg, and prod because all three use the same read-only-role design.

**Fix / verify locally.**
1. Apply migrations so the `ceo_ai.*` source views exist (`000030`), then re-run
   `tools/dev/setup-ceo-ai-local-role.sh` (or rely on the migration's guarded
   grants) so `mesha_cube_readonly` has SELECT on them.
2. Restart Cube to reload the model: `docker restart mesha-cube-local`.
3. Confirm a signed governed query returns data, not a permission error:
   `POST /cubejs-api/v1/load {"query":{"measures":["kpi_vaccination.due_today"]}}`.
