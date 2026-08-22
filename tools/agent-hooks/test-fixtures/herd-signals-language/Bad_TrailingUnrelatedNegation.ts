// Bad_TrailingUnrelatedNegation.ts — adversarial FAIL fixture for
// check-herd-signals-language.mjs. Round 5, bypass 2: an EARLIER version
// of isClaimShaped returned "not a claim" whenever ANY negation existed
// anywhere in the sentence and no "detect" verb was present, regardless of
// whether that negation actually denied the claim -- "The animal is
// eating, but the battery is not low" is a genuine, unhedged eating claim
// followed by an unrelated, trailing negation about the battery, and used
// to pass with zero findings. This file must never be wired into a real
// build target; it exists only for the guard's --self-test.

// The animal is eating, but the battery is not low.
export const note = true;
