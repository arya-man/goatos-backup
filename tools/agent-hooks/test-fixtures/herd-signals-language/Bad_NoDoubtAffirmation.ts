// Bad_NoDoubtAffirmation.ts — adversarial FAIL fixture for
// check-herd-signals-language.mjs. Round 5, bypass 3: NEGATION_RE's bare
// "no" used to match inside the AFFIRMING idiom "no doubt" (also "no
// question", "no longer"), so a sentence asserting certainty about a claim
// read as a denial of that same claim purely because of the word "no".
// This file must never be wired into a real build target; it exists only
// for the guard's --self-test.

// There is no doubt this tag detected eating today.
export const note = true;
