// Lane 2's Slack finding kind: "Data does not add up".
//
// Everything this lane renders into Slack lives in this file. `notify-slack.mjs` only imports it
// and appends it to FINDING_KINDS; it stays a no-op when this lane contributed nothing, so a
// lane-1-only receipt renders byte-identically.
//
// Severity 20: louder than nothing, quieter than lane 4's "the practice data was left changed"
// and its failed journeys, because a number that does not add up is a wrong figure on a screen,
// not a broken action.
//
// Slack text rule for this lane: the screen a person opens, the question that was asked of the
// data, the sentence they read, and how many rows are wrong. No SQL, no table or column names,
// no check codes, no file paths, no rows. The scrub below is enforced, not advisory: rendering
// throws rather than leaking one. The offending rows go in this lane's own HTML report, which is
// attached as the one threaded reply this kind makes.

import { existsSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { redactText } from "../redact.mjs";

export const REPORT_RELATIVE_PATH = "data-sanity.json";
const HTML_RELATIVE_PATH = "data-sanity-report.html";
const MAX_SLACK_FINDINGS = 6;
const PRODUCTION_URL_FALLBACK = "https://dashboard.mesha.sg";

// Anything that would make a farm manager's eyes glaze over, or tell an outsider how the data is
// shaped. Checked against every string this module puts in front of a person.
const ENGINEER_SPEAK = [
  [/\bselect\b[\s\S]{0,120}\bfrom\b/i, "SQL"],
  [/\b(insert\s+into|delete\s+from|update\s+\w+\s+set|order\s+by|group\s+by|is\s+distinct\s+from)\b/i, "SQL"],
  [/::(text|numeric|uuid|int|date|timestamptz)\b|\bnow\(\)|\binterval\s+'|\bcount\(/i, "SQL"],
  [/\b[a-z]+_[a-z_]{2,}\b/, "a table or column name"],
  [/[#.][a-z][\w-]*\s*[>[]|\[[a-z-]+=|:has\(|aria-label=/i, "a selector"],
  [/\b(GOATOS|SLACK|PG)_[A-Z_]+\b/, "an environment variable"],
  [/\.(mjs|json|tsx?|html)\b/, "a file path"]
];

export function assertPlainEnglish(text, where) {
  const value = String(text ?? "");
  for (const [pattern, what] of ENGINEER_SPEAK) {
    // A link's URL is the one place a path is allowed, so strip links before scrubbing.
    const scrubbed = value.replace(/<https?:\/\/[^|>]+\|([^>]*)>/g, "$1");
    if (pattern.test(scrubbed)) {
      throw new Error(`refusing to put ${what} in front of a person (${where}): ${scrubbed.slice(0, 120)}`);
    }
  }
  return value;
}

function link(url, label) {
  return url ? `<${url}|${label}>` : label;
}

function severityRank(severity) {
  return { high: 0, medium: 1, low: 2 }[String(severity)] ?? 1;
}

function pageUrl(productionUrl, page) {
  if (!page?.path) return null;
  try {
    return new URL(page.path, productionUrl || PRODUCTION_URL_FALLBACK).toString();
  } catch {
    return null;
  }
}

function plain(value, max = 300) {
  const text = redactText(value ?? "").replace(/\s+/g, " ").trim();
  return text.length <= max ? text : `${text.slice(0, max - 1)}…`;
}

function countPhrase(finding) {
  const unit = plain(finding.countUnit || "figures that do not add up", 90);
  return `${finding.rowCountIsCapped ? "at least " : ""}${Number(finding.rowCount ?? 0)} ${unit}`;
}

/**
 * Reads this lane's own report out of the run directory and turns it into findings. A finding
 * without a human sentence and a named screen is dropped rather than rendered: a Slack line
 * nobody can act on is worse than no line. No report means no findings, which is what keeps a
 * lane-1-only receipt untouched.
 */
export function toFindings(receipt, reportDir) {
  const declared = (receipt?.artifacts ?? []).find((artifact) => artifact?.kind === "data-sanity-report")?.path;
  const file = declared && existsSync(declared)
    ? declared
    : (reportDir && existsSync(path.join(reportDir, REPORT_RELATIVE_PATH)) ? path.join(reportDir, REPORT_RELATIVE_PATH) : null);
  if (!file) return [];
  let report;
  try {
    report = JSON.parse(readFileSync(file, "utf8"));
  } catch {
    return [];
  }
  const productionUrl = report.productionUrl || receipt?.productionUrl || PRODUCTION_URL_FALLBACK;
  const dir = reportDir || path.dirname(file);
  return (report.findings ?? [])
    .filter((finding) => finding?.humanFailure && finding?.question && finding?.page?.title)
    .map((finding) => ({
      kind: "data-sanity",
      page: plain(finding.page.title, 60),
      url: pageUrl(productionUrl, finding.page),
      question: plain(finding.question, 160),
      story: plain(finding.humanFailure, 300),
      countText: countPhrase(finding),
      severity: ["high", "medium", "low"].includes(finding.severity) ? finding.severity : "medium",
      rowCount: Number(finding.rowCount ?? 0),
      sampleRows: Array.isArray(finding.sampleRows) ? finding.sampleRows : [],
      reportDir: dir
    }))
    .sort((a, b) => severityRank(a.severity) - severityRank(b.severity) || b.rowCount - a.rowCount);
}

/** The header this lane would use when it is the only thing that found something. */
export function headline(findings) {
  const n = findings.length;
  return `🚨 ${n} figure${n === 1 ? "" : "s"} on production do${n === 1 ? "es" : ""} not add up`;
}

/** Blocks inserted straight after the message header. */
export function renderSection(findings) {
  if (!findings.length) return [];
  const shown = findings.slice(0, MAX_SLACK_FINDINGS);
  const lines = shown.map((finding) => [
    `• ${link(finding.url, finding.page)} — *${assertPlainEnglish(finding.countText, finding.page)}*`,
    `   _${assertPlainEnglish(finding.question, finding.page)}_`,
    `   ${assertPlainEnglish(finding.story, finding.page)}`
  ].join("\n"));
  if (findings.length > shown.length) {
    lines.push(`• +${findings.length - shown.length} more — every one of them, with the rows behind it, is in the report in this thread`);
  }
  return [{
    type: "section",
    text: { type: "mrkdwn", text: `*Data does not add up*\n${lines.join("\n")}` }
  }];
}

/** One line appended to the notification's fallback text, for phones and search. */
export function summaryText(findings) {
  if (!findings.length) return "";
  const worst = findings.slice(0, 3).map((finding) => `${finding.page}: ${finding.countText}`).join("; ");
  return assertPlainEnglish(`Data does not add up — ${worst}`, "data sanity summary");
}

/**
 * One threaded reply, carrying this lane's own report. The rows that are wrong live in there and
 * nowhere else — never in a Slack block. There is no screenshot to show for a number that does
 * not add up, so this lane makes one reply rather than one per finding.
 */
export function renderReplies(findings) {
  if (!findings.length) return [];
  // Everything below runs on the Slack upload path, which no dry run and no self-test ever
  // reaches: GOATOS_DASHBOARD_SLACK_DRY_RUN=1 returns before the first upload. So this function
  // must not be able to throw, and must not hand that path a file that is not there — a reply
  // whose `file` is missing takes the whole alert down inside notify-slack's own catch, which
  // calls path.basename on it. Belt and braces: a verified-present file, a caption that falls
  // back to a constant, and no exception out of here under any input.
  try {
    const file = writeHtmlReport(findings);
    if (!file || !existsSync(file)) return [];
    let comment = "*Every figure that does not add up, with the rows behind it*";
    try {
      comment = `${comment}\n${assertPlainEnglish(summaryText(findings), "data sanity reply")}`;
    } catch {
      // A catalogue entry that would have leaked engineer-speak loses its caption line, not the
      // report. The section itself already refused to render, which is the loud signal.
    }
    return [{ file, title: "Data does not add up", comment: comment.slice(0, 2900) }];
  } catch {
    return [];
  }
}

/** This lane's findings come from its own report, not from a failing route, so it adds no rules. */
export function issueRules() {
  return [];
}

export function writeHtmlReport(findings) {
  const dir = findings[0]?.reportDir;
  if (!dir) return null;
  const out = path.join(dir, HTML_RELATIVE_PATH);
  try {
    writeFileSync(out, renderHtml(findings));
    return out;
  } catch {
    return null;
  }
}

export function renderHtml(findings) {
  const esc = (value) => String(value ?? "").replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]);
  const when = new Date().toLocaleString("en-GB", { timeZone: "Asia/Kolkata", dateStyle: "medium", timeStyle: "short" });
  const cards = findings.map((finding, index) => {
    const columns = [...new Set(finding.sampleRows.flatMap((row) => Object.keys(row ?? {})))];
    const table = finding.sampleRows.length
      ? `<table><thead><tr>${columns.map((column) => `<th>${esc(column.replace(/_/g, " "))}</th>`).join("")}</tr></thead>
         <tbody>${finding.sampleRows.map((row) => `<tr>${columns.map((column) => `<td>${esc(redactText(row?.[column] ?? ""))}</td>`).join("")}</tr>`).join("")}</tbody></table>
         <p class="note">Showing ${finding.sampleRows.length} of ${finding.rowCount}.</p>`
      : `<p class="note">No example rows were kept for this one.</p>`;
    return `<article class="issue ${esc(finding.severity)}">
      <div class="head"><span class="num">${index + 1}</span>
        <div><h3>${finding.url ? `<a href="${esc(finding.url)}">${esc(finding.page)}</a>` : esc(finding.page)}</h3>
        <p class="q">${esc(finding.question)}</p></div>
        <span class="where">${esc(finding.countText)}</span></div>
      <p class="what">${esc(finding.story)}</p>
      ${table}
    </article>`;
  }).join("");
  return `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Data does not add up</title>
<style>
 :root{--ink:#14201a;--mut:#5d6b62;--line:#e3e8e4;--bad:#b42318;--warn:#b7791f;--bg:#f6f8f6}
 *{box-sizing:border-box} body{margin:0;background:var(--bg);color:var(--ink);font:16px/1.55 -apple-system,BlinkMacSystemFont,"Segoe UI",system-ui,sans-serif}
 header{background:#fff;border-bottom:1px solid var(--line);padding:26px 24px}
 .wrap{max-width:1000px;margin:0 auto} h1{margin:0 0 4px;font-size:26px;letter-spacing:-.01em}
 .sub{color:var(--mut);font-size:14px} main{padding:22px 24px 60px}
 .issue{background:#fff;border:1px solid var(--line);border-left:4px solid var(--warn);border-radius:12px;padding:16px 18px;margin:0 0 16px}
 .issue.high{border-left-color:var(--bad)}
 .head{display:flex;gap:12px;align-items:flex-start}
 .num{background:var(--bad);color:#fff;border-radius:8px;min-width:26px;height:26px;display:inline-flex;align-items:center;justify-content:center;font-size:14px;font-weight:600;flex:0 0 auto}
 h3{margin:0;font-size:17px} h3 a{color:inherit}
 .q{margin:3px 0 0;color:var(--mut);font-size:14px}
 .what{margin:10px 0 0;color:#333}
 .where{margin-left:auto;color:var(--mut);font-size:13px;white-space:nowrap}
 table{border-collapse:collapse;width:100%;margin-top:12px;font-size:14px}
 th,td{text-align:left;padding:6px 10px;border-bottom:1px solid var(--line);vertical-align:top}
 th{color:var(--mut);font-weight:600;font-size:13px}
 .note{color:var(--mut);font-size:12.5px;margin:6px 0 0}
 @media (prefers-color-scheme: dark){:root{--ink:#e8efe9;--mut:#9aa8a0;--line:#2a332d;--bg:#10150f} header,.issue{background:#161c18} .what{color:#dbe4dd}}
</style></head><body>
<header><div class="wrap"><h1>${findings.length} figure${findings.length === 1 ? "" : "s"} that do${findings.length === 1 ? "es" : ""} not add up</h1>
<div class="sub">Read-only checks against the production data · ${esc(when)} IST</div></div></header>
<main><div class="wrap">${cards}</div></main></body></html>`;
}

// Run from `notify-slack.mjs --self-test`. Proves the two things that matter here: nothing is
// contributed when this lane has no report (which is what keeps lane 1 byte-identical), and the
// engineer-speak scrub actually refuses SQL, a table name and a check code rather than describing
// itself as refusing them. The byte-identical claim itself is proved end to end in
// check-data-sanity.test.mjs by rendering the whole message twice.
export function selfTest() {
  if (toFindings({}, path.join(path.sep, "nowhere", "at", "all")).length !== 0) {
    throw new Error("self-test: no report must mean no findings");
  }
  if (renderSection([]).length !== 0) throw new Error("self-test: no findings must mean no blocks");
  if (summaryText([]) !== "") throw new Error("self-test: no findings must mean no fallback text");
  if (renderReplies([]).length !== 0) throw new Error("self-test: no findings must mean no replies");
  // Nothing this lane puts on the upload path may throw, and no reply may name a file that is
  // not there. Neither is reachable from a dry run, so it is asserted here instead.
  for (const hostile of [
    [{ kind: "data-sanity", page: "x", rowCount: 1, sampleRows: [], reportDir: null }],
    [{ kind: "data-sanity", page: "x", rowCount: 1, sampleRows: [], reportDir: path.join(path.sep, "nowhere", "at", "all") }],
    [{ kind: "data-sanity", page: "x", countText: "select 1 from goats", story: "s", question: "q", rowCount: 1, sampleRows: [], reportDir: null }]
  ]) {
    const replies = renderReplies(hostile);
    for (const reply of replies) {
      if (!reply.file || !existsSync(reply.file)) throw new Error("self-test: a reply must never name a file that is not there");
      if (typeof reply.title !== "string" || !reply.title) throw new Error("self-test: a reply must carry a title");
      if (typeof reply.comment !== "string" || !reply.comment) throw new Error("self-test: a reply must carry a caption");
    }
  }
  if (issueRules().length !== 0) throw new Error("self-test: this lane adds no route-error rules");

  for (const leak of [
    "select display_id from goats where dob is null",
    "the goat_shed_partitions rows disagree",
    "drive_scheduled_for_exited_animal found rows",
    "GOATOS_STG_READONLY_DATABASE_URL was unset",
    "see data-sanity-checks.json",
    "count(*) came back wrong"
  ]) {
    let refused = false;
    try {
      assertPlainEnglish(leak, "self-test");
    } catch {
      refused = true;
    }
    if (!refused) throw new Error(`self-test: the plain-English scrub accepted ${JSON.stringify(leak)}`);
  }
  assertPlainEnglish("Upcoming vaccination rounds still list animals that have already been sold or have died.", "self-test");
  assertPlainEnglish("• <https://dashboard.mesha.sg/feed/analytics|Feed analytics> — *4 feeds sent out in greater quantity than was bought*", "self-test");

  const sample = [{
    kind: "data-sanity",
    page: "Feed analytics",
    url: "https://dashboard.mesha.sg/feed/analytics",
    question: "Has more feed been issued to the pens than was ever bought?",
    story: "More of some feeds has been sent out to the pens than was ever bought.",
    countText: "4 feeds sent out in greater quantity than was bought",
    severity: "high",
    rowCount: 4,
    sampleRows: [{ feed: "Maize", note: "password=hunter2supersecret" }],
    reportDir: null
  }];
  const blocks = renderSection(sample);
  const rendered = JSON.stringify(blocks);
  if (!rendered.includes("Data does not add up")) throw new Error("self-test: the approved label must be used");
  if (!rendered.includes("4 feeds sent out")) throw new Error("self-test: the count must reach Slack");
  for (const leak of ["Maize", "hunter2supersecret", "sampleRows", "rowCount"]) {
    if (rendered.includes(leak)) throw new Error(`self-test: a row value leaked into Slack (${leak})`);
  }
  const html = renderHtml(sample);
  if (!html.includes("Maize")) throw new Error("self-test: the rows must reach this lane's report");
  if (html.includes("hunter2supersecret")) throw new Error("self-test: the report must redact a credential in a row");
  if (!headline(sample).includes("does not add up")) throw new Error("self-test: the headline must read as one figure");
  if (!headline([...sample, sample[0]]).includes("do not add up")) throw new Error("self-test: the headline must read as several figures");
}

export default { id: "data-sanity", severity: 20, issueRules, toFindings, renderSection, renderReplies, summaryText, headline, assertPlainEnglish, selfTest };
