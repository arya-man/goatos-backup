package app

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// Mixed done paths: 3 goats done via completion records + 2 via proof-only. SQL's
// per-animal union DoneCount is 5; the API display counts must use it, not the
// legacy max-of-counters (which would report done=3, open=2 on a finished group).
func TestExecutionDisplayCountsUsesUnionDoneCountForMixedPaths(t *testing.T) {
	p := domain.ExecutionProjection{
		ObligationCount:     5,
		DoneCount:           5,
		CompletedCount:      3,
		ProofSubmittedCount: 2,
	}
	target, open, done := executionDisplayCounts(p)
	if target != 5 || open != 0 || done != 5 {
		t.Fatalf("mixed-path counts: target=%d open=%d done=%d, want 5/0/5", target, open, done)
	}
}

// Fallback: projection without DoneCount (older reader) keeps legacy behavior.
func TestExecutionDisplayCountsFallbackWithoutDoneCount(t *testing.T) {
	p := domain.ExecutionProjection{
		ObligationCount:     5,
		CompletedCount:      1,
		ProofSubmittedCount: 1,
	}
	target, open, done := executionDisplayCounts(p)
	if target != 5 || open != 4 || done != 1 {
		t.Fatalf("fallback counts: target=%d open=%d done=%d, want 5/4/1", target, open, done)
	}
}
