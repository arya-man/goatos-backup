package main

import (
	"os"
	"strings"
	"testing"
)

// The 17:30 feed-proof-times Slack post is a STAGE ON THE SHARED CADENCE, not a cron job: the
// task-kernel lock forbids a module keeping a private scheduler, and the "once per day at 17:30"
// property comes from the notifier's cutoff gate plus its business-date idempotency key. A notifier
// that exists but is never scheduled posts nothing, which is exactly the failure mode nobody notices
// -- the channel simply stays quiet, and quiet is indistinguishable from "no sheet today".
func TestKernelWorkerSchedulesTheFeedProofTimesPost(t *testing.T) {
	raw, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	source := string(raw)
	if !strings.Contains(source, "kernelstages.NewFeedProofTimesStage(deps, tenantID, logger)") {
		t.Fatal("kernel worker does not register the daily feed-proof-times Slack post")
	}
	idx := strings.Index(source, "kernelstages.NewFeedProofTimesStage")
	if strings.LastIndex(source[:idx], "supervisor.RegisterCadence") < 0 {
		t.Fatal("feed-proof-times stage is not attached to a supervisor cadence")
	}
}
