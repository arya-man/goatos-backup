#!/usr/bin/env node
import { existsSync, readFileSync, writeFileSync } from "node:fs";
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

const receiptPath = args.receipt;
if (!receiptPath) fail("usage: node tools/dashboard-automation/agent-review.mjs --receipt <receipt.json> [--out <review.json>]");

const receipt = JSON.parse(readFileSync(receiptPath, "utf8"));
const outPath = args.out ?? path.join(path.dirname(receiptPath), "agent-review.json");
const agentConfig = config.agentReview;
const hasKey = Boolean(process.env.ANTHROPIC_API_KEY || process.env.OPENAI_API_KEY);
const deterministicFailed = receipt.layers?.some((layer) => layer.authority === "deterministic" && layer.status !== "pass");
const evidenceItems = evidenceForAgent(receipt);

const review = {
  status: "skipped",
  reason: null,
  advisoryOnly: true,
  budget: {
    perRunUsd: agentConfig.perRunBudgetUsd,
    monthlyUsd: agentConfig.monthlyBudgetUsd,
    warningPercent: agentConfig.budgetWarningPercent,
    hardStopPercent: agentConfig.budgetHardStopPercent
  },
  sample: {
    passingSamplePercent: deterministicFailed ? 0 : agentConfig.passingSamplePercent,
    evidenceItems: evidenceItems.length
  },
  findings: []
};

if (!agentConfig.enabledByDefault && process.env.GOATOS_DASHBOARD_AGENT_REVIEW !== "1") {
  review.reason = "agent_review_disabled";
} else if (!hasKey) {
  review.reason = "agent_api_key_missing";
} else if (evidenceItems.length === 0) {
  review.status = "pass";
  review.reason = "no_agent_evidence_required";
} else {
  review.status = "manual_queue";
  review.reason = "api_call_not_enabled_in_scaffold";
  review.selfHealing = {
    mode: config.selfHealing.mode,
    enabled: config.selfHealing.enabledByDefault || process.env.GOATOS_DASHBOARD_SELF_HEALING === "1",
    reviewers: config.selfHealing.reviewers,
    requiredHumanApproval: true,
    forbiddenActions: config.selfHealing.forbiddenActions
  };
}

writeFileSync(outPath, `${JSON.stringify(review, null, 2)}\n`);
console.log(`dashboard agent review hook: ${review.status} (${review.reason}); wrote ${path.relative(repo, outPath)}`);

function evidenceForAgent(receipt) {
  const items = [];
  for (const artifact of receipt.artifacts ?? []) {
    if (artifact.kind === "screenshot" || artifact.kind === "trace" || artifact.kind === "har") {
      items.push({
        kind: artifact.kind,
        path: redactText(artifact.path ?? ""),
        route: redactText(artifact.route ?? null),
        viewport: redactText(artifact.viewport ?? null)
      });
    }
  }
  return items;
}

function parseArgs(raw) {
  const parsed = {};
  for (let i = 0; i < raw.length; i += 1) {
    const arg = raw[i];
    if (arg === "--self-test") parsed.selfTest = true;
    else if (arg === "--receipt") parsed.receipt = raw[++i];
    else if (arg === "--out") parsed.out = raw[++i];
    else fail(`unknown argument: ${arg}`);
  }
  return parsed;
}

function selfTest() {
  const sample = {
    layers: [{ name: "playwright", authority: "deterministic", status: "fail" }],
    artifacts: [{ kind: "screenshot", path: "https://x.test/a.png?token=secret", route: "/work-board", viewport: "mobile" }]
  };
  const items = evidenceForAgent(sample);
  if (items[0].path.includes("secret")) throw new Error("self-test: evidence path was not redacted");
  if (containsUnredactedSecret(JSON.stringify(items))) throw new Error("self-test: evidence retained a secret-like value");
  if (config.selfHealing.mode !== "pull_request_only") throw new Error("self-test: self-healing must remain PR-only");
  console.log("dashboard agent review hook: self-test passed");
}

function fail(message) {
  console.error(message);
  process.exit(1);
}
