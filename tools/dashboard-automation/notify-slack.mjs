#!/usr/bin/env node
import { createHash } from "node:crypto";
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
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

if (!args.receipt) fail("usage: node tools/dashboard-automation/notify-slack.mjs --receipt <receipt.json>");

const receiptPath = path.resolve(args.receipt);
const receipt = JSON.parse(readFileSync(receiptPath, "utf8"));
const decision = notificationDecision(receipt);
if (!decision.shouldPost) {
  console.log(`dashboard Slack notify: skipped (${decision.reason})`);
  process.exit(0);
}

const statePath = process.env.GOATOS_DASHBOARD_SLACK_STATE_FILE || path.join(path.dirname(receiptPath), "..", "slack-notify-state.json");
const state = readState(statePath);
const signature = alertSignature(receipt, decision.kind);
const now = Date.now();
const minRepeatMs = Number(config.slackAlerts?.minimumRepeatIntervalMinutes ?? 240) * 60 * 1000;
if (state.lastSignature === signature && now - Number(state.lastPostedAtMs ?? 0) < minRepeatMs) {
  console.log("dashboard Slack notify: skipped (duplicate within cooldown)");
  process.exit(0);
}

const message = formatSlackMessage(receipt, decision.kind, receiptPath);
if (containsUnredactedSecret(JSON.stringify(message))) fail("refusing to send Slack message that appears to contain an unredacted secret");

await postSlack(message);
writeState(statePath, { lastSignature: signature, lastPostedAtMs: now, lastStatus: receipt.status ?? "unknown" });
console.log(`dashboard Slack notify: posted ${decision.kind}`);

function notificationDecision(value) {
  const status = value.status ?? "unknown";
  const blockers = value.blockers ?? [];
  const selfHealingUrl = selfHealingPrUrl(value);
  if (selfHealingUrl) return { shouldPost: true, kind: "self_healing_pr" };
  if (status === "pass") {
    const previous = readState(process.env.GOATOS_DASHBOARD_SLACK_STATE_FILE || path.join(path.dirname(receiptPath), "..", "slack-notify-state.json"));
    if (previous.lastStatus && previous.lastStatus !== "pass") return { shouldPost: true, kind: "recovery" };
    return { shouldPost: false, reason: "pass without prior failure" };
  }
  if (status === "degraded") return { shouldPost: true, kind: "degraded" };
  if (blockers.some((item) => /auth_blocked|missing .*env|token|credential/i.test(JSON.stringify(item)))) {
    return { shouldPost: true, kind: "auth_blocked" };
  }
  if (status === "fail") return { shouldPost: true, kind: "failure" };
  return { shouldPost: false, reason: `status ${status}` };
}

function formatSlackMessage(value, kind, receiptFile) {
  const titleByKind = {
    failure: "Dashboard automation failed",
    degraded: "Dashboard automation ran with degraded data trust",
    auth_blocked: "Dashboard automation needs auth/env",
    self_healing_pr: "Dashboard automation opened a self-healing PR",
    recovery: "Dashboard automation recovered"
  };
  const emojiByKind = {
    failure: ":rotating_light:",
    degraded: ":warning:",
    auth_blocked: ":warning:",
    self_healing_pr: ":hammer_and_wrench:",
    recovery: ":white_check_mark:"
  };
  const blockers = (value.blockers ?? []).slice(0, 6);
  const failedLayers = (value.layers ?? []).filter((layer) => layer.status !== "pass").slice(0, 6);
  const prUrl = selfHealingPrUrl(value);
  const title = `${emojiByKind[kind] ?? ":information_source:"} ${titleByKind[kind] ?? "Dashboard automation update"}`;
  const receiptRel = redactText(path.relative(repo, receiptFile));
  const fields = [
    { type: "mrkdwn", text: `*Status*\n\`${value.status ?? "unknown"}\`` },
    { type: "mrkdwn", text: `*Mode*\n\`${value.mode ?? "unknown"}\`` },
    { type: "mrkdwn", text: `*SHA*\n\`${String(value.repoSha ?? "unknown").slice(0, 12)}\`` },
    { type: "mrkdwn", text: `*Dashboard*\n${value.productionUrl ?? config.productionUrl}` },
    { type: "mrkdwn", text: `*Browser smoke*\n\`${browserSmokeStatus(value)}\`` },
    { type: "mrkdwn", text: `*Parity gate*\n\`${parityGateStatus(value)}\`` }
  ];
  const blocks = [
    { type: "header", text: { type: "plain_text", text: title, emoji: true } },
    { type: "section", fields },
    { type: "divider" }
  ];
  if (failedLayers.length) {
    blocks.push({
      type: "section",
      text: {
        type: "mrkdwn",
        text: `*Failed layers*\n${failedLayers.map((layer) => `• \`${layer.name}\` — ${truncate(redactText(layer.message ?? layer.status), 180)}`).join("\n")}`
      }
    });
  }
  if (value.runtimePolicy?.browserSmoke === "ran_degraded") {
    blocks.push({
      type: "section",
      text: {
        type: "mrkdwn",
        text: "*Important*\nProduction read-only Playwright/browser smoke still ran in degraded mode. This is a UI/WebView signal, not a certified STG-to-OCI data pass."
      }
    });
  } else if (value.runtimePolicy?.browserSmoke === "not_run") {
    blocks.push({
      type: "section",
      text: {
        type: "mrkdwn",
        text: "*Important*\nBrowser smoke did not run. The failure is a prerequisite/env/parity blocker, not a product page result."
      }
    });
  }
  if (blockers.length) {
    blocks.push({
      type: "section",
      text: {
        type: "mrkdwn",
        text: `*Top blockers*\n${blockers.map((blocker) => `• ${truncate(redactText(blockerText(blocker)), 220)}`).join("\n")}`
      }
    });
  }
  if (prUrl) {
    blocks.push({
      type: "section",
      text: { type: "mrkdwn", text: `*Self-healing PR*\n<${prUrl}|Open proposed fix / report PR>` }
    });
  }
  blocks.push({
    type: "context",
    elements: [
      { type: "mrkdwn", text: `Receipt: \`${receiptRel}\`` },
      { type: "mrkdwn", text: `Dedupe: repeated identical failures are muted for ${config.slackAlerts?.minimumRepeatIntervalMinutes ?? 240} min` }
    ]
  });
  const text = [
    `${title} — ${value.status ?? "unknown"} ${value.mode ?? "unknown"} ${String(value.repoSha ?? "unknown").slice(0, 12)}`,
    blockers.length ? `Top blocker: ${blockerText(blockers[0])}` : "",
    prUrl ? `Self-healing PR: ${prUrl}` : ""
  ].filter(Boolean).join("\n");
  return { text, blocks };
}

function blockerText(blocker) {
  if (blocker?.kind === "degraded_browser_smoke") {
    return "production read-only Playwright ran in degraded mode because parity was not certified";
  }
  if (blocker?.kind === "table_count_mismatch") {
    return `\`${blocker.table}\`: STG ${blocker.stg ?? "?"} vs OCI ${blocker.oci ?? "?"}${blocker.delta === undefined ? "" : ` (delta ${blocker.delta})`}`;
  }
  if (blocker?.kind === "sentinel_mismatch") {
    return `sentinel \`${blocker.name}\`${blocker.reason ? ` — ${blocker.reason}` : ""}`;
  }
  if (blocker?.kind === "field_reconciliation_mismatch") {
    return `field reconciliation \`${blocker.name}\`${blocker.reason ? ` — ${blocker.reason}` : ""}`;
  }
  if (blocker?.name) return `${blocker.kind ?? "blocker"} ${blocker.name}${blocker.reason ? ` (${blocker.reason})` : ""}`;
  if (blocker?.layer) return `${blocker.layer}: ${blocker.message ?? JSON.stringify(blocker)}`;
  return JSON.stringify(blocker);
}

function browserSmokeStatus(value) {
  const policy = value.runtimePolicy?.browserSmoke;
  if (policy === "ran_degraded") return "ran_degraded";
  if (policy === "ran_certified") return "ran_certified";
  if (policy === "ran_failed") return "ran_failed";
  if (policy === "not_run") return "not_run";
  const runtimeLayer = (value.layers ?? []).find((layer) => /module-journeys|production-module-journeys|playwright/.test(layer.name ?? ""));
  if (runtimeLayer?.status === "pass") return "ran";
  if (runtimeLayer?.status === "fail") return "ran_failed";
  return "unknown";
}

function parityGateStatus(value) {
  const parityLayers = (value.layers ?? []).filter((layer) => ["latest-full-parity-receipt", "business-data-parity"].includes(layer.name));
  if (parityLayers.length === 0) return "not_checked";
  if (parityLayers.every((layer) => layer.status === "pass")) return "pass";
  return "fail";
}

function selfHealingPrUrl(value) {
  const text = [value.selfHealing?.stdout, value.selfHealing?.stderr].filter(Boolean).join("\n");
  return text.match(/https:\/\/github\.com\/[^\s)]+\/pull\/\d+/)?.[0] ?? null;
}

async function postSlack(payload) {
  if (process.env.GOATOS_DASHBOARD_SLACK_DRY_RUN === "1") {
    console.log(payload.text);
    console.log(JSON.stringify(payload.blocks, null, 2));
    return;
  }
  const webhook = process.env.GOATOS_DASHBOARD_SLACK_WEBHOOK_URL?.trim();
  if (webhook) {
    const response = await fetch(webhook, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload)
    });
    if (!response.ok) throw new Error(`Slack webhook HTTP ${response.status}: ${redactText(await response.text())}`);
    return;
  }
  const token = process.env.SLACK_BOT_TOKEN?.trim() || process.env.GOATOS_DASHBOARD_SLACK_BOT_TOKEN?.trim();
  const channel = process.env.GOATOS_DASHBOARD_SLACK_CHANNEL_ID?.trim() || config.slackAlerts?.channelId;
  if (!token || !channel) {
    console.log("dashboard Slack notify: skipped (missing webhook or bot token/channel)");
    return;
  }
  const response = await fetch("https://slack.com/api/chat.postMessage", {
    method: "POST",
    headers: {
      "Authorization": `Bearer ${token}`,
      "Content-Type": "application/json; charset=utf-8"
    },
    body: JSON.stringify({ channel, text: payload.text, blocks: payload.blocks, unfurl_links: false, unfurl_media: false })
  });
  const body = await response.json().catch(async () => ({ ok: false, error: await response.text() }));
  if (!response.ok || !body.ok) throw new Error(`Slack chat.postMessage failed: ${redactText(JSON.stringify(body))}`);
}

function alertSignature(value, kind) {
  const blockers = (value.blockers ?? []).map((item) => ({ layer: item.layer, kind: item.kind, name: item.name, reason: item.reason, message: item.message }));
  return createHash("sha256")
    .update(JSON.stringify({ kind, mode: value.mode, status: value.status, repoSha: value.repoSha, blockers }))
    .digest("hex");
}

function readState(file) {
  if (!existsSync(file)) return {};
  try {
    return JSON.parse(readFileSync(file, "utf8"));
  } catch {
    return {};
  }
}

function writeState(file, state) {
  mkdirSync(path.dirname(file), { recursive: true });
  writeFileSync(file, `${JSON.stringify(state, null, 2)}\n`);
}

function truncate(value, max) {
  const text = String(value ?? "").replace(/\s+/g, " ").trim();
  return text.length <= max ? text : `${text.slice(0, max - 1)}…`;
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

function selfTest() {
  const sample = {
    mode: "production-smoke",
    status: "fail",
    repoSha: "abc123456789",
    productionUrl: "https://dashboard.mesha.sg",
    layers: [{ name: "business-data-parity", status: "fail", message: "sentinel failed" }],
    blockers: [{ kind: "sentinel_mismatch", name: "weighing_pen_alias_form_b_rows", reason: "planner_alias_form_b_rows" }]
  };
  const message = formatSlackMessage(sample, "failure", path.join(repo, "receipt.json"));
  for (const expected of ["Dashboard automation failed", "weighing_pen_alias_form_b_rows", "Dedupe: repeated identical failures", "Browser smoke", "Parity gate"]) {
    if (!JSON.stringify(message).includes(expected)) throw new Error(`self-test: Slack message missing ${expected}`);
  }
  const degraded = formatSlackMessage({
    ...sample,
    runtimePolicy: { browserSmoke: "ran_degraded" },
    layers: [{ name: "business-data-parity", status: "fail", message: "sentinel failed" }, { name: "production-module-journeys", status: "pass" }],
    blockers: [{ layer: "runner", kind: "degraded_browser_smoke", message: "degraded" }]
  }, "failure", path.join(repo, "receipt.json"));
  if (!JSON.stringify(degraded).includes("ran_degraded") || !JSON.stringify(degraded).includes("Playwright/browser smoke still ran")) {
    throw new Error("self-test: degraded browser smoke Slack wording missing");
  }
  const skipped = formatSlackMessage({
    ...sample,
    runtimePolicy: { browserSmoke: "not_run" },
    layers: [{ name: "business-data-parity", status: "fail", message: "sentinel failed" }]
  }, "failure", path.join(repo, "receipt.json"));
  if (!JSON.stringify(skipped).includes("not_run") || !JSON.stringify(skipped).includes("Browser smoke did not run")) {
    throw new Error("self-test: skipped browser smoke Slack wording missing");
  }
  if (!Array.isArray(message.blocks) || !message.blocks.some((block) => block.type === "header")) {
    throw new Error("self-test: Slack message must use block layout");
  }
  if (notificationDecision(sample).kind !== "failure") throw new Error("self-test: failure decision did not post");
  if (notificationDecision({ ...sample, status: "degraded" }).kind !== "degraded") throw new Error("self-test: degraded decision did not post");
  console.log("dashboard Slack notify: self-test passed");
}

function fail(message) {
  console.error(message);
  process.exit(1);
}
