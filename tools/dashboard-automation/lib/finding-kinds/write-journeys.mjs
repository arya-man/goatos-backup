// Lane 4's Slack finding kind: "Doing this on the site did not work".
//
// Everything this lane renders into Slack lives in this file. `notify-slack.mjs` only imports it
// and appends it to FINDING_KINDS; it must stay a no-op when this lane contributed nothing, so a
// lane-1-only receipt renders byte-identically.
//
// Two severities, both owned here, so nothing in notify-slack.mjs has to be reordered:
//   0  the clone was left changed, or the restore could not be proved  (louder than any journey)
//   1  a journey failed: doing this on the site did not work
//
// Slack text rule for this lane: a farm manager's sentence and nothing else. No SQL, no table or
// column names, no selectors, no check codes, no endpoints, no env var names, no file paths. The
// scrub below is enforced, not advisory: rendering throws rather than leaking one.

import { existsSync, readFileSync } from "node:fs";
import path from "node:path";

export const RECEIPT_RELATIVE_PATH = "write-journeys/write-journeys-receipt.json";

const RESTORE_SEVERITY = 0;
const JOURNEY_SEVERITY = 1;

// Anything that would make a farm manager's eyes glaze over, or tell an outsider how the data is
// shaped. Checked against every string this module puts in front of a person.
const ENGINEER_SPEAK = [
  [/\bselect\b[\s\S]{0,120}\bfrom\b/i, "SQL"],
  [/\b(insert\s+into|delete\s+from|update\s+\w+\s+set|order\s+by|group\s+by)\b/i, "SQL"],
  [/::(text|numeric|uuid|int|timestamptz)\b|\bnow\(\)|\binterval\s+'/i, "SQL"],
  [/\b[a-z]+_[a-z_]{2,}\b/, "a table or column name"],
  [/[#.][a-z][\w-]*\s*[>[]|\[[a-z-]+=|:has\(|aria-label=/i, "a selector"],
  [/\b[a-z][\w-]*\.[a-z][\w-]{1,}\b/, "a selector or a file name"],
  [/\b[A-Z]{2,}_[A-Z_]{2,}\b/, "an env var name"],
  [/\b[A-Z]-[a-z]+-[a-z-]+\b/, "a check code"],
  [/\/(api|v1|internal)\//i, "an endpoint path"],
  [/\bhttp\s+\d{3}\b|\b(?:4|5)\d{2}\s+(?:error|status)\b/i, "a status code"],
  [/(^|\s)\/(?:home|Users|tmp|var|opt)\//, "a file path"]
];

/** Throws rather than let engineer-speak reach Slack. Exported so the tests can attack it. */
export function assertPlainEnglish(text, where = "write-journey finding") {
  const value = String(text ?? "");
  for (const [pattern, what] of ENGINEER_SPEAK) {
    const hit = value.match(pattern);
    if (hit) throw new Error(`${where} would put ${what} in Slack: ${JSON.stringify(hit[0].slice(0, 60))}`);
  }
  return value;
}

function link(url, label) {
  return url ? `<${url}|${label}>` : label;
}

/**
 * Reads this lane's own receipt and turns it into findings. Returns [] when the lane did not run,
 * which is what keeps a lane-1-only receipt unchanged.
 */
export function toFindings(receipt, reportDir) {
  const file = path.join(String(reportDir ?? ""), RECEIPT_RELATIVE_PATH);
  if (!existsSync(file)) return [];
  try {
    return toFindingsFromJourneys(JSON.parse(readFileSync(file, "utf8")).journeys ?? []);
  } catch {
    return [];
  }
}

function restoreFindings(findings) {
  return findings.filter((item) => item.kind === "restore");
}

function journeyFindings(findings) {
  return findings.filter((item) => item.kind === "journey");
}

/** The header this lane would use when it is the only thing that found something. */
export function headline(findings) {
  if (restoreFindings(findings).length) return "‼️ Practice data was left changed";
  const n = journeyFindings(findings).length;
  return `🚨 ${n} thing${n === 1 ? "" : "s"} a person does on the site did not work`;
}

/** Blocks inserted straight after the message header. Severity 0 first, always. */
export function renderSection(findings) {
  if (!findings.length) return [];
  const blocks = [];
  const restores = restoreFindings(findings);
  if (restores.length) {
    const lines = restores.map((item) => `• ${link(item.url, item.page)} — ${assertPlainEnglish(item.headline, item.page)} ${assertPlainEnglish(item.detail, item.page)}`.trim());
    blocks.push({
      type: "section",
      text: {
        type: "mrkdwn",
        text: `*‼️ Practice data was left changed — fix this before anything else*\n${lines.join("\n")}`
      }
    });
  }
  const journeys = journeyFindings(findings);
  if (journeys.length) {
    const lines = journeys.slice(0, 8).map((item) => `• ${link(item.url, item.page)} — ${assertPlainEnglish(item.headline, item.page)}`);
    const more = journeys.length > lines.length ? `\n• +${journeys.length - lines.length} more — full list in the report` : "";
    blocks.push({
      type: "section",
      text: {
        type: "mrkdwn",
        text: `*Doing this on the site did not work*\n${lines.join("\n")}${more}`
      }
    });
  }
  return blocks;
}

/** One line appended to the notification's fallback text, for phones and search. */
export function summaryText(findings) {
  if (!findings.length) return "";
  const parts = [];
  const restores = restoreFindings(findings);
  const journeys = journeyFindings(findings);
  if (restores.length) parts.push(`Practice data was left changed on ${restores.length} check${restores.length === 1 ? "" : "s"}`);
  if (journeys.length) parts.push(`${journeys.length} thing${journeys.length === 1 ? "" : "s"} a person does on the site did not work`);
  return assertPlainEnglish(parts.join(". "), "write-journey summary");
}

/** One threaded reply per finding, each with its own link and its own red-boxed screenshot. */
export function renderReplies(findings) {
  return findings
    .filter((item) => item.screenshot && existsSync(item.screenshot))
    .slice(0, 12)
    .map((item) => ({
      file: item.screenshot,
      title: `${item.page} — ${assertPlainEnglish(item.story || item.headline, item.page)}`.slice(0, 250),
      comment: [
        `*${item.kind === "restore" ? "‼️ " : ""}${link(item.url, item.page)}*`,
        assertPlainEnglish(item.story, item.page),
        assertPlainEnglish(item.headline, item.page),
        assertPlainEnglish(item.detail, item.page)
      ].filter(Boolean).join("\n")
    }));
}

/**
 * This lane's own Slack cases, run by `notify-slack.mjs --self-test` through the registry so the
 * proof lives with the code it protects and notify-slack.mjs keeps only the one-line hook.
 */
export function selfTest() {
  // (a) The registry must be a no-op when this lane did not run: a lane-1-only receipt is unchanged.
  const none = toFindings({}, "/nonexistent-report-dir");
  if (none.length !== 0) throw new Error("self-test: a receipt with no write journeys must contribute no findings");
  if (renderSection(none).length !== 0 || summaryText(none) !== "" || renderReplies(none).length !== 0) {
    throw new Error("self-test: with no findings this lane must add no blocks, no text and no replies");
  }

  // (b) A failed journey renders the story sentence, and leaks no SQL, table name or selector.
  const journeyOnly = [{
    severity: 1,
    kind: "journey",
    page: "Vaccination plan",
    url: "https://dashboard.mesha.sg/vaccination/plan/edit",
    story: "A manager publishes a new vaccination plan version.",
    headline: "Publishing a new vaccination plan version did not save. A manager who publishes a plan would be told it worked and find nothing there.",
    detail: "It stopped at: publish it.",
    screenshot: null
  }];
  const journeyText = JSON.stringify(renderSection(journeyOnly));
  if (!journeyText.includes("Doing this on the site did not work")) {
    throw new Error("self-test: a write-journey finding must say, in plain words, that doing this on the site did not work");
  }
  if (!journeyText.includes("Publishing a new vaccination plan version did not save")) {
    throw new Error("self-test: a write-journey finding must carry the story sentence");
  }
  if (!journeyText.includes("Vaccination plan")) throw new Error("self-test: a write-journey finding must name the screen");
  for (const leak of ["protocol_versions", "select ", "SELECT ", "button.", "GOATOS_", "/api/"]) {
    if (journeyText.includes(leak)) throw new Error(`self-test: write-journey Slack text leaked ${leak}`);
  }
  assertPlainEnglish(summaryText(journeyOnly), "self-test summary");

  // (c) A restore that failed or could not be proved is its own, higher-priority finding.
  const withRestore = toFindingsFromJourneys([
    { name: "a", page: "Feed configuration", story: "s", status: "fail", humanFailure: "Changing a feed rate did not save.", restore: { attempted: true, restored: true, verified: false } },
    { name: "b", page: "Tasks board", story: "s", status: "fail", humanFailure: "Creating a task did not stick." }
  ]);
  if (withRestore[0].kind !== "restore") throw new Error("self-test: a restore problem must outrank the journey failures");
  if (withRestore[0].severity >= withRestore[1].severity) throw new Error("self-test: the restore finding must carry the louder severity");
  const restoreBlocks = renderSection(withRestore);
  if (!JSON.stringify(restoreBlocks[0]).includes("Practice data was left changed")) {
    throw new Error("self-test: the restore finding must be the first block and must say the practice data was left changed");
  }
  if (JSON.stringify(restoreBlocks[0]).includes("Doing this on the site")) {
    throw new Error("self-test: the restore finding must be its own block, not mixed in with the journey failures");
  }
  if (!headline(withRestore).includes("Practice data was left changed")) {
    throw new Error("self-test: when practice data was left changed, that is the headline");
  }
  assertPlainEnglish(JSON.stringify(restoreBlocks), "self-test restore blocks");
  console.log("write-journey Slack finding kind: self-test passed");
}

/** Exported for the tests and the self-test: the receipt-shape -> findings step, without the file. */
export function toFindingsFromJourneys(journeys) {
  const findings = [];
  for (const journey of journeys) {
    const page = journey.page ?? "This screen";
    const restore = journey.restore ?? {};
    if (restore.attempted && (!restore.restored || !restore.verified)) {
      findings.push({
        severity: RESTORE_SEVERITY,
        kind: "restore",
        page,
        url: journey.url ?? null,
        story: journey.story ?? "",
        headline: restore.restored
          ? "The practice copy of the farm's data was put back, but it could not be proved to have come back exactly as it was."
          : "The practice copy of the farm's data was left with this check's changes in it.",
        detail: restore.humanRestore
          ?? "Until someone puts it back, nothing read from that copy should be believed, and no further check should run against it.",
        screenshot: journey.screenshot ?? null
      });
    }
    if (journey.status === "pass") continue;
    findings.push({
      severity: JOURNEY_SEVERITY,
      kind: "journey",
      page,
      url: journey.url ?? null,
      story: journey.story ?? "",
      headline: journey.humanFailure ?? "Doing this on the site did not work.",
      detail: journey.failedStep ? `It stopped at: ${journey.failedStep}.` : "",
      screenshot: journey.screenshot ?? null
    });
  }
  return findings.sort((a, b) => a.severity - b.severity);
}

// Severity decides where this lane sits in a shared message, not where its import sits.
export default { id: "write-journeys", severity: 10, issueRules: () => [], toFindings, renderSection, renderReplies, summaryText, headline, assertPlainEnglish, selfTest };
