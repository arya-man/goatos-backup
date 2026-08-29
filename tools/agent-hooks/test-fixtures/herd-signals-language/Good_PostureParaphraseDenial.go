// Good_PostureParaphraseDenial.go — adversarial PASS fixture for
// check-herd-signals-language.mjs. Round 5: the posture-paraphrase family
// (resting, sleeping, grazing, dozing) added to BANNED_TERMS after a live
// "Resting for short periods is normal" leak must still pass cleanly when
// genuinely DENIED, exactly like every other banned term. This file must
// never be wired into a real build target; it exists only for the guard's
// --self-test.

package fixture

// The tag does not detect resting or sleeping, grazing, or dozing.
func note() {}
