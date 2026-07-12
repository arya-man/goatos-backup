# Prod Promotion: stg→prod (and dev) — Var Flip, Not Rebuild

> Owner: platform/observability · Companion to `INFRA.md`, `LESSONS_AND_GUARDS.md`
> This guide proves Goat OS observability can promote to prod as a configuration
> flip, not a rebuild. It defines what is identical across all envs and what changes
> per-env.

---

## Executive summary

The observability stack (OTel Collector, Grafana, dashboards, datasources, telemetry
guard) is **env-agnostic** — the code and config are identical. Only these values
change per env:

| Category | dev | stg | prod |
|---|---|---|---|
| **GCP Project** | goatos-dev | goatos-stg | goatos-prod |
| **GCP Region** | asia-south1 | asia-south1 | asia-south1 (fixed for all) |
| **Service DNS subdomain** | `<service>-dev-...run.app` | `<service>-stg-...run.app` | `<service>-prod-...run.app` |
| **Service account email** | `goatos-*-dev@goatos-dev.iam` | `goatos-*-stg@goatos-stg.iam` | `goatos-*-prod@goatos-prod.iam` |
| **Secret Manager secrets** | `goatos-dev-grafana-admin-password`, etc. | `goatos-stg-grafana-admin-password`, etc. | `goatos-prod-grafana-admin-password`, etc. |
| **GCS buckets** | `goatos-dev-observability-config`, etc. | `goatos-stg-observability-config`, etc. | `goatos-prod-observability-config`, etc. |
| **BigQuery dataset** | `goatos_dev_analytics_rollup` | `goatos_stg_analytics_rollup` | `goatos_prod_analytics_rollup` |
| **Firebase project** | goatos-dev (or same as stg if not split) | goatos-stg | goatos-prod |
| **GA4 property / export dataset** | analytics_<dev-property-id> | analytics_<stg-property-id> | analytics_<prod-property-id> |
| **Terraform variables file** | `dev.tfvars` | `stg.tfvars` (currently imperative) | `prod.tfvars` (new) |
| **Backend env** | GOATOS_OBS_SINK=otlp + GOATOS_OTLP_ENDPOINT=http://localhost:4318 | GOATOS_OBS_SINK=otlp + GOATOS_OTLP_ENDPOINT=http://localhost:4318 | GOATOS_OBS_SINK=otlp + GOATOS_OTLP_ENDPOINT=http://localhost:4318 |
| **Telemetry enabled?** | TELEMETRY_ENABLED=false (dev), true (stg/prod) | TELEMETRY_ENABLED=true | TELEMETRY_ENABLED=true |
| **Trace sample ratio** | 0.1 (sample 10%, dev-only) | 1.0 (100%, all traces) | 1.0 (100%, all traces) for stg/prod; TBD for prod if cost-driven downsampling is needed |
| **Grafana Cloud Run min instances** | 0 (scale-to-zero, dev only) | 1 (always on, stg) | 1 (always on, prod) |
| **API min instances** | 0 | 1 | 2 (prod higher for SLA) |

**What is IDENTICAL across all envs:**
- OTel Collector image + config (`infra/observability/otel-collector-config.yaml`)
- Grafana image (`grafana/grafana:11.3.0`) + datasource types + dashboard JSON
- Grafana Alloy image (`grafana/alloy:v1.5.1`) + config (`infra/observability/alloy-config.alloy`)
- GMP query-frontend image (`gke.gcr.io/prometheus-engine/frontend:v0.15.1`) + sidecar pattern
- Analytics rollup job logic (same Python/Go code, different BigQuery dataset)
- Mobile/admin-web telemetry wiring (same `@grafana/faro-web-sdk`, same Android Firebase Analytics)
- Telemetry guardrail rule (`docs/observability/TELEMETRY_GUARDRAILS.md`, enforced in CI for all envs)

---

## Two promotion paths

### Path A: Canonical (Terraform-first, recommended for prod)

**Prerequisite:** Unlike stg (which was built imperatively), prod must start **clean
under Terraform** from day one, or the live resources must be imported into `gs://goatos-prod-tf-state`
before this apply. See "Handling the empty-state problem" below.

**Steps:**

1. **Create `infra/envs/prod/`** directory.

2. **Copy and adapt stg terraform files** to prod:
   ```bash
   cd infra/envs
   cp -r stg/observability.tf prod/
   cp -r stg/secrets.tf prod/
   cp -r stg/monitoring.tf prod/
   cp -r stg/cloud_sql.tf prod/
   cp -r stg/analytics_rollup.tf prod/
   cp -r stg/variables.tf prod/
   cp -r stg/outputs.tf prod/
   ```

3. **Edit each file** to change:
   - `google_service_account.grafana` name → `goatos-grafana-prod` (from `-stg`)
   - All resource names from `-stg-` to `-prod-`
   - All project references from `"goatos-stg"` to `"goatos-prod"`
   - Bucket/dataset/secret names from `stg` to `prod`
   - (optional) Adjust `var.observability_operator_members`, SLO thresholds, min instances

4. **Create `prod.tfvars`** (never commit secrets, only supplied at apply time):
   ```hcl
   monitoring_alert_email_addresses = ["ravi@mesha.sg", "alerts@mesha.sg"]
   stg_sweeper_actor_id             = "<prod-workforce-member-id>"  # reviewed
   observability_operator_members   = ["user:ravi@mesha.sg", "user:other@mesha.sg"]
   ga4_export_dataset_id            = ""  # populated after manual Firebase link
   # Optional overrides:
   # grafana_image = "grafana/grafana:11.4.0"  # if newer version is desired
   # api_availability_slo = 0.999  # if prod SLO differs from stg
   # api_min_instances = 2  # prod higher than stg
   ```

5. **Before apply, set up prod's state backend** (if not already done):
   ```bash
   gsutil mb -p goatos-prod -l asia-south1 gs://goatos-prod-tf-state
   gsutil versioning set on gs://goatos-prod-tf-state
   ```

6. **Init and apply:**
   ```bash
   cd infra/envs/prod
   terraform init \
     -backend-config="bucket=goatos-prod-tf-state" \
     -backend-config="prefix=observability"
   
   terraform plan -var-file=prod.tfvars -out=prod-observability.tfplan
   # Review: should show ~20 new resources (3 SAs, 3 Cloud Run services,
   # 1 Cloud Run Job, 1 Scheduler, 2 GCS buckets, 1 BigQuery dataset,
   # 2 secrets, + 5 alert policies + in-place updates to api/kernel Jobs)
   
   terraform apply prod-observability.tfplan
   ```

7. **Manual step: link GA4 → BigQuery** (Firebase console, not Terraform):
   - Firebase console → `goatos-prod` project → Integrations → BigQuery → Link.
   - Select location **asia-south1 (Mumbai)** explicitly.
   - Enable "Daily" export.
   - Copy the created dataset ID (format: `analytics_<property-id>`) into `prod.tfvars`
     under `ga4_export_dataset_id`.
   - Re-run `terraform apply` to wire the conditional IAM grant.

**Post-apply verification (see "Verification checklist" below).**

### Path B: Imperative fallback (if terraform state is unavailable)

Use this **only if** prod's Terraform state cannot be initialized in time for deploy.
This matches how stg was built:

```bash
# Prerequisites
export PROJECT=goatos-prod REGION=asia-south1
export GRAFANA_ADMIN_PASSWORD=$(openssl rand -base64 24)
export GRAFANA_POSTGRES_PASSWORD=$(openssl rand -base64 24)

# 1. Create service accounts (one per component)
gcloud iam service-accounts create goatos-grafana-prod \
  --project=$PROJECT --display-name="Grafana service account"
gcloud iam service-accounts create goatos-grafana-alloy-prod \
  --project=$PROJECT --display-name="Grafana Alloy service account"
gcloud iam service-accounts create goatos-gmp-frontend-prod \
  --project=$PROJECT --display-name="GMP query-frontend service account"
gcloud iam service-accounts create goatos-analytics-rollup-prod \
  --project=$PROJECT --display-name="Analytics rollup job service account"

# 2. Grant roles
gcloud projects add-iam-policy-binding $PROJECT \
  --member=serviceAccount:goatos-grafana-prod@$PROJECT.iam.gserviceaccount.com \
  --role=roles/monitoring.viewer --role=roles/cloudtrace.user \
  --role=roles/logging.viewer --role=roles/bigquery.dataViewer \
  --role=roles/bigquery.jobUser --role=roles/cloudsql.client \
  --condition=None
# (similar for alloy, analytics_rollup)

# 3. Create secrets
gcloud secrets create goatos-prod-grafana-admin-password --project=$PROJECT \
  --replication-policy="automatic"
echo -n "$GRAFANA_ADMIN_PASSWORD" | gcloud secrets versions add \
  goatos-prod-grafana-admin-password --project=$PROJECT --data-file=-
# (similar for postgres datasource password)

# 4. Grant secrets access to service accounts
gcloud secrets add-iam-policy-binding goatos-prod-grafana-admin-password \
  --project=$PROJECT \
  --member=serviceAccount:goatos-grafana-prod@$PROJECT.iam.gserviceaccount.com \
  --role=roles/secretmanager.secretAccessor

# 5. Deploy Grafana Cloud Run service
# (Fetch dashboard JSON from committed `infra/grafana/dashboards/*.json`)
# (Fetch datasource config from `infra/grafana/provisioning/datasources/`)

gcloud run deploy goatos-prod-grafana \
  --image=grafana/grafana:11.3.0 \
  --project=$PROJECT --region=$REGION \
  --service-account=goatos-grafana-prod@$PROJECT.iam.gserviceaccount.com \
  --ingress=internal \
  --set-env-vars="GF_SECURITY_ADMIN_PASSWORD=$GRAFANA_ADMIN_PASSWORD,GF_AUTH_ANONYMOUS_ENABLED=false" \
  --port=3000 --memory=512Mi --cpu=1 --min-instances=1 --max-instances=2

# 6. Deploy GMP query-frontend
gcloud run deploy goatos-prod-gmp-frontend \
  --image=gke.gcr.io/prometheus-engine/frontend:v0.15.1 \
  --project=$PROJECT --region=$REGION \
  --service-account=goatos-gmp-frontend-prod@$PROJECT.iam.gserviceaccount.com \
  --ingress=internal --port=9090

# 7. Deploy Grafana Alloy (with OTel Collector sidecar)
# (Sidecar in same revision, config from GCS or inline)
# See `INFRA.md` §12 for sidecar container definition

# 8. Redeploy backend api + kernel Jobs with OTel Collector sidecar
# (Same pattern as stg: add `otel-collector` sidecar container + env vars)

# 9. Create Cloud Scheduler trigger for analytics rollup
gcloud scheduler jobs create pubsub goatos-prod-analytics-rollup-schedule \
  --project=$PROJECT --location=$REGION \
  --schedule="15 3 * * *" --tz="Asia/Kolkata" \
  --message-body="{}" \
  --topic=projects/$PROJECT/topics/scheduled-tasks
# (Then add Cloud Run Job as the subscriber)
```

**Then follow the same manual GA4 link + verification checklist.**

---

## Handling the empty-state problem

**The problem:** stg's Terraform files are written but live resources were built **imperatively**
(via `gcloud run deploy`, etc.). If you run `terraform plan` on stg today, it shows ~313 resources
to add, which would collide with live ones.

**The solution for prod:** Do NOT let this happen. **Either:**

- **Start prod clean:** use the canonical Terraform path above (Path A), which creates all resources
  fresh under Terraform from day one.
- **Import existing resources:** if prod resources already exist imperatively before terraform,
  import them:
  ```bash
  terraform import google_cloud_run_v2_service.grafana projects/goatos-prod/locations/asia-south1/services/goatos-prod-grafana
  terraform import google_service_account.grafana goatos-grafana-prod@goatos-prod.iam.gserviceaccount.com
  # ... (repeat for every resource)
  terraform plan  # should now show 0 changes
  ```

**For stg cleanup (separate, post-prod):** Once prod is stable and working, consider bringing stg
under Terraform via import so future stg changes don't require imperative steps.

---

## Backend telemetry rollout (any env: dev, stg, or prod)

Once the observability infra is live (Grafana, Collector, datasources, dashboards),
the backend telemetry is **OFF by default** — it requires an env flip to turn on.
This allows a **canary rollout** so you can verify on one workload before rolling to all.

### Canary approach (mandatory for every env)

1. **Pick one backend workload** — typically the smallest/safest:
   - Option A: A single Kernel Job (e.g. just the `reconcile-capacity` job)
   - Option B: The main `api` service with `--min-instances=0` + canary traffic routing (if using Traffic Splitting)

2. **Build a new backend image** with telemetry enabled (already default in code; just ensure
   `GOATOS_OBS_SINK=otlp` is in `cloud_run_services.tf` or `cloud_run_jobs.tf`).

3. **Deploy to the canary workload ONLY:**
   ```bash
   # If canary is a single Job:
   gcloud run jobs update goatos-prod-reconcile-capacity \
     --project=goatos-prod --region=asia-south1 \
     --set-env-vars="GOATOS_OBS_SINK=otlp,GOATOS_OTLP_ENDPOINT=http://localhost:4318" \
     --image=.../<new-image-sha>
   gcloud run jobs execute goatos-prod-reconcile-capacity --project=goatos-prod --wait
   ```

4. **Verify telemetry landed:**
   - Open Grafana → check **any dashboard** (API/RED, Kernel pipeline, etc.)
   - Filter by the canary workload's labels (e.g., `job="reconcile-capacity"`)
   - Look for metrics, traces, logs arriving (may have a 10-15 second latency from Collector)
   - Check Cloud Trace: search by trace id if a test request was sent
   - Check Cloud Logging: filter by resource type + job name

5. **If telemetry flows:** proceed to step 6.
   **If it does NOT flow after 2-3 minutes:** check logs on the Collector sidecar
   (Cloud Run Revisions → that job → Logs tab → filter for `otel-collector` container).
   Common causes: sidecar not booting, env var typo, firewall/VPC blocking loopback.

6. **Roll to api + remaining kernel Jobs:**
   ```bash
   # Redeploy api with the same env vars
   gcloud run deploy goatos-prod-api \
     --project=goatos-prod --region=asia-south1 \
     --set-env-vars="GOATOS_OBS_SINK=otlp,GOATOS_OTLP_ENDPOINT=http://localhost:4318" \
     --image=.../<new-image-sha>
   
   # Redeploy all kernel Jobs (each in a loop or as a script)
   for JOB in reconcile-capacity obligation-sweeper notification-dispatcher ...; do
     gcloud run jobs update goatos-prod-$JOB \
       --project=goatos-prod --region=asia-south1 \
       --set-env-vars="GOATOS_OBS_SINK=otlp,GOATOS_OTLP_ENDPOINT=http://localhost:4318" \
       --image=.../<new-image-sha> --wait
   done
   ```

7. **Re-verify:** Check all 6 dashboards now populate with metrics from the full stack.

### Why canary is mandatory

- **Max-retries risk:** Cloud Run Jobs retry failures up to `max_retries` times. If the sidecar
  fails to start, every retry uses up quota. Canary on a low-traffic job first.
- **Sidecar-completion gotcha:** Cloud Run Jobs' sidecar-completion behavior is not yet fully
  verified in production at scale — there's a risk of double-execution if the completion signal
  misfires. Test on one job first.
- **Cost feedback:** telemetry sends to GMP/Cloud Trace/Cloud Logging. A canary on one small job
  lets you measure the cost per job/day before rolling to all 13 kernel jobs + api.

---

## Post-deploy verification checklist (per env)

Run this checklist for dev, stg, **and** prod after promotion:

- [ ] **Grafana accessibility**
  - [ ] Grafana Cloud Run service is running (check Cloud Run console, status = Ready)
  - [ ] Can reach Grafana URL in a browser or via `gcloud run services proxy`
  - [ ] Admin login works with the Secret Manager password
  - [ ] Service account can list dashboards via API (`curl -H "Authorization: Bearer $TOKEN" $GRAFANA_URL/api/dashboards`)

- [ ] **Cloud Monitoring / GMP metrics**
  - [ ] "Google Managed Prometheus" datasource in Grafana shows "Data source is working"
  - [ ] Sample PromQL query (e.g., `rate(http.server.requests[5m])`) returns data (after telemetry flows)

- [ ] **Cloud Trace**
  - [ ] "Google Cloud Trace" datasource in Grafana shows "Data source is working"
  - [ ] Cloud Trace console (GCP console) shows at least one trace after a backend request is made

- [ ] **Cloud Logging**
  - [ ] "Google Cloud Logging" datasource in Grafana shows "Data source is working"
  - [ ] Sample log query (e.g., resource.type="cloud_run_revision") returns entries

- [ ] **BigQuery datasource**
  - [ ] "BigQuery (GA4 export, ad-hoc)" datasource in Grafana shows "Data source is working"
  - [ ] The `analytics_<ga4-property-id>` dataset exists in BigQuery (check console)

- [ ] **Postgres analytics datasource** (if rollup is enabled)
  - [ ] Create Postgres role `goatos_grafana_ro` on the prod Cloud SQL instance (manually)
  - [ ] "Postgres (analytics rollups)" datasource in Grafana shows "Data source is working"
  - [ ] Query `SELECT COUNT(*) FROM analytics.mobile_funnel_rollup;` returns a number (0 if no data yet)

- [ ] **Each of the 6 dashboards**
  - [ ] "API / RED" dashboard loads, no red alerts (panels empty until telemetry flows)
  - [ ] "Database" dashboard loads, Cloud SQL metrics visible (CPU, memory, connections)
  - [ ] "Kernel pipeline" dashboard loads
  - [ ] "Frontend RUM" dashboard loads
  - [ ] "Mobile" dashboard loads
  - [ ] "SLO / burn" dashboard loads

- [ ] **Alert policies**
  - [ ] All 5 new SLO/burn alert policies exist in Cloud Monitoring (check console)
  - [ ] Alert notification channels point to `var.monitoring_alert_email_addresses` (check policy details)

- [ ] **GA4 → BigQuery link** (manual Firebase console step)
  - [ ] Firebase console → Project settings → Integrations → BigQuery → status shows "Active"
  - [ ] Daily export is enabled (not just streaming)
  - [ ] `analytics_<property-id>` dataset exists in BigQuery, in `asia-south1` location

- [ ] **Analytics rollup job**
  - [ ] Cloud Scheduler job `goatos-<env>-analytics-rollup-schedule` is enabled
  - [ ] Cloud Run Job `goatos-<env>-analytics-rollup` can execute (check revision status)
  - [ ] Run the job manually once to verify no errors: `gcloud run jobs execute goatos-<env>-analytics-rollup --project=goatos-<env> --wait`
  - [ ] Check BigQuery: `SELECT COUNT(*) FROM goatos_<env>_analytics_rollup.mobile_funnel_rollup;` — should have rows after a run

- [ ] **Backend telemetry flow** (after canary rollout)
  - [ ] At least one trace lands in Cloud Trace (send a test request to the API)
  - [ ] Metrics appear on all dashboards (may have 10–15 second delay)
  - [ ] Logs appear in Cloud Logging from the backend
  - [ ] OTel Collector sidecar container logs show no errors (`Exporter: googlecloudtrace: exporting spans`)

- [ ] **Frontend telemetry** (admin-web)
  - [ ] Grafana Alloy Cloud Run service is running
  - [ ] Admin-web is initialized with Faro (`FaroProvider` in `app/layout.tsx`)
  - [ ] Open admin-web in a browser, check browser console for Faro initialization (should see `Grafana Faro SDK initialized`)
  - [ ] RUM events flow to Cloud Logging (check Cloud Logging for resource type = `api` or similar, log name contains "faro")

- [ ] **Mobile telemetry** (goatos-android, if available for this env)
  - [ ] Android app `BuildConfig.TELEMETRY_ENABLED` is `true` for this env
  - [ ] Firebase Analytics events appear in Firebase console after opening the app
  - [ ] Funnels appear in BigQuery under `analytics_<property-id>` after a user flow

---

## Summary: from stg to prod

| Step | Terraform | Imperative |
|---|---|---|
| **1. Create GCP infra (SAs, secrets, Cloud Run services, datasources)** | `terraform apply prod-observability.tfplan` | `gcloud` commands (Path B above) |
| **2. Populate Grafana dashboards** | Terraform mounts GCS → `infra/grafana/dashboards/*.json` | Manual HTTP API or import JSON |
| **3. Link GA4 → BigQuery** | `var.ga4_export_dataset_id` via tfvars | Firebase console (same for both) |
| **4. Redeploy backend api + Jobs with telemetry** | Terraform changes → `terraform apply` | `gcloud run deploy` with OTLP env |
| **5. Verify all 6 dashboards + telemetry** | Run checklist above | Run checklist above |

**Expected result:** prod observability is a **byte-for-byte copy of stg's code/config, with only env
vars changed.** The stack is battle-tested on stg; prod reuses the same Collector config, the same
Grafana image, the same dashboard JSON.

