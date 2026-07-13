# Goat OS Faro RUM (Real User Monitoring) Deployment

## Overview

Faro RUM captures browser telemetry from admin-web (page loads, Web Vitals, JavaScript errors, unhandled rejections, console logs, network timing) and forwards it to Google Cloud Trace for distributed tracing and debugging.

The architecture is:
1. Admin-web browser SDK (`@grafana/faro-web-sdk`) sends RUM payloads via POST to Grafana Alloy's Faro receiver
2. Alloy terminates the HTTP endpoint (port 8080) on a public Cloud Run service
3. Alloy exports traces to Google Cloud Trace (`otelcol.exporter.googlecloud`)
4. Traces appear in Cloud Trace UI for inspection, and can be queried via Grafana dashboards

## Architecture

### Deployment Stack

| Component | Location | Purpose |
|-----------|----------|---------|
| **Grafana Alloy** | Cloud Run service `goatos-stg-grafana-alloy` | Faro receiver; exports OTLP traces to GCP |
| **Admin-web** | Cloud Run service `goatos-admin-web-stg` | Browser app; Faro SDK initialized by `FaroProvider` |
| **Cloud Trace** | GCP project `goatos-stg` | Central trace backend |
| **Grafana** | TBD | Optional: visualize RUM traces via Cloud Monitoring datasource |

### Data Flow

```
Browser (admin-web)
    ↓
    ├─→ Faro SDK captures:
    │   ├─ Web Vitals (LCP, FID, CLS, TTFB)
    │   ├─ JavaScript errors (window.onerror, unhandledrejection)
    │   ├─ Console logs (captured)
    │   ├─ Page navigation
    │   └─ Fetch/XHR timing + trace propagation
    │
    ↓
POST https://goatos-stg-grafana-alloy-*.run.app/faro/receiver?...
    ↓
Grafana Alloy (Cloud Run)
    ├─→ faro.receiver "admin_web_rum"
    │   ├─ Listens on 0.0.0.0:8080
    │   ├─ CORS restricted to https://stg.dashboard.mesha.sg
    │   └─ Parses Faro payload → OTLP traces
    │
    ├─→ otelcol.exporter.googlecloud "default"
    │   ├─ Project auto-detected from metadata (workload identity)
    │   └─ Exports traces to Cloud Trace
    │
    ↓
Google Cloud Trace (goatos-stg project)
    ├─ Traces indexed by trace_id
    └─ Available in Cloud Trace UI + queryable via Grafana
```

### Authentication

- **Alloy to GCP**: Workload identity (Cloud Run SA `goatos-grafana-alloy-stg@goatos-stg.iam.gserviceaccount.com`)
- **Browser to Alloy**: HTTP (public endpoint, CORS-restricted)
- **Alloy to Cloud Trace**: Workload identity (no API keys)

### Security

1. **CORS**: Faro receiver only accepts POST from `https://stg.dashboard.mesha.sg`
2. **Unauthenticated public endpoint**: Browsers must reach Alloy; Cloud Run allows unauthenticated access
3. **Rate limiting**: Cloud Armor policy on Cloud Run service (future; currently Cloud Logging quotas apply)
4. **No secrets**: Workload identity replaces API keys

## Deployment

### Prerequisites

- `gcloud` configured for `goatos-stg` project and `ravi@mesha.sg` user
- Docker and Docker Buildx installed
- Admin-web source at `apps/admin-web/`
- Alloy config at `infra/observability/alloy-config.alloy`

### Step 1: Build and Deploy Alloy

```bash
# 1a. Build Alloy image (includes config)
cd /Users/ravi/mesha/goatos-obs-wt/infra/observability
docker buildx build --platform linux/amd64 \
  -t asia-south1-docker.pkg.dev/goatos-stg/goatos/grafana-alloy:obs-<SHA> \
  --push .

# 1b. Deploy to Cloud Run
gcloud run deploy goatos-stg-grafana-alloy \
  --project=goatos-stg \
  --region=asia-south1 \
  --image=asia-south1-docker.pkg.dev/goatos-stg/goatos/grafana-alloy:obs-<SHA> \
  --service-account=goatos-grafana-alloy-stg@goatos-stg.iam.gserviceaccount.com \
  --port=8080 \
  --allow-unauthenticated \
  --min-instances=1 \
  --max-instances=3 \
  --memory=512Mi \
  --cpu=1

# 1c. Get the Alloy URL (save for step 2)
ALLOY_URL=$(gcloud run services describe goatos-stg-grafana-alloy \
  --project=goatos-stg --region=asia-south1 \
  --format='value(status.url)')
echo "Alloy URL: $ALLOY_URL"
```

### Step 2: Build and Deploy Admin-web

```bash
# 2a. Build admin-web with Faro collector URL
cd /Users/ravi/mesha/goatos-obs-wt
FARO_URL="$ALLOY_URL"
docker buildx build --platform linux/amd64 \
  -t asia-south1-docker.pkg.dev/goatos-stg/goatos/admin-web:obs-<SHA> \
  --build-arg NEXT_PUBLIC_FARO_COLLECTOR_URL="${FARO_URL}" \
  --build-arg NEXT_PUBLIC_GOATOS_ENV="stg" \
  --build-arg NEXT_PUBLIC_APP_VERSION="obs-<SHA>" \
  -f apps/admin-web/Dockerfile \
  --push .

# 2b. Redeploy admin-web to Cloud Run
gcloud run deploy goatos-admin-web-stg \
  --project=goatos-stg \
  --region=asia-south1 \
  --image=asia-south1-docker.pkg.dev/goatos-stg/goatos/admin-web:obs-<SHA>

# 2c. Verify /login returns 200
curl -s -o /dev/null -w "%{http_code}\n" https://stg.dashboard.mesha.sg/login
# Expected: 200
```

### Step 3: Verify RUM is Flowing

```bash
# 3a. Load admin-web a few times to trigger RUM events
for i in {1..3}; do
  curl -s https://stg.dashboard.mesha.sg/login > /dev/null
  echo "Request $i sent"
done

# 3b. Wait 1-2 minutes for traces to propagate
sleep 60

# 3c. Check Cloud Trace for RUM traces (there is no `gcloud trace list`
# command — use the Cloud Trace v1 REST API directly)
TOKEN=$(gcloud auth print-access-token)
curl -s -H "Authorization: Bearer ${TOKEN}" \
  "https://cloudtrace.googleapis.com/v1/projects/goatos-stg/traces?view=ROOTSPAN&pageSize=10"
# Inspect each returned trace's root span name/labels for a browser/RUM origin
# (e.g. Web Vitals / page-load span names), not a Cloud Run health-check span
# like "/readyz" or "/api/live/ws" — those are unrelated backend/Grafana traffic.

# 3d. Check Alloy logs for data receipt
gcloud run services logs read goatos-stg-grafana-alloy \
  --project=goatos-stg --region=asia-south1 --limit=50 \
  | grep -i "faro\|receiver\|http"
```

**Do not stop at "service is Ready" or "curl to the Alloy URL returns 200."** See
`LESSONS_AND_GUARDS.md#finding-23` — Alloy's own UI server can silently win a port
race against `faro.receiver` and answer every path with its own `index.html` while
the actual receiver never binds. Confirm the POST target really is the receiver
(response is not Alloy's UI HTML) and confirm a real trace lands in Cloud Trace
before declaring RUM live.

### Step 4: Rollback (if needed)

```bash
# Get previous admin-web revision (`gcloud run services list-revisions` does
# not exist — the correct subcommand is `gcloud run revisions list`)
PREV_REVISION=$(gcloud run revisions list --service=goatos-admin-web-stg \
  --project=goatos-stg --region=asia-south1 \
  --format='value(metadata.name)' --sort-by="~metadata.creationTimestamp" \
  | sed -n '2p')

# Route traffic back to previous revision
gcloud run services update-traffic goatos-admin-web-stg \
  --project=goatos-stg --region=asia-south1 \
  --to-revisions="${PREV_REVISION}=100"

# Verify /login still works
curl -s -o /dev/null -w "%{http_code}\n" https://stg.dashboard.mesha.sg/login
```

## Configuration

### Alloy Configuration (`infra/observability/alloy-config.alloy`)

Key settings:

```hcl
faro.receiver "admin_web_rum" {
  server {
    listen_address       = "0.0.0.0"
    listen_port          = 8080
    cors_allowed_origins = ["https://stg.dashboard.mesha.sg"]
  }
  output {
    traces = [otelcol.exporter.googlecloud.default.input]
  }
}

otelcol.exporter.googlecloud "default" {
  project = ""  // auto-detect from metadata
  metric {
    prefix = "custom.googleapis.com"
  }
}
```

To update CORS origins, modify `cors_allowed_origins` and redeploy Alloy.

### Admin-web Configuration (`apps/admin-web/Dockerfile`)

Faro collector URL is passed as a build argument:

```dockerfile
ARG NEXT_PUBLIC_FARO_COLLECTOR_URL
ENV NEXT_PUBLIC_FARO_COLLECTOR_URL=${NEXT_PUBLIC_FARO_COLLECTOR_URL}
```

The `FaroProvider` component (`apps/admin-web/components/observability/faro-provider.tsx`) reads `process.env.NEXT_PUBLIC_FARO_COLLECTOR_URL` and initializes the SDK if set (no-op if unset).

### IAM Setup

Service account `goatos-grafana-alloy-stg@goatos-stg.iam.gserviceaccount.com` requires:

```bash
# Cloud Trace agent role
gcloud projects add-iam-policy-binding goatos-stg \
  --member="serviceAccount:goatos-grafana-alloy-stg@goatos-stg.iam.gserviceaccount.com" \
  --role="roles/cloudtrace.agent"

# Cloud Logging writer role
gcloud projects add-iam-policy-binding goatos-stg \
  --member="serviceAccount:goatos-grafana-alloy-stg@goatos-stg.iam.gserviceaccount.com" \
  --role="roles/logging.logWriter"
```

## Monitoring

### Cloud Trace Dashboard

1. Open Cloud Console → Cloud Trace
2. Search for traces with labels:
   - `service_name: "mesha-admin-web"`
   - `environment: "stg"`
3. Click a trace to see:
   - Browser events (page navigation, fetch timing)
   - Web Vitals (LCP, FID, CLS, TTFB)
   - JS errors (if any)
   - Linked server spans (via traceparent header)

### Grafana Dashboard

To visualize RUM in Grafana (optional):

1. Ensure Grafana has a Google Cloud Monitoring datasource configured
2. Import or create a dashboard that queries:
   - `resource.type = "api"`
   - `metric.type = "custom.googleapis.com/faro/*"` (if metrics are enabled)
   - Alternatively, query Cloud Trace directly via Cloud Logging

### Logs

Alloy logs are available in Cloud Logging:

```bash
# Alloy error logs
gcloud logging read 'resource.type="cloud_run_revision" AND resource.labels.service_name="goatos-stg-grafana-alloy" AND severity="ERROR"' \
  --project=goatos-stg --limit=10

# Recent Alloy logs
gcloud run services logs read goatos-stg-grafana-alloy \
  --project=goatos-stg --region=asia-south1 --limit=50
```

## Limitations & Future Work

### Current Limitations (v1)

1. **Logs only, no metrics**: Faro's log output is in Loki format, not OTLP. Logs are not forwarded to Cloud Logging in this pass. RUM visibility is via traces only.
   - **Fix**: Add `loki.write` or a format adapter in Alloy to bridge Loki logs to Cloud Logging.

2. **No rate limiting config**: Rate limiting is enforced at Cloud Armor (future) and GCP quotas, not in Alloy config.
   - **Fix**: Add rate limit policy to Cloud Run service or Cloud Armor.

3. **No log-to-metrics**: Web Vitals are not yet available as queryable metrics in Google Cloud Monitoring.
   - **Fix**: Add log-to-metrics transformation in Cloud Logging to extract Web Vitals.

### Future Enhancements

- Log ingestion (Faro logs → Cloud Logging via loki.write)
- Web Vitals as metrics (log-to-metrics in Cloud Logging)
- Grafana dashboard for RUM KPIs
- Trace-to-logs linking (show backend logs in browser trace context)
- Real user monitoring alerts (high error rate, poor Web Vitals)

## Troubleshooting

### Alloy service not ready

**Symptom**: `gcloud run deploy` fails with "container failed to start and listen on port 8080"

**Fix**:
1. Check logs: `gcloud run services logs read goatos-stg-grafana-alloy --limit=50`
2. Look for config errors (e.g., "unrecognized block name")
3. Ensure config syntax is correct: `community-components.enabled=true` flag is set in Dockerfile
4. Redeploy with `--min-instances=1` to keep instance warm

### Faro SDK not sending data

**Symptom**: No traces in Cloud Trace after loading admin-web

**Diagnosis**:
1. Check admin-web has Faro URL baked into the built JS bundle (the env var is inlined at
   Next.js build time, so it will NOT appear in the served HTML/response headers — grep the
   `_next/static/chunks/*.js` bundles instead):
   `curl -s https://stg.dashboard.mesha.sg/login | grep -o '_next/static/chunks/[^"]*\.js'` then
   `curl -s https://stg.dashboard.mesha.sg/<chunk-path> | grep -o 'https://goatos-stg-grafana-alloy[^"'"'"']*'`
   - If not present, admin-web needs rebuild with the URL
2. Check browser console (DevTools): look for Faro initialization and POST requests to Alloy URL
3. **Do not use "curl the Alloy URL root returns 200/HTML" as a health signal** — Alloy's own
   bundled UI answers EVERY path with its `index.html`, including a broken deployment where the
   faro.receiver's own listener failed to bind (see `LESSONS_AND_GUARDS.md#finding-23`). Instead,
   POST a Faro-shaped payload directly and confirm the response is NOT the Alloy UI HTML:
   `curl -s -X POST https://goatos-stg-grafana-alloy-*.run.app/ -H 'Content-Type: application/json' -d '{"meta":{},"traces":{}}' | head -c 200`
   — if this echoes back `<title>Grafana Alloy</title>`, the receiver is not actually listening.

### CORS errors in browser

**Symptom**: Browser console shows "CORS error" when Faro SDK tries to POST to Alloy

**Fix**:
1. Verify CORS origin in Alloy config: `cors_allowed_origins = ["https://stg.dashboard.mesha.sg"]`
2. If domain has changed, update both Alloy config and admin-web Faro SDK initialization
3. Redeploy Alloy

## Repeating for Production

To deploy to `goatos-prod`:

1. Replace `goatos-stg` with `goatos-prod` in all `gcloud` commands
2. Update `cors_allowed_origins` to prod domain (e.g., `https://dashboard.mesha.sg`)
3. Update admin-web `NEXT_PUBLIC_GOATOS_ENV` to `prod`
4. Create new SA `goatos-grafana-alloy-prod@goatos-prod.iam.gserviceaccount.com` and grant IAM roles
5. Build and push Alloy and admin-web images with `-prod-<SHA>` tags
6. Redeploy both services to `goatos-prod` project

Full production deployment script:

```bash
#!/bin/bash
set -e

PROJECT=goatos-prod
REGION=asia-south1
ENV=prod
SHA=$(git rev-parse --short HEAD)

# 1. Deploy Alloy
docker buildx build --platform linux/amd64 \
  -t asia-south1-docker.pkg.dev/${PROJECT}/goatos/grafana-alloy:obs-${SHA} \
  --push ./infra/observability

gcloud run deploy goatos-${ENV}-grafana-alloy \
  --project=${PROJECT} --region=${REGION} \
  --image=asia-south1-docker.pkg.dev/${PROJECT}/goatos/grafana-alloy:obs-${SHA} \
  --service-account=goatos-grafana-alloy-${ENV}@${PROJECT}.iam.gserviceaccount.com \
  --port=8080 --allow-unauthenticated --min-instances=1 --max-instances=3

ALLOY_URL=$(gcloud run services describe goatos-${ENV}-grafana-alloy \
  --project=${PROJECT} --region=${REGION} \
  --format='value(status.url)')

# 2. Deploy admin-web
docker buildx build --platform linux/amd64 \
  -t asia-south1-docker.pkg.dev/${PROJECT}/goatos/admin-web:obs-${SHA} \
  --build-arg NEXT_PUBLIC_FARO_COLLECTOR_URL="${ALLOY_URL}" \
  --build-arg NEXT_PUBLIC_GOATOS_ENV="${ENV}" \
  --build-arg NEXT_PUBLIC_APP_VERSION="obs-${SHA}" \
  -f apps/admin-web/Dockerfile --push .

gcloud run deploy goatos-admin-web-${ENV} \
  --project=${PROJECT} --region=${REGION} \
  --image=asia-south1-docker.pkg.dev/${PROJECT}/goatos/admin-web:obs-${SHA}

echo "Deployment complete. Alloy: ${ALLOY_URL}"
```

## References

- [Grafana Alloy Faro receiver](https://grafana.com/docs/alloy/latest/reference/components/faro.receiver/)
- [@grafana/faro-web-sdk](https://github.com/grafana/faro-web-sdk)
- [Google Cloud Trace documentation](https://cloud.google.com/trace/docs)
- [Workload Identity for Cloud Run](https://cloud.google.com/run/docs/configuring/service-accounts#workload-identity-setup)

## Related

- `docs/observability/OBSERVABILITY_DESIGN.md` — Overall observability architecture
- `apps/admin-web/components/observability/faro-provider.tsx` — Faro SDK initialization
- `infra/observability/alloy-config.alloy` — Alloy configuration
- `infra/observability/Dockerfile` — Alloy Docker image
