// Lane 3 finding kind: backend API contract + latency findings.
//
// Everything this lane renders into Slack lives in this file. notify-slack.mjs touches
// exactly two lines for it: one import and one FINDING_KINDS entry.
//
// The contract with Slack (see the lane 3 doc):
//   * Slack prints the PAGE and a sentence a farm manager understands. Never an endpoint
//     path, a field path, a status code or a check code — those live in the HTML report.
//   * Latency findings do NOT join lane 1's "Slow pages" list, and they are NOT put
//     through groupSlowPages. Two reasons, both deliberate:
//       1. groupSlowPages exists to merge one page seen on laptop AND phone into a single
//          line. A backend answer has no device dimension — the server replies the same to
//          both — so there is nothing for it to merge, and its budget line would then
//          describe two different budgets (lane 1's 8s page load, this lane's 500ms API).
//       2. The registry's renderSection(findings) signature is owned by lane 4 and frozen;
//          it passes no helpers, so groupSlowPages is not reachable from here anyway.
//     What is left is a sort by median, which is not a fork of anything.
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { redactText } from "../redact.mjs";

export const ARTIFACT_NAME = "api-contracts.json";

// Codes that mean "the screen has no usable data" vs "the screen is slow to answer".
const SLOW_CODES = new Set(["slow"]);

function seconds(ms) {
  return `${(Number(ms) / 1000).toFixed(1).replace(/\.0$/, "")}s`;
}

export function readArtifact(reportDir) {
  const file = path.join(reportDir, ARTIFACT_NAME);
  if (!existsSync(file)) return null;
  try {
    return JSON.parse(readFileSync(file, "utf8"));
  } catch {
    return null;
  }
}

// Normalise the sweep's findings into two buckets. One page can fail several endpoints;
// a person only wants to be told once per page per kind.
export function toFindings(receipt, reportDir) {
  const artifact = readArtifact(reportDir);
  if (!artifact) return [];
  const findings = [];
  for (const item of artifact.findings ?? []) {
    // Never let a row without a page or a human sentence reach Slack: it would print
    // "undefined" to a farm manager. Rows like that are parked by the sweep already.
    if (!item?.humanFailure || !item?.page) continue;
    const slow = SLOW_CODES.has(item.code);
    const seen = findings.find((existing) => existing.page === item.page && existing.slow === slow);
    if (seen) {
      seen.count += 1;
      if (slow && (item.p50Ms ?? 0) > seen.medianMs) { seen.medianMs = item.p50Ms; seen.p95Ms = item.p95Ms; }
      continue;
    }
    findings.push({
      page: item.page,
      url: item.pageUrl ?? null,
      slow,
      what: item.humanFailure,
      count: 1,
      medianMs: item.p50Ms ?? 0,
      p95Ms: item.p95Ms ?? 0,
      budgetMs: item.budgetMs ?? 0,
      code: item.code,
      detail: item.detail ?? "",
      path: item.path ?? "",
      parked: artifact.parked ?? [],
      checked: (artifact.results ?? []).length,
    });
  }
  return findings;
}

const dataOf = (findings) => findings.filter((item) => !item.slow);
const slowOf = (findings) => findings.filter((item) => item.slow);

export function summaryText(findings) {
  const parts = [];
  const data = dataOf(findings); const slow = slowOf(findings);
  if (data.length) parts.push(`${data.length} screen${data.length === 1 ? "" : "s"} did not get their data`);
  if (slow.length) parts.push(`${slow.length} screen${slow.length === 1 ? "" : "s"} slow to answer`);
  return parts.join(", ");
}

export function headline(findings) {
  const data = dataOf(findings);
  if (data.length) return `${data.length} screen${data.length === 1 ? "" : "s"} did not get their data`;
  const slow = slowOf(findings);
  return slow.length ? `${slow.length} screen${slow.length === 1 ? "" : "s"} slow to answer` : null;
}

// Slack blocks. Page names and human sentences only.
export function renderSection(findings) {
  const blocks = [];
  const data = dataOf(findings);
  const slow = slowOf(findings);
  if (data.length) {
    const lines = data.slice(0, 6).map((item) => `• *${item.url ? `<${item.url}|${item.page}>` : item.page}* — ${item.what}`);
    const more = data.length - lines.length;
    blocks.push({ type: "section", text: { type: "mrkdwn", text: `*This screen's data did not load*\n${lines.join("\n")}${more > 0 ? `\n• +${more} more — full detail in the report` : ""}` } });
  }
  if (slow.length) {
    const ranked = [...slow].sort((a, b) => b.medianMs - a.medianMs);
    const budget = ranked.find((item) => item.budgetMs)?.budgetMs ?? 0;
    const lines = ranked.slice(0, 6).map((item) => `• *${item.url ? `<${item.url}|${item.page}>` : item.page}* — takes about ${seconds(item.medianMs)} to answer`);
    const more = ranked.length - lines.length;
    blocks.push({ type: "section", text: { type: "mrkdwn", text: `*This screen is slow to answer*${budget ? ` (should answer within ${seconds(budget)})` : ""}\n${lines.join("\n")}${more > 0 ? `\n• +${more} more — full detail in the report` : ""}` } });
  }
  return blocks;
}

// A threaded reply in this message carries a screenshot, and an API finding has none.
// Rather than invent a picture, lane 3 keeps its detail in the report and its sentence in
// the summary. Returning [] here is deliberate, not unfinished.
export function renderReplies() {
  return [];
}

// Paths, status codes and response fragments live here and nowhere else. The sweep writes
// this next to its JSON artifact; lane 4's registry has no attachment hook, so it reaches a
// person through the receipt's artifact list rather than as a second Slack upload.
export function reportHtml(findings, meta = {}) {
  const esc = (value) => String(value ?? "").replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]);
  const data = dataOf(findings);
  const slow = slowOf(findings);
  const parked = meta.parked ?? [];
  return `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Backend data check</title>
<style>body{font:15px/1.5 system-ui,-apple-system,sans-serif;margin:24px;color:#14201a}
table{border-collapse:collapse;width:100%;margin:12px 0 28px}
th,td{border-bottom:1px solid #e3e8e4;padding:7px 9px;text-align:left;vertical-align:top;font-size:13.5px}
th{color:#5d6b62}code{font-size:12px;word-break:break-all}h2{font-size:17px;margin:22px 0 0}
@media(prefers-color-scheme:dark){body{background:#10150f;color:#e8efe9}th,td{border-color:#2a332d}th{color:#9aa8a0}}</style></head><body>
<h1>Backend data check</h1>
<p>${esc(summaryText(findings) || "Nothing to report.")} &middot; ${Number(meta.checked ?? 0)} endpoint(s) checked, read-only GET.</p>
${data.length ? `<h2>Screens whose data did not load</h2><table><thead><tr><th>Page</th><th>What a person would see</th><th>Endpoint</th><th>Detail</th></tr></thead><tbody>${data.map((item) => `<tr><td>${esc(item.page)}</td><td>${esc(item.what)}</td><td><code>${esc(item.path)}</code></td><td>${esc(item.detail)}</td></tr>`).join("")}</tbody></table>` : ""}
${slow.length ? `<h2>Screens slow to answer</h2><table><thead><tr><th>Page</th><th>Median</th><th>p95</th><th>Budget</th><th>Endpoint</th></tr></thead><tbody>${slow.map((item) => `<tr><td>${esc(item.page)}</td><td>${Math.round(item.medianMs)}ms</td><td>${Math.round(item.p95Ms)}ms</td><td>${item.budgetMs}ms</td><td><code>${esc(item.path)}</code></td></tr>`).join("")}</tbody></table>` : ""}
${parked.length ? `<h2>Parked</h2><ul>${parked.map((item) => `<li>${esc(item.name)} &mdash; ${esc(item.reason)}</li>`).join("")}</ul>` : ""}
</body></html>`;
}

// MUST stay empty. The registry splices these rules into the SHARED issueRules() that
// labels lane 1's visual failures, whether or not this lane contributed a finding — so a
// rule here can silently relabel a lane 1 issue. Lane 3 describes its own findings inside
// renderSection instead, where it cannot reach lane 1's text.
export function issueRules() {
  return [];
}

export function selfTest() {
  // A kind with nothing to say must render nothing at all.
  const none = toFindings({}, "/nonexistent-report-dir");
  if (none.length !== 0) throw new Error("api-contracts self-test: no artifact must mean no findings");
  if (renderSection(none).length !== 0 || summaryText(none) !== "" || renderReplies(none).length !== 0) {
    throw new Error("api-contracts self-test: an inactive lane must add nothing to the message");
  }
  if (headline(none) !== null) throw new Error("api-contracts self-test: an inactive lane must not claim the headline");

  const findings = [
    { page: "Weights", url: "https://dashboard.mesha.sg/weighing/weights", slow: false, count: 1,
      what: "The weights page would show a blank column where the average daily gain should be.",
      medianMs: 0, p95Ms: 0, budgetMs: 0, code: "required-field-missing",
      detail: "rows[0].average_weight_kg is null", path: "/weighing/shed-weights?from=2026-09-01" },
    { page: "Operations health", url: "https://dashboard.mesha.sg/operations/dlq", slow: true, count: 1,
      what: "The operations health panel would not say whether the system is keeping up.",
      medianMs: 3487, p95Ms: 10292, budgetMs: 500, code: "slow",
      detail: "half of the requests took longer than 3487ms", path: "/operations/kernel-health" },
  ];
  const text = JSON.stringify(renderSection(findings));
  for (const leak of ["/weighing/shed-weights", "average_weight_kg", "required-field-missing",
    "/operations/kernel-health", "10292", "p95", "HTTP"]) {
    if (text.includes(leak)) throw new Error(`api-contracts self-test: "${leak}" leaked into Slack text`);
  }
  for (const expected of ["This screen's data did not load", "This screen is slow to answer", "Weights", "Operations health", "3.5s"]) {
    if (!text.includes(expected)) throw new Error(`api-contracts self-test: Slack text missing ${expected}`);
  }
  if (text.includes("Slow pages")) throw new Error("api-contracts self-test: must not reuse lane 1's slow-page heading");
  if (issueRules().length !== 0) throw new Error("api-contracts self-test: issueRules must stay empty");
  // The report is the only place a path may appear.
  const html = reportHtml(findings, { checked: 57, parked: [] });
  if (!html.includes("/operations/kernel-health")) throw new Error("api-contracts self-test: the report must carry the endpoint detail");
  return true;
}

export default {
  id: "api-contracts",
  // Lower sorts first in lane 4's registry. Lane 4 (a write journey silently failing) is
  // louder than a read screen being empty or slow, so lane 3 sits below it.
  severity: 60,
  issueRules,
  toFindings,
  renderSection,
  renderReplies,
  summaryText,
  headline,
  selfTest,
};
