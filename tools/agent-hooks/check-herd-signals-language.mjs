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
//   docs/modules/herd-signals*.md — a GLOB, not a single hardcoded filename
//     (docs/modules/herd-signals-system-design.md landed after the original
//     hardcoded path was written and would otherwise have been invisible to
//     this guard). Every matching file is allowlisted -- see
//     ALLOWLISTED_FILES / isAllowlistedHerdSignalsDoc -- because stating the
//     boundary legitimately requires using the banned vocabulary.
//   mock/herd-signals-mock.html
//   .agents/skills/goatos-herd-signals/SKILL.md — scanned AND explicitly
//     allowlisted (not silently out of scope): it legitimately states the
//     claim boundary too, and an unlisted file is indistinguishable from an
//     oversight to the next reader.
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
// REMAINING BLIND SPOTS (documented honestly; five review rounds closed the
// scope-of-judgement bugs below this line -- see the "CLOSED (round N)"
// notes for what each round fixed and why):
// - Composition across variables/helpers: `label := "eat" + "ing"` or a
//   claim assembled from a template/i18n table keyed by a code that is
//   itself innocuous (e.g. `t("hs.behavior.1")` resolving elsewhere to
//   "Eating detected") is invisible to a line-level regex scan.
// - Non-literal runtime strings: an LLM-, config-, or CMS-driven copy
//   string is not visible to a static scan of source files.
// - Semantic paraphrase BEYOND the named posture family: the banned-term
//   list (BANNED_TERMS) now includes the gait/posture paraphrase family
//   (resting, grazing, dozing, sleeping, idle-as-behaviour) after a live
//   "Resting for short periods is normal" leak into shipped copy proved
//   this gap was not hypothetical -- but "browsing", "napping", "chewing",
//   and any other synonym NOT in that list still passes. Section 4
//   ("Approved product vocabulary") of docs/modules/herd-signals.md is the
//   human-authority backstop for whatever paraphrase this guard cannot
//   yet name; review must still read for paraphrase, this guard cannot.
// - The contracts/openapi/app-api.yaml slice detection is a best-effort
//   marker scan of one large shared file; a herd-signals schema fragment
//   that does not contain any of the marker strings this guard looks for
//   (see HERD_SIGNALS_YAML_START) will not be scanned.
// - It cannot verify negation phrasing is TRUE (i.e. that a denial like
//   "does not detect eating" accurately reflects the code) — only that the
//   sentence is grammatically a denial, not a claim.
//
// Negation/claim SCOPE — the actual history of every real defect found in
// this guard across five review rounds, in order:
//   Round 1 (line): negation was judged per PHYSICAL LINE and failed the
//     product's own mandatory disclaimer the moment JSX wrapped it across
//     two lines. CLOSED by SENTENCE-scoped judgement (below).
//   Round 2 (flat window): a flat N-line window fixed the wrap, but let an
//     unrelated negation in a fully-unrelated, already-ended PRIOR sentence
//     silently suppress a genuine claim just by being nearby. CLOSED by
//     replacing the line-count window with real sentence-BOUNDARY walking:
//     end-of-line `.`/`!`/`?`/`;` punctuation, a blank line, a JSX tag edge
//     (a line ending in `>` or starting with `<`), or a list-item start
//     (`-`, `*`, `1.`). A newline INSIDE a sentence (no boundary yet) is
//     NOT a boundary — this is what keeps a denial wrapped across lines
//     ("... own sensor housing -- not the\n  animal's body temperature.")
//     reading as one sentence.
//   Round 3 (line-granularity sentences): the boundary walk above only
//     looked for boundaries at line ENDS, so two independent sentences
//     sharing ONE physical line ("No cursor here. The gateway confirms the
//     animal is eating right now.") were still judged as a single blob.
//     CLOSED by MID-LINE sentence splitting (splitIntoSentenceFragments):
//     each line is split on `. `/`! `/`? ` (punctuation followed by
//     whitespace and then a capital letter, quote, or end-of-string)
//     BEFORE the boundary walk runs. Deliberately does NOT split a decimal
//     number (`25.1 C`) or a dotted identifier/call (`item.motion_count`,
//     `Number(x).toFixed(2)`) -- neither has whitespace right after the
//     dot -- or a short list of known abbreviations (e.g., i.e., etc., vs.,
//     approx., fig., mr., dr., ...).
//   Round 4 (first-occurrence-per-line): even after mid-line splitting,
//     the outer scan only judged the FIRST banned-term occurrence per
//     line, so a denial fragment anywhere on a line silently licensed a
//     later, unrelated claim fragment on that SAME line ("the tag does not
//     detect eating. The gateway confirms the animal is eating now." only
//     ever judged the first "eating"). CLOSED by judging every occurrence
//     independently via a global regex scan plus fragmentOrdinalForOffset,
//     which maps each occurrence's exact character offset to the specific
//     sentence fragment (by ordinal, not by re-searching for the term's
//     text) that contains it.
//   Round 5 (four more scope/subject bugs, found by adversarial product
//     review, all CLOSED):
//     (a) Bare JSX text claims: `<th>Eating</th>` / `<Tag>Ruminating</Tag>`
//         passed because quotedClaim required quote characters and JSX
//         text children have none. Closed by jsxTextClaim (a `>...<`
//         text-run match).
//     (b) Trailing unrelated negation: isClaimShaped used to return
//         "not a claim" whenever ANY negation existed anywhere in the text
//         and no "detect" verb was present, regardless of order --
//         "The animal is eating, but the battery is not low" passed.
//         Closed by making negationPrecedesTerm (order relative to THIS
//         term, and ONLY this term) the sole negation test.
//     (c) Affirming idioms read as negation: NEGATION_RE's bare "no"
//         matched "no doubt"/"no question"/"no longer", so "There is no
//         doubt this tag detected eating today" read as a denial. Closed
//         by excluding those specific idioms from "no" via a negative
//         lookahead; "no ... sensor" (a genuine denial shape used
//         elsewhere in this codebase) still matches.
//     (d) body-temp-mislabel's own extra `|| !NEGATION_RE.test(window)`
//         fallback let ANY negation anywhere in the window excuse
//         non-field prose regardless of order -- "Body temperature rose
//         above baseline, though the signal is not weak." passed. Closed
//         by dropping that fallback; only order-based negationPrecedesTerm
//         may excuse a body-temp occurrence, identical to (b).
//   Round 5, word-boundary tightening (a fifth, adjacent bug, also
//     CLOSED): BANNED_TERM_RE previously matched each alternative as a bare
//     SUBSTRING with no boundary at all, so "before treating any row" and
//     "in any interesting way" matched "eating"/"resting" as pure noise
//     inside unrelated English words. Closed by wrapping the WHOLE
//     alternation in one `\b...\b` (JS regex backtracking then tries
//     longer alternatives automatically until the boundary holds, so the
//     old "longest-form-first" ordering trick is no longer load-bearing).
//     Trade-off reintroduced ON PURPOSE: a glued identifier (`is_eating`,
//     `IsEating`, `body_temperature_c`) has no real `\b` between its parts
//     either, so BANNED_TERM_RE alone can no longer see it -- covered by
//     the separate, narrower IDENTIFIER_CLAIM_RE pass instead (see
//     coreTermFromIdentifierMatch), which is NOT boundary-anchored on the
//     glued term itself but IS boundary-anchored as a whole compound.
//   Round 5, SUBJECT bug (found live on the real tree, also CLOSED): even
//     with the word-boundary fix, the GAIT/POSTURE family (run, walk,
//     stand, lie, sit, eat, graze, rest, and — added after two more live
//     false positives — idle, sleeping, dozing) has ordinary, innocent
//     TECHNICAL meanings: queries run, sweepers run, tests stand up a
//     container, a connection pool is idle, a goroutine is sleeping. Fully
//     word-bounded, genuinely-technical prose like "filters and search run
//     in the query" matched stateClaim ("is/was <term>") exactly like a
//     real "the animal is running" claim. Closed by requiring an
//     animal-subject cue (animal/goat/herd/tag, or it/they -- a documented
//     over-approximation) OR an explicit assertion verb
//     (detects/confirms/shows) in the same sentence before a GAIT_TERMS
//     stateClaim match is trusted; a bare detection verb alone (with NO
//     subject at all, e.g. "detects grazing") remains sufficient on its
//     own, matching detectClaim's existing leniency. Non-gated terms
//     (rumination, fever, disease) have no innocent technical meaning and
//     are NOT subject-gated -- they stay strict.
//   Round 6 (found live on the real tree, CLOSED): "diagnos*" was left out
//     of the round-5 gate as a "no innocent technical meaning" term -- that
//     assumption was WRONG. "diagnostic"/"diagnostics" describing a DATA
//     FIELD is ordinary engineering vocabulary (a diagnostic value, kept
//     for diagnostics only, a diagnostic field), and it is now load-bearing
//     in this exact module: the gateway's own clock is untrusted (a
//     constant +02:30:00 offset from real IST) and is stored ONLY as
//     diagnostic data, never rendered -- backend/internal/herdsignals/
//     app/service.go:68 documents exactly this and matched stateClaim ("is
//     diagnostic") the moment the module was written. Closed by folding
//     "diagnos*" into the same subject-gated set (checked by PREFIX, since
//     it is a wildcard family, not a single GAIT_TERMS entry) -- with one
//     addition the gait family didn't need: "diagnoses"/"diagnosed"/
//     "diagnose"/"diagnosing" are VERB forms that themselves ARE the
//     assertion ("the tag diagnoses the animal"), unlike "diagnostic"/
//     "diagnosis" (adjective/noun, almost always the field sense), so
//     those verb forms are flagged on ANY subject cue at all, not gated
//     behind stateClaim's "is/was" shape.
//   The policy this round establishes, stated plainly because it will
//     recur: ANY newly banned root needs an innocent-technical-usage
//     review before it ships, not just before it is found broken.
//     Engineering prose reuses clinical and physical vocabulary
//     constantly (diagnostic, monitor, signal, symptom, sensor, temp,
//     alert, trigger, ...); assuming a new banned term has "no innocent
//     technical meaning" without checking is exactly the assumption that
//     produced this round's live false positive on a mandatory comment.
//     When adding a term to BANNED_TERMS, grep the actual scanned
//     surfaces for it first, or add it straight to the gated
//     (isInnocentTechnicalTerm) path if there is any doubt.
//
// The general lesson across ALL of the above, restated once: the unit of
// judgement is the individual term OCCURRENCE, the SENTENCE enclosing it
// (found by real boundaries, not a line or a line-count window), and — for
// terms with an innocent technical meaning — the SUBJECT (or, for a verb
// form that is itself the assertion, any subject at all) that sentence
// attaches the term to. Every version of this guard before the current one
// failed by judging something LARGER or narrower than that, or by assuming
// a term had no innocent meaning without checking.
//
// Residual blind spots on the CURRENT sentence/occurrence/subject logic
// (static heuristics, not a parser):
// - Sentence-boundary detection: an abbreviation NOT in the ABBREVIATIONS
//   list, or any other punctuation-then-capital-letter sequence a human
//   would not read as a sentence break, can still be mis-split; this only
//   degrades to narrower sentence scope (more conservative matching), not
//   to a wrong verdict on unrelated content. A sentence boundary that falls
//   MID-LINE via a period immediately followed by more prose on the SAME
//   physical line (not the dotted-identifier/decimal case, which the
//   splitter already excludes) is handled; an unusual style (all-lowercase
//   sentence starts, a sentence ending in a closing quote before the
//   period) can still fool the heuristic in either direction.
// - buildSentenceWindow caps how far outward it will walk at
//   WINDOW_BEFORE/WINDOW_AFTER lines even if no boundary is found in that
//   span, so a genuinely boundary-free multi-page run-on comment could
//   still merge unrelated context past that cap; widen the constants or
//   use the `herd-signals-language:ignore:` escape hatch for that rare
//   shape.
// - SUBJECT_CUE_RE's "it"/"they" are generic pronouns, not animal-specific
//   on their own -- "the query runs, it caches results" could in principle
//   false-positive if "it" is later judged to refer to the query rather
//   than an animal in some future sentence shape. Not observed in practice
//   during this review; documented as a known over-approximation rather
//   than removed, per explicit product-review guidance.
// - DETECTION_VERB_RE alone (with no subject cue at all) is sufficient to
//   treat a gait/posture term as claim-shaped, mirroring detectClaim's
//   existing leniency -- so "the dashboard shows the job running" (a
//   non-animal subject) could in principle still false-positive if it ever
//   appears in a scanned file. Accepted deliberately: a detection verb
//   co-occurring with a banned term is a narrow, sentence-scoped
//   co-occurrence, not a bare substring match, and product review judged
//   this an acceptable residual risk versus reopening the false-negative
//   this exists to close ("detects grazing" with no subject at all must
//   still flag).
// - GAIT_TERMS is a fixed, named list (run/walk/stand/lie/sit/eat/graze/
//   rest plus idle/sleeping/dozing, extended twice already after live
//   false positives). A future banned term with its own innocent technical
//   meaning that is NOT added to this set will be judged strictly (no
//   subject gate) and could false-positive the same way "idle"/"sleeping"
//   did before they were added -- extend GAIT_TERMS, don't work around it.

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
// Docs allowlisted by GLOB (docs/modules/herd-signals*.md), not a single
// hardcoded filename -- a sibling design doc
// (docs/modules/herd-signals-system-design.md) landed after the original
// hardcoded path was written and was invisible to this guard until the glob
// replaced it, so the next sibling doc is covered automatically instead of
// depending on someone remembering to add it here.
const DOCS_GLOB_PREFIX = "docs/modules/herd-signals";
const DOCS_GLOB_SUFFIX = ".md";

// Explicitly allowlisted, not silently out of scope: this skill file
// legitimately states the claim boundary (and quotes banned words while
// doing so, same as docs/modules/herd-signals*.md), and an unlisted file is
// indistinguishable from an oversight to the next reader -- naming it here
// makes the exemption a decision, not an accident.
const ALLOWLISTED_FILES = [
  "tools/agent-hooks/check-herd-signals-language.mjs",
  ".agents/skills/goatos-herd-signals/SKILL.md",
];

function isAllowlistedHerdSignalsDoc(rel) {
  return (
    rel.startsWith(DOCS_GLOB_PREFIX) &&
    rel.endsWith(DOCS_GLOB_SUFFIX) &&
    rel.slice(0, rel.lastIndexOf("/") + 1) === "docs/modules/"
  );
}

// Banned behavior/health terms (docs/modules/herd-signals.md Section 3),
// PLUS the posture-paraphrase family that the module doc's Section 4
// already bans as a renaming of "active/quiet/no movement" (resting,
// grazing, dozing, sleeping, idle-as-behaviour) -- added round 5 after a
// live "Resting for short periods is normal" leak into shipped copy.
// Every entry is a bare word/word-family; BANNED_TERM_RE below wraps the
// WHOLE alternation in a single \b...\b, not each entry individually --
// see that constant's own comment for why per-entry escaping was replaced.
const BANNED_TERMS = [
  "eating", "eats", "eat",
  "rumination", "ruminating", "ruminate",
  "sitting", "sits", "sit",
  "standing", "stands", "stand",
  "lying", "lies", "lie", "lay",
  "walking", "walks", "walk",
  "running", "runs", "run",
  "feverish", "fever",
  "diseased", "disease",
  "diagnos\\w*",
  "resting", "rests", "rest",
  "grazing", "grazes", "graze",
  "dozing", "dozes", "doze",
  "sleeping", "sleeps",
  "idle",
];

// Whole-alternation word-boundary wrapping (round 5, coordinator-reported
// bypass): per-entry escaping like "eating" with no boundary at all used to
// match as a bare SUBSTRING anywhere -- "before treating any row" and "in
// any interesting way" both matched "eating"/"resting" as pure noise inside
// unrelated words (treating, interesting), and would have been real false
// positives the moment a scanned file used those ordinary English words.
// Wrapping the ENTIRE alternation in one \b...\b, rather than escaping
// each alternative, is what makes JS regex backtracking do the right thing
// regardless of alternative order: at a given start position it tries each
// alternative, and if the trailing \b assertion fails (e.g. "eat" matched
// inside "Eating" with a word character immediately after), it backtracks
// into the alternation and tries the next, longer alternative ("eating")
// until the boundary holds -- so the old "longest-form-first" ordering
// hack is no longer load-bearing (kept only for readability, not
// correctness).
// Trade-off this reintroduces on purpose: an identifier or field name that
// GLUES the term to adjacent letters/underscores with no real word break
// (`is_eating`, `IsEating`, `body_temperature_c`) no longer matches this
// boundary-anchored regex at all, because `_` and camelCase letters are all
// \w -- there is no `\b` between "is" and "Eating" in "IsEating". That
// case is NOT abandoned: IDENTIFIER_CLAIM_RE below is a SEPARATE, narrower
// pass specifically for the compound is_/_detected/_flag/_status shape, so
// field/key names are still caught without reopening the substring-noise
// hole for ordinary prose.
const BANNED_TERM_RE = new RegExp(`\\b(?:${BANNED_TERMS.join("|")})\\b`, "i");

// Compound identifier/field-name shape (is_eating, IsEating, eating_detected,
// fever_flag, disease_status, ...) -- deliberately NOT boundary-anchored on
// the banned-term portion itself (an identifier glues words together with
// no `\b` between them), but the identifier AS A WHOLE is boundary-anchored
// so it doesn't itself become substring noise. This is what still catches
// `IsEating bool \`json:"is_eating"\`` as a claim after BANNED_TERM_RE was
// tightened to real word boundaries above.
const IDENTIFIER_CLAIM_RE = new RegExp(
  `\\b(?:is_?(?:${BANNED_TERMS.join("|")})|(?:${BANNED_TERMS.join("|")})_?(?:detected|flag|status))\\b`,
  "i"
);

// "body temp[erature]" specifically, for the body-temp-mislabel check.
const BODY_TEMP_RE = /\bbody[\s_-]?temp(?:erature)?\b/i;

// Constructions that mark a line as a DENIAL rather than a claim. If any of
// these match near the banned term, the line is a negation and must NOT be
// flagged (this is the "match the assertion, not the word" requirement).
// Genuine denial words only. `no` is excluded when it is part of an
// AFFIRMING idiom rather than a real negation of a claim -- "no doubt",
// "no question", and "no longer" all read as certainty/completion markers
// in ordinary English, not as a denial of whatever claim follows (round-5
// fix, bypass 3: "There is no doubt this tag detected eating today" used to
// read as a denial purely because bare "no" preceded "eating", regardless
// of what "no" was actually negating). Bare "no" immediately before a noun
// (e.g. "no animal-contact sensor", "no direct temperature sensor")
// remains a genuine denial trigger.
const NEGATION_RE =
  /\b(?:not|never|cannot|can't|does\s+not|doesn't|isn't|is\s+not|must\s+not|without|no(?!\s+(?:doubt|question|longer)\b))\b/i;

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
// Given a matched IDENTIFIER_CLAIM_RE compound (e.g. "is_eating",
// "IsEating", "eating_detected", "fever_flag", "disease_status"), strip the
// is_/is prefix or the _detected/_flag/_status suffix to recover the bare
// banned term for negation-window lookup and for re-checking claim shape.
function coreTermFromIdentifierMatch(matched) {
  const withoutPrefix = matched.replace(/^is_?/i, "");
  if (withoutPrefix !== matched) return withoutPrefix;
  return matched.replace(/_?(?:detected|flag|status)$/i, "");
}

// The GAIT/POSTURE family has ordinary, innocent TECHNICAL meanings outside
// animal behavior: queries run, jobs run, sweepers run, tests stand up a
// container, code walks a cursor/index, a cache rests idle, a batch is
// "eating" memory. Word-boundary anchoring (above) fixes substring noise
// ("interesting" != "resting") but not this: "filters and search run in the
// query" is fully word-bounded, genuinely-technical prose that must NOT be
// flagged, and "the test suite is standing up a container" matches the
// EXACT stateClaim shape ("is standing") used to correctly flag "the animal
// is standing." (round-5 fix.) Terms in this family require either an
// animal-subject cue in the same sentence or an explicit detection verb
// before stateClaim (or the gait-specific detection co-occurrence check
// below) may treat them as claim-shaped. Terms with NO innocent technical
// meaning (rumination, fever, disease, diagnos*, sleeping, dozing, idle,
// and "body temperature" via its own separate check) are NOT gated this
// way -- they stay strict, because there is no legitimate non-animal
// sentence that would use them.
const GAIT_TERMS = new Set([
  "run", "running", "runs",
  "walk", "walking", "walks",
  "stand", "standing", "stands",
  "lie", "lying", "lies", "lay",
  "sit", "sitting", "sits",
  "eat", "eating", "eats",
  "graze", "grazing", "grazes",
  "rest", "resting", "rests",
  // Added after empirical testing found the same shape of false positive:
  // "the connection pool is idle between polls" and "the goroutine is
  // sleeping between polls" are both ordinary Go/infra prose that live in
  // this exact module's backend surface (gateway polling, connection
  // handling), and both matched stateClaim ("is idle"/"is sleeping")
  // exactly like the run/stand/eat cases the coordinator demonstrated.
  "idle", "sleeping", "sleeps", "dozing", "dozes", "doze",
]);

// A cue that the SUBJECT of the sentence is an animal (or the tag standing
// in for one) rather than a query, job, sweeper, cursor, cache, or batch.
// "it"/"they" are included per product-review guidance even though they are
// not animal-specific on their own -- a known, documented over-approximation
// (see the guard's header blind-spot list).
const SUBJECT_CUE_RE = /\b(?:animal|animals|goat|goats|herd|tag|tags|it|they|its|their)\b/i;

// An explicit assertion verb: on its own (with NO subject cue at all,
// e.g. "detects grazing") this is still sufficient to read as a claim --
// matches the existing detectClaim shape's own leniency.
const DETECTION_VERB_RE = /\b(?:detects?|detected|detecting|confirms?|confirmed|shows?|showed)\b/i;

function isClaimShaped(text, term) {
  // The ONLY negation test: does a genuine denial word precede this SPECIFIC
  // term's first occurrence in `text`? (round-5 fix, bypass 2/4): an earlier
  // version returned "not a claim" whenever ANY negation existed anywhere in
  // the text and no "detect" verb was present -- so a trailing, UNRELATED
  // negation ("The animal is eating, but the battery is not low") silently
  // excused a real claim that had nothing to do with that negation. Order
  // relative to THIS term is the only thing that may excuse a claim.
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
  // Bare JSX text content -- a column header, a status chip, or any other
  // literal text between tags carries no quotes at all (round-5 fix, bypass
  // 1: `<th>Eating</th>` and `<Tag tone="ok">Ruminating</Tag>` both used to
  // pass zero findings, because quotedClaim requires quote characters and
  // JSX text children have none). Matches the term inside a `>...<` text
  // run with no nested tag boundary in between.
  const jsxTextClaim = new RegExp(`>[^<>{}]*\\b${term}\\b[^<>{}]*<`, "i");

  // fieldNameClaim and jsxTextClaim are inherently product-facing labels/
  // identifiers -- there is no "sentence subject" to require, a chip
  // literally labeled "Running" or a field literally named `is_running` IS
  // the claim regardless of gait-term ambiguity. Unconditional, same as
  // before this round.
  if (fieldNameClaim.test(text) || jsxTextClaim.test(text)) return true;

  // detectClaim already REQUIRES an explicit detect verb by construction,
  // which satisfies "or an explicit detection verb" on its own -- no
  // additional subject-cue gate needed even for gait terms ("detects
  // grazing" must still flag with no subject present at all).
  if (detectClaim.test(text)) return true;

  // "diagnos*" (diagnostic/diagnosis/diagnosed/diagnoses) is a wildcard
  // family, not a single GAIT_TERMS entry -- checked by prefix instead of
  // set membership. Added to the subject-gated set after a live false
  // positive: "received_at ... is truth for ordering/gaps regardless --
  // this is diagnostic only" (backend/internal/herdsignals/app/service.go,
  // documenting that the gateway's own clock runs a constant +02:30:00
  // offset from real IST and is stored ONLY as diagnostic data, never
  // rendered) matched stateClaim ("is diagnostic") exactly like the
  // run/walk/rest cases -- "diagnostic" describing a DATA FIELD is not the
  // banned claim that the SYSTEM diagnoses an ANIMAL's disease.
  const isInnocentTechnicalTerm = GAIT_TERMS.has(term.toLowerCase()) || /^diagnos/i.test(term);

  if (isInnocentTechnicalTerm) {
    // A gait/posture/diagnostic term co-occurring with an explicit
    // assertion verb (confirms/shows/...) in the same sentence, even
    // without the exact "is <term>" shape stateClaim requires -- covers
    // constructions like "the gateway confirms the goat is walking" and
    // "tag A0003B shows the animal standing" that detectClaim/stateClaim
    // alone would miss.
    if (DETECTION_VERB_RE.test(text) && new RegExp(`\\b${term}\\b`, "i").test(text)) {
      return true;
    }
    // "diagnoses"/"diagnosed"/"diagnose"/"diagnosing" are VERB forms that
    // themselves ARE the assertion ("the tag diagnoses the animal",
    // "disease diagnosed by the tag") -- unlike "diagnostic"/"diagnosis"
    // (adjective/noun, almost always the innocent data-field sense), a
    // verb form co-occurring with ANY subject cue at all is the banned
    // claim itself, not a technical description of a field.
    if (/^diagnos(?:e|es|ed|ing)$/i.test(term) && SUBJECT_CUE_RE.test(text)) {
      return true;
    }
    // stateClaim ("is/was <term>") is exactly the shape ordinary technical
    // prose also produces ("the test suite is standing up a container",
    // "the batch is eating memory", "this is diagnostic only") -- require
    // an animal-subject cue before trusting it for this family.
    if (stateClaim.test(text)) return SUBJECT_CUE_RE.test(text);
  } else {
    // Non-gated banned terms (rumination, fever, disease) have no innocent
    // technical meaning -- stay strict, no subject-cue gate.
    if (stateClaim.test(text)) return true;
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

      // Compound identifier/field-name occurrences (is_eating, IsEating,
      // eating_detected, ...) -- BANNED_TERM_RE's boundary anchoring above
      // deliberately cannot see these (no \\b between glued word parts), so
      // this is a SEPARATE pass over the SAME line using IDENTIFIER_CLAIM_RE.
      const globalIdentifierRe = new RegExp(IDENTIFIER_CLAIM_RE.source, "gi");
      let im;
      while ((im = globalIdentifierRe.exec(stripped)) !== null) {
        if (im[0] === "") {
          globalIdentifierRe.lastIndex++;
          continue;
        }
        const matched = im[0];
        const term = coreTermFromIdentifierMatch(matched);
        const fragIdx2 = fragmentOrdinalForOffset(stripped, lineFragments, im.index);
        const window2 = buildSentenceWindow(lines, i, WINDOW_BEFORE, WINDOW_AFTER, fragIdx2);
        if (!VOCAB_LIST_RE.test(window2) && isClaimShaped(window2, term)) {
          findings.push({
            line: lineNo,
            rule: "banned-claim",
            message: `claim-shaped use of banned term "${matched}" — Herd Signals cannot detect/classify behavior; deny it explicitly ("does not detect ...") or remove the claim`,
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
        // The ONLY excuse is a genuine denial that precedes THIS specific
        // occurrence (round-5 fix, bypass 4): an earlier version also
        // excused any non-field-shaped prose whenever the window contained
        // ANY negation at all, anywhere -- so "Body temperature rose above
        // baseline, though the signal is not weak." passed, because a
        // trailing, unrelated "not weak" satisfied `!NEGATION_RE.test`
        // even though nothing actually denied the body-temperature claim.
        // Order-based negationPrecedesTerm is the single source of truth
        // here, exactly like the banned-claim check above.
        const isDenial = negationPrecedesTerm(window, term);
        if (!VOCAB_LIST_RE.test(window) && !isDenial) {
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

      // Compound identifier/property-name occurrences (is_eating,
      // eating_detected, ...) -- an OpenAPI schema `properties:` block
      // commonly uses exactly this snake_case shape.
      const globalIdentifierRe = new RegExp(IDENTIFIER_CLAIM_RE.source, "gi");
      let im;
      while ((im = globalIdentifierRe.exec(stripped)) !== null) {
        if (im[0] === "") {
          globalIdentifierRe.lastIndex++;
          continue;
        }
        const matched = im[0];
        const term = coreTermFromIdentifierMatch(matched);
        const fragIdx2 = fragmentOrdinalForOffset(stripped, lineFragments, im.index);
        const window2 = buildSentenceWindow(sliceLines, idx, WINDOW_BEFORE, WINDOW_AFTER, fragIdx2);
        if (!VOCAB_LIST_RE.test(window2) && isClaimShaped(window2, term)) {
          findings.push({
            line: lineNo,
            rule: "banned-claim",
            message: `claim-shaped use of banned term "${matched}" in the herd-signals OpenAPI slice`,
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

  // Glob, not a hardcoded single filename -- see DOCS_GLOB_PREFIX comment.
  const docsDir = join(repo, "docs/modules");
  if (existsSync(docsDir)) {
    for (const entry of readdirSync(docsDir, { withFileTypes: true })) {
      if (!entry.isFile()) continue;
      if (!entry.name.startsWith("herd-signals") || !entry.name.endsWith(".md")) continue;
      const abs = join(docsDir, entry.name);
      const rel = relative(repo, abs);
      targets.push({ abs, rel, kind: "allowlisted" });
    }
  }

  const mockFile = join(repo, "mock/herd-signals-mock.html");
  if (existsSync(mockFile)) {
    targets.push({ abs: mockFile, rel: "mock/herd-signals-mock.html", kind: "source" });
  }

  const skillFile = join(repo, ".agents/skills/goatos-herd-signals/SKILL.md");
  if (existsSync(skillFile)) {
    targets.push({
      abs: skillFile,
      rel: ".agents/skills/goatos-herd-signals/SKILL.md",
      kind: "allowlisted",
    });
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
    if (ALLOWLISTED_FILES.includes(rel) || isAllowlistedHerdSignalsDoc(rel)) continue;
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

  // Round 5, bypass 1: bare JSX text claims (no quotes) must be caught.
  const bareJsxFindings = findingsForSource(
    readFixture("Bad_BareJsxTextClaim.tsx"),
    "apps/admin-web/features/herd-signals/live-panel.tsx"
  );
  if (!bareJsxFindings.some((f) => f.rule === "banned-claim")) {
    throw new Error(
      `self-test: expected banned-claim on bare-JSX-text-claim fixture (<th>Eating</th> / <Tag>Ruminating</Tag> with no quotes), got: ${JSON.stringify(bareJsxFindings)}`
    );
  }

  // Round 5, bypass 2: a trailing, unrelated negation must not excuse a
  // genuine claim earlier in the sentence.
  const trailingNegationFindings = findingsForSource(
    readFixture("Bad_TrailingUnrelatedNegation.ts"),
    "apps/admin-web/features/herd-signals/format.ts"
  );
  if (!trailingNegationFindings.some((f) => f.rule === "banned-claim")) {
    throw new Error(
      `self-test: expected banned-claim on trailing-unrelated-negation fixture ("is eating, but ... is not low"), got: ${JSON.stringify(trailingNegationFindings)}`
    );
  }

  // Round 5, bypass 3: "no doubt" is an affirmation, not a denial.
  const noDoubtFindings = findingsForSource(
    readFixture("Bad_NoDoubtAffirmation.ts"),
    "apps/admin-web/features/herd-signals/format.ts"
  );
  if (!noDoubtFindings.some((f) => f.rule === "banned-claim")) {
    throw new Error(
      `self-test: expected banned-claim on "no doubt" affirmation fixture, got: ${JSON.stringify(noDoubtFindings)}`
    );
  }

  // Round 5, bypass 4: same trailing-unrelated-negation bug, in the
  // body-temp-mislabel check specifically.
  const bodyTempTrailingFindings = findingsForSource(
    readFixture("Bad_BodyTempTrailingNegation.ts"),
    "apps/admin-web/features/herd-signals/format.ts"
  );
  if (!bodyTempTrailingFindings.some((f) => f.rule === "body-temp-mislabel")) {
    throw new Error(
      `self-test: expected body-temp-mislabel on body-temp trailing-negation fixture, got: ${JSON.stringify(bodyTempTrailingFindings)}`
    );
  }

  // Round 5: word-boundary noise (interesting/treating/arrested/restore/
  // restart/wrestling) must produce zero findings.
  const wordBoundaryNoiseFindings = findingsForSource(
    readFixture("Good_WordBoundaryNoise.ts"),
    "apps/admin-web/features/herd-signals/format.ts"
  );
  if (wordBoundaryNoiseFindings.length) {
    throw new Error(
      `self-test: false positive on word-boundary-noise fixture: ${JSON.stringify(wordBoundaryNoiseFindings)}`
    );
  }

  // Round 5: gait/posture terms in ordinary technical prose (no animal
  // subject, no detection verb) must produce zero findings.
  const gaitTechnicalFindings = findingsForSource(
    readFixture("Good_GaitTermsTechnicalProse.ts"),
    "apps/admin-web/features/herd-signals/format.ts"
  );
  if (gaitTechnicalFindings.length) {
    throw new Error(
      `self-test: false positive on gait-terms-technical-prose fixture: ${JSON.stringify(gaitTechnicalFindings)}`
    );
  }

  // Round 5: the SAME gait/posture terms, now with an animal subject or
  // detection verb, must all be caught.
  const gaitAnimalFindings = findingsForSource(
    readFixture("Bad_GaitTermsAnimalSubject.ts"),
    "apps/admin-web/features/herd-signals/format.ts"
  );
  if (!gaitAnimalFindings.some((f) => f.rule === "banned-claim")) {
    throw new Error(
      `self-test: expected banned-claim finding(s) on gait-terms-animal-subject fixture, got: ${JSON.stringify(gaitAnimalFindings)}`
    );
  }
  if (gaitAnimalFindings.length < 6) {
    throw new Error(
      `self-test: expected all 6 lines of gait-terms-animal-subject fixture to be flagged, got only ${gaitAnimalFindings.length}: ${JSON.stringify(gaitAnimalFindings)}`
    );
  }

  // Round 5: the posture-paraphrase family, genuinely denied, must pass.
  const postureParaphraseDenialFindings = findingsForSource(
    readFixture("Good_PostureParaphraseDenial.go"),
    "backend/internal/herdsignals/app/fixture.go"
  );
  if (postureParaphraseDenialFindings.length) {
    throw new Error(
      `self-test: false positive on posture-paraphrase-denial fixture: ${JSON.stringify(postureParaphraseDenialFindings)}`
    );
  }

  // Round 6: "diagnostic"/"diagnostics" in ordinary engineering usage (a
  // data field, kept for troubleshooting) must pass -- this is now
  // load-bearing vocabulary (the gateway clock's untrusted +02:30:00 IST
  // offset is documented as "diagnostic only" in real backend code).
  const diagnosticTechnicalFindings = findingsForSource(
    readFixture("Good_DiagnosticTechnicalUsage.go"),
    "backend/internal/herdsignals/app/fixture.go"
  );
  if (diagnosticTechnicalFindings.length) {
    throw new Error(
      `self-test: false positive on diagnostic-technical-usage fixture: ${JSON.stringify(diagnosticTechnicalFindings)}`
    );
  }

  // Round 6: the VERB forms of "diagnos*" (diagnoses/diagnosed) attaching
  // to an animal subject ARE the banned claim and must still be caught.
  const diagnosisAnimalFindings = findingsForSource(
    readFixture("Bad_DiagnosisAnimalClaim.go"),
    "backend/internal/herdsignals/app/fixture.go"
  );
  if (!diagnosisAnimalFindings.some((f) => f.rule === "banned-claim")) {
    throw new Error(
      `self-test: expected banned-claim finding(s) on diagnosis-animal-claim fixture, got: ${JSON.stringify(diagnosisAnimalFindings)}`
    );
  }
  if (diagnosisAnimalFindings.length < 3) {
    throw new Error(
      `self-test: expected all 3 lines of diagnosis-animal-claim fixture to be flagged, got only ${diagnosisAnimalFindings.length}: ${JSON.stringify(diagnosisAnimalFindings)}`
    );
  }

  console.log(`  bare-JSX-text-claim fixture -> ${bareJsxFindings.length} finding(s): ${bareJsxFindings.map((f) => f.rule).join(", ")}`);
  console.log(`  trailing-unrelated-negation fixture -> ${trailingNegationFindings.length} finding(s): ${trailingNegationFindings.map((f) => f.rule).join(", ")}`);
  console.log(`  "no doubt" affirmation fixture -> ${noDoubtFindings.length} finding(s): ${noDoubtFindings.map((f) => f.rule).join(", ")}`);
  console.log(`  body-temp trailing-negation fixture -> ${bodyTempTrailingFindings.length} finding(s): ${bodyTempTrailingFindings.map((f) => f.rule).join(", ")}`);
  console.log(`  word-boundary-noise fixture -> ${wordBoundaryNoiseFindings.length} finding(s)`);
  console.log(`  gait-terms-technical-prose fixture -> ${gaitTechnicalFindings.length} finding(s)`);
  console.log(`  gait-terms-animal-subject fixture -> ${gaitAnimalFindings.length} finding(s)`);
  console.log(`  posture-paraphrase-denial fixture -> ${postureParaphraseDenialFindings.length} finding(s)`);
  console.log(`  diagnostic-technical-usage fixture -> ${diagnosticTechnicalFindings.length} finding(s)`);
  console.log(`  diagnosis-animal-claim fixture -> ${diagnosisAnimalFindings.length} finding(s)`);
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
