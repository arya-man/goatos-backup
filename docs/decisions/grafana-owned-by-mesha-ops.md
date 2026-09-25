# Grafana / Observability Is Owned By vgoats/mesha-ops

Status: accepted (maintainer decision, 2026-09-25)

## Decision

Grafana, Grafana Alloy (the Faro RUM receiver), dashboards, datasources and the
Cloud Monitoring / Slack alerting built on them are owned and deployed ONLY by
`vgoats/mesha-ops` (`grafana/cloudbuild.yaml`, run by hand or by its own trigger).
The Goat OS STG deploy (Slack button -> Cloud Build -> Cloud Deploy) does not
deploy them, does not smoke them, and cannot fail because of them. There is no
goatos -> mesha-ops trigger.

## Why

- A backend/web release was being blocked or failed by an unrelated Grafana
  datasource, dashboard query or custom-domain check.
- The observability apply, deploy-time analytics rollup execution and dashboard
  smoke added roughly 4-5 minutes to every backend+web deploy.
- Two repos deploying the same Grafana service is two deployment authorities.

## What moved out of goatos

- `stg-clouddeploy-task.sh`: observability apply/smoke, the deploy-time
  `goatos-stg-analytics-rollup` execution, and the observabilityOnly /
  grafanaDomainOnly / grafanaSsoOnly / alloyImage Cloud Deploy modes.
- `stg-cloudbuild-release.sh`: the Grafana dashboard smoke.
- `cloudbuild.stg.yaml` / `stg-clouddeploy-release.sh`: the Alloy image input.
- Helper scripts, the runner image payload, and `grafana-durability-guard`.

Kept in goatos: the analytics-rollup job definition, image, env and worker IAM
(the kernel worker triggers it), the `NEXT_PUBLIC_FARO_COLLECTOR_URL` build arg,
and the Terraform-referenced config sources under `infra/grafana` and
`infra/observability`. Moving that Terraform state to mesha-ops is a follow-up
and needs a deliberate `terraform state` move, not a delete.

## Contract between the repos

Goat OS promises stable emitted telemetry names. mesha-ops dashboards depend on
them:

- Prometheus/OTel metric names and labels emitted by the API, worker and
  analytics rollup (`goatos_*`, `logging_googleapis_com:user_goatos_*`).
- The Faro collector URL baked into admin-web
  (`NEXT_PUBLIC_FARO_COLLECTOR_URL`).
- Analytics rollup tables (`app_crash_daily`, `app_network_daily`, and related).

## Residual risk

Renaming a metric, label or rollup table in goatos can silently break a
mesha-ops dashboard, because no goatos check sees those dashboards any more.
Follow-up: a scheduled dashboard smoke in mesha-ops that alerts on panels
returning errors or no data.
