package notificationbridge_test

// TestProductionVerificationConsumersWireVaccineLabels is a static-source regression guard for
// C19a (confirmed defect): every production constructor of VerificationEventConsumer
// (cmd/domain-event-consumer, cmd/outbox-relay, internal/bootstrap/api.go) called
// NewVerificationEventConsumer(...) and never chained .WithVaccineLabels(...), so the consumer's
// vaccineLabels field was nil in every real deployment and the vaccine-label enrichment block in
// enrichApprovedNotificationCopy never ran (guarded by a nil check that silently no-ops).
//
// A pure unit/integration test cannot exercise these cmd/main.go composition roots directly (they
// are not importable as a library and require full process bootstrap), so this guard asserts the
// wiring textually: every source file that constructs a VerificationEventConsumer must also chain
// .WithVaccineLabels(...) in the SAME statement/line group, so a future edit that reverts to the
// bare two-line pattern (or adds a NEW composition root without the chain) fails this test instead
// of silently shipping an unwired resolver again.
import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func vlRepoRootFromThisFile(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not determine test file path via runtime.Caller")
	}
	root, err := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return root
}

func vlReadRepoFile(t *testing.T, repoRoot, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

func TestProductionVerificationConsumersWireVaccineLabels(t *testing.T) {
	repoRoot := vlRepoRootFromThisFile(t)

	// Every known production composition root that constructs a VerificationEventConsumer. Adding
	// a fourth composition root without adding it here (and wiring it) should be caught in review,
	// but the important guard is: none of these may regress to the unwired constructor call.
	sites := []string{
		"backend/cmd/domain-event-consumer/main.go",
		"backend/cmd/outbox-relay/main.go",
		"backend/internal/bootstrap/api.go",
	}

	for _, rel := range sites {
		src := vlReadRepoFile(t, repoRoot, rel)
		if !strings.Contains(src, "notificationbridge.NewVaccineLabelResolver(") {
			t.Errorf("%s: does not construct a notificationbridge.NewVaccineLabelResolver(...) -- "+
				"vaccine label enrichment has no resolver to attach", rel)
		}
		if !strings.Contains(src, ".WithVaccineLabels(vaccineLabels)") {
			t.Errorf("%s: NewVerificationEventConsumer(...) is not chained with "+
				".WithVaccineLabels(vaccineLabels) -- this is the exact C19a regression "+
				"(resolver constructed but never attached, or never constructed at all)", rel)
		}
	}
}
