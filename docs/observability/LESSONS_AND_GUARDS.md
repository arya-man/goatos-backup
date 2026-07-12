# Lessons & Guards — Observability Build Anti-Recurrence Ledger

> Owner: platform/observability · Companion to `PROD_PROMOTION.md`
> Every bug/gotcha found + fixed during stg build, so prod doesn't repeat them.
> This ledger is the **master checklist** for any observability rollout (dev, stg, prod).

---

## Anti-recurrence findings

### 1. Cardinality guard: route templates only, never raw paths

**Symptom:** metrics blew up cardinality; dashboards sluggish; GMP billing spike.

**Root cause:** HTTP request metrics were labeled with raw path strings (e.g.,
`http.server.request.duration{path="/api/v1/goats/12345/vaccination/67890"}`
instead of template route). Each distinct goat ID = new label combination =
unbounded cardinality.

**Fix location:** `backend/internal/platform/observability/telemetry.go` →
`HTTPRequestMiddleware` → `route` label extraction. Now uses route **template**
from mux (`/api/v1/goats/{id}/vaccination/{vacc_id}`) for the label, never the
expanded path. Enforced by constant symbol `otelhttp.HTTPAttributeFilter`.

**Prevention guard:** 
- Machine-blocked: `make observability-cardinality-guard` checks for path-based
  metrics in new code (diff-scoped, CI job `guardrails`).
- Manual: every new metric added to telemetry must specify its label schema upfront.
  High-cardinality labels (e.g., goat IDs, timestamps, session tokens) are rejected.
  Approved labels: `route` (template), `method` (GET/POST), `status_class` (2xx/5xx),
  `tenant_id`, `table_name` (schema.table, not row-level), `channel` (notification channel),
  `stage` (kernel stage name).

**Next env check (prod):** Verify the `route` label in a sample PromQL query
(`rate(http.server.requests{route!=""}[5m])`) shows distinct routes only
(~20-30 routes), not individual resource IDs. If cardinality > 100, stop and
investigate.

---

### 2. API os.Exit flush bypass

**Symptom:** API shutdown logs/traces went missing; last request traces did not reach Cloud Trace.

**Root cause:** Backend called `os.Exit(1)` in a panic-recovery handler without flushing
the OpenTelemetry Meter/Tracer exporters first. Pending traces were in-flight buffers, lost
on immediate exit.

**Fix location:** `backend/internal/platform/observability/telemetry.go` → `SetupTelemetry()` →
returns a cleanup function. API main now calls this cleanup via `defer` before exiting, so all
metrics/traces/logs have time to flush to their backends (GMP/Cloud Trace/Cloud Logging).

**Prevention guard:**
- Code review: any `os.Exit()` call must be preceded by a flush/shutdown function.
- Doc: `docs/decisions/observability.md` § "Graceful shutdown" — mandatory pattern for any
  service emitting telemetry.
- Manual: test graceful shutdown with a backend crash: send a request, kill the process,
  verify the trace for that request still appears in Cloud Trace within 10 seconds.

**Next env check (prod):** After deploying api/jobs to prod with telemetry enabled, send a
test request, then stop the service. Open Cloud Trace and verify the last request's trace
is **present and complete** (all spans for api → db → outbox → consumer should exist).

---

### 3. Worker (Kernel Job) bounded flush context

**Symptom:** Long-running kernel workers consumed goroutines waiting for telemetry flush;
OTel Collector sidecar hung on shutdown.

**Root cause:** Kernel Jobs (reconciler, sweeper, notification-dispatcher) had no timeout
on the telemetry flush step. If a consumer is slow or a large batch is in flight,
`ctx.Background()` passed to `exporter.ForceFlush(ctx)` could wait indefinitely,
blocking the job's exit.

**Fix location:** `backend/internal/platform/observability/telemetry.go` →
`ShutdownTelemetry()` accepts a **timeout context** (5 seconds by default).
Each Kernel Job's main now sets a `time.After(5 * time.Second)` cancel on the
flush context, so even if the Collector is slow, the job exits within that window.

**Prevention guard:**
- Mandatory: all job/worker main functions must call telemetry shutdown with a bounded context
  (timeout ≤ 5 seconds).
- Machine-blocked: `make worker-telemetry-guard` (CI job, diff-scoped) checks for unbounded
  flush contexts in worker code.

**Next env check (prod):** Deploy a kernel Job to prod, send a large batch to its input queue,
then manually stop the Job via Cloud Run console mid-execution. The Cloud Run Job container logs
should show the sidecar and main process exiting cleanly within ~7 seconds (5 sec flush + grace).
If it hangs or is terminated by Cloud Run's default timeout, flush context has regressed.

---

### 4. Collector single-port → sidecar pattern (no standalone service)

**Symptom:** OTel Collector as a standalone Cloud Run service could not expose both gRPC (4317)
and HTTP (4318) ports to external callers.

**Root cause:** Cloud Run v2 routes a service's single hostname to **one container port**.
A producer wanting to use gRPC had to dial a different URL than HTTP producers — forcing a
choice per SDK language, or running two Collector services (double the ops burden).

**Fix location:** `infra/observability/otel-collector-config.yaml` + `cloud_run_services.tf` /
`cloud_run_jobs.tf` → OTel Collector is now a **sidecar container** (no `ports` block, no ingress)
inside each Cloud Run service/Job revision. Producers talk to it over loopback (`http://localhost:4318`
for HTTP OTLP), never over the public network. The Collector config still declares both gRPC and
HTTP receivers for completeness, but external callers use HTTP only.

**Prevention guard:**
- Architectural: any observability infra design must place the Collector as a sidecar (in the
  same Cloud Run revision as the app), not a standalone service, to avoid the single-port bottleneck.
- Code review: confirm that `GOATOS_OTLP_ENDPOINT=http://localhost:4318` is set (not a
  `*.run.app` URL), and that `otel-collector` container runs in the same revision.

**Next env check (prod):** After deploying prod api with the Collector sidecar, open the Cloud Run
service → Revisions → latest → Logs. Check for **two containers**: `api` and `otel-collector`.
Both should show successful startup. If only `api` appears, the sidecar was not deployed.

---

### 5. GMP query-frontend sidecar pattern for Grafana

**Symptom:** Grafana's plain `prometheus` datasource could not authenticate to Google Managed
Prometheus (GMP); all GMP metrics queries failed with 401 Unauthenticated.

**Root cause:** Grafana needs to attach a GCP OAuth bearer token to each HTTP request to query
GMP (`monitoring.googleapis.com`). The plain Prometheus datasource in Grafana has no way to
inject that token — it only supports `Authorization: Basic` auth (username/password).

**Fix location:** `infra/envs/stg/observability.tf` → deploy a small proxy service
`google/gke.gcr.io/prometheus-engine/frontend:v0.15.1` (Google's official reference
architecture for this exact problem). The proxy holds `roles/monitoring.viewer` and injects
the GCP OAuth token server-side. Grafana's Prometheus datasource points at the proxy instead
of `monitoring.googleapis.com` directly.

**Prevention guard:**
- Architectural: the GMP query-frontend must be present in **every env** (dev/stg/prod) to serve
  Prometheus-API-compatible queries for GMP.
- Code review: confirm that Grafana datasource `url` points to the GMP-frontend service URL
  (e.g., `https://goatos-stg-gmp-frontend-...run.app`), not directly to `monitoring.googleapis.com`.
- Manual test: from Grafana UI, test the "Google Managed Prometheus" datasource → should show green.

**Next env check (prod):** After `terraform apply` creates the GMP-frontend service, open
Grafana → Datasources → "Google Managed Prometheus" → Test. Should say "Data source is working".
If it fails with 403/401, check that the `goatos-gmp-frontend-prod` SA has `roles/monitoring.viewer`.

---

### 6. GCS bucket objectViewer IAM for sidecar config mount

**Symptom:** Collector sidecar failed to boot; Cloud Run logs showed 403 Forbidden when mounting
the config from GCS.

**Root cause:** Collector sidecar runs as the job's service account (e.g., `goatos-*-stg@...iam`),
which had no read permission on the GCS bucket containing `otel-collector-config.yaml`. The bucket
was created with no explicit IAM grant for the SA, and the service account defaulted to denied.

**Fix location:** `infra/envs/stg/observability.tf` → `google_storage_bucket_iam_member` binding.
Each producer's SA (api, kernel Jobs, grafana_alloy) now has `roles/storage.objectViewer`
on the `goatos-stg-observability-config` GCS bucket.

**Prevention guard:**
- Terraform: every GCS bucket for infrastructure (config, provisioning) must explicitly grant
  `roles/storage.objectViewer` to the SAs that read from it.
- Manual verification: after applying, list IAM bindings on the bucket:
  ```bash
  gsutil iam get gs://goatos-<env>-observability-config
  ```
  Should show all producer SAs with `roles/storage.objectViewer`.

**Next env check (prod):** After `terraform apply`, check the prod observability-config bucket
IAM has all 4 SAs (grafana, alloy, gmp_frontend, analytics_rollup). If any is missing, Collector
sidecars or provisioning jobs will fail.

---

### 7. RUM datasource open sink → CORS + rate-limit + Cloud Armor (follow-up)

**Symptom:** Browser RUM events from admin-web to Grafana Alloy could be spoofed or DDoS'd via
public Faro receiver endpoint (`/v1/traces`).

**Root cause:** The Grafana Alloy `faro.receiver` is a public HTTP endpoint (`port 12347`,
`INGRESS_TRAFFIC_ALL`) to receive RUM from browsers. With no CORS guard, any domain can POST.
With no rate-limit, it can be flooded.

**Fix location:** `infra/observability/alloy-config.alloy` → `faro.receiver` block.
Currently receives unauthenticated, but is **intended for internal admin-web only**.
A follow-up (not yet wired) will:
- Add CORS origin check to only allow `admin-web-<env>.mesha.sg` (via `cors_allowed_origins`)
- Add rate-limit to the Grafana Alloy service (via Cloud Run quota/concurrency settings)
- Place Cloud Armor WAF policy on the Cloud Run service (if behind a Load Balancer, future)

**Prevention guard:**
- Pre-deploy manual: confirm `INGRESS_TRAFFIC_INTERNAL_ONLY` is set on grafana_alloy Cloud Run
  service **until CORS + rate-limiting is implemented**. This limits it to internal GCP calls only.
- Code review: any frontend code initializing Faro must explicitly set the receiver URL to the
  controlled Alloy endpoint (no user input, hardcoded per-env).
- Follow-up task: implement CORS + rate-limit + Cloud Armor before prod goes public.

**Next env check (prod):** Confirm `grafana-alloy` Cloud Run service has `ingress=INGRESS_TRAFFIC_INTERNAL_ONLY`.
If it ever changes to `INGRESS_TRAFFIC_ALL`, ensure CORS + rate-limit + Armor are implemented first.

---

### 8. Error-rate SLO denominator must include non-2xx requests

**Symptom:** SLO burn-rate alert was not firing even when error rate spiked to 50%, because the
alert definition incorrectly measured errors as a ratio of **5xx requests only** instead of
**total requests**.

**Root cause:** The SLO threshold was defined as:
```
rate(http.server.requests{status_class="5xx"}[5m]) / rate(http.server.requests{status_class="5xx"}[5m])
```
This always equals 1.0 (100% of 5xx requests are errors), so no alert. The correct denominator
is all requests, including 2xx, 4xx, and 5xx.

**Fix location:** `infra/envs/stg/monitoring.tf` → alert policy `api_5xx_error_rate_burn` →
`threshold_value=0.005` (0.5% error budget burn). Numerator is `status_class="5xx"`, denominator
is all requests (`status_class=~"[2345]xx"` or `sum(...)` over all status classes).

**Prevention guard:**
- Manual review: every SLO policy's threshold definition must be inspected for:
  - Denominator includes all relevant request types (not a subset)
  - Numerator is the failure cases only
  - Ratio makes business sense (5% error rate, not "5 requests were errors")
- Document each SLO's formula in the alert policy description or a companion doc.

**Next env check (prod):** After alert policies are deployed, manually trigger a 5xx error
(e.g., send a bad request or kill a service), then check Cloud Monitoring → Alerts.
The SLO burn-rate alert should fire within 2 minutes if error rate exceeds the threshold.

---

### 9. GMP metric name suffixes (_total, _seconds, etc. mismatches)

**Symptom:** Some metrics were defined with `_total` suffix (counter convention) but
Grafana dashboard PromQL queries expected them without suffix, or vice versa. Panels
showed "no data".

**Root cause:** OpenTelemetry SDK applies **semantic naming conventions**: counters get `_total`,
histograms get `_bucket/_count/_sum`, gauges have no suffix. If a custom metric is emitted as
one type but the dashboard queries assume another, there's a mismatch.

**Fix location:** `backend/internal/platform/observability/telemetry.go` → all metrics explicitly
declare their type (counter, histogram, gauge) via `MeterProvider.NewInt64Counter()` etc., and
their semantic names include the suffix. Dashboard PromQL queries in `infra/grafana/dashboards/*.json`
match the declared names exactly.

**Prevention guard:**
- Metric definition + dashboard review: every new metric must be defined once (in telemetry.go)
  and referenced in dashboards with its **exact declared name including suffix**.
- Machine-blocked: `make observability-metric-naming-guard` (diff-scoped CI) checks for metrics
  referenced in dashboards but not defined in telemetry code.

**Next env check (prod):** Open each dashboard in Grafana and inspect a panel's PromQL query.
Verify the metric name matches a defined metric in telemetry.go. Run the query; it should
return data (or "no data yet" if telemetry hasn't flowed). If it errors with "undefined metric",
the naming mismatch has regressed.

---

### 10. Dashboard↔schema literal key match (database queries vs panel definitions)

**Symptom:** "Mobile" dashboard `mobile_funnel_rollup` panel queried a Postgres table, but
the table schema in migrations used a different column name (e.g., `funnel_step` vs `step`).
Panel showed "unknown column" error.

**Root cause:** Dashboard JSON was written against an assumed schema, migrations were written
separately, and they drifted.

**Fix location:** `infra/grafana/dashboards/05-mobile.json` → all SQL queries in panels now
match the **actual** schema from `backend/internal/migrations/000157_analytics_rollup.sql`.
Cross-checked: column names, table names, and schema prefix (`analytics.*`).

**Prevention guard:**
- Backend-first review: migrations must be committed and reviewed before dashboards are written.
- Dashboard review: every SQL query in a Grafana panel must be validated against the live
  `backend/migrations/` directory.
- Manual test: once deployed, run each dashboard query manually in Cloud SQL console to ensure
  it returns data (or "empty" if no data exists yet).

**Next env check (prod):** After backend migration 000157 and analytics rollup job are live,
open Grafana → "Mobile" dashboard → each panel. Panels should show data (once rollup job runs)
or "empty result" (not an error). If a panel shows "unknown column", stop and check schema drift.

---

### 11. services.tf missing BigQuery/Cloud Trace API enablement

**Symptom:** Analytics rollup job crashed with "bigquery.googleapis.com not enabled in project"
error, even though BigQuery dataset was created.

**Root cause:** `infra/envs/stg/services.tf` had a list of enabled APIs, but did not include
`bigquery.googleapis.com` or `cloudtrace.googleapis.com`. The Terraform resources (dataset, trace)
were created, but the API itself was disabled, causing runtime failures.

**Fix location:** `infra/envs/stg/services.tf` → added:
```hcl
"bigquery.googleapis.com",
"cloudtrace.googleapis.com",
"logging.googleapis.com",
"monitoring.googleapis.com",
```
to the `enabled_services` list. Now `terraform apply` enables these APIs before creating resources.

**Prevention guard:**
- Mandatory pre-apply: run `terraform plan` and check for any "create" action on a service/resource.
  Cross-check that the corresponding API is in `enabled_services`.
- Manual: after `terraform apply`, verify in GCP console → APIs & Services → that all used APIs
  are marked "Enabled" (not "Disabled" by accident).

**Next env check (prod):** Before applying observability terraform, inspect `services.tf` to ensure
all APIs used by observability resources are listed. After apply, run:
```bash
gcloud services list --enabled --project=goatos-prod | grep -E "bigquery|cloudtrace|logging|monitoring"
```
All four should appear.

---

### 12. Region pinning — all asia-south1, no defaults

**Symptom:** A BigQuery dataset was accidentally created in `US` multi-region instead of
`asia-south1` (Mumbai). Data residency requirement violated; cost unexpected.

**Root cause:** `google_bigquery_dataset` resource had no explicit `location` argument. Terraform
defaulted to the provider's region, which was not set, which GCP interpreted as US.

**Fix location:** All observability resources now explicitly set `location = var.region` or
`location = "asia-south1"`:
- `google_bigquery_dataset.analytics_rollup` → `location = "asia-south1"`
- `google_cloud_run_v2_service.*` → `location = var.region`
- All referenced by templates in dashboards, so the region is visible.

**Prevention guard:**
- Mandatory: every Terraform resource in observability must explicitly declare `location` or
  `region` = `var.region` (which defaults to `"asia-south1"`).
- Machine-blocked: `make observability-region-guard` (diff-scoped CI) checks for resources
  missing `location`/`region` args.

**Next env check (prod):** After `terraform apply prod-observability.tfplan`, verify:
```bash
gcloud sql instances describe goatos-prod-core-db --project=goatos-prod | grep region
# Should show asia-south1, not us-central1

bq ls -d -a --project_id=goatos-prod | grep analytics_rollup
# BigQuery dataset should show location=asia-south1 (or asia-south asia-southeast1)
```

---

### 13. Faro initialization must use correct receiver URL (no user input)

**Symptom:** Admin-web Faro RUM was sending events to the wrong endpoint (dev Alloy instead
of stg Alloy); events were lost or sent to the wrong project.

**Root cause:** The Faro initialization in `apps/admin-web/app/layout.tsx` took the receiver URL
from an env var (`NEXT_PUBLIC_FARO_RECEIVER_URL`), and the build was misconfigured, so the
dev value landed in the stg build.

**Fix location:** `apps/admin-web/app/layout.tsx` → hardcode the receiver URL per-build-flavor:
```typescript
const faroUrl = process.env.NEXT_PUBLIC_FLAVOR === 'prod' 
  ? 'https://goatos-prod-grafana-alloy-...-run.app'
  : process.env.NEXT_PUBLIC_FLAVOR === 'stg'
  ? 'https://goatos-stg-grafana-alloy-...-run.app'
  : 'http://localhost:12347'; // dev
```
No user input, no env var guessing. The flavor (dev/stg/prod) determines the URL.

**Prevention guard:**
- Code review: no `NEXT_PUBLIC_FARO_RECEIVER_URL` should be read from runtime env. It must be
  hardcoded or derived deterministically from the build flavor.
- Build verification: after `npm run build` for a flavor, grep the bundle for the expected
  hostname (e.g., `goatos-stg-grafana-alloy` should appear in stg builds only).

**Next env check (prod):** After building admin-web for prod (`NEXT_PUBLIC_FLAVOR=prod npm run build`),
run:
```bash
grep -r "goatos-prod-grafana-alloy" .next/
```
Should find the prod Alloy URL in the bundle. If it finds a dev/stg URL instead, the build
has the wrong flavor or the hardcoded URL is wrong.

---

### 14. Root error boundary (admin-web) must report to Faro

**Symptom:** JavaScript errors on admin-web routes were not reaching Cloud Logging; only
structured errors from `throw new Error(...)` were visible.

**Root cause:** Admin-web had error boundaries for individual routes, but no global
`ObservabilityErrorBoundary` wrapping all routes. Unhandled exceptions and silent
failures (e.g., network timeouts in useEffect) were not reported.

**Fix location:** `apps/admin-web/app/(admin)/layout.tsx` → add
`ObservabilityErrorBoundary` as a React class component wrapping the entire
admin route group. It implements `componentDidCatch` and calls
`faro.api?.pushError(error)` for every exception, then re-throws so error boundaries
higher up also get a chance.

**Prevention guard:**
- Code review: every `(admin)` sub-layout must have an error boundary at the top level.
- Manual test: trigger a JS error on an admin-web route (e.g., `throw new Error("test")`),
  then check Cloud Logging for the error (search `resource.type="cloud_run_revision"` +
  `severity=ERROR`). It should appear within 2 seconds.

**Next env check (prod):** Trigger a test error on admin-web, wait 2 seconds, then:
```bash
gcloud logging read "resource.type='cloud_run_revision' AND resource.labels.service_name='goatos-prod-admin-web' AND severity='ERROR' AND '(test)'" --limit=5 --project=goatos-prod
```
Should show the error. If it doesn't, the error boundary or Faro initialization has regressed.

---

### 15. Android firebaseAppDistribution regression risk

**Symptom:** Android debug/staging builds broke; `com.google.firebase.firebase-crashlytics`
dependency pulled in a conflicting version of Crashlytics that broke the build.

**Root cause:** Firebase App Distribution (for beta/tester builds) and Crashlytics have
overlapping dependencies. A version bump in one broke the classpath.

**Fix location:** `apps/goatos-android/build.gradle.kts` → explicitly pin Crashlytics and
Firebase versions alongside Firebase App Distribution. Use BOM (Bill of Materials) when
available to keep all Firebase libs in sync.

**Prevention guard:**
- Dependency review: any change to Firebase/Google libs must verify that Crashlytics and
  App Distribution still build without conflicts.
- Machine-blocked: `make android-build` (CI job) compiles the app; if dependency conflicts occur, it fails.

**Next env check (prod):** After updating Firebase dependencies, build the Android app:
```bash
cd apps/goatos-android
./gradlew assembleStgDebug  # for stg build
./gradlew assembleProdRelease  # for prod release
```
Both should succeed without classpath conflicts.

---

### 16. Interceptor gating on TELEMETRY_ENABLED

**Symptom:** OTel SDK metrics were being recorded even in dev flavor, where telemetry
is disabled. Observability calls were polluting logs with init warnings.

**Root cause:** The OTel interceptor was always instantiated, even when
`BuildConfig.TELEMETRY_ENABLED=false`. It attempted to connect to a non-existent
Collector, causing noise.

**Fix location:** `apps/goatos-android/core/core-analytics/src/main/kotlin/sg/mesha/goatos/core/observability/OtelInterceptor.kt`
→ guard the initialization:
```kotlin
if (BuildConfig.TELEMETRY_ENABLED) {
  // Only instantiate OtelInterceptor for stg/prod
  val otelInterceptor = OtelInterceptor(...)
}
```

**Prevention guard:**
- Code review: any SDK (OTel, Firebase, Crashlytics) with a conditional init must guard it
  on `BuildConfig.TELEMETRY_ENABLED`.
- Build verification: in dev builds, the logcat should show **no** telemetry init messages.

**Next env check (prod):** Build the Android app for each flavor:
```bash
./gradlew assembleDevDebug    # should have NO telemetry logs
./gradlew assembleStgDebug    # should HAVE telemetry init logs
./gradlew assembleProdRelease # should HAVE telemetry init logs
```
Grep logcat for "Telemetry\|OTLP\|Firebase" — dev should be silent, stg/prod verbose.

---

### 17. setUserProperty PII allowlist (Android Firebase Analytics)

**Symptom:** A developer added `analytics.setUserProperty("email", user.email)` to track user
identity. Email addresses are PII; this should never land in Firebase Analytics (only in backend
audit logs).

**Root cause:** No code review check for PII in Analytics calls.

**Fix location:** `apps/goatos-android/core/core-analytics/src/main/kotlin/sg/mesha/goatos/core/analytics/AnalyticsEvents.kt`
→ `setUserProperty` calls must use only the allowlist:
```kotlin
// Allowed (non-PII):
ROLE, PRIMARY_PARK, FLAVOR, TENANT, DEPARTMENT, EXPERIENCE_LEVEL, ...

// Blocked:
email, name, phone, nic, rfid, blood_type, ...
```

**Prevention guard:**
- Code review: every `setUserProperty` call must pass only keys from the allowlist.
- Inline comment required: any new allowlist entry must cite the business reason (e.g.,
  "REGION added for geographic filtering in analytics").
- Machine-blocked: `make android-pii-guard` (diff-scoped CI) checks for hardcoded strings
  in `setUserProperty` and rejects non-allowlist keys.

**Next env check (prod):** Search the Android app code for all `setUserProperty` calls:
```bash
grep -r "setUserProperty" apps/goatos-android/
```
Every invocation must pass a key from the allowlist. If any pass a variable or string literal
outside the list, stop and remediate before prod deploy.

---

### 18. Sweeper v4 vs v5 UUID mismatch

**Symptom:** Sweeper logs showed "could not find event UUID in outbox" errors; obligations
were re-created repeatedly because the sweeper's idempotency key (UUID) didn't match the
event UUID stored in the outbox envelope.

**Root cause:** The sweeper was built with a newer UUID library (v5 SHA1) while the outbox
envelope used v4 (random) UUIDs from an older version. The IDs were incompatible even
though both were RFC 4122 compliant.

**Fix location:** `backend/internal/platform/uuid/uuid.go` → standardize on v4 only:
```go
id := uuid.NewV4()  // Always, never uuid.NewV5()
```
Every component (api, sweeper, consumer) now uses v4 exclusively. The outbox envelope
includes the UUID as a string field, not reconstructed at runtime.

**Prevention guard:**
- Architectural: every microservice must agree on one UUID version (v4 for Goat OS). Encode
  UUIDs as strings in durable envelopes, never as reconstructed types.
- Code review: grep for `uuid.NewV5` — should not appear in production code.

**Next env check (prod):** After deploying the sweeper with UUID fixes:
```bash
gcloud logging read "severity='ERROR' AND 'UUID'" --limit=10 --project=goatos-prod
```
Should show **zero** "could not find UUID" errors. If any appear, the sweeper and outbox
are using different UUID versions.

---

### 19. Stray build binary committed to git

**Symptom:** A temporary build artifact (`backend/.build/api-linux-amd64`, 50 MB)
was accidentally committed, bloating the git repo.

**Root cause:** Build output was not in `.gitignore`; a contributor ran a local build
and committed it without noticing.

**Fix location:** `backend/.gitignore` → added:
```
.build/
dist/
*.a
*.so
```
+ existing `.gitignore` reviewed to include all build outputs. Stray binary removed
from history via `git filter-branch` (or squash-and-force for a new branch).

**Prevention guard:**
- Pre-commit hook: `make test:pre-commit` checks for binary files in staged changes
  (diff-scoped, rejects commits containing `.a`, `.so`, `*.o`, `dist/`, `.build/`).
- CI: `make lint` includes the binary check as part of guardrails.

**Next env check (prod):** Before pushing any code to prod, run:
```bash
git diff origin/main --name-only | xargs file | grep -i "ELF\|Mach-O"
```
Should return **nothing**. If any binaries are found, do not push.

---

### 20. \_\_pycache\_\_ committed to git (analytics rollup)

**Symptom:** Python `__pycache__` directories (bytecode cache) were committed, bloating
the repo and causing import-stale issues across different Python versions.

**Root cause:** The analytics rollup job (Python/Go) built locally and a developer ran
`git add -A` without checking `.gitignore`.

**Fix location:** `backend/analytics_rollup/.gitignore` → added:
```
__pycache__/
*.pyc
*.pyo
.Python
*.egg-info/
```

**Prevention guard:**
- Template: every new language/framework directory gets a `.gitignore` on day one
  (Python, Go, Kotlin, TypeScript, etc.).
- Pre-commit hook: `make test:pre-commit` blocks commits containing `__pycache__` or `.pyc`.

**Next env check (prod):** Before pushing Python/analytics code:
```bash
find . -name "__pycache__" -o -name "*.pyc" | head -5
```
Should return **nothing**. If found, run `find . -name '__pycache__' -exec rm -rf {} +` and
re-commit without cache.

---

### 21. Per-app checks are NOT the full guard set — run `make guardrails`

**Symptom:** `apps/admin-web/components/observability/faro-provider.tsx` passed all the
admin-web *local* checks (lint, typecheck, `check:mock-fidelity`, `check-ui-contract-literals`,
token-leak) during its build + review lane, but landed on `main` **red**: the repo-root
`make guardrails` → `tools/agent-hooks/check-boundaries.sh` failed on it. Two sub-guards
tripped: (a) the **Mesha-branding** guard (`goat[ -]*os` in admin-web `.tsx`, minus an
allowlist) flagged `FARO_APP_NAME = "goatos-admin-web"` and a `GoatOSClientOptions` comment;
(b) the **debug-footer** guard (`Trace [A-Za-z]`) flagged the comment `// Trace linkage:`.

**Root cause (process, not just code):** the admin-web lane verified with the app's own
`package.json` scripts and never ran the **repo-root** `make guardrails` / `check-boundaries.sh`,
which is a *different, broader* gate (branding, debug-footer, feature-boundary, secret,
slog/recover guards). Local per-app green ≠ repo guardrails green.

**Fix location:** `faro-provider.tsx` — `FARO_APP_NAME` → `"mesha-admin-web"` (on-brand,
non-user-visible telemetry id), the comment reworded to the allowlisted `@goatos/api-client`
form, and `// Trace linkage` lowercased to `// trace linkage`. `check-boundaries.sh` → EXIT 0.
The Faro app-name also now respects the standing "user-visible brand is Mesha, never Goat OS"
rule (Goat OS stays a codename in package/class/repo only).

**Prevention guard:**
- **Always run `make guardrails` (repo root) before any push/PR — not just the per-app
  `npm run lint/typecheck/mock-fidelity`.** Per-app checks are necessary but not sufficient.
- New user-visible-ish literals in `apps/admin-web/**.tsx` (labels, app names, comments)
  must avoid the `goat[ -]*os`/`vgoat` string unless they use an allowlisted token
  (`@goatos`, `GOATOS_`, `X-GoatOS-`, `GoatOSApiError`, `goatos-build`); prefer Mesha branding.
- Avoid `Trace <Word>` / `Rendered <Word>` capitalized phrasing in admin-web `.tsx`
  (comments included) — it collides with the debug-footer guard.

**Next env check (prod):** the fix is env-agnostic (already on `main`), so it carries forward.
For any new admin-web observability code, run `make guardrails` locally and confirm
`check-boundaries` EXIT 0 before push. Added to the pre-deploy/pre-push checklist below.

---

## PRE-DEPLOY CHECKLIST (mandatory for any env: dev, stg, prod)

> **PRE-PUSH (every commit, before deploy):** run repo-root **`make guardrails`** and
> confirm EXIT 0 — this includes `check-boundaries` (Mesha-branding, debug-footer,
> feature-boundary, secret, slog/recover), `telemetry-guard`, `scale-guard`, `mobile-guard`,
> etc. The per-app `npm run lint/typecheck/check:mock-fidelity` are necessary but do NOT
> substitute for `make guardrails` (see finding #21).

Run this before `terraform apply` or imperative deployment:

### Infrastructure & IaC

- [ ] **Terraform files reviewed**
  - [ ] All new resource names use the correct env suffix (`-dev`, `-stg`, `-prod`)
  - [ ] All resource `location`/`region` fields set to `var.region` (no defaults)
  - [ ] Project IDs are correct (`goatos-dev`, `goatos-stg`, `goatos-prod`)
  - [ ] No hardcoded values for secrets, passwords, or credentials (use Secret Manager)

- [ ] **State backend ready**
  - [ ] `gs://goatos-<env>-tf-state` bucket exists and is versioned
  - [ ] If this is the first apply to an env, state is empty (no prior infrastructure)
  - [ ] If live resources exist imperatively, plan to import them (see PROD_PROMOTION.md)

- [ ] **Services API enabled** in the GCP project:
  - [ ] `run.googleapis.com` (Cloud Run)
  - [ ] `sql.googleapis.com` (Cloud SQL)
  - [ ] `monitoring.googleapis.com` (Cloud Monitoring / GMP)
  - [ ] `cloudtrace.googleapis.com` (Cloud Trace)
  - [ ] `logging.googleapis.com` (Cloud Logging)
  - [ ] `bigquery.googleapis.com` (BigQuery)
  - [ ] `storage.googleapis.com` (GCS)
  - [ ] `secretmanager.googleapis.com` (Secret Manager)
  - [ ] `iam.googleapis.com` (Cloud IAM)
  - [ ] `compute.googleapis.com` (for networking, if not already enabled)

### Images & Build

- [ ] **All images pinned to a specific digest** (not floating tags):
  - [ ] `otel/opentelemetry-collector-contrib:0.114.0` (or reviewed version)
  - [ ] `grafana/grafana:11.3.0` (or reviewed version)
  - [ ] `grafana/alloy:v1.5.1` (or reviewed version)
  - [ ] `gke.gcr.io/prometheus-engine/frontend:v0.15.1` (or reviewed version)
  - [ ] Backend image tag reviewed and built (from `backend_image_tag` variable)
  - [ ] Analytics rollup image tag reviewed and built (from `analytics_rollup_image_tag` variable)

- [ ] **Android/web telemetry wiring complete** (for stg/prod only, TELEMETRY_ENABLED=true):
  - [ ] Android `BuildConfig.TELEMETRY_ENABLED = true` for this flavor
  - [ ] Admin-web `NEXT_PUBLIC_FLAVOR` set to this env (`dev`/`stg`/`prod`)
  - [ ] Faro receiver URL hardcoded (no env var guessing)

### Secrets & Access

- [ ] **Required secrets pre-created in Secret Manager:**
  - [ ] `goatos-<env>-grafana-admin-password` (strong password, not default)
  - [ ] `goatos-<env>-grafana-postgres-datasource-password` (for analytics rollup schema)
  - [ ] Both secrets have at least one version (use `gcloud secrets versions add`)

- [ ] **Service accounts provisioned and IAM reviewed:**
  - [ ] `goatos-grafana-<env>` has roles: `monitoring.viewer`, `cloudtrace.user`, `logging.viewer`,
    `bigquery.dataViewer`, `bigquery.jobUser`, `cloudsql.client`
  - [ ] `goatos-grafana-alloy-<env>` has similar roles + `storage.objectViewer` (for config)
  - [ ] `goatos-gmp-frontend-<env>` has `monitoring.viewer`
  - [ ] `goatos-analytics-rollup-<env>` has `bigquery.dataEditor`, `bigquery.jobUser`, `storage.objectViewer`, `bigquery.dataViewer` (GA4 export)
  - [ ] All backend/kernel Job SAs have `monitoring.metricWriter`, `cloudtrace.agent`, `logging.logWriter`

- [ ] **Access grants provisioned (for humans/agents):**
  - [ ] `var.observability_operator_members` populated with reviewed principals (e.g., `["user:ravi@mesha.sg"]`)
  - [ ] These principals will get `roles/run.invoker` on Grafana service

### GCP Resources

- [ ] **Cloud SQL Query Insights enabled** (INFRA.md §8 step 2):
  - [ ] `google_sql_database_instance.core` has `insights_config` block with reviewed settings
  - [ ] Query Insights runs continuously (no sampling mode), or sampling is intentional

- [ ] **GCS buckets created for config**:
  - [ ] `goatos-<env>-observability-config` exists in `asia-south1`
  - [ ] `infra/observability/otel-collector-config.yaml` object uploaded to bucket
  - [ ] `infra/observability/alloy-config.alloy` object uploaded to bucket
  - [ ] Bucket IAM: all producer SAs have `roles/storage.objectViewer`
  - [ ] Bucket lifecycle policy set to 30-day delete (optional, for old configs)

- [ ] **GCS provisioning bucket for Grafana**:
  - [ ] `goatos-<env>-grafana-provisioning` exists in `asia-south1`
  - [ ] All 6 dashboard JSON files (`infra/grafana/dashboards/*.json`) uploaded
  - [ ] `infra/grafana/provisioning/datasources/datasources.yaml` uploaded
  - [ ] Bucket IAM: `goatos-grafana-<env>` SA has `roles/storage.objectViewer`

- [ ] **BigQuery dataset created**:
  - [ ] `goatos_<env>_analytics_rollup` exists in `asia-south1` (not US multi-region)
  - [ ] Tables: `mobile_funnel_rollup`, `mobile_app_start_rollup`, `mobile_screen_render_rollup`,
    `mobile_crash_free_rollup` will be created by the rollup job (ok if empty before first run)
  - [ ] Conditional: if GA4 export is linked, `analytics_<property-id>` dataset IAM grants
    `roles/bigquery.dataViewer` to `goatos-analytics-rollup-<env>` SA

- [ ] **Cloud SQL user created** (backend-owned, not Terraform):
  - [ ] Postgres role `goatos_grafana_ro` exists on the `goatos-<env>` Cloud SQL instance
  - [ ] Role has `GRANT USAGE ON SCHEMA analytics; GRANT SELECT ON ALL TABLES IN SCHEMA analytics`
  - [ ] Default privileges set: `ALTER DEFAULT PRIVILEGES IN SCHEMA analytics GRANT SELECT ON TABLES TO goatos_grafana_ro`

### Observability Stack

- [ ] **Cardinality guard passed** (no raw paths/IDs in metric labels):
  - [ ] `make observability-cardinality-guard` (or CI guardrails job) passes
  - [ ] No metric uses `{path="..."}`, `{id="..."}`, `{goat_id="..."}` as a label
  - [ ] Approved labels only: `route` (template), `method`, `status_class`, `tenant_id`, `table_name`, `channel`

- [ ] **Metric naming validated**:
  - [ ] `make observability-metric-naming-guard` passes
  - [ ] All metrics in dashboards are defined in `backend/internal/platform/observability/telemetry.go`
  - [ ] Metric names include semantic suffixes (`_total`, `_seconds`, etc.) as declared in code

- [ ] **Region pinning verified**:
  - [ ] `make observability-region-guard` passes (or manual review)
  - [ ] Every resource with a location: explicitly set to `asia-south1`

### Telemetry Wiring (backend, mobile, web)

- [ ] **Backend telemetry guard passed**:
  - [ ] `make guardrails JOB=telemetry` passes
  - [ ] Every new API endpoint has telemetry (metrics, traces, logs — automatic via middleware)
  - [ ] Every new Kernel Job has telemetry initialization + graceful shutdown

- [ ] **Android telemetry guard passed** (if applicable):
  - [ ] `make telemetry-guard` passes
  - [ ] Every new screen/viewmodel references `AnalyticsEvents` (not inline strings)
  - [ ] Crashlytics `recordException` called in error paths
  - [ ] Funnel helpers invoked for tracked journeys

- [ ] **Web/admin-web telemetry wired** (if applicable):
  - [ ] Faro SDK initialized in `app/layout.tsx`
  - [ ] Receiver URL hardcoded per-flavor (not env var)
  - [ ] Error boundary wraps all routes
  - [ ] Sample routes have `faro.api.pushEvent()` calls for key actions

### Manual/Firebase Steps

- [ ] **Firebase GA4 export linked** (Firebase console, not Terraform):
  - [ ] Console → Project settings → Integrations → BigQuery → "Link"
  - [ ] Location explicitly set to `asia-south1` (Mumbai)
  - [ ] Export frequency: "Daily" (not streaming)
  - [ ] Resulting dataset ID captured and provided as `var.ga4_export_dataset_id`

- [ ] **Firebase Android app configured** (if mobile telemetry enabled):
  - [ ] `google-services.json` matches the env's Firebase project
  - [ ] `BuildConfig.TELEMETRY_ENABLED = true` for stg/prod, `false` for dev
  - [ ] Firebase crashlytics + analytics packages versioned and compatible

### Post-Apply Checks (run after terraform apply or imperative deployment)

- [ ] **Service accessibility verified**:
  - [ ] Grafana Cloud Run service is Ready (check console or `gcloud run services describe`)
  - [ ] Grafana is reachable: `gcloud run services proxy` or direct HTTPS URL
  - [ ] Login succeeds with admin + Secret Manager password

- [ ] **Datasources test-connect successfully** (in Grafana UI):
  - [ ] Google Managed Prometheus (via GMP-frontend proxy) → green
  - [ ] Google Cloud Trace → green
  - [ ] Google Cloud Logging → green
  - [ ] BigQuery (GA4 export dataset) → green (if GA4 linked)
  - [ ] Postgres (analytics schema) → green (after `goatos_grafana_ro` role created)

- [ ] **All 6 dashboards load** (no errors, panels may be empty before telemetry flows):
  - [ ] Dashboard 1: API / RED
  - [ ] Dashboard 2: Database
  - [ ] Dashboard 3: Kernel pipeline
  - [ ] Dashboard 4: Frontend RUM
  - [ ] Dashboard 5: Mobile
  - [ ] Dashboard 6: SLO / burn

- [ ] **Alert policies created and wired**:
  - [ ] 5 new SLO/burn policies exist in Cloud Monitoring
  - [ ] Notification channels point to `var.monitoring_alert_email_addresses`

- [ ] **Backend telemetry canary rolled out** (if this is not dev):
  - [ ] One Kernel Job redeployed with `GOATOS_OBS_SINK=otlp`
  - [ ] Metrics/traces/logs flow to Grafana within 15 seconds
  - [ ] OTel Collector sidecar logs show no errors
  - [ ] Full rollout to api + all kernel Jobs, then re-verify all dashboards

- [ ] **Analytics rollup tested** (if applicable):
  - [ ] Cloud Run Job `goatos-<env>-analytics-rollup` can execute without errors
  - [ ] Postgres tables `mobile_*_rollup` exist in `analytics` schema
  - [ ] One manual job run: `gcloud run jobs execute goatos-<env>-analytics-rollup --wait`
  - [ ] BigQuery dataset populated (or will populate on next scheduled run)

### Final Sign-Off

- [ ] **No uncommitted changes in infra/**, **apps/** (for this env's deployment)
- [ ] **All guardrail gates passing** locally:
  ```bash
  make guardrails JOB=observability-cardinality-guard
  make guardrails JOB=observability-metric-naming-guard
  make guardrails JOB=observability-region-guard
  make telemetry-guard  # if applicable
  ```
- [ ] **Deployment log captured** (for audit trail):
  ```bash
  terraform apply 2>&1 | tee /tmp/observability-<env>-apply-$(date +%s).log
  # or for imperative: gcloud run deploy ... 2>&1 | tee /tmp/observability-deploy.log
  ```
- [ ] **Post-deploy verification complete** (checklist from PROD_PROMOTION.md)
- [ ] **Handoff ready**: Grafana URL, admin password (Secret Manager, not chat), dashboard links,
  telemetry confirmation documented

---

## Sync with existing guards

This ledger complements and cross-links these existing Goat OS guardrails:

| Guard | Coverage | Relation to this ledger |
|---|---|---|
| `make telemetry-guard` | Android/web analytics, crashes, funnels | Finding #3, #14, #15, #17 |
| `make scale-guard` | N+1 queries, unbounded loops, compute-on-read | Finding #3 (worker flush context) |
| `make mobile-guard` | Mobile list fetch, pagination | Finding #3 (bounded contexts) |
| `make clinical-defer-guard` | Medical safety (unrelated) | — |
| CI guardrails job | All checks above + more | Findings #1, #2, #9, #19, #20 |

Every observability deployment to a new env should run all these guards as part of the
pre-deploy checklist.

