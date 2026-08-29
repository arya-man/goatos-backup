// Good_TwoDenialsSameLine.ts — adversarial PASS fixture for
// check-herd-signals-language.mjs. Round 4: TWO independent denial
// sentences, each with its own banned term, sharing one physical line —
// both occurrences must be judged (correctly) as denials in their own
// fragment, not merged into a single verdict for the whole line. This file
// must never be wired into a real build target; it exists only for the
// guard's --self-test to load as a fixture.

// The tag does not detect eating. It also does not detect rumination.
export const note = true;
