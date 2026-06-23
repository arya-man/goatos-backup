package sopbridge

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

type captureBus struct{ events []eventbus.Event }

func (b *captureBus) Subscribe(string, eventbus.Handler) {}
func (b *captureBus) Publish(_ context.Context, e eventbus.Event) error {
	b.events = append(b.events, e)
	return nil
}

type fakeLister struct{ ids []string }

func (f fakeLister) RecordedCompletionsByTask(context.Context, string, string) ([]string, error) {
	return f.ids, nil
}

func TestVerifyFanoutEmitsPerCompletion(t *testing.T) {
	ctx := context.Background()

	// Verify → one accepted event per recorded completion.
	bus := &captureBus{}
	n, err := NewVerifyFanout(fakeLister{ids: []string{"c1", "c2"}}, bus).
		OnTaskVerified(ctx, "tenant-1", "task-1", "verifier-1")
	if err != nil || n != 2 || len(bus.events) != 2 {
		t.Fatalf("verify fanout: n=%d events=%d err=%v", n, len(bus.events), err)
	}
	for i, want := range []string{"c1", "c2"} {
		e := bus.events[i]
		if e.Type != vaccapp.EventVaccinationVerifyAccepted || e.TenantID != "tenant-1" || e.Key != want {
			t.Fatalf("event %d: type=%s tenant=%s key=%s", i, e.Type, e.TenantID, e.Key)
		}
		var p vaccapp.VerificationEvent
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			t.Fatalf("payload %d: %v", i, err)
		}
		if p.CompletionID != want || p.VerifiedBy != "verifier-1" {
			t.Fatalf("payload %d: %+v", i, p)
		}
	}

	// Rework → one rejected event carrying the reason.
	rbus := &captureBus{}
	rn, err := NewVerifyFanout(fakeLister{ids: []string{"c3"}}, rbus).
		OnTaskReworked(ctx, "tenant-1", "task-1", "verifier-1", "blurry proof")
	if err != nil || rn != 1 || len(rbus.events) != 1 {
		t.Fatalf("rework fanout: n=%d events=%d err=%v", rn, len(rbus.events), err)
	}
	if rbus.events[0].Type != vaccapp.EventVaccinationVerifyRejected {
		t.Fatalf("rework event type: %s", rbus.events[0].Type)
	}
	var rp vaccapp.VerificationEvent
	if err := json.Unmarshal(rbus.events[0].Payload, &rp); err != nil {
		t.Fatalf("rework payload: %v", err)
	}
	if rp.CompletionID != "c3" || rp.Reason != "blurry proof" {
		t.Fatalf("rework payload: %+v", rp)
	}

	// No recorded completions → no events.
	ebus := &captureBus{}
	if en, err := NewVerifyFanout(fakeLister{ids: nil}, ebus).OnTaskVerified(ctx, "t", "task", "v"); err != nil || en != 0 || len(ebus.events) != 0 {
		t.Fatalf("empty fanout: n=%d events=%d err=%v", en, len(ebus.events), err)
	}
}
