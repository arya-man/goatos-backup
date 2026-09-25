package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

type releaseRecorder struct {
	calls []ports.ReleaseSaleAllocationsCommand
}

func (r *releaseRecorder) ReleaseSaleAllocations(_ context.Context, cmd ports.ReleaseSaleAllocationsCommand) (int, error) {
	r.calls = append(r.calls, cmd)
	return 0, nil
}

// TestOnlyAFailedDealReleasesItsAnimals (maintainer decision 2026-09-25): Deal Failed releases the
// deal's tagged animals, against the person who failed it; any other status releases nothing.
func TestOnlyAFailedDealReleasesItsAnimals(t *testing.T) {
	rec := &releaseRecorder{}
	h := NewSaleFailedReleaseHandler(rec)
	send := func(status string) {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"sales_deal_id": "deal-7", "status": status, "actor_id": "actor-1"})
		if err := h.HandleEvent(context.Background(), eventbus.Event{Type: EventSalesDealStatusChanged, TenantID: "tenant", Key: "deal-7", Payload: raw}); err != nil {
			t.Fatalf("%s: %v", status, err)
		}
	}
	send("Deal Closed")
	send("Advance Paid")
	if len(rec.calls) != 0 {
		t.Fatalf("a non-failed status must release nothing, got %+v", rec.calls)
	}
	send("Deal Failed")
	if len(rec.calls) != 1 || rec.calls[0].SalesDealID != "deal-7" || rec.calls[0].ActorID != "actor-1" || rec.calls[0].OccurredAt.IsZero() {
		t.Fatalf("Deal Failed must release the deal's animals, got %+v", rec.calls)
	}
}
