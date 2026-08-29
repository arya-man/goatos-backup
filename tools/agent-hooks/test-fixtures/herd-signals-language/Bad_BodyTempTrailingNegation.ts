// Bad_BodyTempTrailingNegation.ts — adversarial FAIL fixture for
// check-herd-signals-language.mjs. Round 5, bypass 4: the body-temp-mislabel
// check had its own extra `|| !NEGATION_RE.test(window)` fallback that let
// ANY negation anywhere in the sentence excuse non-field prose regardless
// of order -- "Body temperature rose above baseline, though the signal is
// not weak." is a genuine body-temperature claim with an unrelated,
// trailing negation about signal strength, and used to pass with zero
// findings. This file must never be wired into a real build target; it
// exists only for the guard's --self-test.

// Body temperature rose above baseline, though the signal is not weak.
export const note = true;
