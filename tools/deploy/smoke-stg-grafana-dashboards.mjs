#!/usr/bin/env node
// Post-deploy smoke for staging Grafana dashboard durability.
//
// Compares committed infra/grafana/dashboards/*.json UIDs against the live
// Grafana API. It starts a local `gcloud run services proxy` by default for
// developer laptops. In Cloud Build, use --direct-iam so Grafana keeps its Basic
// auth header while Cloud Run IAM gets X-Serverless-Authorization.

import assert from "node:assert/strict";
import { execFileSync, spawn } from "node:child_process";
import { mkdtempSync, readdirSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import net from "node:net";
import path from "node:path";
import { fileURLToPath } from "node:url";

const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");

function arg(name, fallback) {
  const index = process.argv.indexOf(name);
  return index >= 0 ? process.argv[index + 1] : fallback;
}

function hasFlag(name) {
  return process.argv.includes(name);
}

function shellOut(command, args, options = {}) {
  return execFileSync(command, args, { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"], ...options }).trim();
}

async function freePort() {
  return new Promise((resolve, reject) => {
    const server = net.createServer();
    server.on("error", reject);
    server.listen(0, "127.0.0.1", () => {
      const { port } = server.address();
      server.close(() => resolve(port));
    });
  });
}

function committedDashboards(dir) {
  return readdirSync(dir)
    .filter((name) => name.endsWith(".json"))
    .sort()
    .map((name) => {
      const fullPath = path.join(dir, name);
      const doc = JSON.parse(readFileSync(fullPath, "utf8"));
      return { file: name, uid: doc.uid, title: doc.title, panels: Array.isArray(doc.panels) ? doc.panels.length : 0, doc };
    });
}

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

const requiredDatasourceUids = [
  "cloud-monitoring",
  "gmp-prometheus",
  "cloud-trace",
  "cloud-logging",
  "postgres-analytics",
  "bigquery-analytics",
];

function authHeader(password) {
  return `Basic ${Buffer.from(`admin:${password}`, "utf8").toString("base64")}`;
}

function serviceUrl({ project, region, service }) {
  return shellOut("gcloud", [
    "run",
    "services",
    "describe",
    service,
    `--project=${project}`,
    `--region=${region}`,
    "--format=value(status.url)",
  ]);
}

function identityToken(audience, serviceAccount = "") {
  const activeAccount = shellOut("gcloud", ["config", "get-value", "account"]);
  const baseArgs = [
    "auth",
    "print-identity-token",
    `--audiences=${audience}`,
  ];
  if (!serviceAccount) return shellOut("gcloud", baseArgs);
  const impersonatedArgs = [...baseArgs, `--impersonate-service-account=${serviceAccount}`];
  if (serviceAccount !== activeAccount) return shellOut("gcloud", impersonatedArgs);
  try {
    return shellOut("gcloud", baseArgs);
  } catch {
    return shellOut("gcloud", impersonatedArgs);
  }
}

async function fetchJson(baseUrl, apiPath, password, timeoutMs, iamToken = "") {
  return fetchGrafanaJson(baseUrl, apiPath, password, timeoutMs, iamToken);
}

async function postJson(baseUrl, apiPath, body, password, timeoutMs, iamToken = "") {
  return fetchGrafanaJson(baseUrl, apiPath, password, timeoutMs, iamToken, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(body),
  });
}

async function fetchGrafanaJson(baseUrl, apiPath, password, timeoutMs, iamToken = "", options = {}) {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  try {
    const headers = { Authorization: authHeader(password), ...(options.headers ?? {}) };
    if (iamToken) headers["X-Serverless-Authorization"] = `Bearer ${iamToken}`;
    const response = await fetch(new URL(apiPath, baseUrl), {
      headers,
      signal: controller.signal,
      method: options.method ?? "GET",
      body: options.body,
    });
    const body = await response.text();
    if (!response.ok) {
      throw new Error(`${apiPath} returned HTTP ${response.status}: ${body.slice(0, 200)}`);
    }
    return JSON.parse(body);
  } finally {
    clearTimeout(timer);
  }
}

function requiredLiveDataQueries(project) {
  const cloudRunRequestRate = (name, serviceName) => ({
    name,
    query: {
      refId: "A",
      datasource: { uid: "cloud-monitoring", type: "stackdriver" },
      queryType: "timeSeriesList",
      intervalMs: 300000,
      maxDataPoints: 200,
      timeSeriesList: {
        projectName: project,
        view: "FULL",
        groupBys: [],
        perSeriesAligner: "ALIGN_RATE",
        crossSeriesReducer: "REDUCE_SUM",
        alignmentPeriod: "300s",
        filters: ["resource.type", "=", "cloud_run_revision", "AND", "metric.type", "=", "run.googleapis.com/request_count", "AND", "resource.label.service_name", "=", serviceName],
      },
    },
  });
  return [
    cloudRunRequestRate("grafana_cloud_run_request_count", "goatos-stg-grafana"),
    cloudRunRequestRate("admin_web_cloud_run_request_count", "goatos-admin-web-stg"),
    {
      name: "cloud_sql_active_connections",
      query: {
        refId: "A",
        datasource: { uid: "cloud-monitoring", type: "stackdriver" },
        queryType: "timeSeriesList",
        intervalMs: 300000,
        maxDataPoints: 200,
        timeSeriesList: {
          projectName: project,
          view: "FULL",
          groupBys: [],
          perSeriesAligner: "ALIGN_MEAN",
          crossSeriesReducer: "REDUCE_NONE",
          alignmentPeriod: "60s",
          filters: ["resource.type", "=", "cloudsql_database", "AND", "metric.type", "=", "cloudsql.googleapis.com/database/postgresql/num_backends", "AND", "resource.label.database_id", "=", "goatos-stg:goatos-stg-core-db"],
        },
      },
    },
  ];
}

function grafanaRange(hours = 6) {
  const now = Date.now();
  return {
    now,
    fromMs: now - hours * 60 * 60 * 1000,
    range: {
      from: new Date(now - hours * 60 * 60 * 1000).toISOString(),
      to: new Date(now).toISOString(),
      raw: { from: `now-${hours}h`, to: "now" },
    },
  };
}

async function runDatasourceQuery(baseUrl, password, timeoutMs, iamToken, query, hours = 6) {
  const { now, fromMs, range } = grafanaRange(hours);
  return postJson(
    baseUrl,
    "/api/ds/query",
    {
      from: String(fromMs),
      to: String(now),
      range,
      queries: [query],
    },
    password,
    timeoutMs,
    iamToken,
  );
}

async function assertLiveDataQueries(baseUrl, password, timeoutMs, iamToken, project) {
  const missing = [];
  for (const { name, query } of requiredLiveDataQueries(project)) {
    const response = await runDatasourceQuery(baseUrl, password, timeoutMs, iamToken, query)
      .catch((error) => { throw new Error(`${name}: ${error.message}`, { cause: error }); });
    if (!grafanaQueryHasData(response)) {
      missing.push(name);
    }
  }
  if (missing.length) {
    throw new Error(`Live Grafana datasource queries returned no data: ${missing.join(", ")}`);
  }
}

function featureInventoryQuery(project, routeRegex) {
  return {
    refId: "A",
    datasource: { uid: "cloud-monitoring", type: "stackdriver" },
    queryType: "timeSeriesList",
    intervalMs: 300000,
    maxDataPoints: 200,
    timeSeriesList: {
      projectName: project,
      view: "FULL",
      groupBys: ["metric.label.route", "metric.label.status_class"],
      perSeriesAligner: "ALIGN_RATE",
      crossSeriesReducer: "REDUCE_SUM",
      alignmentPeriod: "300s",
      filters: ["metric.type", "=", "prometheus.googleapis.com/http_server_requests_total/counter", "AND", "resource.type", "=", "prometheus_target", "AND", "metric.label.route", "=~", routeRegex],
    },
  };
}

function panelByTitle(dashboard, title) {
  return (Array.isArray(dashboard?.doc?.panels) ? dashboard.doc.panels : []).find((panel) => panel?.title === title);
}

function panelQueries(panel, project) {
  return (Array.isArray(panel?.targets) ? panel.targets : [])
    .filter((target) => target?.queryType === "timeSeriesList" && Array.isArray(target?.timeSeriesList?.filters))
    .map((target, index) => ({
      refId: target.refId ?? String.fromCharCode("A".charCodeAt(0) + index),
      datasource: { uid: "cloud-monitoring", type: "stackdriver" },
      queryType: "timeSeriesList",
      intervalMs: target.intervalMs ?? 300000,
      maxDataPoints: target.maxDataPoints ?? 200,
      timeSeriesList: {
        ...target.timeSeriesList,
        projectName: target.timeSeriesList.projectName ?? project,
      },
    }));
}

async function assertFeatureWisePanels(baseUrl, password, timeoutMs, iamToken, project, dashboards) {
  const featureDashboard = dashboards.find((dashboard) => dashboard.uid === "goatos-feature-health");
  if (!featureDashboard) {
    throw new Error("Committed dashboards are missing goatos-feature-health");
  }
  const missingTelemetry = [];
  const panelProblems = [];
  for (const { feature, routeRegex } of featurePanelRequirements) {
    const inventory = await runDatasourceQuery(
      baseUrl,
      password,
      timeoutMs,
      iamToken,
      featureInventoryQuery(project, routeRegex),
      24,
    );
    if (!grafanaQueryHasData(inventory)) {
      missingTelemetry.push(feature);
      continue;
    }

    for (const panelTitle of [`${feature}: request rate by route`, `${feature}: p95 latency by route`]) {
      const panel = panelByTitle(featureDashboard, panelTitle);
      if (!panel) {
        panelProblems.push(`${feature}: missing committed panel ${panelTitle}`);
        continue;
      }
      const queries = panelQueries(panel, project);
      if (queries.length === 0) {
        panelProblems.push(`${feature}: ${panelTitle} has no Cloud Monitoring timeSeriesList query`);
        continue;
      }
      const { now, fromMs, range } = grafanaRange(24);
      const response = await postJson(
        baseUrl,
        "/api/ds/query",
        {
          from: String(fromMs),
          to: String(now),
          range,
          queries,
        },
        password,
        timeoutMs,
        iamToken,
      ).catch((error) => { throw new Error(`${panelTitle}: ${error.message}`, { cause: error }); });
      if (!grafanaQueryHasData(response)) {
        panelProblems.push(`${feature}: current route telemetry exists, but ${panelTitle} returned no data`);
      }
    }
  }
  if (panelProblems.length) {
    throw new Error(`Feature-wise Grafana panels failed live data checks: ${panelProblems.join("; ")}`);
  }
  if (missingTelemetry.length) {
    console.log(`grafana-smoke: feature-wise panels skipped for no-current-telemetry slices: ${missingTelemetry.join(", ")}`);
  }
}

function hasNonEmptyValue(series) {
  return Array.isArray(series) && series.some((value) => value !== null && value !== undefined && !Number.isNaN(value));
}

function isTimeField(field) {
  return field?.type === "time" || /(^|_)time$/i.test(field?.name ?? "");
}

function frameHasData(frame) {
  const values = frame?.data?.values;
  if (!Array.isArray(values)) return false;
  const fields = Array.isArray(frame?.schema?.fields) ? frame.schema.fields : [];
  if (fields.length === values.length) {
    return values.some((series, index) => !isTimeField(fields[index]) && hasNonEmptyValue(series));
  }
  return values.slice(1).some((series) => hasNonEmptyValue(series));
}

function grafanaQueryHasData(response) {
  const results = response?.results;
  if (!results || typeof results !== "object") return false;
  return Object.values(results).some((result) => {
    if (result?.error) return false;
    const frames = Array.isArray(result?.frames) ? result.frames : [];
    return frames.some((frame) => frameHasData(frame));
  });
}

async function selfTest() {
  const live = requiredLiveDataQueries("test-project");
  assert.equal(live.length, 3);
  for (const [index, { query }] of live.entries()) {
    const filters = query.timeSeriesList.filters;
    assert.equal(query.timeSeriesList.filter, undefined);
    assert.equal(query.timeSeriesList.projectName, "test-project");
    assert.deepEqual(filters.slice(0, 7), ["resource.type", "=", index < 2 ? "cloud_run_revision" : "cloudsql_database", "AND", "metric.type", "=", index < 2 ? "run.googleapis.com/request_count" : "cloudsql.googleapis.com/database/postgresql/num_backends"]);
    assert.equal(filters.length, 11);
  }
  const inventory = featureInventoryQuery("test-project", ".*weighing.*").timeSeriesList;
  assert.deepEqual(inventory.filters, ["metric.type", "=", "prometheus.googleapis.com/http_server_requests_total/counter", "AND", "resource.type", "=", "prometheus_target", "AND", "metric.label.route", "=~", ".*weighing.*"]);
  assert.deepEqual(inventory.groupBys, ["metric.label.route", "metric.label.status_class"]);
  const empty = { results: { A: { frames: [{ schema: { fields: [{ name: "Time", type: "time" }, { name: "value", type: "number" }] }, data: { values: [[1, 2], [null, null]] } }] } } };
  const present = { results: { A: { frames: [{ schema: { fields: [{ name: "Time", type: "time" }, { name: "value", type: "number" }] }, data: { values: [[1, 2], [null, 3]] } }] } } };
  if (grafanaQueryHasData(empty)) throw new Error("self-test expected null-valued frames to count as empty");
  if (!grafanaQueryHasData(present)) throw new Error("self-test expected numeric value frames to count as data");
  const dashboards = Array.from({ length: 7 }, (_, i) => ({ uid: `dashboard-${i}`, file: `${i}.json` }));
  const datasourceRows = requiredDatasourceUids.map((uid) => ({ uid }));
  const fixture = (respond) => {
    let clock = 0;
    const requests = [];
    return {
      requests,
      now: () => clock,
      sleep: async (ms) => { clock += ms; },
      request: async (_url, apiPath, _password, budget) => {
        requests.push({ apiPath, budget, clock });
        assert.ok(budget > 0 && budget <= 2500 - clock, "request must fit remaining readiness budget");
        return respond(apiPath, clock);
      },
    };
  };
  // Health succeeds immediately, while datasource and dashboard provisioning finish later.
  const startup = fixture((apiPath, clock) => {
    if (apiPath === "/api/health") return { database: "ok" };
    if (apiPath === "/api/datasources") return clock === 0 ? [] : datasourceRows;
    return clock < 2000 ? dashboards.slice(0, 5) : dashboards;
  });
  await waitForGrafana("http://fixture", "unused", 2500, "", dashboards, startup);
  assert.equal(startup.now(), 2000);
  assert.equal(startup.requests.filter((r) => r.apiPath === "/api/health").length, 3);
  // Unrelated UIDs cannot satisfy the gate even when the dashboard count is seven.
  const missing = fixture((apiPath) => apiPath === "/api/datasources" ? datasourceRows :
    apiPath === "/api/health" ? {} : [...dashboards.slice(0, 6), { uid: "unrelated" }]);
  await assert.rejects(waitForGrafana("http://fixture", "unused", 2500, "", dashboards, missing), /dashboard-6/);
  assert.equal(missing.now(), 2500, "readiness must not extend its original deadline");
  const absentDatasource = fixture((apiPath) => apiPath === "/api/datasources" ? datasourceRows.slice(1) : dashboards);
  await assert.rejects(waitForGrafana("http://fixture", "unused", 2500, "", dashboards, absentDatasource), /cloud-monitoring/);
  const unauthorized = fixture(() => { throw new Error("HTTP 401"); });
  await assert.rejects(waitForGrafana("http://fixture", "unused", 2500, "", dashboards, unauthorized), /HTTP 401/);
  assert.equal(unauthorized.now(), 2500);
  await assert.rejects(waitForGrafana("http://fixture", "unused", NaN), /timeout must be positive/);
  console.log("grafana-smoke: self-test passed");
}

async function waitForGrafana(baseUrl, password, timeoutMs, iamToken = "", dashboards = [], {
  request = fetchJson,
  now = Date.now,
  sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms)),
} = {}) {
  if (!Number.isFinite(timeoutMs) || timeoutMs <= 0) throw new Error("Grafana readiness timeout must be positive");
  const deadline = now() + timeoutMs;
  let lastError = null;
  const read = async (apiPath) => {
    const remaining = deadline - now();
    if (remaining <= 0) throw new Error("Grafana provisioning deadline exhausted");
    return request(baseUrl, apiPath, password, Math.min(5000, remaining), iamToken);
  };
  while (now() < deadline) {
    try {
      await read("/api/health");
      const datasources = await read("/api/datasources");
      const datasourceUids = new Set((Array.isArray(datasources) ? datasources : []).map((item) => item.uid));
      const missingDatasources = requiredDatasourceUids.filter((uid) => !datasourceUids.has(uid));
      if (missingDatasources.length) {
        throw new Error(`Live Grafana is missing provisioned datasources: ${missingDatasources.join(", ")}`);
      }
      const search = await read("/api/search?type=dash-db");
      const liveUids = new Set((Array.isArray(search) ? search : []).map((item) => item.uid));
      const missing = dashboards.filter((dashboard) => !liveUids.has(dashboard.uid));
      if (missing.length) {
        throw new Error(`Live Grafana is missing committed dashboards: ${missing.map((dashboard) => `${dashboard.uid} (${dashboard.file})`).join(", ")}`);
      }
      return;
    } catch (error) {
      lastError = error;
      const remaining = deadline - now();
      if (remaining > 0) await sleep(Math.min(1000, remaining));
    }
  }
  throw new Error(`Grafana API/provisioning did not become ready at ${baseUrl}: ${lastError?.message ?? "timeout"}`);
}

function startProxy({ project, region, service, port }) {
  const logDir = mkdtempSync(path.join(tmpdir(), "goatos-grafana-proxy-"));
  const child = spawn("gcloud", [
    "run",
    "services",
    "proxy",
    service,
    `--project=${project}`,
    `--region=${region}`,
    `--port=${port}`,
  ], {
    stdio: ["ignore", "pipe", "pipe"],
    env: process.env,
  });
  let stdout = "";
  let stderr = "";
  child.stdout.on("data", (chunk) => { stdout += chunk.toString(); });
  child.stderr.on("data", (chunk) => { stderr += chunk.toString(); });
  return {
    url: `http://127.0.0.1:${port}`,
    stop() {
      child.kill("SIGTERM");
      rmSync(logDir, { recursive: true, force: true });
    },
    diagnostics() {
      return `${stdout}\n${stderr}`.trim();
    },
  };
}

async function main() {
  if (hasFlag("--self-test")) {
    await selfTest();
    return;
  }
  const dashboardDir = path.resolve(repo, arg("--dashboard-dir", "infra/grafana/dashboards"));
  const dashboards = committedDashboards(dashboardDir);
  if (dashboards.length === 0) {
    console.log("grafana-smoke: no committed dashboard JSONs; nothing to verify");
    return;
  }

  const project = arg("--project", process.env.PROJECT_ID || "goatos-stg");
  const region = arg("--region", process.env.REGION || "asia-south1");
  const service = arg("--service", process.env.GRAFANA_CLOUD_RUN_SERVICE || "goatos-stg-grafana");
  const timeoutMs = Number(arg("--timeout-ms", process.env.GRAFANA_SMOKE_TIMEOUT_MS || "90000"));
  const explicitUrl = arg("--url", process.env.GRAFANA_STG_URL || "");
  const directIam = hasFlag("--direct-iam") || process.env.GRAFANA_DIRECT_IAM === "1";
  const iamServiceAccount = arg("--iam-service-account", process.env.GRAFANA_IAM_SERVICE_ACCOUNT || "");
  const password = process.env.GRAFANA_ADMIN_PASSWORD || shellOut("gcloud", [
    "secrets",
    "versions",
    "access",
    "latest",
    `--secret=${arg("--admin-password-secret", process.env.GRAFANA_ADMIN_PASSWORD_SECRET || "goatos-stg-grafana-admin-password")}`,
    `--project=${project}`,
  ]);
  if (!password) throw new Error("Grafana admin password is unavailable");

  let proxy = null;
  let baseUrl = explicitUrl;
  let iamToken = "";
  if (!baseUrl) {
    if (directIam) {
      baseUrl = serviceUrl({ project, region, service });
      iamToken = identityToken(baseUrl, iamServiceAccount);
    } else if (hasFlag("--no-proxy")) {
      throw new Error("GRAFANA_STG_URL or --url is required with --no-proxy");
    } else {
      const port = Number(arg("--proxy-port", process.env.GRAFANA_PROXY_PORT || await freePort()));
      proxy = startProxy({ project, region, service, port });
      baseUrl = proxy.url;
    }
  } else if (directIam) {
    iamToken = identityToken(baseUrl, iamServiceAccount);
  }

  try {
    // Health may become ready before file provisioning finishes on a cold start.
    // Share the existing readiness deadline across health, datasources and UIDs.
    await waitForGrafana(baseUrl, password, timeoutMs, iamToken, dashboards);
    for (const dashboard of dashboards) {
      const live = await fetchJson(baseUrl, `/api/dashboards/uid/${encodeURIComponent(dashboard.uid)}`, password, 15000, iamToken);
      const livePanels = live?.dashboard?.panels;
      if (!Array.isArray(livePanels) || livePanels.length === 0) {
        throw new Error(`Live Grafana dashboard ${dashboard.uid} has no panels`);
      }
    }
    await assertLiveDataQueries(baseUrl, password, 30000, iamToken, project);
    await assertFeatureWisePanels(baseUrl, password, 30000, iamToken, project, dashboards);
    console.log(`grafana-smoke: OK (${dashboards.length} committed dashboards, ${requiredDatasourceUids.length} datasources, representative live data queries, and feature-wise live data queries checked in live Grafana)`);
  } catch (error) {
    const proxyDiag = proxy?.diagnostics();
    if (proxyDiag) console.error(proxyDiag);
    throw error;
  } finally {
    proxy?.stop();
  }
}

main().catch((error) => {
  console.error(`grafana-smoke: FAILED: ${error.message}`);
  process.exit(1);
});
