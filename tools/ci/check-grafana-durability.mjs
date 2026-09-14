#!/usr/bin/env node
// Static guard for the staging Grafana provisioning lane.
//
// If infra/grafana/dashboards/*.json exists, CI must prove the files are valid,
// Terraform still uploads them into the mounted GCS provisioning bucket, and the
// staging deploy path still runs the live Grafana dashboard smoke.

import assert from "node:assert/strict";
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
  secrets: "infra/envs/stg/secrets.tf",
  deployScript: "tools/deploy/stg-cloudbuild-release.sh",
  smokeScript: "tools/deploy/smoke-stg-grafana-dashboards.mjs",
};

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
    }
    if (!Array.isArray(doc.panels) || doc.panels.length === 0) {
      problems.push(`${relative}: dashboard must contain at least one panel`);
    }
    const serialized = JSON.stringify(doc);
    if (serialized.includes('"filters":')) {
      problems.push(`${relative}: use timeSeriesList.filter strings, not legacy timeSeriesList.filters arrays`);
    }
    if (serialized.includes("resource.labels.")) {
      problems.push(`${relative}: Cloud Monitoring filters must use resource.label.*, not resource.labels.*`);
    }
    if (serialized.includes('resource.type="generic_task"') || serialized.includes("fetch generic_task ::")) {
      problems.push(`${relative}: GMP metrics in goatos-stg are prometheus_target, not generic_task`);
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

  const secrets = readText(root, defaults.secrets);
  includesAll(secrets, [
    'resource "google_secret_manager_secret_iam_member" "grafana_admin_password_deploy_smoke_accessor"',
    "secret_id = google_secret_manager_secret.grafana_admin_password.id",
    'member    = "serviceAccount:${google_service_account.github_deployer.email}"',
  ], defaults.secrets, problems);

  const deployScript = readText(root, defaults.deployScript);
  includesAll(deployScript, [
    "smoke-stg-grafana-dashboards.mjs",
    "smoke_grafana_dashboards",
    "refusing to report backend/web deploy success",
  ], defaults.deployScript, problems);
  if (/REQUIRE_GRAFANA_SMOKE/.test(deployScript)) {
    problems.push(`${defaults.deployScript}: Grafana smoke must fail closed; remove REQUIRE_GRAFANA_SMOKE warning-only bypass`);
  }

  const smokeScript = readText(root, defaults.smokeScript);
  includesAll(smokeScript, [
    "/api/search?type=dash-db",
    "/api/dashboards/uid/",
    "goatos-stg-grafana",
  ], defaults.smokeScript, problems);

  return problems;
}

function writeFixture(root, overrides = {}) {
  mkdirSync(path.join(root, defaults.dashboardDir), { recursive: true });
  mkdirSync(path.join(root, path.dirname(defaults.dashboardsProvider)), { recursive: true });
  mkdirSync(path.join(root, path.dirname(defaults.datasourcesProvider)), { recursive: true });
  mkdirSync(path.join(root, path.dirname(defaults.terraform)), { recursive: true });
  mkdirSync(path.join(root, path.dirname(defaults.secrets)), { recursive: true });
  mkdirSync(path.join(root, path.dirname(defaults.deployScript)), { recursive: true });

  writeFileSync(path.join(root, defaults.dashboardDir, "01-api-red.json"), JSON.stringify({
    uid: "goatos-stg-api-red",
    title: "Goat OS stg - API / RED",
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
  writeFileSync(path.join(root, defaults.secrets), overrides.secrets ?? `
resource "google_secret_manager_secret_iam_member" "grafana_admin_password_deploy_smoke_accessor" {
  secret_id = google_secret_manager_secret.grafana_admin_password.id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:\${google_service_account.github_deployer.email}"
}
`);
  writeFileSync(path.join(root, defaults.deployScript), overrides.deployScript ?? `
smoke_grafana_dashboards() {
  node tools/deploy/smoke-stg-grafana-dashboards.mjs || {
    echo "refusing to report backend/web deploy success"
    return 1
  }
}
smoke_grafana_dashboards
`);
  writeFileSync(path.join(root, defaults.smokeScript), `
const service = "goatos-stg-grafana";
fetch("/api/search?type=dash-db");
fetch("/api/dashboards/uid/" + service);
`);
}

function selfTest() {
  const root = mkdtempSync(path.join(tmpdir(), "goatos-grafana-durability-"));
  try {
    writeFixture(root);
    assert.deepEqual(validate(root), []);

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

    writeFixture(root, { deployScript: `
smoke_grafana_dashboards() {
  node tools/deploy/smoke-stg-grafana-dashboards.mjs || true
}
if [[ "\${REQUIRE_GRAFANA_SMOKE:-0}" == "1" ]]; then return 1; fi
` });
    assert(validate(root).some((problem) => problem.includes("REQUIRE_GRAFANA_SMOKE")));
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
}

if (process.argv.includes("--self-test")) {
  selfTest();
  console.log("check-grafana-durability: self-test passed");
} else {
  const problems = validate();
  if (problems.length) {
    console.error("check-grafana-durability: FAILED");
    for (const problem of problems) console.error(`- ${problem}`);
    process.exit(1);
  }
  console.log("check-grafana-durability: OK");
}
