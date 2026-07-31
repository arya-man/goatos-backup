package main

import (
	"os"
	"strings"
	"testing"
)

// WEIGHING PHASE 2: weighing had NO time-driven kernel and the kernel worker had
// ZERO weighing awareness. The cadence must be registered inside THIS existing
// consolidated worker — no new worker binary, no Cloud Scheduler cron, no
// scheduled Cloud Run Job (see
// docs/decisions/operational-kernel-5k-50k-scale-envelope.md). A stage that
// exists but is never scheduled surfaces nothing.
func TestKernelWorkerSchedulesWeighingKernelOnOperationalCadence(t *testing.T) {
	raw, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	source := string(raw)
	const ctor = "kernelstages.NewWeighingKernelStage(deps, tenantID)"
	if !strings.Contains(source, ctor) {
		t.Fatal("kernel worker does not register the weighing kernel stage")
	}
	idx := strings.Index(source, ctor)
	preceding := source[:idx]
	cadence := strings.LastIndex(preceding, "supervisor.RegisterCadence")
	if cadence < 0 {
		t.Fatal("weighing kernel stage is not attached to a supervisor cadence")
	}
	// It must sit on the OPERATIONAL lane, not generation/housekeeping: day-start
	// surfacing and escalation must land within minutes of the Asia/Kolkata
	// business-day boundary, not up to an hour later.
	lane := source[cadence:idx]
	if !strings.Contains(lane, `"operational"`) {
		t.Fatalf("weighing kernel stage is not on the operational cadence lane; registered under: %q", strings.SplitN(lane, "\n", 2)[0])
	}
}
