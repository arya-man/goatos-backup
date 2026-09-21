#!/usr/bin/env node
import { execFileSync, spawnSync } from "node:child_process";
import { existsSync, mkdirSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { containsUnredactedSecret, redactText } from "./lib/redact.mjs";

const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const config = JSON.parse(readFileSync(path.join(repo, "tools/dashboard-automation/config.json"), "utf8"));
const args = parseArgs(process.argv.slice(2));

if (args.selfTest) {
  selfTest();
  process.exit(0);
}

const mode = args.mode ?? "production-smoke";
const runId = new Date().toISOString().replaceAll(/[:.]/g, "-");
const outDir = path.resolve(args.outDir ?? path.join(repo, ".codex-goatos-render/dashboard-automation", runId));
mkdirSync(outDir, { recursive: true });

const receipt = {
  runId,
  mode,
  startedAt: new Date().toISOString(),
  repoSha: git(["rev-parse", "HEAD"]),
  originMainSha: git(["rev-parse", "origin/main"]),
  productionUrl: config.productionUrl,
  layers: [],
  artifacts: [],
  blockers: [],
  fatalError: null
};

try {
  const staticOk = layer("static", "deterministic", () => runNode(["tools/dashboard-automation/check-static-inventory.mjs"]));
  let dataParityOk = true;
  if (process.env.GOATOS_DASHBOARD_DATA_PARITY === "1") {
    dataParityOk = layer("business-data-parity", "deterministic", () => runNode(["tools/dashboard-automation/check-business-data-parity.mjs"]));
  }
  const ociOk = staticOk && dataParityOk && layer("oci-free-preflight", "deterministic", () => assertOciAlwaysFree());
  if (!ociOk) throw new Error("stopping before runtime automation because a prerequisite deterministic layer failed");
  layer("firebase-analytics-guard", "deterministic", () => runNode(["tools/agent-hooks/check-firebase-analytics-param-budget.mjs"]));
  if (process.env.GOATOS_DASHBOARD_API_LATENCY === "1") {
    layer("api-latency", "deterministic", () => runApiLatency(outDir));
  }
  if (process.env.GOATOS_DASHBOARD_LIGHTHOUSE === "1") {
    layer("lighthouse", "deterministic", () => runLighthouse(outDir));
  }
  if (process.env.GOATOS_DASHBOARD_GRAFANA_SMOKE === "1") {
    layer("grafana-smoke", "deterministic", () => runNode(["tools/deploy/smoke-stg-grafana-dashboards.mjs"]));
  }
  if (mode === "post-main-certification") {
    layer("postgresql-integration", "deterministic", () => assertPostgresIntegrationConfigured());
    layer("playwright-e2e", "deterministic", () => runPreviewPlaywright());
  } else if (mode === "production-smoke") {
    layer("production-playwright-smoke", "deterministic", () => runProductionSmoke());
  } else {
    throw new Error(`unknown mode: ${mode}`);
  }
} catch (error) {
  const message = redactText(error?.message ?? String(error));
  receipt.fatalError = message;
  receipt.blockers.push({ layer: "runner", message });
} finally {
  receipt.finishedAt = new Date().toISOString();
  receipt.status = !receipt.fatalError && receipt.layers.length > 0 && receipt.layers.every((item) => item.status === "pass") ? "pass" : "fail";
  const receiptPath = path.join(outDir, "receipt.json");
  writeReceipt(receiptPath, receipt);
  const agent = spawnSync(process.execPath, ["tools/dashboard-automation/agent-review.mjs", "--receipt", receiptPath], {
    cwd: repo,
    env: process.env,
    encoding: "utf8"
  });
  receipt.agentReview = {
    status: agent.status === 0 ? "completed" : "failed",
    stdout: redactText(agent.stdout).trim(),
    stderr: redactText(agent.stderr).trim()
  };
  if (agent.status !== 0) {
    receipt.status = "fail";
    receipt.blockers.push({
      layer: "agent-review",
      message: receipt.agentReview.stderr || receipt.agentReview.stdout || `agent review exited ${agent.status}`
    });
  }
  writeReceipt(receiptPath, receipt);
  console.log(`dashboard automation ${receipt.status}; receipt ${path.relative(repo, receiptPath)}`);
  if (receipt.status !== "pass") process.exit(1);
}

function layer(name, authority, fn) {
  const startedAt = new Date().toISOString();
  try {
    fn();
    receipt.layers.push({ name, authority, status: "pass", startedAt, finishedAt: new Date().toISOString() });
    return true;
  } catch (error) {
    const message = redactText(error?.message ?? String(error));
    receipt.layers.push({ name, authority, status: "fail", startedAt, finishedAt: new Date().toISOString(), message });
    receipt.blockers.push({ layer: name, message });
    return false;
  }
}

function assertOciAlwaysFree() {
  const freeGb = filesystemFreeGb(repo);
  if (freeGb < config.ociAlwaysFree.minimumFreeFilesystemGb) {
    throw new Error(`filesystem headroom ${freeGb.toFixed(1)} GB is below required ${config.ociAlwaysFree.minimumFreeFilesystemGb} GB`);
  }
  if (process.env.GOATOS_OCI_ALWAYS_FREE_CONFIRMED !== "1") {
    throw new Error("GOATOS_OCI_ALWAYS_FREE_CONFIRMED=1 is required before this runner can use OCI resources");
  }
  if (process.env.GOATOS_OCI_ALLOW_PAID === "1") {
    throw new Error("refusing GOATOS_OCI_ALLOW_PAID=1; dashboard automation is free-tier only");
  }
}

function assertPostgresIntegrationConfigured() {
  if (!process.env.GOATOS_DASHBOARD_PREVIEW_DATABASE_URL) {
    throw new Error("GOATOS_DASHBOARD_PREVIEW_DATABASE_URL is required for disposable PostgreSQL preview certification");
  }
  if (!/dashboard[_-]automation|preview|tmp|throwaway/i.test(process.env.GOATOS_DASHBOARD_PREVIEW_DATABASE_URL)) {
    throw new Error("preview database URL must identify a run-owned disposable dashboard automation database");
  }
}

function runPreviewPlaywright() {
  const required = ["GOATOS_ADMIN_WEB_BASE_URL", "GOATOS_API_BASE_URL", "GOATOS_BEARER_TOKEN", "GOATOS_TENANT_ID"];
  const missing = required.filter((key) => !process.env[key]);
  if (missing.length) throw new Error(`missing preview Playwright env: ${missing.join(", ")}`);
  runNpm(["--prefix", "apps/admin-web", "run", "smoke:visual:live"]);
}

function runProductionSmoke() {
  const required = ["GOATOS_API_BASE_URL", "GOATOS_BEARER_TOKEN", "GOATOS_TENANT_ID"];
  const missing = required.filter((key) => !process.env[key]);
  if (missing.length) throw new Error(`auth_blocked: missing production smoke env: ${missing.join(", ")}`);
  const apiUrl = new URL(process.env.GOATOS_API_BASE_URL);
  if (apiUrl.protocol !== "https:" || !["api.mesha.sg", "goatos-api.mesha.sg"].includes(apiUrl.hostname)) {
    throw new Error(`daily production smoke refuses non-production API URL: ${apiUrl.origin}`);
  }
  runNpm(["--prefix", "apps/admin-web", "run", "smoke:visual:live"], {
    ...process.env,
    GOATOS_ADMIN_WEB_BASE_URL: config.productionUrl
  });
}

function runApiLatency(targetDir) {
  const manifests = readdirSync(path.join(repo, "tools/perf"))
    .filter((name) => /^hot-paths\..*\.json$/.test(name))
    .sort();
  if (manifests.length === 0) throw new Error("no API latency hot-path manifests found under tools/perf");
  for (const manifestName of manifests) {
    const output = path.join(targetDir, `api-latency-${manifestName.replaceAll(/[^a-zA-Z0-9._-]/g, "_")}`);
    runNode([
      "tools/perf/api-latency-gate.mjs",
      "--manifest",
      path.join("tools/perf", manifestName),
      "--output",
      output,
      "--expected-sha",
      receipt.repoSha
    ]);
    receipt.artifacts.push({ kind: "api-latency-report", manifest: path.join("tools/perf", manifestName), path: output });
  }
}

function runLighthouse(targetDir) {
  const output = path.join(targetDir, "lighthouse.json");
  const headers = JSON.stringify({
    Cookie: `goatos_firebase_id_token=${process.env.GOATOS_BEARER_TOKEN ?? ""}`,
    "X-GoatOS-Tenant-ID": process.env.GOATOS_TENANT_ID ?? ""
  });
  runNode(["apps/admin-web/scripts/capture-lighthouse.mjs", "--url", config.productionUrl, "--output", output], {
    ...process.env,
    ADMIN_WEB_LIGHTHOUSE_EXPECT_FINAL_URL_CONTAINS: new URL(config.productionUrl).host,
    ADMIN_WEB_LIGHTHOUSE_EXTRA_HEADERS: headers
  });
  receipt.artifacts.push({ kind: "lighthouse-report", path: output });
}

function filesystemFreeGb(target) {
  const output = execFileSync("df", ["-k", target], { encoding: "utf8" }).trim().split("\n").at(-1);
  const parts = output.trim().split(/\s+/);
  return Number(parts[3]) / 1024 / 1024;
}

function runNode(args, env = process.env) {
  run(process.execPath, args, env);
}

function runNpm(args, env = process.env) {
  run("npm", args, env);
}

function run(command, args, env = process.env) {
  const result = spawnSync(command, args, { cwd: repo, env, encoding: "utf8" });
  const stdout = redactText(result.stdout);
  const stderr = redactText(result.stderr);
  if (stdout.trim()) console.log(stdout.trim());
  if (stderr.trim()) console.error(stderr.trim());
  if (result.status !== 0) throw new Error(`${command} ${args.join(" ")} failed with exit ${result.status}`);
}

function writeReceipt(file, value) {
  const text = `${JSON.stringify(value, null, 2)}\n`;
  if (containsUnredactedSecret(text)) throw new Error("refusing to write receipt that appears to contain an unredacted secret");
  writeFileSync(file, text);
}

function git(args) {
  return execFileSync("git", args, { cwd: repo, encoding: "utf8" }).trim();
}

function parseArgs(raw) {
  const parsed = {};
  for (let i = 0; i < raw.length; i += 1) {
    const arg = raw[i];
    if (arg === "--self-test") parsed.selfTest = true;
    else if (arg === "--mode") parsed.mode = raw[++i];
    else if (arg === "--out-dir") parsed.outDir = raw[++i];
    else throw new Error(`unknown argument: ${arg}`);
  }
  return parsed;
}

function selfTest() {
  if (!containsUnredactedSecret("Bearer abc.def")) throw new Error("self-test: bearer should be detected before redaction");
  const redacted = redactText("postgres://user:pass@example/db?token=secret");
  if (redacted.includes("pass") || redacted.includes("secret")) throw new Error("self-test: redaction failed");
  if (config.selfHealing.mode !== "pull_request_only") throw new Error("self-test: self-healing must be PR-only");
  if (!config.selfHealing.forbiddenActions.includes("writeOciData")) throw new Error("self-test: OCI writes must remain forbidden");
  if (config.apiLatencyPolicy.normalDashboardApisMustStayUnderMs !== 500) throw new Error("self-test: dashboard API latency policy drifted");
  if (!config.selfHealing.checksBeforePr.includes("apiLatencyPolicy")) throw new Error("self-test: self-healing must include API latency checks");
  console.log("dashboard automation runner: self-test passed");
}
