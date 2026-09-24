# Grafana: independent deploy

Grafana (`https://grafana.mesha.sg`, Cloud Run `goatos-stg-grafana`) and its
Alloy collector (`goatos-stg-grafana-alloy`) are ops tooling. They ship on their
own pipeline and never wait for, or ride along with, a Goat OS STG release.

| What | Owned by |
|---|---|
| Dashboards + provisioning (`infra/grafana/**`), Faro log metrics (`infra/observability/faro-log-metrics.json`), Alloy image pin | `cloudbuild.grafana.yaml` -> `tools/deploy/stg-observability.py deploy` |
| Live proof: auth boundary + dashboard query smoke | `cloudbuild.grafana.yaml` -> `tools/deploy/smoke-stg-grafana-dashboards.mjs --query-validity-only` |
| Grafana service, bucket, IAM, secrets | Terraform: `infra/envs/stg/observability.tf`, `secrets.tf` |
| Break-glass Cloud Deploy modes (observabilityOnly, Grafana SSO, Grafana domain) | `tools/deploy/stg-clouddeploy-task.sh` (unchanged) |

## Deploy

```sh
gcloud config get account   # ravi@mesha.sg; project goatos-stg (vgoats.com)
gcloud builds submit --project=goatos-stg --config=cloudbuild.grafana.yaml .
```

By default the build keeps the Alloy image currently running on
`goatos-stg-grafana-alloy` (resolved to its `@sha256:` digest). To roll a new
Alloy image, pass `--substitutions=_ALLOY_IMAGE=asia-south1-docker.pkg.dev/goatos-stg/goatos/grafana-alloy@sha256:<64hex>`.

Steps, all fail-closed: resolve Alloy digest -> apply assets/metrics/Alloy ->
auth boundary (`/login` 200, anonymous `/api/search` 401) -> live dashboard
query smoke. A green build is deployment/query validity only; full-data
certification stays in `docs/observability/READINESS_CERTIFICATION.md`.

`make grafana-durability-guard` requires this pipeline to keep the apply and the
fail-closed smoke, and rejects the smoke reappearing in the release wrapper.

## First-run checks

- `goatos-github-deploy-stg` can still invoke the smoke: Terraform
  `grafana_deploy_smoke_invoker` and `grafana_admin_password_deploy_smoke_accessor`.
- The Alloy service image is a `grafana-alloy@sha256:` digest (the build refuses
  anything else).
- The build log shows `Grafana auth boundary ok` and
  `Grafana query validation passed`.
