package bootstrap

import (
	"os"
	"strings"
	"testing"
)

// TestAPIDoesNotWireInstantFeedCompletionStore keeps the pre-gate instant feed-completion path
// unwired in the API composition root.
//
// The ratified contract is submit -> pending_verification -> verifier approve -> completed. With
// WithCompletionStore wired, feeddirection.CompleteSession writes 'completed' at operator submit,
// which is a live bypass of that gate. Unwired, it fails closed with ports.ErrCompletionUnavailable.
// api.go is a composition root with no injectable seam, so the assertion is on its source — the
// same thing a reviewer would check, made mechanical.
func TestAPIDoesNotWireInstantFeedCompletionStore(t *testing.T) {
	src, err := os.ReadFile("api.go")
	if err != nil {
		t.Fatalf("read api.go: %v", err)
	}
	for _, line := range strings.Split(string(src), "\n") {
		code := strings.TrimSpace(line)
		if strings.HasPrefix(code, "//") {
			continue
		}
		if strings.Contains(code, "WithCompletionStore(") {
			t.Fatalf("api.go wires the instant feed-completion store (%q); that path completes a feed session at operator submit and bypasses the verifier gate", code)
		}
	}
}
