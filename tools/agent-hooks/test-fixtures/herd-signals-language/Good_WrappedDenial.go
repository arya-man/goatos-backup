// Good_WrappedDenial.go — adversarial PASS fixture for
// check-herd-signals-language.mjs. A denial comment wrapped across two Go
// comment lines: the negation is on the line above the banned term. Must
// never be wired into a real build target; exists only for the guard's
// --self-test.

package fixture

// This field is not the animal's
// body temperature; it is only the tag's own housing temperature, never a
// clinical reading.
func note() {}
