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
  const fullParityReceiptOk = config.businessDataParity.latestFullParityReceiptRequired === true
    ? layer("latest-full-parity-receipt", "deterministic", () => runLatestFullParityReceipt())
    : true;
  const dataParityOk = fullParityReceiptOk && (
    enabled("GOATOS_DASHBOARD_DATA_PARITY", config.businessDataParity.enabledByDefault)
      ? layer("business-data-parity", "deterministic", () => runBusinessDataParity(outDir))
      : true
  );
  const ociOk = staticOk && fullParityReceiptOk && dataParityOk && layer("oci-free-preflight", "deterministic", () => assertOciAlwaysFree());
  if (!ociOk) throw new Error("stopping before runtime automation because a prerequisite deterministic layer failed");
  layer("firebase-analytics-guard", "deterministic", () => runNode(["tools/agent-hooks/check-firebase-analytics-param-budget.mjs"]));
  if (enabled("GOATOS_DASHBOARD_API_LATENCY", true)) {
    layer("api-latency", "deterministic", () => runApiLatency(outDir));
  }
  if (process.env.GOATOS_DASHBOARD_LIGHTHOUSE === "1") {
    layer("lighthouse", "deterministic", () => runLighthouse(outDir));
  }
  if (process.env.GOATOS_DASHBOARD_GRAFANA_SMOKE === "1") {
    layer("grafana-smoke", "deterministic", () => runNode(["tools/deploy/smoke-stg-grafana-dashboards.mjs"]));
  }
  layer("vaccination-lifecycle", "deterministic", () => runVaccinationLifecycleTests());
  if (mode === "post-main-certification") {
    layer("postgresql-integration", "deterministic", () => assertPostgresIntegrationConfigured());
    layer("playwright-module-journeys", "deterministic", () => runPreviewPlaywright(outDir));
  } else if (mode === "production-smoke") {
    layer("production-module-journeys", "deterministic", () => runProductionSmoke(outDir));
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
  if (receipt.status !== "pass" && enabled("GOATOS_DASHBOARD_SELF_HEALING", config.selfHealing.enabledByDefault)) {
    const selfHeal = spawnSync(process.execPath, ["tools/dashboard-automation/self-heal-pr.mjs", "--receipt", receiptPath], {
      cwd: repo,
      env: process.env,
      encoding: "utf8"
    });
    receipt.selfHealing = {
      status: selfHeal.status === 0 ? "completed" : "failed",
      stdout: redactText(selfHeal.stdout).trim(),
      stderr: redactText(selfHeal.stderr).trim()
    };
    if (selfHeal.status !== 0) {
      receipt.blockers.push({
        layer: "self-healing",
        message: receipt.selfHealing.stderr || receipt.selfHealing.stdout || `self-healing exited ${selfHeal.status}`
      });
    }
    writeReceipt(receiptPath, receipt);
  }
  if (enabled("GOATOS_DASHBOARD_SLACK_ALERTS", config.slackAlerts.enabledByDefault)) {
    const slack = spawnSync(process.execPath, ["tools/dashboard-automation/notify-slack.mjs", "--receipt", receiptPath], {
      cwd: repo,
      env: process.env,
      encoding: "utf8"
    });
    receipt.slackAlert = {
      status: slack.status === 0 ? "completed" : "failed",
      stdout: redactText(slack.stdout).trim(),
      stderr: redactText(slack.stderr).trim()
    };
    writeReceipt(receiptPath, receipt);
  }
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

function enabled(envName, defaultValue = false) {
  const value = process.env[envName];
  if (value == null || value === "") return Boolean(defaultValue);
  return !["0", "false", "no", "off"].includes(String(value).trim().toLowerCase());
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

function runPreviewPlaywright(targetDir) {
  const required = ["GOATOS_ADMIN_WEB_BASE_URL", "GOATOS_API_BASE_URL", "GOATOS_BEARER_TOKEN", "GOATOS_TENANT_ID"];
  const missing = required.filter((key) => !process.env[key]);
  if (missing.length) throw new Error(`missing preview Playwright env: ${missing.join(", ")}`);
  runNode(["tools/dashboard-automation/run-module-journeys.mjs", "--out-dir", path.join(targetDir, "module-journeys")], {
    ...process.env,
    GOATOS_SMOKE_READ_ONLY: "1"
  });
}

function runProductionSmoke(targetDir) {
  const required = ["GOATOS_API_BASE_URL", "GOATOS_BEARER_TOKEN", "GOATOS_TENANT_ID"];
  const missing = required.filter((key) => !process.env[key]);
  if (missing.length) throw new Error(`auth_blocked: missing production smoke env: ${missing.join(", ")}`);
  const apiUrl = new URL(process.env.GOATOS_API_BASE_URL);
  if (apiUrl.protocol !== "https:" || !["api.goatos.mesha.sg", "api.mesha.sg", "goatos-api.mesha.sg"].includes(apiUrl.hostname)) {
    throw new Error(`daily production smoke refuses non-production API URL: ${apiUrl.origin}`);
  }
  runNode(["tools/dashboard-automation/run-module-journeys.mjs", "--out-dir", path.join(targetDir, "module-journeys")], {
    ...process.env,
    GOATOS_ADMIN_WEB_BASE_URL: config.productionUrl,
    GOATOS_SMOKE_READ_ONLY: "1"
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

function runBusinessDataParity(targetDir) {
  const output = path.join(targetDir, "business-data-parity.json");
  runNode(["tools/dashboard-automation/check-business-data-parity.mjs", "--out", output]);
  receipt.artifacts.push({ kind: "business-data-parity-report", path: output });
}

function runLatestFullParityReceipt() {
  runNode(["tools/dashboard-automation/check-latest-parity-receipt.mjs"]);
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

function runVaccinationLifecycleTests() {
  run("go", [
    "test",
    "./internal/vaccination/app",
    "-run",
    "TestGenerateForVersionUsesTopLevelProcurementFirstWaveWhenPurposeBlank|TestGenerateForVersionUsesProcurementPurposePlans|TestGenerateForVersionDefersDuringWarmupHold|TestGenerateForVersionTreatsTerminalAndClinicalStatesDifferently",
    "-count=1"
  ]);
  run("go", [
    "test",
    "./internal/obligation/adapters/postgres",
    "-run",
    "TestPublishingAnAddedVaccineLeavesTheOtherFiveUntouched|TestCarryOverRebindsMedicalEquivalentPrimaryCourseFollowUp|TestGoatExitedRemovesAnimalFromPlannedDriveAssignments|TestMatrixClinicalDeferHoldsWorkAndReleasesPlannedDrive|TestMatrixClinicalRecoveryReopensWithoutCorruptingPlannedDrive",
    "-count=1"
  ]);
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
  if (!config.selfHealing.checksBeforePr.includes("vaccinationLifecycle")) throw new Error("self-test: self-healing must include vaccination lifecycle checks");
  if (config.businessDataParity.enabledByDefault !== true) throw new Error("self-test: business data parity must be default-on for OCI automation");
  if (config.businessDataParity.latestFullParityReceiptRequired !== true) throw new Error("self-test: latest full STG-to-OCI parity receipt must be required");
  if (config.businessDataParity.latestFullParityReceiptIncludedTableCount !== 293) throw new Error("self-test: full parity receipt must require 293 included tables");
  if (config.businessDataParity.latestFullParityReceiptStatus !== "READBACK_PASS") throw new Error("self-test: full parity receipt must require READBACK_PASS");
  if (config.slackAlerts.enabledByDefault !== true) throw new Error("self-test: Slack alerts must be default-on for OCI automation");
  if (config.selfHealing.enabledByDefault !== true) throw new Error("self-test: self-healing PR creation must be default-on for failing OCI automation");
  if (!config.businessDataParity.sentinelQueries.some((item) => item.name === "godel_2_timewise_adg" && item.implementationStatus === "implemented")) {
    throw new Error("self-test: Manohar/Godel ADG sentinel must stay configured as implemented");
  }
  if (!config.businessDataParity.sentinelQueries.some((item) => item.name === "castro_reconciliation" && item.implementationStatus === "implemented")) {
    throw new Error("self-test: Castro reconciliation sentinel must stay configured as implemented");
  }
  if (!readFileSync(fileURLToPath(import.meta.url), "utf8").includes("run-module-journeys.mjs")) {
    throw new Error("self-test: runner must invoke module-wise Playwright journeys, not only one generic smoke");
  }
  if (!readFileSync(fileURLToPath(import.meta.url), "utf8").includes("runVaccinationLifecycleTests")) {
    throw new Error("self-test: runner must invoke deep vaccination lifecycle tests");
  }
  for (const key of ["GOATOS_DASHBOARD_DATA_PARITY", "GOATOS_DASHBOARD_API_LATENCY", "GOATOS_DASHBOARD_LIGHTHOUSE", "GOATOS_DASHBOARD_GRAFANA_SMOKE"]) {
    if (!readFileSync(fileURLToPath(import.meta.url), "utf8").includes(key)) {
      throw new Error(`self-test: runner no longer wires ${key}`);
    }
  }
  runNode(["tools/dashboard-automation/check-module-journeys.mjs", "--self-test"]);
  runNode(["tools/dashboard-automation/run-module-journeys.mjs", "--self-test"]);
  runNode(["tools/dashboard-automation/self-heal-pr.mjs", "--self-test"]);
  runNode(["tools/dashboard-automation/notify-slack.mjs", "--self-test"]);
  runNode(["tools/dashboard-automation/check-latest-parity-receipt.mjs", "--self-test"]);
  console.log("dashboard automation runner: self-test passed");
}
