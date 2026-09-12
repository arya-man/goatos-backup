package eventwiring

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

// spyBus records how many handlers subscribe to each event type without delivering anything.
type spyBus struct{ subs map[string]int }

func (b *spyBus) Subscribe(eventType string, _ eventbus.Handler) { b.subs[eventType]++ }
func (b *spyBus) Publish(_ context.Context, _ eventbus.Event) error {
	return nil
}

// TestRegisterVerificationAppliersRegistersAllEleven is the drift guard for the incident this package
// was created to fix: every process with a domain bus must register EVERY verdict applier, or a
// verifier's approval is silently dropped and the session/move/observation stays
// pending_verification. Because all three binaries call this one function, asserting the function
// subscribes all six appliers (to BOTH the approved and rework verdicts) is enough to catch a
// dropped applier. nil stores are fine here: nothing is published, so no handler method runs.
func TestRegisterVerificationAppliersRegistersAllEleven(t *testing.T) {
	bus := &spyBus{subs: map[string]int{}}

	RegisterVerificationAppliers(bus, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	// shifting + milk-preparation + feed-distribution + feed-packing + feed-transport + feed-wastage
	// + weighing + pc-care + health + milk-feeding + pen-visits = 11 appliers, each subscribing to
	// BOTH verdict types. Weighing and milk preparation each joined this list because they
	// enqueued a verification item with no consumer at all, so every verdict for them was a
	// silent drop. Milk FEEDING joined on 2026-09-11: it was registered in two side processes and
	// never on the API bus or the relay, so a milk-feeding reject applied nowhere that mattered.
	// Pen visits joined on 2026-09-12: the visit video is the last clip of a pen's care chain.
	const wantAppliers = 11
	for _, eventType := range []string{
		"verification.verdict.approved",
		"verification.verdict.rework",
	} {
		if got := bus.subs[eventType]; got != wantAppliers {
			t.Fatalf("%s subscribers = %d, want %d (shifting + milk-preparation + milk-feeding + feed-distribution + feed-packing + feed-transport + feed-wastage + weighing + pc-care + health + pen-visits)", eventType, got, wantAppliers)
		}
	}
	// The pen visit's parent closure rides its own durable event (maintainer decision
	// 2026-09-12); a bus without this subscriber verifies visits that never close the task.
	if got := bus.subs["pen_visit.verified"]; got != 1 {
		t.Fatalf("pen_visit.verified subscribers = %d, want 1", got)
	}
}
