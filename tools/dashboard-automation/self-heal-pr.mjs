#!/usr/bin/env node
import { execFileSync } from "node:child_process";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, writeFileSync } from "node:fs";
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
const token = process.env.GITHUB_TOKEN?.trim();
if (!token) fail("GITHUB_TOKEN is required for dashboard self-healing PR creation");

const repoSlug = process.env.GOATOS_DASHBOARD_AUTOMATION_REPO || "vgoats/goatos";
const shortSha = String(receipt.repoSha || git(["rev-parse", "HEAD"])).slice(0, 12);
const stamp = new Date().toISOString().replaceAll(/[:.]/g, "-");
const branch = `automation/dashboard-smoke-${stamp}-${shortSha}`;
const reportRel = `docs/progress/dashboard-automation-failures/${stamp}-${shortSha}.md`;
const reportWorktree = mkdtempSync(path.join(tmpdir(), "goatos-dashboard-self-heal-"));
const reportPath = path.join(reportWorktree, reportRel);

git(["fetch", "--quiet", "origin", "main"]);
git(["worktree", "add", "--quiet", "-B", branch, reportWorktree, "origin/main"]);
mkdirSync(path.dirname(reportPath), { recursive: true });
writeFileSync(reportPath, failureReport(receipt, args.receipt, agentReview));
git(["add", reportRel], reportWorktree);
git(["commit", "-m", "automation: report dashboard smoke failure"], reportWorktree);
git(["push", "--set-upstream", "origin", branch], reportWorktree);

const pr = await githubJson(`/repos/${repoSlug}/pulls`, {
  method: "POST",
  body: {
    title: `Dashboard automation failure: ${receipt.mode ?? "smoke"} ${shortSha}`,
    head: branch,
    base: "main",
    body: [
      "Automated dashboard smoke/parity failure report.",
      "",
      "This PR is report-only. It does not approve, merge, deploy, or write production/staging/OCI data.",
      "",
      `Receipt SHA: ${shortSha}`,
      `Report: ${reportRel}`,
      agentReview ? `Agent review: ${agentReview.status} / ${agentReview.reason} / ${agentReview.model ?? "unknown model"}` : "Agent review: not found beside receipt",
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

function failureReport(value, receiptPath, agentReview = null) {
  const blockers = (value.blockers ?? []).slice(0, 25);
  const layers = (value.layers ?? []).map((layer) => `- ${layer.name}: ${layer.status}${layer.message ? ` — ${redactText(layer.message)}` : ""}`).join("\n");
  const agentLines = renderAgentReview(agentReview);
  return `${[
    "# Dashboard automation failure",
    "",
    `Generated: ${new Date().toISOString()}`,
    `Mode: ${value.mode ?? "unknown"}`,
    `Repo SHA: ${value.repoSha ?? "unknown"}`,
    `Receipt path: ${redactText(path.relative(repo, path.resolve(receiptPath)))}`,
    "",
    "## Layers",
    "",
    layers || "- none",
    "",
    "## Blockers",
    "",
    blockers.length ? blockers.map((item) => `- ${redactText(JSON.stringify(item))}`).join("\n") : "- none",
    "",
    "## Anthropic agent review",
    "",
    agentLines,
    "",
    "## Safety",
    "",
    "- Report-only PR.",
    "- No auto-merge.",
    "- No deploy.",
    "- No STG/production/OCI data writes.",
    ""
  ].join("\n")}\n`;
}

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

function renderAgentReview(review) {
  if (!review) return "- No `agent-review.json` was found beside the receipt.";
  const lines = [
    `- Status: ${redactText(review.status ?? "unknown")}`,
    `- Reason: ${redactText(review.reason ?? "unknown")}`,
    `- Model: ${redactText(review.model ?? "unknown")}`
  ];
  if (review.usage) lines.push(`- Usage: ${redactText(JSON.stringify(review.usage))}`);
  if (review.rawSummary) lines.push(`- Summary: ${redactText(review.rawSummary)}`);
  const findings = Array.isArray(review.findings) ? review.findings.slice(0, 10) : [];
  if (findings.length) {
    lines.push("");
    lines.push("### Findings");
    for (const finding of findings) {
      lines.push(`- [${redactText(finding.severity ?? "medium")}] ${redactText(finding.title ?? "Untitled")}: ${redactText(finding.detail ?? "")}`);
    }
  }
  if (review.remediation) {
    lines.push("");
    lines.push("### Remediation");
    lines.push("```json");
    lines.push(redactText(JSON.stringify(review.remediation, null, 2)).slice(0, 6000));
    lines.push("```");
  }
  return lines.join("\n");
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
  const report = failureReport(
    { mode: "production-smoke", repoSha: "abc123", blockers: [{ message: "Bearer secret-token" }] },
    "/tmp/receipt.json",
    { status: "completed", reason: "anthropic_review_completed", model: "claude-test", findings: [{ severity: "high", title: "x", detail: "token=secret" }] },
  );
  if (report.includes("secret-token")) throw new Error("self-test: report did not redact secret-like text");
  if (report.includes("token=secret")) throw new Error("self-test: agent finding did not redact secret-like text");
  const source = readFileSync(fileURLToPath(import.meta.url), "utf8");
  if (!source.includes("git([\"worktree\", \"add\"") || source.includes("checkout\", \"--quiet\", \"-B\"")) {
    throw new Error("self-test: self-healing must create its report branch in a separate worktree");
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
