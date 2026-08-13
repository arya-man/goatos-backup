package main

import (
	"os"
	"strings"
	"testing"
)

// BUG-016: `backfill-goat-created` had ZERO non-dev call sites — recovery of a
// lost goat.created event was manual-only. The recovery stage must be REGISTERED
// on a cadence in the deployed worker; a stage that exists but is never
// scheduled repairs nothing.
func TestKernelWorkerSchedulesGoatCreatedRecovery(t *testing.T) {
	raw, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	source := string(raw)
	if !strings.Contains(source, "kernelstages.NewGoatCreatedRecoveryStage(deps, tenantID)") {
		t.Fatal("kernel worker does not register the goat.created recovery stage")
	}
	idx := strings.Index(source, "kernelstages.NewGoatCreatedRecoveryStage")
	preceding := source[:idx]
	cadence := strings.LastIndex(preceding, "supervisor.RegisterCadence")
	if cadence < 0 {
		t.Fatal("goat.created recovery stage is not attached to a supervisor cadence")
	}
}

// Feed direction and packing worklists are generated previews until the daily
// direction is issued. If the lifecycle stage is not registered in the one
// deployed kernel worker, no rows are frozen and the same day becomes an honest
// empty historical read after midnight.
func TestKernelWorkerSchedulesFeedDirectionLifecycle(t *testing.T) {
	raw, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	source := string(raw)
	if !strings.Contains(source, "kernelstages.NewFeedDirectionLifecycleStage(deps, tenantID)") {
		t.Fatal("kernel worker does not register the feed direction issue/amend/lock lifecycle stage")
	}
	idx := strings.Index(source, "kernelstages.NewFeedDirectionLifecycleStage")
	preceding := source[:idx]
	cadence := strings.LastIndex(preceding, "supervisor.RegisterCadence")
	if cadence < 0 {
		t.Fatal("feed direction lifecycle stage is not attached to a supervisor cadence")
	}
}
