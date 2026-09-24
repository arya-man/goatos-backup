#!/usr/bin/env node
// Static guard for the staging Grafana provisioning lane.
//
// If infra/grafana/dashboards/*.json exists, CI must prove the files are valid,
// Terraform still uploads them into the mounted GCS provisioning bucket, and the
// standalone Grafana pipeline (cloudbuild.grafana.yaml) still applies the assets
// and runs the live Grafana dashboard smoke fail-closed. Grafana is deliberately NOT
// part of the Goat OS STG release (see docs/runbooks/grafana-deploy.md).

import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, mkdirSync, readFileSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");

const defaults = {
  dashboardDir: "infra/grafana/dashboards",
  dashboardsProvider: "infra/grafana/provisioning/dashboards/dashboards.yaml",
  datasourcesProvider: "infra/grafana/provisioning/datasources/datasources.yaml",
  terraform: "infra/envs/stg/observability.tf",
  monitoring: "infra/envs/stg/monitoring.tf",
  secrets: "infra/envs/stg/secrets.tf",
  grafanaPipeline: "cloudbuild.grafana.yaml",
  deployScript: "tools/deploy/stg-cloudbuild-release.sh",
  cloudDeployReleaseScript: "tools/deploy/stg-clouddeploy-release.sh",
  smokeScript: "tools/deploy/smoke-stg-grafana-dashboards.mjs",
};

const featurePanelRequirements = [
  { feature: "Weights", routeRegex: ".*weighing.*|.*growth-director.*" },
  { feature: "Vaccination / PC", routeRegex: ".*vaccination.*|.*preventive.*|.*protocol-adherence.*|.*action-center.*" },
  { feature: "Work Board / Tasks", routeRegex: ".*work-board.*|.*workflows.*|.*tasks.*|.*approvals.*|.*verify.*" },
  { feature: "Feed", routeRegex: ".*feed.*" },
  { feature: "Sales", routeRegex: ".*sales.*" },
  { feature: "Procurement", routeRegex: ".*procurement.*" },
  { feature: "Counts / Milk", routeRegex: ".*counts.*|.*milk.*" },
  { feature: "Health / Herd Signals", routeRegex: ".*health.*|.*herd-signals.*" },
];

function readText(root, relativePath) {
  return readFileSync(path.join(root, relativePath), "utf8");
}

function dashboardFiles(root, dashboardDir = defaults.dashboardDir) {
  const dir = path.join(root, dashboardDir);
  return readdirSync(dir)
    .filter((name) => name.endsWith(".json"))
    .sort();
}

function includesAll(text, snippets, label, problems) {
  for (const snippet of snippets) {
    if (!text.includes(snippet)) problems.push(`${label}: missing ${JSON.stringify(snippet)}`);
  }
}

// Grafana Cloud Monitoring's getFilter consumes key/operator/value/AND tokens.
// Regex operands use =~; passing a raw Cloud Monitoring filter is ignored.
function grafanaFilterString(filters) {
  if (!Array.isArray(filters) || filters.length % 4 !== 3 || filters.some((x) => typeof x !== "string")) return null;
  const clauses = [];
  for (let i = 0; i < filters.length; i += 4) {
    const [key, op, value, join] = filters.slice(i, i + 4);
    if (!/^(metric|resource)\.(type|label\.[\w]+)$/.test(key) || !["=", "!=", "=~", "!=~"].includes(op) || (join !== undefined && join !== "AND")) return null;
    clauses.push(`${key}${op.replace("~", "")}${op.endsWith("~") ? `monitoring.regex.full_match(${JSON.stringify(value)})` : JSON.stringify(value)}`);
  }
  return clauses.join(" AND ");
}

function collectFilterStrings(value, out = []) {
  if (!value || typeof value !== "object") return out;
  if (Array.isArray(value)) {
    for (const item of value) collectFilterStrings(item, out);
    return out;
  }
  for (const [key, child] of Object.entries(value)) {
    if (key === "filter" && typeof child === "string") out.push(child);
    if (key === "filters" && grafanaFilterString(child)) out.push(grafanaFilterString(child));
    collectFilterStrings(child, out);
  }
  return out;
}

function validateGrafanaFilterShape(filter, label, problems) {
  if (filter.includes("prometheus.googleapis.com/") && !/resource\.type\s*=\s*"prometheus_target"/.test(filter)) {
    problems.push(`${label}: GMP metric filters must include resource.type = "prometheus_target"`);
  }
  if (filter.includes("run.googleapis.com/") && !/resource\.type\s*=\s*"cloud_run_revision"/.test(filter)) {
    problems.push(`${label}: Cloud Run metric filters must include resource.type = "cloud_run_revision"`);
  }
  if (filter.includes("cloudsql.googleapis.com/") && !/resource\.type\s*=\s*"cloudsql_database"/.test(filter)) {
    problems.push(`${label}: Cloud SQL metric filters must include resource.type = "cloudsql_database"`);
  }
  if (filter.includes("pubsub.googleapis.com/") && !/resource\.type\s*=\s*"pubsub_subscription"/.test(filter)) {
    problems.push(`${label}: Pub/Sub metric filters must include resource.type = "pubsub_subscription"`);
  }
}

function panelTargets(panel) {
  return Array.isArray(panel?.targets) ? panel.targets : [];
}

function findPanelByTitle(doc, title) {
  return Array.isArray(doc?.panels) ? doc.panels.find((panel) => panel?.title === title) : null;
}

function panelHasGmpFilter(panel, { metric, routeRegex }) {
  return panelTargets(panel).some((target) => {
    const filter = grafanaFilterString(target?.timeSeriesList?.filters);
    return typeof filter === "string"
      && filter.includes(`metric.type="${metric}"`)
      && filter.includes('resource.type="prometheus_target"')
      && filter.includes(`metric.label.route=monitoring.regex.full_match("${routeRegex}")`);
  });
}

function validateFeatureHealthPanels(doc, relative, problems) {
  for (const { feature, routeRegex } of featurePanelRequirements) {
    const requestTitle = `${feature}: request rate by route`;
    const latencyTitle = `${feature}: p95 latency by route`;
    const requestPanel = findPanelByTitle(doc, requestTitle);
    const latencyPanel = findPanelByTitle(doc, latencyTitle);
    if (!requestPanel) {
      problems.push(`${relative}: missing feature-wise request panel ${JSON.stringify(requestTitle)}`);
    } else if (!panelHasGmpFilter(requestPanel, {
      metric: "prometheus.googleapis.com/http_server_requests_total/counter",
      routeRegex,
    })) {
      problems.push(`${relative}: ${requestTitle} must query current GMP request telemetry for ${routeRegex}`);
    }
    if (!latencyPanel) {
      problems.push(`${relative}: missing feature-wise latency panel ${JSON.stringify(latencyTitle)}`);
    } else if (!panelHasGmpFilter(latencyPanel, {
      metric: "prometheus.googleapis.com/http_server_request_duration_seconds/histogram",
      routeRegex,
    })) {
      problems.push(`${relative}: ${latencyTitle} must query current GMP latency telemetry for ${routeRegex}`);
    }
  }
  const apmStatus = findPanelByTitle(doc, "APM status");
  const apmContent = apmStatus?.options?.content ?? "";
  for (const pending of ["Faro/RUM", "Cloud Trace", "traceparent"]) {
    if (!apmContent.includes(pending)) {
      problems.push(`${relative}: APM status must explicitly document pending ${pending} wiring instead of implying end-to-end APM is complete`);
    }
  }
}

export function validate(root = repo) {
  const problems = [];
  const files = dashboardFiles(root);

  if (files.length === 0) {
    return problems;
  }

  const seenUids = new Map();
  for (const file of files) {
    const relative = `${defaults.dashboardDir}/${file}`;
    let doc;
    try {
      doc = JSON.parse(readText(root, relative));
    } catch (error) {
      problems.push(`${relative}: invalid JSON (${error.message})`);
      continue;
    }
    if (!doc || typeof doc !== "object" || Array.isArray(doc)) {
      problems.push(`${relative}: dashboard JSON must be an object`);
      continue;
    }
    if (typeof doc.uid !== "string" || doc.uid.trim() === "") {
      problems.push(`${relative}: missing stable dashboard uid`);
    } else if (seenUids.has(doc.uid)) {
      problems.push(`${relative}: duplicate dashboard uid ${doc.uid} also used by ${seenUids.get(doc.uid)}`);
    } else {
      seenUids.set(doc.uid, relative);
    }
    if (typeof doc.title !== "string" || doc.title.trim() === "") {
      problems.push(`${relative}: missing dashboard title`);
    } else if (/\bGoat OS stg\b/i.test(doc.title)) {
      problems.push(`${relative}: dashboard title must use Goat OS, not Goat OS stg`);
    }
    if (!Array.isArray(doc.panels) || doc.panels.length === 0) {
      problems.push(`${relative}: dashboard must contain at least one panel`);
    }
    const serialized = JSON.stringify(doc);
    for (const panel of doc.panels ?? []) {
      for (const target of panelTargets(panel)) {
        if (target.queryType === "promQL") {
          if (!target.promQLQuery?.expr || !target.promQLQuery?.projectName || !target.promQLQuery?.step
            || !target.timeSeriesList || Object.keys(target.timeSeriesList).length !== 0) {
            problems.push(`${relative}: PromQL requires expression/project/step and empty timeSeriesList migration sentinel for Grafana 11.3`);
          }
          continue;
        }
        if (!target.timeSeriesList) continue;
        const query = target.timeSeriesList;
        const cumulativeHistogram = query.filters?.some((value) => typeof value === "string" && value.startsWith("prometheus.googleapis.com/") && value.endsWith("/histogram"));
        if (cumulativeHistogram && (query.perSeriesAligner?.startsWith("ALIGN_PERCENTILE_")
          || (query.crossSeriesReducer?.startsWith("REDUCE_PERCENTILE_") && query.perSeriesAligner !== "ALIGN_DELTA"))) {
          problems.push(`${relative}: cumulative GMP histograms require ALIGN_DELTA before REDUCE_PERCENTILE`);
        }
        if ("filter" in target.timeSeriesList || !grafanaFilterString(target.timeSeriesList.filters)
          || !target.timeSeriesList.filters.some((token, index, tokens) => index % 4 === 0 && token === "metric.type" && tokens[index + 1] === "=" && tokens[index + 2])) {
          problems.push(`${relative}: timeSeriesList.filters must use Grafana key/operator/value/AND tokens; singular filter is ignored`);
        }
      }
    }
    if (serialized.includes("resource.labels.")) {
      problems.push(`${relative}: Cloud Monitoring filters must use resource.label.*, not resource.labels.*`);
    }
    if (serialized.includes('resource.type="generic_task"') || serialized.includes("fetch generic_task ::")) {
      problems.push(`${relative}: GMP metrics in goatos-stg are prometheus_target, not generic_task`);
    }
    for (const filter of collectFilterStrings(doc)) {
      validateGrafanaFilterShape(filter, relative, problems);
    }
    if (doc.uid === "goatos-feature-health") {
      validateFeatureHealthPanels(doc, relative, problems);
    }
  }

  const dashboardsYaml = readText(root, defaults.dashboardsProvider);
  includesAll(dashboardsYaml, [
    "apiVersion: 1",
    "providers:",
    "type: file",
    "path: /mnt/grafana-provisioning/dashboards",
  ], defaults.dashboardsProvider, problems);

  const datasourcesYaml = readText(root, defaults.datasourcesProvider);
  includesAll(datasourcesYaml, [
    "apiVersion: 1",
    "datasources:",
    "uid: gmp-prometheus",
    "uid: cloud-monitoring",
    "uid: postgres-analytics",
  ], defaults.datasourcesProvider, problems);

  const terraform = readText(root, defaults.terraform);
  includesAll(terraform, [
    'name  = "GF_PATHS_PROVISIONING"',
    'value = "/mnt/grafana-provisioning/provisioning"',
    'name       = "grafana-provisioning"',
    'mount_path = "/mnt/grafana-provisioning"',
    'resource "google_storage_bucket" "grafana_provisioning"',
    'resource "google_storage_bucket_object" "grafana_datasources"',
    'name   = "provisioning/datasources/datasources.yaml"',
    'source = "${path.module}/../../grafana/provisioning/datasources/datasources.yaml"',
    'resource "google_storage_bucket_object" "grafana_dashboards_provider"',
    'name   = "provisioning/dashboards/dashboards.yaml"',
    'source = "${path.module}/../../grafana/provisioning/dashboards/dashboards.yaml"',
    'resource "google_storage_bucket_object" "grafana_dashboard_jsons"',
    'for_each = fileset("${path.module}/../../grafana/dashboards", "*.json")',
    'name   = "dashboards/${each.value}"',
    'source = "${path.module}/../../grafana/dashboards/${each.value}"',
    'resource "google_cloud_run_v2_service_iam_member" "grafana_deploy_smoke_invoker"',
    'member   = "serviceAccount:${google_service_account.github_deployer.email}"',
    'resource "google_storage_bucket_iam_member" "grafana_provisioning_grafana_reader"',
    'member = "serviceAccount:${google_service_account.grafana.email}"',
  ], defaults.terraform, problems);

  const monitoring = readText(root, defaults.monitoring);
  if (/resource\.type=\\?"generic_task\\?"/.test(monitoring)) {
    problems.push(`${defaults.monitoring}: GMP metrics in goatos-stg are prometheus_target, not generic_task`);
  }
  for (const [index, line] of monitoring.split("\n").entries()) {
    if (!line.includes("prometheus.googleapis.com/")) continue;
    if (line.includes("resource.label.")) {
      problems.push(`${defaults.monitoring}:${index + 1}: Cloud Monitoring alert filters must use resource.labels.*, not Grafana query resource.label.*`);
    }
    if (line.includes("metric.label.")) {
      problems.push(`${defaults.monitoring}:${index + 1}: Cloud Monitoring alert filters must use metric.labels.*, not Grafana query metric.label.*`);
    }
  }

  const secrets = readText(root, defaults.secrets);
  includesAll(secrets, [
    'resource "google_secret_manager_secret_iam_member" "grafana_admin_password_deploy_smoke_accessor"',
    "secret_id = google_secret_manager_secret.grafana_admin_password.id",
    'member    = "serviceAccount:${google_service_account.github_deployer.email}"',
  ], defaults.secrets, problems);

  const pipeline = readText(root, defaults.grafanaPipeline);
  includesAll(pipeline, [
    "goatos-github-deploy-stg@goatos-stg.iam.gserviceaccount.com",
    "python3 tools/deploy/stg-observability.py deploy --assets infra/grafana",
    "--log-metrics infra/observability/faro-log-metrics.json",
    "grafana-alloy@sha256:[a-f0-9]{64}",
    "node tools/deploy/smoke-stg-grafana-dashboards.mjs",
    "--query-validity-only",
    "--firebase-initial-export-receipt infra/observability/firebase-initial-export.json",
    "/api/search",
    "401",
    "- -ceu",
  ], defaults.grafanaPipeline, problems);
  if (/REQUIRE_GRAFANA_SMOKE|allowFailure|\|\|\s*true|set \+e/.test(pipeline)) {
    problems.push(`${defaults.grafanaPipeline}: Grafana smoke must fail closed; remove warning-only bypass (REQUIRE_GRAFANA_SMOKE / allowFailure / || true / set +e)`);
  }
  const deployScript = readText(root, defaults.deployScript);
  if (/smoke-stg-grafana-dashboards\.mjs/.test(deployScript)) {
    problems.push(`${defaults.deployScript}: Grafana smoke belongs to ${defaults.grafanaPipeline}, not the Goat OS STG release`);
  }

  const cloudDeployReleaseScript = readText(root, defaults.cloudDeployReleaseScript);
  includesAll(cloudDeployReleaseScript, [
    "--build-arg NEXT_PUBLIC_FARO_COLLECTOR_URL=https://goatos-stg-grafana-alloy-awtrpmn4za-el.a.run.app/collect",
    "--build-arg NEXT_PUBLIC_GOATOS_ENV=stg",
    "--build-arg NEXT_PUBLIC_APP_VERSION=",
  ], defaults.cloudDeployReleaseScript, problems);

  const smokeScript = readText(root, defaults.smokeScript);
  includesAll(smokeScript, [
    "/api/search?type=dash-db",
    "/api/dashboards/uid/",
    "/api/ds/query",
    "representative live data queries",
    "feature-wise live data queries",
    "goatos-stg-grafana",
  ], defaults.smokeScript, problems);
  for (const filter of smokeScript.matchAll(/filter:\s*(`[^`]+`|'[^']+'|"[^"]+")/g)) {
    validateGrafanaFilterShape(filter[1], defaults.smokeScript, problems);
  }

  return problems;
}

function writeFixture(root, overrides = {}) {
  rmSync(path.join(root, defaults.dashboardDir), { recursive: true, force: true });
  mkdirSync(path.join(root, defaults.dashboardDir), { recursive: true });
  mkdirSync(path.join(root, path.dirname(defaults.dashboardsProvider)), { recursive: true });
  mkdirSync(path.join(root, path.dirname(defaults.datasourcesProvider)), { recursive: true });
  mkdirSync(path.join(root, path.dirname(defaults.terraform)), { recursive: true });
  mkdirSync(path.join(root, path.dirname(defaults.monitoring)), { recursive: true });
  mkdirSync(path.join(root, path.dirname(defaults.secrets)), { recursive: true });
  mkdirSync(path.join(root, path.dirname(defaults.deployScript)), { recursive: true });
  mkdirSync(path.join(root, path.dirname(defaults.cloudDeployReleaseScript)), { recursive: true });

  writeFileSync(path.join(root, defaults.dashboardDir, "01-api-red.json"), JSON.stringify({
    uid: "goatos-stg-api-red",
    title: "Goat OS - API / RED",
    panels: [{ id: 1, title: "Requests", type: "timeseries" }],
  }, null, 2));
  writeFileSync(path.join(root, defaults.dashboardsProvider), overrides.dashboardsYaml ?? `
apiVersion: 1
providers:
  - name: goatos-stg
    type: file
    options:
      path: /mnt/grafana-provisioning/dashboards
`);
  writeFileSync(path.join(root, defaults.datasourcesProvider), `
apiVersion: 1
datasources:
  - uid: gmp-prometheus
  - uid: cloud-monitoring
  - uid: postgres-analytics
`);
  writeFileSync(path.join(root, defaults.terraform), overrides.terraform ?? `
resource "google_cloud_run_v2_service" "grafana" {
  template {
    containers {
      env {
        name  = "GF_PATHS_PROVISIONING"
        value = "/mnt/grafana-provisioning/provisioning"
      }
      volume_mounts {
        name       = "grafana-provisioning"
        mount_path = "/mnt/grafana-provisioning"
      }
    }
  }
}
resource "google_storage_bucket" "grafana_provisioning" {}
resource "google_storage_bucket_object" "grafana_datasources" {
  name   = "provisioning/datasources/datasources.yaml"
  source = "\${path.module}/../../grafana/provisioning/datasources/datasources.yaml"
}
resource "google_storage_bucket_object" "grafana_dashboards_provider" {
  name   = "provisioning/dashboards/dashboards.yaml"
  source = "\${path.module}/../../grafana/provisioning/dashboards/dashboards.yaml"
}
resource "google_storage_bucket_object" "grafana_dashboard_jsons" {
  for_each = fileset("\${path.module}/../../grafana/dashboards", "*.json")
  name   = "dashboards/\${each.value}"
  source = "\${path.module}/../../grafana/dashboards/\${each.value}"
}
resource "google_cloud_run_v2_service_iam_member" "grafana_deploy_smoke_invoker" {
  member   = "serviceAccount:\${google_service_account.github_deployer.email}"
}
resource "google_storage_bucket_iam_member" "grafana_provisioning_grafana_reader" {
  member = "serviceAccount:\${google_service_account.grafana.email}"
}
`);
  writeFileSync(path.join(root, defaults.monitoring), overrides.monitoring ?? `
resource "google_monitoring_alert_policy" "api_error_rate_slo_burn" {
  conditions {
    condition_threshold {
      filter = "metric.type=\\"prometheus.googleapis.com/http_server_requests_total/counter\\" AND resource.type=\\"prometheus_target\\" AND metric.labels.status_class=\\"5xx\\""
    }
  }
}
`);
  writeFileSync(path.join(root, defaults.secrets), overrides.secrets ?? `
resource "google_secret_manager_secret_iam_member" "grafana_admin_password_deploy_smoke_accessor" {
  secret_id = google_secret_manager_secret.grafana_admin_password.id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:\${google_service_account.github_deployer.email}"
}
`);
  writeFileSync(path.join(root, defaults.deployScript), overrides.deployScript ?? `
tools/deploy/stg-clouddeploy-release.sh
`);
  writeFileSync(path.join(root, defaults.grafanaPipeline), overrides.grafanaPipeline ?? `
serviceAccount: projects/goatos-stg/serviceAccounts/goatos-github-deploy-stg@goatos-stg.iam.gserviceaccount.com
steps:
  - id: resolve
    args:
      - -ceu
      - |
        [[ "$img" =~ grafana-alloy@sha256:[a-f0-9]{64}$ ]]
        python3 tools/deploy/stg-observability.py deploy --assets infra/grafana --alloy-image "$img" --log-metrics infra/observability/faro-log-metrics.json
        [[ "$(curl -o /dev/null -w '%{http_code}' https://grafana.mesha.sg/api/search)" == "401" ]]
        node tools/deploy/smoke-stg-grafana-dashboards.mjs --no-proxy --query-validity-only --firebase-initial-export-receipt infra/observability/firebase-initial-export.json
`);
  writeFileSync(path.join(root, defaults.cloudDeployReleaseScript), overrides.cloudDeployReleaseScript ?? `
docker build --platform linux/amd64 \\
  --build-arg NEXT_PUBLIC_FIREBASE_PERFORMANCE_ENABLED=1 \\
  --build-arg NEXT_PUBLIC_FARO_COLLECTOR_URL=https://goatos-stg-grafana-alloy-awtrpmn4za-el.a.run.app/collect \\
  --build-arg NEXT_PUBLIC_GOATOS_ENV=stg \\
  --build-arg NEXT_PUBLIC_APP_VERSION="$commit_sha" \\
  -f apps/admin-web/Dockerfile -t "$admin_web_image" .
`);
  writeFileSync(path.join(root, defaults.smokeScript), `
const service = "goatos-stg-grafana";
fetch("/api/search?type=dash-db");
fetch("/api/dashboards/uid/" + service);
fetch("/api/ds/query", { method: "POST" });
console.log("representative live data queries");
console.log("feature-wise live data queries");
`);
}

function selfTest() {
  const root = mkdtempSync(path.join(tmpdir(), "goatos-grafana-durability-"));
  try {
    writeFixture(root);
    assert.deepEqual(validate(root), []);
    assert.equal(grafanaFilterString(["metric.type", "=", "run.googleapis.com/request_count"]), 'metric.type="run.googleapis.com/request_count"');
    assert.equal(grafanaFilterString(["metric.label.route", "=~", ".*weighing.*"]), 'metric.label.route=monitoring.regex.full_match(".*weighing.*")');
    assert.equal(grafanaFilterString(["metric.type", "run.googleapis.com/request_count"]), null);
    assert.equal(grafanaFilterString([]), null);
    assert.equal(grafanaFilterString(["metric.type", "=", "a", "OR", "resource.type", "=", "b"]), null);

    writeFixture(root);
    writeFileSync(path.join(root, defaults.dashboardDir, "02-missing-metric.json"), JSON.stringify({
      uid: "missing-metric", title: "Missing metric", panels: [{ targets: [{ timeSeriesList: {
        filters: ["resource.type", "=", "cloud_run_revision"],
      } }] }],
    }));
    assert(validate(root).some((problem) => problem.includes("timeSeriesList.filters must use")));
    writeFileSync(path.join(root, defaults.dashboardDir, "02-missing-metric.json"), JSON.stringify({
      uid: "bad-histogram", title: "Bad histogram", panels: [{ targets: [{ timeSeriesList: {
        filters: ["metric.type", "=", "prometheus.googleapis.com/http_server_request_duration_seconds/histogram", "AND", "resource.type", "=", "prometheus_target"],
        perSeriesAligner: "ALIGN_PERCENTILE_95", crossSeriesReducer: "REDUCE_NONE",
      } }] }],
    }));
    assert(validate(root).some((problem) => problem.includes("cumulative GMP histograms require")));

    writeFixture(root);
    const promFixture = path.join(root, defaults.dashboardDir, "02-promql.json");
    const prom = { queryType: "promQL", promQLQuery: {expr:"sum(rate(requests_total[5m]))",projectName:"goatos-stg",step:"300s"}, timeSeriesList:{} };
    writeFileSync(promFixture, JSON.stringify({uid:"promql",title:"PromQL",panels:[{targets:[prom]}]}));
    assert(!validate(root).some(p => p.includes("PromQL requires") || p.includes("timeSeriesList.filters must use")));
    delete prom.timeSeriesList;
    writeFileSync(promFixture, JSON.stringify({uid:"promql",title:"PromQL",panels:[{targets:[prom]}]}));
    assert(validate(root).some(p => p.includes("PromQL requires")));

    writeFixture(root, { terraform: "resource \"google_storage_bucket\" \"grafana_provisioning\" {}" });
    assert(validate(root).some((problem) => problem.includes("grafana_dashboard_jsons")));

    writeFixture(root, { dashboardsYaml: "apiVersion: 1\nproviders: []\n" });
    assert(validate(root).some((problem) => problem.includes("/mnt/grafana-provisioning/dashboards")));

    writeFixture(root, { secrets: "resource \"google_secret_manager_secret_iam_member\" \"grafana_admin_password_accessor\" {}" });
    assert(validate(root).some((problem) => problem.includes("grafana_admin_password_deploy_smoke_accessor")));

    writeFixture(root);
    writeFileSync(path.join(root, defaults.dashboardDir, "02-empty.json"), JSON.stringify({
      uid: "goatos-stg-api-red",
      title: "Duplicate UID",
      panels: [],
    }));
    const duplicateProblems = validate(root);
    assert(duplicateProblems.some((problem) => problem.includes("duplicate dashboard uid")));
    assert(duplicateProblems.some((problem) => problem.includes("at least one panel")));

    writeFixture(root, { grafanaPipeline: `
serviceAccount: projects/goatos-stg/serviceAccounts/goatos-github-deploy-stg@goatos-stg.iam.gserviceaccount.com
steps:
  - args:
      - -ceu
      - |
        [[ "$img" =~ grafana-alloy@sha256:[a-f0-9]{64}$ ]]
        python3 tools/deploy/stg-observability.py deploy --assets infra/grafana --alloy-image "$img" --log-metrics infra/observability/faro-log-metrics.json
        [[ "$(curl https://grafana.mesha.sg/api/search)" == "401" ]]
        node tools/deploy/smoke-stg-grafana-dashboards.mjs --no-proxy --query-validity-only --firebase-initial-export-receipt infra/observability/firebase-initial-export.json || true
` });
    assert(validate(root).some((problem) => problem.includes("must fail closed")));

    writeFixture(root, { grafanaPipeline: "steps: []\n" });
    const missingPipeline = validate(root);
    assert(missingPipeline.some((problem) => problem.includes("smoke-stg-grafana-dashboards.mjs")));
    assert(missingPipeline.some((problem) => problem.includes("stg-observability.py deploy")));

    writeFixture(root, { deployScript: "node tools/deploy/smoke-stg-grafana-dashboards.mjs --no-proxy\n" });
    assert(validate(root).some((problem) => problem.includes("not the Goat OS STG release")));

    writeFixture(root, {
      cloudDeployReleaseScript: "docker build --platform linux/amd64 --build-arg NEXT_PUBLIC_FIREBASE_PERFORMANCE_ENABLED=1 -f apps/admin-web/Dockerfile -t $admin_web_image .",
    });
    assert(validate(root).some((problem) => problem.includes("NEXT_PUBLIC_FARO_COLLECTOR_URL")));

    writeFixture(root, { monitoring: 'filter = "metric.type=\\"prometheus.googleapis.com/http_server_requests_total/counter\\" AND resource.type=\\"generic_task\\" AND metric.label.status_class=\\"5xx\\""\n' });
    const monitoringProblems = validate(root);
    assert(monitoringProblems.some((problem) => problem.includes("prometheus_target")));
    assert(monitoringProblems.some((problem) => problem.includes("metric.labels.*")));

    writeFixture(root);
    writeFileSync(path.join(root, defaults.dashboardDir, "01-api-red.json"), JSON.stringify({
      uid: "goatos-stg-api-red",
      title: "Goat OS - API / RED",
      panels: [{
        id: 1,
        title: "Requests",
        type: "timeseries",
        targets: [{
          timeSeriesList: {
            filter: 'metric.type = "run.googleapis.com/request_count" AND resource.label.service_name = "goatos-api-stg"',
          },
        }],
      }],
    }));
    assert(validate(root).some((problem) => problem.includes('resource.type = "cloud_run_revision"')));
    assert(validate(root).some((problem) => problem.includes('singular filter is ignored')));

    writeFixture(root);
    writeFileSync(path.join(root, defaults.dashboardDir, "01-api-red.json"), JSON.stringify({
      uid: "goatos-stg-api-red",
      title: "Goat OS - API / RED",
      panels: [{
        id: 1,
        title: "OTel metric",
        type: "timeseries",
        targets: [{
          timeSeriesList: {
            filter: 'metric.type = "prometheus.googleapis.com/http_client_request_duration_seconds/histogram" AND resource.label.service_name = "goatos-android"',
          },
        }],
      }],
    }));
    assert(validate(root).some((problem) => problem.includes('resource.type = "prometheus_target"')));

    writeFixture(root);
    writeFileSync(path.join(root, defaults.dashboardDir, "01-api-red.json"), JSON.stringify({
      uid: "goatos-stg-api-red",
      title: "Goat OS - Kernel",
      panels: [{
        id: 1,
        title: "Pub/Sub",
        type: "timeseries",
        targets: [{
          timeSeriesList: {
            filter: 'metric.type = "pubsub.googleapis.com/subscription/num_undelivered_messages" AND resource.label.subscription_id = "goatos-stg-domain-events"',
          },
        }],
      }],
    }));
    assert(validate(root).some((problem) => problem.includes('resource.type = "pubsub_subscription"')));

    writeFixture(root);
    writeFileSync(path.join(root, defaults.smokeScript), `
fetch("/api/search?type=dash-db");
fetch("/api/dashboards/uid/goatos-stg-grafana");
fetch("/api/ds/query");
console.log("representative live data queries");
const q = { filter: 'metric.type = "cloudsql.googleapis.com/database/postgresql/num_backends" AND resource.label.database_id = "goatos-stg:goatos-stg-core-db"' };
`);
    assert(validate(root).some((problem) => problem.includes('resource.type = "cloudsql_database"')));

    writeFixture(root);
    writeFileSync(path.join(root, defaults.dashboardDir, "01-api-red.json"), JSON.stringify({
      uid: "goatos-stg-api-red",
      title: "Goat OS stg - API / RED",
      panels: [{ id: 1, title: "Requests", type: "timeseries" }],
    }));
    assert(validate(root).some((problem) => problem.includes("Goat OS, not Goat OS stg")));

    writeFixture(root);
    writeFileSync(path.join(root, defaults.dashboardDir, "00-feature-health.json"), JSON.stringify({
      uid: "goatos-feature-health",
      title: "Goat OS — Feature health",
      panels: [
        {
          id: 116,
          title: "APM status",
          type: "text",
          options: { content: "Server spans are present. Pending Faro/RUM, Cloud Trace datasource, and traceparent outbox propagation." },
        },
        ...featurePanelRequirements.flatMap(({ feature, routeRegex }, index) => [
          {
            id: 1000 + index,
            title: `${feature}: request rate by route`,
            type: "timeseries",
            targets: [{ timeSeriesList: { filters: ["metric.type", "=", "prometheus.googleapis.com/http_server_requests_total/counter", "AND", "resource.type", "=", "prometheus_target", "AND", "metric.label.route", "=~", routeRegex] } }],
          },
          {
            id: 1100 + index,
            title: `${feature}: p95 latency by route`,
            type: "timeseries",
            targets: [{ timeSeriesList: { filters: ["metric.type", "=", "prometheus.googleapis.com/http_server_request_duration_seconds/histogram", "AND", "resource.type", "=", "prometheus_target", "AND", "metric.label.route", "=~", routeRegex] } }],
          },
        ]),
      ],
    }));
    assert.deepEqual(validate(root), []);

    writeFixture(root);
    writeFileSync(path.join(root, defaults.dashboardDir, "00-feature-health.json"), JSON.stringify({
      uid: "goatos-feature-health",
      title: "Goat OS — Feature health",
      panels: [{ id: 116, title: "APM status", type: "text", options: { content: "APM complete." } }],
    }));
    const featureProblems = validate(root);
    assert(featureProblems.some((problem) => problem.includes("missing feature-wise request panel")));
    assert(featureProblems.some((problem) => problem.includes("APM status must explicitly document pending Faro/RUM")));
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
}

if (process.argv.includes("--self-test")) {
  selfTest();
  execFileSync("python3", [path.join(repo, "tools/deploy/stg-runner-receipt_test.py")], { stdio: "inherit" });
  console.log("check-grafana-durability: self-test passed");
} else {
  execFileSync("python3", [path.join(repo, "tools/deploy/stg-runner-receipt.py")], { stdio: "inherit" });
  const problems = validate();
  if (problems.length) {
    console.error("check-grafana-durability: FAILED");
    for (const problem of problems) console.error(`- ${problem}`);
    process.exit(1);
  }
  console.log("check-grafana-durability: OK");
}
