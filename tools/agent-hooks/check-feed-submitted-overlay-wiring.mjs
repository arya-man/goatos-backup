#!/usr/bin/env node
// check-feed-submitted-overlay-wiring.mjs — keeps the queued-submit badge overlay WIRED.
//
// Bug this exists for (254.mp4): an operator submitted a shed-session, returned to the worklist,
// and the row still read "Pending" because the write was only in the outbox. Every Feed list
// renders its chip from `lifecycleStatus`, so the fix has THREE parts that must all stay present:
//
//   1. the submit ViewModel records the grain on ENQUEUE SUCCESS (markSubmittedForReview),
//   2. the list ViewModel COMBINES feedCompletionStore.submittedForReviewKeys, and
//   3. its row mapping runs the status through overlayFeedLifecycleStatus.
//
// Deleting ANY ONE of those silently restores the bug while every unit test still passes: the
// rule's own tests (FeedPacking/FeedDirectionSubmittedForReviewOverlayTest) call the pure function
// directly, so they cannot see a severed call site. That is exactly the gap this guard closes.
//
// A paging-level test was tried first and rejected: `rows` ends in `cachedIn(viewModelScope)`, and
// `asSnapshot()` over a cached PagingData never settles under `runTest` (UncompletedCoroutinesError
// after 1m). A structural guard is deterministic where that test was flaky.
//
// Modes:
//   (default)     verify the wiring in the real sources.
//   --self-test   run the built-in adversarial fixtures and exit.

import fs from "node:fs";
import path from "node:path";

const repo = path.resolve(new URL("../..", import.meta.url).pathname);
const vm = "apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel";

/** List ViewModels: must combine the submitted set AND apply the shared rule. */
const LIST_VIEW_MODELS = [
  `${vm}/FeedPackingViewModel.kt`,
  `${vm}/FeedDirectionViewModel.kt`,
];

/** Task-grain lists (Feed Transport, Milk Feeding): own status vocabulary, own overlay rule. */
const TASK_GRAIN_LISTS = [
  { file: `${vm}/FeedTransportViewModel.kt`, rule: "overlayTransportStatus(" },
  { file: `${vm}/MilkFeedingViewModel.kt`, rule: "overlayMilkFeedingStatus(" },
  { file: `${vm}/MilkPreparationViewModel.kt`, rule: "overlayMilkPreparationStatus(" },
];

/** Submit ViewModels: must record the grain when the enqueue succeeds. */
const SUBMIT_VIEW_MODELS = [
  `${vm}/FeedPackingCompleteViewModel.kt`,
  `${vm}/FeedCompleteViewModel.kt`,
  `${vm}/FeedDistributionCompleteViewModel.kt`,
  `${vm}/FeedTransportViewModel.kt`,
  `${vm}/MilkFeedingViewModel.kt`,
  `${vm}/MilkPreparationViewModel.kt`,
];

/** The engine that must RETRACT the badge when a submit dies. */
const SYNC_ENGINE =
  "apps/goatos-android/core/core-data/src/main/kotlin/sg/mesha/goatos/core/data/sync/SyncEngine.kt";

// ---- pure analysis (unit-tested by --self-test) ---------------------------------------------

export function checkListViewModel(source, label) {
  if (!source.includes("submittedForReviewKeys")) {
    throw new Error(
      `${label}: list no longer combines feedCompletionStore.submittedForReviewKeys — a queued ` +
        `submit cannot reach the row, so the chip stays "Pending" (254.mp4 regression)`,
    );
  }
  if (!source.includes("overlayFeedLifecycleStatus(")) {
    throw new Error(
      `${label}: row mapping no longer calls overlayFeedLifecycleStatus — the chip renders the ` +
        `raw backend lifecycleStatus and a queued submit reads "Pending"`,
    );
  }
}

export function checkTaskGrainList(source, rule, label) {
  if (!source.includes("submittedForReviewKeys")) {
    throw new Error(`${label}: task-grain list no longer combines submittedForReviewKeys`);
  }
  if (!source.includes(rule)) {
    throw new Error(`${label}: row mapping no longer calls ${rule} — a queued submit reads stale`);
  }
}

export function checkSubmitViewModel(source, label) {
  if (!source.includes("markSubmittedForReview(")) {
    throw new Error(
      `${label}: submit no longer calls markSubmittedForReview on enqueue success — nothing ` +
        `records the grain, so the list overlay has nothing to apply`,
    );
  }
}

/** The rule itself must stay a single shared definition, not be copied per flow. */
/**
 * The badge is a CLAIM that work was sent. When the outbox row dies (rejected, or attempts
 * exhausted) the claim must be retracted, or the list keeps lying about sent work — worse than the
 * stale "Pending" the overlay exists to fix. Enforced structurally because nothing else fails.
 */
export function checkTerminalClear(source, label) {
  if (!source.includes("clearSubmittedForReview(")) {
    throw new Error(
      `${label}: nothing clears the optimistic badge on terminal failure — a rejected or ` +
        `exhausted submit would keep reading "In review" until logout or the next business day`,
    );
  }
  if (!source.includes("submittedOverlayKeyOf(")) {
    throw new Error(`${label}: the opType -> grain-key mapping used to clear the badge is gone`);
  }
}

export function checkSingleRuleDefinition(sources) {
  const definitions = sources.filter((s) => s.body.includes("internal fun overlayFeedLifecycleStatus"));
  if (definitions.length !== 1) {
    throw new Error(
      `overlayFeedLifecycleStatus must have exactly ONE definition (found ${definitions.length}: ` +
        `${definitions.map((d) => d.label).join(", ")}). Copies drift apart — that is how Feed ` +
        `Direction and Feed Packing ended up with different behaviour.`,
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
  const goodList = `
    combine(_filters, feedCompletionStore.completedKeys, feedCompletionStore.submittedForReviewKeys) { a, b, c -> Triple(a, b, c) }
    val overlaid = overlayFeedLifecycleStatus(lifecycleStatus = lifecycleStatus, reworkReason = "", isLocallySubmittedForReview = x)
  `;
  const goodSubmit = `feedCompletionStore.markSubmittedForReview(completionKey)`;

  checkListViewModel(goodList, "fixture");
  console.log("  ok   accepts a correctly wired list ViewModel");
  checkSubmitViewModel(goodSubmit, "fixture");
  console.log("  ok   accepts a correctly wired submit ViewModel");

  expectThrow(
    () => checkListViewModel(goodList.replace("feedCompletionStore.submittedForReviewKeys", "emptySet()"), "fixture"),
    "a list that stopped combining submittedForReviewKeys",
  );
  expectThrow(
    () => checkListViewModel(goodList.replace("overlayFeedLifecycleStatus(", "passThrough("), "fixture"),
    "a row mapping that dropped the overlay call",
  );
  checkTaskGrainList("submittedForReviewKeys ... overlayTransportStatus(x)", "overlayTransportStatus(", "fixture");
  console.log("  ok   accepts a correctly wired task-grain list");
  expectThrow(
    () => checkTaskGrainList("submittedForReviewKeys only", "overlayTransportStatus(", "fixture"),
    "a task-grain list missing its overlay rule",
  );
  checkTerminalClear("submittedOverlayKeyOf(item) ... clearSubmittedForReview(it)", "fixture");
  console.log("  ok   accepts an engine that retracts the badge on terminal failure");
  expectThrow(
    () => checkTerminalClear("report(TERMINAL)", "fixture"),
    "an engine that never clears the badge on terminal failure",
  );
  expectThrow(
    () => checkSubmitViewModel("analytics.track(SUBMITTED)", "fixture"),
    "a submit that never records the grain",
  );
  expectThrow(
    () =>
      checkSingleRuleDefinition([
        { label: "a.kt", body: "internal fun overlayFeedLifecycleStatus(" },
        { label: "b.kt", body: "internal fun overlayFeedLifecycleStatus(" },
      ]),
    "a second copy of the rule",
  );

  checkSingleRuleDefinition([
    { label: "a.kt", body: "internal fun overlayFeedLifecycleStatus(" },
    { label: "b.kt", body: "overlayFeedLifecycleStatus(...)" },
  ]);
  console.log("  ok   accepts exactly one definition with many call sites");
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
  }
  for (const { file, rule } of TASK_GRAIN_LISTS) {
    const { label, body } = read(file);
    checkTaskGrainList(body, rule, label);
  }
  for (const rel of SUBMIT_VIEW_MODELS) {
    const { label, body } = read(rel);
    checkSubmitViewModel(body, label);
  }
  {
    const { label, body } = read(SYNC_ENGINE);
    checkTerminalClear(body, label);
  }
  checkSingleRuleDefinition([...LIST_VIEW_MODELS, ...TASK_GRAIN_LISTS.map((t) => t.file), `${vm}/FeedLifecycleOverlay.kt`].map(read));

  console.log("feed-submitted-overlay-wiring: ok");
}

try {
  main();
} catch (error) {
  console.error(`feed-submitted-overlay-wiring: FAIL\n  ${error.message}`);
  process.exit(1);
}
