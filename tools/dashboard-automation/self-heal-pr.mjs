#!/usr/bin/env node
import { execFileSync, spawnSync } from "node:child_process";
import { existsSync, mkdtempSync, readFileSync, unlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { redactText } from "./lib/redact.mjs";

const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const args = parseArgs(process.argv.slice(2));
const config = JSON.parse(readFileSync(path.join(repo, "tools/dashboard-automation/config.json"), "utf8"));

if (args.selfTest) {
  selfTest();
  process.exit(0);
}

if (!args.receipt) fail("usage: node tools/dashboard-automation/self-heal-pr.mjs --receipt <receipt.json>");
if (!enabled("GOATOS_DASHBOARD_SELF_HEALING", config.selfHealing.enabledByDefault)) {
  console.log("dashboard self-healing PR: skipped (disabled by GOATOS_DASHBOARD_SELF_HEALING/config)");
  process.exit(0);
}
const receipt = JSON.parse(readFileSync(args.receipt, "utf8"));
if (receipt.status === "pass") {
  console.log("dashboard self-healing PR: skipped (receipt passed)");
  process.exit(0);
}
if (config.selfHealing.mode !== "pull_request_only") fail("self-healing mode must stay pull_request_only");
const agentReview = readAgentReview(args.receipt);
const codePatchDecision = codePatchableFailure(receipt, agentReview);
if (!codePatchDecision.shouldOpen) {
  console.log(`dashboard self-healing PR: skipped (${codePatchDecision.reason})`);
  process.exit(0);
}
const patch = patchFromAgentReview(agentReview);
if (!patch.ok) {
  console.log(`dashboard self-healing PR: skipped (${patch.reason})`);
  process.exit(0);
}
const token = process.env.GITHUB_TOKEN?.trim();
if (!token) fail("GITHUB_TOKEN is required for dashboard self-healing PR creation");

const repoSlug = process.env.GOATOS_DASHBOARD_AUTOMATION_REPO || "vgoats/goatos";
const shortSha = String(receipt.repoSha || git(["rev-parse", "HEAD"])).slice(0, 12);
const stamp = new Date().toISOString().replaceAll(/[:.]/g, "-");
const branch = `automation/dashboard-smoke-${stamp}-${shortSha}`;
const fixWorktree = mkdtempSync(path.join(tmpdir(), "goatos-dashboard-self-heal-"));

git(["fetch", "--quiet", "origin", "main"]);
git(["worktree", "add", "--quiet", "-B", branch, fixWorktree, "origin/main"]);
applyAgentPatch(fixWorktree, patch.diff);
const changedFiles = git(["diff", "--name-only"], fixWorktree).split("\n").filter(Boolean);
const fileDecision = changedFilesAreSafeFixes(changedFiles);
if (!fileDecision.ok) fail(`refusing dashboard self-healing PR: ${fileDecision.reason}`);
runAgentTests(fixWorktree, agentReview);
git(["add", "--", ...changedFiles], fixWorktree);
git(["commit", "-m", "fix: repair dashboard automation failure"], fixWorktree);
git(["push", "--set-upstream", "origin", branch], fixWorktree);

const pr = await githubJson(`/repos/${repoSlug}/pulls`, {
  method: "POST",
  body: {
    title: `Fix dashboard automation failure: ${receipt.mode ?? "smoke"} ${shortSha}`,
    head: branch,
    base: "main",
    body: [
      "Automated dashboard self-healing fix proposal.",
      "",
      "This PR contains code/test changes only. It does not approve, merge, deploy, or write production/staging/OCI data.",
      "",
      `Receipt SHA: ${shortSha}`,
      `Changed files: ${changedFiles.map((file) => `\`${file}\``).join(", ")}`,
      agentReview ? `Agent review: ${agentReview.status} / ${agentReview.reason} / ${agentReview.model ?? "unknown model"}` : "Agent review: not found beside receipt",
      `Summary: ${redactText(agentReview?.rawSummary ?? agentReview?.remediation?.immediate_next_step ?? "agent supplied a concrete patch")}`,
      "",
      "Requested reviewers: @raviteja786143 @manohar-mesha"
    ].join("\n")
  }
});

await githubJson(`/repos/${repoSlug}/pulls/${pr.number}/requested_reviewers`, {
  method: "POST",
  body: { reviewers: config.selfHealing.reviewers ?? [] },
  tolerateStatuses: [201, 422]
});

console.log(`dashboard self-healing PR: opened ${pr.html_url}`);

function readAgentReview(receiptPath) {
  const p = path.join(path.dirname(path.resolve(receiptPath)), "agent-review.json");
  if (!existsSync(p)) return null;
  return JSON.parse(readFileSync(p, "utf8"));
}

function codePatchableFailure(value, agentReview = null) {
  const failedLayers = (value.layers ?? [])
    .filter((layer) => layer.status !== "pass")
    .map((layer) => String(layer.name ?? ""));
  const blockerText = JSON.stringify(value.blockers ?? []);
  const preconditionLayers = new Set([
    "latest-full-parity-receipt",
    "business-data-parity",
    "oci-free-preflight",
    "postgresql-integration"
  ]);
  if (failedLayers.length > 0 && failedLayers.every((name) => preconditionLayers.has(name))) {
    return { shouldOpen: false, reason: `precondition failure (${failedLayers.join(", ")})` };
  }
  if (/auth_blocked|missing .*env|credential|token|latest-full-parity-receipt|READBACK_PASS|parity receipt/i.test(blockerText)) {
    return { shouldOpen: false, reason: "auth/env/parity prerequisite failure" };
  }
  if (!agentReview) {
    return { shouldOpen: false, reason: "agent review missing; refusing report-only PR" };
  }
  if (agentReview.shouldOpenFixPr !== true || agentReview.safeToPatchCode !== true) {
    return { shouldOpen: false, reason: "agent review marked not code-patchable" };
  }
  const patchableClasses = new Set(["product_ui", "product_api", "latency"]);
  if (agentReview?.failureClass && !patchableClasses.has(agentReview.failureClass)) {
    return { shouldOpen: false, reason: `agent failure class ${agentReview.failureClass}` };
  }
  return { shouldOpen: true, reason: "code-patchable failure" };
}

function patchFromAgentReview(review) {
  const diff = review?.remediation?.unified_diff ?? review?.remediation?.patch ?? review?.patch;
  if (typeof diff !== "string" || diff.trim() === "" || diff.trim() === "null") {
    return { ok: false, reason: "agent review did not provide a concrete unified diff" };
  }
  if (!/^diff --git /m.test(diff) && !/^--- a\//m.test(diff)) {
    return { ok: false, reason: "agent review patch is not a unified git diff" };
  }
  if (containsUnredactedSecret(diff)) fail("refusing dashboard self-healing patch that appears to contain an unredacted secret");
  return { ok: true, diff };
}

function applyAgentPatch(cwd, diff) {
  const patchPath = path.join(cwd, ".dashboard-self-heal.patch");
  writeFileSync(patchPath, diff);
  try {
    runChecked("git", ["apply", "--check", patchPath], cwd);
    runChecked("git", ["apply", patchPath], cwd);
  } finally {
    try {
      unlinkSync(patchPath);
    } catch {}
  }
}

function changedFilesAreSafeFixes(files) {
  if (files.length === 0) return { ok: false, reason: "agent patch produced no file changes" };
  const docsOnly = files.every((file) => file.endsWith(".md") || file.startsWith("docs/progress/"));
  if (docsOnly) return { ok: false, reason: "agent patch is docs/report-only" };
  const forbidden = files.find((file) =>
    file.startsWith(".github/workflows/") ||
    file.startsWith("docs/progress/dashboard-automation-failures/") ||
    file.includes("/secrets/") ||
    file.endsWith(".env") ||
    file.includes(".env.")
  );
  if (forbidden) return { ok: false, reason: `agent patch touched forbidden path ${forbidden}` };
  return { ok: true };
}

function runAgentTests(cwd, review) {
  const commands = Array.isArray(review?.remediation?.tests_to_run) ? review.remediation.tests_to_run.slice(0, 5) : [];
  const allowedCommands = new Set(["go", "make", "node", "npm", process.execPath]);
  const safeCommands = commands.filter((command) => {
    if (typeof command !== "string" || !command.trim() || /[;&|`$<>]/.test(command)) return false;
    const commandName = command.trim().split(/\s+/)[0];
    return allowedCommands.has(commandName);
  });
  if (safeCommands.length === 0) {
    runChecked(process.execPath, ["tools/dashboard-automation/run.mjs", "--self-test"], cwd);
    return;
  }
  for (const command of safeCommands) {
    const parts = command.trim().split(/\s+/);
    runChecked(parts[0], parts.slice(1), cwd);
  }
}

async function githubJson(apiPath, options) {
  const response = await fetch(`https://api.github.com${apiPath}`, {
    method: options.method,
    headers: {
      "Accept": "application/vnd.github+json",
      "Authorization": `Bearer ${token}`,
      "Content-Type": "application/json",
      "X-GitHub-Api-Version": "2022-11-28"
    },
    body: JSON.stringify(options.body)
  });
  if (!response.ok && !(options.tolerateStatuses ?? []).includes(response.status)) {
    const body = redactText(await response.text());
    throw new Error(`GitHub ${apiPath} HTTP ${response.status}: ${body}`);
  }
  return response.status === 204 ? {} : response.json();
}

function git(args, cwd = repo) {
  return execFileSync("git", args, { cwd, encoding: "utf8" }).trim();
}

function runChecked(command, commandArgs, cwd) {
  const result = spawnSync(command, commandArgs, { cwd, encoding: "utf8" });
  if (result.status !== 0) {
    throw new Error(`${command} ${commandArgs.join(" ")} failed: ${redactText(result.stderr || result.stdout || "")}`);
  }
  return redactText(result.stdout ?? "").trim();
}

function parseArgs(raw) {
  const parsed = {};
  for (let i = 0; i < raw.length; i += 1) {
    const arg = raw[i];
    if (arg === "--self-test") parsed.selfTest = true;
    else if (arg === "--receipt") parsed.receipt = raw[++i];
    else fail(`unknown argument: ${arg}`);
  }
  return parsed;
}

function enabled(envName, defaultValue = false) {
  const value = process.env[envName];
  if (value == null || value === "") return Boolean(defaultValue);
  return !["0", "false", "no", "off"].includes(String(value).trim().toLowerCase());
}

function selfTest() {
  const original = process.env.GOATOS_DASHBOARD_SELF_HEALING;
  delete process.env.GOATOS_DASHBOARD_SELF_HEALING;
  if (!enabled("GOATOS_DASHBOARD_SELF_HEALING", true)) throw new Error("self-test: default-on self-healing should be enabled");
  process.env.GOATOS_DASHBOARD_SELF_HEALING = "0";
  if (enabled("GOATOS_DASHBOARD_SELF_HEALING", true)) throw new Error("self-test: explicit self-healing disable should win");
  if (original == null) delete process.env.GOATOS_DASHBOARD_SELF_HEALING;
  else process.env.GOATOS_DASHBOARD_SELF_HEALING = original;
  const source = readFileSync(fileURLToPath(import.meta.url), "utf8");
  if (!source.includes("git([\"worktree\", \"add\"") || source.includes("checkout\", \"--quiet\", \"-B\"")) {
    throw new Error("self-test: self-healing must create its fix branch in a separate worktree");
  }
  const parityDecision = codePatchableFailure({
    layers: [{ name: "latest-full-parity-receipt", status: "fail" }],
    blockers: [{ layer: "latest-full-parity-receipt", message: "READBACK_PASS receipt missing" }]
  });
  if (parityDecision.shouldOpen) throw new Error("self-test: parity receipt precondition must not open a PR");
  const productDecision = codePatchableFailure({
    layers: [{ name: "production-module-journeys", status: "fail" }],
    blockers: [{ layer: "production-module-journeys", message: "The board could not be loaded" }]
  }, { failureClass: "product_ui", shouldOpenFixPr: true, safeToPatchCode: true });
  if (!productDecision.shouldOpen) throw new Error("self-test: product UI failures must remain eligible for PR creation");
  const missingPatch = patchFromAgentReview({ remediation: { immediate_next_step: "fix route", unified_diff: null } });
  if (missingPatch.ok) throw new Error("self-test: missing patch must not open a PR");
  const docsOnly = changedFilesAreSafeFixes(["docs/progress/dashboard-automation-failures/example.md"]);
  if (docsOnly.ok) throw new Error("self-test: docs/report-only changes must not open a PR");
  const codeFiles = changedFilesAreSafeFixes(["apps/admin-web/features/weighing/example.tsx", "apps/admin-web/scripts/example.test.mjs"]);
  if (!codeFiles.ok) throw new Error("self-test: code/test changes must remain eligible");
  const missingReview = codePatchableFailure({
    layers: [{ name: "production-module-journeys", status: "fail" }],
    blockers: [{ layer: "production-module-journeys", message: "The board could not be loaded" }]
  });
  if (missingReview.shouldOpen) throw new Error("self-test: missing agent review must not open report-only PRs");
  const agentVeto = codePatchableFailure({
    layers: [{ name: "production-module-journeys", status: "fail" }],
    blockers: [{ layer: "production-module-journeys", message: "manual data repair needed" }]
  }, { failureClass: "business_data_parity", shouldOpenFixPr: false, safeToPatchCode: false });
  if (agentVeto.shouldOpen) throw new Error("self-test: agent non-code failures must not open a PR");
  console.log("dashboard self-healing PR: self-test passed");
}

function fail(message) {
  console.error(message);
  process.exit(1);
}
