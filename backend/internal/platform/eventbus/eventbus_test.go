package eventbus

import (
	"context"
	"errors"
	"testing"
)

func TestInProcessBusDispatchesToSubscribers(t *testing.T) {
	bus := NewInProcessBus()
	var aCalls, bCalls int
	bus.Subscribe("goat.created", HandlerFunc(func(_ context.Context, _ Event) error { aCalls++; return nil }))
	bus.Subscribe("goat.created", HandlerFunc(func(_ context.Context, _ Event) error { bCalls++; return nil }))
	bus.Subscribe("goat.exited", HandlerFunc(func(_ context.Context, _ Event) error { t.Fatal("wrong handler"); return nil }))

	if err := bus.Publish(context.Background(), Event{Type: "goat.created", TenantID: "t", Key: "g1"}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if aCalls != 1 || bCalls != 1 {
		t.Fatalf("expected both goat.created handlers called once, got a=%d b=%d", aCalls, bCalls)
	}
}

func TestInProcessBusUnknownTypeIsNoop(t *testing.T) {
	bus := NewInProcessBus()
	if err := bus.Publish(context.Background(), Event{Type: "nobody.listening"}); err != nil {
		t.Fatalf("unknown type should be no-op, got %v", err)
	}
}

func TestInProcessBusJoinsHandlerErrors(t *testing.T) {
	bus := NewInProcessBus()
	errA := errors.New("a failed")
	errB := errors.New("b failed")
	bus.Subscribe("x", HandlerFunc(func(_ context.Context, _ Event) error { return errA }))
	bus.Subscribe("x", HandlerFunc(func(_ context.Context, _ Event) error { return errB }))

	err := bus.Publish(context.Background(), Event{Type: "x"})
	if err == nil || !errors.Is(err, errA) || !errors.Is(err, errB) {
		t.Fatalf("expected joined errors A+B, got %v", err)
	}
}

func TestPermanentErrorClassificationRequiresAllJoinedErrorsPermanent(t *testing.T) {
	permanentA := errors.New("bad event")
	permanentB := errors.New("bad payload")
	transient := errors.New("database unavailable")

	if !IsPermanentError(PermanentError(permanentA)) {
		t.Fatalf("plain permanent error was not classified")
	}
	if !IsPermanentError(errors.Join(PermanentError(permanentA), PermanentError(permanentB))) {
		t.Fatalf("joined permanent errors were not classified")
	}
	if IsPermanentError(errors.Join(PermanentError(permanentA), transient)) {
		t.Fatalf("mixed permanent/transient join must remain retryable")
	}
	if IsPermanentError(errors.New("plain transient")) {
		t.Fatalf("plain transient error classified as permanent")
	}
}
