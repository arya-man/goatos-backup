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
