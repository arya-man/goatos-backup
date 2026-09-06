package http

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
)

// The Stock card's `not_started` must cross the wire, and cross it with the
// name the OpenAPI schema declares required. The DTO is built by a direct
// struct conversion from domain.StockItem, so a field added on one side and
// not the other is a compile error -- but a field that never reaches the JSON
// is not, and that is the "scaffolded is not wired" defect class: the client
// faithfully renders a card that can never say feeding has not begun.
func TestStockItemWireCarriesNotStarted(t *testing.T) {
	item := domain.StockItem{
		FarmLabel: "CBE", FeedItemLabel: "Mesha Kids Concentrate",
		FeedItemKey: "mesha_kids_concentrate", BalanceKg: "4300.0",
		AvgDailyKg: "", DaysLeft: nil, LatestBatchNo: 351,
		LowStock: false, NotStarted: true,
	}
	raw, err := json.Marshal(stockItemDTO(item))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(raw)
	for _, want := range []string{
		`"not_started":true`,
		`"low_stock":false`,
		`"days_left":null`,
		`"balance_kg":"4300.0"`,
		`"latest_batch_no":351`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("stock item wire is missing %s\n got: %s", want, got)
		}
	}

	// A card WITH a burn rate must not claim feeding has not started -- the two
	// states are exclusive and the client picks its copy off this one boolean.
	fed := item
	fed.NotStarted = false
	fed.AvgDailyKg = "324.6"
	days := int64(1)
	fed.DaysLeft = &days
	fed.LowStock = true
	raw, err = json.Marshal(stockItemDTO(fed))
	if err != nil {
		t.Fatalf("marshal fed: %v", err)
	}
	if got := string(raw); !strings.Contains(got, `"not_started":false`) || !strings.Contains(got, `"days_left":1`) {
		t.Errorf("fed item wire: %s", got)
	}
}
