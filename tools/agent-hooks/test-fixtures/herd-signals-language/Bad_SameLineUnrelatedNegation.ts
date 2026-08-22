// Bad_SameLineUnrelatedNegation.ts — adversarial FAIL fixture for
// check-herd-signals-language.mjs. Reproduces the coordinator's exact
// exploit of the sentence-scoped (but still line-granularity) denial fix:
// an unrelated, fully-formed denial sentence ("No cursor here.") shares ONE
// PHYSICAL LINE with a genuine, unhedged behavior claim ("The gateway
// confirms the animal is eating right now."). A line-granularity sentence
// walk treated the whole line as one blob and let the first sentence's
// negation suppress the second sentence's claim. Mid-line sentence
// splitting must treat these as two independent sentences and still flag
// the claim. This file must never be wired into a real build target; it
// exists only for the guard's --self-test to load as a fixture.

// No cursor here. The gateway confirms the animal is eating right now.
export const note = true;
