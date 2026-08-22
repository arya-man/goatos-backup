#!/usr/bin/env node

// check-herd-signals-language.mjs — enforces the Herd Signals product claim
// boundary: docs/modules/herd-signals.md.
//
// The HoneyComm BLE ear tag exposes ONLY: tag id, BLE MAC, RSSI, battery
// voltage, tag temperature, a cumulative motion_count, sensor-OK/fault bits,
// gateway id, packet-received timestamp, and the raw advertisement payload.
// It CANNOT detect eating, rumination, sitting, standing, lying, walking,
// running, fever, body temperature, disease, or any behavior/health
// classification — see docs/modules/herd-signals.md Section 3 ("What we do
// not claim"). This module exists precisely so that boundary is real, not
// just documented: this guard is the executable half of that contract.
//
// Checks (ENFORCED):
//   banned-claim
//     A banned term (eat/eating, rumination/ruminating, sit/sitting,
//     stand/standing, lie/lying/lay, walk/walking, run/running, fever,
//     disease, diagnos*, "body temp[erature]" as a claim) used in a
//     CLAIM-SHAPED construction: "<tag> detects/detected X", "X detected",
//     "animal is eating/ruminating/...", a UI label/string literal naming
//     the behavior directly, or a field/column/JSON-key name that encodes
//     the banned concept (e.g. `is_eating`, `body_temperature_c`).
//     NOT flagged: the same words used to DENY the capability ("does not
//     detect eating", "not rumination", "never claims fever") or listed
//     inside a banned-terms/vocabulary table (this file, and
//     docs/modules/herd-signals.md Section 3/4 itself, are allowlisted by
//     path — see ALLOWLISTED_FILES). Denial detection is SENTENCE-scoped,
//     not single-line and not a flat line-count window: a negation is only
//     honored if it falls in the SAME sentence as the term (before it),
//     found by walking outward from the term's line until a real sentence
//     boundary (. ! ? ; / blank line / JSX tag edge / list-item start). This
//     is what makes a denial sentence wrapped by JSX/prose line breaks
//     ("... own sensor housing — not the\n  animal's body temperature.") read
//     correctly as one sentence, while an unrelated negation on a PRIOR,
//     already-ended sentence nearby does NOT suppress a genuine claim on the
//     next one — see buildSentenceWindow below.
//   body-temp-mislabel
//     "body temp" / "body temperature" used as a FIELD LABEL/VALUE where
//     "tag temperature" is meant (e.g. a struct field, JSON key, UI label,
//     or table column named/labeled "body_temperature" or "Body Temp").
//     The tag has no animal-contact temperature sensor; the only
//     temperature this module may report is tag temperature.
//   mock-language-leak
//     Any UI string literal in apps/admin-web/features/herd-signals/**
//     containing mock/demo/sample/synthetic. Per
//     docs/modules/herd-signals.md ("Required UI states" / "Seed and scale
//     data"), seed data may be generated at realistic scale but the SHIPPED
//     UI must never say so — no state switcher, no "mock"/"demo"/"sample"/
//     "synthetic" wording in the product.
//
// Scope (Herd Signals surfaces only):
//   backend/internal/herdsignals/**  (non-test .go)
//   apps/admin-web/features/herd-signals/**  (.ts/.tsx)
//   contracts/openapi/app-api.yaml — only the herd-signals slice, detected
//     by scanning between a `herd-signals` / `HerdSignals` marker and the
//     next top-level path/schema marker at the same indentation depth (a
//     single shared file, so this guard must not flag unrelated slices).
//   docs/modules/herd-signals.md
//   mock/herd-signals-mock.html
//
// Modes:
//   (default)     scan the tree, fail on any violation.
//   --self-test   run the built-in adversarial fixtures (fail-case +
//                 pass-case, both from tools/agent-hooks/test-fixtures/
//                 herd-signals-language/).
//
// Exception (must be COMPLETE — owner, issue, scope, expiry all present):
//   herd-signals-language:ignore: owner=<name> issue=<url|id> scope=<why> expiry=<YYYY-MM-DD>
//
// REMAINING BLIND SPOTS (documented, cannot be caught by this static scan):
// - Composition across variables/helpers: `label := "eat" + "ing"` or a
//   claim assembled from a template/i18n table keyed by a code that is
//   itself innocuous (e.g. `t("hs.behavior.1")` resolving elsewhere to
//   "Eating detected") is invisible to a line-level regex scan.
// - Non-literal runtime strings: an LLM-, config-, or CMS-driven copy
//   string is not visible to a static scan of source files.
// - Semantic paraphrase: "grazing", "browsing", "chewing", "resting",
//   "sleeping", "napping" and similar synonyms are NOT in the banned-term
//   list below (only the literal terms named in Section 3 are), so a
//   paraphrase that avoids the literal banned words entirely will pass.
//   docs/modules/herd-signals.md Section 4 ("Approved product vocabulary")
//   is the human-authority backstop for this gap; review must still check
//   for paraphrase, this guard cannot.
// - The contracts/openapi/app-api.yaml slice detection is a best-effort
//   marker scan of one large shared file; a herd-signals schema fragment
//   that does not contain any of the marker strings this guard looks for
//   (see HERD_SIGNALS_YAML_START) will not be scanned.
// - It cannot verify negation phrasing is TRUE (i.e. that a denial like
//   "does not detect eating" accurately reflects the code) — only that the
//   sentence is grammatically a denial, not a claim.
// - Negation scope is SENTENCE-scoped, not line-scoped and not a flat
//   line-count window. A denial is recognized only if the negation word and
//   the banned term/"body temp" fall in the SAME sentence, with the negation
//   BEFORE the term. "Same sentence" is found by walking outward from the
//   candidate line (bounded by WINDOW_BEFORE/WINDOW_AFTER lines, see the
//   constants below) and stopping at a real sentence boundary: end-of-line
//   punctuation (. ! ? ;), a blank line, a JSX tag boundary (a line ending in
//   `>` or starting with `<`), or a list-item start (`-`, `*`, `1.`). A
//   newline INSIDE a sentence (no boundary punctuation yet) is NOT a
//   boundary, which is what makes wrapped prose/JSX text still read as one
//   sentence ("... own sensor housing -- not the\n  animal's body
//   temperature." stays one denial). This closes the earlier flat-window
//   false negative where an unrelated "no"/"not" on a PRIOR, already-ended
//   sentence (e.g. a preceding comment ending in a period, "// There is no
//   cursor on the first page.") could silently suppress a genuine claim on
//   the next sentence merely by being nearby.
//   Sentence boundaries are also detected MID-LINE, not just at line ends:
//   each line is first split into sentence fragments on `. `/`! `/`? `
//   (punctuation followed by whitespace and then a capital letter, quote, or
//   end-of-string), so two sentences sharing one physical line ("No cursor
//   here. The gateway confirms the animal is eating right now.") are judged
//   as two separate sentences, not one blob -- this closes the earlier
//   same-line exploit where an unrelated denial and a genuine claim shared
//   a line. The splitter deliberately does NOT split on a decimal number
//   (`25.1 C`), a dotted identifier/call (`item.motion_count`,
//   `Number(x).toFixed(2)`) -- both lack whitespace right after the dot, so
//   they never match the split pattern -- or a short list of known
//   abbreviations (e.g., i.e., etc., vs., approx., fig., mr., dr., ...).
//   Residual blind spots (static heuristic, not a parser):
//   - An abbreviation NOT in the ABBREVIATIONS list, or any other
//     punctuation-then-capital-letter sequence that a human would not read
//     as a sentence break, can still be mis-split; this only degrades to
//     narrower sentence scope (more conservative denial matching), not to a
//     wrong claim/denial verdict on unrelated content the way the two prior
//     defects did.
//   - It caps how far outward it will walk at WINDOW_BEFORE/WINDOW_AFTER
//     lines even if no boundary is found in that span, so a genuinely
//     boundary-free multi-page run-on comment could still merge unrelated
//     context past that cap; widen the constants or use the
//     `herd-signals-language:ignore:` escape hatch for that rare shape.
//   - It cannot verify that a mid-line split point is semantically correct
//     beyond the punctuation/capitalization/abbreviation heuristic above —
//     an unusual style (all-lowercase sentence starts, sentences ending in
//     a closing quote before the period, etc.) can fool it in either
//     direction.
// - CLOSED (round 4): every banned-term and "body temp" OCCURRENCE on a
//   line is now judged independently, in the specific sentence fragment
//   that contains it (see fragmentOrdinalForOffset), not just "the first
//   occurrence on this line" and not "does this line contain a denial
//   anywhere". A denial sentence sharing a line with an unrelated claim
//   sentence ("the tag does not detect eating. The gateway confirms the
//   animal is eating now.") used to have the denial's verdict silently
//   cover the later, unrelated claim; each occurrence now gets its own
//   fragment-scoped sentence window and its own verdict.

import { readFileSync, existsSync, readdirSync } from "node:fs";
import { join, relative, resolve, extname } from "node:path";

const repo = resolve(import.meta.dirname, "../..");

const IGNORE_RE =
  /herd-signals-language:ignore:\s*owner=\S+\s+issue=\S+\s+scope=[^\n]*?\s+expiry=\d{4}-\d{2}-\d{2}/;

// Files that are ALLOWED to contain banned terms outright, because their entire
// job is to state the boundary (deny the capability) or enumerate the banned
// vocabulary itself. Denial lines inside these files still must read as
// denials to a human, but this guard does not line-scan them for the
// banned-claim check at all -- they are the source of truth for the boundary,
// not a surface the boundary protects.
const ALLOWLISTED_FILES = [
  "docs/modules/herd-signals.md",
  "tools/agent-hooks/check-herd-signals-language.mjs",
];

// Banned behavior/health terms (docs/modules/herd-signals.md Section 3).
// Word-boundary matched, singular/plural/participle forms.
// Longest-form-first within each family: JS regex alternation picks the
// FIRST alternative that matches at a given starting position, not the
// longest, so "eat" ahead of "eating" would match only the "eat" prefix of
// "Eating" and break every downstream claim-shape regex built from that
// short match. Order matters here.
const BANNED_TERMS = [
  "eating", "eats", "\\beat\\b",
  "rumination", "ruminating", "ruminate",
  "sitting", "sits", "\\bsit\\b",
  "standing", "stands", "\\bstand\\b",
  "lying", "lies", "\\blie\\b", "\\blay\\b",
  "walking", "walks", "\\bwalk\\b",
  "running", "runs", "\\brun\\b",
  "feverish", "fever",
  "diseased", "disease",
  "diagnos\\w*",
];
const BANNED_TERM_RE = new RegExp(`(${BANNED_TERMS.join("|")})`, "i");

// "body temp[erature]" specifically, for the body-temp-mislabel check.
const BODY_TEMP_RE = /\bbody[\s_-]?temp(?:erature)?\b/i;

// Constructions that mark a line as a DENIAL rather than a claim. If any of
// these match near the banned term, the line is a negation and must NOT be
// flagged (this is the "match the assertion, not the word" requirement).
const NEGATION_RE =
  /\b(?:not|never|no|cannot|can't|does not|doesn't|isn't|is not|without)\b/i;

// Hard cap on how far outward buildSentenceWindow will walk looking for a
// sentence boundary, even if none is found. This is a safety bound, not the
// primary mechanism -- the primary mechanism is REAL sentence-boundary
// detection (see buildSentenceWindow); these caps only stop a pathological
// boundary-free run of lines from pulling in unbounded context. See the
// header comment's "Residual blind spot" note.
const WINDOW_BEFORE = 3;
const WINDOW_AFTER = 2;

// Strip a line's leading comment/JSX-text noise so joined sentence text
// reads as plain prose instead of "// foo /* bar" — improves negation/term
// adjacency matching without changing which words are present.
function stripLineNoise(line) {
  return line.replace(/^\s*(?:\{\/\*|\/\*|\*\/|\/\/|\*)\s?/, "").trim();
}

// Does `line` (already stripped) END with a real sentence terminator, or is
// it blank? Either ends the sentence the line belongs to.
function lineEndsSentence(strippedLine) {
  if (strippedLine === "") return true;
  return /[.!?;]\s*$/.test(strippedLine);
}

// Does `line` (already stripped) look like the START of a new prose block —
// a JSX element, or a list item — regardless of whether the previous line
// ended with punctuation? JSX/markdown authors routinely start a new
// "sentence" (a new element, a new bullet) without a preceding period.
function lineStartsNewBlock(strippedLine) {
  return /^</.test(strippedLine) || /^[-*]\s/.test(strippedLine) || /^\d+\.\s/.test(strippedLine);
}

// Does `line` (already stripped) look like the END of a JSX element (closes
// with `>` or `/>`)? Crossing out of a JSX tag is a boundary the same way
// crossing into one is.
function lineEndsBlock(strippedLine) {
  return /(?:\/>|>)\s*$/.test(strippedLine);
}

// Is there a real sentence boundary BETWEEN `prevStripped` (the earlier
// line) and `nextStripped` (the later line)? Used both walking backward and
// walking forward from the candidate line.
function isSentenceBoundaryBetween(prevStripped, nextStripped) {
  return (
    lineEndsSentence(prevStripped) ||
    lineStartsNewBlock(nextStripped) ||
    lineEndsBlock(prevStripped)
  );
}

// Known abbreviations whose internal/trailing period is NOT a sentence
// boundary even though it is followed by whitespace (e.g. "... that is,
// i.e. the tag ..." must not split after "i.e."). Checked against the run
// of letters immediately before the candidate period, and against a
// 4-character tail check for two-letter dotted forms like "e.g"/"i.e"
// whose OWN internal dot would otherwise also look like a candidate split
// (it never does here, because that internal dot has no following
// whitespace — see splitIntoSentenceFragments).
const ABBREVIATIONS = new Set([
  "e.g", "i.e", "etc", "vs", "approx", "fig", "mr", "mrs", "ms", "dr", "st", "jr", "sr",
]);

// Split ONE already-stripped line into sentence fragments, so two sentences
// sharing a single physical line ("No cursor here. The gateway confirms the
// animal is eating right now.") are judged as separate sentences instead of
// one blob. A split point requires: `.`/`!`/`?` immediately followed by
// whitespace (so "item.motion_count" and "25.1 C" never qualify -- neither
// has whitespace right after the dot), AND the next non-space content
// looking like a new sentence (capital letter, quote, backtick, or open
// paren, or end of the line), AND the word immediately before the
// punctuation not being a known abbreviation. Trailing punctuation stays
// attached to the fragment it ends, so a single-fragment line's END state
// (does it end mid-sentence or not) is unchanged from before this split was
// introduced -- cross-line boundary detection keeps working the same way.
function splitIntoSentenceFragments(strippedLine) {
  if (strippedLine === "") return [""];
  const text = strippedLine;
  const fragments = [];
  let start = 0;
  const splitRe = /[.!?](\s+)/g;
  let m;
  while ((m = splitRe.exec(text)) !== null) {
    const splitAt = m.index + m[0].length;
    const before = text.slice(0, m.index);
    const wordMatch = before.match(/([A-Za-z]+)$/);
    const word = wordMatch ? wordMatch[1].toLowerCase() : "";
    const tail4 = before.slice(-4).toLowerCase();
    const isAbbreviation =
      ABBREVIATIONS.has(word) || tail4.endsWith("e.g") || tail4.endsWith("i.e");
    if (isAbbreviation) continue;
    const after = text.slice(splitAt);
    const looksLikeSentenceStart = after === "" || /^[A-Z"'‘“`(]/.test(after);
    if (!looksLikeSentenceStart) continue;
    fragments.push(text.slice(start, splitAt).trim());
    start = splitAt;
  }
  const last = text.slice(start).trim();
  if (last !== "" || fragments.length === 0) fragments.push(last);
  return fragments;
}

// Build the SENTENCE containing ONE SPECIFIC fragment of line i --
// `fragmentIndexOnLine` is the 0-based ordinal among the fragments that
// splitIntoSentenceFragments(stripLineNoise(lines[i])) itself produces (the
// SAME split every atom below is built from, so the ordinal always lands on
// the correct fragment even when a line holds two+ independent sentences
// with the same banned term repeated in more than one of them -- see the
// header comment's history of this guard for why identifying "the fragment
// that CONTAINS this term's text" by substring search was not enough: it
// always resolved to the FIRST matching fragment, so a denial anywhere on
// the line could cover every later claim on that same line). Each line in
// the bounded range is first split into sentence fragments, producing a
// flat list of atoms; we then walk backward/forward from the identified
// atom, merging adjacent atoms only while no real sentence boundary
// (fragment-end punctuation, blank line, JSX tag edge, list-item start)
// separates them, capped at WINDOW_BEFORE/WINDOW_AFTER LINES as a safety
// bound (not an atom-count bound -- a line normally holds at most a couple
// of sentences). Because within-line splits keep the terminal punctuation
// attached to the fragment it closes, the exact same
// isSentenceBoundaryBetween check works uniformly whether adjacent atoms
// came from the same physical line or from different, adjacent lines --
// that is what makes wrapped cross-line denials keep working while same-line
// unrelated sentences no longer bleed into each other, AND (this round's
// fix) while two independent sentences on one line -- a denial and a claim,
// or a claim and a denial -- are judged separately instead of the first
// fragment's verdict silently covering every later one.
function buildSentenceWindow(lines, i, before, after, fragmentIndexOnLine) {
  const segStart = Math.max(0, i - before);
  const segEnd = Math.min(lines.length - 1, i + after);

  const atoms = []; // { text, lineIdx }
  for (let k = segStart; k <= segEnd; k++) {
    const stripped = stripLineNoise(lines[k]);
    for (const frag of splitIntoSentenceFragments(stripped)) {
      atoms.push({ text: frag, lineIdx: k });
    }
  }

  let centerAtomIdx = -1;
  if (typeof fragmentIndexOnLine === "number") {
    let seenOnLine = 0;
    for (let k = 0; k < atoms.length; k++) {
      if (atoms[k].lineIdx !== i) continue;
      if (seenOnLine === fragmentIndexOnLine) {
        centerAtomIdx = k;
        break;
      }
      seenOnLine++;
    }
  }
  if (centerAtomIdx === -1) {
    // Fallback: no fragment ordinal supplied, or it's out of range (should
    // not happen in practice) -- use the LAST atom on line i so the term's
    // own line is still represented in the sentence window.
    for (let k = atoms.length - 1; k >= 0; k--) {
      if (atoms[k].lineIdx === i) {
        centerAtomIdx = k;
        break;
      }
    }
  }
  if (centerAtomIdx === -1) return stripLineNoise(lines[i]);

  let startIdx = centerAtomIdx;
  for (let k = centerAtomIdx - 1; k >= 0; k--) {
    if (isSentenceBoundaryBetween(atoms[k].text, atoms[k + 1].text)) break;
    startIdx = k;
  }

  let endIdx = centerAtomIdx;
  for (let k = centerAtomIdx + 1; k < atoms.length; k++) {
    if (isSentenceBoundaryBetween(atoms[k - 1].text, atoms[k].text)) break;
    endIdx = k;
  }

  return atoms
    .slice(startIdx, endIdx + 1)
    .map((a) => a.text)
    .join(" ")
    .replace(/\s+/g, " ")
    .trim();
}

// Given the stripped text of line i and the character offset (within that
// SAME stripped text) of a term occurrence, return the 0-based ordinal of
// the sentence fragment (as produced by splitIntoSentenceFragments) that
// contains that offset. Walks the fragments left-to-right re-locating each
// one via indexOf from a monotonic cursor -- robust to the trimming
// splitIntoSentenceFragments performs, since fragments are trimmed
// substrings of the original in left-to-right order with no overlaps.
function fragmentOrdinalForOffset(strippedLine, fragments, offset) {
  let cursor = 0;
  for (let idx = 0; idx < fragments.length; idx++) {
    const frag = fragments[idx];
    if (frag === "") continue;
    const fragStart = strippedLine.indexOf(frag, cursor);
    if (fragStart === -1) continue;
    const fragEnd = fragStart + frag.length;
    if (offset >= fragStart && offset < fragEnd) return idx;
    cursor = fragEnd;
  }
  // Fallback: offset past the last located fragment (shouldn't normally
  // happen) -- attribute it to the last fragment.
  return Math.max(0, fragments.length - 1);
}

// Does a negation word precede the given term's first occurrence anywhere in
// `text` (a single line OR a joined window)? Shared by the banned-claim and
// body-temp-mislabel checks so both use identical denial-scope logic.
function negationPrecedesTerm(text, term) {
  if (!NEGATION_RE.test(text)) return false;
  const lower = text.toLowerCase();
  const negIdx = lower.search(NEGATION_RE);
  const termIdx = lower.indexOf(term.toLowerCase());
  return negIdx !== -1 && termIdx !== -1 && negIdx < termIdx;
}

// A line that is a banned-terms list / vocabulary enumeration (comma or
// pipe separated list of the banned words themselves), rather than prose
// making a claim. Conservative: only matches an actual list literal.
const VOCAB_LIST_RE =
  /BANNED_TERMS|banned[\s_-]?terms?|forbidden[\s_-]?terms?|\[\s*["'`][a-z]+["'`]\s*,/i;

// CLAIM-SHAPED constructions: the banned term appearing as an assertion,
// not a denial. `text` is a SENTENCE WINDOW (see buildSentenceWindow) for the
// banned-claim check, not necessarily a single raw line -- a multi-line
// denial sentence is joined into one string before this function ever sees
// it, so the negation-scope logic below works the same whether the negation
// and the term were on the same physical line or several lines apart within
// the window. Matches:
//   - "<subject> detect(s|ed) <term>" / "<term> detect(ed|ion)"
//   - "is/was <term>ing" (e.g. "animal is eating")
//   - a quoted UI-string literal containing the term as a headline claim
//     ("Eating detected", "Currently ruminating")
//   - a field/key/column name encoding the concept: is_eating, isEating,
//     eating_detected, body_temperature_c, fever_flag, etc.
function isClaimShaped(text, term) {
  if (NEGATION_RE.test(text) && !/detect(?:s|ed|ion)?\s+\w*/i.test(text)) {
    // Negation present and this isn't a "detects X" construction that
    // could still be a claim despite an unrelated "not" elsewhere in the
    // window (rare; err toward treating bare negation as a denial).
    return false;
  }
  // Even with a "detect" verb present, if the negation precedes the term
  // anywhere in the window (the common denial shape: "does not detect
  // eating", including when wrapped across lines), treat as denial.
  if (negationPrecedesTerm(text, term)) return false;
  const detectClaim = new RegExp(
    `detect(?:s|ed|ion)?\\s+(?:\\w+\\s+){0,2}${term}|${term}\\s+detect(?:s|ed|ion)?`,
    "i"
  );
  const stateClaim = new RegExp(`\\bis\\s+${term}|\\bwas\\s+${term}`, "i");
  const fieldNameClaim = new RegExp(
    `\\b(?:is_?${term}|${term}_?detected|${term}_?flag|${term}_?status)\\b`,
    "i"
  );
  const quotedClaim = new RegExp(`["'\`][^"'\`]*\\b${term}\\b[^"'\`]*["'\`]`, "i");
  if (detectClaim.test(text) || stateClaim.test(text) || fieldNameClaim.test(text)) {
    return true;
  }
  // A quoted literal containing the term is only a claim if it is not
  // itself a denial sentence (already excluded above) and not a
  // vocabulary/allowlist entry.
  if (quotedClaim.test(text) && !VOCAB_LIST_RE.test(text)) return true;
  return false;
}

function findingsForSource(source, relPath) {
  const findings = [];
  if (IGNORE_RE.test(source)) {
    // File-wide ignore is not supported; ignores are line-scoped only. This
    // branch intentionally does nothing (kept for readability/symmetry with
    // other guards' ignore handling).
  }
  const lines = source.split("\n");
  lines.forEach((text, i) => {
    const lineNo = i + 1;
    if (IGNORE_RE.test(text)) return;
    const trimmed = text.trim();
    if (!trimmed) return;

    // Line-level vocabulary-list escape hatch (a banned-terms table/array
    // literal) skips BOTH checks entirely for this line, same as before.
    const lineIsVocabList = VOCAB_LIST_RE.test(text);
    const stripped = stripLineNoise(text);
    const lineFragments = splitIntoSentenceFragments(stripped);

    // banned-claim -- judge EVERY occurrence of a banned term on this line
    // INDEPENDENTLY, each in the fragment (sentence) that actually contains
    // it. A single `.match()` here would only ever see the FIRST occurrence,
    // which is how a denial anywhere on a line ("the tag does not detect
    // eating. The gateway confirms the animal is eating now.") used to
    // silently cover a later, unrelated claim on the SAME line -- the
    // negation lookup was correct, but the occurrence being judged was the
    // wrong one (always the first).
    if (!lineIsVocabList) {
      const globalBannedRe = new RegExp(BANNED_TERM_RE.source, "gi");
      let bm;
      while ((bm = globalBannedRe.exec(stripped)) !== null) {
        if (bm[0] === "") {
          globalBannedRe.lastIndex++;
          continue;
        }
        const term = bm[0];
        const fragIdx = fragmentOrdinalForOffset(stripped, lineFragments, bm.index);
        // Sentence window keyed to THIS specific occurrence's fragment --
        // see buildSentenceWindow.
        const window = buildSentenceWindow(lines, i, WINDOW_BEFORE, WINDOW_AFTER, fragIdx);
        if (!VOCAB_LIST_RE.test(window) && isClaimShaped(window, term.replace(/\\b/g, ""))) {
          findings.push({
            line: lineNo,
            rule: "banned-claim",
            message: `claim-shaped use of banned term "${term}" — Herd Signals cannot detect/classify behavior; deny it explicitly ("does not detect ...") or remove the claim`,
          });
        }
      }
    }

    // body-temp-mislabel -- same per-occurrence treatment as banned-claim
    // above: "body temp[erature]" used as a label/field, not inside a
    // denial, judged in the fragment (sentence) that actually contains each
    // specific occurrence, so a negation on an adjacent wrapped line or an
    // earlier fragment of the SAME line is recognized, while an unrelated
    // denial covering a DIFFERENT occurrence on the same line no longer
    // silently suppresses this one.
    if (!lineIsVocabList) {
      const globalBodyTempRe = new RegExp(BODY_TEMP_RE.source, "gi");
      let tm;
      while ((tm = globalBodyTempRe.exec(stripped)) !== null) {
        if (tm[0] === "") {
          globalBodyTempRe.lastIndex++;
          continue;
        }
        const term = tm[0];
        const fragIdx = fragmentOrdinalForOffset(stripped, lineFragments, tm.index);
        const window = buildSentenceWindow(lines, i, WINDOW_BEFORE, WINDOW_AFTER, fragIdx);
        const isDenial = negationPrecedesTerm(window, term);
        const looksLikeFieldOrLabel =
          /["'`][^"'`]*body[\s_-]?temp/i.test(text) || // quoted UI label
          /\bbody_?temp(?:erature)?_?\w*\s*[:=]/i.test(text) || // field/key assignment
          /\b(?:type|struct|Body_?Temp|BodyTemp)\b.*body[\s_-]?temp/i.test(text);
        if (!VOCAB_LIST_RE.test(window) && !isDenial && (looksLikeFieldOrLabel || !NEGATION_RE.test(window))) {
          findings.push({
            line: lineNo,
            rule: "body-temp-mislabel",
            message:
              'label/field says "body temp[erature]" — the tag has no animal-contact temperature sensor; the only field this module may report is "tag temperature" (docs/modules/herd-signals.md Section 1/3)',
          });
        }
      }
    }

    // mock-language-leak — admin-web UI strings only, applied by caller scoping.
    if (relPath.startsWith("apps/admin-web/features/herd-signals/")) {
      const quoted = text.match(/["'`]([^"'`]*)["'`]/g) || [];
      for (const q of quoted) {
        if (/\b(mock|demo|sample|synthetic)\b/i.test(q)) {
          findings.push({
            line: lineNo,
            rule: "mock-language-leak",
            message: `UI string literal contains mock/demo/sample/synthetic wording (${q.trim()}) — per docs/modules/herd-signals.md "Required UI states", seed data may be generated at scale but the shipped UI must never say so`,
          });
          break;
        }
      }
    }
  });
  return findings;
}

// --- YAML slice extraction for the shared contracts/openapi/app-api.yaml ---
// Extract only lines that look like they belong to the herd-signals slice:
// from a line matching the start marker to the next top-level (2-space
// indented) path/schema key, or a line with equal/lesser indentation whose
// key does not mention herd-signals.
const HERD_SIGNALS_YAML_START = /herd[-_]signals|HerdSignals/;

function extractHerdSignalsYamlSlice(source) {
  const lines = source.split("\n");
  const out = [];
  let inSlice = false;
  let sliceIndent = null;
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const indentMatch = line.match(/^(\s*)/);
    const indent = indentMatch ? indentMatch[1].length : 0;
    if (!inSlice) {
      if (HERD_SIGNALS_YAML_START.test(line)) {
        inSlice = true;
        sliceIndent = indent;
        out.push({ text: line, lineNo: i + 1 });
      }
      continue;
    }
    // Stay in slice while indentation is deeper than the marker line, OR the
    // line still mentions herd-signals at the same/lesser indent (adjacent
    // path key). Exit once we hit a same-or-lesser-indent line that does NOT
    // mention herd-signals (a different path/schema).
    if (indent > sliceIndent || line.trim() === "") {
      out.push({ text: line, lineNo: i + 1 });
      continue;
    }
    if (indent <= sliceIndent && HERD_SIGNALS_YAML_START.test(line)) {
      sliceIndent = indent;
      out.push({ text: line, lineNo: i + 1 });
      continue;
    }
    inSlice = false;
    sliceIndent = null;
  }
  return out;
}

function findingsForYamlSlice(source) {
  const slice = extractHerdSignalsYamlSlice(source);
  const sliceLines = slice.map((s) => s.text);
  const findings = [];
  slice.forEach(({ text, lineNo }, idx) => {
    // Same sentence-scoping and per-occurrence judging as findingsForSource:
    // a YAML `description:` block can wrap a denial sentence across
    // adjacent lines, or share a line with an unrelated sentence, and each
    // term occurrence must be judged in ITS OWN fragment, not a shared
    // line/window blob and not just "the first occurrence on this line".
    const lineIsVocabList = VOCAB_LIST_RE.test(text);
    const stripped = stripLineNoise(text);
    const lineFragments = splitIntoSentenceFragments(stripped);

    if (!lineIsVocabList) {
      const globalBannedRe = new RegExp(BANNED_TERM_RE.source, "gi");
      let bm;
      while ((bm = globalBannedRe.exec(stripped)) !== null) {
        if (bm[0] === "") {
          globalBannedRe.lastIndex++;
          continue;
        }
        const term = bm[0];
        const fragIdx = fragmentOrdinalForOffset(stripped, lineFragments, bm.index);
        const window = buildSentenceWindow(sliceLines, idx, WINDOW_BEFORE, WINDOW_AFTER, fragIdx);
        if (!VOCAB_LIST_RE.test(window) && isClaimShaped(window, term.replace(/\\b/g, ""))) {
          findings.push({
            line: lineNo,
            rule: "banned-claim",
            message: `claim-shaped use of banned term "${term}" in the herd-signals OpenAPI slice`,
          });
        }
      }
    }

    if (!lineIsVocabList) {
      const globalBodyTempRe = new RegExp(BODY_TEMP_RE.source, "gi");
      let tm;
      while ((tm = globalBodyTempRe.exec(stripped)) !== null) {
        if (tm[0] === "") {
          globalBodyTempRe.lastIndex++;
          continue;
        }
        const term = tm[0];
        const fragIdx = fragmentOrdinalForOffset(stripped, lineFragments, tm.index);
        const window = buildSentenceWindow(sliceLines, idx, WINDOW_BEFORE, WINDOW_AFTER, fragIdx);
        const isDenial = negationPrecedesTerm(window, term);
        if (!VOCAB_LIST_RE.test(window) && !isDenial) {
          findings.push({
            line: lineNo,
            rule: "body-temp-mislabel",
            message: 'contract field/description says "body temp[erature]" in the herd-signals OpenAPI slice — should be "tag temperature"',
          });
        }
      }
    }
  });
  return findings;
}

// --- File discovery ---

function walk(dir, exts) {
  const out = [];
  if (!existsSync(dir)) return out;
  const stack = [dir];
  while (stack.length) {
    const d = stack.pop();
    let entries;
    try {
      entries = readdirSync(d, { withFileTypes: true });
    } catch {
      continue;
    }
    for (const entry of entries) {
      const full = join(d, entry.name);
      if (entry.isDirectory()) {
        stack.push(full);
      } else if (entry.isFile() && exts.includes(extname(entry.name))) {
        out.push(full);
      }
    }
  }
  return out;
}

function collectTargets() {
  const targets = [];

  const backendDir = join(repo, "backend/internal/herdsignals");
  for (const abs of walk(backendDir, [".go"])) {
    const rel = relative(repo, abs);
    if (rel.endsWith("_test.go")) continue;
    targets.push({ abs, rel, kind: "source" });
  }

  const adminDir = join(repo, "apps/admin-web/features/herd-signals");
  for (const abs of walk(adminDir, [".ts", ".tsx"])) {
    const rel = relative(repo, abs);
    targets.push({ abs, rel, kind: "source" });
  }

  const docsFile = join(repo, "docs/modules/herd-signals.md");
  if (existsSync(docsFile)) {
    targets.push({ abs: docsFile, rel: "docs/modules/herd-signals.md", kind: "allowlisted" });
  }

  const mockFile = join(repo, "mock/herd-signals-mock.html");
  if (existsSync(mockFile)) {
    targets.push({ abs: mockFile, rel: "mock/herd-signals-mock.html", kind: "source" });
  }

  return targets;
}

function scanTree() {
  const findings = [];
  for (const t of collectTargets()) {
    const rel = t.rel;
    let source;
    try {
      source = readFileSync(t.abs, "utf8");
    } catch {
      continue;
    }
    if (ALLOWLISTED_FILES.includes(rel)) continue;
    for (const f of findingsForSource(source, rel)) findings.push({ ...f, rel });
  }

  const contractFile = join(repo, "contracts/openapi/app-api.yaml");
  if (existsSync(contractFile)) {
    const source = readFileSync(contractFile, "utf8");
    for (const f of findingsForYamlSlice(source)) {
      findings.push({ ...f, rel: "contracts/openapi/app-api.yaml" });
    }
  }

  return findings;
}

// --- Self-test ---

function readFixture(name) {
  const p = join(repo, "tools/agent-hooks/test-fixtures/herd-signals-language", name);
  return readFileSync(p, "utf8");
}

function selfTest() {
  const failFixture = readFixture("Bad_ClaimShapedCopy.go");
  const passFixture = readFixture("Good_DenialAndVocabList.go");

  const failFindings = findingsForSource(failFixture, "backend/internal/herdsignals/app/fixture.go");
  if (!failFindings.some((f) => f.rule === "banned-claim")) {
    throw new Error(
      `self-test: expected banned-claim on FAIL fixture, got: ${JSON.stringify(failFindings)}`
    );
  }
  if (!failFindings.some((f) => f.rule === "body-temp-mislabel")) {
    throw new Error(
      `self-test: expected body-temp-mislabel on FAIL fixture, got: ${JSON.stringify(failFindings)}`
    );
  }

  const passFindings = findingsForSource(passFixture, "backend/internal/herdsignals/app/fixture.go");
  if (passFindings.length) {
    throw new Error(
      `self-test: false positive on PASS fixture (same words, negation/denial context): ${JSON.stringify(passFindings)}`
    );
  }

  const mockFixtureBad = `const label = "Sample data shown for demo";\n`;
  const mockFindings = findingsForSource(
    mockFixtureBad,
    "apps/admin-web/features/herd-signals/live-panel.tsx"
  );
  if (!mockFindings.some((f) => f.rule === "mock-language-leak")) {
    throw new Error(
      `self-test: expected mock-language-leak on admin-web mock/demo/sample string, got: ${JSON.stringify(mockFindings)}`
    );
  }

  const mockFixtureGood = `const label = "Live tags";\n`;
  const mockFindingsGood = findingsForSource(
    mockFixtureGood,
    "apps/admin-web/features/herd-signals/live-panel.tsx"
  );
  if (mockFindingsGood.length) {
    throw new Error(
      `self-test: false positive on clean admin-web string: ${JSON.stringify(mockFindingsGood)}`
    );
  }

  // Wrapped-denial regression fixtures (the reported real-world defect:
  // herd-signals-drawer.tsx:218's mandatory "not the animal's body
  // temperature" disclaimer, split across two JSX text lines, used to
  // false-positive as body-temp-mislabel). Each of these MUST produce zero
  // findings -- a denial split across lines is still a denial.
  const wrappedDenialFixtures = [
    ["Good_WrappedDenialJSX.tsx", "apps/admin-web/features/herd-signals/herd-signals-drawer.tsx"],
    ["Good_WrappedDenial3Lines.tsx", "apps/admin-web/features/herd-signals/herd-signals-drawer.tsx"],
    ["Good_WrappedDenial.go", "backend/internal/herdsignals/app/fixture.go"],
    ["Good_WrappedDenial.md", "docs/modules/herd-signals-wrapped-denial-fixture.md"],
  ];
  const wrappedDenialResults = [];
  for (const [file, relPath] of wrappedDenialFixtures) {
    const findingsHere = findingsForSource(readFixture(file), relPath);
    if (findingsHere.length) {
      throw new Error(
        `self-test: false positive on wrapped-denial fixture ${file} (denial split across lines must still pass): ${JSON.stringify(findingsHere)}`
      );
    }
    wrappedDenialResults.push([file, findingsHere.length]);
  }

  // Inverse: a genuine claim split across lines (no negation anywhere) must
  // still be caught. Fixing the false positive above must not weaken the
  // check into missing a wrapped claim.
  const wrappedClaimFindings = findingsForSource(
    readFixture("Bad_WrappedClaim.tsx"),
    "apps/admin-web/features/herd-signals/herd-signals-drawer.tsx"
  );
  if (!wrappedClaimFindings.some((f) => f.rule === "body-temp-mislabel")) {
    throw new Error(
      `self-test: expected body-temp-mislabel on wrapped-claim fixture, got: ${JSON.stringify(wrappedClaimFindings)}`
    );
  }
  if (!wrappedClaimFindings.some((f) => f.rule === "banned-claim")) {
    throw new Error(
      `self-test: expected banned-claim on wrapped-claim fixture, got: ${JSON.stringify(wrappedClaimFindings)}`
    );
  }

  // Regression: an unrelated negation in a PRIOR, already-ended sentence
  // must NOT suppress a genuine claim in the next, independent sentence.
  // This is the coordinator-discovered false negative that the flat-window
  // approach allowed and sentence-boundary scoping must close.
  const unrelatedNegationFindings = findingsForSource(
    readFixture("Bad_UnrelatedNegationPriorSentence.ts"),
    "apps/admin-web/features/herd-signals/format.ts"
  );
  if (!unrelatedNegationFindings.some((f) => f.rule === "banned-claim")) {
    throw new Error(
      `self-test: expected banned-claim on unrelated-prior-sentence-negation fixture (an earlier "There is no cursor..." sentence must not suppress the later eating claim), got: ${JSON.stringify(unrelatedNegationFindings)}`
    );
  }

  // Regression: the SAME exploit, but sharing one PHYSICAL LINE instead of
  // separate lines ("No cursor here. The gateway confirms the animal is
  // eating right now."). Requires mid-line sentence splitting, not just
  // multi-line sentence walking, to close.
  const sameLineFindings = findingsForSource(
    readFixture("Bad_SameLineUnrelatedNegation.ts"),
    "apps/admin-web/features/herd-signals/format.ts"
  );
  if (!sameLineFindings.some((f) => f.rule === "banned-claim")) {
    throw new Error(
      `self-test: expected banned-claim on same-line-unrelated-negation fixture ("No cursor here." must not suppress the eating claim sharing its line), got: ${JSON.stringify(sameLineFindings)}`
    );
  }

  // A complete single-sentence, single-line denial must still pass -- proves
  // mid-line splitting doesn't fragment a real single sentence.
  const oneLineDenialFindings = findingsForSource(
    readFixture("Good_OneLineDenial.go"),
    "backend/internal/herdsignals/app/fixture.go"
  );
  if (oneLineDenialFindings.length) {
    throw new Error(
      `self-test: false positive on one-line denial fixture: ${JSON.stringify(oneLineDenialFindings)}`
    );
  }

  // A dotted identifier (item.motion_count) next to a legitimate wrapped
  // denial must not trigger a spurious split, and the wrapped denial itself
  // must still pass.
  const identifierDotFindings = findingsForSource(
    readFixture("Good_IdentifierDotNextToWrappedDenial.go"),
    "backend/internal/herdsignals/app/fixture.go"
  );
  if (identifierDotFindings.length) {
    throw new Error(
      `self-test: false positive on identifier-dot-next-to-wrapped-denial fixture: ${JSON.stringify(identifierDotFindings)}`
    );
  }

  // Round 4 regression: a legitimate denial and a genuine claim sharing ONE
  // physical line must be judged SEPARATELY, per occurrence -- a denial
  // anywhere on the line must not license a later, unrelated claim on that
  // same line. This is the coordinator's exact exploit of the round-3 fix.
  const denialThenClaimFindings = findingsForSource(
    readFixture("Bad_DenialThenClaimSameLine.ts"),
    "apps/admin-web/features/herd-signals/format.ts"
  );
  if (!denialThenClaimFindings.some((f) => f.rule === "banned-claim")) {
    throw new Error(
      `self-test: expected banned-claim on denial-then-claim-same-line fixture (the denial covering "eating" must not also cover the second, unrelated "eating" claim on the same line), got: ${JSON.stringify(denialThenClaimFindings)}`
    );
  }

  // A denial fragment followed by completely innocent prose (no banned term
  // at all) on the same line must still pass.
  const denialThenInnocentFindings = findingsForSource(
    readFixture("Good_DenialThenInnocentProseSameLine.ts"),
    "apps/admin-web/features/herd-signals/format.ts"
  );
  if (denialThenInnocentFindings.length) {
    throw new Error(
      `self-test: false positive on denial-then-innocent-prose-same-line fixture: ${JSON.stringify(denialThenInnocentFindings)}`
    );
  }

  // Two independent denials sharing one line must both read as denials.
  const twoDenialsFindings = findingsForSource(
    readFixture("Good_TwoDenialsSameLine.ts"),
    "apps/admin-web/features/herd-signals/format.ts"
  );
  if (twoDenialsFindings.length) {
    throw new Error(
      `self-test: false positive on two-denials-same-line fixture: ${JSON.stringify(twoDenialsFindings)}`
    );
  }

  console.log("check-herd-signals-language self-test: PASS");
  console.log(`  FAIL fixture -> ${failFindings.length} finding(s): ${failFindings.map((f) => f.rule).join(", ")}`);
  console.log(`  PASS fixture (same words, denial context) -> ${passFindings.length} finding(s)`);
  console.log(`  admin-web mock-language fixture -> ${mockFindings.length} finding(s): ${mockFindings.map((f) => f.rule).join(", ")}`);
  console.log(`  admin-web clean fixture -> ${mockFindingsGood.length} finding(s)`);
  for (const [file, count] of wrappedDenialResults) {
    console.log(`  wrapped-denial fixture ${file} -> ${count} finding(s)`);
  }
  console.log(`  wrapped-claim (inverse) fixture -> ${wrappedClaimFindings.length} finding(s): ${wrappedClaimFindings.map((f) => f.rule).join(", ")}`);
  console.log(`  unrelated-prior-sentence-negation fixture -> ${unrelatedNegationFindings.length} finding(s): ${unrelatedNegationFindings.map((f) => f.rule).join(", ")}`);
  console.log(`  same-line-unrelated-negation fixture -> ${sameLineFindings.length} finding(s): ${sameLineFindings.map((f) => f.rule).join(", ")}`);
  console.log(`  one-line denial fixture -> ${oneLineDenialFindings.length} finding(s)`);
  console.log(`  identifier-dot-next-to-wrapped-denial fixture -> ${identifierDotFindings.length} finding(s)`);
  console.log(`  denial-then-claim-same-line (inverse) fixture -> ${denialThenClaimFindings.length} finding(s): ${denialThenClaimFindings.map((f) => f.rule).join(", ")}`);
  console.log(`  denial-then-innocent-prose-same-line fixture -> ${denialThenInnocentFindings.length} finding(s)`);
  console.log(`  two-denials-same-line fixture -> ${twoDenialsFindings.length} finding(s)`);
}

if (process.argv.includes("--self-test")) {
  selfTest();
  process.exit(0);
}

const findings = scanTree();
const targetCount = collectTargets().length;

if (findings.length) {
  console.error(
    `herd-signals-language: ${findings.length} claim-boundary violation(s) in Herd Signals surfaces`
  );
  for (const f of findings) {
    console.error(`- ${f.rule} ${f.rel}:${f.line}: ${f.message}`);
  }
  console.error(
    "See docs/modules/herd-signals.md Section 3 (\"What we do not claim\"). If a line is a " +
      "genuine, reviewed exception, append `herd-signals-language:ignore: owner=<name> " +
      "issue=<url|id> scope=<why> expiry=<YYYY-MM-DD>` on that line."
  );
  process.exit(1);
}
console.log(
  `herd-signals-language: ok (${targetCount} Herd Signals file(s) + the OpenAPI slice scanned; no claim-boundary violations found)`
);
