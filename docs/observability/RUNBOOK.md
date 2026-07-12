# Observability Runbook — Deploy, Verify, Operate

> Companion to `INFRA.md` (resource-level Terraform detail — this doc does not
> duplicate it, only cross-links) and `OBSERVABILITY_DESIGN.md` (architecture).
> Env: **goatos-stg**, region **asia-south1**.

## 0. Before you touch anything: confirm context

Per the org boundary rule, state and verify the active account/org/project
before any mutating command:

```text
Correct account:       ravi@mesha.sg
Correct GCP org:        vgoats.com
Correct project:        goatos-stg
Wrong for this work:    any Heva/Slice project or account, goatos-sheets
```

```bash
gcloud config get-value account
gcloud config get-value project
gcloud organizations list   # confirm vgoats.com if in doubt
```

If any of these don't match, stop and fix context before proceeding
(`gcloud config set project goatos-stg`, `gcloud auth login ravi@mesha.sg`).

## 1. Deploy (ordered)

Nothing in this stack has been applied yet — live `goatos-stg` resources
still need import into `gs://goatos-stg-tf-state` first (see
`infra/envs/stg/README.md`). Once that's done:

```bash
# 1. Confirm project context (see §0)
gcloud config set project goatos-stg

# 2. Init the real backend (not -backend=false)
cd infra/envs/stg
terraform init

# 3. Targeted plan — observability resources only, so this never touches the
#    live api/admin-web/kernel-job resources in main.tf/cloud_run_services.tf/
#    cloud_run_jobs.tf. Supply the two no-default vars explicitly (see
#    INFRA.md §8 — they intentionally have no default so no principal/actor
#    is guessed).
terraform plan \
  -var='monitoring_alert_email_addresses=["ravi@mesha.sg"]' \
  -var='stg_sweeper_actor_id=<reviewed workforce_member_id>' \
  -var='observability_operator_members=["user:ravi@mesha.sg"]' \
  -out=observability.tfplan

# 4. Review the plan — it must show ONLY additive resources (5 service
#    accounts, 4 Cloud Run services, 1 Cloud Run Job, 1 Scheduler job, 2 GCS
#    buckets + objects, 1 BigQuery dataset, 2 secrets, 1 in-place Cloud SQL
#    update for insights_config, 5 new alert policies) — nothing destructive
#    to api/admin_web/kernel jobs. See INFRA.md §7 for the full resource list
#    and the recommended staged sub-apply order (secrets → Cloud SQL insights
#    → collector/gmp-frontend → grafana → alloy → full apply).
terraform apply observability.tfplan
```

For the resource-by-resource detail (why 5 SAs for 3 named services, the
Cloud Run single-ingress-port constraint, Grafana datasource list, regionality
caveats) — see **`INFRA.md`**, not this doc.

```bash
# 5. Enable OTLP export on the backend api + 13 kernel Jobs (backend lane's
#    change, not Terraform's — this flips the existing stubbed sink live).
#    Set on each Cloud Run service/job's env, or via the stg-branch CI deploy
#    pipeline config:
GOATOS_OBS_SINK=otlp
GOATOS_OTLP_ENDPOINT=<terraform output observability_cloud_run_services.otel_collector>

# 6. Deploy the backend OTel-enabled image via the stg-branch CI pipeline
#    (same deploy path as any other backend release — see
#    docs/runbooks/github-workflows.md for the CI/CD job detail). Do not
#    hand-deploy a one-off image outside that pipeline.
```

## 2. Verify telemetry is flowing

Run this checklist after any deploy that touches the collector, Grafana, or a
telemetry producer (backend api/kernel jobs/admin-web/mobile).

### 2.1 Generate traffic

```bash
# Hit a handful of real API routes to produce RED metrics + traces + logs.
curl -sS https://<api-stg-url>/app/bootstrap -H "Authorization: Bearer <token>"
# Repeat for 2-3 more representative routes (read + write) so p50/p90 aren't
# single-sample noise.
```

### 2.2 Check GMP has metrics

```bash
gcloud monitoring time-series list \
  --project=goatos-stg \
  --filter='metric.type="prometheus.googleapis.com/http_server_request_duration_seconds/histogram"' \
  --interval-start-time="$(date -u -v-15M +%Y-%m-%dT%H:%M:%SZ)" \
  --interval-end-time="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
```

Expect at least one series. If empty: check the OTel Collector's own Cloud
Run logs for exporter errors (`gcloud run services logs read
goatos-stg-otel-collector --project=goatos-stg --region=asia-south1`), and
confirm the caller (api/job) actually has `roles/run.invoker` on the
collector (`otel_collector_invoker` binding in `observability.tf`).

### 2.3 Check Cloud Trace has spans

```bash
gcloud logging read \
  'resource.type="cloud_run_revision" AND resource.labels.service_name="goatos-stg-api"' \
  --project=goatos-stg --limit=5 --format='value(trace)'
```

Pick a trace id from the output and confirm it resolves in Cloud Trace
(console, or `gcloud trace` if the CLI surface is available in your gcloud
version). A healthy trace for a write request should show spans from the API
handler down through the DB query **and** — if the write triggered an outbox
publish — the async consumer/obligation spans on the same trace, since
`traceparent` is propagated through the outbox envelope
(`OBSERVABILITY_DESIGN.md` §2.2).

### 2.4 Open each Grafana dashboard and confirm panels populate

Using the access flow in `GRAFANA_ACCESS.md`:

```bash
# Headless check via API — confirm the dashboard resolves and its panels'
# datasource queries return data (spot-check a couple of panel queries
# rather than every one).
curl -sS "$GRAFANA_STG_URL/api/dashboards/uid/api-red" \
  -H "Authorization: Bearer $GRAFANA_STG_TOKEN"
```

Walk all 6 (`docs/observability/README.md` has the full list) after traffic
generation:

1. **API/RED** — request rate + p50/90/99 should show non-zero data for the
   routes hit in §2.1.
2. **DB** — query duration panels populate; Cloud SQL Insights panel needs the
   `insights_config` apply from step 2 of §1 to have landed.
3. **Kernel pipeline** — needs a write that triggers an outbox event (e.g. a
   goat move/stage change) to show non-zero outbox/consumer panels.
4. **Frontend RUM** — needs admin-web's Faro SDK wired and a real browser
   session hitting it; empty until that lane's rollout step runs.
5. **Mobile** — needs the analytics rollup job to have run at least once
   (`goatos-stg-analytics-rollup`, `03:15 IST` daily, or trigger manually —
   see §4) AND the `analytics.mobile_*_rollup` tables to exist (backend-owned
   migration, see `INFRA.md` §9).
6. **SLO/burn** — populates automatically once dashboards 1-3 have data; burn
   rate needs a longer observation window (hours, not minutes) to be
   meaningful.

If a panel is empty, check the specific gap first (Faro not wired, rollup not
run, GA4→BQ link not done) before assuming a plumbing bug — several of these
are known-pending per `docs/observability/README.md`'s rollout status.

## 3. Day-2 operations

### 3.1 Add a new dashboard

1. Export the dashboard JSON from a Grafana editing session, or hand-author
   it following the shape of `infra/grafana/dashboards/01-api-red.json` etc.
2. Drop the file in `infra/grafana/dashboards/<NN>-<name>.json`.
3. `terraform apply` (the `google_storage_bucket_object.grafana_dashboard_jsons`
   resource uses `fileset(...)`, so a new file is picked up automatically —
   no Terraform code change needed).
4. Grafana's file-provider (`infra/grafana/provisioning/dashboards/dashboards.yaml`)
   polls the mounted directory; a new Cloud Run revision (triggered by the
   `terraform apply` in step 3) picks it up on cold start.
5. Verify it renders and its panels resolve against the intended datasource
   (see the 6-datasource table in `INFRA.md` §4).

### 3.2 Rotate the Grafana token

If a `GRAFANA_STG_TOKEN` leaks (committed, logged, pasted somewhere it
shouldn't be):

```bash
# 1. Revoke the old service-account token via the Grafana API (§2 of
#    GRAFANA_ACCESS.md for the auth pattern):
curl -sS -X DELETE "$GRAFANA_STG_URL/api/serviceaccounts/<SA_ID>/tokens/<TOKEN_ID>" \
  -H "Authorization: Bearer $ID_TOKEN" \
  -u "admin:$GRAFANA_ADMIN_PASSWORD"

# 2. Mint a fresh one (GRAFANA_ACCESS.md §2 step (e)).
# 3. Update ~/.zshrc locally for every developer/agent that held the old
#    token. There is no GitHub secret to rotate today (GRAFANA_ACCESS.md §5)
#    — only do that once that promotion trigger has actually fired.
```

If the Grafana **admin** password itself is compromised, rotate the Secret
Manager secret instead:

```bash
gcloud secrets versions add goatos-stg-grafana-admin-password \
  --project=goatos-stg --data-file=- <<< "<new-strong-password>"
# Then force a new Cloud Run revision so Grafana picks up the new secret
# version (Cloud Run reads `version = "latest"` at container start, not live):
gcloud run services update goatos-stg-grafana \
  --project=goatos-stg --region=asia-south1 --no-traffic=false
```

### 3.3 Adjust the trace sample ratio

Sampling is controlled by `GOATOS_TRACE_SAMPLE_RATIO`
(`OBSERVABILITY_DESIGN.md` §3): default `0.1` in stg, `1.0` in dev. To change
it in stg, update the env var on the backend api / kernel job Cloud Run
services (same deploy path as any other config change — via the stg-branch
CI pipeline, not a manual `gcloud run services update` unless doing a quick
rollback). Raise it temporarily (e.g. to `1.0`) when debugging a specific
incident that needs full trace coverage, then revert — 100% sampling in stg
is fine cost-wise short-term but not the steady-state default.

### 3.4 Respond to an SLO alert

The 5 SLO/burn alert policies (`INFRA.md` §2) page
`monitoring_alert_email_addresses`. On a page:

1. Open the **SLO/burn** dashboard (#6) to see which SLO is burning and how
   fast (multi-window burn rate — a fast-burn short-window alert means "act
   now", a slow-burn long-window alert means "this week, not tonight").
2. Cross-reference the **relevant signal dashboard**:
   - API availability/latency burn → dashboard 1 (API/RED), then Cloud Trace
     for the specific slow/failing route.
   - Consumer-lag burn → dashboard 3 (Kernel pipeline) — check outbox queue
     depth and DLQ first; a growing DLQ usually means a poison message, not
     capacity.
   - Notification-failure-rate burn → dashboard 3 — check success-by-channel
     panel to isolate which channel (FCM/email/Slack/webhook) is failing.
3. If the root cause is a bad deploy, roll back via the same CI pipeline used
   to deploy (see `docs/runbooks/github-workflows.md`), not a manual
   `gcloud run services update --image=...` unless the pipeline itself is
   down.
4. Document the incident per whatever incident process this repo tracks
   elsewhere — this runbook only covers the observability-specific triage
   steps, not incident-management process.

## 4. Manually trigger the analytics rollup job (off-schedule)

Useful when verifying dashboard 5 (Mobile) without waiting for the
`03:15 IST` daily schedule:

```bash
gcloud run jobs execute goatos-stg-analytics-rollup \
  --project=goatos-stg --region=asia-south1 --wait
```

Requires the GA4→BigQuery Firebase-console link to already be done and
`var.ga4_export_dataset_id` to be set (`INFRA.md` §9) — otherwise the job has
nothing to read yet.

## 5. Cross-references

- Resource-level Terraform detail, apply order staging, datasource list,
  regionality caveats, and known assumptions/risks: **`INFRA.md`**.
- Architecture, signal design, SLO targets: **`OBSERVABILITY_DESIGN.md`**.
- Access/auth flow for Grafana: **`GRAFANA_ACCESS.md`**.
- CI guardrail requiring telemetry on every new feature: **`TELEMETRY_GUARDRAILS.md`**.
- Backend logging ADR (`slog`, `GOATOS_OBS_SINK`): **`../decisions/observability.md`**.
