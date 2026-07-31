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

// TestRegisterVerificationAppliersRegistersAllFive is the drift guard for the incident this package
// was created to fix: every process with a domain bus must register the shifting + feed-distribution +
// feed-packing verdict appliers, or a verifier's approval is silently dropped and the session/move
// stays pending_verification. Because all three binaries now call this one function, asserting the
// function subscribes all three appliers (to BOTH the approved and rework verdicts) is enough to catch
// a dropped applier. nil stores are fine here: nothing is published, so no handler method runs.
func TestRegisterVerificationAppliersRegistersAllFive(t *testing.T) {
	bus := &spyBus{subs: map[string]int{}}

	RegisterVerificationAppliers(bus, nil, nil, nil, nil)

	// shifting + feed-distribution + feed-packing + feed-transport + weighing = 5 appliers,
	// each subscribing to BOTH verdict types. Weighing joined this list because it enqueued a
	// verification item with no consumer at all, so every weighing verdict was a silent drop.
	const wantAppliers = 5
	for _, eventType := range []string{
		"verification.verdict.approved",
		"verification.verdict.rework",
	} {
		if got := bus.subs[eventType]; got != wantAppliers {
			t.Fatalf("%s subscribers = %d, want %d (shifting + feed-distribution + feed-packing + feed-transport + weighing)", eventType, got, wantAppliers)
		}
	}
}
