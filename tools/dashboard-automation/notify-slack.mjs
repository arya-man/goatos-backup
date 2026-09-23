#!/usr/bin/env node
import { createHash } from "node:crypto";
import { existsSync, mkdirSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { containsUnredactedSecret, redactText } from "./lib/redact.mjs";
import { staticIssueRules } from "./lib/issue-rules.mjs";

// --- finding kinds (additive; one import + one registry entry per lane) ---
// A lane adds its Slack rendering as a module here and NOTHING else in this file changes.
// With no lane contributing findings every function below is a no-op, so a lane-1-only
// receipt renders byte-identically to what it rendered before the registry existed.
import writeJourneysKind from "./lib/finding-kinds/write-journeys.mjs";
import dataSanityKind from "./lib/finding-kinds/data-sanity.mjs";
import apiContractsKind from "./lib/finding-kinds/api-contracts.mjs";
import androidJourneysKind from "./lib/finding-kinds/android-journeys.mjs";
import mobileFlickerKind from "./lib/finding-kinds/mobile-flicker.mjs";
const FINDING_KINDS = [writeJourneysKind, dataSanityKind, apiContractsKind, androidJourneysKind, mobileFlickerKind];

// Only the lanes that actually found something on THIS receipt are active. A registered lane that
// contributed no findings contributes no rules and no blocks, so it cannot relabel, reorder or
// otherwise alter a lane-1-only message. `collectFindingKinds` must run before `issueRules()` is
// first consumed, which is why it sits above the humanIssue loop.
let activeFindingKinds = [];
// A lane whose module throws loses its own section, never the message. Its failure becomes a
// finding of its own -- silence is the worst outcome, because the automation would go quiet
// exactly when something is wrong. The module's name and error go to the log, never to Slack.
const BROKEN_KIND_SEVERITY = 5;
const BROKEN_KIND_SENTENCE = "One of the checks could not run at all, so whatever it looks after was not checked this time.";
const brokenFindingKind = {
  id: "finding-kind-failed",
  severity: BROKEN_KIND_SEVERITY,
  issueRules: () => [],
  renderSection: (findings) => [{
    type: "section",
    text: { type: "mrkdwn", text: `*Something could not be checked*\n${findings.map((item) => `• ${item.what}`).join("\n")}` }
  }],
  summaryText: (findings) => `${findings.length} part${findings.length === 1 ? "" : "s"} of this run could not be checked at all`,
  renderReplies: () => []
};
function collectFindingKinds(value, receiptFile) {
  const collected = [];
  const broken = [];
  for (const kind of FINDING_KINDS) {
    // One lane's broken module must never silence the whole alert, including lane 1's part of it.
    try {
      const findings = kind.toFindings?.(value, path.dirname(receiptFile)) ?? [];
      if (findings.length) collected.push({ kind, findings, severity: Number(kind.severity ?? 100) });
    } catch (error) {
      console.error(`dashboard Slack notify: finding kind ${kind?.id ?? "unknown"} failed and was dropped: ${redactText(String(error?.message ?? error))}`);
      broken.push({ what: BROKEN_KIND_SENTENCE });
    }
  }
  if (broken.length) collected.push({ kind: brokenFindingKind, findings: broken, severity: BROKEN_KIND_SEVERITY });
  // Severity decides who leads the message and who owns the headline. Registry order never does.
  collected.sort((a, b) => a.severity - b.severity);
  activeFindingKinds = collected;
  return collected;
}
function findingKindRules() {
  return activeFindingKinds.flatMap(({ kind }) => kind.issueRules?.() ?? []);
}
function applyFindingKinds(message, collected, context = {}) {
  if (!collected.length) return message;
  const headerAt = message.blocks.findIndex((block) => block.type === "header");
  let insertAt = headerAt + 1;
  let headline = null;
  const dropped = [];
  for (const { kind, findings } of collected) {
    let blocks;
    let summary;
    let ownHeadline = null;
    try {
      // Everything this kind contributes is resolved BEFORE a single block is spliced, the
      // headline included. A kind whose headline() throws then drops exactly like one whose
      // renderSection() throws, instead of killing the notifier after the message is half built
      // — which is what happened when headline() was called on its own at the tail.
      blocks = kind.renderSection?.(findings) ?? [];
      summary = kind.summaryText?.(findings) ?? "";
      if (!context.hasOwnIssues && typeof kind.headline === "function") ownHeadline = kind.headline(findings);
    } catch (error) {
      console.error(`dashboard Slack notify: finding kind ${kind?.id ?? "unknown"} could not render and was dropped: ${redactText(String(error?.message ?? error))}`);
      if (kind?.id !== brokenFindingKind.id) dropped.push({ what: BROKEN_KIND_SENTENCE });
      continue;
    }
    message.blocks.splice(insertAt, 0, ...blocks);
    insertAt += blocks.length;
    if (summary) message.text = `${message.text}\n${summary}`;
    if (headline === null && ownHeadline !== null) headline = ownHeadline;
  }
  // A lane dropped while rendering must still be visible, in plain English, or a crash has only
  // been traded for a silent gap.
  if (dropped.length) {
    try {
      const blocks = brokenFindingKind.renderSection(dropped);
      message.blocks.splice(insertAt, 0, ...blocks);
      message.text = `${message.text}\n${brokenFindingKind.summaryText(dropped)}`;
    } catch {
      // Never let the note about a dropped lane become the thing that drops the message.
    }
  }
  if (headline !== null && headerAt >= 0) message.blocks[headerAt].text.text = headline;
  return message;
}
function findingKindReplies(collected) {
  return collected.flatMap(({ kind, findings }) => {
    try {
      return kind.renderReplies?.(findings) ?? [];
    } catch {
      return [];
    }
  });
}
function findingKindSelfTests() {
  selfTestEveryFindingKindHasAPlainEnglishLabel();
  for (const kind of FINDING_KINDS) kind.selfTest?.();
  selfTestInactiveKindChangesNothing();
  selfTestBrokenKindDoesNotSilenceTheAlert();
}
// A layer with no labelByLayer entry falls through to its own raw name, so the generic failure
// card says "android-journeys" — a check code in Slack, which is the one thing this channel must
// never show. Each lane may only add an import and a registry entry here, so no lane could add
// its own label; this refuses the next one that forgets. A kind's id IS its layer name in run.mjs.
function selfTestEveryFindingKindHasAPlainEnglishLabel() {
  const labels = labelByLayer();
  for (const kind of FINDING_KINDS) {
    const id = String(kind?.id ?? "");
    if (!id) throw new Error("self-test: every registered finding kind must have an id naming its layer");
    const label = labels[id];
    if (!label) {
      throw new Error(`self-test: layer "${id}" has no labelByLayer entry, so Slack would show the check code itself`);
    }
    if (label === id) throw new Error(`self-test: layer "${id}" is labelled with its own code, not plain English`);
    if (/[-_]/.test(label)) throw new Error(`self-test: layer "${id}" label reads like a code, not a sentence: ${label}`);
  }
}

// A lane whose module throws must lose its own section, not the whole message.
function selfTestBrokenKindDoesNotSilenceTheAlert() {
  const broken = { id: "fake-broken", toFindings() { throw new Error("boom"); }, renderSection() { throw new Error("boom"); } };
  const healthy = {
    id: "fake-healthy",
    severity: 50,
    toFindings: () => [{ what: "x" }],
    renderSection: () => [{ type: "section", text: { type: "mrkdwn", text: "*Healthy lane still reported*" } }],
    summaryText: () => "healthy lane summary",
    renderReplies: () => []
  };
  const saved = FINDING_KINDS.splice(0, FINDING_KINDS.length, broken, healthy);
  try {
    const collected = collectFindingKinds({}, path.join(repo, "receipt.json"));
    if (!collected.some(({ kind }) => kind.id === "fake-healthy")) {
      throw new Error("self-test: a broken lane must not take a healthy lane's findings down with it");
    }
    if (!collected.some(({ kind }) => kind.id === "finding-kind-failed")) {
      throw new Error("self-test: a lane whose module throws must become a finding of its own, not silence");
    }
    // The lane-1 part of the message must survive untouched, and the failure must read plainly.
    const laneOneBlocks = [
      { type: "header", text: { type: "plain_text", text: "h" } },
      { type: "section", text: { type: "mrkdwn", text: "*What is wrong*\n• 1 x Chart labels squashed, cut off or missing" } }
    ];
    const message = { text: "lane one text", blocks: [...laneOneBlocks.map((block) => JSON.parse(JSON.stringify(block)))] };
    applyFindingKinds(message, collected, { hasOwnIssues: true });
    if (JSON.stringify(message.blocks.at(-1)) !== JSON.stringify(laneOneBlocks[1])) {
      throw new Error("self-test: lane 1's own section must survive a broken lane intact");
    }
    const rendered = JSON.stringify(message);
    if (!rendered.includes("Something could not be checked") || !rendered.includes("could not run at all")) {
      throw new Error("self-test: a broken lane must be reported in plain words");
    }
    if (rendered.includes("fake-broken") || rendered.includes("boom")) {
      throw new Error("self-test: a broken lane's module name and error must stay out of Slack");
    }
    if (!rendered.includes("Healthy lane still reported")) {
      throw new Error("self-test: the healthy lane's section must still be in the message");
    }
    if (!message.text.includes("lane one text")) throw new Error("self-test: lane 1's fallback text must survive");
    // A kind that throws while RENDERING is dropped, lane 1's header survives, and the drop is
    // said out loud rather than becoming a silent gap.
    const second = { text: "t", blocks: [{ type: "header", text: { type: "plain_text", text: "h" } }] };
    applyFindingKinds(second, [{ kind: broken, findings: [{}], severity: 0 }], { hasOwnIssues: true });
    if (JSON.stringify(second.blocks[0]) !== JSON.stringify({ type: "header", text: { type: "plain_text", text: "h" } })) {
      throw new Error("self-test: a kind that throws while rendering must not damage lane 1's header");
    }
    if (!JSON.stringify(second).includes("Something could not be checked") || !second.text.startsWith("t")) {
      throw new Error("self-test: a kind dropped while rendering must still be reported in plain words");
    }
    if (JSON.stringify(second).includes("fake-broken") || JSON.stringify(second).includes("boom")) {
      throw new Error("self-test: a dropped kind's module name and error must stay out of Slack");
    }

    // The judge's reproduction: a kind whose headline() throws, on a receipt where lane 1 found
    // nothing (so hasOwnIssues is false and the headline is actually asked for). This used to kill
    // the whole notifier AFTER blocks had been spliced, which is the failure isolation exists for.
    const headlineThrows = {
      id: "fake-headline-explodes",
      severity: 1,
      toFindings: () => [{ what: "x" }],
      renderSection: () => [{ type: "section", text: { type: "mrkdwn", text: "*Exploding lane section*" } }],
      summaryText: () => "exploding lane summary",
      headline() { throw new Error("headline blew up"); },
      renderReplies: () => []
    };
    const third = {
      text: "lane one text",
      blocks: [
        { type: "header", text: { type: "plain_text", text: "Dashboard automation failed" } },
        { type: "section", text: { type: "mrkdwn", text: "*What is wrong*\n• 1 x Chart labels squashed, cut off or missing" } }
      ]
    };
    applyFindingKinds(third, [
      { kind: headlineThrows, findings: [{ what: "x" }], severity: 1 },
      { kind: healthy, findings: [{ what: "y" }], severity: 50 }
    ], { hasOwnIssues: false });
    const thirdText = JSON.stringify(third);
    if (third.blocks[0].text.text !== "Dashboard automation failed") {
      throw new Error("self-test: a kind whose headline throws must not be allowed to rewrite the header");
    }
    if (thirdText.includes("Exploding lane section")) {
      throw new Error("self-test: a kind whose headline throws must lose its own section too");
    }
    if (!thirdText.includes("Healthy lane still reported")) {
      throw new Error("self-test: a healthy lane must still be delivered when another lane's headline throws");
    }
    if (!thirdText.includes("*What is wrong*") || !third.text.includes("lane one text")) {
      throw new Error("self-test: lane 1's own content must survive a lane whose headline throws");
    }
    if (!thirdText.includes("Something could not be checked")) {
      throw new Error("self-test: a lane dropped because its headline threw must still be reported");
    }
    if (thirdText.includes("headline blew up") || thirdText.includes("fake-headline-explodes")) {
      throw new Error("self-test: the error and the module name must stay out of Slack");
    }
  } finally {
    FINDING_KINDS.splice(0, FINDING_KINDS.length, ...saved);
    activeFindingKinds = [];
  }
}
// The invariant that outlives this lane: a registered kind that found NOTHING must not change a
// lane-1 message, even when it contributes rules. Registering lane 2 or lane 3 must not break this.
function selfTestInactiveKindChangesNothing() {
  const failure = { module: "tasks", route: "laptop:tasks-board", error: 'tasks-board laptop pen label reads "Godel 1" instead of the partition it belongs to' };
  const before = humanIssue(failure);
  const saved = activeFindingKinds;
  try {
    activeFindingKinds = [];
    const silent = { id: "fake-silent", issueRules: () => [[/pen label reads/, "Pen shown without its part number"]], toFindings: () => [] };
    const collected = [];
    for (const kind of [silent]) {
      const found = kind.toFindings() ?? [];
      if (found.length) collected.push({ kind, findings: found, severity: 0 });
    }
    activeFindingKinds = collected;
    const after = humanIssue(failure);
    if (JSON.stringify(before) !== JSON.stringify(after)) {
      throw new Error("self-test: a finding kind with no findings must not change how a lane-1 issue is described");
    }
    const message = { text: "t", blocks: [{ type: "header", text: { type: "plain_text", text: "h" } }] };
    const rendered = JSON.stringify(applyFindingKinds(message, collected, { hasOwnIssues: true }));
    if (rendered !== JSON.stringify({ text: "t", blocks: [{ type: "header", text: { type: "plain_text", text: "h" } }] })) {
      throw new Error("self-test: a finding kind with no findings must add no blocks and no text");
    }
  } finally {
    activeFindingKinds = saved;
  }
}
// --- end finding kinds ---

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
const findingKinds = collectFindingKinds(receipt, receiptPath); // --- finding kinds (additive) ---
const humanIssues = moduleFailures(receiptPath).map(humanIssue).filter(Boolean);
const slowIssues = humanIssues.filter((issue) => issue.kind === "slow-page");
const visualIssues = groupVisualIssues(humanIssues.filter((issue) => issue.kind !== "slow-page"));
const slowPages = groupSlowPages(slowIssues);
const message = (visualIssues.length || slowPages.pages.length) && decision.kind === "failure"
  ? formatVisualIssuesMessage(receipt, visualIssues, slowPages)
  : formatSlackMessage(receipt, decision.kind, receiptPath);
applyFindingKinds(message, findingKinds, { hasOwnIssues: visualIssues.length > 0 || slowPages.pages.length > 0 }); // --- finding kinds (additive) ---
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
inlineShots.push(...findingKindReplies(findingKinds)); // --- finding kinds (additive) ---
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
export function issueRules() {
  return [
  ...staticIssueRules(),
  ...findingKindRules(), // --- finding kinds (additive) ---
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
  // A rule's label may be a function when one check covers several kinds of element and the
  // sentence has to name the one that was measured (see tapTargetLabel).
  const what = rule ? (typeof rule[1] === "function" ? rule[1](raw) : rule[1]) : raw.replace(/\[[A-Za-z-]+\]\s*/g, "").slice(0, 120);
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
    // The route NAME as the sweep knows it (`tasks-overdue`), kept beside the display name so
    // grouping can work from the route and its URL instead of guessing from words on screen.
    route: routeName ?? null,
    deviceLabel,
    what,
    example,
    screenshot,
    raw,
    kind,
    // Computed above and previously dropped here, so every `issue.url` downstream read undefined
    // and no finding or slow page ever carried its link. Findings must name the page AND link to it.
    url,
    loadMs: slow ? Number(slow[1]) : null,
    budgetMs: slow ? Number(slow[2]) : null,
    caption: `${page} · ${deviceLabel.replace(/^\S+ /, "")} — ${what}${example}`.slice(0, 250)
  };
}

// The page a route belongs to, taken from the route's own URL PATH. The path is the only thing
// that actually knows whether two routes are the same page; the display name does not. Guessing
// from its first two words ("Feed Analytics", "Weighing Weights") read the second word as part of
// the family, which is true for those two and false for `tasks`: `tasks`, `tasks-list`,
// `tasks-overdue` and `tasks-search` are all `/tasks` with a different filter, so one button that
// is too small to tap posted as findings 11, 12, 13 and 14. Paths merge those four and keep
// `/feed/analytics` apart from `/feed/config`, which are genuinely different pages.
// A failure with no link is never merged on a guess: it keeps its own route as its key.
function pageFamilyKey(issue) {
  if (!issue?.url) return `route:${issue?.route || issue?.page || ""}`;
  try {
    return new URL(issue.url, "https://dashboard.mesha.sg").pathname.replace(/(.)\/+$/, "$1");
  } catch {
    return `route:${issue.route || issue.page || ""}`;
  }
}

// What to call a merged finding: the page itself, read off the path the merged routes share.
// /tasks -> "Tasks", /feed/analytics -> "Feed Analytics", /counts/sops -> "Counts Sops".
// One route keeps its own full name, so a lone `tasks-overdue` failure still says "Tasks Overdue".
// A path with an id in it (/goats/<uuid>) names nothing, so those fall through to the route names
// the variants agree on — as does anything grouped without a link.
function pageLabel(familyKey, routes, fallback) {
  const segments = String(familyKey ?? "").split("/").filter(Boolean);
  if (routes.length > 1 && String(familyKey).startsWith("/") && segments.length > 0
    && segments.every((segment) => /^[a-z][a-z-]*$/.test(segment))) {
    return segments.join(" ").replace(/-/g, " ").replace(/\b\w/g, (c) => c.toUpperCase());
  }
  const parts = (routes ?? []).filter(Boolean).map((name) => String(name).split("-"));
  if (parts.length === 0) return fallback;
  let shared = parts[0];
  for (const other of parts.slice(1)) {
    let i = 0;
    while (i < shared.length && i < other.length && shared[i] === other[i]) i += 1;
    shared = shared.slice(0, i);
  }
  if (shared.length === 0) return fallback;
  return shared.join(" ").replace(/\b\w/g, (c) => c.toUpperCase());
}

// One problem on one page is one finding, however many route variants and viewports saw it.
// "seen on N views" keeps the count so the merge never hides how widespread it is.
function groupVisualIssues(issues) {
  const grouped = [];
  for (const issue of issues) {
    const family = pageFamilyKey(issue);
    const key = `${issue.what}|${family}`;
    const seen = grouped.find((existing) => existing.key === key);
    if (seen) {
      if (!seen.deviceLabel.includes(issue.deviceLabel)) seen.deviceLabel += ` ${issue.deviceLabel}`;
      seen.views += 1;
      if (issue.route && !seen.routes.includes(issue.route)) seen.routes.push(issue.route);
      seen.page = pageLabel(family, seen.routes, seen.page);
      continue;
    }
    grouped.push({ ...issue, key, family, views: 1, routes: issue.route ? [issue.route] : [] });
  }
  return grouped;
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

// A layer's raw name is a check code, which must never reach Slack. Both the failed-checks
// list and the blocker list go through layerLabel() so a new layer cannot leak its id.
function labelByLayer() {
  return {
    "latest-full-parity-receipt": "STG-to-OCI parity receipt",
    "business-data-parity": "STG-to-OCI business data parity",
    "api-latency": "Dashboard API latency",
    "api-contracts": "Screens getting their data from the server", // --- finding kinds (additive) ---
    "data-sanity": "Figures on production that do not add up", // --- finding kinds (additive) ---
    "write-journeys": "Things a person does on the site", // --- finding kinds (additive) ---
    "android-journeys": "The phone app's opening screens", // --- finding kinds (additive) ---
    "mobile-flicker": "Screens that flicker on the phone", // --- finding kinds (additive) ---
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
}

function layerLabel(name) {
  return labelByLayer()[String(name ?? "unknown")] ?? String(name ?? "unknown");
}

function layerText(layer) {
  return `*${layerLabel(layer.name ?? "unknown")}* — ${friendlyLayerMessage(layer)}`;
}

function friendlyLayerMessage(layer) {
  const name = String(layer.name ?? "");
  const message = String(layer.message ?? layer.status ?? "");
  if (name === "coverage-sync") return coverageSyncMessage(message);
  if (name === "business-data-parity") return "OCI data does not currently match the required STG business snapshot.";
  if (name === "api-latency") return "At least one normal dashboard API exceeded the latency policy.";
  if (name === "api-contracts") return "Some screens did not get usable data from the server, or were slow to answer. They are named above."; // --- finding kinds (additive) ---
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
  if (blocker?.layer) return `${layerLabel(blocker.layer)}: ${friendlyBlockerMessage(blocker.message ?? JSON.stringify(blocker))}`;
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
    // The reply TEXT, not just the title: it is the line a person actually reads in the thread,
    // and it is where "seen on N views" says how many places one merged finding came from.
    // Flattened onto the one line so a dry run stays greppable line by line.
    for (const shot of inlineShots) {
      console.log(`dashboard Slack notify: would show inline ${path.basename(shot.file)} — ${shot.title} — reply: ${String(shot.comment ?? "").replace(/\n+/g, " ⏎ ").trim()}`);
    }
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
  selfTestIssueCarriesItsLink();
  selfTestShotFileDescription();
  selfTestEvidenceComment();
  selfTestSlowPageGrouping();
  selfTestRouteVariantGrouping();
  selfTestTapTargetWording();
  findingKindSelfTests(); // --- finding kinds (additive) ---
  console.log("dashboard Slack notify: self-test passed");
}

// Guards the caption above, and the whole upload path it lives on, against another stale
// identifier. The runtime crash this pins was invisible to every existing test because
// GOATOS_DASHBOARD_SLACK_DRY_RUN=1 returns before any upload happens.
function selfTestIssueCarriesItsLink() {
  const issue = humanIssue({
    module: "smoke",
    route: "mobile:weighing-weights",
    url: "https://dashboard.mesha.sg/weighing/weights",
    error: "weighing-weights mobile page load 14812ms exceeded budget 8000ms"
  });
  if (issue.url !== "https://dashboard.mesha.sg/weighing/weights") {
    throw new Error("self-test: a finding must carry its link — three render sites read issue.url");
  }
  const plain = humanIssue({ module: "smoke", route: "laptop:herd-signals", error: "text-overlap: \"Godel 1\" overlaps" });
  if (plain.url !== null) throw new Error("self-test: a finding with no url must report null, not undefined");
}

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

// One problem on four route variants of /tasks is ONE finding; two genuinely different pages
// that happen to share a first word stay two.
function selfTestRouteVariantGrouping() {
  const smallTarget = (route, query, device) => ({
    module: "operations",
    route: `${device}:${route}`,
    url: `https://dashboard.mesha.sg/tasks?scope_mode=company${query}`,
    error: `${route} ${device} has interactive targets below 40px: [{"tag":"input","className":"","ariaLabel":"Search by title, or type a task number","text":"","width":196,"height":18}]`
  });
  const tasks = groupVisualIssues([
    smallTarget("tasks", "", "mobile"),
    smallTarget("tasks-list", "&t_view=list", "mobile"),
    smallTarget("tasks-overdue", "&filter=overdue", "mobile"),
    smallTarget("tasks-search", "&t_q=pen", "mobile")
  ].map(humanIssue).filter(Boolean));
  if (tasks.length !== 1) {
    throw new Error(`self-test: four /tasks route variants with one problem must post once, got ${tasks.length}`);
  }
  if (tasks[0].page !== "Tasks") throw new Error(`self-test: the merged finding must be called "Tasks", got "${tasks[0].page}"`);
  if (tasks[0].views !== 4) throw new Error(`self-test: the merged finding must still say it was seen on 4 views, got ${tasks[0].views}`);

  // A lone variant keeps its own name rather than being shortened to the page stem.
  const lone = groupVisualIssues([smallTarget("tasks-overdue", "&filter=overdue", "mobile")].map(humanIssue).filter(Boolean));
  if (lone[0].page !== "Tasks Overdue") throw new Error(`self-test: a single route keeps its own name, got "${lone[0].page}"`);

  // Two pages under one module are two pages. This is the merge that must NEVER happen.
  const clipped = (route, path) => ({
    module: "feed",
    route: `laptop:${route}`,
    url: `https://dashboard.mesha.sg${path}?scope_mode=company`,
    error: `${route} laptop A-chart-label-clipped: label "Warmup Ration" clipped`
  });
  const feed = groupVisualIssues([
    clipped("feed-analytics", "/feed/analytics"),
    clipped("feed-config", "/feed/config")
  ].map(humanIssue).filter(Boolean));
  if (feed.length !== 2) throw new Error("self-test: Feed Analytics and Feed Config are different pages and must stay separate");
  if (feed.map((issue) => issue.page).join("|") !== "Feed Analytics|Feed Config") {
    throw new Error(`self-test: separate feed pages must keep their own names, got ${feed.map((issue) => issue.page).join("|")}`);
  }

  // The same page on laptop and phone is still one finding with both devices named.
  const bothDevices = groupVisualIssues([
    clipped("feed-analytics", "/feed/analytics"),
    { ...clipped("feed-analytics", "/feed/analytics"), route: "mobile:feed-analytics" }
  ].map(humanIssue).filter(Boolean));
  if (bothDevices.length !== 1 || !/Laptop/.test(bothDevices[0].deviceLabel) || !/Phone/.test(bothDevices[0].deviceLabel)) {
    throw new Error("self-test: laptop + phone on one page must stay one finding naming both devices");
  }

  // Tab variants of one analytics page merge; a sibling page under the same prefix does not.
  const weighing = groupVisualIssues([
    { module: "weighing", route: "mobile:weighing-analytics-breed", url: "https://dashboard.mesha.sg/weighing/analytics?tab=breed", error: 'weighing-analytics-breed mobile has interactive targets below 40px: [{"tag":"button","text":"x","width":20,"height":20}]' },
    { module: "weighing", route: "mobile:weighing-analytics-shed", url: "https://dashboard.mesha.sg/weighing/analytics?tab=shed", error: 'weighing-analytics-shed mobile has interactive targets below 40px: [{"tag":"button","text":"x","width":20,"height":20}]' },
    { module: "weighing", route: "mobile:weighing-weights", url: "https://dashboard.mesha.sg/weighing/weights", error: 'weighing-weights mobile has interactive targets below 40px: [{"tag":"button","text":"x","width":20,"height":20}]' }
  ].map(humanIssue).filter(Boolean));
  if (weighing.length !== 2) throw new Error(`self-test: /weighing/analytics tabs merge but /weighing/weights is its own page, got ${weighing.length}`);
  if (weighing[0].page !== "Weighing Analytics") throw new Error(`self-test: merged weighing tabs must read "Weighing Analytics", got "${weighing[0].page}"`);

  // A merged finding is named after the page, not after the words its route names happen to
  // share. `counts-sops` + `counts-sop-flow` are both /counts/sops; calling that finding "Counts"
  // would point a reader at the wrong screen.
  const sops = groupVisualIssues([
    { module: "counts", route: "mobile:counts-sops", url: "https://dashboard.mesha.sg/counts/sops?scope_mode=company", error: 'counts-sops mobile has interactive targets below 40px: [{"tag":"button","text":"x","width":20,"height":20}]' },
    { module: "counts", route: "mobile:counts-sop-flow", url: "https://dashboard.mesha.sg/counts/sops?scope_mode=company&compose=1&view=flow", error: 'counts-sop-flow mobile has interactive targets below 40px: [{"tag":"button","text":"x","width":20,"height":20}]' }
  ].map(humanIssue).filter(Boolean));
  if (sops.length !== 1 || sops[0].page !== "Counts Sops") {
    throw new Error(`self-test: /counts/sops variants must merge and be named for the page, got ${sops.length} × "${sops[0]?.page}"`);
  }

  // A path that is mostly an id names nothing, so those fall back to the route names.
  const passport = groupVisualIssues([
    { module: "counts", route: "laptop:goat-passport", url: "https://dashboard.mesha.sg/goats/7f3a1c20-0000-4000-8000-000000000001", error: 'goat-passport laptop chip-crushed: "Warmup" cut off' },
    { module: "counts", route: "mobile:goat-passport", url: "https://dashboard.mesha.sg/goats/7f3a1c20-0000-4000-8000-000000000001", error: 'goat-passport mobile chip-crushed: "Warmup" cut off' }
  ].map(humanIssue).filter(Boolean));
  if (passport.length !== 1 || passport[0].page !== "Goat Passport") {
    throw new Error(`self-test: an id-bearing path must fall back to the route name, got "${passport[0]?.page}"`);
  }

  // A failure with no link must never be merged on a guess about its name.
  const unlinked = groupVisualIssues([
    { module: "operations", route: "mobile:tasks-overdue", error: 'tasks-overdue mobile has interactive targets below 40px: [{"tag":"button","text":"x","width":20,"height":20}]' },
    { module: "operations", route: "mobile:tasks-search", error: 'tasks-search mobile has interactive targets below 40px: [{"tag":"button","text":"x","width":20,"height":20}]' }
  ].map(humanIssue).filter(Boolean));
  if (unlinked.length !== 2) throw new Error("self-test: without a URL there is nothing to prove two routes are one page; they must not merge");
}

// The sentence must be true for the element that was measured.
function selfTestTapTargetWording() {
  const say = (tag) => humanIssue({
    route: "mobile:tasks-search",
    error: `tasks-search mobile has interactive targets below 40px: [{"tag":"${tag}","className":"","ariaLabel":"Search by title, or type a task number","text":"","width":196,"height":18}]`
  }).what;
  if (say("input") !== "Box you type in is too small to tap") throw new Error(`self-test: an <input> is not a button, got "${say("input")}"`);
  if (say("a") !== "Links too small to tap") throw new Error(`self-test: an <a> must be called a link, got "${say("a")}"`);
  if (say("button") !== "Buttons too small to tap") throw new Error(`self-test: a <button> must still read as a button, got "${say("button")}"`);
  const mixed = humanIssue({
    route: "mobile:tasks",
    error: 'tasks mobile has interactive targets below 40px: [{"tag":"input","width":196,"height":18},{"tag":"button","width":20,"height":20}]'
  }).what;
  if (mixed !== "Too small to tap on a phone") throw new Error(`self-test: a mixed finding needs wording true for all of them, got "${mixed}"`);
  if (/[Bb]utton/.test(say("input"))) throw new Error("self-test: the word button must not appear in a finding about a text field");
}

function fail(message) {
  console.error(message);
  process.exit(1);
}
