import { spawnSync } from "node:child_process";
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { compareRoutes } from "./discover-admin-routes.mjs";
import { runPreflight } from "./preflight-oci-free.mjs";

const repoRoot = dirname(dirname(dirname(fileURLToPath(import.meta.url))));
const config = JSON.parse(readFileSync(join(repoRoot, "tools", "dashboard-automation", "config.json"), "utf8"));

const args = new Set(process.argv.slice(2));
const mode = readArg("--mode") ?? "production-smoke";
const dryRun = args.has("--dry-run");
const withAgent = args.has("--with-agent") || process.env.GOATOS_DASHBOARD_AGENT_REVIEW === "1";

const startedAt = new Date().toISOString();
const sha = run("git", ["rev-parse", "HEAD"], { allowFailure: false }).stdout.trim();
const receiptDir = join(repoRoot, config.receiptRoot, startedAt.replaceAll(/[:.]/g, "-"));
mkdirSync(receiptDir, { recursive: true });

const receipt = {
  version: 1,
  mode,
  dry_run: dryRun,
  started_at: startedAt,
  sha,
  deterministic_layers: {},
  agent_review: { requested: withAgent, status: "not_started" },
  safety: {},
  artifacts: { receipt_dir: receiptDir },
};

try {
  receipt.safety.oci_free_preflight = runPreflight({ requireOci: !dryRun });
  if (!receipt.safety.oci_free_preflight.ok) fail("oci_free_preflight_failed");

  const routeComparison = compareRoutes();
  receipt.deterministic_layers.route_inventory = {
    filesystem_routes: routeComparison.filesystemRoutes.length,
    smoke_routes: routeComparison.smokeRoutes.length,
    missing: routeComparison.missing,
  };
  if (routeComparison.missing.length > 0) fail("route_inventory_missing_coverage");

  if (dryRun) {
    receipt.deterministic_layers.playwright = { status: "skipped_dry_run" };
  } else {
    validateEnv(config.deterministicSmoke.requiredEnv);
    const smoke = run("node", [config.deterministicSmoke.adminWebScript], { cwd: join(repoRoot, "apps", "admin-web"), allowFailure: true });
    receipt.deterministic_layers.playwright = {
      status: smoke.status === 0 ? "pass" : "fail",
      exit_code: smoke.status,
      stdout_tail: redact(smoke.stdout).slice(-4000),
      stderr_tail: redact(smoke.stderr).slice(-4000),
    };
    if (smoke.status !== 0) fail("playwright_smoke_failed");
  }

  receipt.agent_review = buildAgentPlan({ withAgent, dryRun });
  if (withAgent && receipt.agent_review.status === "blocked_missing_key") fail("agent_requested_without_key");

  receipt.status = "pass";
} catch (error) {
  receipt.status = receipt.status ?? "fail";
  receipt.failure = receipt.failure ?? String(error.message || error);
} finally {
  receipt.finished_at = new Date().toISOString();
  writeFileSync(join(receiptDir, "receipt.json"), JSON.stringify(receipt, null, 2));
  console.log(JSON.stringify({ status: receipt.status, receipt: join(receiptDir, "receipt.json"), failure: receipt.failure ?? null }, null, 2));
  if (receipt.status !== "pass") process.exit(1);
}

function buildAgentPlan({ withAgent, dryRun }) {
  if (!withAgent) {
    return {
      requested: false,
      status: "skipped",
      reason: "agent review is opt-in until three deterministic green lifecycle runs exist",
    };
  }
  const envName = config.agentReview.providerEnv;
  if (!process.env[envName]) {
    return { requested: true, status: "blocked_missing_key", provider_env: envName };
  }
  return {
    requested: true,
    status: dryRun ? "planned_dry_run" : "ready_not_called_by_repo_runner",
    provider_env: envName,
    per_run_usd_cap: config.agentReview.perRunUsdCap,
    monthly_usd_cap: config.agentReview.monthlyUsdCap,
    passing_sample_percent: config.agentReview.passingSamplePercent,
    note: "repo runner records budget policy; model invocation stays in the OCI host adapter so secrets never enter Git receipts",
  };
}

function validateEnv(keys) {
  const missing = keys.filter((key) => !process.env[key]);
  if (missing.length > 0) {
    receipt.auth = { status: "auth_blocked", missing_env: missing };
    fail("auth_blocked");
  }
}

function readArg(name) {
  const index = process.argv.indexOf(name);
  if (index === -1) return null;
  return process.argv[index + 1] ?? null;
}

function run(command, runArgs, { cwd = repoRoot, allowFailure = false } = {}) {
  const result = spawnSync(command, runArgs, { cwd, encoding: "utf8", env: process.env });
  if (!allowFailure && result.status !== 0) {
    throw new Error(`${command} ${runArgs.join(" ")} failed: ${result.stderr || result.stdout}`);
  }
  return { status: result.status, stdout: result.stdout ?? "", stderr: result.stderr ?? "" };
}

function fail(reason) {
  receipt.status = "fail";
  receipt.failure = reason;
  throw new Error(reason);
}

function redact(text) {
  return String(text)
    .replaceAll(/Bearer\s+[A-Za-z0-9._~+/=-]+/g, "Bearer [REDACTED]")
    .replaceAll(/GOATOS_BEARER_TOKEN=[^\s]+/g, "GOATOS_BEARER_TOKEN=[REDACTED]")
    .replaceAll(/ANTHROPIC_API_KEY=[^\s]+/g, "ANTHROPIC_API_KEY=[REDACTED]")
    .replaceAll(/eyJ[A-Za-z0-9._-]+/g, "[JWT_REDACTED]");
}
