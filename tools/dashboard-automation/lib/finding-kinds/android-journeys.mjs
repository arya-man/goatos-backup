// Lane 5's Slack rendering, and ALL of it. notify-slack.mjs gains one import and one
// registry entry; nothing else in that file changes, so a lane-1-only receipt renders
// byte-identically to what it rendered before this module existed.
//
// The rule this module exists to keep: an Android finding says what a person holding
// the phone sees. "The photo never finished uploading and the app showed no error."
// Not a class name, not a test id, not a selector, not a stack trace, not a check
// code. Every string that leaves this file goes through `plainEnglish` first.
import { existsSync, readFileSync } from "node:fs";
import path from "node:path";

export const id = "android-journeys";

// Lane 1 (the web) leads a mixed message; a phone finding follows it. Lower runs first.
export const severity = 60;

const RECEIPT = "android-journeys/android-journeys-receipt.json";

// ---------------------------------------------------------------------------
// The guard on our own text
// ---------------------------------------------------------------------------
// Each pattern is something an engineer writes and a farm manager cannot read.
const ENGINEERING_LEAKS = [
  [/\b(?:[A-Za-z]+\.)*[A-Z][A-Za-z0-9]*(?:Test|Journeys?|Activity|Fragment|ViewModel|Screen|Receiver|Worker)\b/, "a class name"],
  [/\bsg\.mesha\.[\w.]+/, "a package name"],
  [/\b[a-z][A-Za-z0-9]*\([^)]*\)/, "a function call"],
  [/\bat [\w./$]+:\d+\b|\bstack trace\b|\bCaused by\b|Exception\b/i, "a stack trace"],
  [/data-testid|testTag|By\.(?:desc|text|res)|R\.(?:id|string)\./, "a test id or selector"],
  [/\blane[0-9]\.[a-z-]|\bP-[a-z-]{3,}\b/, "a check code"],
  [/\bandroid[:.]\w+|adb shell|am broadcast|\bAPK\b/, "a device command"],
  [/\b(?:HTTP\s?\d{3}|\d{3}\s?error|5xx|4xx|null|undefined|NaN)\b/, "an engineering term"],
  [/\b[a-z]+_[a-z_]{2,}\b/, "an internal key"]
];

/** Exported so the tests can hold every catalogue sentence to the same standard. */
export function engineeringLeaks(text) {
  const value = String(text ?? "");
  return ENGINEERING_LEAKS.filter(([rx]) => rx.test(value)).map(([, what]) => what);
}

/**
 * The last line of defence. If a sentence somehow picked up jargon on its way here,
 * it is replaced rather than printed: Slack showing nothing useful is recoverable,
 * Slack showing a stack trace to a farm manager is the thing we are avoiding.
 */
function plainEnglish(text, fallback = "Something on the phone did not work and the app did not say why.") {
  const value = String(text ?? "").replace(/\s+/g, " ").trim();
  if (!value) return fallback;
  return engineeringLeaks(value).length ? fallback : value;
}

/** "Proof upload / sync status" reads as a place; it is the one technical-ish word we keep. */
function screenName(value) {
  const name = String(value ?? "").trim();
  if (!name) return "the app";
  return engineeringLeaks(name).length ? "the app" : name;
}

// ---------------------------------------------------------------------------
// Findings
// ---------------------------------------------------------------------------
/**
 * Lane 5 does not reinterpret lane 1's route failures, so it contributes no rules.
 * The hook stays so the shape of every finding kind is the same and a later Android
 * phrasing rule has somewhere to live.
 */
export function issueRules() {
  return [];
}

export function toFindings(receipt, receiptDir) {
  const file = path.join(receiptDir ?? ".", RECEIPT);
  if (!existsSync(file)) return [];
  let parsed;
  try {
    parsed = JSON.parse(readFileSync(file, "utf8"));
  } catch {
    return [];
  }
  return (parsed.journeys ?? [])
    .filter((journey) => journey.outcome === "fail")
    .map((journey) => ({
      name: journey.name,
      screen: screenName(journey.screen),
      what: plainEnglish(journey.humanFailure),
      seen: journey.seen ? plainEnglish(journey.seen, "") : "",
      screenshot: journey.screenshot && existsSync(journey.screenshot) ? journey.screenshot : null,
      device: plainEnglish(parsed.device, "an Android phone")
    }));
}

// ---------------------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------------------
export function headline(findings) {
  const n = findings.length;
  return `📱 ${n} thing${n === 1 ? "" : "s"} a person on the phone would hit`;
}

export function summaryText(findings) {
  if (!findings.length) return "";
  return `On the phone: ${findings.map((f) => `${f.screen} — ${f.what}`).join(" · ")}`;
}

export function renderSection(findings) {
  if (!findings.length) return [];
  const lines = findings.slice(0, 8).map((f) => `• *${f.screen}* — ${f.what}`);
  const more = findings.length > lines.length ? `\n• +${findings.length - lines.length} more on the phone — every one in this thread` : "";
  return [
    {
      type: "section",
      text: {
        type: "mrkdwn",
        text: `*On the phone*\n${lines.join("\n")}${more}`
      }
    },
    {
      type: "context",
      elements: [{
        type: "mrkdwn",
        text: `Checked on ${findings[0].device}. Screenshot of each one below.`
      }]
    }
  ];
}

/** One threaded reply per finding, carrying that screen's Test Lab screenshot. */
export function renderReplies(findings) {
  return findings
    // Evidence is pulled out of Test Lab into a GCS bucket, so a screenshot path can
    // easily be a `gs://…` string rather than a file. Handing one of those to Slack
    // makes readFileSync throw INSIDE postSlack's catch, where path.basename then
    // throws again, unhandled, after the message has already posted. Nothing leaves
    // here that is not a file on this disk right now.
    .filter((f) => f.screenshot && !/^[a-z][a-z0-9+.-]*:\/\//i.test(f.screenshot) && existsSync(f.screenshot))
    .slice(0, 10)
    .map((f) => ({
      file: f.screenshot,
      title: `${f.screen} — ${f.what}`.slice(0, 250),
      comment: [
        `*📱 ${f.screen}*  on ${f.device}`,
        f.what,
        f.seen ? `_On screen:_ ${f.seen.slice(0, 200)}_` : null
      ].filter(Boolean).join("\n")
    }));
}

// ---------------------------------------------------------------------------
export function selfTest() {
  const assert = (condition, message) => { if (!condition) throw new Error(`self-test: android-journeys ${message}`); };

  // The leak detector actually detects.
  assert(engineeringLeaks("GoatOsSyncJourneys.workRecordedOffline failed").length > 0, "must catch a class name");
  assert(engineeringLeaks("at sg.mesha.goatos.Foo:41").length > 0, "must catch a stack trace");
  assert(engineeringLeaks("By.desc(\"Calendar\") not found").length > 0, "must catch a selector");
  assert(engineeringLeaks("lane5.offline-queue-survives-force-stop").length > 0, "must catch a check code");
  assert(engineeringLeaks("HTTP 500 from the server").length > 0, "must catch an engineering term");
  assert(engineeringLeaks("The photo never finished uploading and the app showed no error.").length === 0,
    "must let a plain sentence through");

  // Jargon that reaches the renderer is replaced, never printed.
  const dirty = renderReplies([{
    screen: "GoatOsSyncJourneys",
    what: "java.lang.IllegalStateException at sg.mesha.goatos.Sync:12",
    seen: "",
    screenshot: null,
    device: "a phone"
  }]);
  assert(dirty.length === 0, "a finding with no screenshot gets no reply");
  // A bucket URL is not a file. Slack must never be handed one.
  assert(renderReplies([{ screen: "Sync status", what: "It did not send.", seen: "", screenshot: "gs://bucket/shot.png", device: "a phone" }]).length === 0,
    "a bucket URL must never be offered to Slack as a screenshot");
  assert(renderReplies([{ screen: "Sync status", what: "It did not send.", seen: "", screenshot: "/no/such/file.png", device: "a phone" }]).length === 0,
    "a screenshot that is not on this disk must never be offered to Slack");
  const rendered = JSON.stringify(renderSection(toFindingsFromRows([{
    name: "upload-killed-mid-flight-resumes",
    screen: "GoatOsSyncJourneys",
    outcome: "fail",
    humanFailure: "at sg.mesha.goatos.Sync:12 threw"
  }])));
  for (const banned of ["sg.mesha", "GoatOsSyncJourneys", "threw"]) {
    assert(!rendered.includes(banned), `must not leak ${banned}`);
  }

  // Nothing found, nothing rendered.
  assert(renderSection([]).length === 0, "no findings must render no blocks");
  assert(renderReplies([]).length === 0, "no findings must render no replies");
  assert(summaryText([]) === "", "no findings must add no summary text");
  assert(issueRules().length === 0, "lane 5 must contribute no rules that could relabel a lane-1 issue");

  // A lane-1-only receipt has no Android receipt beside it. Lane 5 must then be
  // completely silent: no findings, no rules, no blocks, no text, no replies, no
  // headline. That silence is what makes a lane-1 message render byte-identically.
  const laneOneOnly = toFindings({ status: "fail" }, "/nonexistent-receipt-dir");
  assert(laneOneOnly.length === 0, "a receipt with no Android run must produce no Android findings");
  assert(renderSection(laneOneOnly).length === 0 && renderReplies(laneOneOnly).length === 0 && summaryText(laneOneOnly) === "",
    "a receipt with no Android run must add nothing at all to the message");

  // End to end: a real finding, rendered every way it can reach Slack, leaks nothing.
  const findings = toFindingsFromRows([{
    name: "upload-killed-mid-flight-resumes",
    screen: "Proof upload / sync status",
    outcome: "fail",
    humanFailure: "The photo never finished uploading and the app showed no error.",
    seen: "Uploading proof | 2 queued"
  }], "Firebase Test Lab virtual MediumPhone.arm / Android 33");
  const everywhere = JSON.stringify([renderSection(findings), renderReplies(findings), summaryText(findings), headline(findings)]);
  assert(engineeringLeaks(everywhere).length === 0,
    `Slack text leaked ${engineeringLeaks(everywhere).join(", ")}: ${everywhere.slice(0, 200)}`);
  assert(everywhere.includes("Proof upload"), "a finding must name the screen a person was on");
  assert(everywhere.includes("never finished uploading"), "a finding must say what the person saw");
}

/** Test seam: the same mapping toFindings does, without needing a file on disk. */
export function toFindingsFromRows(rows, device = "an Android phone") {
  return rows.filter((r) => r.outcome === "fail").map((r) => ({
    name: r.name,
    screen: screenName(r.screen),
    what: plainEnglish(r.humanFailure),
    seen: r.seen ? plainEnglish(r.seen, "") : "",
    screenshot: r.screenshot && existsSync(r.screenshot) ? r.screenshot : null,
    device: plainEnglish(device, "an Android phone")
  }));
}

export default { id, severity, issueRules, toFindings, renderSection, renderReplies, headline, summaryText, selfTest };
