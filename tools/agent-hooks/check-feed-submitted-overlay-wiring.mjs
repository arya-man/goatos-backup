#!/usr/bin/env node
// check-feed-submitted-overlay-wiring.mjs — keeps the queued-submit badge OUTBOX-DERIVED.
//
// Bug this exists for (254.mp4): an operator submitted a shed-session, returned to the worklist,
// and the row still read "Pending" because the write was only in the outbox.
//
// The FIRST fix kept an in-memory set that was marked at enqueue and hand-cleared when the outbox
// row died. That design shipped four defects, every one of them caused by rebuilding the grain key
// in two places that could disagree:
//   * pen-day `sessionNo = 0` marked, `1` cleared            -> badge stranded on "In review"
//   * `businessDate()` read either side of midnight          -> badge stranded on "In review"
//   * null vs blank partition labels                         -> badge stranded on "In review"
//   * process death wiped the set while the row survived     -> the ORIGINAL bug, back again
//
// The current design derives the badge from ACTIVE OUTBOX ROWS, so a submit that succeeds or dies
// leaves the set by itself: nothing to clear, no second key, nothing to lose on process death.
// This guard exists to stop anyone quietly reintroducing the old shape, because every one of those
// four bugs passes unit tests right up until it reaches an operator.
//
// Modes:
//   (default)     verify the wiring in the real sources.
//   --self-test   run the built-in adversarial fixtures and exit.

import fs from "node:fs";
import path from "node:path";

const repo = path.resolve(new URL("../..", import.meta.url).pathname);
const vm = "apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel";
const data = "apps/goatos-android/core/core-data/src/main/kotlin/sg/mesha/goatos/core/data";

/** Every list that renders a verifier-gated badge. Adding a flow means adding it HERE. */
const LIST_VIEW_MODELS = [
  `${vm}/FeedPackingViewModel.kt`,
  `${vm}/FeedDirectionViewModel.kt`,
  `${vm}/FeedTransportViewModel.kt`,
  `${vm}/MilkFeedingViewModel.kt`,
  `${vm}/MilkPreparationViewModel.kt`,
];

const CODEC = `${data}/sync/SubmittedGrainKeys.kt`;
const RULE = `${vm}/FeedLifecycleOverlay.kt`;
const PROJECTION = `${data}/sync/SyncRepository.kt`;

/** APIs the old in-memory design used. Their return is the regression. */
const BANNED = ["markSubmittedForReview", "clearSubmittedForReview", "submittedForReviewKeys"];

// ---- pure analysis (unit-tested by --self-test) ---------------------------------------------

export function checkListViewModel(source, label) {
  if (!source.includes("submittedGrains.observe()")) {
    throw new Error(
      `${label}: list no longer combines the OUTBOX-derived submitted grains — a queued submit ` +
        `cannot reach the row, so the chip stays "Pending" (254.mp4 regression)`,
    );
  }
  if (!source.includes("overlayVerificationStatus(")) {
    throw new Error(
      `${label}: row mapping no longer calls overlayVerificationStatus — the chip renders the raw ` +
        `backend status and a queued submit reads "Pending"`,
    );
  }
}

/**
 * A ViewModel must not build a grain key from loose arguments.
 *
 * `shedSessionKey(date, shedId, partitionLabel, sessionNo, workflow)` is positional, so passing the
 * wrong value is invisible: the key never matches, the badge never appears, and every test and this
 * guard still pass. That exact mistake shipped once — the Feed Direction list passed `null` for a
 * partition its own submit sends as "1", reopening 254.mp4 on every partitioned shed. The row owns
 * its key now (`row.submittedGrainKey(date)`), which leaves nothing to pass wrongly.
 */
export function checkNoHandRolledKey(rawSource, label) {
  const source = stripComments(rawSource);
  for (const raw of ["shedSessionKey(", "taskGrainKey("]) {
    if (source.includes(raw)) {
      throw new Error(
        `${label}: builds a grain key by hand with '${raw}'. Use the row's own ` +
          `submittedGrainKey(...) instead — positional args are how the partition bug shipped.`,
      );
    }
  }
  if (!source.includes("submittedGrainKey(")) {
    throw new Error(`${label}: no row-owned submittedGrainKey(...) call — how is the row keyed?`);
  }
}

/** The in-memory mechanism must not come back anywhere. */
export function checkNoInMemoryOverlay(rawSource, label) {
  const source = stripComments(rawSource);
  for (const api of BANNED) {
    if (source.includes(api)) {
      throw new Error(
        `${label}: '${api}' is the in-memory badge mechanism that shipped four stranded-badge ` +
          `defects. The badge is derived from active outbox rows now — do not mark or clear it.`,
      );
    }
  }
}

/** The key must be a pure function of the payload: a clock in it reopens the midnight bug. */
/** Comments legitimately NAME the banned APIs when explaining why they are banned. */
export function stripComments(source) {
  return source.replace(/\/\*[\s\S]*?\*\//g, "").replace(/^\s*\/\/.*$/gm, "");
}

export function checkCodecHasNoClock(rawSource, label) {
  const source = stripComments(rawSource);
  const clocks = ["businessDate(", "LocalDate.now(", "System.currentTimeMillis(", "Instant.now("];
  for (const clock of clocks) {
    if (source.includes(clock)) {
      throw new Error(
        `${label}: the grain key reads the clock ('${clock}'). A key built at 23:50 and rebuilt at ` +
          `00:05 would not match, which is exactly how the badge got stranded before.`,
      );
    }
  }
  if (!source.includes("fun shedSessionKey(") || !source.includes("fun taskGrainKey(")) {
    throw new Error(`${label}: the shared key builders are gone; callers will hand-roll keys again`);
  }
}

/** The projection must read the FULL active set, never a window. */
export function checkProjectionIsUnwindowed(source, label) {
  if (!source.includes("observeSubmittedForReviewGrains")) {
    throw new Error(`${label}: the outbox-derived badge projection is gone`);
  }
  const impl = source.slice(source.indexOf("override fun observeSubmittedForReviewGrains"));
  const body = impl.slice(0, 600);
  if (body.includes("observeActiveWindow") || body.includes("observeRecentTerminals")) {
    throw new Error(
      `${label}: the badge projection uses a WINDOWED outbox read. A newest-N window silently drops ` +
        `the oldest pending submit once other features queue rows after it.`,
    );
  }
  if (!body.includes("observeActive")) {
    throw new Error(`${label}: the badge projection no longer reads the active outbox set`);
  }
}

/** One rule, one codec — copies are how Packing and Direction drifted apart. */
export function checkSingleDefinition(sources, needle, what) {
  const defs = sources.filter((s) => s.body.includes(needle));
  if (defs.length !== 1) {
    throw new Error(
      `${what} must have exactly ONE definition (found ${defs.length}: ` +
        `${defs.map((d) => d.label).join(", ")}). Copies drift apart.`,
    );
  }
}

// ---- self-test ------------------------------------------------------------------------------

function expectThrow(fn, what) {
  let threw = false;
  try {
    fn();
  } catch {
    threw = true;
  }
  if (!threw) throw new Error(`self-test: expected a failure for ${what}, got none`);
  console.log(`  ok   rejects ${what}`);
}

function selfTest() {
  const goodList = `combine(_filters, submittedGrains.observe()) { ... }
    val s = overlayVerificationStatus(backendStatus = x, reworkReason = r, isLocallySubmitted = b, inReviewToken = t)`;
  checkListViewModel(goodList, "fixture");
  console.log("  ok   accepts an outbox-derived list");
  checkNoInMemoryOverlay(goodList, "fixture");
  console.log("  ok   accepts a list with no in-memory mark/clear");

  expectThrow(
    () => checkListViewModel(goodList.replace("submittedGrains.observe()", "emptySet()"), "fixture"),
    "a list that stopped combining the outbox-derived grains",
  );
  expectThrow(
    () => checkListViewModel(goodList.replace("overlayVerificationStatus(", "passThrough("), "fixture"),
    "a list that dropped the shared overlay rule",
  );
  expectThrow(
    () => checkNoInMemoryOverlay("feedCompletionStore.markSubmittedForReview(k)", "fixture"),
    "a resurrected in-memory mark",
  );
  expectThrow(
    () => checkNoInMemoryOverlay("store.clearSubmittedForReview(k)", "fixture"),
    "a resurrected in-memory clear",
  );

  checkNoHandRolledKey("val k = row.submittedGrainKey(date)", "fixture");
  console.log("  ok   accepts a row-owned key");
  expectThrow(
    () => checkNoHandRolledKey("shedSessionKey(date, shedId, null, sessionNo, workflow)", "fixture"),
    "a ViewModel hand-rolling a shed-session key (the partition bug)",
  );
  expectThrow(
    () => checkNoHandRolledKey('taskGrainKey("feed-transport", it.taskId)', "fixture"),
    "a ViewModel hand-rolling a task key",
  );

  const goodCodec = "fun shedSessionKey( ... ) fun taskGrainKey( ... )";
  checkCodecHasNoClock(goodCodec, "fixture");
  checkCodecHasNoClock("// businessDate() is banned here\n" + goodCodec, "fixture-comment");
  console.log("  ok   a comment naming the banned clock is not a violation");
  console.log("  ok   accepts a clock-free codec");
  expectThrow(
    () => checkCodecHasNoClock(goodCodec + " businessDate()", "fixture"),
    "a grain key that reads the clock (the midnight bug)",
  );

  const goodProjection =
    "override fun observeSubmittedForReviewGrains(): Flow<Set<String>> = store.observeActive().map { }";
  checkProjectionIsUnwindowed(goodProjection, "fixture");
  console.log("  ok   accepts an unwindowed projection");
  expectThrow(
    () =>
      checkProjectionIsUnwindowed(
        goodProjection.replace("observeActive()", "observeActiveWindow(50)"),
        "fixture",
      ),
    "a windowed badge projection",
  );

  expectThrow(
    () =>
      checkSingleDefinition(
        [
          { label: "a.kt", body: "internal fun overlayVerificationStatus(" },
          { label: "b.kt", body: "internal fun overlayVerificationStatus(" },
        ],
        "internal fun overlayVerificationStatus(",
        "the overlay rule",
      ),
    "a second copy of the rule",
  );

  console.log("feed-submitted-overlay-wiring: self-test PASS");
}

// ---- main -----------------------------------------------------------------------------------

function main() {
  if (process.argv.includes("--self-test")) {
    selfTest();
    return;
  }

  const read = (rel) => {
    const abs = path.join(repo, rel);
    if (!fs.existsSync(abs)) throw new Error(`missing expected file: ${rel}`);
    return { label: rel, body: fs.readFileSync(abs, "utf8") };
  };

  for (const rel of LIST_VIEW_MODELS) {
    const { label, body } = read(rel);
    checkListViewModel(body, label);
    checkNoInMemoryOverlay(body, label);
    checkNoHandRolledKey(body, label);
  }

  const codec = read(CODEC);
  checkCodecHasNoClock(codec.body, codec.label);

  const projection = read(PROJECTION);
  checkProjectionIsUnwindowed(projection.body, projection.label);

  checkSingleDefinition(
    [read(RULE), ...LIST_VIEW_MODELS.map(read)],
    "internal fun overlayVerificationStatus(",
    "the overlay rule",
  );
  checkSingleDefinition([codec], "internal fun submittedGrainKeyOf(", "the grain-key codec");

  console.log("feed-submitted-overlay-wiring: ok (outbox-derived)");
}

try {
  main();
} catch (error) {
  console.error(`feed-submitted-overlay-wiring: FAIL\n  ${error.message}`);
  process.exit(1);
}
