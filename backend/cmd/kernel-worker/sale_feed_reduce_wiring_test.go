package main

import (
	"os"
	"strings"
	"testing"
)

// The feed-day reminder after a sale is a STAGE ON THE SHARED CADENCE, not a cron job: the
// task-kernel lock forbids a module keeping a private scheduler, and "once, on the feed day" comes
// from the notifier's gate plus its per-confirm idempotency key. A notifier that exists but is never
// scheduled reminds nobody, and quiet is indistinguishable from "no sale this week".
func TestKernelWorkerSchedulesTheSaleFeedReduceReminder(t *testing.T) {
	raw, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	source := string(raw)
	if !strings.Contains(source, "kernelstages.NewSaleFeedReduceReminderStage(deps, tenantID, logger)") {
		t.Fatal("kernel worker does not register the sale feed-reduce reminder")
	}
	idx := strings.Index(source, "kernelstages.NewSaleFeedReduceReminderStage")
	if strings.LastIndex(source[:idx], "supervisor.RegisterCadence") < 0 {
		t.Fatal("sale feed-reduce reminder stage is not attached to a supervisor cadence")
	}
}
