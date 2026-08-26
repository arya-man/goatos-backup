package main

import (
	"os"
	"strings"
	"testing"
)

// Inventory-vaccine director tasks are time-triggered from vaccination drive
// assignments, including rows inserted directly in the database. The reconciler
// must therefore run in the always-on kernel worker's operational lane, not only
// behind UI writes or the hourly generation cadence.
func TestKernelWorkerSchedulesPcCareInventoryVaccineOnOperationalCadence(t *testing.T) {
	raw, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	source := string(raw)
	const ctor = "kernelstages.NewPcCareInventoryVaccineStage(deps, tenantID)"
	if !strings.Contains(source, ctor) {
		t.Fatal("kernel worker does not register the PC Care inventory-vaccine stage")
	}
	idx := strings.Index(source, ctor)
	preceding := source[:idx]
	cadence := strings.LastIndex(preceding, "supervisor.RegisterCadence")
	if cadence < 0 {
		t.Fatal("PC Care inventory-vaccine stage is not attached to a supervisor cadence")
	}
	lane := source[cadence:idx]
	if !strings.Contains(lane, `"operational"`) {
		t.Fatalf("PC Care inventory-vaccine stage is not on the operational cadence lane; registered under: %q", strings.SplitN(lane, "\n", 2)[0])
	}
	if !strings.Contains(lane, "5*time.Minute") {
		t.Fatalf("PC Care inventory-vaccine stage is not on the 5-minute operational cadence; registered under: %q", strings.SplitN(lane, "\n", 2)[0])
	}
}
