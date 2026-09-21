#!/usr/bin/env node
import { readFileSync, writeFileSync } from "node:fs";
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
const anthropicKey = process.env.ANTHROPIC_API_KEY?.trim();
const deterministicFailed = receipt.layers?.some((layer) => layer.authority === "deterministic" && layer.status !== "pass");
const evidenceItems = evidenceForAgent(receipt, args.receipt);

const review = {
  status: "skipped",
  reason: null,
  advisoryOnly: true,
  provider: "anthropic",
  model: null,
  usage: null,
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
  findings: [],
  remediation: null
};

if (!agentConfig.enabledByDefault && process.env.GOATOS_DASHBOARD_AGENT_REVIEW !== "1") {
  review.reason = "agent_review_disabled";
} else if (!anthropicKey) {
  review.reason = "agent_api_key_missing";
} else {
  const agent = await runAnthropicReview({ apiKey: anthropicKey, receipt, receiptPath: args.receipt, evidenceItems });
  Object.assign(review, agent);
  review.selfHealing = {
    mode: config.selfHealing.mode,
    enabled: config.selfHealing.enabledByDefault || process.env.GOATOS_DASHBOARD_SELF_HEALING === "1",
    reviewers: config.selfHealing.reviewers,
    requiredHumanApproval: true,
    forbiddenActions: config.selfHealing.forbiddenActions
  };
}

writeFileSync(outPath, `${JSON.stringify(review, null, 2)}\n`);
console.log(`dashboard agent review hook: ${review.status} (${review.reason}); model=${review.model ?? "none"} wrote ${path.relative(repo, outPath)}`);

function evidenceForAgent(receipt, receiptPath) {
  const items = [];
  items.push({
    kind: "receipt_summary",
    path: redactText(path.relative(repo, path.resolve(receiptPath))),
    mode: redactText(receipt.mode ?? "unknown"),
    repoSha: redactText(receipt.repoSha ?? "unknown"),
    productionUrl: redactText(receipt.productionUrl ?? receipt.dashboardUrl ?? "unknown"),
    status: redactText(receipt.status ?? "unknown")
  });
  for (const layer of receipt.layers ?? []) {
    items.push({
      kind: "layer",
      name: redactText(layer.name),
      authority: redactText(layer.authority),
      status: redactText(layer.status),
      message: redactText(layer.message ?? "")
    });
  }
  for (const blocker of receipt.blockers ?? []) {
    items.push({
      kind: "blocker",
      layer: redactText(blocker.layer ?? ""),
      message: redactText(blocker.message ?? JSON.stringify(blocker))
    });
  }
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

async function runAnthropicReview({ apiKey, receipt, receiptPath, evidenceItems }) {
  const model = await resolveAnthropicModel(apiKey);
  const prompt = buildPrompt({ receipt, receiptPath, evidenceItems });
  const body = {
    model,
    max_tokens: Number(process.env.GOATOS_DASHBOARD_AGENT_MAX_TOKENS ?? 1600),
    system: [
      "You are a production QA self-healing reviewer for GoAT OS.",
      "Return JSON only. Do not include secrets. Do not propose production, staging, or OCI data writes.",
      "If the blocker is data parity, say so and route to the parity repair workflow instead of inventing a product code fix.",
      "If the blocker is a product UI/API regression, propose tests and likely files, but keep every action pull-request-only and human-reviewed."
    ].join(" "),
    messages: [{ role: "user", content: prompt }]
  };
  const response = await fetch("https://api.anthropic.com/v1/messages", {
    method: "POST",
    headers: {
      "content-type": "application/json",
      "x-api-key": apiKey,
      "anthropic-version": "2023-06-01"
    },
    body: JSON.stringify(body)
  });
  const text = await response.text();
  if (!response.ok) {
    return {
      status: "blocked",
      reason: "anthropic_api_error",
      model,
      findings: [{ severity: "blocker", title: "Anthropic review API failed", detail: redactText(`HTTP ${response.status}: ${text.slice(0, 800)}`) }]
    };
  }
  const payload = JSON.parse(text);
  const outputText = (payload.content ?? []).map((part) => part.type === "text" ? part.text : "").join("\n").trim();
  const parsed = parseModelJson(outputText);
  if (containsUnredactedSecret(JSON.stringify(parsed))) {
    throw new Error("agent review refused to write unredacted secret-like model output");
  }
  return {
    status: "completed",
    reason: "anthropic_review_completed",
    model: payload.model ?? model,
    usage: payload.usage ?? null,
    findings: normalizeFindings(parsed.findings),
    remediation: parsed.remediation ?? parsed,
    rawSummary: redactText(parsed.summary ?? "")
  };
}

async function resolveAnthropicModel(apiKey) {
  const explicit = process.env.GOATOS_DASHBOARD_AGENT_MODEL?.trim();
  if (explicit) return explicit;
  const fallback = "claude-sonnet-4-5-20250929";
  try {
    const response = await fetch("https://api.anthropic.com/v1/models?limit=20", {
      headers: {
        "x-api-key": apiKey,
        "anthropic-version": "2023-06-01"
      }
    });
    if (!response.ok) return fallback;
    const payload = await response.json();
    const ids = (payload.data ?? []).map((item) => String(item.id ?? ""));
    return ids.find((id) => /sonnet/i.test(id)) ?? ids.find((id) => /opus/i.test(id)) ?? ids[0] ?? fallback;
  } catch {
    return fallback;
  }
}

function buildPrompt({ receipt, receiptPath, evidenceItems }) {
  const compactReceipt = {
    mode: receipt.mode,
    status: receipt.status,
    repoSha: receipt.repoSha,
    productionUrl: receipt.productionUrl ?? receipt.dashboardUrl,
    layers: receipt.layers,
    blockers: receipt.blockers,
    fatalError: receipt.fatalError
  };
  return JSON.stringify({
    task: "Review this dashboard automation failure and produce a PR-only self-healing diagnosis.",
    output_schema: {
      summary: "one sentence",
      failure_class: "business_data_parity | auth_blocker | product_ui | product_api | latency | infra | unknown",
      should_open_fix_pr: "boolean",
      safe_to_patch_code: "boolean",
      required_human_action: "string or null",
      findings: [{ severity: "blocker|high|medium|low", title: "short", detail: "specific evidence" }],
      remediation: {
        immediate_next_step: "specific next step",
        proposed_files: ["repo relative paths"],
        tests_to_run: ["commands"],
        forbidden_actions_acknowledged: ["no production data writes", "no staging data writes", "no OCI data writes", "no auto merge"]
      }
    },
    receipt_path: redactText(path.relative(repo, path.resolve(receiptPath))),
    receipt: compactReceipt,
    evidence: evidenceItems
  });
}

function parseModelJson(text) {
  const trimmed = text.trim();
  const json = trimmed.startsWith("{") ? trimmed : trimmed.match(/\{[\s\S]*\}/)?.[0];
  if (!json) {
    return {
      summary: "Model returned non-JSON output",
      findings: [{ severity: "medium", title: "Non-JSON model output", detail: redactText(trimmed.slice(0, 1000)) }]
    };
  }
  return JSON.parse(json);
}

function normalizeFindings(value) {
  if (!Array.isArray(value)) return [];
  return value.slice(0, 20).map((item) => ({
    severity: redactText(item?.severity ?? "medium"),
    title: redactText(item?.title ?? "Untitled finding"),
    detail: redactText(item?.detail ?? "")
  }));
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
    blockers: [{ layer: "playwright", message: "Bearer secret-token" }],
    artifacts: [{ kind: "screenshot", path: "https://x.test/a.png?token=secret", route: "/work-board", viewport: "mobile" }]
  };
  const items = evidenceForAgent(sample, "/tmp/receipt.json");
  if (JSON.stringify(items).includes("secret-token") || JSON.stringify(items).includes("token=secret")) throw new Error("self-test: evidence was not redacted");
  if (containsUnredactedSecret(JSON.stringify(items))) throw new Error("self-test: evidence retained a secret-like value");
  if (config.selfHealing.mode !== "pull_request_only") throw new Error("self-test: self-healing must remain PR-only");
  if (!config.selfHealing.forbiddenActions.includes("writeOciData")) throw new Error("self-test: OCI writes must remain forbidden");
  console.log("dashboard agent review hook: self-test passed");
}

function fail(message) {
  console.error(message);
  process.exit(1);
}
