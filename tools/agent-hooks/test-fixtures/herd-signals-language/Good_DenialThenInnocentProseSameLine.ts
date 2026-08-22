// Good_DenialThenInnocentProseSameLine.ts — adversarial PASS fixture for
// check-herd-signals-language.mjs. Round 4: a denial fragment followed by
// completely innocent prose (no banned term at all) on the SAME line must
// still pass — proves per-occurrence judging doesn't over-correct into
// flagging a denial sentence itself, or anything that merely sits near one.
// This file must never be wired into a real build target; it exists only
// for the guard's --self-test to load as a fixture.

// the tag does not detect eating. Motion count is cumulative.
export const note = true;
