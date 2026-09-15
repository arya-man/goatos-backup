#!/usr/bin/env node
// Post-deploy smoke for staging Grafana dashboard durability.
//
// Compares committed infra/grafana/dashboards/*.json UIDs against the live
// Grafana API. It starts a local `gcloud run services proxy` by default for
// developer laptops. In Cloud Build, use --direct-iam so Grafana keeps its Basic
// auth header while Cloud Run IAM gets X-Serverless-Authorization.

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
      return { file: name, uid: doc.uid, title: doc.title, panels: Array.isArray(doc.panels) ? doc.panels.length : 0 };
    });
}

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
  const args = [
    "auth",
    "print-identity-token",
    `--audiences=${audience}`,
  ];
  if (serviceAccount) args.push(`--impersonate-service-account=${serviceAccount}`);
  return shellOut("gcloud", args);
}

async function fetchJson(baseUrl, apiPath, password, timeoutMs, iamToken = "") {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  try {
    const headers = { Authorization: authHeader(password) };
    if (iamToken) headers["X-Serverless-Authorization"] = `Bearer ${iamToken}`;
    const response = await fetch(new URL(apiPath, baseUrl), {
      headers,
      signal: controller.signal,
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

async function waitForGrafana(baseUrl, password, timeoutMs, iamToken = "") {
  const started = Date.now();
  let lastError = null;
  while (Date.now() - started < timeoutMs) {
    try {
      await fetchJson(baseUrl, "/api/health", password, 5000, iamToken);
      return;
    } catch (error) {
      lastError = error;
      await new Promise((resolve) => setTimeout(resolve, 1000));
    }
  }
  throw new Error(`Grafana API did not become ready at ${baseUrl}: ${lastError?.message ?? "timeout"}`);
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
    await waitForGrafana(baseUrl, password, timeoutMs, iamToken);
    const search = await fetchJson(baseUrl, "/api/search?type=dash-db", password, 15000, iamToken);
    const liveUids = new Set((Array.isArray(search) ? search : []).map((item) => item.uid).filter(Boolean));
    if (liveUids.size < dashboards.length) {
      throw new Error(`Grafana search returned ${liveUids.size} dashboards, but ${dashboards.length} dashboard JSONs are committed`);
    }

    const missing = [];
    for (const dashboard of dashboards) {
      if (!liveUids.has(dashboard.uid)) missing.push(`${dashboard.uid} (${dashboard.file})`);
      const live = await fetchJson(baseUrl, `/api/dashboards/uid/${encodeURIComponent(dashboard.uid)}`, password, 15000, iamToken);
      const livePanels = live?.dashboard?.panels;
      if (!Array.isArray(livePanels) || livePanels.length === 0) {
        throw new Error(`Live Grafana dashboard ${dashboard.uid} has no panels`);
      }
    }
    if (missing.length) {
      throw new Error(`Live Grafana is missing committed dashboards: ${missing.join(", ")}`);
    }
    console.log(`grafana-smoke: OK (${dashboards.length} committed dashboards present in live Grafana)`);
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
