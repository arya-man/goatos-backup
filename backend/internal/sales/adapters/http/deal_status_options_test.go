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
	live := toDealPayload(domain.Deal{DealID: "d2", Status: domain.StatusAdvancePaid})
	if len(live.StatusOptions) != len(domain.Statuses) {
		t.Fatalf("a live deal must offer every status, got %v", live.StatusOptions)
	}
}
