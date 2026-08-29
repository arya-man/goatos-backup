// Good_DiagnosticTechnicalUsage.go — adversarial PASS fixture for
// check-herd-signals-language.mjs. Round 6: diagnostic and diagnostics in
// their ordinary engineering sense (a data field, a value kept for
// troubleshooting) must pass cleanly. This is not incidental vocabulary:
// the maintainer established that the gateway's own clock is untrusted
// (it runs a constant +02:30:00 offset ahead of real IST) and is stored
// only as diagnostic data, never rendered — so this vocabulary will appear
// repeatedly in real comments. This file must never be wired into a real
// build target; it exists only for the guard's --self-test.

package fixture

// A diagnostic value for troubleshooting; never rendered to an operator.
var diagnosticValue int

// received_at is truth for ordering/gaps regardless -- this is diagnostic only.
func noteA() {}

// Stored for diagnostics only, never rendered.
func noteB() {}

// Kept for diagnostic purposes: this is a diagnostic field.
func noteC() {}
