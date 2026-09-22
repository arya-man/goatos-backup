#!/usr/bin/env node
import { createHash } from "node:crypto";
import { existsSync, mkdirSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
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

// Same problem on laptop + phone, or on sibling tab routes, is one issue with one screenshot.
// Slow page loads are a budget breach, not a visual break: they get collapsed into one
// line of their own instead of flooding the numbered list (see groupSlowPages).
const visualIssues = [];
const slowIssues = [];
for (const issue of moduleFailures(receiptPath).map(humanIssue).filter(Boolean)) {
  if (issue.kind === "slow-page") {
    slowIssues.push(issue);
    continue;
  }
  // Group by page family (first two words: "Feed Analytics", "Weighing Weights") + problem.
  const family = issue.page.split(" ").slice(0, 2).join(" ");
  const key = `${issue.what}|${family}`;
  const seen = visualIssues.find((existing) => existing.key === key);
  if (seen) {
    if (!seen.deviceLabel.includes(issue.deviceLabel)) seen.deviceLabel += ` ${issue.deviceLabel}`;
    seen.views += 1;
    continue;
  }
  visualIssues.push({ ...issue, page: family, key, views: 1 });
}
const slowPages = groupSlowPages(slowIssues);
const message = (visualIssues.length || slowPages.pages.length) && decision.kind === "failure"
  ? formatVisualIssuesMessage(receipt, visualIssues, slowPages)
  : formatSlackMessage(receipt, decision.kind, receiptPath);
if (containsUnredactedSecret(JSON.stringify(message))) fail("refusing to send Slack message that appears to contain an unredacted secret");

const screenshotFiles = screenshotPaths(receipt, receiptPath);
const reportFile = writeHtmlReport(receipt, receiptPath, decision.kind, screenshotFiles, visualIssues, slowPages);
// Each reply says what is wrong, where to see it, and carries that page's screenshot.
// Slow pages are issues too: one reply each, with the seconds, the slowest request and a screenshot.
const slowRepliesSource = slowIssues
  .filter((issue) => issue.screenshot)
  .sort((a, b) => (b.slowMs ?? 0) - (a.slowMs ?? 0))
  .filter((issue, i, all) => all.findIndex((other) => other.page === issue.page) === i)
  .slice(0, 6);
const inlineShots = [...visualIssues.filter((issue) => issue.screenshot).slice(0, 20), ...slowRepliesSource].map((issue, i) => ({
  file: issue.screenshot,
  title: `${issue.page} — ${issue.what}`.slice(0, 250),
  comment: [
    `*${i + 1}. ${issue.url ? `<${issue.url}|${issue.page}>` : issue.page}*  ${issue.deviceLabel}${issue.views > 1 ? `  ·  seen on ${issue.views} views` : ""}`,
    `${issue.what}${issue.example}`,
    null
  ].filter(Boolean).join("\n")
}));
await postSlack(message, inlineShots.length ? [reportFile] : [reportFile, ...screenshotFiles], statePath, { lastSignature: signature, lastPostedAtMs: now, lastStatus: receipt.status ?? "unknown" }, inlineShots);
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
  if (blockers.some((item) => /auth_blocked|missing [A-Z_]*\s*env|\b(bearer|id|firebase|refresh|access) token\b|token (expired|invalid|missing)|credentials? (missing|expired|invalid)|\b401\b|\b403\b/i.test(JSON.stringify(item)))) {
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

// Turn a raw check failure into something a person reads in two seconds:
// page, device, what is wrong, and the example text straight from the screen.
function issueRules() {
  return [
  [/A-svg-text-tiny|renders at ~/, "Chart text too small to read"],
  [/A-chart-label-column-narrow|A-chart-label-ellipsised|A-chart-label-collapsed|A-chart-label-clipped|A-chart-label-overlap|A-svg-text-(overlap|clipped)|A-chart-value-missing|A-chart-empty/, "Chart labels squashed, cut off or missing"],
  [/text-overlap|overlaps /, "Text drawn on top of other text"],
  [/chip-crushed/, "Label crushed / cut off"],
  [/C-cell-mid-word-wrap|split over \d+ lines/, "Word broken across two lines"],
  [/C-cell-overpaint/, "Table text spilling into the next column"],
  [/ISO date/, "Date shown as YYYY-MM-DD (farm reads DD/MM/YYYY)"],
  [/snake_case code|copy key|raw value "NaN|raw value/, "Internal code shown to users"],
  [/doubled label/, "Label repeated twice"],
  [/B-container-overflow|past \.card|cut at the viewport edge|panels cut/, "Content spilling out of its card"],
  [/D-page-overflow|horizontal overflow|scrolls sideways/, "Page scrolls sideways on the phone"],
  [/cannot be horizontally scrolled/, "Wide table cut off with no sideways scroll"],
  [/interactive targets below 40px/, "Buttons too small to tap"],
  [/clipped button\/link text/, "Button text cut off"],
  [/text-cut-off|text hidden/, "Text cut off"],
  [/overlay .*did not open|never mounted/, "Clicking it did not open"],
  [/feature missing/, "Feature missing or broken"],
  [/header not visible at the top/, "Drawer opens with its title bar scrolled out of view"],
  [/overlapping interactive elements/, "Buttons overlapping each other"],
  [/anchored to ancestor|backdrop-f/, "Popup opens in the wrong place (pinned to the header, not the screen)"],
  [/locator\.click: Timeout/, "A button on the page could not be clicked"],
  [/Smoke route redirected/, null],
  [/overlay .*off-screen|outside the viewport|translate/, "Drawer/popup opens off-screen"],
  [/page load \d+ms exceeded/, "Page slow to load"],
  [/accessibility violations/, null],
  [/new commit\(s\) need smoke coverage/, "New work shipped with no smoke check covering it"],
  [/assertion\(s\) need review/, "Some smoke checks point at screen text that no longer exists"],
  ];
}

function humanIssue(failure) {
  const whole = String(failure.error ?? "");
  // One route reports several checks joined by " || "; describe and quote from the SAME one,
  // or a feature title from another check leaks in as if it were text on the screen.
  const segments = whole.split(" || ").filter(Boolean);
  const raw = segments.find((segment) => issueRules().some(([re, label]) => label && re.test(segment))) ?? whole;
  const rule = issueRules().find(([re]) => re.test(raw));
  if (rule && rule[1] === null) return null;
  const what = rule ? rule[1] : raw.replace(/\[[A-Za-z-]+\]\s*/g, "").slice(0, 120);
  const [device, routeName] = String(failure.route ?? "").includes(":") ? failure.route.split(":") : ["", failure.route ?? failure.module];
  const quoted = [...raw.matchAll(/"([^"]{1,60})"(?!\s*:)/g)].map((m) => m[1]).filter((t) => t.trim().length > 1 && !/^[:;,.\s]+$/.test(t) && !/^\w+-\w+-/.test(t) && !/^(tag|kind|className|ariaLabel|text|table|missing-scroll-owner|button|input|a|span|div|td)$/.test(t));
  // Checks that dump element JSON: name the thing by its label or visible text.
  // Raw-code findings: show the code itself. Element-dump findings: name the element by label/text.
  const featureTitle = raw.match(/feature missing: ([^\[;]{3,120})/)?.[1]?.trim();
  if (featureTitle) quoted.unshift(featureTitle);
  const code = raw.match(/(?:snake_case code|copy key|raw value) "([^"]{1,60})"/)?.[1];
  const named = /^\s*[\w-]+ \w+ (has|overlay)/.test(raw) ? (raw.match(/"ariaLabel":"([^"]{2,60})"/)?.[1] || raw.match(/"text":"([^"]{2,60})"/)?.[1] || raw.match(/"className":"([^"]{2,40})"/)?.[1]) : null;
  if (code) quoted.unshift(code);
  else if (named) quoted.unshift(named.trim());
  const example = quoted[0] ? ` — "${quoted[0]}"` : (raw.match(/(\d+px[^;|]*)/)?.[1] ? ` — ${raw.match(/(\d+px[^;|]*)/)[1].slice(0, 60)}` : "");
  const page = String(routeName ?? "").replace(/-/g, " ").replace(/\b\w/g, (c) => c.toUpperCase());
  const deviceLabel = device === "mobile" ? "📱 Phone" : device === "laptop" ? "💻 Laptop" : "";
  const screenshot = (failure.screenshots ?? []).filter((file) => existsSync(file)).pop() ?? null;
  const slowMs = Number(raw.match(/page load (\d+)ms exceeded budget (\d+)ms/)?.[1] ?? 0);
  const budgetMs = Number(raw.match(/page load \d+ms exceeded budget (\d+)ms/)?.[1] ?? 0);
  const url = failure.url ?? null;
  // "<route> <viewport> page load 14812ms exceeded budget 8000ms" — keep the numbers so the
  // slow-page summary can rank worst-first and name the budget once.
  const slow = raw.match(/page load (\d+)ms exceeded budget (\d+)ms/);
  const kind = slow ? "slow-page" : "visual";
  return {
    page,
    deviceLabel,
    what,
    example,
    screenshot,
    raw,
    kind,
    loadMs: slow ? Number(slow[1]) : null,
    budgetMs: slow ? Number(slow[2]) : null,
    caption: `${page} · ${deviceLabel.replace(/^\S+ /, "")} — ${what}${example}`.slice(0, 250)
  };
}

// One slow page seen on laptop and phone is one slow page. Worst load time wins, and the
// devices merge into a single "laptop+phone" note.
function groupSlowPages(issues) {
  const pages = [];
  for (const issue of issues) {
    const seen = pages.find((existing) => existing.page === issue.page);
    const device = /phone/i.test(issue.deviceLabel) ? "phone" : /laptop/i.test(issue.deviceLabel) ? "laptop" : null;
    if (seen) {
      if (device && !seen.devices.includes(device)) seen.devices.push(device);
      if ((issue.loadMs ?? 0) > seen.loadMs) seen.loadMs = issue.loadMs ?? seen.loadMs;
      seen.views += 1;
      seen.raws.push(issue.raw);
      continue;
    }
    pages.push({
      page: issue.page,
      url: issue.url ?? null,
      cause: (issue.raw ?? "").match(/slowest requests: ([^|]+)/)?.[1]?.trim() ?? null,
      loadMs: issue.loadMs ?? 0,
      budgetMs: issue.budgetMs ?? 0,
      devices: device ? [device] : [],
      views: 1,
      raws: [issue.raw]
    });
  }
  pages.sort((a, b) => b.loadMs - a.loadMs);
  const budgetMs = pages.find((p) => p.budgetMs)?.budgetMs ?? 0;
  const devices = [...new Set(pages.flatMap((p) => p.devices))].sort();
  return { pages, budgetMs, devices };
}

// Declared, not const: --self-test runs before the module body finishes evaluating.
function seconds(ms) {
  return `${(Number(ms) / 1000).toFixed(1).replace(/\.0$/, "")}s`;
}

// "12 pages slower than 8s — worst: Goat Passport 14.8s, Herd Signals 9.1s (laptop+phone)"
function slowPagesLine(slow) {
  if (!slow?.pages?.length) return "";
  const worst = slow.pages.slice(0, 3).map((p) => `${p.page} ${seconds(p.loadMs)}`).join(", ");
  const where = slow.devices.length ? ` (${slow.devices.join("+")})` : "";
  const budget = slow.budgetMs ? ` slower than ${seconds(slow.budgetMs)}` : " over the page load budget";
  return `${slow.pages.length} page${slow.pages.length === 1 ? "" : "s"}${budget} — worst: ${worst}${where}`;
}

// Lanes are separate processes; they leave their counts in one folder so every message
// can say "part 2 of 3" and the total across the whole run.
function runPartSummary(count) {
  const part = process.env.GOATOS_DASHBOARD_RUN_PART;
  const dir = process.env.GOATOS_DASHBOARD_RUN_PART_DIR;
  const parts = Number(process.env.GOATOS_DASHBOARD_RUN_PARTS ?? 0);
  const modules = process.env.GOATOS_DASHBOARD_MODULES;
  if (!part || !dir) return "";
  try {
    mkdirSync(dir, { recursive: true });
    writeFileSync(path.join(dir, `${part}.json`), JSON.stringify({ part, count, modules: modules ?? "", at: Date.now() }));
    const seen = readdirSync(dir).filter((f) => f.endsWith(".json")).map((f) => JSON.parse(readFileSync(path.join(dir, f), "utf8")));
    const total = seen.reduce((sum, item) => sum + Number(item.count ?? 0), 0);
    const posted = seen.length;
    return `Part ${part.toUpperCase()} of ${parts || posted}${modules ? ` — ${modules.split(",").join(", ")}` : ""} · ${count} here · *${total} so far across ${posted} of ${parts || posted} parts* (each part posts its own message)`;
  } catch {
    return "";
  }
}

function formatVisualIssuesMessage(value, issues, slow = { pages: [] }) {
  const byKind = new Map();
  for (const issue of issues) byKind.set(issue.what.replace(/ —.*$/, ""), (byKind.get(issue.what.replace(/ —.*$/, "")) ?? 0) + 1);
  const kindLines = [...byKind.entries()].sort((a, b) => b[1] - a[1]).slice(0, 10).map(([what, n]) => `• ${n} × ${what}`);
  const slowLines = slow.pages.slice(0, 5).map((p) => {
    const cause = (p.cause ?? "").split(",")[0].trim();
    return `• ${p.url ? `<${p.url}|${p.page}>` : p.page} — *${seconds(p.loadMs)}*${cause ? ` · slowest ${cause}` : ""}`;
  });
  const title = issues.length
    ? `:rotating_light: ${issues.length} visible issue${issues.length === 1 ? "" : "s"} on production`
    : `:hourglass_flowing_sand: ${slow.pages.length} page${slow.pages.length === 1 ? "" : "s"} slow on production`;
  const blocks = [
    { type: "header", text: { type: "plain_text", text: title.replace(":rotating_light: ", "🚨 ").replace(":hourglass_flowing_sand: ", "⏳ "), emoji: true } },
  ];
  if (kindLines.length) blocks.push({ type: "section", text: { type: "mrkdwn", text: `*What is wrong*\n${kindLines.join("\n")}` } });
  if (slowLines.length) {
    blocks.push({ type: "section", text: { type: "mrkdwn", text: `*Slow pages*${slow.budgetMs ? ` (budget ${seconds(slow.budgetMs)})` : ""}\n${slowLines.join("\n")}${slow.pages.length > slowLines.length ? `\n• +${slow.pages.length - slowLines.length} more pages — full list in the report` : ""}` } });
  }
  const part = runPartSummary(issues.length);
  if (part) blocks.push({ type: "context", elements: [{ type: "mrkdwn", text: part }] });
  blocks.push({ type: "context", elements: [{ type: "mrkdwn", text: `Every issue below in this thread, one screenshot each · laptop 1440 + Android phone 390 · build \`${String(value.repoSha ?? "").slice(0, 9)}\` · full report in thread` }] });
  const text = `${title}\n${kindLines.join("\n")}${slowLines.length ? `\nSlow pages: ${slowPagesLine(slow)}` : ""}`;
  return { text, blocks };
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
  const failures = moduleFailures(receiptFile);
  if (failures.length) {
    blocks.push({
      type: "section",
      text: {
        type: "mrkdwn",
        text: `*${failures.length} failing route(s)*\n${failures.slice(0, 12).map((f) => `• *${f.module}* \`${f.route ?? "?"}\` — ${truncate(redactText(f.error.replace(/: \[\{.*$/, "")), 180)}`).join("\n")}`
      }
    });
  } else if (failedLayers.length) {
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
  if (blockers.length && !failures.length) {
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
    "coverage-sync": "Smoke coverage vs origin/main",
    static: "Static automation inventory"
  };
  const label = labelByLayer[name] ?? name;
  return `*${label}* — ${friendlyLayerMessage(layer)}`;
}

function friendlyLayerMessage(layer) {
  const name = String(layer.name ?? "");
  const message = String(layer.message ?? layer.status ?? "");
  if (name === "coverage-sync") return coverageSyncMessage(message);
  if (name === "business-data-parity") return "OCI data does not currently match the required STG business snapshot.";
  if (name === "api-latency") return "At least one normal dashboard API exceeded the latency policy.";
  if (name === "lighthouse") return "Frontend page performance check failed.";
  if (name === "grafana-smoke") return "Grafana/monitoring smoke check failed.";
  if (name === "vaccination-lifecycle") return "Focused vaccination lifecycle tests failed.";
  if (/module-journeys|playwright/i.test(name)) {
    const errors = message.split("\n").map((line) => line.trim())
      .filter((line) => /Error:/.test(line) && !/module journeys? .*failed|failed with exit/.test(line))
      .map((line) => truncate(redactText(line.replace(/^.*?Error:\s*/, "").replace(/: \[\{.*$/, "")), 160));
    if (!errors.length) return "Browser journey smoke failed; see the HTML report and screenshots in the thread.";
    return `${errors.length} failing route(s):\n${[...new Set(errors)].slice(0, 8).map((e) => `   ◦ ${e}`).join("\n")}`;
  }
  if (/required deterministic layer failed/i.test(message)) return "A prerequisite gate failed before this layer could run cleanly.";
  return truncate(redactText(message), 140);
}

// Plain English for the operator: how many commits shipped with no smoke check, and what to do.
export function coverageSyncMessage(message) {
  const text = String(message ?? "");
  const commits = Number(text.match(/(\d+) new commit\(s\) need smoke coverage/)?.[1] ?? 0);
  const stale = Number(text.match(/(\d+) assertion\(s\) need review/)?.[1] ?? 0);
  const parts = [];
  if (commits) parts.push(commits === 1 ? "1 new commit needs smoke coverage" : `${commits} new commits need smoke coverage`);
  if (stale) parts.push(stale === 1 ? "1 existing check points at screen text or elements that no longer exist" : `${stale} existing checks point at screen text or elements that no longer exist`);
  if (!parts.length) return "Smoke coverage could not be compared against the latest main.";
  return `${parts.join(", and ")}. Run \`node tools/dashboard-automation/sync-coverage.mjs --write\` and fill in the new entries.`;
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

function moduleFailures(receiptFile) {
  const file = path.join(path.dirname(receiptFile), "module-journeys", "module-journeys-receipt.json");
  if (!existsSync(file)) return [];
  try {
    return (JSON.parse(readFileSync(file, "utf8")).modules ?? []).flatMap((mod) => (mod.failures ?? (mod.failure ? [mod.failure] : [])).map((failure) => ({ module: mod.id, ...failure })));
  } catch {
    return [];
  }
}

function screenshotPaths(value, receiptFile) {
  const structured = moduleFailures(receiptFile).flatMap((failure) => (failure.screenshots ?? []).slice(-1)).filter((file) => existsSync(file));
  if (structured.length) return structured.slice(0, 20);
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

async function postSlack(payload, attachments, statePath, nextState, inlineShots = []) {
  if (process.env.GOATOS_DASHBOARD_SLACK_DRY_RUN === "1") {
    console.log(payload.text);
    console.log(JSON.stringify(payload.blocks, null, 2));
    for (const file of attachments) console.log(`dashboard Slack notify: would upload ${path.relative(repo, file)}`);
    for (const shot of inlineShots) console.log(`dashboard Slack notify: would show inline ${path.basename(shot.file)} — ${shot.title}`);
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
  // One threaded reply per issue: its own text, its own link, its own screenshot.
  for (const shot of inlineShots) {
    try {
      const id = await uploadSlackFile(token, shot.file);
      await slackApi(token, "files.completeUploadExternal", {
        files: [{ id, title: shot.title }],
        channel_id: channel,
        thread_ts: body.ts,
        initial_comment: shot.comment ?? shot.title
      });
    } catch (error) {
      // describeShotFile, not path.basename: a lane that hands us a non-path (undefined, or a
      // gs:// URI from a Test Lab pull) makes readFileSync throw, and path.basename(undefined)
      // then throws again from inside this catch — unhandled, after the message already posted.
      console.error(`dashboard Slack notify: upload failed for ${describeShotFile(shot)}: ${redactText(error?.message ?? String(error))}`);
    }
  }
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
    initial_comment: evidenceComment(inlineShots, uploaded)
  });
}

// The evidence caption on the final bundle upload. Extracted so it is reachable from --self-test:
// this line only ever runs on the real upload path, never in dry-run, which is how a stale
// identifier (`inline` for `inlineShots`) shipped and crashed every live alert after the message
// had already posted — the alert arrived with no screenshots attached.
// Never throws. The only job here is to name the offending attachment inside a catch block, so it
// must survive whatever a finding-kind module put in `file` — including nothing at all.
function describeShotFile(shot) {
  const file = shot?.file;
  if (typeof file !== "string" || file === "") return "an attachment with no usable file path";
  try {
    return path.basename(file);
  } catch {
    return file.slice(0, 80);
  }
}

function evidenceComment(inlineShots, uploaded) {
  return inlineShots.length
    ? "Full report (every check, every page)"
    : `Evidence: HTML report + ${uploaded.length - 1} failure screenshot(s)`;
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

function writeHtmlReport(value, receiptFile, kind, screenshots, issues = [], slow = { pages: [] }) {
  const esc = (v) => String(v ?? "").replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]);
  const img = (file) => `data:image/png;base64,${readFileSync(file).toString("base64")}`;
  const layers = value.layers ?? [];
  const when = new Date().toLocaleString("en-GB", { timeZone: "Asia/Kolkata", dateStyle: "medium", timeStyle: "short" });
  const phone = (issue) => /phone/i.test(issue.deviceLabel);

  const cards = issues.map((issue, i) => `
    <article class="issue">
      <div class="issue-head">
        <span class="num">${i + 1}</span>
        <div>
          <h3>${esc(issue.page)}${issue.views > 1 ? ` <span class="views">${issue.views} views</span>` : ""}</h3>
          <p class="what">${esc(issue.what)}${issue.example ? ` <span class="eg">${esc(issue.example.replace(/^ — /, ""))}</span>` : ""}</p>
        </div>
        <span class="where">${esc(issue.deviceLabel.replace(/[^\x20-\x7E]/g, "").trim() || "Laptop")}</span>
      </div>
      ${issue.screenshot && existsSync(issue.screenshot) ? `<figure class="${phone(issue) ? "phone" : "laptop"}"><img loading="lazy" src="${img(issue.screenshot)}" alt=""><figcaption>Problem outlined in red · ${esc(path.basename(issue.screenshot))}</figcaption></figure>` : ""}
      <details><summary>Technical detail</summary><pre>${esc(redactText(issue.raw ?? ""))}</pre></details>
    </article>`).join("");

  // Slow pages live in their own section, with every page and every raw line kept.
  const slowSection = slow?.pages?.length ? `
    <section class="slow">
      <h2>Slow pages</h2>
      <p class="what">${esc(slowPagesLine(slow))}</p>
      <table>
        <thead><tr><th>Page</th><th>Worst load</th><th>Budget</th><th>Seen on</th></tr></thead>
        <tbody>${slow.pages.map((p) => `<tr><td>${esc(p.page)}</td><td>${esc(seconds(p.loadMs))}</td><td>${p.budgetMs ? esc(seconds(p.budgetMs)) : "—"}</td><td>${esc(p.devices.join(" + ") || "—")}</td></tr>`).join("")}</tbody>
      </table>
      <details><summary>Technical detail</summary><pre>${esc(redactText(slow.pages.flatMap((p) => p.raws).join("\n")))}</pre></details>
    </section>` : "";

  const failedLayers = layers.filter((l) => l.status !== "pass").map((l) => esc(l.name)).join(", ");
  const html = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Production check · ${esc(value.mode ?? "")}</title>
<style>
 :root{--ink:#14201a;--mut:#5d6b62;--line:#e3e8e4;--bad:#b42318;--ok:#1f6f43;--bg:#f6f8f6}
 *{box-sizing:border-box} body{margin:0;background:var(--bg);color:var(--ink);font:16px/1.55 -apple-system,BlinkMacSystemFont,"Segoe UI",system-ui,sans-serif}
 header{background:#fff;border-bottom:1px solid var(--line);padding:26px 24px}
 .wrap{max-width:1000px;margin:0 auto}
 h1{margin:0 0 4px;font-size:26px;letter-spacing:-.01em}
 .sub{color:var(--mut);font-size:14px}
 .tags{display:flex;gap:8px;flex-wrap:wrap;margin-top:14px}
 .tag{border:1px solid var(--line);border-radius:999px;padding:4px 11px;font-size:13px;color:var(--mut);background:#fff}
 main{padding:22px 24px 60px}
 .issue{background:#fff;border:1px solid var(--line);border-left:4px solid var(--bad);border-radius:12px;padding:16px 18px;margin:0 0 16px}
 .issue-head{display:flex;gap:12px;align-items:flex-start}
 .num{background:var(--bad);color:#fff;border-radius:8px;min-width:26px;height:26px;display:inline-flex;align-items:center;justify-content:center;font-size:14px;font-weight:600;flex:0 0 auto}
 h3{margin:0;font-size:17px}
 .views{color:var(--mut);font-weight:400;font-size:13px}
 .what{margin:3px 0 0;color:#333}
 .eg{color:var(--mut)}
 .where{margin-left:auto;color:var(--mut);font-size:13px;white-space:nowrap}
 figure{margin:14px 0 0}
 figure img{display:block;border:1px solid var(--line);border-radius:8px;background:#fff}
 figure.phone img{width:320px;max-width:100%}
 figure.laptop img{width:100%;max-height:460px;object-fit:cover;object-position:top}
 figcaption{color:var(--mut);font-size:12.5px;margin-top:6px}
 details{margin-top:12px} summary{cursor:pointer;color:var(--mut);font-size:13px}
 pre{white-space:pre-wrap;word-break:break-word;background:#0f1512;color:#e8efe9;padding:12px;border-radius:8px;font-size:12px;overflow:auto}
 .slow{background:#fff;border:1px solid var(--line);border-left:4px solid #b7791f;border-radius:12px;padding:16px 18px;margin:0 0 16px}
 .slow h2{margin:0 0 4px;font-size:17px}
 .slow table{border-collapse:collapse;width:100%;margin-top:12px;font-size:14px}
 .slow th,.slow td{text-align:left;padding:6px 10px;border-bottom:1px solid var(--line)}
 .slow th{color:var(--mut);font-weight:600;font-size:13px}
 footer{color:var(--mut);font-size:13px;border-top:1px solid var(--line);padding-top:14px;margin-top:22px}
 @media (prefers-color-scheme: dark){:root{--ink:#e8efe9;--mut:#9aa8a0;--line:#2a332d;--bg:#10150f} header,.issue,.slow,.tag,figure img{background:#161c18} .what{color:#dbe4dd}}
</style></head><body>
<header><div class="wrap">
  <h1>${issues.length ? `${issues.length} issue${issues.length === 1 ? "" : "s"} a person would notice` : "Production check"}</h1>
  <div class="sub">Checked every page on a laptop and an Android phone · ${esc(when)} IST</div>
  <div class="tags"><span class="tag">Build ${esc(String(value.repoSha ?? "").slice(0, 9))}</span><span class="tag">${esc(value.mode ?? "")}</span>${failedLayers ? `<span class="tag">Failed stages: ${failedLayers}</span>` : ""}</div>
</div></header>
<main><div class="wrap">
  ${cards || "<p>No visible issues found on this run.</p>"}
  ${slowSection}
  <footer>Each screenshot is the page as the check saw it, with the problem outlined in red. Receipt: <code>${esc(path.relative(repo, receiptFile))}</code></footer>
</div></main></body></html>`;
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
  const coverage = formatSlackMessage({
    ...sample,
    layers: [{ name: "coverage-sync", status: "fail", message: "coverage-sync: 7 new commit(s) need smoke coverage, 2 assertion(s) need review (since f30769625)" }],
    blockers: [{ layer: "coverage-sync", message: "coverage-sync: 7 new commit(s) need smoke coverage, 2 assertion(s) need review" }]
  }, "failure", path.join(repo, "receipt.json"));
  const coverageText = JSON.stringify(coverage);
  for (const expected of ["Smoke coverage vs origin/main", "7 new commits need smoke coverage", "no longer exist", "sync-coverage.mjs --write"]) {
    if (!coverageText.includes(expected)) throw new Error(`self-test: coverage-sync Slack wording missing ${expected}`);
  }
  if (coverageText.includes("needs-assertion")) throw new Error("self-test: coverage-sync Slack wording must stay plain English, not manifest jargon");
  const singular = coverageSyncMessage("coverage-sync: 1 new commit(s) need smoke coverage, 1 assertion(s) need review");
  if (!singular.includes("1 new commit needs smoke coverage") || !singular.includes("1 existing check points at")) {
    throw new Error("self-test: coverage-sync wording must read naturally for a single commit/check");
  }
  if (!Array.isArray(message.blocks) || !message.blocks.some((block) => block.type === "header")) {
    throw new Error("self-test: Slack message must use block layout");
  }
  if (notificationDecision(sample).kind !== "failure") throw new Error("self-test: failure decision did not post");
  const uiFailure = notificationDecision({ ...sample, blockers: [{ layer: "production-module-journeys", message: "calendar laptop C-cell-mid-token-wrap: Warmup split over 2 lines" }] });
  if (uiFailure.kind !== "failure") throw new Error("self-test: UI text containing 'token' must not be classed as auth_blocked");
  if (notificationDecision({ ...sample, status: "degraded" }).kind !== "degraded") throw new Error("self-test: degraded decision did not post");
  const parityOnlyNoBrowser = notificationDecision({
    ...sample,
    runtimePolicy: { browserSmoke: "not_run" },
    layers: [{ name: "business-data-parity", status: "fail", message: "sentinel failed" }]
  });
  if (parityOnlyNoBrowser.shouldPost || !parityOnlyNoBrowser.reason.includes("receipt-only")) {
    throw new Error("self-test: parity-only browser-not-run failures must stay out of Slack");
  }
  selfTestShotFileDescription();
  selfTestEvidenceComment();
  selfTestSlowPageGrouping();
  console.log("dashboard Slack notify: self-test passed");
}

// Guards the caption above, and the whole upload path it lives on, against another stale
// identifier. The runtime crash this pins was invisible to every existing test because
// GOATOS_DASHBOARD_SLACK_DRY_RUN=1 returns before any upload happens.
function selfTestShotFileDescription() {
  // Each of these made postSlack's catch block throw a second, unhandled error.
  for (const shot of [{}, { file: undefined }, { file: null }, { file: 123 }, { file: "" }, undefined]) {
    const described = describeShotFile(shot);
    if (typeof described !== "string" || described === "") {
      throw new Error(`self-test: describeShotFile must always name something: ${JSON.stringify(shot)}`);
    }
  }
  if (describeShotFile({ file: "/tmp/a/b/weighing-issues.png" }) !== "weighing-issues.png") {
    throw new Error("self-test: describeShotFile stopped naming a normal screenshot by its file name");
  }
  // A Test Lab pull lands in a GCS bucket, not on local disk; lane 5 can hand us one of these.
  if (!describeShotFile({ file: "gs://goatos-testlab/run/shot.png" }).includes("shot.png")) {
    throw new Error("self-test: describeShotFile should still name a gs:// artefact");
  }
}

function selfTestEvidenceComment() {
  const withShots = evidenceComment([{ file: "a.png" }], [{ id: "1" }, { id: "2" }]);
  if (withShots !== "Full report (every check, every page)") {
    throw new Error("self-test: evidence caption changed when per-issue screenshots are present");
  }
  const withoutShots = evidenceComment([], [{ id: "1" }, { id: "2" }, { id: "3" }]);
  if (withoutShots !== "Evidence: HTML report + 2 failure screenshot(s)") {
    throw new Error(`self-test: evidence caption wrong without per-issue screenshots: ${withoutShots}`);
  }
  // The upload path never runs in dry-run, so read it as source and refuse any identifier that is
  // not in scope there. `inline` was one; this stops the next one reaching production.
  const source = readFileSync(fileURLToPath(import.meta.url), "utf8");
  const uploadPath = source.slice(source.indexOf("async function postSlack"), source.indexOf("async function slackApi"));
  for (const stale of [/[^\w.]inline\./, /[^\w.]shots\./, /[^\w.]attachment\./]) {
    if (stale.test(uploadPath)) {
      throw new Error(`self-test: postSlack references an identifier that is not in scope there: ${stale}`);
    }
  }
}

function selfTestSlowPageGrouping() {
  const slowFailure = (route, device, ms) => ({
    module: "perf",
    route: `${device}:${route}`,
    error: `${route} ${device} page load ${ms}ms exceeded budget 8000ms`
  });
  const raw = [
    slowFailure("goat-passport", "laptop", 14812),
    slowFailure("goat-passport", "mobile", 12004),
    slowFailure("herd-signals", "laptop", 9142),
    { module: "health", route: "laptop:health-analytics", error: "health-analytics laptop feature missing: Health Analytics screen with tabs [798497220]" }
  ].map(humanIssue).filter(Boolean);

  const visual = raw.filter((issue) => issue.kind !== "slow-page");
  const slow = groupSlowPages(raw.filter((issue) => issue.kind === "slow-page"));

  if (visual.length !== 1) throw new Error("self-test: slow page loads must stay out of the visible-issue list");
  if (slow.pages.length !== 2) throw new Error(`self-test: slow pages must collapse per page, got ${slow.pages.length}`);
  if (slow.pages[0].page !== "Goat Passport" || slow.pages[0].loadMs !== 14812) {
    throw new Error("self-test: slow pages must be ranked worst-first with the worst load time kept");
  }
  if (slow.pages[0].devices.join("+") !== "laptop+phone") throw new Error("self-test: laptop + phone must merge into one slow page");

  const line = slowPagesLine(slow);
  for (const expected of ["2 pages slower than 8s", "worst: Goat Passport 14.8s, Herd Signals 9.1s", "(laptop+phone)"]) {
    if (!line.includes(expected)) throw new Error(`self-test: slow page line missing ${expected} — got "${line}"`);
  }

  const message = formatVisualIssuesMessage({ repoSha: "abc123456789" }, visual, slow);
  const asText = JSON.stringify(message);
  if (!asText.includes("Slow pages")) throw new Error("self-test: Slack message needs its own slow-pages section");
  if (/\*1\. Goat Passport\*/.test(asText)) throw new Error("self-test: slow pages must not be numbered among the visible issues");
  if (!message.text.includes("Slow pages:")) throw new Error("self-test: Slack fallback text must mention slow pages");

  // Slow pages alone still produce the grouped message rather than the generic failure card.
  const slowOnly = formatVisualIssuesMessage({ repoSha: "abc123456789" }, [], slow);
  if (!JSON.stringify(slowOnly).includes("Slow pages")) throw new Error("self-test: slow-only runs must still report slow pages");
  if (slowOnly.blocks.some((block) => block.type === "section" && /\*1\./.test(block.text?.text ?? ""))) {
    throw new Error("self-test: slow-only runs must not emit an empty numbered list");
  }
}

function fail(message) {
  console.error(message);
  process.exit(1);
}
