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
    recovery: "Dashboard automation recovered"
  };
  const emojiByKind = {
    failure: ":rotating_light:",
    degraded: ":warning:",
    auth_blocked: ":warning:",
    recovery: ":white_check_mark:"
  };
  const blockers = (value.blockers ?? []).slice(0, 4);
  const failedLayers = (value.layers ?? []).filter((layer) => layer.status !== "pass").slice(0, 6);
  const title = `${emojiByKind[kind] ?? ":information_source:"} ${titleByKind[kind] ?? "Dashboard automation update"}`;
  const receiptRel = redactText(path.relative(repo, receiptFile));
  const summary = automationSummary(value, kind);
  const nextAction = automationNextAction(value, kind);
  const fields = [
    { type: "mrkdwn", text: `*Mode*\n\`${value.mode ?? "unknown"}\`` },
    { type: "mrkdwn", text: `*SHA*\n\`${String(value.repoSha ?? "unknown").slice(0, 12)}\`` },
    { type: "mrkdwn", text: `*Browser*\n\`${browserSmokeStatus(value)}\`` },
    { type: "mrkdwn", text: `*Data parity*\n\`${parityGateStatus(value)}\`` }
  ];
  const blocks = [
    { type: "header", text: { type: "plain_text", text: title, emoji: true } },
    {
      type: "section",
      text: {
        type: "mrkdwn",
        text: `*What happened*\n${summary}\n\n*Next action*\n${nextAction}`
      }
    },
    { type: "section", fields },
    { type: "divider" }
  ];
  if (failedLayers.length) {
    blocks.push({
      type: "section",
      text: {
        type: "mrkdwn",
        text: `*Failed checks*\n${failedLayers.map((layer) => `• ${layerText(layer)}`).join("\n")}`
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
  blocks.push({
    type: "context",
    elements: [
      { type: "mrkdwn", text: `Receipt: \`${receiptRel}\`` },
      { type: "mrkdwn", text: `Dedupe: repeated identical failures are muted for ${config.slackAlerts?.minimumRepeatIntervalMinutes ?? 240} min` }
    ]
  });
  const text = [
    `${title} — ${value.status ?? "unknown"} ${value.mode ?? "unknown"} ${String(value.repoSha ?? "unknown").slice(0, 12)}`,
    summary,
    `Next action: ${nextAction}`
  ].filter(Boolean).join("\n");
  return { text, blocks };
}

function automationSummary(value, kind) {
  if (kind === "recovery") return "The latest dashboard automation run is green again.";
  if (kind === "auth_blocked") return "The runner is missing or has expired auth/env, so product quality was not fully tested.";
  if (value.runtimePolicy?.browserSmoke === "not_run") {
    return "The runner stopped before browser/Playwright checks. Treat this as automation setup/data-prep failure, not a proven product UI failure.";
  }
  if (value.runtimePolicy?.browserSmoke === "ran_degraded") {
    return "Browser/Playwright checks ran, but data trust was degraded because STG-to-OCI parity was not certified.";
  }
  if (browserSmokeStatus(value) === "ran_failed") {
    return "Browser/Playwright checks ran and found a product-visible failure.";
  }
  return "One or more dashboard automation gates failed. See failed checks below.";
}

function automationNextAction(value, kind) {
  if (kind === "recovery") return "No action needed.";
  if (value.runtimePolicy?.browserSmoke === "not_run") return "Fix the prerequisite/env/data-parity gate first, then rerun browser smoke.";
  if (browserSmokeStatus(value) === "ran_failed") return "Open the receipt/screenshots and fix the product route that failed.";
  if (parityGateStatus(value) === "fail") return "Refresh/repair OCI parity from STG read-only data, then rerun.";
  return "Open the receipt only if this is new or not covered by the muted duplicate.";
}

function layerText(layer) {
  const name = String(layer.name ?? "unknown");
  const labelByLayer = {
    "latest-full-parity-receipt": "STG-to-OCI parity receipt",
    "business-data-parity": "STG-to-OCI business data parity",
    "api-latency": "Dashboard API latency",
    lighthouse: "Lighthouse page performance",
    "grafana-smoke": "Grafana/dashboard health",
    "vaccination-lifecycle": "Vaccination backend lifecycle tests",
    "production-module-journeys": "Production browser module journeys",
    "playwright-module-journeys": "Preview browser module journeys",
    "firebase-analytics-guard": "Firebase analytics guard",
    "oci-free-preflight": "OCI Always Free/storage preflight",
    static: "Static automation inventory"
  };
  const label = labelByLayer[name] ?? name;
  return `*${label}* — ${friendlyLayerMessage(layer)}`;
}

function friendlyLayerMessage(layer) {
  const name = String(layer.name ?? "");
  const message = String(layer.message ?? layer.status ?? "");
  if (name === "business-data-parity") return "OCI data does not currently match the required STG business snapshot.";
  if (name === "api-latency") return "At least one normal dashboard API exceeded the latency policy.";
  if (name === "lighthouse") return "Frontend page performance check failed.";
  if (name === "grafana-smoke") return "Grafana/monitoring smoke check failed.";
  if (name === "vaccination-lifecycle") return "Focused vaccination lifecycle tests failed.";
  if (/module-journeys|playwright/i.test(name)) return "Browser journey smoke failed; check screenshots/receipt for the route.";
  if (/required deterministic layer failed/i.test(message)) return "A prerequisite gate failed before this layer could run cleanly.";
  return truncate(redactText(message), 140);
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
  if (blocker?.layer) return `${blocker.layer}: ${friendlyBlockerMessage(blocker.message ?? JSON.stringify(blocker))}`;
  return JSON.stringify(blocker);
}

function friendlyBlockerMessage(message) {
  const text = String(message ?? "");
  if (/check-business-data-parity\.mjs/i.test(text)) return "STG-to-OCI business data parity check failed.";
  if (/api-latency-gate\.mjs/i.test(text)) return "Dashboard API latency gate failed.";
  if (/capture-lighthouse\.mjs/i.test(text)) return "Lighthouse performance gate failed.";
  if (/smoke-stg-grafana-dashboards\.mjs/i.test(text)) return "Grafana/dashboard smoke failed.";
  if (/go test \.\/internal\/vaccination\/app/i.test(text)) return "Vaccination lifecycle test suite failed.";
  if (/run-module-journeys\.mjs/i.test(text)) return "Browser module journey smoke failed.";
  return text;
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
  for (const expected of ["Dashboard automation failed", "weighing_pen_alias_form_b_rows", "Dedupe: repeated identical failures", "Browser", "Data parity"]) {
    if (!JSON.stringify(message).includes(expected)) throw new Error(`self-test: Slack message missing ${expected}`);
  }
  if (JSON.stringify(formatSlackMessage({
    ...sample,
    selfHealing: { stdout: "opened https://github.com/vgoats/goatos/pull/999" }
  }, "failure", path.join(repo, "receipt.json"))).includes("Self-healing PR")) {
    throw new Error("self-test: Slack message must not surface self-healing PR jargon");
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
