// The mobile-webview flicker lane's Slack rendering, and ALL of it. notify-slack.mjs
// gains one import, one registry entry and one plain-English layer label; nothing else
// in that file changes, so a lane-1-only receipt renders byte-identically to what it
// rendered before this module existed.
//
// The rule this module exists to keep, and it is stricter than the other lanes':
//
//   A STILL PICTURE CANNOT SHOW FLICKER. Attaching a screenshot of a page that
//   flickers and calling it evidence is worse than attaching nothing, because it
//   looks like proof and is not. Everything this lane offers Slack is an animated
//   GIF or a filmstrip of the frames either side of the moment it went wrong.
//
// And the usual one: Slack says what a person on a phone sees. "The task filter bar
// flickers while you scroll on the phone." Not a selector, not a CSS property, not a
// check code, not a frame number, not a timestamp. Those belong in the HTML report,
// which carries the whole receipt. Every string that leaves this file goes through
// `plainEnglish` first.
import { existsSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";

export const id = "mobile-flicker";

// Lane 1 (the web) leads a mixed message. A filmed, reproduced-on-a-phone defect
// follows it closely, ahead of the lanes that default to 100.
export const severity = 70;

const RECEIPT = "mobile-flicker/mobile-flicker-receipt.json";

// ---------------------------------------------------------------------------
// The guard on our own text
// ---------------------------------------------------------------------------
// Each pattern is something an engineer writes and a farm manager cannot read.
const ENGINEERING_LEAKS = [
  [/(?:^|[\s("'])[.#][a-zA-Z][\w-]*(?=[\s.#,:>)"']|$)/, "a selector"],
  [/\b(?:backdrop-filter|position\s*:\s*\w+|z-index|will-change|transform|compositor|rasteris|GPU|repaint|reflow|viewport|px\b)/i, "a CSS or rendering term"],
  [/\bframes?\s*\d|\bf\d{3}\b|\bfps\b|\b\d+(?:\.\d+)?\s*(?:ms|Hz)\b|\b\d+\.\d{2,}s\b/i, "a frame number or a timing measurement"],
  [/\b[a-z-]+\.(?:css|mjs|js|tsx?)\b|:\d+\b/, "a file or a line number"],
  [/\bcheck\s?[A-Z]\b|\blane\s?\d|\bmobile-flicker\b|\bpinned-and-blurred\b/, "a check code"],
  [/\b[a-z]+_[a-z_]{2,}\b|\bnull\b|\bundefined\b|\bNaN\b/, "an engineering term"],
];

/** Exported so the tests can hold every catalogue sentence to the same standard. */
export function engineeringLeaks(text) {
  const value = String(text ?? "");
  return ENGINEERING_LEAKS.filter(([rx]) => rx.test(value)).map(([, what]) => what);
}

/**
 * The last line of defence. If a sentence somehow picked up jargon on its way here it
 * is replaced rather than printed: Slack showing a duller sentence is recoverable,
 * Slack showing a CSS selector to a farm manager is the thing this lane is avoiding.
 */
function plainEnglish(text, fallback = "Part of the screen flickers on the phone.") {
  const value = String(text ?? "").replace(/\s+/g, " ").trim();
  if (!value) return fallback;
  return engineeringLeaks(value).length ? fallback : value;
}

// The static check knows the bar by the name the stylesheet gives it. Slack must not.
// Anything not named here becomes the honest generic phrase rather than a selector.
const PLACE_NAMES = new Map([
  [".top", "the bar across the top of every page"],
  [".navback", "the back bar under the page title"],
  [".lt-page .lt-fbar", "the task filter bar"],
]);

/**
 * The panel's own heading is the best name a person could have for it — it is the word
 * printed on the thing they tapped. It still goes through the jargon guard, because a
 * heading could be anything, and falls back to a phrase rather than to a class name.
 */
function panelName(label) {
  const clean = String(label ?? "").replace(/\s+/g, " ").trim();
  if (!clean || clean.length > 32 || engineeringLeaks(clean).length) return "a panel that opens over the page";
  return `the ${clean.toLowerCase()} panel`;
}

function placeName(selector) {
  return PLACE_NAMES.get(String(selector ?? "").trim()) ?? "a bar that stays on screen while the page scrolls";
}

function list(names) {
  const unique = [...new Set(names)];
  if (unique.length <= 1) return unique[0] ?? "";
  return `${unique.slice(0, -1).join(", ")} and ${unique[unique.length - 1]}`;
}

// ---------------------------------------------------------------------------
// Findings
// ---------------------------------------------------------------------------
/**
 * This lane does not reinterpret lane 1's route failures, so it contributes no rules.
 * The hook stays so every finding kind has the same shape.
 */
export function issueRules() {
  return [];
}

/** Test seam: the same mapping toFindings does, without needing a file on disk. */
export function toFindingsFromReceipt(receipt) {
  if (!receipt || typeof receipt !== "object") return [];
  const findings = [];

  // The clearest finding this lane has, and the one Ravi actually filmed: a panel the
  // browser says is solid, showing the page behind it. It needs no repetition and no
  // judgement call — an opaque element can never legitimately be see-through.
  for (const run of receipt.temporal?.runs ?? []) {
    for (const panel of run.overlay?.findings ?? []) {
      const where = panelName(panel.label);
      const evidence = panel.evidence ?? {};
      findings.push({
        kind: "seen",
        what: `On ${plainEnglish(run.pageName, "one of the screens")}, ${where} goes see-through for a moment when you change a filter on the phone — the list behind it shows straight through and the words sit on top of each other, then it snaps back.`,
        moving: [evidence.gif, evidence.filmstrip].filter((file) => file && existsSync(file)),
        stills: (evidence.frames ?? []).filter((file) => file && existsSync(file)).slice(0, 4),
        note: plainEnglish(evidence.note, ""),
      });
    }
    // An overlay the check could not judge is said out loud rather than counted clean.
    for (const skipped of run.overlay?.unjudged ?? []) {
      findings.push({
        kind: "parked",
        what: `${panelName(skipped.label)} could not be checked this time, so nothing is known about it.`,
        moving: [],
        stills: [],
        note: "",
      });
    }
  }

  // The symptom: a page that was filmed and flickered. This is what a person saw.
  for (const run of receipt.temporal?.runs ?? []) {
    if (!run?.result?.flicker) continue;
    const where = plainEnglish(run.pageName, "Part of the screen");
    // "while you scroll" only earns its place when the run knows what the person was
    // doing. A recording does not, and "flickers while you use the page" is filler.
    const doing = run.whileDoing ? plainEnglish(run.whileDoing, "") : "";
    const evidence = run.evidence ?? {};
    findings.push({
      kind: "seen",
      what: `${where} flickers${doing ? ` while you ${doing}` : ""} on the phone.`,
      // Two or three frames of it, so a person can look rather than take our word.
      moving: [evidence.gif, evidence.filmstrip].filter((file) => file && existsSync(file)),
      stills: (evidence.frames ?? []).filter((file) => file && existsSync(file)).slice(0, 4),
      note: plainEnglish(evidence.note, ""),
    });
  }

  // The cause: the same combination still sitting in the stylesheet. Reported even
  // when nothing was filmed, because this is the half that catches it the day it
  // lands rather than the day somebody films it.
  // Only the elements that are STILL pinned-and-blurred at phone width. An element
  // the stylesheet switches off below 640px is a laptop-only combination, and telling
  // a farm manager their phone will flicker because of it would be a confident wrong
  // answer they have no way to check.
  const scrolling = receipt.staticCheck?.onPhone ?? receipt.staticCheck?.scrolling ?? [];
  if (scrolling.length) {
    const names = list(scrolling.map((f) => placeName(f.selector)));
    findings.push({
      kind: "cause",
      what:
        `${scrolling.length === 1 ? "One part" : `${scrolling.length} parts`} of the site — ${names} — ` +
        "stay in place while the page scrolls underneath and blur what is behind them. " +
        "Phones have to redraw that blur on every single frame, which is what makes the screen flicker. Laptops do not, which is why it never shows up on one.",
      moving: [],
      stills: [],
      note: "",
    });
  }

  // A page the check never managed to open is said out loud. A lane that quietly
  // checks nothing goes green exactly when it is blind, which is the worst failure
  // mode available to this automation.
  const parked = receipt.parked ?? [];
  if (parked.length) {
    findings.push({
      kind: "parked",
      what: `${parked.length} screen${parked.length === 1 ? "" : "s"} could not be checked for flickering at all this time, so nothing is known about ${parked.length === 1 ? "it" : "them"}.`,
      moving: [],
      stills: [],
      note: "",
    });
  }

  // Said out loud, because a quiet temporal result is not the same as a clean phone.
  if (receipt.headlessCaveat && findings.length) {
    findings.push({
      kind: "caveat",
      what: "The phone check runs on a test browser on a server, which draws the screen differently from a real phone. A quiet result here does not prove a real phone is fine.",
      moving: [],
      stills: [],
      note: "",
    });
  }
  return findings;
}

export function toFindings(receipt, receiptDir) {
  const file = path.join(receiptDir ?? ".", RECEIPT);
  if (!existsSync(file)) return [];
  try {
    return toFindingsFromReceipt(JSON.parse(readFileSync(file, "utf8")));
  } catch {
    return [];
  }
}

// ---------------------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------------------
export function headline(findings) {
  const seen = findings.filter((f) => f.kind === "seen").length;
  if (seen) return `📱 ${seen} screen${seen === 1 ? "" : "s"} that flicker${seen === 1 ? "s" : ""} on the phone`;
  if (findings.some((f) => f.kind === "cause")) return "📱 Something on the site will flicker on a phone";
  return "📱 The phone screens could not be checked this time";
}

export function summaryText(findings) {
  const real = findings.filter((f) => f.kind !== "caveat");
  if (!real.length) return "";
  return `On the phone: ${real.map((f) => f.what).join(" ")}`;
}

export function renderSection(findings) {
  if (!findings.length) return [];
  const lines = findings.filter((f) => f.kind !== "caveat").map((f) => `• ${f.what}`);
  const caveat = findings.find((f) => f.kind === "caveat");
  const anyMoving = findings.some((f) => f.moving.length || f.stills.length);
  const blocks = [{
    type: "section",
    text: { type: "mrkdwn", text: `*Flickering on the phone*\n${lines.join("\n")}` },
  }];
  const context = [];
  if (anyMoving) {
    // The one thing this lane must never let anyone assume: that the picture below
    // is a normal screenshot they can judge by looking at a single frame.
    context.push("A still picture cannot show this, so the clip below is a few frames in a row — watch the screen change and change back.");
  }
  if (caveat) context.push(caveat.what);
  if (context.length) blocks.push({ type: "context", elements: [{ type: "mrkdwn", text: context.join(" ") }] });
  return blocks;
}

/** One threaded reply per piece of evidence, and never a single still on its own. */
export function renderReplies(findings) {
  const replies = [];
  for (const finding of findings) {
    for (const file of finding.moving) {
      replies.push({
        file,
        title: finding.what.slice(0, 250),
        comment: `*📱 ${finding.what}*\nWatch it change and change back — a single picture cannot show this.`,
      });
    }
    // Only when there is no animation to show do the raw frames go up, and then they
    // go up together with the sentence that says they are consecutive frames.
    if (!finding.moving.length && finding.stills.length >= 2) {
      finding.stills.forEach((file, n) => {
        replies.push({
          file,
          title: `${finding.what} (${n + 1} of ${finding.stills.length})`.slice(0, 250),
          comment: n === 0
            ? `*📱 ${finding.what}*\nThese are frames one after another, a fraction of a second apart.${finding.note ? ` ${finding.note}` : ""}`
            : "The next frame.",
        });
      });
    }
  }
  return replies.slice(0, 10);
}

// ---------------------------------------------------------------------------
export function selfTest() {
  const assert = (condition, message) => { if (!condition) throw new Error(`self-test: mobile-flicker ${message}`); };
  // Only what a person READS is held to the plain-English rule. A reply also carries
  // the local path of the file being uploaded, and that path legitimately contains
  // this lane's own name; holding it to the rule would be testing the wrong string.
  const readable = (...parts) => JSON.stringify(parts.map((part) =>
    Array.isArray(part) ? part.map((r) => (r?.file ? `${r.title} ${r.comment}` : r)) : part));

  // The leak detector actually detects.
  assert(engineeringLeaks(".lt-page .lt-fbar is sticky").length > 0, "must catch a selector");
  assert(engineeringLeaks("backdrop-filter: blur(8px)").length > 0, "must catch a CSS property");
  assert(engineeringLeaks("flicker at frame 42").length > 0, "must catch a frame number");
  assert(engineeringLeaks("mesha-theme.css:3839").length > 0, "must catch a file and line");
  assert(engineeringLeaks("check A found it").length > 0, "must catch a check code");
  assert(engineeringLeaks("The task filter bar flickers while you scroll on the phone.").length === 0,
    "must let the sentence this lane exists to send through");

  // The map from the stylesheet's name to a place a person knows.
  assert(placeName(".lt-page .lt-fbar") === "the task filter bar", "must name the filter bar in plain English");
  assert(panelName("Filters") === "the filters panel", "a panel is named by the word printed on it");
  assert(panelName(".lt-fgroup") === "a panel that opens over the page", "never a selector, even from the page");
  assert(panelName("") === "a panel that opens over the page", "an unnamed panel still reads as English");

  // The finding Ravi filmed, end to end, from a receipt shaped as the runner writes it.
  const seeThrough = toFindingsFromReceipt({
    staticCheck: { onPhone: [] },
    temporal: { runs: [{
      pageName: "The Tasks page",
      result: { flicker: false },
      overlay: { findings: [{ label: "Filters", events: [{ seconds: 0.2 }], evidence: { gif: "/no/such.gif" } }] },
    }] },
  });
  assert(seeThrough.length === 1, "a panel that showed through is a finding on its own");
  const said = readable(renderSection(seeThrough), summaryText(seeThrough), headline(seeThrough));
  assert(engineeringLeaks(said).length === 0, `leaked ${engineeringLeaks(said).join(", ")}: ${said.slice(0, 200)}`);
  assert(said.includes("filters panel") && said.includes("see-through") && said.includes("phone"),
    `the sentence must name the panel, what happens and the device: ${said.slice(0, 200)}`);
  assert(!placeName(".something-new").startsWith("."), "an unmapped element must never render as a selector");

  // End to end on a receipt shaped exactly like the runner writes.
  const findings = toFindingsFromReceipt({
    temporal: { runs: [{ pageName: "The task filter bar", whileDoing: "scroll", result: { flicker: true }, evidence: { gif: "/no/such.gif", frames: [] } }] },
    staticCheck: { onPhone: [{ selector: ".top" }, { selector: ".navback" }, { selector: ".lt-page .lt-fbar" }] },
    headlessCaveat: "headless composites differently",
  });
  assert(findings.length === 3, `a filmed page, the cause and the caveat make three findings, got ${findings.length}`);
  const everywhere = readable(renderSection(findings), renderReplies(findings), summaryText(findings), headline(findings));
  assert(engineeringLeaks(everywhere).length === 0,
    `Slack text leaked ${engineeringLeaks(everywhere).join(", ")}: ${everywhere.slice(0, 220)}`);
  assert(everywhere.includes("task filter bar"), "a finding must name the place a person is looking at");
  assert(everywhere.includes("flickers while you scroll on the phone"),
    "the sentence this lane exists to send must come out of a real receipt unchanged");
  assert(everywhere.includes("phone"), "a finding must name the device");

  // A file that is not on this disk is never offered to Slack, and with nothing to
  // show the message does not promise a clip it cannot attach.
  assert(renderReplies(findings).length === 0, "evidence that is not on this disk must never be offered to Slack");
  assert(!everywhere.includes("still picture cannot show"),
    "with no clip to attach the message must not promise one");

  // With a real clip on disk, the warning that a still cannot show flicker is
  // mandatory: it is the whole difference between evidence and a misleading picture.
  const clip = path.join(tmpdir(), `mobile-flicker-selftest-${process.pid}.gif`);
  writeFileSync(clip, Buffer.from("GIF89a", "ascii"));
  try {
    const withClip = toFindingsFromReceipt({
      temporal: { runs: [{ pageName: "The task filter bar", whileDoing: "scroll", result: { flicker: true }, evidence: { gif: clip } }] },
      staticCheck: { scrolling: [] }
    });
    const shown = readable(renderSection(withClip), renderReplies(withClip));
    assert(shown.includes("still picture cannot show"),
      "the message must warn that a still cannot show flicker whenever it attaches frames");
    assert(renderReplies(withClip).length === 1, "a clip on disk must be offered to Slack exactly once");
    assert(engineeringLeaks(shown).length === 0, `Slack text leaked ${engineeringLeaks(shown).join(", ")}`);
  } finally {
    rmSync(clip, { force: true });
  }

  // Nothing found, nothing rendered.
  assert(renderSection([]).length === 0, "no findings must render no blocks");
  assert(renderReplies([]).length === 0, "no findings must render no replies");
  assert(summaryText([]) === "", "no findings must add no summary text");
  assert(issueRules().length === 0, "this lane must contribute no rules that could relabel a lane-1 issue");

  // A lane-1-only receipt has no flicker receipt beside it. This lane must then be
  // completely silent — that silence is what makes a lane-1 message byte-identical.
  const laneOneOnly = toFindings({ status: "fail" }, "/nonexistent-receipt-dir");
  assert(laneOneOnly.length === 0, "a receipt with no flicker run must produce no flicker findings");
  assert(renderSection(laneOneOnly).length === 0 && renderReplies(laneOneOnly).length === 0 && summaryText(laneOneOnly) === "",
    "a receipt with no flicker run must add nothing at all to the message");

  // A page that was never opened is reported as unknown, never as clean, and the
  // headline does not claim flicker when all it knows is that it could not look.
  const blind = toFindingsFromReceipt({ staticCheck: { onPhone: [] }, parked: [{ route: "tasks", why: "not signed in" }] });

  // An element that only carries the combination on a laptop is not a phone finding.
  assert(toFindingsFromReceipt({ staticCheck: { onPhone: [], scrolling: [{ selector: ".lt-page .lt-fbar" }] } }).length === 0,
    "a laptop-only combination must not be reported to a phone channel");
  assert(blind.length === 1 && blind[0].kind === "parked", "a page that could not be opened must be reported");
  assert(headline(blind).includes("could not be checked"), "the headline must not claim flicker it never saw");
  assert(engineeringLeaks(readable(renderSection(blind), summaryText(blind), headline(blind))).length === 0,
    "the parked sentence must read like English too");

  // A receipt where the static check is clean and nothing was filmed says nothing.
  assert(toFindingsFromReceipt({ staticCheck: { onPhone: [] }, temporal: { runs: [{ result: { flicker: false } }] } }).length === 0,
    "a clean run must produce no findings");
}

export default { id, severity, issueRules, toFindings, renderSection, renderReplies, headline, summaryText, selfTest };
