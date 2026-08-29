// Bad_DiagnosisAnimalClaim.go — adversarial FAIL fixture for
// check-herd-signals-language.mjs. Round 6: the flip side of
// Good_DiagnosticTechnicalUsage.go — the VERB forms of "diagnos*"
// (diagnoses/diagnosed/diagnose/diagnosing) co-occurring with an animal
// subject ARE the banned claim (the system diagnosing an animal's
// disease), and must still be caught even though "diagnostic"/"diagnosis"
// as a noun/adjective describing a data field now passes. This file must
// never be wired into a real build target; it exists only for the guard's
// --self-test.

package fixture

// The tag diagnoses the animal.
func noteA() {}

// The goat has disease diagnosed by the tag.
func noteB() {}

// This module detects disease.
func noteC() {}
