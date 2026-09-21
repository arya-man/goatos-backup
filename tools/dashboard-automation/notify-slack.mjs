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
if (containsUnredactedSecret(message)) fail("refusing to send Slack message that appears to contain an unredacted secret");

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
  if (blockers.some((item) => /auth_blocked|missing .*env|token|credential/i.test(JSON.stringify(item)))) {
    return { shouldPost: true, kind: "auth_blocked" };
  }
  if (status === "fail") return { shouldPost: true, kind: "failure" };
  return { shouldPost: false, reason: `status ${status}` };
}

function formatSlackMessage(value, kind, receiptFile) {
  const titleByKind = {
    failure: "Dashboard automation failed",
    auth_blocked: "Dashboard automation needs auth/env",
    self_healing_pr: "Dashboard automation opened a self-healing PR",
    recovery: "Dashboard automation recovered"
  };
  const emojiByKind = {
    failure: ":rotating_light:",
    auth_blocked: ":warning:",
    self_healing_pr: ":hammer_and_wrench:",
    recovery: ":white_check_mark:"
  };
  const blockers = (value.blockers ?? []).slice(0, 6);
  const failedLayers = (value.layers ?? []).filter((layer) => layer.status !== "pass").slice(0, 6);
  const prUrl = selfHealingPrUrl(value);
  const lines = [
    `${emojiByKind[kind] ?? ":information_source:"} *${titleByKind[kind] ?? "Dashboard automation update"}*`,
    `*Mode:* \`${value.mode ?? "unknown"}\`  *Status:* \`${value.status ?? "unknown"}\`  *SHA:* \`${String(value.repoSha ?? "unknown").slice(0, 12)}\``,
    `*URL:* ${value.productionUrl ?? config.productionUrl}`,
  ];
  if (failedLayers.length) {
    lines.push("*Failed layers:*");
    for (const layer of failedLayers) lines.push(`• \`${layer.name}\` — ${truncate(redactText(layer.message ?? layer.status), 220)}`);
  }
  if (blockers.length) {
    lines.push("*Top blockers:*");
    for (const blocker of blockers) lines.push(`• ${truncate(redactText(blockerText(blocker)), 240)}`);
  }
  if (prUrl) lines.push(`*Self-healing PR:* ${prUrl}`);
  lines.push(`*Receipt:* \`${redactText(path.relative(repo, receiptFile))}\``);
  lines.push("_No ping on repeated identical failures inside the cooldown. Green runs stay quiet unless they recover a prior red run._");
  return lines.join("\n");
}

function blockerText(blocker) {
  if (blocker?.name) return `${blocker.kind ?? "blocker"} ${blocker.name}${blocker.reason ? ` (${blocker.reason})` : ""}`;
  if (blocker?.layer) return `${blocker.layer}: ${blocker.message ?? JSON.stringify(blocker)}`;
  return JSON.stringify(blocker);
}

function selfHealingPrUrl(value) {
  const text = [value.selfHealing?.stdout, value.selfHealing?.stderr].filter(Boolean).join("\n");
  return text.match(/https:\/\/github\.com\/[^\s)]+\/pull\/\d+/)?.[0] ?? null;
}

async function postSlack(message) {
  if (process.env.GOATOS_DASHBOARD_SLACK_DRY_RUN === "1") {
    console.log(message);
    return;
  }
  const webhook = process.env.GOATOS_DASHBOARD_SLACK_WEBHOOK_URL?.trim();
  if (webhook) {
    const response = await fetch(webhook, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ text: message })
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
    body: JSON.stringify({ channel, text: message, unfurl_links: false, unfurl_media: false })
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
  for (const expected of ["Dashboard automation failed", "weighing_pen_alias_form_b_rows", "No ping on repeated identical failures"]) {
    if (!message.includes(expected)) throw new Error(`self-test: Slack message missing ${expected}`);
  }
  if (notificationDecision(sample).kind !== "failure") throw new Error("self-test: failure decision did not post");
  console.log("dashboard Slack notify: self-test passed");
}

function fail(message) {
  console.error(message);
  process.exit(1);
}
