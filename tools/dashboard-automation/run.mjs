#!/usr/bin/env node
import { execFileSync, spawnSync } from "node:child_process";
import { existsSync, mkdirSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { containsUnredactedSecret, redactText } from "./lib/redact.mjs";
import { classifyWriteTarget } from "./lib/table-snapshot.mjs";
import { modeFlags } from "./lib/run-modes.mjs";

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
  runtimePolicy: runtimePolicyForMode(mode),
  layers: [],
  artifacts: [],
  blockers: [],
  degraded: [],
  fatalError: null
};

try {
  // lib/run-modes.mjs, so the two pre-existing modes can be PROVED unchanged by a unit test.
  const { isProductionSmoke, isWriteJourneys, parityIsNeverAGate, runCertificationExtras, dataTrustWhenUnchecked } =
    modeFlags(mode, enabled("GOATOS_DASHBOARD_CERTIFICATION_EXTRAS", false));
  const staticOk = layer("static", "deterministic", () => runNode(["tools/dashboard-automation/check-static-inventory.mjs"]));
  const fullParityReceiptOk = parityIsNeverAGate
    ? true
    : (config.businessDataParity.latestFullParityReceiptRequired === true
      ? layer("latest-full-parity-receipt", "deterministic", () => runLatestFullParityReceipt())
      : true);
  const dataParityOk = parityIsNeverAGate || (fullParityReceiptOk && (
    enabled("GOATOS_DASHBOARD_DATA_PARITY", config.businessDataParity.enabledByDefault)
      ? layer("business-data-parity", "deterministic", () => runBusinessDataParity(outDir))
      : true
  ));
  const ociFreeOk = layer("oci-free-preflight", "deterministic", () => assertOciAlwaysFree());
  const strictPrereqOk = staticOk && fullParityReceiptOk && dataParityOk && ociFreeOk;
  const runtimePrereqOk = staticOk && ociFreeOk && (receipt.runtimePolicy.dataParityRequiredBeforeBrowser === false || (fullParityReceiptOk && dataParityOk));
  if (!runtimePrereqOk) {
    receipt.runtimePolicy.browserSmoke = "not_run";
    throw new Error("stopping before runtime automation because a required deterministic layer failed");
  }
  if (isWriteJourneys || isProductionSmoke) {
    receipt.runtimePolicy.dataTrust = dataTrustWhenUnchecked;
  } else if (!strictPrereqOk && receipt.runtimePolicy.dataParityRequiredBeforeBrowser === false) {
    receipt.runtimePolicy.dataTrust = "degraded";
    receipt.degraded.push({
      layer: "business-data-parity",
      kind: "degraded_browser_smoke",
      message: "production read-only browser smoke will run even though STG-to-OCI parity is not certified; certification remains failed"
    });
  } else {
    receipt.runtimePolicy.dataTrust = "certified";
  }
  layer("firebase-analytics-guard", "deterministic", () => runNode(["tools/agent-hooks/check-firebase-analytics-param-budget.mjs"]));
  // Lane 2: read-only SQL against the production replica. Seconds of pure reads, so it is on by
  // default; it never writes and never blocks the browser sweep that follows it.
  if (enabled("GOATOS_DASHBOARD_DATA_SANITY", true)) {
    layer("data-sanity", "deterministic", () => runDataSanity(outDir));
  }
  // Lane 5: the farm journeys on a Firebase Test Lab VIRTUAL device. DEFAULT OFF, and it
  // stays off: the free tier is 10 tests and 60 device-minutes a day for the whole project,
  // so this is a nightly or on-demand layer, never something every run spends quota on.
  // The runner refuses a matrix that would cross either line rather than trimming it.
  if (enabled("GOATOS_DASHBOARD_ANDROID_JOURNEYS", false)) {
    layer("android-journeys", "deterministic", () => runAndroidJourneys(outDir));
  }
  if (runCertificationExtras && enabled("GOATOS_DASHBOARD_API_LATENCY", true)) {
    layer("api-latency", "deterministic", () => runApiLatency(outDir));
  }
  // Lane 3. Read-only GET sweep of the API the dashboard's pages call: no 5xx, the shape
  // the page contract promises, required fields present, no null/NaN/unknown enum in a
  // field a screen renders, and latency inside apiLatencyPolicy. Default-on for the daily
  // production smoke so it runs alongside the browser sweep; it does not replace the
  // api-latency gate above, which certifies a build with deeper sampling.
  if (enabled("GOATOS_DASHBOARD_API_CONTRACTS", isProductionSmoke)) {
    layer("api-contracts", "deterministic", () => runApiContracts(outDir));
  }
  if (runCertificationExtras && enabled("GOATOS_DASHBOARD_LIGHTHOUSE", false)) {
    layer("lighthouse", "deterministic", () => runLighthouse(outDir));
  }
  if (runCertificationExtras && enabled("GOATOS_DASHBOARD_GRAFANA_SMOKE", false)) {
    layer("grafana-smoke", "deterministic", () => runNode(["tools/deploy/smoke-stg-grafana-dashboards.mjs"]));
  }
  if (runCertificationExtras && enabled("GOATOS_DASHBOARD_VACCINATION_LIFECYCLE", true)) {
    layer("vaccination-lifecycle", "deterministic", () => runVaccinationLifecycleTests());
  }
  if (mode === "post-main-certification") {
    // Coverage must stay self-updating: new user-visible admin-web commits and assertions whose
    // selectors were renamed away are reported here, not discovered months later.
    layer("coverage-sync", "deterministic", () => runNode(["tools/dashboard-automation/sync-coverage.mjs", "--check"]));
    layer("postgresql-integration", "deterministic", () => assertPostgresIntegrationConfigured());
    const playwrightOk = layer("playwright-module-journeys", "deterministic", () => runPreviewPlaywright(outDir));
    receipt.runtimePolicy.browserSmoke = playwrightOk
      ? (receipt.runtimePolicy.dataTrust === "degraded" ? "ran_degraded" : "ran_certified")
      : "ran_failed";
    // Lane 4: write-path journeys on the writable OCI clone. Default OFF, nightly and on demand
    // only, and never in production-smoke. Production and STG are read-only for this automation.
    if (enabled("GOATOS_DASHBOARD_WRITE_JOURNEYS", false)) {
      layer("write-journeys", "deterministic", () => runWriteJourneys(outDir));
    }
  } else if (mode === "write-journeys") {
    if (!enabled("GOATOS_DASHBOARD_WRITE_JOURNEYS", false)) {
      throw new Error("write journeys are off by default; set GOATOS_DASHBOARD_WRITE_JOURNEYS=1 for a nightly or on-demand run");
    }
    const writeOk = layer("write-journeys", "deterministic", () => runWriteJourneys(outDir));
    receipt.runtimePolicy.browserSmoke = writeOk ? "ran_certified" : "ran_failed";
  } else if (mode === "production-smoke") {
    const productionSmokeOk = layer("production-module-journeys", "deterministic", () => runProductionSmoke(outDir));
    if (productionSmokeOk) {
      receipt.runtimePolicy.browserSmoke = receipt.runtimePolicy.dataTrust === "degraded" ? "ran_degraded" : "ran_certified";
    } else {
      receipt.runtimePolicy.browserSmoke = "ran_failed";
    }
  } else {
    throw new Error(`unknown mode: ${mode}`);
  }
} catch (error) {
  const message = redactText(error?.message ?? String(error));
  receipt.fatalError = message;
  receipt.blockers.push({ layer: "runner", message });
} finally {
  receipt.finishedAt = new Date().toISOString();
  receipt.status = computeStatus();
  const receiptPath = path.join(outDir, "receipt.json");
  writeReceipt(receiptPath, receipt);
  if (receipt.status === "degraded" && onlyDegradablePrerequisitesFailed()) {
    receipt.agentReview = {
      status: "skipped",
      reason: "degraded production smoke only failed parity prerequisites; no code-patchable product failure to review"
    };
  } else {
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
  }
  writeReceipt(receiptPath, receipt);
  if (receipt.status === "fail" && enabled("GOATOS_DASHBOARD_SELF_HEALING", config.selfHealing.enabledByDefault)) {
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

function computeStatus() {
  if (receipt.fatalError) return "fail";
  if (receipt.layers.length === 0) return "fail";
  if (receipt.layers.every((item) => item.status === "pass")) return "pass";
  if (mode === "production-smoke" && receipt.runtimePolicy.browserSmoke === "ran_degraded" && productionBrowserSmokePassed() && onlyDegradablePrerequisitesFailed()) {
    return "degraded";
  }
  return "fail";
}

function productionBrowserSmokePassed() {
  return receipt.layers.some((layer) => layer.name === "production-module-journeys" && layer.status === "pass");
}

function onlyDegradablePrerequisitesFailed() {
  const allowed = new Set(["latest-full-parity-receipt", "business-data-parity"]);
  return receipt.layers
    .filter((layer) => layer.status !== "pass")
    .every((layer) => allowed.has(layer.name));
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

// Lane 4 write-path journeys. The layer refuses to start before the target database has been
// classified, so a production or STG DSN fails here rather than inside the journey runner.
function runWriteJourneys(targetDir) {
  if (mode === "production-smoke") throw new Error("write journeys never run in the production smoke; production is read-only for this automation");
  const databaseUrl = process.env.GOATOS_WRITE_JOURNEY_DATABASE_URL;
  if (!databaseUrl) throw new Error("GOATOS_WRITE_JOURNEY_DATABASE_URL is required; write journeys run only against the OCI clone or a disposable preview database");
  const verdict = classifyWriteTarget(databaseUrl, process.env);
  if (!verdict.allowed) throw new Error(`write journeys refuse ${verdict.target}: ${verdict.reason}`);
  const output = path.join(targetDir, "write-journeys", "write-journeys-receipt.json");
  receipt.artifacts.push({ kind: "write-journeys-report", target: verdict.target, targetKind: verdict.kind, path: output });
  try {
    runNode(["tools/dashboard-automation/run-write-journeys.mjs", "--out", output]);
  } catch (error) {
    const message = redactText(error?.message ?? String(error));
    // Exit 3 is the runner saying nothing was attempted: no browser and no stand-in, so no
    // journey could be carried out. That is a gap in cover, not the site being broken, and the
    // layer message is quoted into the receipt and reaches Slack — so it must already be the
    // sentence a farm manager reads, and it must not accuse the site of anything.
    if (/failed with exit 3\b/.test(message)) {
      throw new Error("None of the checks for things people do on the site could be carried out this time, so nothing is known about them either way. Nothing here says the site is working or that it is broken.");
    }
    throw new Error(writeJourneySentence(output) ?? message);
  }
}

// "2 checks on the site failed, on the vaccination plan and the tasks board." A place and a
// problem, never a journey name and never an exit code.
function writeJourneySentence(reportPath) {
  try {
    const report = JSON.parse(readFileSync(reportPath, "utf8"));
    const failed = (report.journeys ?? []).filter((journey) => journey.status === "fail");
    if (!failed.length) return null;
    const pages = [...new Set(failed.map((journey) => journey.page).filter(Boolean))];
    return `${failed.length} check${failed.length === 1 ? "" : "s"} on the site failed, on ${pages.slice(0, 3).join(", ")}` +
      `${pages.length > 3 ? ` and ${pages.length - 3} other screen(s)` : ""}. Each one has its own screenshot in the thread.`;
  } catch {
    return null;
  }
}

function runApiLatency(targetDir) {
  const manifests = readdirSync(path.join(repo, "tools/perf"))
    .filter((name) => /^hot-paths\..*\.json$/.test(name))
    .sort();
  if (manifests.length === 0) throw new Error("no API latency hot-path manifests found under tools/perf");
  const requireExactApiSha = mode === "post-main-certification" || enabled("GOATOS_DASHBOARD_REQUIRE_API_SHA", false);
  for (const manifestName of manifests) {
    const output = path.join(targetDir, `api-latency-${manifestName.replaceAll(/[^a-zA-Z0-9._-]/g, "_")}`);
    const apiLatencyArgs = [
      "tools/perf/api-latency-gate.mjs",
      "--manifest",
      path.join("tools/perf", manifestName),
      "--output",
      output
    ];
    if (requireExactApiSha) {
      apiLatencyArgs.push("--expected-sha", receipt.repoSha);
    } else {
      apiLatencyArgs.push("--allow-deployed-build", "1");
    }
    runNode(apiLatencyArgs);
    receipt.artifacts.push({ kind: "api-latency-report", manifest: path.join("tools/perf", manifestName), path: output });
  }
}

function runAndroidJourneys(targetDir) {
  const output = path.join(targetDir, "android-journeys", "android-journeys-receipt.json");
  receipt.artifacts.push({ kind: "android-journeys-report", path: output });
  try {
    runNode([
      "tools/dashboard-automation/run-android-journeys.mjs",
      "--out", output,
      "--out-dir", path.join(targetDir, "android-journeys")
    ]);
  } catch (error) {
    // Exit 2 is the free-tier guard refusing to submit. That is the runner working, not the
    // farm having a problem, so it is reported as a parked run rather than as a phone failure.
    const message = redactText(error?.message ?? String(error));
    if (/REFUSED|free tier/i.test(message)) {
      throw new Error("The phone checks did not run today because the free daily allowance of test devices is used up. No money was spent. They run again tomorrow.");
    }
    // The layer message is quoted into the receipt and reaches Slack, so it must already
    // be the sentence a farm manager reads, not the runner's stderr.
    throw new Error(androidJourneySentence(output) ?? message);
  }
}

// "2 of the phone checks failed: the proof upload screen, and signing in." Reads as a
// place and a problem; never a journey name, a class or an exit code.
function androidJourneySentence(reportPath) {
  try {
    const report = JSON.parse(readFileSync(reportPath, "utf8"));
    const failed = (report.journeys ?? []).filter((journey) => journey.outcome === "fail");
    if (!failed.length) return null;
    const screens = [...new Set(failed.map((journey) => journey.screen).filter(Boolean))];
    return `${failed.length} check${failed.length === 1 ? "" : "s"} on the phone failed, on ${screens.slice(0, 3).join(", ")}` +
      `${screens.length > 3 ? ` and ${screens.length - 3} other screen(s)` : ""}. Each one has its own screenshot in the thread.`;
  } catch {
    return null;
  }
}

function runDataSanity(targetDir) {
  const output = path.join(targetDir, "data-sanity.json");
  receipt.artifacts.push({ kind: "data-sanity-report", path: output });
  try {
    runNode(["tools/dashboard-automation/check-data-sanity.mjs", "--out", output]);
  } catch (error) {
    // The layer message is quoted into the receipt and can reach Slack, so it must already be
    // the sentence a farm manager reads. The runner writes that sentence into its own report.
    throw new Error(dataSanitySentence(output) ?? redactText(error?.message ?? String(error)));
  }
}

function dataSanitySentence(reportPath) {
  try {
    const report = JSON.parse(readFileSync(reportPath, "utf8"));
    const sentence = String(report.slackLayerMessage ?? "").trim();
    return sentence.length > 0 ? sentence : null;
  } catch {
    return null;
  }
}

function runApiContracts(targetDir) {
  const required = ["GOATOS_API_BASE_URL", "GOATOS_BEARER_TOKEN", "GOATOS_TENANT_ID"];
  const missing = required.filter((key) => !process.env[key]);
  if (missing.length) throw new Error(`auth_blocked: missing api contract env: ${missing.join(", ")}`);
  // The sweep re-checks the resolved URL of every request itself; this is the outer gate.
  const apiUrl = new URL(process.env.GOATOS_API_BASE_URL);
  if (apiUrl.protocol !== "https:" || !["api.goatos.mesha.sg", "api.mesha.sg", "goatos-api.mesha.sg"].includes(apiUrl.hostname)) {
    throw new Error(`api contract sweep refuses non-production API URL: ${apiUrl.origin}`);
  }
  // notify-slack.mjs reads this file from the receipt's own directory.
  const output = path.join(targetDir, "api-contracts.json");
  try {
    runNode(["tools/dashboard-automation/check-api-contracts.mjs", "--out", output]);
  } finally {
    // Record the artifact even when the sweep failed: the findings ARE the artifact.
    receipt.artifacts.push({ kind: "api-contracts-report", path: output });
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
  runInDir(path.join(repo, "backend"), "go", [
    "test",
    "./internal/vaccination/app",
    "-run",
    "TestGenerateForVersionUsesTopLevelProcurementFirstWaveWhenPurposeBlank|TestGenerateForVersionUsesProcurementPurposePlans|TestGenerateForVersionDefersDuringWarmupHold|TestGenerateForVersionTreatsTerminalAndClinicalStatesDifferently",
    "-count=1"
  ]);
  runInDir(path.join(repo, "backend"), "go", [
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
  return runInDir(repo, command, args, env);
}

function runInDir(cwd, command, args, env = process.env) {
  const result = spawnSync(command, args, { cwd, env, encoding: "utf8" });
  const stdout = redactText(result.stdout);
  const stderr = redactText(result.stderr);
  if (stdout.trim()) console.log(stdout.trim());
  if (stderr.trim()) console.error(stderr.trim());
  if (result.status !== 0) {
    const detail = truncateRunOutput([stderr, stdout].filter(Boolean).join("\n"));
    throw new Error(`${command} ${args.join(" ")} failed with exit ${result.status}${detail ? `\n${detail}` : ""}`);
  }
}

function truncateRunOutput(value, max = 1800) {
  const text = String(value ?? "").trim();
  if (text.length <= max) return text;
  return `…${text.slice(-max)}`;
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

function runtimePolicyForMode(value) {
  if (value === "write-journeys") {
    return {
      dataParityRequiredBeforeBrowser: false,
      browserSmoke: "pending",
      dataTrust: "not_checked_write_clone",
      reason: "write-path journeys run against the writable OCI clone and restore what they touch; STG-to-OCI parity is never a gate for them"
    };
  }
  if (value === "production-smoke") {
    return {
      dataParityRequiredBeforeBrowser: false,
      browserSmoke: "pending",
      dataTrust: "pending",
      reason: "production read-only Playwright smoke must still collect UI/WebView evidence when OCI parity is degraded"
    };
  }
  return {
    dataParityRequiredBeforeBrowser: false,
    browserSmoke: "pending",
    dataTrust: "pending",
    reason: "post-main certification must still collect read-only browser evidence when OCI parity is degraded; full success still requires fresh STG-to-OCI parity"
  };
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
  if (config.businessDataParity.enabledByDefault !== false) throw new Error("self-test: live STG-to-OCI equality must be explicit-only because STG continuously moves");
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
  const runnerSource = readFileSync(fileURLToPath(import.meta.url), "utf8");
  if (!runnerSource.includes("dataParityRequiredBeforeBrowser: false") || !runnerSource.includes("ran_degraded") || !runnerSource.includes("productionSmokeOk")) {
    throw new Error("self-test: production smoke must mark degraded browser smoke only after Playwright actually runs");
  }
  if (!runnerSource.includes("degraded production smoke only failed parity prerequisites") || !runnerSource.includes('receipt.status === "fail" && enabled("GOATOS_DASHBOARD_SELF_HEALING"')) {
    throw new Error("self-test: degraded parity-only browser smoke must not spend agent/self-heal PR budget");
  }
  if (!runnerSource.includes("not_checked_read_only_smoke")) {
    throw new Error("self-test: production smoke must not wait for STG-to-OCI parity");
  }
  if (!runnerSource.includes("post-main certification must still collect read-only browser evidence")) {
    throw new Error("self-test: post-main certification must not skip browser smoke only because parity is degraded");
  }
  if (!runnerSource.includes("GOATOS_DASHBOARD_REQUIRE_API_SHA") || !runnerSource.includes("--allow-deployed-build")) {
    throw new Error("self-test: production API latency must test deployed prod without requiring latest main SHA unless explicitly requested");
  }
  if (!runnerSource.includes("GOATOS_DASHBOARD_CERTIFICATION_EXTRAS") || !runnerSource.includes('mode !== "production-smoke" || enabled("GOATOS_DASHBOARD_CERTIFICATION_EXTRAS", false)')) {
    throw new Error("self-test: production smoke must keep broad certification extras explicit-only");
  }
  if (!readFileSync(fileURLToPath(import.meta.url), "utf8").includes("runVaccinationLifecycleTests")) {
    throw new Error("self-test: runner must invoke deep vaccination lifecycle tests");
  }
  if (!runnerSource.includes('runInDir(path.join(repo, "backend"), "go"')) {
    throw new Error("self-test: backend Go lifecycle tests must run from the backend module");
  }
  if (!runnerSource.includes('layer("api-contracts"') || !runnerSource.includes("check-api-contracts.mjs")) {
    throw new Error("self-test: production smoke must run the read-only API contract sweep alongside the browser sweep");
  }
  if (!runnerSource.includes('enabled("GOATOS_DASHBOARD_API_CONTRACTS", isProductionSmoke)')) {
    throw new Error("self-test: the API contract sweep must be default-on for the daily production smoke");
  }
  if (!runnerSource.includes("api contract sweep refuses non-production API URL")) {
    throw new Error("self-test: the API contract layer must refuse a non-production API host before it runs");
  }
  if (!runnerSource.includes('layer("api-latency"')) {
    throw new Error("self-test: the API contract sweep must not have replaced the api-latency build gate");
  }
  for (const key of ["GOATOS_DASHBOARD_DATA_PARITY", "GOATOS_DASHBOARD_CERTIFICATION_EXTRAS", "GOATOS_DASHBOARD_API_LATENCY", "GOATOS_DASHBOARD_API_CONTRACTS", "GOATOS_DASHBOARD_LIGHTHOUSE", "GOATOS_DASHBOARD_GRAFANA_SMOKE", "GOATOS_DASHBOARD_VACCINATION_LIFECYCLE"]) {
    if (!readFileSync(fileURLToPath(import.meta.url), "utf8").includes(key)) {
      throw new Error(`self-test: runner no longer wires ${key}`);
    }
  }
  if (!runnerSource.includes('layer("coverage-sync"') || !runnerSource.includes("sync-coverage.mjs")) {
    throw new Error("self-test: post-main certification must run the coverage-sync layer so new commits cannot fall out of smoke coverage");
  }
  if (!runnerSource.includes('layer("data-sanity"') || !runnerSource.includes("check-data-sanity.mjs")) {
    throw new Error("self-test: the runner must run the read-only data sanity layer against the production replica");
  }
  if (!runnerSource.includes('enabled("GOATOS_DASHBOARD_DATA_SANITY", true)')) {
    throw new Error("self-test: read-only data sanity must stay on by default; it is seconds of pure reads");
  }
  if (!runnerSource.includes("dataSanitySentence")) {
    throw new Error("self-test: a failing data sanity layer must report the plain-English sentence, not a command line");
  }
  if (!runnerSource.includes('kind: "data-sanity-report"')) {
    throw new Error("self-test: the data sanity report must be an artifact on the receipt so Slack can find it");
  }
  if (new Set(["latest-full-parity-receipt", "business-data-parity"]).has("data-sanity")) {
    throw new Error("self-test: data sanity must never be treated as a degradable prerequisite");
  }
  if (!runnerSource.includes('layer("android-journeys"') || !runnerSource.includes("run-android-journeys.mjs")) {
    throw new Error("self-test: the Android farm journeys layer must stay wired into the runner");
  }
  if (!runnerSource.includes('enabled("GOATOS_DASHBOARD_ANDROID_JOURNEYS", false)')) {
    throw new Error("self-test: the Android journeys layer must stay DEFAULT OFF; the Test Lab free tier is 10 tests a day for the whole project");
  }
  if (!runnerSource.includes('kind: "android-journeys-report"')) {
    throw new Error("self-test: an Android journeys run must leave its receipt as an artifact so Slack can carry the phone screens");
  }
  if (!runnerSource.includes("No money was spent")) {
    throw new Error("self-test: a run parked by the free-tier guard must say plainly that nothing was spent, not read as a product failure");
  }
  // The guard itself must exist in the Android runner, not merely be described in a doc.
  {
    const androidRunner = readFileSync(path.join(repo, "tools/dashboard-automation/run-android-journeys.mjs"), "utf8");
    if (!androidRunner.includes("export function freeTierVerdict")) {
      throw new Error("self-test: the Test Lab free-tier guard must exist in code, in run-android-journeys.mjs");
    }
    if (!/virtualTestsPerDay:\s*10/.test(androidRunner) || !/virtualDeviceMinutesPerDay:\s*60/.test(androidRunner)) {
      throw new Error("self-test: the free-tier numbers (10 tests, 60 device-minutes a day) must stay pinned in the guard");
    }
    // The word "physical" may appear in the block as a comment saying there is none.
    // What may never appear is a physical KEY WITH A NUMBER, which reads as an allowance.
    const freeTierBlock = androidRunner.slice(androidRunner.indexOf("export const FREE_TIER"), androidRunner.indexOf("export const PAID_RATES"));
    if (/physical\w*\s*:\s*\d/i.test(freeTierBlock)) {
      throw new Error("self-test: no physical-device allowance may sit inside anything labelled free tier; the free tier grants none and a number there reads as permission to spend");
    }
  }
  runNode(["tools/dashboard-automation/check-data-sanity.mjs", "--self-test"]);
  if (modeFlags("write-journeys").parityIsNeverAGate !== true) {
    throw new Error("self-test: parity must never gate the write-path lane; it restores what it touched and proves it");
  }
  if (modeFlags("post-main-certification").parityIsNeverAGate !== false || modeFlags("production-smoke").parityIsNeverAGate !== true) {
    throw new Error("self-test: the pre-existing modes' parity behaviour must not have changed");
  }
  if (!runnerSource.includes('layer("write-journeys"') || !runnerSource.includes('enabled("GOATOS_DASHBOARD_WRITE_JOURNEYS", false)')) {
    throw new Error("self-test: write-path journeys must be a gated layer that is OFF by default");
  }
  const productionSmokeBranch = runnerSource.slice(
    runnerSource.indexOf('} else if (mode === "production-smoke") {'),
    runnerSource.indexOf('    throw new Error(`unknown mode: ${mode}`);')
  );
  if (productionSmokeBranch.includes("write-journeys")) {
    throw new Error("self-test: write-path journeys must never run in the production smoke");
  }
  if (!runnerSource.includes('write journeys never run in the production smoke')) {
    throw new Error("self-test: the write-journey layer must refuse production-smoke explicitly, not only by where it is called");
  }
  // This lane can never point at production or STG, whatever the environment says.
  for (const url of [
    "postgres://app:pw@10.20.30.40:5432/goatos",
    "postgres://app@api.goatos.mesha.sg:5432/goatos_dashboard_automation",
    "postgres://app@127.0.0.1:5455/goatos",
    "postgres://app@127.0.0.1:5432/goatos"
  ]) {
    if (classifyWriteTarget(url, { GOATOS_STG_READONLY_DATABASE_URL: "postgres://r@127.0.0.1:5455/goatos" }).allowed) {
      throw new Error("self-test: write journeys must never be pointable at production or STG");
    }
  }
  if (!classifyWriteTarget("postgres://p@127.0.0.1:5999/goatos_dashboard_automation_tmp", {}).allowed) {
    throw new Error("self-test: write journeys must still run against a disposable automation database");
  }
  runNode(["tools/dashboard-automation/run-write-journeys.mjs", "--self-test"]);
  runNode(["tools/dashboard-automation/run-android-journeys.mjs", "--self-test"]);
  runNode(["tools/dashboard-automation/check-module-journeys.mjs", "--self-test"]);
  runNode(["tools/dashboard-automation/run-module-journeys.mjs", "--self-test"]);
  runNode(["tools/dashboard-automation/self-heal-pr.mjs", "--self-test"]);
  runNode(["tools/dashboard-automation/notify-slack.mjs", "--self-test"]);
  runNode(["tools/dashboard-automation/refresh-firebase-token.mjs", "--self-test"]);
  runNode(["tools/dashboard-automation/check-latest-parity-receipt.mjs", "--self-test"]);
  runNode(["tools/dashboard-automation/check-api-contracts.mjs", "--self-test"]);
  console.log("dashboard automation runner: self-test passed");
}
