// Bad_UnrelatedNegationPriorSentence.ts — adversarial FAIL fixture for
// check-herd-signals-language.mjs. Reproduces the coordinator's exact
// exploit of the flat-window denial fix: an ORDINARY, unrelated negation in
// a fully-ended prior sentence/comment ("There is no cursor on the first
// page.") sits a couple of lines above a genuine, unhedged behavior claim.
// A flat line-count window treated the two sentences as one blob and let
// the earlier "no" silently suppress the claim below it — a false negative
// on exactly the thing this guard exists to catch, and trivially common
// (any nearby "no"/"not" in ordinary prose). Sentence-boundary scoping must
// stop at the period ending the first sentence and NOT let its negation
// reach into the second, independent sentence. This file must never be
// wired into a real build target; it exists only for the guard's
// --self-test to load as a fixture.

// There is no cursor on the first page.
// The gateway confirms the animal is eating right now.
export const note = true;
