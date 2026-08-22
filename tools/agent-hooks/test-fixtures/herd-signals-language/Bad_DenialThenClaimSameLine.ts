// Bad_DenialThenClaimSameLine.ts — adversarial FAIL fixture for
// check-herd-signals-language.mjs. Round 4: the coordinator's exact
// exploit of the per-line (not per-occurrence) fix. A legitimate denial
// sentence and a genuine, unhedged claim sentence share ONE physical line.
// The denial ("the tag does not detect eating.") must not license the
// claim right after it ("The gateway confirms the animal is eating now.")
// on the same line — this is exactly the shape of a real doc/comment/JSX
// block, since the required disclaimer is often followed by more prose.
// Each occurrence of a banned term must be judged in its OWN fragment, not
// "does this line contain a denial anywhere". This file must never be
// wired into a real build target; it exists only for the guard's
// --self-test to load as a fixture.

// the tag does not detect eating. The gateway confirms the animal is eating now.
export const note = true;
