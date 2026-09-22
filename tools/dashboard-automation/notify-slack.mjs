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

const screenshotFiles = screenshotPaths(receipt, receiptPath);
const reportFile = writeHtmlReport(receipt, receiptPath, decision.kind, screenshotFiles);
await postSlack(message, [reportFile, ...screenshotFiles], statePath, { lastSignature: signature, lastPostedAtMs: now, lastStatus: receipt.status ?? "unknown" });
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
  if (isParityOnlyNoBrowserFailure(value)) {
    return { shouldPost: false, reason: "parity-only browser-not-run failure is receipt-only" };
  }
  if (status === "fail") return { shouldPost: true, kind: "failure" };
  return { shouldPost: false, reason: `status ${status}` };
}

function isParityOnlyNoBrowserFailure(value) {
  if ((value.status ?? "unknown") !== "fail") return false;
  if (browserSmokeStatus(value) !== "not_run") return false;
  const failingLayers = (value.layers ?? []).filter((layer) => layer.status !== "pass").map((layer) => layer.name);
  if (failingLayers.length === 0) return false;
  return failingLayers.every((name) => ["latest-full-parity-receipt", "business-data-parity"].includes(name));
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
  if (value.runtimePolicy?.browserSmoke === "not_run") return "Fix the runner/auth/static prerequisite first, then rerun browser smoke.";
  if (browserSmokeStatus(value) === "ran_failed") return "Open the receipt/screenshots and fix the product route that failed.";
  if (parityGateStatus(value) === "fail") return "Treat OCI parity as a data-trust signal only; fix product routes only if browser/API evidence failed.";
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
  if (/module-journeys|playwright/i.test(name)) {
    const lines = message.split("\n").map((line) => line.trim());
    const route = lines.filter((line) => line.startsWith("visual_route_start=")).pop()?.slice("visual_route_start=".length);
    const error = lines.filter((line) => /Error:/.test(line)).pop();
    if (!error) return "Browser journey smoke failed; see the HTML report and screenshots in the thread.";
    return `${route ? `route \`${route}\`: ` : ""}${truncate(redactText(error.replace(/^.*?Error:\s*/, "")), 400)}`;
  }
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
  if (/run-module-journeys\.mjs/i.test(text)) {
    const concrete = text
      .split("\n")
      .map((line) => line.trim())
      .filter((line) => /Error:|screenshot_path=|visual_route_start=/.test(line))
      .slice(-6)
      .join(" ");
    return concrete || "Browser module journey smoke failed.";
  }
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

function screenshotPaths(value, receiptFile) {
  const root = path.dirname(receiptFile);
  const text = [
    ...(value.layers ?? []).map((layer) => layer.message ?? ""),
    ...(value.blockers ?? []).map((blocker) => blocker.message ?? JSON.stringify(blocker)),
  ].join("\n");
  const found = [];
  for (const match of text.matchAll(/screenshot_path=([^\s]+)/g)) {
    const raw = redactText(match[1]);
    const absolute = path.isAbsolute(raw) ? raw : path.resolve(repo, raw);
    const fallback = path.resolve(root, raw);
    const file = existsSync(absolute) ? absolute : existsSync(fallback) ? fallback : null;
    if (file && !found.includes(file)) found.push(file);
  }
  // The route that failed is the last one screenshotted; keep the tail.
  return found.slice(-4);
}

async function postSlack(payload, attachments, statePath, nextState) {
  if (process.env.GOATOS_DASHBOARD_SLACK_DRY_RUN === "1") {
    console.log(payload.text);
    console.log(JSON.stringify(payload.blocks, null, 2));
    for (const file of attachments) console.log(`dashboard Slack notify: would upload ${path.relative(repo, file)}`);
    return;
  }
  const token = process.env.SLACK_BOT_TOKEN?.trim() || process.env.GOATOS_DASHBOARD_SLACK_BOT_TOKEN?.trim();
  const channel = process.env.GOATOS_DASHBOARD_SLACK_CHANNEL_ID?.trim() || config.slackAlerts?.channelId;
  const webhook = process.env.GOATOS_DASHBOARD_SLACK_WEBHOOK_URL?.trim();
  if (!token || !channel) {
    if (!webhook) {
      console.log("dashboard Slack notify: skipped (missing webhook or bot token/channel)");
      return;
    }
    // Webhooks cannot carry files; say so in the alert instead of silently dropping evidence.
    payload.blocks.push({ type: "context", elements: [{ type: "mrkdwn", text: ":warning: Screenshots/report not attached: runner has only a webhook. Set SLACK_BOT_TOKEN (files:write) to get them in-thread." }] });
    const response = await fetch(webhook, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(payload) });
    if (!response.ok) throw new Error(`Slack webhook HTTP ${response.status}: ${redactText(await response.text())}`);
    writeState(statePath, nextState);
    return;
  }
  const body = await slackApi(token, "chat.postMessage", { channel, text: payload.text, blocks: payload.blocks, unfurl_links: false, unfurl_media: false });
  // Persist dedupe state as soon as the alert is posted so a failed upload can never cause repeat spam.
  writeState(statePath, nextState);
  const uploaded = [];
  for (const file of attachments) {
    try {
      uploaded.push({ id: await uploadSlackFile(token, file), title: path.basename(file) });
    } catch (error) {
      console.error(`dashboard Slack notify: upload failed for ${path.basename(file)}: ${redactText(error.message)}`);
    }
  }
  if (uploaded.length === 0) return;
  await slackApi(token, "files.completeUploadExternal", {
    files: uploaded,
    channel_id: channel,
    thread_ts: body.ts,
    initial_comment: `Evidence: HTML report + ${uploaded.length - 1} failure screenshot(s)`
  });
}

async function slackApi(token, method, payload) {
  const response = await fetch(`https://slack.com/api/${method}`, {
    method: "POST",
    headers: { "Authorization": `Bearer ${token}`, "Content-Type": "application/json; charset=utf-8" },
    body: JSON.stringify(payload)
  });
  const body = await response.json().catch(async () => ({ ok: false, error: await response.text() }));
  if (!response.ok || !body.ok) throw new Error(`Slack ${method} failed: ${redactText(JSON.stringify(body))}`);
  return body;
}

async function uploadSlackFile(token, file) {
  const bytes = readFileSync(file);
  const params = new URLSearchParams({ filename: path.basename(file), length: String(bytes.length) });
  const response = await fetch("https://slack.com/api/files.getUploadURLExternal", {
    method: "POST",
    headers: { "Authorization": `Bearer ${token}`, "Content-Type": "application/x-www-form-urlencoded" },
    body: params
  });
  const meta = await response.json();
  if (!meta.ok) throw new Error(`files.getUploadURLExternal: ${JSON.stringify(meta)}`);
  const put = await fetch(meta.upload_url, { method: "POST", body: bytes });
  if (!put.ok) throw new Error(`upload POST HTTP ${put.status}`);
  return meta.file_id;
}

function writeHtmlReport(value, receiptFile, kind, screenshots) {
  const esc = (v) => String(v ?? "").replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]);
  const layers = value.layers ?? [];
  const errorLines = layers
    .filter((layer) => layer.status !== "pass")
    .flatMap((layer) => String(layer.message ?? "").split("\n").filter((line) => /Error:|failed|screenshot_path=|visual_route_start=/.test(line)).slice(-12).map((line) => ({ layer: layer.name, line: redactText(line.trim()) })));
  const shots = screenshots.map((file) => `<figure><img src="data:image/png;base64,${readFileSync(file).toString("base64")}" alt="${esc(path.basename(file))}"><figcaption>${esc(path.basename(file))}</figcaption></figure>`).join("");
  const rows = layers.map((layer) => `<tr class="${layer.status === "pass" ? "ok" : "bad"}"><td>${esc(layer.name)}</td><td>${esc(layer.status)}</td><td>${esc(layer.durationMs ? `${Math.round(layer.durationMs / 1000)}s` : "")}</td></tr>`).join("");
  const html = `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>GoatOS dashboard automation ${esc(value.mode)}</title>
<style>:root{--g:#1f6f43;--bad:#b42318;--bg:#f7f8f6;--ink:#1b1f1c;--mut:#5d665f}body{margin:0;font:15px/1.5 system-ui,sans-serif;background:var(--bg);color:var(--ink)}header{background:var(--g);color:#fff;padding:20px 24px}header.fail{background:var(--bad)}main{max-width:1100px;margin:0 auto;padding:16px}h1{margin:0;font-size:22px}.meta{display:flex;gap:18px;flex-wrap:wrap;opacity:.9;margin-top:6px;font-size:13px}section{background:#fff;border-radius:10px;padding:16px;margin:14px 0;box-shadow:0 1px 3px #0001}table{width:100%;border-collapse:collapse}td{padding:6px 8px;border-bottom:1px solid #eee}tr.bad td{color:var(--bad);font-weight:600}pre{white-space:pre-wrap;background:#111;color:#f3f3f3;padding:12px;border-radius:8px;font-size:12.5px;overflow:auto}figure{margin:0 0 18px}img{max-width:100%;border:1px solid #ddd;border-radius:8px}figcaption{color:var(--mut);font-size:13px}</style></head><body>
<header class="${value.status === "pass" ? "" : "fail"}"><h1>${esc(kind === "recovery" ? "Dashboard automation recovered" : "Dashboard automation failed")}</h1><div class="meta"><span>Mode: ${esc(value.mode)}</span><span>SHA: ${esc(String(value.repoSha ?? "").slice(0, 12))}</span><span>Browser: ${esc(browserSmokeStatus(value))}</span><span>${esc(new Date().toISOString())}</span></div></header>
<main><section><h2>What failed</h2>${errorLines.length ? `<pre>${errorLines.map((e) => `[${esc(e.layer)}] ${esc(e.line)}`).join("\n")}</pre>` : "<p>No concrete error lines captured; see receipt.</p>"}</section>
<section><h2>Screenshots</h2>${shots || "<p>No screenshots captured for this run.</p>"}</section>
<section><h2>All checks</h2><table>${rows}</table></section>
<section><p>Receipt: <code>${esc(path.relative(repo, receiptFile))}</code></p></section></main></body></html>`;
  const out = path.join(path.dirname(receiptFile), "report.html");
  writeFileSync(out, html);
  return out;
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
  const parityOnlyNoBrowser = notificationDecision({
    ...sample,
    runtimePolicy: { browserSmoke: "not_run" },
    layers: [{ name: "business-data-parity", status: "fail", message: "sentinel failed" }]
  });
  if (parityOnlyNoBrowser.shouldPost || !parityOnlyNoBrowser.reason.includes("receipt-only")) {
    throw new Error("self-test: parity-only browser-not-run failures must stay out of Slack");
  }
  console.log("dashboard Slack notify: self-test passed");
}

function fail(message) {
  console.error(message);
  process.exit(1);
}
