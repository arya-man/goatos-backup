// Good_OneLineDenial.go — adversarial PASS fixture for
// check-herd-signals-language.mjs. A complete, single-sentence, single-line
// denial ("This is not the animal's body temperature.") must still pass:
// mid-line sentence splitting must not fragment a single sentence just
// because it contains "is"/"not"/other short words, and the negation and
// the term are in the same (only) sentence on the line. This file must
// never be wired into a real build target; it exists only for the guard's
// --self-test to load as a fixture.

package fixture

// This is not the animal's body temperature.
func note() {}
