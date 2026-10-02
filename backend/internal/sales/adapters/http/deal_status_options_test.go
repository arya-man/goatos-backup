package http

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/sales/domain"
)

// TestDealPayloadOffersNoStatusOnAFailedDeal: the web and phone status editors render the deal's
// own status_options and hide themselves on an empty list (Deal Failed is final, 2026-09-25). The
// list must serialise as [] -- a null would read as "an older server, offer everything".
func TestDealPayloadOffersNoStatusOnAFailedDeal(t *testing.T) {
	failed, err := json.Marshal(toDealPayload(domain.Deal{DealID: "d1", Status: domain.StatusDealFailed}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(failed), `"status_options":[]`) {
		t.Fatalf("a failed deal must offer an EMPTY status list, got %s", failed)
	}
	live := toDealPayload(domain.Deal{DealID: "d2", Status: domain.StatusAdvancePaid, ProductType: domain.ProductSheep, Breed: "Anantapur"})
	if len(live.StatusOptions) != len(domain.Statuses) {
		t.Fatalf("a live deal must offer every status, got %v", live.StatusOptions)
	}
	// An advance-only sale (2026-10-02) names no product yet and cannot close, so the editor is
	// never offered a close the server would refuse.
	bare := toDealPayload(domain.Deal{DealID: "d3", Status: domain.StatusAdvancePaid})
	if !bare.AdvanceOnly {
		t.Fatal("a sale with no product and no lines must read advance_only")
	}
	for _, s := range bare.StatusOptions {
		if s == domain.StatusDealClosed {
			t.Fatalf("an advance-only sale must not offer Deal Closed, got %v", bare.StatusOptions)
		}
	}
}
