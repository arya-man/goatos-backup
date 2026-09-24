# Cost alert bridge: independent deploy

`goatos-stg-cost-alert-bridge` posts GCP budget, billing-anomaly and media-cost
alerts to Slack `#goatos-stg-cost-alerts`. It is an ops tool, not part of the
Goat OS product, so it ships on its own pipeline and never waits for a Goat OS
STG release.

| What | Owned by |
|---|---|
| Bridge image (code) | `cloudbuild.cost-alert-bridge.yaml` + `deploy/cost-alert-bridge/Dockerfile` |
| Env vars, secrets, IAM, state bucket, budget, Pub/Sub | Terraform: `infra/envs/stg/cost_alerts.tf`, `cloud_run_services.tf` |

Terraform ignores the service's image (`lifecycle.ignore_changes`), so an apply
never rolls the bridge back to an older backend image.

## Ship a bridge code change

```sh
gcloud config get account   # ravi@mesha.sg
gcloud builds submit --project=goatos-stg --config=cloudbuild.cost-alert-bridge.yaml .
```

The build runs `go test -race ./cmd/cost-alert-bridge`, builds the bridge-only
image, rolls the one Cloud Run service and checks `/readyz`. Nothing else in
Goat OS is touched. Optional: create a manual Cloud Build trigger pointing at
this file to get a one-click button.

## Change config only (channel, thresholds, budget)

Edit the Terraform and apply it; no image build is needed. Budget thresholds
live in `google_billing_budget.goatos_monthly_forecast_slack_alerts`.

## First rollout

Before the first run, confirm the state bucket and its IAM exist (the bridge
refuses to start without `GOATOS_COST_ALERT_STATE_BUCKET`):

```sh
gcloud storage buckets describe gs://goatos-stg-cost-alert-state --project=goatos-stg
gcloud run services describe goatos-stg-cost-alert-bridge --project=goatos-stg \
  --region=asia-south1 --format='value(spec.template.spec.containers[0].env)' | tr ';' '\n' | grep STATE_BUCKET
```

If either is missing, apply the `cost_alerts.tf` resources first. Delivery
semantics (once per threshold, 24h reminder) are in
`cost-alert-budget-delivery.md`.
