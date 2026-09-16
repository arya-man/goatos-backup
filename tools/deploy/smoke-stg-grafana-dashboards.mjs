#!/usr/bin/env node
// Post-deploy smoke for staging Grafana dashboard durability.
//
// Compares committed infra/grafana/dashboards/*.json UIDs against the live
// Grafana API. It starts a local `gcloud run services proxy` by default for
// developer laptops. In Cloud Build, use --direct-iam so Grafana keeps its Basic
// auth header while Cloud Run IAM gets X-Serverless-Authorization.

import assert from "node:assert/strict";
import { createHash } from "node:crypto";
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

export function panelQueries(panel, project, { fromMs = Date.now() - 86400000, now = Date.now(), variables = {} } = {}) {
  return (panel?.targets ?? []).filter((target) => !target.hide).map((target, index) => {
    const replacements = { "${window_seconds}": String(Math.max(1, Math.floor((now - fromMs) / 1000))), "${__from}": String(fromMs), "${__to}": String(now), ...variables };
    let serialized = JSON.stringify(target);
    for (const [key, value] of Object.entries(replacements)) serialized = serialized.split(key).join(String(value));
    const query = JSON.parse(serialized);
    query.refId ??= String.fromCharCode(65 + index);
    if (typeof query.datasource === "string" || !query.datasource) query.datasource = { uid: "cloud-monitoring", type: "stackdriver" };
    query.intervalMs ??= 300000;
    query.maxDataPoints ??= 300;
    for (const key of ["timeSeriesList", "timeSeriesQuery", "promQLQuery"]) {
      if (query[key] && Object.keys(query[key]).length) query[key].projectName ??= project;
    }
    return query;
  });
}

const conditionalEmitters = {
  kernel_consumer_validation_errors_total: "backend/internal/domainconsumer/app/service.go",
  kernel_cloudtasks_idempotent_collisions_total: "backend/internal/platform/taskqueue/cloudtasks.go",
  kernel_sweeper_obligations_swept_total: "backend/internal/platform/kmetrics/sweeper.go",
  kernel_sweeper_tasks_created_total: "backend/internal/platform/kmetrics/sweeper.go",
  "logging_googleapis_com:user_goatos_rum_exceptions": "infra/observability/alloy-config.alloy",
};

export function conditionalDataReason(query) {
  const condition = query.goatosDataCondition;
  if (!condition) return null;
  const expression = query.promQLQuery?.expr ?? "";
  const entry = Object.entries(conditionalEmitters).find(([metric, source]) => [ `sum(rate(${metric}[5m]))`, `sum(increase(${metric}[5m]))` ].includes(expression) && condition.source === source);
  if (!entry || condition.kind !== "event-conditional" || !condition.event || /vector\(0\)|or\s+0/.test(expression)) throw new Error(`Invalid conditional-data annotation on ${query.refId}`);
  return `${condition.event}; emitter: ${condition.source}`;
}

export function assertQueryHealth(result, query, label) {
  const values = (name) => (result?.frames ?? []).flatMap((frame) => {
    const index = (frame.schema?.fields ?? []).findIndex((field) => field.name === name);
    return index < 0 ? [] : (frame.data?.values?.[index] ?? []);
  });
  if (query.goatosFreshnessMaxHours !== undefined) {
    const ages = values("age_hours");
    if (ages.length !== 1 || typeof ages[0] !== "number" || !Number.isFinite(ages[0]) || ages[0] < 0 || ages[0] > query.goatosFreshnessMaxHours) {
      throw new Error(`${label}: rollup never completed or last successful run is stale (> ${query.goatosFreshnessMaxHours}h)`);
    }
  }
  if (query.goatosRequiredStatus !== undefined) {
    const statuses = values("status");
    if (statuses.length !== 1 || statuses[0] !== query.goatosRequiredStatus) throw new Error(`${label}: latest rollup status is ${statuses[0] ?? "missing"}, expected ${query.goatosRequiredStatus}`);
  }
}

export function assertQueryResults(response, queries, label) {
  for (const query of queries) {
    const result = response?.results?.[query.refId];
    if (!result) throw new Error(`${label}/${query.refId}: missing query result`);
    if (result.error || (result.status && result.status >= 400)) throw new Error(`${label}/${query.refId}: ${result.error ?? `HTTP ${result.status}`}`);
  }
}

const firebaseDataSources = { firebase_crashlytics: "665f3379-0000-2ce4-9a00-001a1148aea6", firebase_sessions: "685fbeb4-0000-2244-a54b-30fd38104754", firebase_performance: "5c49fb6c-0000-24d0-86d7-883d24f8b018" };
const firebaseDatasets = Object.keys(firebaseDataSources);
const firebasePendingTargets = {
  1: { provider: "performance", sha256: "1e07a7480d45e222d112464546a53f2909154b530ac7d4e278867c381c3a8f4b" },
  5: { provider: "crash-sessions", sha256: "6e7133b4a86652d3fe615e4ea3d08bf55587cdcc3a37ec846f1db5c449974810" },
  9: { provider: "performance", sha256: "261506ebf0297e1e8677fc29e25d4a65fe43a7aee7778e2a4c09250a6982b98c" },
};

// A time-limited provider wait is not a data-health success. The immutable BQ
// creationTime prevents a fresh receipt from restarting the first-export clock.
export async function verifyFirebaseInitialExport(receipt, project, readBQ, now = Date.now()) {
  if (project !== "goatos-stg" || receipt.project !== project || receipt.appId !== "sg.mesha.goatos" || receipt.schemaVersion !== 1 || receipt.firebaseAppId !== "1:514832198871:android:2b3a80736ff2e8d9f19492") throw new Error("Invalid Firebase readiness project/app/version");
  if (!Array.isArray(receipt.datasets) || receipt.datasets.length !== 3 || new Set(receipt.datasets.map(d => d.id)).size !== 3) throw new Error("Firebase readiness requires exactly three datasets");
  const empty = {};
  const expires = {};
  for (const id of firebaseDatasets) {
    const declared = receipt.datasets.find(d => d.id === id);
    const created = Number(declared?.creationTime);
    if (!declared || !Number.isFinite(created) || created <= 0 || created > now) throw new Error(`Invalid Firebase creation time: ${id}`);
    const metadata = await readBQ(id, "metadata");
    if (metadata.datasetReference?.projectId !== project || metadata.datasetReference?.datasetId !== id || metadata.location !== "asia-south1" || Number(metadata.creationTime) !== created) throw new Error(`Firebase dataset metadata mismatch: ${id}`);
    if (!/^projects\/514832198871\/locations\/asia-south1\/transferConfigs\/[a-zA-Z0-9_-]+$/.test(declared.transferConfig ?? "") || declared.dataSourceId !== firebaseDataSources[id]) throw new Error(`Invalid Firebase transfer identity: ${id}`);
    const transfer = await readBQ(id, "transfer");
    if (transfer.name !== declared.transferConfig || transfer.destinationDatasetId !== id || transfer.dataSourceId !== declared.dataSourceId || transfer.disabled === true || ["FAILED", "CANCELLED"].includes(transfer.state) || transfer.params?.platform !== "ANDROID" || transfer.params?.client_namespace !== receipt.appId || transfer.params?.gmp_app_id !== receipt.firebaseAppId) throw new Error(`Firebase transfer disabled or source/configuration mismatched: ${id}`);
    const tables = await readBQ(id, "tables");
    if (!/^(0|[1-9][0-9]*)$/.test(String(tables.totalItems)) || !Number.isSafeInteger(Number(tables.totalItems)) || Number(tables.totalItems) < 0 || (tables.tables !== undefined && !Array.isArray(tables.tables))) throw new Error(`Invalid Firebase table-list response: ${id}`);
    empty[id] = Number(tables.totalItems) === 0 && !(tables.tables?.length) && !tables.nextPageToken;
    expires[id] = created + 48 * 60 * 60 * 1000;
  }
  const pending = {};
  for (const [provider, ids] of Object.entries({ performance: ["firebase_performance"], "crash-sessions": ["firebase_crashlytics", "firebase_sessions"] })) {
    const deadline = Math.min(...ids.map(id => expires[id]));
    if (ids.every(id => empty[id]) && now < deadline) pending[provider] = new Date(deadline).toISOString();
  }
  return pending;
}

export function firebasePendingReason(uid, panel, query, pending) {
  const spec = firebasePendingTargets[panel.id];
  if (uid !== "goatos-stg-mobile" || !spec || query.refId !== "A" || query.datasource?.uid !== "postgres-analytics" || !pending[spec.provider]) return null;
  if (createHash("sha256").update(query.rawSql ?? "").digest("hex") !== spec.sha256) throw new Error(`Firebase pending target SQL changed: ${panel.id}`);
  return `${spec.provider}: awaiting initial Firebase export until ${pending[spec.provider]} (live datasets still empty)`;
}

async function loadFirebaseInitialExport(project) {
  const receiptPath = arg("--firebase-initial-export-receipt", process.env.GOATOS_FIREBASE_INITIAL_EXPORT_RECEIPT);
  if (!receiptPath) return {};
  const receipt = JSON.parse(readFileSync(receiptPath, "utf8"));
  const token = shellOut("gcloud", ["auth", "print-access-token"], { timeout: 15000 });
  return verifyFirebaseInitialExport(receipt, project, async (dataset, kind) => {
    const suffix = kind === "tables" ? "/tables?maxResults=1" : "";
    const url = kind === "transfer" ? `https://bigquerydatatransfer.googleapis.com/v1/${receipt.datasets.find(d => d.id === dataset).transferConfig}` : `https://bigquery.googleapis.com/bigquery/v2/projects/${project}/datasets/${dataset}${suffix}`;
    const response = await fetch(url, { headers: { Authorization: `Bearer ${token}` }, signal: AbortSignal.timeout(15000) });
    if (!response.ok) throw new Error(`Firebase dataset ${kind} read failed: ${dataset} HTTP ${response.status}`);
    return response.json();
  });
}

// Only a live-verified, still-empty initial export may be omitted from this
// execution. Job configuration remains intact for the next scheduled retry.
export function firebaseRollupArgs(pending, tables) {
  const args = [];
  const expected = {
    crash: "goatos-stg.firebase_crashlytics.sg_mesha_goatos_ANDROID",
    sessions: "goatos-stg.firebase_sessions.sg_mesha_goatos_ANDROID",
    performance: "goatos-stg.firebase_performance.sg_mesha_goatos_ANDROID",
  };
  if (pending["crash-sessions"]) {
    if (tables.crash !== expected.crash || tables.sessions !== expected.sessions) throw new Error("Pending receipt does not cover configured crash/session tables");
    args.push("-crashlytics-bq-table=", "-crashlytics-sessions-table=");
  }
  if (pending.performance) {
    if (tables.performance !== expected.performance) throw new Error("Pending receipt does not cover configured performance table");
    args.push("-performance-bq-table=");
  }
  return args.join(",");
}

export function assertSmokeDataMode({ queryValidityOnly, requireAll, empty, providerPending }) {
  if (queryValidityOnly && requireAll) throw new Error("query-validity-only cannot be combined with require-all-panel-data");
  if (queryValidityOnly) return "QUERY VALIDITY PASS; FINAL DATA CERTIFICATION PENDING";
  if (empty.length) throw new Error(`All-panel data required: ${empty.length} targets are empty`);
  return providerPending.length ? "AVAILABLE-DATA PASS; FIREBASE INITIAL EXPORT PENDING" : "FULL-DATA PASS";
}

export function assertExceptionMetricDefinition(actual, expected) {
  if (actual.disabled === true || actual.name !== expected.name || actual.filter !== expected.filter || actual.metricDescriptor?.metricKind !== expected.metricDescriptor.metricKind || actual.metricDescriptor?.valueType !== expected.metricDescriptor.valueType) throw new Error("RUM exception metric missing or definition mismatched");
}

function verifyExceptionMetric(project) {
  const metrics = JSON.parse(readFileSync(path.join(repo, "infra/observability/faro-log-metrics.json"), "utf8"));
  const expected = metrics.find(metric => metric.name === "goatos_rum_exceptions");
  if (!expected) throw new Error("RUM exception metric source definition missing");
  const actual = JSON.parse(shellOut("gcloud", ["logging", "metrics", "describe", expected.name, `--project=${project}`, "--format=json"], { timeout: 15000 }));
  assertExceptionMetricDefinition(actual, expected);
}

export async function assertAllDashboardQueries(baseUrl, password, timeoutMs, iamToken, project, dashboards, firebasePending = {}) {
  const empty = [];
  const providerPending = [];
  const conditionalEmpty = [];
  let checked = 0;
  for (const dashboard of dashboards) {
    const { now, fromMs, range } = grafanaRange(dashboard.doc.time?.from === "now-7d" ? 168 : 24);
    const variables = {};
    for (const variable of dashboard.doc.templating?.list ?? []) {
      const value = variable.current?.value;
      if (value === "$__all") variables[`\${${variable.name}:regex}`] = variable.allValue || ".*";
      else if (typeof value === "string") variables[`\${${variable.name}:sqlstring}`] = `'${value.replaceAll("'", "''")}'`;
      // Exercise range-variable datasource independently, rather than hiding a
      // broken variable query behind the smoke's explicit duration substitution.
      if (variable.name === "window_seconds") {
        const query = panelQueries({ targets: [{ refId: "window", datasource: variable.datasource, rawSql: variable.query, format: "table", rawQuery: true }] }, project, { fromMs, now })[0];
        const result = await postJson(baseUrl, "/api/ds/query", { from: String(fromMs), to: String(now), range, queries: [query] }, password, timeoutMs, iamToken);
        assertQueryResults(result, [query], `${dashboard.uid}/window_seconds`);
        if (!grafanaQueryHasData(result)) throw new Error(`${dashboard.uid}: selected-window variable has no value`);
      }
    }
    const walk = (panels) => panels.flatMap((panel) => [panel, ...walk(panel.panels ?? [])]);
    for (const panel of walk(dashboard.doc.panels ?? [])) {
      const queries = panelQueries(panel, project, { fromMs, now, variables });
      if (panel.goatosMissingCapability) empty.push(`${dashboard.uid}/${panel.title}: missing capability ${panel.goatosMissingCapability}`);
      if (!queries.length) continue;
      const response = await postJson(baseUrl, "/api/ds/query", { from: String(fromMs), to: String(now), range, queries }, password, timeoutMs, iamToken);
      assertQueryResults(response, queries, `${dashboard.uid}/${panel.title}`);
      for (const query of queries) {
        checked++;
        assertQueryHealth(response.results[query.refId], query, `${dashboard.uid}/${panel.title}`);
        const condition = conditionalDataReason(query);
        if (!grafanaQueryHasData({ results: { [query.refId]: response.results[query.refId] } })) {
          const label = `${dashboard.uid}/${panel.title}/${query.refId}`;
          if (condition) {
            if (query.promQLQuery?.expr?.includes("logging_googleapis_com:user_goatos_rum_exceptions")) verifyExceptionMetric(project);
            conditionalEmpty.push(`${label} [${condition}]`);
          }
          else {
            const waiting = firebasePendingReason(dashboard.uid, panel, query, firebasePending);
            if (waiting) providerPending.push(`${label} [${waiting}]`);
            else empty.push(`${label} [collection/data missing]`);
          }
        }
      }
    }
  }
  console.log(`grafana-smoke: all ${checked} committed panel targets executed without query errors`);
  if (conditionalEmpty.length) console.log(`grafana-smoke: event-conditional targets without observations (not zero): ${conditionalEmpty.join("; ")}`);
  if (empty.length) console.log(`grafana-smoke: EMPTY panel targets (not evidence of success/zero): ${empty.join("; ")}`);
  if (providerPending.length) console.log(`grafana-smoke: PROVIDER PENDING (not data-ready): ${providerPending.join("; ")}`);
  return { empty, providerPending };
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

export function assertDashboardQueryReadback(expected, actual) {
  const view = (doc) => ({
    title: doc.title,
    time: doc.time,
    variables: (doc.templating?.list ?? []).map(({ name, type, query, refresh, datasource }) => ({ name, type, query, refresh, datasource })),
    panels: (doc.panels ?? []).map(({ id, title, type, targets, panels }) => ({ id, title, type, targets: targets ?? [], panels: panels ?? [] })),
  });
  assert.deepEqual(view(actual), view(expected), `Live dashboard ${expected.uid} queries/config do not match committed provisioning`);
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
      for (const dashboard of dashboards) {
        if (dashboard.doc) {
          const loaded = await read(`/api/dashboards/uid/${encodeURIComponent(dashboard.uid)}`);
          assertDashboardQueryReadback(dashboard.doc, loaded?.dashboard ?? {});
        }
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
  if (hasFlag("--firebase-rollup-args")) {
    if (!arg("--firebase-initial-export-receipt")) throw new Error("Initial-export receipt required");
    const pending = await loadFirebaseInitialExport("goatos-stg");
    const args = firebaseRollupArgs(pending, {
      crash: arg("--crash-table"), sessions: arg("--sessions-table"), performance: arg("--performance-table"),
    });
    if (args) console.error("Firebase initial export pending; this execution omits only verified pending providers. Full-data certification remains pending.");
    console.log(args);
    return;
  }
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
    const queryValidityOnly = hasFlag("--query-validity-only");
    if (queryValidityOnly && hasFlag("--require-all-panel-data")) throw new Error("query-validity-only cannot be combined with require-all-panel-data");
    const firebasePending = queryValidityOnly ? {} : await loadFirebaseInitialExport(project);
    const { empty: emptyPanels, providerPending } = await assertAllDashboardQueries(baseUrl, password, 30000, iamToken, project, dashboards, firebasePending);
    const dataStatus = assertSmokeDataMode({ queryValidityOnly, requireAll: hasFlag("--require-all-panel-data"), empty: emptyPanels, providerPending });
    console.log(`grafana-smoke: ${dataStatus} (${dashboards.length} committed dashboards, ${requiredDatasourceUids.length} datasources, representative live data queries, and feature-wise live data queries checked in live Grafana)`);
  } catch (error) {
    const proxyDiag = proxy?.diagnostics();
    if (proxyDiag) console.error(proxyDiag);
    throw error;
  } finally {
    proxy?.stop();
  }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) main().catch((error) => {
  console.error(`grafana-smoke: FAILED: ${error.message}`);
  process.exit(1);
});
