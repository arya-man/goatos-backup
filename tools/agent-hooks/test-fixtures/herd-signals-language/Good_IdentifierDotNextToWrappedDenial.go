// Good_IdentifierDotNextToWrappedDenial.go — adversarial PASS fixture for
// check-herd-signals-language.mjs. A dotted identifier reference
// (`item.motion_count`) sits directly next to a legitimate denial sentence
// that wraps across two lines. The identifier's internal period has no
// following whitespace, so it must never be mistaken for a sentence
// boundary — that would either wrongly fragment the denial (if it dropped
// the merge) or, if handled sloppily, still work by accident; this fixture
// pins that item.motion_count specifically does not trigger a spurious
// split. The denial itself (negation on line 1, term on line 2) must still
// read as one sentence and pass, exactly like the earlier wrapped-denial
// fixtures. This file must never be wired into a real build target; it
// exists only for the guard's --self-test to load as a fixture.

package fixture

// item.motion_count is not the
// animal's body temperature; it is the tag's own housing reading.
func note() {}
