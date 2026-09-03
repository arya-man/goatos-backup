package main

import (
	"os"
	"strings"
	"testing"
)

// Pen reconciliation enqueue debt must be drained by the always-on kernel worker, not only by
// phone retries: a completed request can lose its verifier enqueue after the card already became
// pending_verification.
func TestKernelWorkerSchedulesPenReconciliationEnqueueRecoveryOnOperationalCadence(t *testing.T) {
	raw, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	source := string(raw)
	const ctor = "kernelstages.NewPenReconciliationEnqueueRecoveryStage(deps, tenantID)"
	if !strings.Contains(source, ctor) {
		t.Fatal("kernel worker does not register the pen reconciliation enqueue recovery stage")
	}
	idx := strings.Index(source, ctor)
	preceding := source[:idx]
	cadence := strings.LastIndex(preceding, "supervisor.RegisterCadence")
	if cadence < 0 {
		t.Fatal("pen reconciliation enqueue recovery stage is not attached to a supervisor cadence")
	}
	lane := source[cadence:idx]
	if !strings.Contains(lane, `"operational"`) {
		t.Fatalf("pen reconciliation enqueue recovery stage is not on the operational cadence lane; registered under: %q", strings.SplitN(lane, "\n", 2)[0])
	}
}
