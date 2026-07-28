package eventwiring

import (
	"testing"
)

// TestRegisterWorkflowConsumersRegistersAll is the drift guard for the birth/death workflow engine:
// every process with a domain bus must register the goat.created / goat.exited /
// goat.identifier.added openers and the death_evidence verdict appliers, or an approved birth/death
// silently opens no follow-up work (and a park head's verdict is dropped). All five binaries call
// RegisterWorkflowConsumers, so asserting its subscription set here is enough to catch a dropped
// handler. A nil service is fine: nothing is published, so no handler method runs.
func TestRegisterWorkflowConsumersRegistersAll(t *testing.T) {
	bus := &spyBus{subs: map[string]int{}}

	RegisterWorkflowConsumers(bus, nil, nil)

	want := map[string]int{
		"counts.death.reported":         1,
		"counts.death.rejected":         1,
		"goat.created":                  1,
		"goat.exited":                   1,
		"goat.identifier.added":         1,
		"verification.verdict.approved": 1,
		"verification.verdict.rework":   1,
	}
	for eventType, wantCount := range want {
		if got := bus.subs[eventType]; got != wantCount {
			t.Fatalf("%s subscribers = %d, want %d", eventType, got, wantCount)
		}
	}
}
